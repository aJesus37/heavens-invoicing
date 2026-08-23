package deliver_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/ajesus37/heavens-invoicing/internal/deliver"
	"github.com/ajesus37/heavens-invoicing/internal/model"
	"github.com/ajesus37/heavens-invoicing/internal/whatsapp"
)

var _ deliver.WhatsAppAPI = (*whatsapp.Session)(nil)

type waCall struct {
	method   string // "SendMessage" | "SendDocument"
	jid      string
	text     string
	filename string
	data     []byte
	caption  string
}

type fakeWhatsApp struct {
	calls []waCall
	err   error
}

func (f *fakeWhatsApp) SendMessage(_ context.Context, jid, text string) error {
	f.calls = append(f.calls, waCall{method: "SendMessage", jid: jid, text: text})
	return f.err
}

func (f *fakeWhatsApp) SendDocument(_ context.Context, jid, filename string, data []byte, caption string) error {
	f.calls = append(f.calls, waCall{method: "SendDocument", jid: jid, filename: filename, data: data, caption: caption})
	return f.err
}

func waClient(phone *string) model.Client {
	return model.Client{Name: "Acme Ltda", Phone: phone}
}

func TestWhatsAppName(t *testing.T) {
	if got := deliver.NewWhatsApp(&fakeWhatsApp{}, "").Name(); got != "whatsapp" {
		t.Fatalf("want %q, got %q", "whatsapp", got)
	}
}

func TestWhatsAppSendInvoice(t *testing.T) {
	api := &fakeWhatsApp{}
	d := deliver.NewWhatsApp(api, "pix@fallback.com")
	phone := "+55 11 99999-9999"
	pdf := []byte("%PDF-fake")

	if err := d.SendInvoice(context.Background(), waClient(&phone), testInvoice(), pdf); err != nil {
		t.Fatal(err)
	}

	if len(api.calls) != 2 {
		t.Fatalf("want 2 calls (document + pix message), got %d", len(api.calls))
	}
	call := api.calls[0]

	if call.method != "SendDocument" {
		t.Fatalf("method: want SendDocument, got %q", call.method)
	}
	if call.jid != "5511999999999@s.whatsapp.net" {
		t.Fatalf("JID: want %q, got %q", "5511999999999@s.whatsapp.net", call.jid)
	}
	if call.filename != "fatura-000001.pdf" {
		t.Fatalf("filename: want %q, got %q", "fatura-000001.pdf", call.filename)
	}
	if string(call.data) != string(pdf) {
		t.Fatal("document bytes differ from PDF passed in")
	}
	for _, want := range []string{"Fatura #000001", "Acme Ltda"} {
		assertContains(t, call.caption, want)
	}
	// Caption must contain pix label but NOT the key; key is sent as second copyable message.
	if !strings.Contains(call.caption, "Chave PIX:") {
		t.Fatalf("caption must contain pix label, got %q", call.caption)
	}
	if strings.Contains(call.caption, "pix@fallback.com") {
		t.Fatalf("caption must not contain pix key, got %q", call.caption)
	}
	pixMsg := api.calls[1]
	if pixMsg.method != "SendMessage" {
		t.Fatalf("second method: want SendMessage, got %q", pixMsg.method)
	}
	if pixMsg.jid != "5511999999999@s.whatsapp.net" {
		t.Fatalf("pix JID: want %q, got %q", "5511999999999@s.whatsapp.net", pixMsg.jid)
	}
	assertContains(t, pixMsg.text, "pix@fallback.com")
}

func TestWhatsAppSendReminder(t *testing.T) {
	api := &fakeWhatsApp{}
	d := deliver.NewWhatsApp(api, "")
	phone := "+5511999999999"

	if err := d.SendReminder(context.Background(), waClient(&phone), testInvoice()); err != nil {
		t.Fatal(err)
	}

	if len(api.calls) != 1 {
		t.Fatalf("want 1 call, got %d", len(api.calls))
	}
	call := api.calls[0]

	if call.method != "SendMessage" {
		t.Fatalf("method: want SendMessage, got %q", call.method)
	}
	if call.jid != "5511999999999@s.whatsapp.net" {
		t.Fatalf("JID: want %q, got %q", "5511999999999@s.whatsapp.net", call.jid)
	}
	for _, want := range []string{"000001", "05/09/2026"} {
		if !strings.Contains(call.text, want) {
			t.Fatalf("reminder text %q does not contain %q", call.text, want)
		}
	}
}

