package deliver_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/jesus/invoice-app/internal/deliver"
	"github.com/jesus/invoice-app/internal/model"
)

func strPtr(s string) *string { return &s }

type fakeCall struct {
	from        string
	to          []string
	subject     string
	body        string
	attachments map[string][]byte
}

type fakeSender struct {
	calls []fakeCall
	err   error
}

func (f *fakeSender) Send(from string, to []string, subject, body string, attachments map[string][]byte) error {
	f.calls = append(f.calls, fakeCall{from: from, to: to, subject: subject, body: body, attachments: attachments})
	return f.err
}

func testClient(email *string) model.Client {
	return model.Client{Name: "Acme Ltda", Email: email}
}

func testInvoice() model.Invoice {
	return model.Invoice{
		Number:    1,
		IssueDate: mustDate("2026-08-01"),
		DueDate:   mustDate("2026-09-05"),
	}
}

func mustDate(s string) time.Time {
	t, err := time.Parse("2006-01-02", s)
	if err != nil {
		panic(err)
	}
	return t
}

func TestEmailName(t *testing.T) {
	if got := deliver.NewEmail(&fakeSender{}, "from@me.com", "").Name(); got != "email" {
		t.Fatalf("want %q, got %q", "email", got)
	}
}

func TestEmailSendInvoice(t *testing.T) {
	sender := &fakeSender{}
	d := deliver.NewEmail(sender, "billing@me.com", "")
	email := "to@acme.com"
	pdf := []byte("%PDF-fake")

	err := d.SendInvoice(context.Background(), testClient(&email), testInvoice(), pdf)
	if err != nil {
		t.Fatal(err)
	}

	if len(sender.calls) != 1 {
		t.Fatalf("want 1 call, got %d", len(sender.calls))
	}
	call := sender.calls[0]

	if call.from != "billing@me.com" {
		t.Fatalf("from: want %q, got %q", "billing@me.com", call.from)
	}
	if len(call.to) != 1 || call.to[0] != "to@acme.com" {
		t.Fatalf("to: want [to@acme.com], got %v", call.to)
	}
	if call.subject != "Fatura #000001" {
		t.Fatalf("subject: want %q, got %q", "Fatura #000001", call.subject)
	}

	data, ok := call.attachments["fatura-000001.pdf"]
	if !ok {
		t.Fatalf("attachment fatura-000001.pdf missing; got keys %v", keysOf(call.attachments))
	}
	if string(data) != string(pdf) {
		t.Fatal("attachment bytes differ")
	}
}

func TestEmailSendInvoicePlaceholders(t *testing.T) {
	sender := &fakeSender{}
	d := deliver.NewEmail(sender, "billing@me.com", "")
	email := "to@acme.com"

	if err := d.SendInvoice(context.Background(), testClient(&email), testInvoice(), []byte("pdf")); err != nil {
		t.Fatal(err)
	}
	body := sender.calls[0].body

	for _, want := range []string{"Acme Ltda", "000001"} {
		if !strings.Contains(body, want) {
			t.Fatalf("body %q does not contain %q", body, want)
		}
	}
	if strings.Contains(body, "{{") {
		t.Fatalf("unreplaced placeholder in body: %q", body)
	}
}

