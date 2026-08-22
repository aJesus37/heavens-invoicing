package api_test

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/jesus/invoice-app/internal/model"
	"github.com/jesus/invoice-app/internal/repo"
)

// Wire-format shapes used by assertions: dates are YYYY-MM-DD strings on
// the API surface, so tests decode into these instead of model.Invoice.
type invoiceItemShape struct {
	ID          string  `json:"id"`
	ProductID   *string `json:"product_id,omitempty"`
	Description string  `json:"description"`
	UnitPrice   int64   `json:"unit_price"`
	Quantity    int64   `json:"quantity"`
	Total       int64   `json:"total"`
}

type invoiceShape struct {
	ID        string             `json:"id"`
	ClientID  string             `json:"client_id"`
	Number    int64              `json:"number"`
	Status    string             `json:"status"`
	IssueDate string             `json:"issue_date"`
	DueDate   string             `json:"due_date"`
	Subtotal  int64              `json:"subtotal"`
	Total     int64              `json:"total"`
	Notes     string             `json:"notes"`
	Items     []invoiceItemShape `json:"items"`
}

func mustCreateClient(t *testing.T, repos *repo.Repos, name string) *model.Client {
	t.Helper()
	c, err := repos.Clients.Create(context.Background(), &model.Client{Name: name})
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func setClientEmail(t *testing.T, repos *repo.Repos, c *model.Client, email string) {
	t.Helper()
	c.Email = &email
	if err := repos.Clients.Update(context.Background(), c); err != nil {
		t.Fatal(err)
	}
}

func mustCreateInvoiceViaAPI(t *testing.T, h http.Handler, clientID string) invoiceShape {
	t.Helper()
	rec := do(t, h, "POST", "/api/invoices", map[string]any{
		"client_id":  clientID,
		"issue_date": "2026-08-01",
		"due_date":   "2026-09-01",
		"items":      []map[string]any{{"description": "Serviço", "unit_price": 10000, "quantity": 2}},
	})
	assertStatus(t, rec, 201, "seed invoice via api")
	return decode[invoiceShape](t, rec)
}

func TestInvoicesCreateAndGet(t *testing.T) {
	env := newTestEnv(t)
	h := env.handler

	client := mustCreateClient(t, env.repos, "Acme")

	rec := do(t, h, "POST", "/api/invoices", map[string]any{
		"client_id":  client.ID,
		"issue_date": "2026-08-01",
		"due_date":   "2026-09-01",
		"notes":      "Obrigado",
		"items": []map[string]any{
			{"description": "Serviço", "unit_price": 10000, "quantity": 2},
		},
	})
	assertStatus(t, rec, 201, "create invoice")
	created := decode[invoiceShape](t, rec)
	if created.Number != 1 || created.Status != "draft" || created.Total != 20000 ||
		created.IssueDate != "2026-08-01" || created.DueDate != "2026-09-01" || len(created.Items) != 1 {
		t.Fatalf("created = %+v", created)
	}

	rec = do(t, h, "GET", "/api/invoices/"+created.ID, nil)
	assertStatus(t, rec, 200, "get invoice")

	rec = do(t, h, "GET", "/api/invoices/missing", nil)
	assertStatus(t, rec, 404, "get missing invoice")
}

func TestInvoicesCreateValidation(t *testing.T) {
	env := newTestEnv(t)
	h := env.handler
	client := mustCreateClient(t, env.repos, "Acme")

	tests := []struct {
		name string
		body any
	}{
		{"missing client_id", map[string]any{"issue_date": "2026-08-01", "due_date": "2026-09-01", "items": []any{map[string]any{"description": "x", "quantity": 1}}}},
		{"unknown client", map[string]any{"client_id": "ghost", "issue_date": "2026-08-01", "due_date": "2026-09-01", "items": []any{map[string]any{"description": "x", "quantity": 1}}}},
		{"no items", map[string]any{"client_id": client.ID, "issue_date": "2026-08-01", "due_date": "2026-09-01"}},
		{"bad issue date", map[string]any{"client_id": client.ID, "issue_date": "01/08/2026", "due_date": "2026-09-01", "items": []any{map[string]any{"description": "x", "quantity": 1}}}},
		{"item without description", map[string]any{"client_id": client.ID, "issue_date": "2026-08-01", "due_date": "2026-09-01", "items": []any{map[string]any{"quantity": 1}}}},
		{"item zero quantity", map[string]any{"client_id": client.ID, "issue_date": "2026-08-01", "due_date": "2026-09-01", "items": []any{map[string]any{"description": "x", "quantity": 0}}}},
		{"item negative unit price", map[string]any{"client_id": client.ID, "issue_date": "2026-08-01", "due_date": "2026-09-01", "items": []any{map[string]any{"description": "x", "quantity": 1, "unit_price": -100}}}},
		{"due date before issue date", map[string]any{"client_id": client.ID, "issue_date": "2026-09-10", "due_date": "2026-09-01", "items": []any{map[string]any{"description": "x", "quantity": 1}}}},
		{"invalid status", map[string]any{"client_id": client.ID, "status": "weird", "issue_date": "2026-08-01", "due_date": "2026-09-01", "items": []any{map[string]any{"description": "x", "quantity": 1}}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := do(t, h, "POST", "/api/invoices", tt.body)
			assertStatus(t, rec, 400, tt.name)
		})
	}
}

func TestInvoiceCreateDBFailureIs500Not400(t *testing.T) {
	env := newTestEnv(t)
	env.conn.Close() // any repo access now fails with a non-ErrNotFound error

	rec := do(t, env.handler, "POST", "/api/invoices", map[string]any{
		"client_id":  "whatever",
		"issue_date": "2026-08-01",
		"due_date":   "2026-09-01",
		"items":      []any{map[string]any{"description": "x", "quantity": 1, "unit_price": 100}},
	})
	assertStatus(t, rec, 500, "db failure on create invoice")

	rec = do(t, env.handler, "POST", "/api/recurring", map[string]any{
		"client_id":           "whatever",
		"invoice_template_id": "whatever",
		"frequency":           "monthly",
		"delivery_method":     "all",
	})
	assertStatus(t, rec, 500, "db failure on create recurring")
}

func TestInvoicesListFilters(t *testing.T) {
	env := newTestEnv(t)
	h := env.handler

	c1 := mustCreateClient(t, env.repos, "One")
	c2 := mustCreateClient(t, env.repos, "Two")
	mustCreateInvoiceViaAPI(t, h, c1.ID)
	mustCreateInvoiceViaAPI(t, h, c2.ID)

	// all
	list := decode[[]invoiceShape](t, do(t, h, "GET", "/api/invoices", nil))
	if len(list) != 2 {
		t.Fatalf("want 2 invoices, got %d", len(list))
	}

	// by client
	list = decode[[]invoiceShape](t, do(t, h, "GET", "/api/invoices?client_id="+c1.ID, nil))
	if len(list) != 1 || list[0].ClientID != c1.ID {
		t.Fatalf("by client = %+v", list)
	}

	// mark c1's invoice paid through the dedicated endpoint
	paid := decode[invoiceShape](t, do(t, h, "POST", "/api/invoices/"+list[0].ID+"/mark-paid", nil))
	if paid.Status != "paid" {
		t.Fatalf("mark-paid status = %q", paid.Status)
	}

	list = decode[[]invoiceShape](t, do(t, h, "GET", "/api/invoices?status=paid", nil))
	if len(list) != 1 || list[0].Status != "paid" {
		t.Fatalf("by status = %+v", list)
	}
	list = decode[[]invoiceShape](t, do(t, h, "GET", "/api/invoices?status=draft", nil))
	if len(list) != 1 {
		t.Fatalf("draft filter = %+v", list)
	}
	list = decode[[]invoiceShape](t, do(t, h, "GET", "/api/invoices?status=draft&client_id="+c1.ID, nil))
	if len(list) != 0 { // c1's only invoice is now paid
		t.Fatalf("combined filter = %+v", list)
	}

	if rec := do(t, h, "GET", "/api/invoices?status=nope", nil); rec.Code != 400 {
		t.Fatalf("invalid status filter: status = %d, want 400", rec.Code)
	}
}

func TestInvoicePDFEndpoint(t *testing.T) {
	env := newTestEnv(t)
	h := env.handler
	client := mustCreateClient(t, env.repos, "Acme")
	inv := mustCreateInvoiceViaAPI(t, h, client.ID)

	rec := doRaw(t, h, "GET", "/api/invoices/"+inv.ID+"/pdf", "")
	assertStatus(t, rec, 200, "pdf")
	if ct := rec.Header().Get("Content-Type"); ct != "application/pdf" {
		t.Fatalf("content-type = %q", ct)
	}
	cd := rec.Header().Get("Content-Disposition")
	if !strings.Contains(cd, `attachment; filename="fatura-000001.pdf"`) {
		t.Fatalf("content-disposition = %q", cd)
	}
	body := rec.Body.Bytes()
	if len(body) < 5 || string(body[:4]) != "%PDF" {
		t.Fatalf("body does not look like a pdf (%d bytes)", len(body))
	}

	if rec := doRaw(t, h, "GET", "/api/invoices/nope/pdf", ""); rec.Code != 404 {
		t.Fatalf("missing invoice pdf: status = %d, want 404", rec.Code)
	}
}

type sendResponseShape struct {
	Sent    bool                   `json:"sent"`
	Results []channelResultPayload `json:"results"`
	Error   string                 `json:"error,omitempty"`
}

type channelResultPayload struct {
	Channel string `json:"channel"`
	Error   string `json:"error,omitempty"`
}

func TestSendInvoiceEndpoint(t *testing.T) {
	env := newTestEnv(t)
	h := env.handler
	client := mustCreateClient(t, env.repos, "Acme")
	setClientEmail(t, env.repos, client, "a@acme.com")
	inv := mustCreateInvoiceViaAPI(t, h, client.ID)

	rec := do(t, h, "POST", "/api/invoices/"+inv.ID+"/send", map[string]any{"method": "email"})
	assertStatus(t, rec, 200, "send email")
	resp := decode[sendResponseShape](t, rec)
	if !resp.Sent || len(resp.Results) != 1 || resp.Results[0].Channel != "email" || resp.Results[0].Error != "" {
		t.Fatalf("send resp = %+v", resp)
	}
	if env.email.invoiceCalls != 1 {
		t.Fatalf("email calls = %d, want 1", env.email.invoiceCalls)
	}

	got := decode[invoiceShape](t, do(t, h, "GET", "/api/invoices/"+inv.ID, nil))
	if got.Status != "sent" {
		t.Fatalf("invoice status = %q, want sent", got.Status)
	}
	if len(env.notifier.texts) != 1 || !strings.Contains(env.notifier.texts[0], "email") {
		t.Fatalf("notifier texts = %v", env.notifier.texts)
	}
}

type staticErr struct{ msg string }

func (e *staticErr) Error() string { return e.msg }

func TestSendInvoiceValidationAndFailures(t *testing.T) {
	env := newTestEnv(t)
	h := env.handler
	client := mustCreateClient(t, env.repos, "Acme")
	setClientEmail(t, env.repos, client, "a@acme.com")
	inv := mustCreateInvoiceViaAPI(t, h, client.ID)

	if rec := do(t, h, "POST", "/api/invoices/"+inv.ID+"/send", map[string]any{"method": "fax"}); rec.Code != 400 {
		t.Fatalf("unknown method: status = %d, want 400", rec.Code)
	}
	if rec := do(t, h, "POST", "/api/invoices/"+inv.ID+"/send", map[string]any{}); rec.Code != 400 {
		t.Fatalf("empty method: status = %d, want 400", rec.Code)
	}
	if rec := do(t, h, "POST", "/api/invoices/nope/send", map[string]any{"method": "all"}); rec.Code != 404 {
		t.Fatalf("unknown invoice send: status = %d, want 404", rec.Code)
	}
	if calls := env.email.invoiceCalls + env.wa.invoiceCalls + env.tg.invoiceCalls; calls != 0 {
		t.Fatalf("rejected sends must not contact channels, got %d calls", calls)
	}

	// failing channel: endpoint still answers 200 with per-channel errors,
	// and the invoice keeps its draft status.
	env.email.err = &staticErr{msg: "smtp down"}
	resp := decode[sendResponseShape](t, do(t, h, "POST", "/api/invoices/"+inv.ID+"/send", map[string]any{"method": "email"}))
	if resp.Sent || len(resp.Results) != 1 || resp.Results[0].Error != "smtp down" {
		t.Fatalf("failure resp = %+v", resp)
	}
	got := decode[invoiceShape](t, do(t, h, "GET", "/api/invoices/"+inv.ID, nil))
	if got.Status != "draft" {
		t.Fatalf("status = %q after failed send, want draft", got.Status)
	}
}
