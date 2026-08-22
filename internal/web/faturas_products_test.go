package web_test

import (
	"context"
	"strings"
	"testing"

	"github.com/jesus/invoice-app/internal/model"
	"github.com/jesus/invoice-app/internal/repo"
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

	status, body := get(t, ts, "/faturas/nova")
	if status != 200 {
		t.Fatalf("GET /faturas/nova: got %d want 200\nbody: %s", status, body)
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
	status, body := get(t, ts, "/faturas/nova")
	if status != 200 {
		t.Fatalf("GET /faturas/nova (no products): got %d want 200", status)
	}
	// Should still render the form without panic; picker may be empty.
	if !strings.Contains(body, `name="item_desc_0"`) {
		t.Error("invoice form missing item rows when no products")
	}
	_ = body
}

func TestProductPickerJS(t *testing.T) {
	ts, _ := newTestEnv(t)
	status, body := get(t, ts, "/faturas/nova")
	if status != 200 {
		t.Fatalf("GET /faturas/nova: got %d want 200", status)
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
	status, body := get(t, ts, "/faturas/nova")
	if status != 200 {
		t.Fatalf("GET /faturas/nova (pt-BR): got %d want 200\nbody: %s", status, body)
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
	status, body = get(t, ts, "/faturas/nova")
	if status != 200 {
		t.Fatalf("GET /faturas/nova (en): got %d want 200\nbody: %s", status, body)
	}
	if strings.Contains(body, "!label.select_product") {
		t.Error("invoice form contains missing i18n marker !label.select_product (en)")
	}
	if !strings.Contains(body, "Select product") {
		t.Error("invoice form missing en translation for label.select_product: want \"Select product\"")
	}
}