func TestEmailPIXKeyPrecedence(t *testing.T) {
	email := "to@acme.com"
	invoicePix := "pix@invoice.com"
	fallbackPix := "pix@sender.com"

	t.Run("invoice key wins over fallback", func(t *testing.T) {
		sender := &fakeSender{}
		d := deliver.NewEmail(sender, "billing@me.com", fallbackPix)
		inv := testInvoice()
		inv.PIXKey = &invoicePix

		if err := d.SendInvoice(context.Background(), testClient(&email), inv, []byte("pdf")); err != nil {
			t.Fatal(err)
		}
		body := sender.calls[0].body
		assertContains(t, body, invoicePix)
		if strings.Contains(body, fallbackPix) {
			t.Fatalf("fallback must not appear when invoice has its own key: %q", body)
		}
	})

	t.Run("fallback used when invoice has none", func(t *testing.T) {
		sender := &fakeSender{}
		d := deliver.NewEmail(sender, "billing@me.com", fallbackPix)

		if err := d.SendReminder(context.Background(), testClient(&email), testInvoice()); err != nil {
			t.Fatal(err)
		}
		body := sender.calls[0].body
		assertContains(t, body, fallbackPix)
		assertContains(t, body, "000001")
	})

	t.Run("omitted entirely when no key anywhere", func(t *testing.T) {
		sender := &fakeSender{}
		d := deliver.NewEmail(sender, "billing@me.com", "")

		if err := d.SendInvoice(context.Background(), testClient(&email), testInvoice(), []byte("pdf")); err != nil {
			t.Fatal(err)
		}
		body := sender.calls[0].body
		if strings.Contains(body, "PIX") || strings.Contains(body, "{{") {
			t.Fatalf("empty PIX key must omit the section, got %q", body)
		}

		sender.calls = nil
		if err := d.SendReminder(context.Background(), testClient(&email), testInvoice()); err != nil {
			t.Fatal(err)
		}
		body = sender.calls[0].body
		if strings.Contains(body, "PIX") || strings.Contains(body, "{{") {
			t.Fatalf("empty PIX key must omit the section, got %q", body)
		}
	})

	t.Run("empty-string invoice key treated as unset", func(t *testing.T) {
		sender := &fakeSender{}
		d := deliver.NewEmail(sender, "billing@me.com", fallbackPix)
		inv := testInvoice()
		inv.PIXKey = strPtr("")

		if err := d.SendInvoice(context.Background(), testClient(&email), inv, []byte("pdf")); err != nil {
			t.Fatal(err)
		}
		assertContains(t, sender.calls[0].body, fallbackPix)
	})
}

func TestEmailSendReminder(t *testing.T) {
	sender := &fakeSender{}
	d := deliver.NewEmail(sender, "billing@me.com", "")
	email := "to@acme.com"

	if err := d.SendReminder(context.Background(), testClient(&email), testInvoice()); err != nil {
		t.Fatal(err)
	}

	if len(sender.calls) != 1 {
		t.Fatalf("want 1 call, got %d", len(sender.calls))
	}
	call := sender.calls[0]
	if len(call.to) != 1 || call.to[0] != "to@acme.com" {
		t.Fatalf("to: want [to@acme.com], got %v", call.to)
	}

	for _, want := range []string{"000001", "05/09/2026"} {
		if !strings.Contains(call.body, want) {
			t.Fatalf("reminder body %q does not contain %q", call.body, want)
		}
	}
	if len(call.attachments) != 0 {
		t.Fatalf("reminder should have no attachments, got %v", keysOf(call.attachments))
	}
}

// TestEmailLocalizesPerClientLanguage pins subject/body selection by the
// client's stored language; an unknown value keeps pt-BR.
func TestEmailLocalizesPerClientLanguage(t *testing.T) {
	tests := []struct {
		name            string
		language        string
		invoiceSubject  string
		reminderSubject string
		bodyMarker      string
		notWant         []string // other locale's unique markers
	}{
		{
			name:            "pt-BR client",
			language:        "pt-BR",
			invoiceSubject:  "Fatura #000001",
			reminderSubject: "Lembrete de vencimento - Fatura #000001",
			bodyMarker:      "Olá Acme Ltda",
			notWant:         []string{"Hello Acme", "Payment reminder"},
		},
		{
			name:            "en client",
			language:        "en",
			invoiceSubject:  "Invoice #000001",
			reminderSubject: "Payment reminder - Invoice #000001",
			bodyMarker:      "Hello Acme Ltda",
			notWant:         []string{"Olá Acme", "Lembrete"},
		},
		{
			name:            "unknown language falls back to pt-BR",
			language:        "klingon",
			invoiceSubject:  "Fatura #000001",
			reminderSubject: "Lembrete de vencimento - Fatura #000001",
			bodyMarker:      "Olá Acme Ltda",
			notWant:         []string{"Hello Acme"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			sender := &fakeSender{}
			d := deliver.NewEmail(sender, "billing@me.com", "")
			client := testClient(strPtr("to@acme.com"))
			client.Language = tt.language

			if err := d.SendInvoice(ctx, client, testInvoice(), []byte("pdf")); err != nil {
				t.Fatal(err)
			}
			call := sender.calls[0]
			if call.subject != tt.invoiceSubject {
				t.Errorf("invoice subject = %q, want %q", call.subject, tt.invoiceSubject)
			}
			assertContains(t, call.body, tt.bodyMarker)
			for _, m := range tt.notWant {
				if strings.Contains(call.body+call.subject, m) {
					t.Errorf("language %q: invoice unexpectedly contains %q", tt.language, m)
				}
			}

			if err := d.SendReminder(ctx, client, testInvoice()); err != nil {
				t.Fatal(err)
			}
			call = sender.calls[1]
			if call.subject != tt.reminderSubject {
				t.Errorf("reminder subject = %q, want %q", call.subject, tt.reminderSubject)
			}
			for _, want := range []string{tt.bodyMarker, "05/09/2026"} {
				assertContains(t, call.body, want)
			}
		})
	}
}

