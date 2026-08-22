package auth_test

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jesus/invoice-app/internal/auth"
	"github.com/jesus/invoice-app/internal/db"
	"github.com/jesus/invoice-app/internal/repo"
)

// newManager boots a real Manager over a throwaway database and returns it
// with the raw connection for storage-level assertions.
func newManager(t *testing.T) (*auth.Manager, *sql.DB, context.Context) {
	t.Helper()
	return newManagerAt(t, time.Now)
}

func newManagerAt(t *testing.T, now func() time.Time) (*auth.Manager, *sql.DB, context.Context) {
	t.Helper()
	conn, err := db.Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { conn.Close() })
	repos := repo.New(conn)
	mgr := auth.New(repos.Sessions, repos.Settings)
	mgr.Now = now
	return mgr, conn, context.Background()
}

func cookieOf(t *testing.T, resp *http.Response, name string) *http.Cookie {
	t.Helper()
	for _, c := range resp.Cookies() {
		if c.Name == name {
			return c
		}
	}
	t.Fatalf("no %q cookie in response", name)
	return nil
}

func TestNewTokenShapeAndUniqueness(t *testing.T) {
	seen := make(map[string]bool)
	for i := 0; i < 200; i++ {
		raw, hash, err := auth.NewToken()
		if err != nil {
			t.Fatal(err)
		}
		if seen[raw] {
			t.Fatalf("token repeated at iteration %d", i)
		}
		seen[raw] = true

		// 32 raw bytes → 43 unpadded base64url characters.
		if len(raw) != 43 {
			t.Fatalf("raw token length = %d, want 43", len(raw))
		}
		decoded, err := base64.RawURLEncoding.DecodeString(raw)
		if err != nil || len(decoded) != 32 {
			t.Fatalf("token does not decode to 32 bytes: %v (%d)", err, len(decoded))
		}
		// The persisted derivation is SHA-256 of the exact string the
		// cookie carries, so validation needs no decoding step.
		sum := sha256.Sum256([]byte(raw))
		if hash != hex.EncodeToString(sum[:]) {
			t.Fatalf("hash mismatch: got %s want %s", hash, hex.EncodeToString(sum[:]))
		}
	}
}

func TestHashTokenIsSHA256(t *testing.T) {
	raw := "abc123"
	sum := sha256.Sum256([]byte(raw))
	if got := auth.HashToken(raw); got != hex.EncodeToString(sum[:]) {
		t.Fatal("HashToken must be the hex SHA-256 of the input")
	}
}

func TestPasswordLifecycle(t *testing.T) {
	mgr, _, ctx := newManager(t)

	setup, err := mgr.NeedsSetup(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if !setup {
		t.Fatal("fresh install must need setup")
	}

	if err := mgr.SetPassword(ctx, "hunter2hunter2"); err != nil {
		t.Fatal(err)
	}

	setup, err = mgr.NeedsSetup(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if setup {
		t.Fatal("after SetPassword the setup phase must be over")
	}

	ok, err := mgr.VerifyPassword(ctx, "hunter2hunter2")
	if err != nil || !ok {
		t.Fatalf("correct password must verify (ok=%v err=%v)", ok, err)
	}
	ok, err = mgr.VerifyPassword(ctx, "wrong")
	if err != nil || ok {
		t.Fatalf("wrong password must fail (ok=%v err=%v)", ok, err)
	}
}

func TestSetPasswordRejectsShortPasswords(t *testing.T) {
	mgr, _, ctx := newManager(t)
	if err := mgr.SetPassword(ctx, "short"); !errors.Is(err, auth.ErrWeakPassword) {
		t.Fatalf("want ErrWeakPassword, got %v", err)
	}
	setup, err := mgr.NeedsSetup(ctx)
	if err != nil || !setup {
		t.Fatalf("rejected password must not be stored (setup=%v err=%v)", setup, err)
	}
}

func TestStartSessionCookieFlagsAndHashOnlyStorage(t *testing.T) {
	mgr, conn, ctx := newManager(t)

	rec := httptest.NewRecorder()
	if err := mgr.StartSession(ctx, rec); err != nil {
		t.Fatal(err)
	}
	raw := cookieOf(t, rec.Result(), auth.CookieSession).Value

	wantMaxAge := int(auth.SessionTTL / time.Second)
	c := cookieOf(t, rec.Result(), auth.CookieSession)
	if len(c.Value) != 43 {
		t.Errorf("cookie value length = %d, want 43-char base64url token", len(c.Value))
	}
	if !c.HttpOnly {
		t.Error("session cookie must be HttpOnly")
	}
	if c.SameSite != http.SameSiteLaxMode {
		t.Errorf("session cookie SameSite = %v, want Lax", c.SameSite)
	}
	if c.Path != "/" {
		t.Errorf("session cookie Path = %q, want /", c.Path)
	}
	if c.MaxAge != wantMaxAge {
		t.Errorf("session cookie Max-Age = %d, want %d", c.MaxAge, wantMaxAge)
	}
	if c.Secure {
		t.Error("Secure must stay unset: this build targets plain-HTTP homelab LANs (documented in session.go)")
	}

	// Storage holds exactly the SHA-256 of the raw token — never the raw
	// value itself, so the cookie cannot be reconstructed from the DB.
	var stored string
	if err := conn.QueryRow(`SELECT token_hash FROM sessions`).Scan(&stored); err != nil {
		t.Fatal(err)
	}
	if stored != auth.HashToken(raw) {
		t.Fatalf("stored hash mismatch: got %s", stored)
	}
	if strings.Contains(stored, raw[:8]) || stored == raw {
		t.Fatal("raw token material leaked into storage")
	}
	var n int
	if err := conn.QueryRow(`SELECT COUNT(*) FROM sessions WHERE token_hash = ?`, raw).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Fatal("raw token found in the sessions table")
	}
}

func TestSessionValidationLifecycle(t *testing.T) {
	mgr, _, ctx := newManager(t)

	rec := httptest.NewRecorder()
	if err := mgr.StartSession(ctx, rec); err != nil {
		t.Fatal(err)
	}
	raw := cookieOf(t, rec.Result(), auth.CookieSession).Value

	validReq := httptest.NewRequest("GET", "/", nil)
	validReq.AddCookie(&http.Cookie{Name: auth.CookieSession, Value: raw})

	ok, err := mgr.ValidSession(validReq)
	if err != nil || !ok {
		t.Fatalf("fresh session must validate (ok=%v err=%v)", ok, err)
	}

	// Garbage / forged tokens fail without error noise.
	badReq := httptest.NewRequest("GET", "/", nil)
	badReq.AddCookie(&http.Cookie{Name: auth.CookieSession, Value: "forged"})
	if ok, _ := mgr.ValidSession(badReq); ok {
		t.Fatal("forged cookie must not validate")
	}
	noCookieReq := httptest.NewRequest("GET", "/", nil)
	if ok, _ := mgr.ValidSession(noCookieReq); ok {
		t.Fatal("missing cookie must not validate")
	}

	// Logout invalidates server-side: replaying the old cookie fails.
	endRec := httptest.NewRecorder()
	mgr.EndSession(endRec, validReq)
	if ok, _ := mgr.ValidSession(validReq); ok {
		t.Fatal("session must be dead after EndSession")
	}
	// And the logout response expires both cookies client-side too.
	var expired []string
	for _, c := range endRec.Result().Cookies() {
		if c.MaxAge < 0 {
			expired = append(expired, c.Name)
		}
	}
	if len(expired) == 0 {
		t.Fatal("EndSession must expire cookies (Max-Age < 0)")
	}
}
