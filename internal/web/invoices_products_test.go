package web_test

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/ajesus37/heavens-invoicing/internal/model"
	"github.com/ajesus37/heavens-invoicing/internal/repo"
)

func TestNewInvoiceFormShowsProductPicker(t *testing.T) {
	ts, repos := newTestEnv(t)
	ctx := context.Background()

	// Seed two products with distinct names, descriptions and prices.
	p1, err := repos.Products.Create(ctx, &model.Product{
		Name:        "Consultoria",
		Description: "Hora técnica",
		UnitPrice:   150000, // 1500,00
	})
	if err != nil {
		t.Fatalf("create p1: %v", err)
	}
	p2, err := repos.Products.Create(ctx, &model.Product{
		Name:        "Hospedagem",
		Description: "Mensalidade servidor",
		UnitPrice:   9990, // 99,90
	})
	if err != nil {
		t.Fatalf("create p2: %v", err)
	}

	status, body := get(t, ts, "/invoices/new")
	if status != 200 {
		t.Fatalf("GET /invoices/new: got %d want 200\nbody: %s", status, body)
	}

	// Both product names should appear as options.
	for _, name := range []string{"Consultoria", "Hospedagem"} {
		if !strings.Contains(body, name) {
			t.Errorf("invoice form missing product %q", name)
		}
	}

	// Check product picker options carry snapshot data in data attributes.
	// Value should be product ID, and description/price should be present.
	for _, p := range []*model.Product{p1, p2} {
		if !strings.Contains(body, `value="`+p.ID+`"`) {
			t.Errorf("invoice form missing option value %q", p.ID)
		}
	}
	if !strings.Contains(body, `data-description="Hora técnica"`) {
		t.Error("invoice form missing data-description for p1")
	}
	if !strings.Contains(body, `data-description="Mensalidade servidor"`) {
		t.Error("invoice form missing data-description for p2")
	}
	// Price is rendered via formatReais (e.g. "1500,00") or FormatBRL ("R$ 1.500,00").
	// Accept either, but must contain the numeric value.
	if !strings.Contains(body, `data-price="1500,00"`) && !strings.Contains(body, `data-price="R$ 1.500,00"`) {
		t.Error("invoice form missing data-price for p1 (expected 1500,00)")
	}
	if !strings.Contains(body, `data-price="99,90"`) && !strings.Contains(body, `data-price="R$ 99,90"`) {
		t.Error("invoice form missing data-price for p2 (expected 99,90)")
	}
	// Picker should be linked to rows via class and data-row.
	if !strings.Contains(body, `product-picker`) {
		t.Error("invoice form missing product-picker marker")
	}
}

func TestNewInvoiceFormWithNoProducts(t *testing.T) {
	ts, _ := newTestEnv(t)
	status, body := get(t, ts, "/invoices/new")
	if status != 200 {
		t.Fatalf("GET /invoices/new (no products): got %d want 200", status)
	}
	// Should still render the form without panic; picker may be empty.
	if !strings.Contains(body, `name="item_desc_0"`) {
		t.Error("invoice form missing item rows when no products")
	}
	_ = body
}

func TestProductPickerJS(t *testing.T) {
	ts, _ := newTestEnv(t)
	status, body := get(t, ts, "/invoices/new")
	if status != 200 {
		t.Fatalf("GET /invoices/new: got %d want 200", status)
	}
	if !strings.Contains(body, "<script>") {
		t.Error("invoice form missing <script> for product picker")
	}
	if !strings.Contains(body, "product-picker") {
		t.Error("invoice form script missing product-picker selector")
	}
	if !strings.Contains(body, "addEventListener") || !strings.Contains(body, "change") {
		t.Error("invoice form script missing addEventListener change handling")
	}
	if !strings.Contains(body, "dataset.description") && !strings.Contains(body, "data-description") {
		t.Error("invoice form script missing data-description/dataset.description handling")
	}
	if !strings.Contains(body, "dataset.price") && !strings.Contains(body, "data-price") {
		t.Error("invoice form script missing data-price/dataset.price handling")
	}
	if !strings.Contains(body, "item_desc_") {
		t.Error("invoice form script missing item_desc_ input handling")
	}
	if !strings.Contains(body, "item_price_") {
		t.Error("invoice form script missing item_price_ input handling")
	}
	if !strings.Contains(body, "dataset.row") {
		t.Error("invoice form script missing dataset.row handling")
	}
	if !strings.Contains(body, "querySelector") {
		t.Error("invoice form script missing querySelector handling")
	}
}

