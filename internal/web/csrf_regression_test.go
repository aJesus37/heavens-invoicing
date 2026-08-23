package web_test

import (
	"net/http"
	"net/url"
	"strings"
	"testing"
)

// TestWhatsAppConnectCSRFNotBlocked is a regression test for the bug where the
// htmx CSRF header was never attached because the config-request listener was
// registered on document.body (null in <head>) and threw. That made every
// htmx POST — including the WhatsApp pairing button — return 403 and do
// nothing. The fix registers on document, so the header reaches the gate.
func TestWhatsAppConnectCSRFNotBlocked(t *testing.T) {
	ts, _, _ := newAuthEnv(t)
	sess := loginOnce(t, ts)

	// The CSRF cookie is issued on the login GET; reuse it for the header.
	lr := getReq(t, ts, "/login")
	csrf := cookieNamed(lr, "csrf_token")
	lr.Body.Close()
	if csrf == nil {
		t.Fatal("no CSRF cookie from /login")
	}

	// The settings page must now wire htmx CSRF on `document`, not the
	// (null at script time) body, and must emit the csrf-token meta.
	settings := getReq(t, ts, "/settings", sess)
	if settings.StatusCode != http.StatusOK {
		t.Fatalf("GET /settings: got %d want 200", settings.StatusCode)
	}
	body := readBody(settings)
	if strings.Contains(body, "document.body.addEventListener('htmx:config-request'") {
		t.Error("layout still binds htmx CSRF on document.body (throws in <head>)")
	}
	if !strings.Contains(body, "document.addEventListener('htmx:config-request'") {
		t.Error("layout must bind htmx CSRF on document")
	}
	if !strings.Contains(body, `name="csrf-token"`) {
		t.Error("settings page must emit the csrf-token meta for htmx")
	}

	// Replicate exactly what htmx sends for the pairing button: the
	// X-CSRF-Token header carrying the double-submit cookie value.
	req, err := http.NewRequest(http.MethodPost, ts.URL+"/settings/whatsapp/connect", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.AddCookie(sess)
	req.AddCookie(csrf)
	req.Header.Set("HX-Request", "true")
	req.Header.Set("X-CSRF-Token", csrf.Value)
	resp, err := ts.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusForbidden {
		t.Fatalf("POST /settings/whatsapp/connect returned 403: htmx CSRF header still not accepted by the gate")
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("POST /settings/whatsapp/connect: got %d want 200 (fragment)", resp.StatusCode)
	}
}

// TestLoginDoesNotRotateCSRFCookie is a regression test for the bug where
// every GET /login minted a fresh CSRF token. Any incidental late hit on
// /login (favicon redirect chains, prefetch, a second tab) then rotated the
// cookie out from under an already-rendered form, whose hidden field still
// held the old token — so submitting always failed with "security check
// failed". The form and cookie must stay consistent across re-loads.
func TestLoginDoesNotRotateCSRFCookie(t *testing.T) {
	ts, _, _ := newAuthEnv(t)

	first := getReq(t, ts, "/login")
	c1 := cookieNamed(first, "csrf_token")
	body := readBody(first)
	if c1 == nil {
		t.Fatal("first GET /login must plant a CSRF cookie")
	}
	if !strings.Contains(body, `value="`+c1.Value+`"`) {
		t.Fatal("first load: hidden field must match the planted cookie")
	}

	// Second load carrying the existing cookie: no rotation allowed.
	second := getReq(t, ts, "/login", c1)
	defer second.Body.Close()
	for _, ck := range second.Cookies() {
		if ck.Name == "csrf_token" && ck.Value != c1.Value {
			t.Fatalf("second GET /login rotated the CSRF cookie: %s -> %s", c1.Value[:8], ck.Value[:8])
		}
	}
	secondBody := readBody(second)
	if !strings.Contains(secondBody, `value="`+c1.Value+`"`) {
		t.Fatal("second load: hidden field must reuse the existing cookie value")
	}

	// The setup POST from that second load must be accepted.
	set := doForm(t, ts, http.MethodPost, "/login", url.Values{
		"password":   {"supersecret1"},
		"confirm":    {"supersecret1"},
		"csrf_token": {c1.Value},
	}, c1)
	defer set.Body.Close()
	if set.StatusCode != http.StatusSeeOther {
		t.Fatalf("setup POST after re-load: got %d want 303 (cookie/field mismatch?)", set.StatusCode)
	}
}

// TestFaviconDoesNotChaseIntoLogin pins the favicon: it must serve the
// logo SVG (not fall through to dashboard → 303 → /login) and not chase.
func TestFaviconDoesNotChaseIntoLogin(t *testing.T) {
	ts, _, _ := newAuthEnv(t)
	resp := getReq(t, ts, "/favicon.ico")
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET /favicon.ico: got %d want 200", resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); ct != "image/svg+xml" {
		t.Fatalf("favicon Content-Type: got %q want image/svg+xml", ct)
	}
	if loc := resp.Header.Get("Location"); loc != "" {
		t.Fatalf("favicon redirected to %q — it must not chase into /login", loc)
	}
	body := readBody(resp)
	if !strings.Contains(body, "<svg") {
		t.Fatalf("favicon body must contain SVG, got %q", body[:min(200, len(body))])
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
