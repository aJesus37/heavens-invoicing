package main

import (
	"context"
	"database/sql"
	"log"
	"strings"

	"github.com/jesus/invoice-app/internal/deliver"
	"github.com/jesus/invoice-app/internal/repo"
	"github.com/jesus/invoice-app/internal/telegram"
	"github.com/jesus/invoice-app/internal/whatsapp"
	waLog "go.mau.fi/whatsmeow/util/log"
)

// settingOr returns the stored value for key or "" when unset.
func settingOr(ctx context.Context, s *repo.SettingsRepo, key string) string {
	value, err := s.Get(ctx, key)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(value)
}

// setupWhatsApp prepares the session store and attempts an initial
// connect. Both steps are non-fatal: WhatsApp is optional at runtime, and
// sends through an unlinked session fail with a clear error instead.
func setupWhatsApp(ctx context.Context, conn *sql.DB) *whatsapp.Session {
	session, err := whatsapp.NewSession(ctx, conn, waLog.Noop)
	if err != nil {
		log.Printf("whatsapp unavailable (continuing without): %v", err)
		return nil
	}
	if err := session.Connect(ctx); err != nil {
		log.Printf("whatsapp connect failed (sends will retry on use): %v", err)
	} else if session.IsConnected() {
		log.Println("whatsapp connected")
	}
	return session
}

func setupWhatsAppDeliverer(s *whatsapp.Session, pixFallback string) deliver.Deliverer {
	if s == nil {
		return nil
	}
	return deliver.NewWhatsApp(s, pixFallback)
}

// setupTelegram returns the API client when a bot token is configured,
// along with the admin chat id (possibly empty).
func setupTelegram(ctx context.Context, s *repo.SettingsRepo) (*telegram.Client, string) {
	token := settingOr(ctx, s, repo.SettingTelegramBotToken)
	adminChatID := settingOr(ctx, s, repo.SettingAdminTelegramChatID)
	if token == "" {
		return nil, adminChatID
	}
	return telegram.NewClient(nil, "", token), adminChatID
}

func setupTelegramDeliverer(c *telegram.Client, pixFallback string) deliver.Deliverer {
	if c == nil {
		return nil
	}
	return deliver.NewTelegram(c, pixFallback)
}

// tgNotifier wraps the admin notifier; it is a silent no-op whenever the
// bot or the admin chat is unconfigured.
func tgNotifier(c *telegram.Client, adminChatID string) deliver.Notifier {
	return telegram.NewNotifier(c, adminChatID)
}

// startAdminBot launches the polling loop only when both the bot token and
// the admin chat are configured; otherwise it logs why it is disabled.
func startAdminBot(ctx context.Context, c *telegram.Client, adminChatID string, repos *repo.Repos) {
	switch {
	case c == nil && adminChatID == "":
		log.Println("admin bot disabled: no telegram bot token or admin chat configured")
	case c == nil:
		log.Println("admin bot disabled: no telegram bot token configured")
	case adminChatID == "":
		log.Println("admin bot disabled: no admin telegram chat configured")
	default:
		bot := telegram.NewAdminBot(c, adminChatID, repos.Invoices, repos.Clients)
		go func() {
			if err := bot.Run(ctx); err != nil && ctx.Err() == nil {
				log.Printf("admin bot stopped: %v", err)
			}
		}()
	}
}

// setupEmail builds the SMTP-backed email deliverer when its required
// settings are present (host, port, from); credentials are optional.
func setupEmail(ctx context.Context, s *repo.SettingsRepo, pixFallback string) deliver.Deliverer {
	host := settingOr(ctx, s, repo.SettingSMTPHost)
	port := settingOr(ctx, s, repo.SettingSMTPPort)
	from := settingOr(ctx, s, repo.SettingSMTPFrom)
	if host == "" || port == "" || from == "" {
		return nil
	}
	user := settingOr(ctx, s, repo.SettingSMTPUser)
	pass := settingOr(ctx, s, repo.SettingSMTPPass)
	return deliver.NewEmail(deliver.NewSMTP(host, port, user, pass), from, pixFallback)
}
