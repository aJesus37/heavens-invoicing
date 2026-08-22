package repo_test

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
	"time"

	"github.com/jesus/invoice-app/internal/db"
	"github.com/jesus/invoice-app/internal/repo"
)

// newSessionDB opens a throwaway database and returns the raw connection
// (for schema/row assertions) together with the repos under test.
func newSessionDB(t *testing.T) (*sql.DB, *repo.Repos) {
	t.Helper()
	conn, err := db.Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { conn.Close() })
	return conn, repo.New(conn)
}

func TestSessionCreateAndGetValid(t *testing.T) {
	conn, repos := newSessionDB(t)
	sessions := repos.Sessions
	ctx := context.Background()

	if err := sessions.Create(ctx, "hash-a", time.Now().Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	ok, err := sessions.GetValid(ctx, "hash-a")
	if err != nil {
		t.Fatal(err)
	}
	if !ok {
		t.Fatal("freshly created session should be valid")
	}

	// The raw token must never be stored: the column holds only a hash,
	// so the plaintext value we passed here is absent from storage.
	var stored string
	if err := conn.QueryRow(`SELECT token_hash FROM sessions`).Scan(&stored); err != nil {
		t.Fatal(err)
	}
	if stored == "raw-token-plaintext" {
		t.Fatal("sessions table must not store raw tokens")
	}
}

func TestSessionGetValidUnknownHash(t *testing.T) {
	_, repos := newSessionDB(t)
	ok, err := repos.Sessions.GetValid(context.Background(), "no-such-hash")
	if err != nil {
		t.Fatal(err)
	}
	if ok {
		t.Fatal("unknown token hash must not validate")
	}
}

func TestSessionExpiredIsInvalid(t *testing.T) {
	_, repos := newSessionDB(t)
	sessions := repos.Sessions
	ctx := context.Background()

	// Comfortably in the past: an expired row must fail validation even
	// though it still exists in the table.
	if err := sessions.Create(ctx, "hash-old", time.Now().Add(-time.Minute)); err != nil {
		t.Fatal(err)
	}
	ok, err := sessions.GetValid(ctx, "hash-old")
	if err != nil {
		t.Fatal(err)
	}
	if ok {
		t.Fatal("expired session must be invalid")
	}
}

func TestSessionExpiryBoundaryIsStrict(t *testing.T) {
	_, repos := newSessionDB(t)
	sessions := repos.Sessions
	ctx := context.Background()

	// expires_at exactly equal to now must already count as expired:
	// validation uses a strict greater-than comparison.
	if err := sessions.Create(ctx, "hash-edge", time.Now()); err != nil {
		t.Fatal(err)
	}
	ok, err := sessions.GetValid(ctx, "hash-edge")
	if err != nil {
		t.Fatal(err)
	}
	if ok {
		t.Fatal("session with expires_at == now must be invalid")
	}
}

func TestSessionDelete(t *testing.T) {
	_, repos := newSessionDB(t)
	sessions := repos.Sessions
	ctx := context.Background()

	if err := sessions.Create(ctx, "hash-del", time.Now().Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	if err := sessions.Delete(ctx, "hash-del"); err != nil {
		t.Fatal(err)
	}
	ok, err := sessions.GetValid(ctx, "hash-del")
	if err != nil {
		t.Fatal(err)
	}
	if ok {
		t.Fatal("deleted session must no longer validate")
	}
	// Deleting again is a no-op, not an error (logout idempotence).
	if err := sessions.Delete(ctx, "hash-del"); err != nil {
		t.Fatalf("repeat delete should not error: %v", err)
	}
}

func TestSessionDeleteExpiredSweep(t *testing.T) {
	conn, repos := newSessionDB(t)
	sessions := repos.Sessions
	ctx := context.Background()

	mustCreate := func(hash string, ttl time.Duration) {
		t.Helper()
		if err := sessions.Create(ctx, hash, time.Now().Add(ttl)); err != nil {
			t.Fatal(err)
		}
	}
	mustCreate("sweep-expired-1", -2*time.Hour)
	mustCreate("sweep-expired-2", -time.Second)
	mustCreate("sweep-keep", time.Hour)

	if err := sessions.DeleteExpired(ctx); err != nil {
		t.Fatal(err)
	}

	for _, hash := range []string{"sweep-expired-1", "sweep-expired-2"} {
		var n int
		if err := conn.QueryRow(`SELECT COUNT(*) FROM sessions WHERE token_hash = ?`, hash).Scan(&n); err != nil {
			t.Fatal(err)
		}
		if n != 0 {
			t.Errorf("%s should have been swept, found %d rows", hash, n)
		}
	}
	// The survivor row must still exist at all (not merely validate).
	var n int
	if err := conn.QueryRow(`SELECT COUNT(*) FROM sessions WHERE token_hash = 'sweep-keep'`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("valid session must survive sweep, found %d rows", n)
	}
}

func TestSessionsExpiryIndexExists(t *testing.T) {
	conn, _ := newSessionDB(t)
	var name string
	err := conn.QueryRow(
		`SELECT name FROM sqlite_master WHERE type = 'index' AND name = 'idx_sessions_expires'`,
	).Scan(&name)
	if err != nil {
		t.Fatalf("idx_sessions_expires index missing after migrations: %v", err)
	}
}
