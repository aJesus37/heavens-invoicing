package auth

import (
	"log"
	"net/http"
	"strings"
)

// Exempt paths: the health probe, static assets, and the login page
// (reachable pre-authentication by definition). Everything else — every
// web page, every htmx fragment endpoint, every /api/ route — requires a
// valid session. /logout deliberately is NOT exempt: it acts on session
// state and so demands an authenticated, CSRF-valid request.
const (
	healthzPath  = "/healthz"
	staticPrefix = "/static/"
	loginPath    = "/login"
	faviconPath  = "/favicon.ico"
)

// stateChanging reports whether method mutates server state and therefore
// must clear the CSRF gate. Safe methods (GET, HEAD, OPTIONS) are exempt
// so navigation and probes stay frictionless.
func stateChanging(method string) bool {
	switch method {
	case http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete:
		return true
	default:
		return false
	}
}

// Gate wraps next with session + CSRF enforcement. It is applied once, in
// internal/server, around the assembled mux — the single choke point in
// front of both the web UI and the JSON API.
func (m *Manager) Gate(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := r.URL.Path

		// Public by design: liveness probes, stylesheet assets and the
		// favicon probe (dead-ended by the web mux; letting it fall
		// through would bounce anonymous visitors into /login).
		if path == healthzPath || path == faviconPath || strings.HasPrefix(path, staticPrefix) {
			next.ServeHTTP(w, r)
			return
		}

		// /login is reachable without a session, but its POST creates a
		// session / sets the password, so it still needs CSRF. The token
		// was planted by the GET /login handler; without it the POST is a
		// forged attempt and must be refused before any password logic.
		if path == loginPath {
			if r.Method == http.MethodPost && !m.checkCSRF(r) {
				m.rejectForbidden(r.Context(), w, path)
				return
			}
			next.ServeHTTP(w, r)
			return
		}

		valid, err := m.ValidSession(r)
		if err != nil {
			// Fail closed: an unreadable session store must never wave
			// requests through.
			log.Printf("auth: session lookup failed: %v", err)
			m.rejectInternal(r.Context(), w, path)
			return
		}
		if !valid {
			if isAPIRequest(path) {
				writeJSONError(w, http.StatusUnauthorized, "unauthorized")
				return
			}
			// Expire any stale cookie so browsers stop replaying a dead
			// token on every navigation.
			http.SetCookie(w, &http.Cookie{Name: CookieSession, Value: "", Path: "/",
				MaxAge: -1, HttpOnly: true, SameSite: http.SameSiteLaxMode})
			http.Redirect(w, r, loginPath, http.StatusSeeOther)
			return
		}

		// Authenticated: every state-changing request must present a valid
		// CSRF token (double-submit cookie). Safe methods pass freely.
		if stateChanging(r.Method) && !m.checkCSRF(r) {
			m.rejectForbidden(r.Context(), w, path)
			return
		}

		next.ServeHTTP(w, r)
	})
}
