package telegram_test

import (
	"context"
	"errors"
	"testing"

	"github.com/jesus/invoice-app/internal/telegram"
)

type fakeSender struct {
	calls   int
	chatIDs []string
	texts   []string
	err     error
}

func (f *fakeSender) SendMessage(ctx context.Context, chatID, text string) error {
	f.calls++
	f.chatIDs = append(f.chatIDs, chatID)
	f.texts = append(f.texts, text)
	return f.err
}

func TestNotifierForwardsToAdminChat(t *testing.T) {
	sentinel := errors.New("boom")
	api := &fakeSender{err: sentinel}
	n := telegram.NewNotifier(api, "  777  ")

	if err := n.Notify(context.Background(), "olá"); !errors.Is(err, sentinel) {
		t.Fatalf("want api error propagated, got %v", err)
	}
	if api.calls != 1 || api.chatIDs[0] != "777" || api.texts[0] != "olá" {
		t.Fatalf("unexpected call: calls=%d chat=%q text=%q", api.calls, api.chatIDs[0], api.texts[0])
	}
}

func TestNotifierNoopWithoutAdminChat(t *testing.T) {
	for _, chatID := range []string{"", "   "} {
		api := &fakeSender{}
		n := telegram.NewNotifier(api, chatID)

		if err := n.Notify(context.Background(), "ignored"); err != nil {
			t.Fatalf("chat %q: want nil error, got %v", chatID, err)
		}
		if api.calls != 0 {
			t.Fatalf("chat %q: api called %d times, want 0", chatID, api.calls)
		}
	}
}
