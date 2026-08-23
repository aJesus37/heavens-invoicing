// Package auth provides the single-admin session layer: password setup and
// verification (bcrypt), cookie-bound sessions whose server-side state is
// only a SHA-256 token hash, login rate limiting, CSRF double-submit
// helpers, and the gate middleware that wraps every route in the app.
//
// Threat model notes:
//   - The raw session token lives only in the client's cookie; the DB can
//     never be replayed into a login.
//   - Session cookies are intentionally NOT marked Secure: this build
//     targets plain-HTTP homelab LAN deployments where TLS is not
//     terminated, and Secure would silently break login there. Flip this
//     if the app ever ships behind HTTPS.
package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"log"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/ajesus37/heavens-invoicing/internal/repo"

	"golang.org/x/crypto/bcrypt"
)

const (
	// CookieSession carries the raw base64url token.
	CookieSession = "session"
	// CookieCSRF carries the double-submit CSRF token. It must stay
	// readable by same-origin JS/forms, so it is NOT HttpOnly.
	CookieCSRF = "csrf_token"

	// SessionTTL is both the DB expiry and the cookie Max-Age so they age
	// out together (7 days).
	SessionTTL = 7 * 24 * time.Hour

	// tokenBytes is the entropy of a fresh session/CSRF token: 256 bits,
	// immune to brute force and enumeration.
	tokenBytes = 32

	// MinPasswordLength guards against instant-brute-force admin
	// passwords at first-run setup. bcrypt slows offline attacks but
	// nothing stops "123456" except refusing it up front.
	MinPasswordLength = 8
)

// ErrWeakPassword reports a rejected password during setup.
var ErrWeakPassword = errors.New("auth: password too short")

// Manager owns the auth state and policy. Construct with New; tests may
// override Now to travel through lockout windows deterministically.
type Manager struct {
	sessions *repo.SessionRepo
	settings *repo.SettingsRepo
	limiter  *loginLimiter

	// Now feeds token expiry stamping and the rate limiter's clock.
	Now func() time.Time
}

func New(sessions *repo.SessionRepo, settings *repo.SettingsRepo) *Manager {
	m := &Manager{
		sessions: sessions,
		settings: settings,
		Now:      time.Now,
	}
	m.limiter = newLoginLimiter(func() time.Time { return m.Now() })
	return m
}

// NewToken returns a fresh (raw, hash) pair. raw is base64url of 32 random
// bytes and goes only into the cookie; hash is its hex SHA-256 and is all
// that ever reaches storage.
func NewToken() (raw, hash string, err error) {
	buf := make([]byte, tokenBytes)
	if _, err := rand.Read(buf); err != nil {
		return "", "", err
	}
	raw = base64.RawURLEncoding.EncodeToString(buf)
	return raw, HashToken(raw), nil
}

// HashToken is the one-way derivation applied before persisting tokens.
func HashToken(raw string) string {
	sum := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(sum[:])
}

// NeedsSetup reports whether the admin password has been configured yet.
func (m *Manager) NeedsSetup(ctx context.Context) (bool, error) {
	_, err := m.settings.Get(ctx, repo.SettingAdminPasswordHash)
	if errors.Is(err, repo.ErrNotFound) {
		return true, nil
	}
	if err != nil {
		return false, err
	}
	return false, nil
}

// SetPassword stores a bcrypt hash of password. Refuses passwords below
// MinPasswordLength without touching storage.
func (m *Manager) SetPassword(ctx context.Context, password string) error {
	if len(password) < MinPasswordLength {
		return ErrWeakPassword
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return err
	}
	return m.settings.Set(ctx, repo.SettingAdminPasswordHash, string(hash))
}

// VerifyPassword reports whether password matches the stored hash. An
// unconfigured password simply fails verification (first-run is steered
// through the setup form instead).
func (m *Manager) VerifyPassword(ctx context.Context, password string) (bool, error) {
	stored, err := m.settings.Get(ctx, repo.SettingAdminPasswordHash)
	if errors.Is(err, repo.ErrNotFound) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	err = bcrypt.CompareHashAndPassword([]byte(stored), []byte(password))
	if errors.Is(err, bcrypt.ErrMismatchedHashAndPassword) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return true, nil
}

// StartSession creates server-side state and sets the session cookie. The
// caller owns error handling: a failure here must abort the login. A fresh
// CSRF double-submit cookie is issued alongside the session so the very
// next page the browser loads already carries a token the forms can use.
func (m *Manager) StartSession(ctx context.Context, w http.ResponseWriter) error {
	raw, hash, err := NewToken()
	if err != nil {
		return err
	}
	expires := m.Now().Add(SessionTTL)
	if err := m.sessions.Create(ctx, hash, expires); err != nil {
		return err
	}
	http.SetCookie(w, &http.Cookie{
		Name:  CookieSession,
		Value: raw,
		Path:  "/",
		// Secure deliberately omitted — see package comment.
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   int(SessionTTL / time.Second),
	})
	if _, err := m.IssueCSRFToken(w); err != nil {
		return err
	}
	return nil
}

