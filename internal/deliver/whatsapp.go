package deliver

import (
	"context"
	"errors"
	"strings"

	"github.com/jesus/invoice-app/internal/model"
	"github.com/jesus/invoice-app/internal/whatsapp"
)

// WhatsAppAPI is the subset of the WhatsApp session used by the deliverer.
type WhatsAppAPI interface {
	SendMessage(ctx context.Context, jid, text string) error
	SendDocument(ctx context.Context, jid, filename string, data []byte, caption string) error
}

type WhatsAppDeliverer struct {
	api          WhatsAppAPI
	pixFallback  string
	businessName string
}

func NewWhatsApp(api WhatsAppAPI, pixFallback string) *WhatsAppDeliverer {
	return &WhatsAppDeliverer{api: api, pixFallback: pixFallback}
}

func NewWhatsAppWithBusiness(api WhatsAppAPI, pixFallback, businessName string) *WhatsAppDeliverer {
	return &WhatsAppDeliverer{api: api, pixFallback: pixFallback, businessName: businessName}
}

func (d *WhatsAppDeliverer) Name() string { return "whatsapp" }

func (d *WhatsAppDeliverer) SendInvoice(ctx context.Context, c model.Client, inv model.Invoice, pdf []byte) error {
	jid, err := whatsappJID(c)
	if err != nil {
		return err
	}
	num := invoiceNumber(inv)
	lang := clientLang(c)
	caption := invoiceCaption(lang, c, inv, d.businessName)
	caption += pixLine(lang, pixKeyFor(inv, d.pixFallback))
	return d.api.SendDocument(ctx, jid, "fatura-"+num+".pdf", pdf, caption)
}

func (d *WhatsAppDeliverer) SendReminder(ctx context.Context, c model.Client, inv model.Invoice) error {
	jid, err := whatsappJID(c)
	if err != nil {
		return err
	}
	lang := clientLang(c)
	text := reminderText(lang, inv)
	text += pixLine(lang, pixKeyFor(inv, d.pixFallback))
	return d.api.SendMessage(ctx, jid, text)
}

// whatsappJID resolves and validates the client's phone number and builds
// the WhatsApp direct-chat JID from it.
func whatsappJID(c model.Client) (string, error) {
	if c.Phone == nil || strings.TrimSpace(*c.Phone) == "" {
		return "", errors.New("client has no WhatsApp phone number")
	}
	normalized, err := whatsapp.NormalizePhone(*c.Phone)
	if err != nil {
		return "", err
	}
	return normalized + "@s.whatsapp.net", nil
}
