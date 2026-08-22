package auth_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/jesus/invoice-app/internal/auth"
)

// gateHarness builds a Manager behind a recording next handler so tests
// can assert whether the request reached the wrapped stack.
type gateHarness struct {
	mgr  *auth.Manager
	next http.Handler
	hits int
	last *http.Request
}

func newGateHarness(t *testing.T) *gateHarness {
	t.Helper()
	mgr, _, _ := newManager(t)
	h := &gateHarness{mgr: mgr}
	h.next = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h.hits++
		h.last = r
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("reached"))
	})
	return h
}

// withCSRF issues a CSRF token from the manager, plants it as the
// double-submit cookie on a matching request, and echoes it in the
// X-CSRF-Token header — exactly what a legitimately rendered page does.
func withCSRF(t *testing.T, mgr *auth.Manager, req *http.Request) {
	t.Helper()
	rec := httptest.NewRecorder()
	tok, err := mgr.IssueCSRFToken(rec)
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range rec.Result().Cookies() {
		req.AddCookie(c)
	}
	req.Header.Set("X-CSRF-Token", tok)
}

func TestGateExemptPaths(t *testing.T) {
	cases := []struct {
		method, path string
		csrf         bool
	}{
		{"GET", "/healthz", false},
		{"GET", "/static/app.css", false},
		{"GET", "/login", false},
		// POST /login is session-exempt but state-changing, so it needs a
		// valid CSRF token (planted by the GET /login handler).
		{"POST", "/login", true},
	}
	for _, tc := range cases {
		h := newGateHarness(t)
		req := httptest.NewRequest(tc.method, tc.path, nil)
		if tc.csrf {
			withCSRF(t, h.mgr, req)
		}
		rec := httptest.NewRecorder()
		h.mgr.Gate(h.next).ServeHTTP(rec, req)
		if rec.Code != http.StatusOK || rec.Body.String() != "reached" {
			t.Errorf("%s %s must pass through unauthenticated, got %d %q",
				tc.method, tc.path, rec.Code, rec.Body.String())
		}
	}
}

func TestGateUnauthenticatedWebRedirects(t *testing.T) {
	for _, path := range []string{"/", "/clientes", "/configuracoes", "/anything"} {
		h := newGateHarness(t)
		req := httptest.NewRequest("GET", path, nil)
		rec := httptest.NewRecorder()
		h.mgr.Gate(h.next).ServeHTTP(rec, req)

		if rec.Code != http.StatusSeeOther {
			t.Errorf("GET %s: got %d want 303", path, rec.Code)
		}
		if loc := rec.Header().Get("Location"); loc != "/login" {
			t.Errorf("GET %s: Location = %q want /login", path, loc)
		}
		if h.last != nil {
			t.Errorf("GET %s: request must not reach handlers", path)
		}
	}
}

func TestGateUnauthenticatedAPIGets401JSON(t *testing.T) {
	for _, tc := range []struct{ method, path string }{
		{"GET", "/api/clients"},
		{"POST", "/api/clients"},
		{"PUT", "/api/products/1"},
		{"DELETE", "/api/invoices/1"},
	} {
		h := newGateHarness(t)
		req := httptest.NewRequest(tc.method, tc.path, nil)
		rec := httptest.NewRecorder()
		h.mgr.Gate(h.next).ServeHTTP(rec, req)

		if rec.Code != http.StatusUnauthorized {
			t.Errorf("%s %s: got %d want 401", tc.method, tc.path, rec.Code)
		}
		if ct := rec.Header().Get("Content-Type"); !strings.Contains(ct, "application/json") {
			t.Errorf("%s %s: Content-Type = %q, want JSON", tc.method, tc.path, ct)
		}
		if body := rec.Body.String(); !strings.Contains(body, `"unauthorized"`) {
			t.Errorf("%s %s: body = %q, want unauthorized error JSON", tc.method, tc.path, body)
		}
	}
}

func TestGateAuthenticatedRequestsPass(t *testing.T) {
	h := newGateHarness(t)

	rec := httptest.NewRecorder()
	if err := h.mgr.StartSession(context.Background(), rec); err != nil {
		t.Fatal(err)
	}
	raw := cookieOf(t, rec.Result(), auth.CookieSession).Value

	req := httptest.NewRequest("GET", "/clientes", nil)
	req.AddCookie(&http.Cookie{Name: auth.CookieSession, Value: raw})
	out := httptest.NewRecorder()
	h.mgr.Gate(h.next).ServeHTTP(out, req)

	if out.Code != http.StatusOK || out.Body.String() != "reached" {
		t.Fatalf("authenticated GET must reach handlers, got %d %q", out.Code, out.Body.String())
	}
}

func TestGateInvalidSessionCookieRedirectsAndClearsIt(t *testing.T) {
	h := newGateHarness(t)

	req := httptest.NewRequest("GET", "/", nil)
	req.AddCookie(&http.Cookie{Name: auth.CookieSession, Value: "dead-token"})
	out := httptest.NewRecorder()
	h.mgr.Gate(h.next).ServeHTTP(out, req)

	if out.Code != http.StatusSeeOther {
		t.Fatalf("stale cookie: got %d want 303", out.Code)
	}
	var cleared bool
	for _, c := range out.Result().Cookies() {
		if c.Name == auth.CookieSession && c.MaxAge < 0 {
			cleared = true
		}
	}
	if !cleared {
		t.Error("a stale session cookie should be expired so browsers stop replaying it")
	}
}

func TestGateLogoutRequiresSession(t *testing.T) {
	h := newGateHarness(t)

	// Unauthenticated POST /logout is not exempt: it redirects to login.
	req := httptest.NewRequest("POST", "/logout", nil)
	out := httptest.NewRecorder()
	h.mgr.Gate(h.next).ServeHTTP(out, req)
	if out.Code != http.StatusSeeOther {
		t.Fatalf("unauthenticated logout: got %d want 303", out.Code)
	}

	// Authenticated logout passes through to the handler.
	rec := httptest.NewRecorder()
	if err := h.mgr.StartSession(context.Background(), rec); err != nil {
		t.Fatal(err)
	}
	raw := cookieOf(t, rec.Result(), auth.CookieSession).Value
	authReq := httptest.NewRequest("POST", "/logout", nil)
	authReq.AddCookie(&http.Cookie{Name: auth.CookieSession, Value: raw})
	withCSRF(t, h.mgr, authReq)
	authOut := httptest.NewRecorder()
	h.mgr.Gate(h.next).ServeHTTP(authOut, authReq)
	if authOut.Code != http.StatusOK {
		t.Fatalf("authenticated logout must reach handlers, got %d", authOut.Code)
	}
}
