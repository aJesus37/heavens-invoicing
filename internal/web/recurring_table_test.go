package web_test

import (
	"context"
	"os"
	"strings"
	"testing"

	"github.com/ajesus37/heavens-invoicing/internal/model"
)

func TestRecurringTableColspan(t *testing.T) {
	ts, repos := newTestEnv(t)

	// 1. Empty state: GET /recurring should have 8 <th> and colspan="8" (Actions column consolidated)
	status, body := get(t, ts, "/recurring")
	if status != 200 {
		t.Fatalf("GET /recurring: got %d want 200\nbody: %s", status, body)
	}

	// Count <th> elements: use "<th>" and "<th " to avoid matching <thead>
	thCount := strings.Count(body, "<th>") + strings.Count(body, "<th ")
	if thCount != 8 {
		t.Errorf("header th count = %d, want 8 (body emits 8 td; mismatch breaks alignment)", thCount)
	}

	if !strings.Contains(body, `colspan="8"`) {
		t.Errorf("empty state colspan should be 8, body missing colspan=\"8\"; got body snippet: %s", snippet(body, 2000))
	}
	if strings.Contains(body, `colspan="9"`) {
		t.Errorf("found stale colspan=\"9\", should be 8")
	}

	// Check overflow wrapper around table for mobile
	hasOverflowWrapper := strings.Contains(body, "overflow-x:auto") || strings.Contains(body, "overflow-x: auto")
	hasTableWrapClass := strings.Contains(body, "table-wrap")
	if !hasOverflowWrapper && !hasTableWrapClass {
		t.Errorf("recurring table should be wrapped in element with overflow-x:auto for mobile; missing wrapper")
	}
	// Wrapper should be inside .panel and around <table>
	if hasOverflowWrapper {
		// ensure wrapper contains <table> inside .panel
		panelIdx := strings.Index(body, `class="panel"`)
		tableIdx := strings.Index(body, "<table>")
		overflowIdx := strings.Index(body, "overflow-x:auto")
		if overflowIdx == -1 {
			overflowIdx = strings.Index(body, "overflow-x: auto")
		}
		if !(panelIdx != -1 && overflowIdx != -1 && tableIdx != -1 && panelIdx < overflowIdx && overflowIdx < tableIdx) {
			t.Errorf("overflow wrapper should be inside .panel and wrap <table> (panel=%d overflow=%d table=%d)", panelIdx, overflowIdx, tableIdx)
		}
	}

	// 2. With data: row <td> count should match header <th> count
	ctx := context.Background()
	clientID := seedClient(t, repos, "Colspan Client")
	tpl := &model.Invoice{
		ClientID:  clientID,
		Status:    "draft",
		IssueDate: dateUTC(2026, 8, 1),
		DueDate:   dateUTC(2026, 9, 1),
		Items:     []*model.InvoiceItem{{Description: "Serviço", UnitPrice: 1000, Quantity: 1}},
	}
	if err := repos.Invoices.Create(ctx, tpl); err != nil {
		t.Fatalf("create template: %v", err)
	}
	sched := &model.RecurringSchedule{
		ClientID:          clientID,
		InvoiceTemplateID: tpl.ID,
		Frequency:         "monthly",
		NextSendDate:      dateUTC(2026, 9, 1),
		DeliveryMethod:    "email",
	}
	if err := repos.Recurring.Create(ctx, sched); err != nil {
		t.Fatalf("create schedule: %v", err)
	}

	status, body = get(t, ts, "/recurring")
	if status != 200 {
		t.Fatalf("GET /recurring with row: got %d want 200", status)
	}

	// Re-check th count still 8
	thCount = strings.Count(body, "<th>") + strings.Count(body, "<th ")
	if thCount != 8 {
		t.Errorf("with data: header th count = %d, want 8", thCount)
	}

	// Count td in first data row (between first <tr> after <tbody> and </tr>)
	// Find tbody section
	tbodyIdx := strings.Index(body, "<tbody>")
	if tbodyIdx == -1 {
		t.Fatalf("missing <tbody>")
	}
	// Find first <tr> after tbody that is not the empty state
	// Look for the first occurrence of "<tr>" after tbody, then count <td> until </tr>
	trStart := strings.Index(body[tbodyIdx:], "<tr>")
	if trStart == -1 {
		t.Fatalf("missing <tr> in tbody")
	}
	trStart += tbodyIdx
	trEnd := strings.Index(body[trStart:], "</tr>")
	if trEnd == -1 {
		t.Fatalf("missing </tr>")
	}
	trEnd += trStart
	rowHTML := body[trStart:trEnd]
	tdCount := strings.Count(rowHTML, "<td")
	if tdCount != thCount {
		t.Errorf("row td count = %d, header th count = %d, should match; row snippet: %s", tdCount, thCount, snippet(rowHTML, 500))
	}
	if tdCount != 8 {
		t.Errorf("row td count = %d, want 8", tdCount)
	}

	// 3. CSS should provide overflow handling
	cssPath := "web/static/app.css"
	// Try relative to repo root; if not found, try from working dir
	cssData, err := os.ReadFile(cssPath)
	if err != nil {
		// fallback to absolute path used in CI
		cssData, err = os.ReadFile("/home/jesus/invoice-app/web/static/app.css")
		if err != nil {
			t.Fatalf("read css: %v", err)
		}
	}
	css := string(cssData)
	if !strings.Contains(css, "overflow-x:auto") && !strings.Contains(css, "overflow-x: auto") {
		t.Errorf("app.css missing overflow-x:auto for table wrapper/mobile overflow")
	}
	// Accept either .table-wrap or .panel table with overflow
	hasCSSWrapper := strings.Contains(css, ".table-wrap") && strings.Contains(css, "overflow-x")
	hasPanelTableOverflow := strings.Contains(css, ".panel table") || strings.Contains(css, "table-wrap")
	_ = hasPanelTableOverflow
	if !hasCSSWrapper && !strings.Contains(css, "overflow-x") {
		t.Errorf("app.css should define .table-wrap or equivalent overflow rule")
	}
}
