package main

import (
	"context"
	"database/sql"
	"log"
	"strings"
	"time"

	"github.com/jesus/invoice-app/internal/deliver"
	"github.com/jesus/invoice-app/internal/i18n"
	"github.com/jesus/invoice-app/internal/pdf"
	"github.com/jesus/invoice-app/internal/repo"
	"github.com/jesus/invoice-app/internal/schedule"
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

// settingsLocale resolves the stored locale at call time so a change on
// the settings page reaches admin notifications and bot replies without a
// restart. Unset or unknown values keep pt-BR. The unnamed func type is
// assignable to both deliver.AdminLocaleFunc and telegram.LocaleFunc.
func settingsLocale(s *repo.SettingsRepo) func() i18n.Lang {
	return func() i18n.Lang {
		v, err := s.Get(context.Background(), repo.SettingLocale)
		if err != nil {
			return i18n.PtBR
		}
		return i18n.Resolve(v)
	}
}

// setupSenderInfo loads the business identity printed on invoice PDFs.
func setupSenderInfo(ctx context.Context, s *repo.SettingsRepo) pdf.SenderInfo {
	return pdf.SenderInfo{
		Name:    settingOr(ctx, s, repo.SettingBusinessName),
		Address: settingOr(ctx, s, repo.SettingBusinessAddress),
		PIXKey:  settingOr(ctx, s, repo.SettingDefaultPIXKey),
	}
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

// Admin bot restart pacing; variables so tests can shrink them.
var (
	adminBotInitialBackoff = 30 * time.Second
	adminBotMaxBackoff     = 5 * time.Minute
)

// nextBackoff picks the restart delay after a failed Run that lasted ran.
// A run that outlived the max backoff was healthy for a long stretch, so
// escalation resets to base instead of punishing a fresh failure with the
// previous stale delay.
func nextBackoff(previous, ran, base, max time.Duration) time.Duration {
	if ran >= max {
		return base
	}
	return min(previous*2, max)
}

// runAdminBot keeps the polling loop alive for the process lifetime:
// AdminBot.Run exits permanently on its first polling error, so wrap it
// with restarts on a growing backoff (30s doubling up to a 5min cap) until
// the context is canceled.
func runAdminBot(ctx context.Context, bot *telegram.AdminBot) {
	base, max := adminBotInitialBackoff, adminBotMaxBackoff
	backoff := base
	for {
		started := time.Now()
		err := bot.Run(ctx)
		if ctx.Err() != nil {
			return
		}
		backoff = nextBackoff(backoff, time.Since(started), base, max)
		log.Printf("admin bot stopped (%v); restarting in %s", err, backoff)
		select {
		case <-ctx.Done():
			return
		case <-time.After(backoff):
		}
	}
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
		bot := telegram.NewAdminBot(c, adminChatID, repos.Invoices, repos.Clients, settingsLocale(repos.Settings))
		go runAdminBot(ctx, bot)
	}
}

// startScheduler launches the recurring-invoice scheduler goroutine. It runs
// unconditionally: the Router degrades gracefully when channels are
// unconfigured (per-channel "not configured" results), so ticks without any
// delivery channel just report failures to the (possibly silent) notifier.
func startScheduler(ctx context.Context, repos *repo.Repos, router *deliver.Router, notifier deliver.Notifier, senderInfo pdf.SenderInfo) {
	sched := schedule.New(repos.Recurring, repos.Invoices, repos.Clients, router, notifier, senderInfo, time.Now)
	go func() {
		log.Printf("scheduler started (tick every %s)", schedule.DefaultInterval)
		sched.Run(ctx)
	}()
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