func TestProductPickerI18n(t *testing.T) {
	ts, repos := newTestEnv(t)
	ctx := context.Background()

	// Default locale is pt-BR: picker placeholder should be Portuguese.
	status, body := get(t, ts, "/invoices/new")
	if status != 200 {
		t.Fatalf("GET /invoices/new (pt-BR): got %d want 200\nbody: %s", status, body)
	}
	if strings.Contains(body, "!label.select_product") {
		t.Error("invoice form contains missing i18n marker !label.select_product (pt-BR)")
	}
	if !strings.Contains(body, "Selecione o produto") {
		t.Error("invoice form missing pt-BR translation for label.select_product: want \"Selecione o produto\"")
	}

	// Switch to English and verify translation changes.
	if err := repos.Settings.Set(ctx, repo.SettingLocale, "en"); err != nil {
		t.Fatalf("set locale en: %v", err)
	}
	status, body = get(t, ts, "/invoices/new")
	if status != 200 {
		t.Fatalf("GET /invoices/new (en): got %d want 200\nbody: %s", status, body)
	}
	if strings.Contains(body, "!label.select_product") {
		t.Error("invoice form contains missing i18n marker !label.select_product (en)")
	}
	if !strings.Contains(body, "Select product") {
		t.Error("invoice form missing en translation for label.select_product: want \"Select product\"")
	}
}

func TestInvoiceFormHasTotalsAndAddButton(t *testing.T) {
	ts, _ := newTestEnv(t)
	status, body := get(t, ts, "/invoices/new")
	if status != 200 {
		t.Fatalf("GET invoice form: got %d want 200\nbody: %s", status, body)
	}
	if !strings.Contains(body, "row-total") {
		t.Error("invoice form missing row-total cell (expected class=\"row-total\")")
	}
	if !strings.Contains(body, "grand-total") {
		t.Error("invoice form missing grand total element (expected id=\"grand-total\")")
	}
	if !strings.Contains(body, `id="grand-total"`) {
		t.Error("invoice form missing id=\"grand-total\" element")
	}
	if !strings.Contains(body, `id="add-item"`) {
		t.Error("invoice form missing add button (expected id=\"add-item\")")
	}
	if !strings.Contains(body, `id="row-template"`) {
		t.Error("invoice form missing template#row-template")
	}
	if !strings.Contains(body, "<template") {
		t.Error("invoice form missing <template> for new rows")
	}
	if !strings.Contains(body, "__IDX__") {
		t.Error("invoice form missing __IDX__ placeholder in row template")
	}
	if !strings.Contains(body, "remove-row") {
		t.Error("invoice form missing remove-row button")
	}
	if !strings.Contains(body, "<tfoot") {
		t.Error("invoice form missing <tfoot> with grand total")
	}
	// Script recalc / total logic
	if !strings.Contains(body, "recalc") {
		t.Error("invoice form script missing recalc logic")
	}
	if !strings.Contains(body, "parsePrice") && !strings.Contains(body, "parse") {
		t.Error("invoice form script missing parsePrice logic")
	}
	if !strings.Contains(strings.ToLower(body), "grand-total") {
		t.Error("invoice form script missing grand-total handling")
	}
	if !strings.Contains(body, "row-total") {
		t.Error("invoice form script missing row-total handling")
	}
}

func TestInvoiceFormSingleInitialRow(t *testing.T) {
	ts, _ := newTestEnv(t)
	status, body := get(t, ts, "/invoices/new")
	if status != 200 {
		t.Fatalf("GET invoice form: got %d want 200", status)
	}
	// Count visible product-picker selects, excluding the <template> block.
	// Only count <select> tags with the marker to avoid counting JS selectors.
	visible := body
	if start := strings.Index(visible, `<template`); start != -1 {
		if end := strings.Index(visible[start:], `</template>`); end != -1 {
			visible = visible[:start] + visible[start+end+len(`</template>`):]
		}
	}
	// Strip <script> blocks for counts that would also appear in JS.
	stripScripts := func(s string) string {
		for {
			start := strings.Index(s, "<script")
			if start == -1 {
				break
			}
			end := strings.Index(s[start:], "</script>")
			if end == -1 {
				break
			}
			s = s[:start] + s[start+end+len("</script>"):]
		}
		return s
	}
	visibleNoScript := stripScripts(visible)
	count := strings.Count(visibleNoScript, `class="product-picker"`)
	if count != 1 {
		snippet := visible
		if len(snippet) > 2000 {
			snippet = snippet[:2000]
		}
		t.Errorf("expected 1 visible product-picker select, got %d (visible body snippet: %q)", count, snippet)
	}
	// Also ensure only one initial item_desc_0 visible and not 5 pre-rendered rows.
	visibleDescCount := strings.Count(visibleNoScript, `name="item_desc_`)
	if visibleDescCount != 1 {
		t.Errorf("expected 1 visible item row (item_desc_0), got %d", visibleDescCount)
	}
}

