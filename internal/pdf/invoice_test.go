package pdf

import (
	"bytes"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ajesus37/heavens-invoicing/internal/i18n"
	"github.com/ajesus37/heavens-invoicing/internal/model"
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

// renderUncompressed produces an uncompressed stream so tests can assert
// on localized text markers inside the PDF content.
func renderUncompressed(t *testing.T, client model.Client) string {
	t.Helper()
	pix := fixturePIX
	var buf bytes.Buffer
	if err := renderInvoice(&buf, senderFixture(), client, invoiceFixture(&pix), false); err != nil {
		t.Fatalf("renderInvoice() error = %v", err)
	}
	return buf.String()
}

func TestRenderInvoiceLocalizesPerClientLanguage(t *testing.T) {
	tests := []struct {
		name        string
		language    string
		wantMarkers []string
		notWant     []string
	}{
		{
			name:     "en client gets english labels",
			language: "en",
			wantMarkers: []string{
				"Invoice #000001", "Issued: 21/08/2026", "Due: 05/09/2026",
				"Bill to:", "Qty", "Description", "Unit price",
				"Subtotal", "Total", "PIX key: " + fixturePIX,
				"Notes:",
			},
			notWant: []string{"Fatura #", "Chave PIX"},
		},
		{
			name:     "pt-BR client keeps portuguese labels",
			language: "pt-BR",
			// Accented labels are translated to cp1252 single bytes by
			// tr(), so markers use their byte sequences or ASCII prefixes.
			wantMarkers: []string{
				"Fatura #000001", "Emiss\xe3o: 21/08/2026", "Vencimento: 05/09/2026",
				"Cobran\xe7a para:", "Qtd", "Descri\xe7\xe3o", "Pre\xe7o Unit.",
				"Subtotal", "Total", "Chave PIX: " + fixturePIX,
				"Observa\xe7\xf5es:",
			},
			notWant: []string{"Invoice #", "Bill to:"},
		},
		{
			name:        "empty language defaults to pt-BR",
			language:    "",
			wantMarkers: []string{"Fatura #000001", "Cobran\xe7a para:", "Chave PIX:"},
			notWant:     []string{"Invoice #", "Bill to:"},
		},
		{
			name:        "unknown language falls back to pt-BR",
			language:    "klingon",
			wantMarkers: []string{"Fatura #000001", "Vencimento:", "Chave PIX:"},
			notWant:     []string{"Invoice #", "PIX key:"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := renderUncompressed(t, model.Client{Name: "Maria Souza", Language: tt.language})
			for _, m := range tt.wantMarkers {
				if !strings.Contains(got, m) {
					t.Errorf("language %q: pdf missing marker %q", tt.language, m)
				}
			}
			for _, m := range tt.notWant {
				if strings.Contains(got, m) {
					t.Errorf("language %q: pdf unexpectedly contains %q", tt.language, m)
				}
			}
		})
	}
}

func TestPixKeyFor(t *testing.T) {
	sender := senderFixture()
	invoicePIX := "pix@invoice.com"
	tests := []struct {
		name       string
		invPix     *string
		senderPix  string
		wantNil    bool
		wantString string
	}{
		{name: "invoice pix wins", invPix: &invoicePIX, senderPix: sender.PIXKey, wantString: invoicePIX},
		{name: "empty invoice pix falls back", invPix: strPtr(""), senderPix: sender.PIXKey, wantString: fixturePIX},
		{name: "falls back to sender", invPix: nil, senderPix: sender.PIXKey, wantString: fixturePIX},
		{name: "no pix at all", invPix: nil, senderPix: "", wantNil: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := PixKeyFor(invoiceFixture(tt.invPix), SenderInfo{Name: "Heaven Labs LTDA", PIXKey: tt.senderPix})
			if tt.wantNil {
				if got != nil {
					t.Errorf("PixKeyFor() = %q, want nil", *got)
				}
				return
			}
			if got == nil || *got != tt.wantString {
				t.Errorf("PixKeyFor() = %v, want %q", got, tt.wantString)
			}
		})
	}
}

func strPtr(s string) *string { return &s }

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
	got := tr("Emissão Cobrança Preço Observações")
	want := "Emiss\xE3o Cobran\xE7a Pre\xE7o Observa\xE7\xF5es"
	if got != want {
		t.Errorf("tr() = %q, want %q", got, want)
	}
	if got := tr("plain ASCII stays"); got != "plain ASCII stays" {
		t.Errorf("tr(ascii) = %q", got)
	}
}

func TestRenderInvoiceConcurrent(t *testing.T) {
	const goroutines = 8
	const rendersPerGoroutine = 5

	errs := make(chan error, goroutines*rendersPerGoroutine)
	var wg sync.WaitGroup
	for range goroutines {
		wg.Add(1)
		go func() {
			defer wg.Done()
			sender := SenderInfo{
				Name:    "Heaven Labs LTDA",
				Address: "Rua das Flores, 123\nSão Paulo - SP",
				PIXKey:  fixturePIX,
			}
			client := model.Client{Name: "José da Conceição", Address: "Av Paulista, 1000\nSão Paulo - SP"}
			pix := fixturePIX
			for range rendersPerGoroutine {
				inv := invoiceFixture(&pix)
				var buf bytes.Buffer
				if err := RenderInvoice(&buf, sender, client, inv); err != nil {
					errs <- err
					return
				}
				if !bytes.HasPrefix(buf.Bytes(), []byte("%PDF")) {
					errs <- errors.New("output missing %PDF magic")
					return
				}
			}
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Error(err)
	}
}

func TestFormatInvoiceNumber(t *testing.T) {
	tests := []struct {
		lang   i18n.Lang
		number int64
		want   string
	}{
		{i18n.PtBR, 1, "Fatura #000001"},
		{i18n.PtBR, 123456, "Fatura #123456"},
		{i18n.En, 1, "Invoice #000001"},
		// Unknown langs resolve through the en catalog.
		{i18n.Lang("es"), 1234567, "Invoice #1234567"},
	}
	for _, tt := range tests {
		if got := formatInvoiceNumber(tt.lang, tt.number); got != tt.want {
			t.Errorf("formatInvoiceNumber(%q, %d) = %q, want %q", tt.lang, tt.number, got, tt.want)
		}
	}
}
