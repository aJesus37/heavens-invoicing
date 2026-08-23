package web_test

import (
	"context"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/ajesus37/heavens-invoicing/internal/model"
	"github.com/ajesus37/heavens-invoicing/internal/repo"
)

// setLocale stores the UI language preference the way the settings page
// does.
func setLocale(t *testing.T, repos *repo.Repos, lang string) {
	t.Helper()
	if err := repos.Settings.Set(context.Background(), repo.SettingLocale, lang); err != nil {
		t.Fatal(err)
	}
}

// TestPagesRenderUnderBothLocales walks the dashboard plus one page per
// resource under pt-BR (default) and en, asserting translated markers.
func TestPagesRenderUnderBothLocales(t *testing.T) {
	ts, repos := newTestEnv(t)

	want := map[string]map[string][]string{
		// lang → path → markers that must appear on that page
		"pt-BR": {
			"/":              {"Dashboard", "Faturas pendentes", "Recorrentes nos próximos 7 dias"},
			"/clients":      {"Clientes", "Novo cliente", "Nenhum cliente cadastrado."},
			"/products":      {"Produtos", "Novo produto", "Preço unitário", "Nenhum produto cadastrado."},
			"/invoices":       {"Faturas", "Todas", "Rascunho"},
			"/recurring":   {"Recorrentes", "Nova recorrência", "Próximo envio"},
			"/settings": {"Configurações", "Negócio", "Idioma", "Português"},
		},
		"en": {
			"/":              {"Dashboard", "Pending invoices", "Recurring in the next 7 days"},
			"/clients":      {"Clients", "New client", "No clients registered."},
			"/products":      {"Products", "New product", "Unit price", "No products registered."},
			"/invoices":       {"Invoices", "All", "Draft"},
			"/recurring":   {"Recurring", "New recurring", "Next send"},
			"/settings": {"Settings", "Business", "Language", "English"},
		},
	}
	// Labels unique to the OTHER locale that must never appear.
	oppositeNav := map[string]string{"pt-BR": ">Settings<", "en": ">Configurações<"}

	for _, lang := range []string{"pt-BR", "en"} {
		setLocale(t, repos, lang)
		for path, markers := range want[lang] {
			status, body := get(t, ts, path)
			if status != http.StatusOK {
				t.Fatalf("[%s] %s: got %d want 200", lang, path, status)
			}
			for _, m := range markers {
				if !strings.Contains(body, m) {
					t.Errorf("[%s] %s missing marker %q", lang, path, m)
				}
			}
			if strings.Contains(body, oppositeNav[lang]) {
				t.Errorf("[%s] %s still shows the other locale's nav label %q", lang, path, oppositeNav[lang])
			}
		}
	}
}

func TestUnknownStoredLocaleFallsBackToEnglish(t *testing.T) {
	ts, repos := newTestEnv(t)
	setLocale(t, repos, "klingon")

	status, body := get(t, ts, "/")
	if status != http.StatusOK {
		t.Fatalf("got %d want 200", status)
	}
	if !strings.Contains(body, "Pending invoices") {
		t.Error("unknown locale should fall back to en")
	}
	if strings.Contains(body, "Faturas pendentes") {
		t.Error("unknown locale must not render pt-BR strings")
	}
}

func TestLocaleSelectorPersistsAndAppliesImmediately(t *testing.T) {
	ts, _ := newTestEnv(t)

	// Default is pt-BR with Português selected in the selector.
	_, body := get(t, ts, "/settings")
	if !strings.Contains(body, `value="pt-BR" selected`) || !strings.Contains(body, "Português") {
		t.Fatal("default selector should show Português selected")
	}

	// Saving English applies on the very next request.
	resp, _ := postForm(t, ts, "/settings", url.Values{"locale": {"en"}})
	if resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("save locale: got %d want 303", resp.StatusCode)
	}
	_, body = get(t, ts, "/")
	for _, marker := range []string{">Clients<", ">Invoices<", ">Settings<"} {
		if !strings.Contains(body, marker) {
			t.Errorf("nav not translated to en after save (missing %q)", marker)
		}
	}
	_, body = get(t, ts, "/settings")
	if !strings.Contains(body, `value="en" selected`) || !strings.Contains(body, "English") {
		t.Error("selector should now show English selected")
	}

	// A junk locale value is ignored instead of breaking every page.
	resp, _ = postForm(t, ts, "/settings", url.Values{"locale": {"xx-XX"}})
	if resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("junk locale save: got %d want 303", resp.StatusCode)
	}
	_, body = get(t, ts, "/")
	if !strings.Contains(body, "Pending invoices") {
		t.Error("junk locale should keep the previous en UI")
	}
}

func TestSendFragmentTranslatesRouterErrors(t *testing.T) {
	ts, repos := newTestEnv(t)

	c, err := repos.Clients.Create(context.Background(), &model.Client{
		Name:  "Alvo",
		Email: strPtr("alvo@x.com"),
	})
	if err != nil {
		t.Fatal(err)
	}
	inv := &model.Invoice{
		ClientID:  c.ID,
		Status:    "draft",
		IssueDate: dateUTC(2026, 8, 1),
		DueDate:   dateUTC(2026, 8, 20),
		Items:     []*model.InvoiceItem{{Description: "Serviço", Quantity: 1, UnitPrice: 1000}},
	}
	if err := repos.Invoices.Create(context.Background(), inv); err != nil {
		t.Fatal(err)
	}

	// Default pt-BR: unconfigured channel reports the PT text.
	respPT, bodyPT := postForm(t, ts, "/invoices/"+inv.ID+"/send", url.Values{"method": {"email"}})
	if respPT.StatusCode != http.StatusOK || !strings.Contains(bodyPT, "não configurado") {
		t.Fatalf("pt send fragment: got %d want 200 with 'não configurado'\n%s", respPT.StatusCode, bodyPT)
	}

	setLocale(t, repos, "en")
	respEN, bodyEN := postForm(t, ts, "/invoices/"+inv.ID+"/send", url.Values{"method": {"email"}})
	if respEN.StatusCode != http.StatusOK || !strings.Contains(bodyEN, "not configured") {
		t.Fatalf("en send fragment: got %d want 200 with 'not configured'\n%s", respEN.StatusCode, bodyEN)
	}
}
