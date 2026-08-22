package web_test

import (
	"net/http"
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
	settings := getReq(t, ts, "/configuracoes", sess)
	if settings.StatusCode != http.StatusOK {
		t.Fatalf("GET /configuracoes: got %d want 200", settings.StatusCode)
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
	req, err := http.NewRequest(http.MethodPost, ts.URL+"/configuracoes/whatsapp/conectar", nil)
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
		t.Fatalf("POST /configuracoes/whatsapp/conectar returned 403: htmx CSRF header still not accepted by the gate")
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("POST /configuracoes/whatsapp/conectar: got %d want 200 (fragment)", resp.StatusCode)
	}
}
