package deliver

import (
	"context"
	"errors"
	"fmt"
	"net/mail"
	"strings"

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

// Templates are fixed constants for now; user-editable templates stored in
// the settings table are deferred until there is a concrete need.

const invoiceBodyTmpl = `Olá {{.Client.Name}},

Segue em anexo a fatura #{{.Invoice.Number}}.{{.PIXSection}}

Qualquer dúvida, estamos à disposição.

Atenciosamente.`

const reminderBodyTmpl = `Olá {{.Client.Name}},

Lembramos que a fatura #{{.Invoice.Number}}, com vencimento em {{.Invoice.DueDate}}, ainda está pendente.{{.PIXSection}}

Caso o pagamento já tenha sido realizado, por favor desconsidere esta mensagem.

Atenciosamente.`

// pixSectionTmpl is rendered into the message body when a PIX key is
// available; when there is none the section is omitted entirely.
const pixSectionTmpl = "\n\nPara pagamento via PIX, utilize a chave {{.PIXKey}}."

func (e *EmailDeliverer) SendInvoice(ctx context.Context, c model.Client, inv model.Invoice, pdf []byte) error {
	from, to, err := e.addresses(c)
	if err != nil {
		return err
	}
	num := fmt.Sprintf("%06d", inv.Number)
	subject := "Fatura #" + num
	body := replacePlaceholders(invoiceBodyTmpl, c.Name, num, "", pixSection(pixKeyFor(inv, e.pixFallback)))
	return e.sender.Send(from, []string{to}, subject, body, map[string][]byte{"fatura-" + num + ".pdf": pdf})
}

func (e *EmailDeliverer) SendReminder(ctx context.Context, c model.Client, inv model.Invoice) error {
	from, to, err := e.addresses(c)
	if err != nil {
		return err
	}
	num := fmt.Sprintf("%06d", inv.Number)
	subject := "Lembrete de vencimento - Fatura #" + num
	body := replacePlaceholders(reminderBodyTmpl, c.Name, num, inv.DueDate.Format("02/01/2006"), pixSection(pixKeyFor(inv, e.pixFallback)))
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

func pixSection(key string) string {
	if key == "" {
		return ""
	}
	return strings.NewReplacer("{{.PIXKey}}", key).Replace(pixSectionTmpl)
}

func replacePlaceholders(tmpl, clientName, number, dueDate, pixSection string) string {
	return strings.NewReplacer(
		"{{.Client.Name}}", clientName,
		"{{.Invoice.Number}}", number,
		"{{.Invoice.DueDate}}", dueDate,
		"{{.PIXSection}}", pixSection,
	).Replace(tmpl)
}
