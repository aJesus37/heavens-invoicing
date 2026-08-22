package deliver

import (
	"context"
	"errors"
	"fmt"
	"net/mail"
	"strings"

	"github.com/jesus/invoice-app/internal/i18n"
	"github.com/jesus/invoice-app/internal/model"
)

type MailSender interface {
	Send(from string, to []string, subject, body string, attachments map[string][]byte) error
}

type EmailDeliverer struct {
	sender      MailSender
	from        string
	pixFallback string
}

func NewEmail(sender MailSender, from, pixFallback string) *EmailDeliverer {
	return &EmailDeliverer{sender: sender, from: from, pixFallback: pixFallback}
}

func (e *EmailDeliverer) Name() string { return "email" }

// Message texts come from the i18n catalogs (email.* keys), selected by
// the client's language. The attachment filename stays "fatura-<n>.pdf" on
// purpose: it is an identifier-like artifact, not prose, and a stable name
// keeps mail clients from treating resends as unrelated threads.
// Templates stored in the settings table remain deferred until there is a
// concrete need.

func (e *EmailDeliverer) SendInvoice(ctx context.Context, c model.Client, inv model.Invoice, pdf []byte) error {
	from, to, err := e.addresses(c)
	if err != nil {
		return err
	}
	lang := clientLang(c)
	num := invoiceNumber(inv)
	subject := i18n.T(lang, "email.subject_invoice", num)
	body := i18n.T(lang, "email.body_invoice", c.Name, num, emailPIXSection(lang, pixKeyFor(inv, e.pixFallback)))
	return e.sender.Send(from, []string{to}, subject, body, map[string][]byte{"fatura-" + num + ".pdf": pdf})
}

func (e *EmailDeliverer) SendReminder(ctx context.Context, c model.Client, inv model.Invoice) error {
	from, to, err := e.addresses(c)
	if err != nil {
		return err
	}
	lang := clientLang(c)
	num := invoiceNumber(inv)
	subject := i18n.T(lang, "email.subject_reminder", num)
	body := i18n.T(lang, "email.body_reminder", c.Name, num, formatDate(inv.DueDate),
		emailPIXSection(lang, pixKeyFor(inv, e.pixFallback)))
	return e.sender.Send(from, []string{to}, subject, body, nil)
}

// addresses resolves and validates the envelope sender and recipient,
// guarding against SMTP header injection through either field.
func (e *EmailDeliverer) addresses(c model.Client) (from, to string, err error) {
	if from, err = validAddress(e.from); err != nil {
		return "", "", err
	}
	if to, err = recipient(c); err != nil {
		return "", "", err
	}
	return from, to, nil
}

func recipient(c model.Client) (string, error) {
	if c.Email == nil || strings.TrimSpace(*c.Email) == "" {
		return "", errors.New("client has no email address")
	}
	return validAddress(*c.Email)
}

// validAddress rejects addresses containing CR, LF or other control
// characters and parses the remainder with net/mail, returning the bare
// normalized address safe for use in headers and the SMTP envelope.
func validAddress(addr string) (string, error) {
	if strings.ContainsFunc(addr, func(r rune) bool { return r < 0x20 || r == 0x7f }) {
		return "", fmt.Errorf("invalid email address %q", addr)
	}
	parsed, err := mail.ParseAddress(strings.TrimSpace(addr))
	if err != nil {
		return "", fmt.Errorf("invalid email address %q", addr)
	}
	return parsed.Address, nil
}

// pixKeyFor returns the key to offer for payment: the invoice's own key
// when set, otherwise the configured fallback, otherwise "".
func pixKeyFor(inv model.Invoice, fallback string) string {
	if inv.PIXKey != nil && *inv.PIXKey != "" {
		return *inv.PIXKey
	}
	return fallback
}