// IssueCSRFToken mints a fresh CSRF token, plants it as the non-HttpOnly
// CookieCSRF double-submit cookie, and returns it so the caller can embed
// it in forms / htmx. It is called on session creation and on GET /login,
// guaranteeing every state-changing request can present a matching value.
// The cookie is explicitly NOT HttpOnly: same-origin JS must read it to
// feed htmx's X-CSRF-Token header, and the browser already sends it on
// every request, so HttpOnly would only break the JS path.
func (m *Manager) IssueCSRFToken(w http.ResponseWriter) (string, error) {
	raw, _, err := NewToken()
	if err != nil {
		return "", err
	}
	http.SetCookie(w, &http.Cookie{
		Name:     CookieCSRF,
		Value:    raw,
		Path:     "/",
		HttpOnly: false,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   int(SessionTTL / time.Second),
	})
	return raw, nil
}

// checkCSRF verifies the double-submit token for a state-changing request.
// The presented value must come from either the X-CSRF-Token header (htmx
// and API clients) or the csrf_token form field (plain browser posts) and
// must equal the CookieCSRF value the browser also sent. A missing cookie
// or any mismatch fails closed. The CSRF cookie is never trusted on its
// own: an attacker who can set cookies still lacks the header/field.
func (m *Manager) checkCSRF(r *http.Request) bool {
	c, err := r.Cookie(CookieCSRF)
	if err != nil || c.Value == "" {
		return false
	}
	token := r.Header.Get("X-CSRF-Token")
	if token == "" {
		// Only browser form posts use the field; JSON APIs use the header.
		if r.Method == http.MethodPost {
			if perr := r.ParseForm(); perr != nil {
				return false
			}
			token = r.PostFormValue("csrf_token")
		}
	}
	return token != "" && token == c.Value
}

// ValidSession checks the request's session cookie against live DB state.
func (m *Manager) ValidSession(r *http.Request) (bool, error) {
	hash, ok := sessionHash(r)
	if !ok {
		return false, nil
	}
	return m.sessions.GetValid(r.Context(), hash)
}

// EndSession deletes the server-side row and expires both auth cookies
// client-side. Safe to call without a session cookie (no-op delete).
func (m *Manager) EndSession(w http.ResponseWriter, r *http.Request) {
	if hash, ok := sessionHash(r); ok {
		if err := m.sessions.Delete(r.Context(), hash); err != nil {
			// Best-effort: cookies below still log the browser out; the
			// orphaned row dies at natural expiry. Log loudly regardless —
			// until then that token would still replay if stolen.
			log.Printf("auth: delete session on logout: %v", err)
		}
	}
	expired := func(name string) *http.Cookie {
		return &http.Cookie{Name: name, Value: "", Path: "/", MaxAge: -1,
			HttpOnly: name == CookieSession, SameSite: http.SameSiteLaxMode}
	}
	http.SetCookie(w, expired(CookieSession))
	http.SetCookie(w, expired(CookieCSRF))
}

// sessionHash extracts the SHA-256 hash of the session cookie's token.
func sessionHash(r *http.Request) (string, bool) {
	c, err := r.Cookie(CookieSession)
	if err != nil || c.Value == "" {
		return "", false
	}
	return HashToken(c.Value), true
}

// SweepExpired prunes dead sessions; called lazily after successful logins
// rather than from a background ticker (one indexed DELETE per login is
// plenty for a single-admin app, and there is no goroutine to supervise).
func (m *Manager) SweepExpired(ctx context.Context) error {
	return m.sessions.DeleteExpired(ctx)
}

// AllowLogin reports whether remoteAddr may attempt a login right now.
// The key comes from r.RemoteAddr ONLY: X-Forwarded-For is deliberately
// ignored because clients can forge it, letting attackers rotate fake IPs
// past the limiter or frame innocent ones. Behind a reverse proxy every
// request shares the proxy address, so lockouts apply collectively there —
// an accepted homelab tradeoff.
func (m *Manager) AllowLogin(remoteAddr string) bool {
	return m.limiter.allow(ClientIP(remoteAddr))
}

// LoginFailure records a failed attempt for remoteAddr.
func (m *Manager) LoginFailure(remoteAddr string) {
	m.limiter.failure(ClientIP(remoteAddr))
}

// LoginSuccess clears failure state for remoteAddr after a good login.
func (m *Manager) LoginSuccess(remoteAddr string) {
	m.limiter.success(ClientIP(remoteAddr))
}

// ClientIP strips the port from a RemoteAddr, yielding the rate-limit key.
// It reads r.RemoteAddr ONLY: X-Forwarded-For is deliberately not honored
// because clients can forge it, which would let attackers rotate fake IPs
// past the limiter or lock out innocent ones. Behind a reverse proxy all
// requests share the proxy address, so a lockout applies collectively
// there — an accepted homelab tradeoff (see also the login handler).
func ClientIP(remoteAddr string) string {
	host, _, err := net.SplitHostPort(remoteAddr)
	if err != nil {
		return remoteAddr
	}
	return host
}

// isAPIRequest reports whether the path belongs to the JSON API (which
// gets JSON rejections instead of redirects).
func isAPIRequest(path string) bool {
	return strings.HasPrefix(path, "/api/")
}
