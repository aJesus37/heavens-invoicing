package deliver

import (
	"fmt"
	"strings"
	"time"

	"github.com/jesus/invoice-app/internal/i18n"
	"github.com/jesus/invoice-app/internal/model"
	"github.com/jesus/invoice-app/internal/pdf"
)

// clientLang resolves the language for texts addressed to the client,
// falling back to pt-BR when the stored preference is empty or unknown.
func clientLang(c model.Client) i18n.Lang {
	return i18n.Resolve(c.Language)
}

// invoiceNumber renders the zero-padded display number shared by all
// channels ("000001").
func invoiceNumber(inv model.Invoice) string {
	return fmt.Sprintf("%06d", inv.Number)
}

// formatDate renders dates as dd/mm/yyyy in every locale — same documented
// decision as the PDF: both audiences read it unambiguously.
func formatDate(t time.Time) string {
	return t.Format("02/01/2006")
}

// invoiceCaption is the document caption sent alongside the PDF on
// messaging channels (WhatsApp, Telegram). It now includes the sender
// company, itemized products with quantities and prices, and the grand total
// so the message is readable without opening the PDF.
func invoiceCaption(lang i18n.Lang, c model.Client, inv model.Invoice, businessName string) string {
	var b strings.Builder
	if strings.TrimSpace(businessName) != "" {
		b.WriteString(i18n.T(lang, "deliver.caption_company", strings.TrimSpace(businessName)))
		b.WriteString("\n")
	}
	b.WriteString(i18n.T(lang, "deliver.caption_invoice", invoiceNumber(inv), c.Name))
	if len(inv.Items) > 0 {
		b.WriteString("\n")
		for _, it := range inv.Items {
			b.WriteString("\n")
			b.WriteString(i18n.T(lang, "deliver.caption_item",
				it.Description,
				it.Quantity,
				pdf.FormatBRL(it.UnitPrice),
				pdf.FormatBRL(it.Total),
			))
		}
		b.WriteString("\n")
		b.WriteString(i18n.T(lang, "deliver.caption_total", pdf.FormatBRL(inv.Total)))
	}
	return b.String()
}

// reminderText is the payment-reminder wording for messaging channels.
func reminderText(lang i18n.Lang, inv model.Invoice) string {
	return i18n.T(lang, "deliver.reminder_text", invoiceNumber(inv), formatDate(inv.DueDate))
}

// pixLine appends the localized PIX payment line; empty key means no line.
func pixLine(lang i18n.Lang, key string) string {
	if key == "" {
		return ""
	}
	return i18n.T(lang, "deliver.pix_line", key)
}

// pixMessage returns the localized PIX key as a separate copyable message;
// empty key means no message. The catalog entry deliver.pix_message is
// expected to be "%s" (just the raw key) so the second message is easily
// copyable on mobile. Using i18n keeps the option to add a label later
// without changing deliverers.
func pixMessage(lang i18n.Lang, key string) string {
	if key == "" {
		return ""
	}
	return i18n.T(lang, "deliver.pix_message", key)
}

// pixLabel returns the label that precedes the separate PIX message, e.g.
// "Chave PIX:" / "PIX key:". Empty when no label needed.
func pixLabel(lang i18n.Lang) string {
	return i18n.T(lang, "deliver.pix_label")
}

// emailPIXSection renders the PIX paragraph embedded into email bodies
// (double newline separated); empty key means no section.
func emailPIXSection(lang i18n.Lang, key string) string {
	if key == "" {
		return ""
	}
	return i18n.T(lang, "email.pix_section", key)
}