// TestWhatsAppLocalizesPerClientLanguage pins caption/reminder wording to
// the client's stored language; unknown values keep pt-BR.
func TestWhatsAppLocalizesPerClientLanguage(t *testing.T) {
	tests := []struct {
		name           string
		language       string
		captionMarker  string
		reminderMarker string
		notWant        []string
	}{
		{
			name:           "pt-BR client",
			language:       "pt-BR",
			captionMarker:  "Fatura #000001 para Acme Ltda",
			reminderMarker: "Lembrete: a fatura #000001, com vencimento em 05/09/2026, ainda está pendente.",
			notWant:        []string{"Invoice", "Reminder"},
		},
		{
			name:           "en client",
			language:       "en",
			captionMarker:  "Invoice #000001 for Acme Ltda",
			reminderMarker: "Reminder: invoice #000001, due on 05/09/2026, is still pending.",
			notWant:        []string{"Fatura", "Lembrete"},
		},
		{
			name:           "unknown language falls back to pt-BR",
			language:       "xx-XX",
			captionMarker:  "Fatura #000001 para Acme Ltda",
			reminderMarker: "Lembrete: a fatura #000001, com vencimento em 05/09/2026, ainda está pendente.",
			notWant:        []string{"Invoice"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			api := &fakeWhatsApp{}
			d := deliver.NewWhatsApp(api, "")
			client := waClient(strPtr("+5511999999999"))
			client.Language = tt.language

			if err := d.SendInvoice(ctx, client, testInvoice(), []byte("pdf")); err != nil {
				t.Fatal(err)
			}
			assertContains(t, api.calls[0].caption, tt.captionMarker)

			if err := d.SendReminder(ctx, client, testInvoice()); err != nil {
				t.Fatal(err)
			}
			assertContains(t, api.calls[1].text, tt.reminderMarker)

			for _, m := range tt.notWant {
				if strings.Contains(api.calls[0].caption+api.calls[1].text, m) {
					t.Errorf("language %q: message unexpectedly contains %q", tt.language, m)
				}
			}
		})
	}
}

func TestWhatsAppPIXKeyPrecedence(t *testing.T) {
	invoicePix := "pix@invoice.com"
	fallbackPix := "pix@fallback.com"

	invoiceWithKey := func() model.Invoice {
		inv := testInvoice()
		inv.PIXKey = strPtr(invoicePix)
		return inv
	}
	phone := "+5511999999999"

	t.Run("invoice key over fallback", func(t *testing.T) {
		api := &fakeWhatsApp{}
		d := deliver.NewWhatsApp(api, fallbackPix)

		if err := d.SendInvoice(context.Background(), waClient(&phone), invoiceWithKey(), []byte("pdf")); err != nil {
			t.Fatal(err)
		}
		if len(api.calls) != 2 {
			t.Fatalf("SendInvoice: want 2 calls (document + pix), got %d", len(api.calls))
		}
		if strings.Contains(api.calls[0].caption, invoicePix) {
			t.Fatalf("caption must not contain pix key, got %q", api.calls[0].caption)
		}
		if !strings.Contains(api.calls[0].caption, "Chave PIX:") {
			t.Fatalf("caption must contain pix label, got %q", api.calls[0].caption)
		}
		if strings.Contains(api.calls[0].caption, fallbackPix) && fallbackPix != invoicePix {
			t.Fatalf("fallback key must not appear in caption: %q", api.calls[0].caption)
		}
		assertContains(t, api.calls[1].text, invoicePix)
		if strings.Contains(api.calls[1].text, fallbackPix) && fallbackPix != invoicePix {
			t.Fatalf("fallback must not appear when invoice has its own key: %q", api.calls[1].text)
		}
		if api.calls[1].method != "SendMessage" {
			t.Fatalf("second call must be SendMessage, got %q", api.calls[1].method)
		}

		if err := d.SendReminder(context.Background(), waClient(&phone), invoiceWithKey()); err != nil {
			t.Fatal(err)
		}
		if len(api.calls) != 4 {
			t.Fatalf("after reminder: want 4 calls (invoice 2 + reminder 2), got %d", len(api.calls))
		}
		assertContains(t, api.calls[2].text, "Chave PIX:")
		assertContains(t, api.calls[3].text, invoicePix)
	})

	t.Run("falls back when invoice has none", func(t *testing.T) {
		api := &fakeWhatsApp{}
		d := deliver.NewWhatsApp(api, fallbackPix)

		if err := d.SendInvoice(context.Background(), waClient(&phone), testInvoice(), []byte("pdf")); err != nil {
			t.Fatal(err)
		}
		if len(api.calls) != 2 {
			t.Fatalf("SendInvoice: want 2 calls, got %d", len(api.calls))
		}
		if strings.Contains(api.calls[0].caption, fallbackPix) {
			t.Fatalf("caption must not contain pix key, got %q", api.calls[0].caption)
		}
		if !strings.Contains(api.calls[0].caption, "Chave PIX:") {
			t.Fatalf("caption must contain pix label, got %q", api.calls[0].caption)
		}
		assertContains(t, api.calls[1].text, fallbackPix)
		if api.calls[1].method != "SendMessage" {
			t.Fatalf("second call must be SendMessage, got %q", api.calls[1].method)
		}

		if err := d.SendReminder(context.Background(), waClient(&phone), testInvoice()); err != nil {
			t.Fatal(err)
		}
		if len(api.calls) != 4 {
			t.Fatalf("after reminder: want 4 calls (invoice 2 + reminder 2), got %d", len(api.calls))
		}
		assertContains(t, api.calls[2].text, "Chave PIX:")
		assertContains(t, api.calls[3].text, fallbackPix)
	})

	t.Run("omitted entirely when no key anywhere", func(t *testing.T) {
		api := &fakeWhatsApp{}
		d := deliver.NewWhatsApp(api, "")

		if err := d.SendInvoice(context.Background(), waClient(&phone), testInvoice(), []byte("pdf")); err != nil {
			t.Fatal(err)
		}
		if len(api.calls) != 1 {
			t.Fatalf("want 1 call when no pix, got %d", len(api.calls))
		}
		if strings.Contains(api.calls[0].caption, "Chave PIX") {
			t.Fatalf("empty key must omit the line, got %q", api.calls[0].caption)
		}

		if err := d.SendReminder(context.Background(), waClient(&phone), testInvoice()); err != nil {
			t.Fatal(err)
		}
		if len(api.calls) != 2 {
			t.Fatalf("after reminder: want 2 calls, got %d", len(api.calls))
		}
		if strings.Contains(api.calls[1].text, "Chave PIX") {
			t.Fatalf("empty key must omit the line, got %q", api.calls[1].text)
		}
	})
}

