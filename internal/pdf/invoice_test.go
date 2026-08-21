package pdf

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"github.com/jesus/invoice-app/internal/model"
)

var fixturePIX = "pagamentos@heaven-labs.com"

func senderFixture() SenderInfo {
	return SenderInfo{
		Name:    "Heaven Labs LTDA",
		Address: "Rua das Flores, 123\nSao Paulo - SP\nCEP 01234-567",
		PIXKey:  fixturePIX,
	}
}

func invoiceFixture(pix *string) model.Invoice {
	return model.Invoice{
		ID:        model.NewID(),
		ClientID:  model.NewID(),
		Number:    1,
		Status:    "draft",
		IssueDate: time.Date(2026, time.August, 21, 0, 0, 0, 0, time.UTC),
		DueDate:   time.Date(2026, time.September, 5, 0, 0, 0, 0, time.UTC),
		Subtotal:  15000,
		Total:     15000,
		Notes:     "Obrigado pela preferencia.",
		PIXKey:    pix,
		Items: []*model.InvoiceItem{
			{ID: model.NewID(), InvoiceID: model.NewID(), Description: "Desenvolvimento de site", UnitPrice: 5000, Quantity: 2, Total: 10000},
			{ID: model.NewID(), InvoiceID: model.NewID(), Description: "Hospedagem mensal", UnitPrice: 2500, Quantity: 2, Total: 5000},
		},
	}
}

func TestRenderInvoiceProducesPDF(t *testing.T) {
	pix := fixturePIX
	client := model.Client{
		ID:      model.NewID(),
		Name:    "Maria Souza",
		Address: "Av Paulista, 1000\nSao Paulo - SP",
	}
	var buf bytes.Buffer
	if err := RenderInvoice(&buf, senderFixture(), client, invoiceFixture(&pix)); err != nil {
		t.Fatalf("RenderInvoice() error = %v", err)
	}
	out := buf.Bytes()
	if !bytes.HasPrefix(out, []byte("%PDF")) {
		t.Errorf("output does not start with %%PDF magic: %q", out[:min(len(out), 8)])
	}
	if buf.Len() <= 1000 {
		t.Errorf("PDF too small: %d bytes", buf.Len())
	}
}

func TestPixKeyFor(t *testing.T) {
	sender := senderFixture()
	invoicePIX := fixturePIX
	tests := []struct {
		name       string
		invPix     *string
		senderPix  string
		want       *string
		wantString string
	}{
		{name: "invoice pix wins", invPix: &invoicePIX, senderPix: sender.PIXKey, wantString: invoicePIX},
		{name: "falls back to sender", invPix: nil, senderPix: sender.PIXKey, wantString: fixturePIX},
		{name: "no pix at all", invPix: nil, senderPix: "", want: nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := PixKeyFor(invoiceFixture(tt.invPix), SenderInfo{Name: "Heaven Labs LTDA", PIXKey: tt.senderPix})
			if tt.want == nil && got == nil {
				return
			}
			if got == nil || *got != tt.wantString {
				t.Errorf("PixKeyFor() = %v, want %q", got, tt.wantString)
			}
		})
	}
}

func TestRenderInvoiceWithoutNotesAndNoPix(t *testing.T) {
	inv := invoiceFixture(nil)
	inv.Notes = ""
	sender := SenderInfo{Name: "Heaven Labs LTDA", Address: "Rua das Flores, 123"}
	var buf bytes.Buffer
	if err := RenderInvoice(&buf, sender, model.Client{Name: "Maria Souza", Address: "Av Paulista, 1000"}, inv); err != nil {
		t.Fatalf("RenderInvoice() error = %v", err)
	}
	if !strings.HasPrefix(buf.String(), "%PDF") {
		t.Error("expected PDF output")
	}
}

func TestRenderInvoiceLongDescription(t *testing.T) {
	pix := fixturePIX
	inv := invoiceFixture(&pix)
	inv.Items[0].Description = strings.Repeat("Descricao muito longa ", 20)
	var buf bytes.Buffer
	if err := RenderInvoice(&buf, senderFixture(), model.Client{Name: "Maria Souza"}, inv); err != nil {
		t.Fatalf("RenderInvoice() error = %v", err)
	}
	if buf.Len() <= 1000 {
		t.Errorf("PDF too small: %d bytes", buf.Len())
	}
}

func TestUniTranslatesAccentsToCP1252(t *testing.T) {
	got := uni("Emissão Cobrança Preço Observações")
	want := "Emiss\xE3o Cobran\xE7a Pre\xE7o Observa\xE7\xF5es"
	if got != want {
		t.Errorf("uni() = %q, want %q", got, want)
	}
	if got := uni("plain ASCII stays"); got != "plain ASCII stays" {
		t.Errorf("uni(ascii) = %q", got)
	}
}

func TestFormatInvoiceNumber(t *testing.T) {
	tests := []struct {
		number int64
		want   string
	}{
		{1, "Fatura #000001"},
		{123456, "Fatura #123456"},
		{1234567, "Fatura #1234567"},
	}
	for _, tt := range tests {
		if got := formatInvoiceNumber(tt.number); got != tt.want {
			t.Errorf("formatInvoiceNumber(%d) = %q, want %q", tt.number, got, tt.want)
		}
	}
}
