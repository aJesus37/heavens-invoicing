package web_test

import (
	"context"
	"strings"
	"testing"

	"github.com/ajesus37/heavens-invoicing/internal/model"
)

func TestRecurringNewShowsNoDraftsHint(t *testing.T) {
	ts, repos := newTestEnv(t)
	ctx := context.Background()

	// No drafts: GET /recurring/new should contain hint.no_drafts_link and href="/invoices/new"
	status, body := get(t, ts, "/recurring/new")
	if status != 200 {
		t.Fatalf("GET /recurring/new (no drafts): got %d want 200\nbody: %s", status, body)
	}
	// Check hint is present: translated phrase and literal href
	hasHintPhrase := strings.Contains(body, "No draft invoices yet") || strings.Contains(body, "Nenhuma fatura rascunho") || strings.Contains(body, "create a draft invoice") || strings.Contains(body, "crie uma fatura rascunho")
	if !hasHintPhrase {
		t.Errorf("GET /recurring/new with 0 drafts: body missing hint.no_drafts_link phrase\nbody: %s", snippet(body, 2000))
	}
	if !strings.Contains(body, `href="/invoices/new"`) {
		t.Errorf("GET /recurring/new with 0 drafts: body missing href=\"/invoices/new\" (got escaped or missing)\nbody: %s", snippet(body, 2000))
	}
	if strings.Contains(body, "!hint.no_drafts_link") {
		t.Errorf("GET /recurring/new: body contains missing i18n marker !hint.no_drafts_link")
	}

	// With 1 draft: hint absent
	clientID := seedClient(t, repos, "Hint Client")
	inv := &model.Invoice{
		ClientID:  clientID,
		Status:    "draft",
		IssueDate: dateUTC(2026, 8, 1),
		DueDate:   dateUTC(2026, 8, 10),
		Items:     []*model.InvoiceItem{{Description: "Mensalidade", Quantity: 1, UnitPrice: 9900}},
	}
	if err := repos.Invoices.Create(ctx, inv); err != nil {
		t.Fatalf("create draft: %v", err)
	}

	status2, body2 := get(t, ts, "/recurring/new")
	if status2 != 200 {
		t.Fatalf("GET /recurring/new (with 1 draft): got %d want 200\nbody: %s", status2, body2)
	}
	hasHintAfter := strings.Contains(body2, "No draft invoices yet") || strings.Contains(body2, "Nenhuma fatura rascunho") || strings.Contains(body2, "create a draft invoice") || strings.Contains(body2, "crie uma fatura rascunho")
	if hasHintAfter {
		t.Errorf("GET /recurring/new with 1 draft: hint should be absent but found phrase\nbody: %s", snippet(body2, 2000))
	}
	// The recurring template hint should still be present, but the no_drafts_link hint should be gone
	// We check that the specific no_drafts phrase is gone; href for the hint should also be gone (or at least not in hint context)
	// To avoid false positive from other links, check phrase absence is primary
}

func snippet(s string, n int) string {
	if len(s) > n {
		return s[:n]
	}
	return s
}
