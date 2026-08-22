package deliver

import (
	"fmt"
	"time"

	"github.com/jesus/invoice-app/internal/i18n"
	"github.com/jesus/invoice-app/internal/model"
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
// messaging channels (WhatsApp, Telegram).
func invoiceCaption(lang i18n.Lang, c model.Client, inv model.Invoice) string {
	return i18n.T(lang, "deliver.caption_invoice", invoiceNumber(inv), c.Name)
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

// emailPIXSection renders the PIX paragraph embedded into email bodies
// (double newline separated); empty key means no section.
func emailPIXSection(lang i18n.Lang, key string) string {
	if key == "" {
		return ""
	}
	return i18n.T(lang, "email.pix_section", key)
}
