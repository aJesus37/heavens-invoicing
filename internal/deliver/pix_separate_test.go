package deliver_test

import (
	"context"
	"strings"
	"testing"

	"github.com/ajesus37/heavens-invoicing/internal/deliver"
	"github.com/ajesus37/heavens-invoicing/internal/model"
)

// TestPixAsSeparateMessage verifies the new Task 11 behavior: SendInvoice
// must NOT embed the PIX key in the document caption; instead after a
// successful SendDocument it must send a second message containing just the
// copyable key. This test is expected to FAIL before the fix.
func TestPixAsSeparateMessage(t *testing.T) {
	t.Run("whatsapp two-step pix", func(t *testing.T) {
		api := &fakeWhatsApp{}
		fallbackPix := "pix@fallback.com"
		d := deliver.NewWhatsApp(api, fallbackPix)
		phone := "+5511999999999"
		inv := testInvoice()
		// testInvoice has no pix key, so fallback will be used.
		if err := d.SendInvoice(context.Background(), waClient(&phone), inv, []byte("pdf")); err != nil {
			t.Fatalf("SendInvoice: %v", err)
		}
		if len(api.calls) != 2 {
			t.Fatalf("want 2 calls (SendDocument + SendMessage for pix), got %d: %+v", len(api.calls), api.calls)
		}
		doc := api.calls[0]
		if doc.method != "SendDocument" {
			t.Fatalf("first call method: want SendDocument, got %q", doc.method)
		}
		if strings.Contains(doc.caption, fallbackPix) {
			t.Fatalf("caption must NOT contain pix key, got %q", doc.caption)
		}
		if !strings.Contains(doc.caption, "Chave PIX:") && !strings.Contains(doc.caption, "PIX key:") {
			t.Fatalf("caption must contain pix label, got %q", doc.caption)
		}
		msg := api.calls[1]
		if msg.method != "SendMessage" {
			t.Fatalf("second call method: want SendMessage, got %q", msg.method)
		}
		// Second message should be copyable key: either raw key or via i18n.
		// Accept both raw key and wrapped forms, but must contain the key.
		if !strings.Contains(msg.text, fallbackPix) {
			t.Fatalf("second message must contain pix key %q, got %q", fallbackPix, msg.text)
		}
		// For pure copyable, the text should be exactly the key or with minimal label.
		// We at least ensure the key is present.
	})

	t.Run("whatsapp no pix no second message", func(t *testing.T) {
		api := &fakeWhatsApp{}
		d := deliver.NewWhatsApp(api, "")
		phone := "+5511999999999"
		inv := testInvoice()
		if err := d.SendInvoice(context.Background(), waClient(&phone), inv, []byte("pdf")); err != nil {
			t.Fatal(err)
		}
		if len(api.calls) != 1 {
			t.Fatalf("want 1 call when no pix, got %d", len(api.calls))
		}
		if api.calls[0].method != "SendDocument" {
			t.Fatalf("method: want SendDocument, got %q", api.calls[0].method)
		}
		if strings.Contains(api.calls[0].caption, "PIX") || strings.Contains(api.calls[0].caption, "Chave") {
			t.Fatalf("caption must not contain PIX when no key, got %q", api.calls[0].caption)
		}
	})

	t.Run("telegram two-step pix", func(t *testing.T) {
		api := &fakeTelegram{}
		fallbackPix := "pix@fallback.com"
		d := deliver.NewTelegram(api, fallbackPix)
		chatID := "12345"
		inv := testInvoice()
		if err := d.SendInvoice(context.Background(), tgClient(&chatID), inv, []byte("pdf")); err != nil {
			t.Fatalf("SendInvoice: %v", err)
		}
		if len(api.calls) != 2 {
			t.Fatalf("want 2 calls (SendDocument + SendMessage for pix), got %d: %+v", len(api.calls), api.calls)
		}
		doc := api.calls[0]
		if doc.method != "SendDocument" {
			t.Fatalf("first call method: want SendDocument, got %q", doc.method)
		}
		if strings.Contains(doc.caption, fallbackPix) {
			t.Fatalf("caption must NOT contain pix key, got %q", doc.caption)
		}
		if !strings.Contains(doc.caption, "Chave PIX:") && !strings.Contains(doc.caption, "PIX key:") {
			t.Fatalf("caption must contain pix label, got %q", doc.caption)
		}
		msg := api.calls[1]
		if msg.method != "SendMessage" {
			t.Fatalf("second call method: want SendMessage, got %q", msg.method)
		}
		if !strings.Contains(msg.text, fallbackPix) {
			t.Fatalf("second message must contain pix key %q, got %q", fallbackPix, msg.text)
		}
	})

	t.Run("telegram invoice pix precedence", func(t *testing.T) {
		api := &fakeTelegram{}
		d := deliver.NewTelegram(api, "pix@fallback.com")
		chatID := "12345"
		inv := testInvoice()
		pix := "pix@invoice.com"
		inv.PIXKey = &pix
		if err := d.SendInvoice(context.Background(), tgClient(&chatID), inv, []byte("pdf")); err != nil {
			t.Fatal(err)
		}
		if len(api.calls) != 2 {
			t.Fatalf("want 2 calls, got %d", len(api.calls))
		}
		if !strings.Contains(api.calls[1].text, pix) {
			t.Fatalf("second message must contain invoice pix %q, got %q", pix, api.calls[1].text)
		}
		if strings.Contains(api.calls[1].text, "pix@fallback.com") {
			t.Fatalf("fallback must not appear when invoice has its own key: %q", api.calls[1].text)
		}
		if strings.Contains(api.calls[0].caption, pix) {
			t.Fatalf("caption must not contain pix, got %q", api.calls[0].caption)
		}
	})
}

