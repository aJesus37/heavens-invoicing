package repo

import (
	"context"
	"database/sql"
	"errors"
)

const (
	SettingBusinessName    = "business_name"
	SettingBusinessAddress = "business_address"

	SettingSMTPHost = "smtp_host"
	SettingSMTPPort = "smtp_port"
	SettingSMTPUser = "smtp_user"
	SettingSMTPPass = "smtp_pass"
	SettingSMTPFrom = "smtp_from"

	SettingTelegramBotToken    = "telegram_bot_token"
	SettingAdminTelegramChatID = "admin_telegram_chat_id"

	// SettingAdminPasswordHash stores the bcrypt hash gating the web UI
	// and API (auth package). Only the hash is stored, never the password.
	SettingAdminPasswordHash = "admin_password_hash"

	SettingDefaultPIXKey = "default_pix_key"

	// SettingLocale selects the web UI language ("en" or "pt-BR").
	SettingLocale = "locale"
)

type SettingsRepo struct {
	db *sql.DB
}

func (s *SettingsRepo) Get(ctx context.Context, key string) (string, error) {
	var value string
	err := s.db.QueryRowContext(ctx, `SELECT value FROM settings WHERE key = ?`, key).Scan(&value)
	if errors.Is(err, sql.ErrNoRows) {
		return "", ErrNotFound
	}
	if err != nil {
		return "", err
	}
	return value, nil
}

func (s *SettingsRepo) Set(ctx context.Context, key, value string) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO settings (key, value) VALUES (?, ?)
		 ON CONFLICT(key) DO UPDATE SET value = excluded.value`,
		key, value)
	return err
}
