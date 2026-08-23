package web_test

import (
	"context"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/ajesus37/heavens-invoicing/internal/model"
)

func TestPickerUsesListActive(t *testing.T) {
	ts, repos := newTestEnv(t)
	ctx := context.Background()

	pActive, err := repos.Products.Create(ctx, &model.Product{Name: "ActiveProd", Description: "active desc", UnitPrice: 1000})
	if err != nil {
		t.Fatalf("create active: %v", err)
	}
	pInactive, err := repos.Products.Create(ctx, &model.Product{Name: "InactiveProd", Description: "inactive desc", UnitPrice: 2000})
	if err != nil {
		t.Fatalf("create inactive: %v", err)
	}
	pInactive.Active = false
	if err := repos.Products.Update(ctx, pInactive); err != nil {
		t.Fatalf("deactivate: %v", err)
	}

	status, body := get(t, ts, "/invoices/new")
	if status != 200 {
		t.Fatalf("GET /invoices/new %d", status)
	}
	if !strings.Contains(body, "ActiveProd") {
		t.Errorf("picker should contain ActiveProd")
	}
	if strings.Contains(body, "InactiveProd") {
		t.Errorf("picker should NOT contain InactiveProd (inactive filtered via ListActive)")
	}
	if !strings.Contains(body, "("+pActive.ID+")") {
		t.Errorf("picker option should include ID in parentheses for dedup (Label — Price (ID)), missing (%s)", pActive.ID)
	}
	if strings.Count(body, "("+pActive.ID+")") < 2 {
		t.Errorf("both selects (main and template) should contain ID, count %d", strings.Count(body, "("+pActive.ID+")"))
	}
}

func TestRecurringClientTemplateValidation(t *testing.T) {
	ts, repos := newTestEnv(t)
	ctx := context.Background()

	c1, err := repos.Clients.Create(ctx, &model.Client{Name: "Client A"})
	if err != nil {
		t.Fatal(err)
	}
	c2, err := repos.Clients.Create(ctx, &model.Client{Name: "Client B"})
	if err != nil {
		t.Fatal(err)
	}
	inv := &model.Invoice{
		ClientID: c1.ID,
		Status:   "draft",
		IssueDate: time.Date(2026, 8, 1, 12, 0, 0, 0, time.UTC),
		DueDate:   time.Date(2026, 8, 10, 12, 0, 0, 0, time.UTC),
		Items:     []*model.InvoiceItem{{Description: "Svc", Quantity: 1, UnitPrice: 1000}},
	}
	if err := repos.Invoices.Create(ctx, inv); err != nil {
		t.Fatalf("create inv %v", err)
	}

	form := url.Values{
		"client_id":           {c2.ID},
		"invoice_template_id": {inv.ID},
		"frequency":           {"monthly"},
		"delivery_method":     {"email"},
		"next_send_date":      {"2026-09-01"},
	}
	resp, body := postForm(t, ts, "/recurring/new", form)
	if resp.StatusCode != 400 {
		t.Fatalf("mismatched client/template should be 400 got %d body %s", resp.StatusCode, body)
	}
	// error banner should mention mismatch (translated)
	if !strings.Contains(strings.ToLower(body), "template") && !strings.Contains(strings.ToLower(body), "modelo") {
		t.Errorf("mismatch response should mention template/client mismatch, got %q", body[:2000])
	}
	// matched should succeed
	form2 := url.Values{
		"client_id":           {c1.ID},
		"invoice_template_id": {inv.ID},
		"frequency":           {"monthly"},
		"delivery_method":     {"email"},
		"next_send_date":      {"2026-09-02"},
	}
	resp2, _ := postForm(t, ts, "/recurring/new", form2)
	if resp2.StatusCode != 303 {
		t.Fatalf("matched client/template should be 303 got %d", resp2.StatusCode)
	}
	scheds, _ := repos.Recurring.List(ctx)
	if len(scheds) != 1 {
		t.Fatalf("expected 1 schedule after one mismatch (blocked) + one valid, got %d", len(scheds))
	}
}