// TestPixSeparateDoesNotAffectReminder confirms reminders now also send pix as separate message.
func TestPixSeparateDoesNotAffectReminder(t *testing.T) {
	api := &fakeWhatsApp{}
	d := deliver.NewWhatsApp(api, "pix@fallback.com")
	phone := "+5511999999999"
	if err := d.SendReminder(context.Background(), waClient(&phone), testInvoice()); err != nil {
		t.Fatal(err)
	}
	if len(api.calls) != 2 {
		t.Fatalf("reminder: want 2 calls (text + pix), got %d", len(api.calls))
	}
	if !strings.Contains(api.calls[0].text, "Chave PIX:") && !strings.Contains(api.calls[0].text, "PIX key:") {
		t.Fatalf("reminder first message must contain pix label, got %q", api.calls[0].text)
	}
	if strings.Contains(api.calls[0].text, "pix@fallback.com") {
		t.Fatalf("reminder first message must not contain pix key, got %q", api.calls[0].text)
	}
	if !strings.Contains(api.calls[1].text, "pix@fallback.com") {
		t.Fatalf("reminder second message must contain pix key, got %q", api.calls[1].text)
	}
	// Ensure test helper exists for model
	_ = model.Client{}
}

func TestPixSeparateSecondMessageIsCopyable(t *testing.T) {
	// The second message must be exactly the raw key so mobile clients can
	// long-press → copy without trimming a label. The i18n entry
	// deliver.pix_message is "%s" for this reason.
	pix := "123e4567-e89b-12d3-a456-426614174000"
	for _, lang := range []string{"pt-BR", "en"} {
		t.Run(lang, func(t *testing.T) {
			api := &fakeWhatsApp{}
			d := deliver.NewWhatsApp(api, pix)
			phone := "+5511999999999"
			client := waClient(&phone)
			client.Language = lang
			if err := d.SendInvoice(context.Background(), client, testInvoice(), []byte("pdf")); err != nil {
				t.Fatal(err)
			}
			if len(api.calls) != 2 {
				t.Fatalf("want 2 calls, got %d", len(api.calls))
			}
			got := api.calls[1].text
			if got != pix {
				t.Fatalf("second message must be exactly the pix key for copyability, lang %q: want %q, got %q", lang, pix, got)
			}
			// Caption already tested elsewhere, but ensure no pix leakage even with i18n
			if strings.Contains(api.calls[0].caption, pix) {
				t.Fatalf("caption must not leak pix, got %q", api.calls[0].caption)
			}
		})
	}
	for _, lang := range []string{"pt-BR", "en"} {
		t.Run("telegram "+lang, func(t *testing.T) {
			api := &fakeTelegram{}
			d := deliver.NewTelegram(api, pix)
			chatID := "12345"
			client := tgClient(&chatID)
			client.Language = lang
			if err := d.SendInvoice(context.Background(), client, testInvoice(), []byte("pdf")); err != nil {
				t.Fatal(err)
			}
			if got := api.calls[1].text; got != pix {
				t.Fatalf("telegram second message must be exactly pix, lang %q: want %q, got %q", lang, pix, got)
			}
		})
	}
}

