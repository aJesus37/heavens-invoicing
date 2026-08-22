package deliver

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/jesus/invoice-app/internal/model"
)

type TelegramAPI interface {
	SendMessage(ctx context.Context, chatID, text string) error
	SendDocument(ctx context.Context, chatID, filename string, content []byte, caption string) error
}

type TelegramDeliverer struct {
	api         TelegramAPI
	pixFallback string
}

func NewTelegram(api TelegramAPI, pixFallback string) *TelegramDeliverer {
	return &TelegramDeliverer{api: api, pixFallback: pixFallback}
}

func (d *TelegramDeliverer) Name() string { return "telegram" }

func (d *TelegramDeliverer) SendInvoice(ctx context.Context, c model.Client, inv model.Invoice, pdf []byte) error {
	chatID, err := telegramChatID(c)
	if err != nil {
		return err
	}
	num := fmt.Sprintf("%06d", inv.Number)
	caption := fmt.Sprintf("Fatura #%s para %s", num, c.Name)
	caption += pixLine(pixKeyFor(inv, d.pixFallback))
	return d.api.SendDocument(ctx, chatID, "fatura-"+num+".pdf", pdf, caption)
}

func (d *TelegramDeliverer) SendReminder(ctx context.Context, c model.Client, inv model.Invoice) error {
	chatID, err := telegramChatID(c)
	if err != nil {
		return err
	}
	num := fmt.Sprintf("%06d", inv.Number)
	text := fmt.Sprintf("Lembrete: a fatura #%s, com vencimento em %s, ainda está pendente.",
		num, inv.DueDate.Format("02/01/2006"))
	text += pixLine(pixKeyFor(inv, d.pixFallback))
	return d.api.SendMessage(ctx, chatID, text)
}

func telegramChatID(c model.Client) (string, error) {
	if c.TelegramChatID == nil || strings.TrimSpace(*c.TelegramChatID) == "" {
		return "", errors.New("client has no Telegram chat ID")
	}
	return strings.TrimSpace(*c.TelegramChatID), nil
}

func pixLine(key string) string {
	if key == "" {
		return ""
	}
	return "\nChave PIX: " + key
}
