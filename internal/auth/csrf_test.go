package auth_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/jesus/invoice-app/internal/auth"
)

// authedCSRFPair boots a real session and returns its cookies plus the raw
// CSRF token, mirroring what a logged-in browser holds.
func authedCSRFPair(t *testing.T, mgr *auth.Manager) (*http.Cookie, string) {
	t.Helper()
	rec := httptest.NewRecorder()
	if err := mgr.StartSession(context.Background(), rec); err != nil {
		t.Fatal(err)
	}
	resp := rec.Result()
	var sess *http.Cookie
	var csrf string
	for _, c := range resp.Cookies() {
		if c.Name == auth.CookieSession {
			sess = c
		}
		if c.Name == auth.CookieCSRF {
			csrf = c.Value
		}
	}
	if sess == nil || csrf == "" {
		t.Fatal("StartSession must set both session and CSRF cookies")
	}
	return sess, csrf
}

// authedReq builds a request carrying the session cookie and, when csrf is
// non-empty, the double-submit token in both the cookie and header.
func authedReq(t *testing.T, method, path, csrf string, sess *http.Cookie) *http.Request {
	t.Helper()
	req := httptest.NewRequest(method, path, nil)
	req.AddCookie(sess)
	if csrf != "" {
		req.AddCookie(&http.Cookie{Name: auth.CookieCSRF, Value: csrf})
		req.Header.Set("X-CSRF-Token", csrf)
	}
	return req
}

func TestCSRFAuthWebPostRejectedWithoutToken(t *testing.T) {
	h := newGateHarness(t)
	sess, _ := authedCSRFPair(t, h.mgr)

	// No CSRF cookie/header at all → 403.
	req := authedReq(t, "POST", "/clientes/novo", "", sess)
	rec := httptest.NewRecorder()
	h.mgr.Gate(h.next).ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("authenticated POST without CSRF: got %d want 403", rec.Code)
	}
	if h.hits != 0 {
		t.Error("request must not reach handlers without a CSRF token")
	}
}

func TestCSRFAuthWebPostRejectedOnMismatch(t *testing.T) {
	h := newGateHarness(t)
	sess, csrf := authedCSRFPair(t, h.mgr)

	// The signed CSRF cookie is correct, but the header/field value sent
	// with the request differs → 403. We never trust the cookie alone.
	req := httptest.NewRequest("POST", "/clientes/novo", nil)
	req.AddCookie(sess)
	req.AddCookie(&http.Cookie{Name: auth.CookieCSRF, Value: csrf})
	req.Header.Set("X-CSRF-Token", "wrong-token")
	rec := httptest.NewRecorder()
	h.mgr.Gate(h.next).ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("mismatched CSRF: got %d want 403", rec.Code)
	}
}

func TestCSRFAuthWebPostAcceptedWithToken(t *testing.T) {
	h := newGateHarness(t)
	sess, csrf := authedCSRFPair(t, h.mgr)

	req := authedReq(t, "POST", "/clientes/novo", csrf, sess)
	rec := httptest.NewRecorder()
	h.mgr.Gate(h.next).ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || rec.Body.String() != "reached" {
		t.Fatalf("valid CSRF on web POST: got %d %q", rec.Code, rec.Body.String())
	}
}

func TestCSRFWebFormFieldAccepted(t *testing.T) {
	h := newGateHarness(t)
	sess, csrf := authedCSRFPair(t, h.mgr)

	// Plain browser form posts send the token as a field, not a header.
	req := httptest.NewRequest("POST", "/clientes/novo", nil)
	req.AddCookie(sess)
	req.AddCookie(&http.Cookie{Name: auth.CookieCSRF, Value: csrf})
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.PostForm = map[string][]string{"csrf_token": {csrf}}

	rec := httptest.NewRecorder()
	h.mgr.Gate(h.next).ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("csrf_token form field: got %d want 200", rec.Code)
	}
}

func TestCSRSafeMethodsUnaffected(t *testing.T) {
	h := newGateHarness(t)
	sess, _ := authedCSRFPair(t, h.mgr)

	// GET is safe: no CSRF token required, must pass.
	req := authedReq(t, "GET", "/clientes", "", sess)
	rec := httptest.NewRecorder()
	h.mgr.Gate(h.next).ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("authenticated GET without CSRF: got %d want 200", rec.Code)
	}
}

func TestCSRFAPiWriteRejectedAndAccepted(t *testing.T) {
	h := newGateHarness(t)
	sess, csrf := authedCSRFPair(t, h.mgr)

	// Missing token → JSON 403.
	req := authedReq(t, "POST", "/api/clients", "", sess)
	rec := httptest.NewRecorder()
	h.mgr.Gate(h.next).ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("API POST without CSRF: got %d want 403", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); !strings.Contains(ct, "application/json") {
		t.Errorf("API CSRF failure Content-Type = %q, want JSON", ct)
	}
	if !strings.Contains(rec.Body.String(), `"csrf token invalid"`) {
		t.Errorf("API CSRF failure body = %q", rec.Body.String())
	}

	// Matching token → 200.
	reqOK := authedReq(t, "PUT", "/api/clients/1", csrf, sess)
	recOK := httptest.NewRecorder()
	h.mgr.Gate(h.next).ServeHTTP(recOK, reqOK)
	if recOK.Code != http.StatusOK {
		t.Fatalf("API write with CSRF: got %d want 200", recOK.Code)
	}
}

func TestCSRFLoginPostRejectedWithoutToken(t *testing.T) {
	h := newGateHarness(t)

	// The CSRF cookie/header was never issued (no prior GET /login), so a
	// blind POST /login must be refused regardless of credentials.
	req := httptest.NewRequest("POST", "/login", nil)
	rec := httptest.NewRecorder()
	h.mgr.Gate(h.next).ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("login POST without CSRF: got %d want 403", rec.Code)
	}
}

func TestCSRFCookieFlags(t *testing.T) {
	h := newGateHarness(t)
	rec := httptest.NewRecorder()
	if _, err := h.mgr.IssueCSRFToken(rec); err != nil {
		t.Fatal(err)
	}
	c := cookieOf(t, rec.Result(), auth.CookieCSRF)

	// Deliberately NOT HttpOnly: same-origin JS must read it to feed the
	// htmx X-CSRF-Token header. The browser still sends it on every
	// request, so HttpOnly would only break the JS path.
	if c.HttpOnly {
		t.Error("CSRF cookie must NOT be HttpOnly (JS/htmx needs to read it)")
	}
	if c.SameSite != http.SameSiteLaxMode {
		t.Errorf("CSRF cookie SameSite = %v, want Lax", c.SameSite)
	}
	if c.Path != "/" {
		t.Errorf("CSRF cookie Path = %q, want /", c.Path)
	}
	if c.Secure {
		t.Error("CSRF cookie Secure must stay unset (plain-HTTP homelab)")
	}
	if c.MaxAge != int(auth.SessionTTL/time.Second) {
		t.Errorf("CSRF cookie Max-Age = %d, want %d", c.MaxAge, int(auth.SessionTTL/time.Second))
	}
}
