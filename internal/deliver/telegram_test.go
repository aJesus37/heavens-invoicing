package deliver_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/jesus/invoice-app/internal/deliver"
	"github.com/jesus/invoice-app/internal/model"
	"github.com/jesus/invoice-app/internal/telegram"
)

var _ deliver.TelegramAPI = (*telegram.Client)(nil)

type tgCall struct {
	method   string // "SendMessage" | "SendDocument"
	chatID   string
	text     string
	filename string
	content  []byte
	caption  string
}

type fakeTelegram struct {
	calls []tgCall
	err   error
}

func (f *fakeTelegram) SendMessage(_ context.Context, chatID, text string) error {
	f.calls = append(f.calls, tgCall{method: "SendMessage", chatID: chatID, text: text})
	return f.err
}

func (f *fakeTelegram) SendDocument(_ context.Context, chatID, filename string, content []byte, caption string) error {
	f.calls = append(f.calls, tgCall{method: "SendDocument", chatID: chatID, filename: filename, content: content, caption: caption})
	return f.err
}

func tgClient(chatID *string) model.Client {
	return model.Client{Name: "Acme Ltda", TelegramChatID: chatID}
}

func TestTelegramName(t *testing.T) {
	if got := deliver.NewTelegram(&fakeTelegram{}, "").Name(); got != "telegram" {
		t.Fatalf("want %q, got %q", "telegram", got)
	}
}

func TestTelegramSendInvoice(t *testing.T) {
	api := &fakeTelegram{}
	d := deliver.NewTelegram(api, "pix@fallback.com")
	chatID := "12345"
	pdf := []byte("%PDF-fake")

	if err := d.SendInvoice(context.Background(), tgClient(&chatID), testInvoice(), pdf); err != nil {
		t.Fatal(err)
	}

	if len(api.calls) != 1 {
		t.Fatalf("want 1 call, got %d", len(api.calls))
	}
	call := api.calls[0]

	if call.method != "SendDocument" {
		t.Fatalf("method: want SendDocument, got %q", call.method)
	}
	if call.chatID != "12345" {
		t.Fatalf("chat ID: want %q, got %q", "12345", call.chatID)
	}
	if call.filename != "fatura-000001.pdf" {
		t.Fatalf("filename: want %q, got %q", "fatura-000001.pdf", call.filename)
	}
	if string(call.content) != string(pdf) {
		t.Fatal("document bytes differ from PDF passed in")
	}
	for _, want := range []string{"Fatura #000001", "Acme Ltda"} {
		assertContains(t, call.caption, want)
	}
}

func TestTelegramSendReminder(t *testing.T) {
	api := &fakeTelegram{}
	d := deliver.NewTelegram(api, "")
	chatID := "12345"

	if err := d.SendReminder(context.Background(), tgClient(&chatID), testInvoice()); err != nil {
		t.Fatal(err)
	}

	if len(api.calls) != 1 {
		t.Fatalf("want 1 call, got %d", len(api.calls))
	}
	call := api.calls[0]

	if call.method != "SendMessage" {
		t.Fatalf("method: want SendMessage, got %q", call.method)
	}
	if call.chatID != "12345" {
		t.Fatalf("chat ID: want %q, got %q", "12345", call.chatID)
	}
	for _, want := range []string{"000001", "05/09/2026"} {
		if !strings.Contains(call.text, want) {
			t.Fatalf("reminder text %q does not contain %q", call.text, want)
		}
	}
}

