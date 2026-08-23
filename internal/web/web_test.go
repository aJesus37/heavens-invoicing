package web_test

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ajesus37/heavens-invoicing/internal/auth"
	"github.com/ajesus37/heavens-invoicing/internal/db"
	"github.com/ajesus37/heavens-invoicing/internal/deliver"
	"github.com/ajesus37/heavens-invoicing/internal/model"
	"github.com/ajesus37/heavens-invoicing/internal/pdf"
	"github.com/ajesus37/heavens-invoicing/internal/repo"
	"github.com/ajesus37/heavens-invoicing/internal/web"
)

// seedInvoice creates a client and an invoice with the given status,
// returning the invoice id for detail/list assertions.
func seedInvoice(t *testing.T, repos *repo.Repos, status string) (string, string) {
	t.Helper()
	ctx := context.Background()
	client := &model.Client{Name: "Acme"}
	if _, err := repos.Clients.Create(ctx, client); err != nil {
		t.Fatal(err)
	}
	inv := &model.Invoice{
		ClientID:  client.ID,
		Status:    status,
		IssueDate: dateUTC(2026, 8, 1),
		DueDate:   dateUTC(2026, 9, 1),
		Items:     []*model.InvoiceItem{{Description: "Serviço", UnitPrice: 1000, Quantity: 1}},
	}
	if err := repos.Invoices.Create(ctx, inv); err != nil {
		t.Fatal(err)
	}
	return client.ID, inv.ID
}

// newTestEnv boots the real stack (sqlite repos + router + web handlers)
// against a throwaway database and returns a live test server along with
// its repos for direct seeding.
func newTestEnv(t *testing.T) (*httptest.Server, *repo.Repos) {
	t.Helper()
	conn, err := db.Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { conn.Close() })
	repos := repo.New(conn)

	router := deliver.NewRouter(repos.Invoices, nil, nil, nil, nil, nil)
	authManager := auth.New(repos.Sessions, repos.Settings)
	handlers, err := web.New(repos, router, nil, pdf.SenderInfo{}, authManager)
	if err != nil {
		t.Fatalf("web.New: %v", err)
	}
	ts := httptest.NewServer(handlers.Mux())
	// Keep redirect responses visible so flows can assert 303 + Location.
	ts.Client().CheckRedirect = func(req *http.Request, via []*http.Request) error {
		return http.ErrUseLastResponse
	}
	return ts, repos
}

func dateUTC(y int, m time.Month, d int) time.Time {
	return time.Date(y, m, d, 12, 0, 0, 0, time.UTC)
}

func get(t *testing.T, ts *httptest.Server, path string) (int, string) {
	t.Helper()
	resp, err := ts.Client().Get(ts.URL + path)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, string(body)
}

func postForm(t *testing.T, ts *httptest.Server, path string, form url.Values) (*http.Response, string) {
	t.Helper()
	resp, err := ts.Client().PostForm(ts.URL+path, form)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	return resp, string(body)
}

func TestDashboardRenders(t *testing.T) {
	ts, _ := newTestEnv(t)
	status, body := get(t, ts, "/")
	if status != http.StatusOK {
		t.Fatalf("got %d want 200", status)
	}
	for _, marker := range []string{"Dashboard", "Faturas pendentes", "Recorrentes nos próximos 7 dias", "/clients"} {
		if !strings.Contains(body, marker) {
			t.Errorf("dashboard missing marker %q", marker)
		}
	}
}

func TestClientCreateFlowEndToEnd(t *testing.T) {
	ts, _ := newTestEnv(t)

	form := url.Values{
		"name":    {"Acme Ltda"},
		"email":   {"contato@acme.com"},
		"phone":   {"+5511999999999"},
		"address": {"Rua das Flores, 123 - São Paulo/SP"},
	}
	resp, body := postForm(t, ts, "/clients/new", form)
	if resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("create: got %d want 303\nbody: %s", resp.StatusCode, body)
	}

	status, body := get(t, ts, resp.Header.Get("Location"))
	if status != http.StatusOK {
		t.Fatalf("detail: got %d want 200", status)
	}
	for _, marker := range []string{"Acme Ltda", "contato@acme.com", "5511999999999", "Rua das Flores"} {
		if !strings.Contains(body, marker) {
			t.Errorf("client detail missing %q", marker)
		}
	}

	status, body = get(t, ts, "/clients")
	if status != http.StatusOK || !strings.Contains(body, "Acme Ltda") {
		t.Fatalf("client list should show created client (status=%d)", status)
	}

	// Blank name must be rejected without creating anything.
	resp2, _ := postForm(t, ts, "/clients/new", url.Values{"name": {" "}})
	if resp2.StatusCode != http.StatusBadRequest {
		t.Fatalf("blank name: got %d want 400", resp2.StatusCode)
	}
}