func TestCreateInvoiceDynamicRows(t *testing.T) {
	t.Run("sparse indices 0,5,12 creates 3 items", func(t *testing.T) {
		ts, repos := newTestEnv(t)
		clientID := seedClient(t, repos, "Sparse Client")

		// Use Portuguese route (current); fall back to English if renamed.
		form := url.Values{
			"client_id":    {clientID},
			"issue_date":   {"2026-08-01"},
			"due_date":     {"2026-08-15"},
			"item_desc_0":  {"Service A"},
			"item_qty_0":   {"1"},
			"item_price_0": {"100,00"},
			"item_desc_5":  {"Service B"},
			"item_qty_5":   {"2"},
			"item_price_5": {"50,00"},
			"item_desc_12": {"Service C"},
			"item_qty_12":  {"3"},
			"item_price_12": {"10,00"},
		}
		resp, body := postForm(t, ts, "/invoices/new", form)
		if resp.StatusCode != http.StatusSeeOther {
			t.Fatalf("sparse POST: got %d want 303\nbody: %s", resp.StatusCode, body)
		}
		invoices, err := repos.Invoices.List(context.Background())
		if err != nil {
			t.Fatalf("list invoices: %v", err)
		}
		if len(invoices) != 1 {
			t.Fatalf("expected 1 invoice, got %d", len(invoices))
		}
		inv, err := repos.Invoices.Get(context.Background(), invoices[0].ID)
		if err != nil {
			t.Fatalf("get invoice: %v", err)
		}
		if len(inv.Items) != 3 {
			t.Fatalf("expected 3 items (sparse 0,5,12), got %d", len(inv.Items))
		}
		want := map[string]bool{"Service A": false, "Service B": false, "Service C": false}
		for _, it := range inv.Items {
			if _, ok := want[it.Description]; ok {
				want[it.Description] = true
			}
		}
		for desc, found := range want {
			if !found {
				t.Errorf("missing item %q in invoice", desc)
			}
		}
		// Verify quantities/prices preserved in order (0,5,12).
		if inv.Items[0].Quantity != 1 || inv.Items[0].UnitPrice != 10000 {
			t.Errorf("item 0 mismatch: qty=%d price=%d", inv.Items[0].Quantity, inv.Items[0].UnitPrice)
		}
		if inv.Items[1].Quantity != 2 || inv.Items[1].UnitPrice != 5000 {
			t.Errorf("item 1 (sparse idx 5) mismatch: qty=%d price=%d", inv.Items[1].Quantity, inv.Items[1].UnitPrice)
		}
		if inv.Items[2].Quantity != 3 || inv.Items[2].UnitPrice != 1000 {
			t.Errorf("item 2 (sparse idx 12) mismatch: qty=%d price=%d", inv.Items[2].Quantity, inv.Items[2].UnitPrice)
		}
	})

	t.Run("empty gaps are ignored", func(t *testing.T) {
		ts, repos := newTestEnv(t)
		clientID := seedClient(t, repos, "Gap Client")
		form := url.Values{
			"client_id":    {clientID},
			"issue_date":   {"2026-08-01"},
			"due_date":     {"2026-08-15"},
			"item_desc_0":  {"Keep A"},
			"item_qty_0":   {"1"},
			"item_price_0": {"10,00"},
			// gap at 1 (completely empty) should be ignored
			"item_desc_2":  {"Keep B"},
			"item_qty_2":   {"1"},
			"item_price_2": {"20,00"},
			// also ensure empty intermediate indices don't create items
		}
		resp, body := postForm(t, ts, "/invoices/new", form)
		if resp.StatusCode != http.StatusSeeOther {
			t.Fatalf("gap POST: got %d want 303\nbody: %s", resp.StatusCode, body)
		}
		invoices, err := repos.Invoices.List(context.Background())
		if err != nil {
			t.Fatalf("list: %v", err)
		}
		if len(invoices) != 1 {
			t.Fatalf("expected 1 invoice, got %d", len(invoices))
		}
		inv, err := repos.Invoices.Get(context.Background(), invoices[0].ID)
		if err != nil {
			t.Fatalf("get: %v", err)
		}
		if len(inv.Items) != 2 {
			t.Fatalf("expected 2 items with gap ignored, got %d", len(inv.Items))
		}
		if inv.Items[0].Description != "Keep A" || inv.Items[1].Description != "Keep B" {
			t.Errorf("gap items wrong order: %q, %q", inv.Items[0].Description, inv.Items[1].Description)
		}
	})

	t.Run("20 items max succeeds", func(t *testing.T) {
		ts, repos := newTestEnv(t)
		clientID := seedClient(t, repos, "Max Client")
		form := url.Values{
			"client_id":  {clientID},
			"issue_date": {"2026-08-01"},
			"due_date":   {"2026-08-15"},
		}
		for i := 0; i < 20; i++ {
			form.Set(fmt.Sprintf("item_desc_%d", i), fmt.Sprintf("Item %d", i))
			form.Set(fmt.Sprintf("item_qty_%d", i), "1")
			form.Set(fmt.Sprintf("item_price_%d", i), "10,00")
		}
		resp, body := postForm(t, ts, "/invoices/new", form)
		if resp.StatusCode != http.StatusSeeOther {
			t.Fatalf("20 items POST: got %d want 303\nbody: %s", resp.StatusCode, body)
		}
		invoices, err := repos.Invoices.List(context.Background())
		if err != nil {
			t.Fatalf("list: %v", err)
		}
		if len(invoices) != 1 {
			t.Fatalf("expected 1 invoice, got %d", len(invoices))
		}
		inv, err := repos.Invoices.Get(context.Background(), invoices[0].ID)
		if err != nil {
			t.Fatalf("get: %v", err)
		}
		if len(inv.Items) != 20 {
			t.Fatalf("expected 20 items, got %d", len(inv.Items))
		}
		for i, it := range inv.Items {
			want := fmt.Sprintf("Item %d", i)
			if it.Description != want {
				t.Errorf("item %d: got %q want %q", i, it.Description, want)
			}
		}
	})

	t.Run("21st item is ignored", func(t *testing.T) {
		ts, repos := newTestEnv(t)
		clientID := seedClient(t, repos, "Overflow Client")
		form := url.Values{
			"client_id":  {clientID},
			"issue_date": {"2026-08-01"},
			"due_date":   {"2026-08-15"},
		}
		for i := 0; i < 21; i++ {
			form.Set(fmt.Sprintf("item_desc_%d", i), fmt.Sprintf("Item %d", i))
			form.Set(fmt.Sprintf("item_qty_%d", i), "1")
			form.Set(fmt.Sprintf("item_price_%d", i), "10,00")
		}
		resp, body := postForm(t, ts, "/invoices/new", form)
		if resp.StatusCode != http.StatusSeeOther {
			t.Fatalf("21 items POST: got %d want 303 (21st should be ignored)\nbody: %s", resp.StatusCode, body)
		}
		invoices, err := repos.Invoices.List(context.Background())
		if err != nil {
			t.Fatalf("list: %v", err)
		}
		if len(invoices) != 1 {
			t.Fatalf("expected 1 invoice, got %d", len(invoices))
		}
		inv, err := repos.Invoices.Get(context.Background(), invoices[0].ID)
		if err != nil {
			t.Fatalf("get: %v", err)
		}
		if len(inv.Items) != 20 {
			t.Fatalf("expected 20 items (21st ignored), got %d", len(inv.Items))
		}
		for _, it := range inv.Items {
			if it.Description == "Item 20" {
				t.Errorf("21st item should have been ignored but found %q", it.Description)
			}
		}
		// Ensure last kept is Item 19.
		if inv.Items[19].Description != "Item 19" {
			t.Errorf("last item should be Item 19, got %q", inv.Items[19].Description)
		}
	})
}