func TestWhatsAppMissingPhone(t *testing.T) {
	ctx := context.Background()
	pdf := []byte("pdf")

	for _, tc := range []struct {
		name  string
		phone *string
	}{
		{"nil", nil},
		{"empty", strPtr("")},
		{"blank spaces", strPtr("   ")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			api := &fakeWhatsApp{}
			d := deliver.NewWhatsApp(api, "")
			client := waClient(tc.phone)

			errInv := d.SendInvoice(ctx, client, testInvoice(), pdf)
			if errInv == nil || !strings.Contains(errInv.Error(), "no WhatsApp phone") {
				t.Errorf("SendInvoice: want descriptive missing-phone error, got %v", errInv)
			}
			errRem := d.SendReminder(ctx, client, testInvoice())
			if errRem == nil || !strings.Contains(errRem.Error(), "no WhatsApp phone") {
				t.Errorf("SendReminder: want descriptive missing-phone error, got %v", errRem)
			}
			if len(api.calls) != 0 {
				t.Fatalf("no send expected, got %d calls", len(api.calls))
			}
		})
	}
}

func TestWhatsAppInvalidPhone(t *testing.T) {
	raw := "+55 11 abc"
	api := &fakeWhatsApp{}
	d := deliver.NewWhatsApp(api, "")
	client := waClient(&raw)

	err := d.SendInvoice(context.Background(), client, testInvoice(), []byte("pdf"))
	if err == nil || !strings.Contains(err.Error(), raw) {
		t.Fatalf("want error mentioning raw input %q, got %v", raw, err)
	}
	if len(api.calls) != 0 {
		t.Fatalf("no send expected, got %d calls", len(api.calls))
	}
}

func TestWhatsAppAPIErrorPropagates(t *testing.T) {
	wantErr := errors.New("whatsapp down")
	api := &fakeWhatsApp{err: wantErr}
	d := deliver.NewWhatsApp(api, "")
	phone := "+5511999999999"

	if err := d.SendInvoice(context.Background(), waClient(&phone), testInvoice(), []byte("pdf")); !errors.Is(err, wantErr) {
		t.Fatalf("want API error, got %v", err)
	}
	if err := d.SendReminder(context.Background(), waClient(&phone), testInvoice()); !errors.Is(err, wantErr) {
		t.Fatalf("want API error, got %v", err)
	}
}