// TestTelegramLocalizesPerClientLanguage pins caption/reminder wording to
// the client's stored language; unknown values keep pt-BR.
func TestTelegramLocalizesPerClientLanguage(t *testing.T) {
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
			language:       "",
			captionMarker:  "Fatura #000001 para Acme Ltda",
			reminderMarker: "Lembrete: a fatura #000001, com vencimento em 05/09/2026, ainda está pendente.",
			notWant:        []string{"Invoice"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			api := &fakeTelegram{}
			d := deliver.NewTelegram(api, "")
			client := tgClient(strPtr("12345"))
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

func TestTelegramPIXKeyPrecedence(t *testing.T) {
	invoicePix := "pix@invoice.com"
	fallbackPix := "pix@fallback.com"

	invoiceWithKey := func() model.Invoice {
		inv := testInvoice()
		inv.PIXKey = strPtr(invoicePix)
		return inv
	}

	t.Run("caption uses invoice key over fallback", func(t *testing.T) {
		api := &fakeTelegram{}
		d := deliver.NewTelegram(api, fallbackPix)
		chatID := "12345"

		if err := d.SendInvoice(context.Background(), tgClient(&chatID), invoiceWithKey(), []byte("pdf")); err != nil {
			t.Fatal(err)
		}
		assertContains(t, api.calls[0].caption, "Chave PIX: "+invoicePix)
		if strings.Contains(api.calls[0].caption, fallbackPix) {
			t.Fatalf("fallback must not appear when invoice has its own key: %q", api.calls[0].caption)
		}
	})

	t.Run("caption falls back when invoice has none", func(t *testing.T) {
		api := &fakeTelegram{}
		d := deliver.NewTelegram(api, fallbackPix)
		chatID := "12345"

		if err := d.SendInvoice(context.Background(), tgClient(&chatID), testInvoice(), []byte("pdf")); err != nil {
			t.Fatal(err)
		}
		assertContains(t, api.calls[0].caption, "Chave PIX: "+fallbackPix)
	})

	t.Run("reminder uses invoice key over fallback", func(t *testing.T) {
		api := &fakeTelegram{}
		d := deliver.NewTelegram(api, fallbackPix)
		chatID := "12345"

		if err := d.SendReminder(context.Background(), tgClient(&chatID), invoiceWithKey()); err != nil {
			t.Fatal(err)
		}
		assertContains(t, api.calls[0].text, "Chave PIX: "+invoicePix)
		if strings.Contains(api.calls[0].text, fallbackPix) {
			t.Fatalf("fallback must not appear when invoice has its own key: %q", api.calls[0].text)
		}
	})

	t.Run("omitted entirely when no key anywhere", func(t *testing.T) {
		api := &fakeTelegram{}
		d := deliver.NewTelegram(api, "")
		chatID := "12345"

		if err := d.SendInvoice(context.Background(), tgClient(&chatID), testInvoice(), []byte("pdf")); err != nil {
			t.Fatal(err)
		}
		if strings.Contains(api.calls[0].caption, "Chave PIX") {
			t.Fatalf("empty key must omit the line, got %q", api.calls[0].caption)
		}

		if err := d.SendReminder(context.Background(), tgClient(&chatID), testInvoice()); err != nil {
			t.Fatal(err)
		}
		if strings.Contains(api.calls[1].text, "Chave PIX") {
			t.Fatalf("empty key must omit the line, got %q", api.calls[1].text)
		}
	})
}

func TestTelegramMissingChatID(t *testing.T) {
	ctx := context.Background()
	pdf := []byte("pdf")

	for _, tc := range []struct {
		name   string
		chatID *string
	}{
		{"nil", nil},
		{"empty", strPtr("")},
		{"blank spaces", strPtr("   ")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			api := &fakeTelegram{}
			d := deliver.NewTelegram(api, "")
			client := tgClient(tc.chatID)

			if err := d.SendInvoice(ctx, client, testInvoice(), pdf); err == nil {
				t.Error("SendInvoice: want error for missing chat ID, got nil")
			}
			if err := d.SendReminder(ctx, client, testInvoice()); err == nil {
				t.Error("SendReminder: want error for missing chat ID, got nil")
			}
			if len(api.calls) != 0 {
				t.Fatalf("no send expected, got %d calls", len(api.calls))
			}
		})
	}
}

func TestTelegramChatIDTrimmed(t *testing.T) {
	api := &fakeTelegram{}
	d := deliver.NewTelegram(api, "")
	chatID := " 12345 "

	if err := d.SendReminder(context.Background(), tgClient(&chatID), testInvoice()); err != nil {
		t.Fatal(err)
	}
	if got := api.calls[0].chatID; got != "12345" {
		t.Fatalf("chat ID: want trimmed %q, got %q", "12345", got)
	}
}

func TestTelegramAPIErrorPropagates(t *testing.T) {
	wantErr := errors.New("telegram down")
	api := &fakeTelegram{err: wantErr}
	d := deliver.NewTelegram(api, "")
	chatID := "12345"

	if err := d.SendInvoice(context.Background(), tgClient(&chatID), testInvoice(), []byte("pdf")); !errors.Is(err, wantErr) {
		t.Fatalf("want API error, got %v", err)
	}
	if err := d.SendReminder(context.Background(), tgClient(&chatID), testInvoice()); !errors.Is(err, wantErr) {
		t.Fatalf("want API error, got %v", err)
	}
}
