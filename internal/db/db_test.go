package db_test

import (
	"path/filepath"
	"testing"

	"github.com/jesus/invoice-app/internal/db"
)

func TestOpenRunsMigrations(t *testing.T) {
	path := filepath.Join(t.TempDir(), "test.db")
	conn, err := db.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()

	var version int
	if err := conn.QueryRow(`SELECT version FROM schema_meta`).Scan(&version); err != nil {
		t.Fatal(err)
	}
	if version < 1 {
		t.Fatalf("schema version = %d, want >= 1", version)
	}
}

func TestOpenIsIdempotent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "test.db")
	conn, err := db.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()

	conn2, err := db.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer conn2.Close()

	var count int
	if err := conn2.QueryRow(`SELECT COUNT(*) FROM schema_meta`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("schema_meta rows = %d, want 1 (migrations re-applied)", count)
	}

	for _, table := range []string{"clients", "products", "invoices", "invoice_items", "recurring_schedules", "settings"} {
		if _, err := conn2.Exec("SELECT 1 FROM " + table); err != nil {
			t.Fatalf("table %s missing: %v", table, err)
		}
	}
}