func TestPixSeparateWithBusinessName(t *testing.T) {
	api := &fakeWhatsApp{}
	biz := "Acme Corp"
	d := deliver.NewWhatsAppWithBusiness(api, "pix@fallback.com", biz)
	phone := "+5511999999999"
	inv := testInvoice()
	inv.Items = []*model.InvoiceItem{{Description: "Service", Quantity: 1, UnitPrice: 100, Total: 100}}
	inv.Total = 100
	if err := d.SendInvoice(context.Background(), waClient(&phone), inv, []byte("pdf")); err != nil {
		t.Fatal(err)
	}
	if len(api.calls) != 2 {
		t.Fatalf("want 2 calls, got %d", len(api.calls))
	}
	assertContains(t, api.calls[0].caption, biz)
	assertContains(t, api.calls[0].caption, "Service")
	if strings.Contains(api.calls[0].caption, "pix@fallback.com") {
		t.Fatalf("caption must not contain pix even with business name: %q", api.calls[0].caption)
	}
	if api.calls[1].text != "pix@fallback.com" {
		t.Fatalf("pix message: want %q, got %q", "pix@fallback.com", api.calls[1].text)
	}
}

func TestPixSeparateDocumentErrorDoesNotSendPix(t *testing.T) {
	// If SendDocument fails, the second SendMessage must not be attempted and the error returned.
	wantErr := context.DeadlineExceeded
	api := &fakeWhatsAppWithErrors{docErr: wantErr}
	d := deliver.NewWhatsApp(api, "pix@fallback.com")
	phone := "+5511999999999"
	err := d.SendInvoice(context.Background(), waClient(&phone), testInvoice(), []byte("pdf"))
	if err == nil || err != wantErr {
		t.Fatalf("want document error %v, got %v", wantErr, err)
	}
	if len(api.calls) != 1 || api.calls[0].method != "SendDocument" {
		t.Fatalf("only document should be attempted, got %+v", api.calls)
	}
	// Telegram variant
	apiTg := &fakeTelegramWithErrors{docErr: wantErr}
	dTg := deliver.NewTelegram(apiTg, "pix@fallback.com")
	chatID := "12345"
	err = dTg.SendInvoice(context.Background(), tgClient(&chatID), testInvoice(), []byte("pdf"))
	if err == nil || err != wantErr {
		t.Fatalf("telegram: want document error, got %v", err)
	}
	if len(apiTg.calls) != 1 {
		t.Fatalf("telegram: only document, got %d", len(apiTg.calls))
	}
}

func TestPixSeparateSecondMessageErrorPropagates(t *testing.T) {
	// Document succeeds but pix message fails → error must be returned.
	wantErr := context.Canceled
	api := &fakeWhatsAppWithErrors{msgErr: wantErr}
	d := deliver.NewWhatsApp(api, "pix@fallback.com")
	phone := "+5511999999999"
	err := d.SendInvoice(context.Background(), waClient(&phone), testInvoice(), []byte("pdf"))
	if err == nil || err != wantErr {
		t.Fatalf("want pix message error %v, got %v", wantErr, err)
	}
	if len(api.calls) != 2 {
		t.Fatalf("want 2 calls (doc ok + pix failed), got %d", len(api.calls))
	}
	// Telegram
	apiTg := &fakeTelegramWithErrors{msgErr: wantErr}
	dTg := deliver.NewTelegram(apiTg, "pix@fallback.com")
	chatID := "12345"
	err = dTg.SendInvoice(context.Background(), tgClient(&chatID), testInvoice(), []byte("pdf"))
	if err == nil || err != wantErr {
		t.Fatalf("telegram: want pix error, got %v", err)
	}
	if len(apiTg.calls) != 2 {
		t.Fatalf("telegram: want 2 calls, got %d", len(apiTg.calls))
	}
}

// Helpers for error injection on second message vs document.
type fakeWhatsAppWithErrors struct {
	calls  []waCall
	docErr error
	msgErr error
}

func (f *fakeWhatsAppWithErrors) SendMessage(_ context.Context, jid, text string) error {
	f.calls = append(f.calls, waCall{method: "SendMessage", jid: jid, text: text})
	return f.msgErr
}
func (f *fakeWhatsAppWithErrors) SendDocument(_ context.Context, jid, filename string, data []byte, caption string) error {
	f.calls = append(f.calls, waCall{method: "SendDocument", jid: jid, filename: filename, data: data, caption: caption})
	return f.docErr
}

type fakeTelegramWithErrors struct {
	calls  []tgCall
	docErr error
	msgErr error
}

func (f *fakeTelegramWithErrors) SendMessage(_ context.Context, chatID, text string) error {
	f.calls = append(f.calls, tgCall{method: "SendMessage", chatID: chatID, text: text})
	return f.msgErr
}
func (f *fakeTelegramWithErrors) SendDocument(_ context.Context, chatID, filename string, content []byte, caption string) error {
	f.calls = append(f.calls, tgCall{method: "SendDocument", chatID: chatID, filename: filename, content: content, caption: caption})
	return f.docErr
}