func TestEmailMissingRecipient(t *testing.T) {
	ctx := context.Background()
	pdf := []byte("pdf")

	for _, tc := range []struct {
		name  string
		email *string
	}{
		{"nil", nil},
		{"empty", strPtr("")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			sender := &fakeSender{}
			d := deliver.NewEmail(sender, "billing@me.com", "")
			client := testClient(tc.email)

			if err := d.SendInvoice(ctx, client, testInvoice(), pdf); err == nil {
				t.Error("SendInvoice: want error for missing email, got nil")
			}
			if err := d.SendReminder(ctx, client, testInvoice()); err == nil {
				t.Error("SendReminder: want error for missing email, got nil")
			}
			if len(sender.calls) != 0 {
				t.Fatalf("no send expected, got %d calls", len(sender.calls))
			}
		})
	}
}

func TestEmailSenderErrorPropagates(t *testing.T) {
	wantErr := errors.New("smtp down")
	sender := &fakeSender{err: wantErr}
	d := deliver.NewEmail(sender, "billing@me.com", "")
	email := "to@acme.com"

	if err := d.SendInvoice(context.Background(), testClient(&email), testInvoice(), []byte("pdf")); !errors.Is(err, wantErr) {
		t.Fatalf("want smtp error, got %v", err)
	}
	if err := d.SendReminder(context.Background(), testClient(&email), testInvoice()); !errors.Is(err, wantErr) {
		t.Fatalf("want smtp error, got %v", err)
	}
}

func TestEmailHeaderInjectionRejected(t *testing.T) {
	ctx := context.Background()

	for _, tc := range []struct {
		name        string
		from        string
		clientEmail *string
	}{
		{"recipient with CRLF", "billing@me.com", strPtr("a@b.com\r\nBcc: x@evil.com")},
		{"recipient with LF", "billing@me.com", strPtr("a@b.com\nBcc: x@evil.com")},
		{"recipient with NUL", "billing@me.com", strPtr("bad\x00@example.com")},
		{"unparseable recipient", "billing@me.com", strPtr("not-an-email")},
		{"sender with CRLF", "evil@x.com\r\nBcc: y@evil.com", strPtr("to@acme.com")},
		{"empty sender", "", strPtr("to@acme.com")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			sender := &fakeSender{}
			d := deliver.NewEmail(sender, tc.from, "")
			client := testClient(tc.clientEmail)

			err := d.SendInvoice(ctx, client, testInvoice(), []byte("pdf"))
			if err == nil || !strings.Contains(err.Error(), "invalid email address") {
				t.Fatalf("SendInvoice: want 'invalid email address' error, got %v", err)
			}
			err = d.SendReminder(ctx, client, testInvoice())
			if err == nil || !strings.Contains(err.Error(), "invalid email address") {
				t.Fatalf("SendReminder: want 'invalid email address' error, got %v", err)
			}
			if len(sender.calls) != 0 {
				t.Fatalf("no send expected, got %d calls: %+v", len(sender.calls), sender.calls)
			}
		})
	}
}

func assertContains(t *testing.T, s, want string) {
	t.Helper()
	if !strings.Contains(s, want) {
		t.Fatalf("%q does not contain %q", s, want)
	}
}

func keysOf(m map[string][]byte) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
