package telegram

import (
	"context"
	"strings"
)

// MessageSender is the minimal Telegram surface needed to deliver messages.
// *Client satisfies it; tests use fakes.
type MessageSender interface {
	SendMessage(ctx context.Context, chatID, text string) error
}

// Notifier sends one-way messages to a single admin chat. With no chat
// configured it is a silent no-op: notifications must never crash the process.
type Notifier struct {
	api         MessageSender
	adminChatID string
}

func NewNotifier(api MessageSender, adminChatID string) *Notifier {
	return &Notifier{api: api, adminChatID: strings.TrimSpace(adminChatID)}
}

func (n *Notifier) Notify(ctx context.Context, text string) error {
	if n.adminChatID == "" || n.api == nil {
		return nil
	}
	return n.api.SendMessage(ctx, n.adminChatID, text)
}
