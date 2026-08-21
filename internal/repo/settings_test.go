package repo_test

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"github.com/jesus/invoice-app/internal/db"
	"github.com/jesus/invoice-app/internal/repo"
)

func TestSettingsRoundTrip(t *testing.T) {
	r := openTestDB(t)
	ctx := context.Background()

	if err := r.Settings.Set(ctx, repo.SettingSMTPHost, "smtp.gmail.com"); err != nil {
		t.Fatal(err)
	}
	got, err := r.Settings.Get(ctx, repo.SettingSMTPHost)
	if err != nil {
		t.Fatal(err)
	}
	if got != "smtp.gmail.com" {
		t.Fatalf("want %q, got %q", "smtp.gmail.com", got)
	}
}

func TestSettingsNotFound(t *testing.T) {
	r := openTestDB(t)
	if _, err := r.Settings.Get(context.Background(), repo.SettingSMTPHost); !errors.Is(err, repo.ErrNotFound) {
		t.Fatalf("want ErrNotFound, got %v", err)
	}
}

func TestSettingsUpsertOverwrites(t *testing.T) {
	conn, err := db.Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { conn.Close() })
	r := repo.New(conn).Settings
	ctx := context.Background()

	if err := r.Set(ctx, repo.SettingSMTPPort, "587"); err != nil {
		t.Fatal(err)
	}
	if err := r.Set(ctx, repo.SettingSMTPPort, "465"); err != nil {
		t.Fatal(err)
	}

	got, err := r.Get(ctx, repo.SettingSMTPPort)
	if err != nil {
		t.Fatal(err)
	}
	if got != "465" {
		t.Fatalf("want overwritten value %q, got %q", "465", got)
	}

	var n int
	if err := conn.QueryRow(`SELECT COUNT(*) FROM settings WHERE key = ?`, repo.SettingSMTPPort).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("want 1 row after upsert, got %d", n)
	}
}
