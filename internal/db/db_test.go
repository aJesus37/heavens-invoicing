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
	if err := conn.QueryRow(`SELECT COALESCE(MAX(version),0) FROM schema_meta`).Scan(&version); err != nil {
		t.Fatal(err)
	}
	if version < 2 {
		t.Fatalf("schema version = %d, want >= 2", version)
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
	// One row per applied migration; re-opening must not add any.
	if count != 3 {
		t.Fatalf("schema_meta rows = %d, want 3 (migrations re-applied)", count)
	}

	for _, table := range []string{"clients", "products", "invoices", "invoice_items", "recurring_schedules", "settings", "sessions"} {
		if _, err := conn2.Exec("SELECT 1 FROM " + table); err != nil {
			t.Fatalf("table %s missing: %v", table, err)
		}
	}
}

// TestMigration002ClientLanguageAndSessions pins the R2 Task 2 schema:
// clients carry a defaulted language column and the sessions table exists
// for the upcoming session auth task.
func TestMigration002ClientLanguageAndSessions(t *testing.T) {
	path := filepath.Join(t.TempDir(), "test.db")
	conn, err := db.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()

	// Existing rows (and fresh inserts without a language) default to pt-BR.
	if _, err := conn.Exec(`INSERT INTO clients (id, name) VALUES ('c1', 'Acme')`); err != nil {
		t.Fatal(err)
	}
	var language string
	if err := conn.QueryRow(`SELECT language FROM clients WHERE id = 'c1'`).Scan(&language); err != nil {
		t.Fatalf("clients.language missing: %v", err)
	}
	if language != "pt-BR" {
		t.Fatalf("default language = %q, want pt-BR", language)
	}

	// Sessions accepts the shape Task 3 needs.
	if _, err := conn.Exec(
		`INSERT INTO sessions (token_hash, expires_at) VALUES ('abc', '2027-01-01 00:00:00')`); err != nil {
		t.Fatalf("sessions insert failed: %v", err)
	}
}
