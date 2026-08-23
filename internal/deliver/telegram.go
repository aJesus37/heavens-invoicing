package deliver

import (
	"context"
	"errors"
	"strings"

	"github.com/jesus/invoice-app/internal/model"
)

type TelegramAPI interface {
	SendMessage(ctx context.Context, chatID, text string) error
	SendDocument(ctx context.Context, chatID, filename string, content []byte, caption string) error
}

type TelegramDeliverer struct {
	api          TelegramAPI
	pixFallback  string
	businessName string
}

func NewTelegram(api TelegramAPI, pixFallback string) *TelegramDeliverer {
	return &TelegramDeliverer{api: api, pixFallback: pixFallback}
}

func NewTelegramWithBusiness(api TelegramAPI, pixFallback, businessName string) *TelegramDeliverer {
	return &TelegramDeliverer{api: api, pixFallback: pixFallback, businessName: businessName}
}

func (d *TelegramDeliverer) Name() string { return "telegram" }

func (d *TelegramDeliverer) SendInvoice(ctx context.Context, c model.Client, inv model.Invoice, pdf []byte) error {
	chatID, err := telegramChatID(c)
	if err != nil {
		return err
	}
	num := invoiceNumber(inv)
	lang := clientLang(c)
	caption := invoiceCaption(lang, c, inv, d.businessName)
	if err := d.api.SendDocument(ctx, chatID, "fatura-"+num+".pdf", pdf, caption); err != nil {
		return err
	}
	if pix := pixKeyFor(inv, d.pixFallback); pix != "" {
		if err := d.api.SendMessage(ctx, chatID, pixMessage(lang, pix)); err != nil {
			return err
		}
	}
	return nil
}

func (d *TelegramDeliverer) SendReminder(ctx context.Context, c model.Client, inv model.Invoice) error {
	chatID, err := telegramChatID(c)
	if err != nil {
		return err
	}
	lang := clientLang(c)
	text := reminderText(lang, inv)
	text += pixLine(lang, pixKeyFor(inv, d.pixFallback))
	return d.api.SendMessage(ctx, chatID, text)
}

func telegramChatID(c model.Client) (string, error) {
	if c.TelegramChatID == nil || strings.TrimSpace(*c.TelegramChatID) == "" {
		return "", errors.New("client has no Telegram chat ID")
	}
	return strings.TrimSpace(*c.TelegramChatID), nil
}
