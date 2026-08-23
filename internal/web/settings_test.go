package web_test

import (
	"context"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/jesus/invoice-app/internal/repo"
)

func TestSettingsSaveAndReload(t *testing.T) {
	ts, repos := newTestEnv(t)
	ctx := context.Background()

	form := url.Values{
		"business_name":    {"Meu Negócio LTDA"},
		"business_address": {"Av Central, 500 - Campinas/SP"},
		"default_pix_key":  {"pagos@meunegocio.com"},
		"smtp_host":        {"smtp.meunegocio.com"},
		"smtp_port":        {"587"},
		"smtp_user":        {"faturamento@meunegocio.com"},
		"smtp_pass":        {"segredo123"},
		"smtp_from":        {"faturamento@meunegocio.com"},
	}
	resp, body := postForm(t, ts, "/settings", form)
	if resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("save: got %d want 303\n%s", resp.StatusCode, body)
	}

	for key, want := range map[string]string{
		repo.SettingBusinessName:    "Meu Negócio LTDA",
		repo.SettingBusinessAddress: "Av Central, 500 - Campinas/SP",
		repo.SettingDefaultPIXKey:   "pagos@meunegocio.com",
		repo.SettingSMTPHost:        "smtp.meunegocio.com",
		repo.SettingSMTPPass:        "segredo123",
	} {
		got, err := repos.Settings.Get(ctx, key)
		if err != nil || got != want {
			t.Errorf("setting %s = %q (%v), want %q", key, got, err, want)
		}
	}

	// The saved banner shows and stored secrets are never re-rendered.
	status, pageBody := get(t, ts, "/settings?saved=1")
	if status != http.StatusOK || !strings.Contains(pageBody, "Configurações salvas.") {
		t.Fatalf("settings page missing saved banner (status=%d)", status)
	}
	if strings.Contains(pageBody, "segredo123") {
		t.Error("stored SMTP password leaked into the form")
	}
	if !strings.Contains(pageBody, `value="Meu Negócio LTDA"`) {
		t.Error("business name not prefilled")
	}

	// Blank secret keeps the stored value; other fields still update.
	resp2, _ := postForm(t, ts, "/settings", url.Values{
		"business_name": {"Outro Nome"},
		"smtp_host":     {"novo.smtp.com"},
		"smtp_pass":     {""},
	})
	if resp2.StatusCode != http.StatusSeeOther {
		t.Fatalf("second save: got %d want 303", resp2.StatusCode)
	}
	if pass, err := repos.Settings.Get(ctx, repo.SettingSMTPPass); err != nil || pass != "segredo123" {
		t.Errorf("blank submit cleared the stored password (got %q, %v)", pass, err)
	}
}

func TestWhatsAppStatusFragmentUnavailable(t *testing.T) {
	ts, _ := newTestEnv(t)

	status, body := get(t, ts, "/settings")
	if status != http.StatusOK {
		t.Fatalf("settings page: got %d want 200", status)
	}
	for _, marker := range []string{"Configurações", "wa-status", "/settings/whatsapp/status"} {
		if !strings.Contains(body, marker) {
			t.Errorf("settings page missing %q", marker)
		}
	}

	fragStatus, fragBody := get(t, ts, "/settings/whatsapp/status")
	if fragStatus != http.StatusOK || !strings.Contains(fragBody, "indisponível") {
		t.Fatalf("fragment without session should say indisponível (status=%d body=%q)", fragStatus, fragBody)
	}

	// QR endpoint without an active pairing is a clean 404.
	qrStatus, _ := get(t, ts, "/settings/whatsapp/qr.png")
	if qrStatus != http.StatusNotFound {
		t.Fatalf("qr without pairing: got %d want 404", qrStatus)
	}
}