func TestClientUpdateFlow(t *testing.T) {
	ts, repos := newTestEnv(t)

	form := url.Values{"name": {"Beta"}}
	resp, _ := postForm(t, ts, "/clients/new", form)
	id := strings.TrimPrefix(resp.Header.Get("Location"), "/clients/")
	id = strings.Split(id, "?")[0]

	resp, body := postForm(t, ts, "/clients/"+id, url.Values{
		"name":  {"Beta SA"},
		"notes": {"atualizado"},
	})
	if resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("update: got %d want 303 (body: %s)", resp.StatusCode, body)
	}
	_, body = get(t, ts, "/clients/"+id)
	if !strings.Contains(body, "Beta SA") || !strings.Contains(body, "atualizado") {
		t.Fatal("update did not persist")
	}

	// Updates trim the name just like creation does.
	respTrim, _ := postForm(t, ts, "/clients/"+id, url.Values{"name": {"  Gamma  "}})
	if respTrim.StatusCode != http.StatusSeeOther {
		t.Fatalf("trim update: got %d want 303", respTrim.StatusCode)
	}
	got, err := repos.Clients.Get(context.Background(), id)
	if err != nil {
		t.Fatal(err)
	}
	if got.Name != "Gamma" {
		t.Fatalf("update did not trim name, got %q", got.Name)
	}
}

func TestClientDetailNotFound(t *testing.T) {
	ts, _ := newTestEnv(t)
	status, _ := get(t, ts, "/clients/naoexiste")
	if status != http.StatusNotFound {
		t.Fatalf("got %d want 404", status)
	}
}

func TestClientLanguageSelectFlow(t *testing.T) {
	ts, repos := newTestEnv(t)

	// The new-client form carries the language selector, Português first.
	_, body := get(t, ts, "/clients/new")
	for _, marker := range []string{`<select id="language" name="language">`, `value="pt-BR"`, `value="en"`} {
		if !strings.Contains(body, marker) {
			t.Errorf("new client form missing language select marker %q", marker)
		}
	}
	if !strings.Contains(body, `value="pt-BR" selected`) {
		t.Error("Português should be preselected by default")
	}

	resp, _ := postForm(t, ts, "/clients/new", url.Values{
		"name":     {"Bilíngue"},
		"language": {"en"},
	})
	if resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("create with language: got %d want 303", resp.StatusCode)
	}
	id := strings.TrimPrefix(resp.Header.Get("Location"), "/clients/")
	id = strings.Split(id, "?")[0]
	got, err := repos.Clients.Get(context.Background(), id)
	if err != nil {
		t.Fatal(err)
	}
	if got.Language != "en" {
		t.Fatalf("stored language = %q, want en", got.Language)
	}

	// The edit form preselects the stored language.
	_, body = get(t, ts, "/clients/"+id)
	if !strings.Contains(body, `value="en" selected`) {
		t.Error("edit form should preselect English for an en client")
	}

	// Junk languages are rejected instead of persisted.
	respBad, _ := postForm(t, ts, "/clients/new", url.Values{
		"name":     {"Ruim"},
		"language": {"klingon"},
	})
	if respBad.StatusCode != http.StatusBadRequest {
		t.Fatalf("invalid language: got %d want 400", respBad.StatusCode)
	}
}

func TestInvoiceCancelButtonVisibility(t *testing.T) {
	ts, repos := newTestEnv(t)

	_, draftID := seedInvoice(t, repos, "draft")
	_, sentID := seedInvoice(t, repos, "sent")
	_, paidID := seedInvoice(t, repos, "paid")
	_, cancelledID := seedInvoice(t, repos, "cancelled")

	// Cancellable statuses (draft, sent) expose the cancel action.
	for _, id := range []string{draftID, sentID} {
		status, body := get(t, ts, "/invoices/"+id)
		if status != http.StatusOK {
			t.Fatalf("detail %s: got %d want 200", id, status)
		}
		if !strings.Contains(body, "/invoices/"+id+"/cancel") {
			t.Errorf("invoice %s should show the cancel action", id)
		}
	}

	// Paid and cancelled must hide it.
	for _, id := range []string{paidID, cancelledID} {
		status, body := get(t, ts, "/invoices/"+id)
		if status != http.StatusOK {
			t.Fatalf("detail %s: got %d want 200", id, status)
		}
		if strings.Contains(body, "/invoices/"+id+"/cancel") {
			t.Errorf("invoice %s must not show the cancel action", id)
		}
	}
}

func TestRecurringToggleControlPresent(t *testing.T) {
	ts, repos := newTestEnv(t)
	ctx := context.Background()
	client, err := repos.Clients.Create(ctx, &model.Client{Name: "Acme"})
	if err != nil {
		t.Fatal(err)
	}
	tpl := &model.Invoice{
		ClientID:  client.ID,
		Status:    "draft",
		IssueDate: dateUTC(2026, 8, 1),
		DueDate:   dateUTC(2026, 9, 1),
		Items:     []*model.InvoiceItem{{Description: "Serviço", UnitPrice: 1000, Quantity: 1}},
	}
	if err := repos.Invoices.Create(ctx, tpl); err != nil {
		t.Fatal(err)
	}
	sched := &model.RecurringSchedule{
		ClientID:          client.ID,
		InvoiceTemplateID: tpl.ID,
		Frequency:         "monthly",
		NextSendDate:      dateUTC(2026, 9, 1),
		DeliveryMethod:    "email",
	}
	if err := repos.Recurring.Create(ctx, sched); err != nil {
		t.Fatal(err)
	}

	status, body := get(t, ts, "/recurring")
	if status != http.StatusOK {
		t.Fatalf("recorrentes: got %d want 200", status)
	}
	if !strings.Contains(body, "/recurring/"+sched.ID+"/toggle") {
		t.Error("recurring list should expose the pause/resume control")
	}
	if !strings.Contains(body, "Desativar") {
		t.Error("active schedule should offer a Disable control")
	}
}
