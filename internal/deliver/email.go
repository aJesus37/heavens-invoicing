package deliver

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/jesus/invoice-app/internal/model"
)

type MailSender interface {
	Send(from string, to []string, subject, body string, attachments map[string][]byte) error
}

type EmailDeliverer struct {
	sender MailSender
	from   string
}

func NewEmail(sender MailSender, from string) *EmailDeliverer {
	return &EmailDeliverer{sender: sender, from: from}
}

func (e *EmailDeliverer) Name() string { return "email" }

const invoiceBodyTmpl = `Olá {{.Client.Name}},

Segue em anexo a fatura #{{.Invoice.Number}}.

Qualquer dúvida, estamos à disposição.

Atenciosamente.`

const reminderBodyTmpl = `Olá {{.Client.Name}},

Lembramos que a fatura #{{.Invoice.Number}}, com vencimento em {{.Invoice.DueDate}}, ainda está pendente.

Caso o pagamento já tenha sido realizado, por favor desconsidere esta mensagem.

Atenciosamente.`

func (e *EmailDeliverer) SendInvoice(ctx context.Context, c model.Client, inv model.Invoice, pdf []byte) error {
	to, err := recipient(c)
	if err != nil {
		return err
	}
	num := fmt.Sprintf("%06d", inv.Number)
	subject := "Fatura #" + num
	body := replacePlaceholders(invoiceBodyTmpl, c.Name, num, "")
	return e.sender.Send(e.from, []string{to}, subject, body, map[string][]byte{"fatura-" + num + ".pdf": pdf})
}

func (e *EmailDeliverer) SendReminder(ctx context.Context, c model.Client, inv model.Invoice) error {
	to, err := recipient(c)
	if err != nil {
		return err
	}
	num := fmt.Sprintf("%06d", inv.Number)
	subject := "Lembrete de vencimento - Fatura #" + num
	body := replacePlaceholders(reminderBodyTmpl, c.Name, num, inv.DueDate.Format("02/01/2006"))
	return e.sender.Send(e.from, []string{to}, subject, body, nil)
}

func recipient(c model.Client) (string, error) {
	if c.Email == nil || strings.TrimSpace(*c.Email) == "" {
		return "", errors.New("client has no email address")
	}
	return strings.TrimSpace(*c.Email), nil
}

func replacePlaceholders(tmpl, clientName, number, dueDate string) string {
	return strings.NewReplacer(
		"{{.Client.Name}}", clientName,
		"{{.Invoice.Number}}", number,
		"{{.Invoice.DueDate}}", dueDate,
	).Replace(tmpl)
}
