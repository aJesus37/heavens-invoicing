package repo

import (
	"context"
	"database/sql"
	"time"
)

// SessionRepo stores server-side session state. Only a SHA-256 hash of the
// session token is persisted; the raw token lives exclusively in the
// client's cookie, so a database leak cannot be replayed as a login.
type SessionRepo struct {
	db *sql.DB
}

// storeTime normalizes timestamps before they cross into SQLite. The
// driver renders time.Time values as RFC3339 text with variable-width
// fractional seconds, so mixed formats would compare lexicographically
// rather than chronologically. Truncating to whole UTC seconds yields
// fixed-width strings whose byte order matches time order; second
// granularity is ample for hour-scale session lifetimes.
func storeTime(t time.Time) time.Time {
	return t.UTC().Truncate(time.Second)
}

// Create inserts a session row keyed by tokenHash with the given expiry.
func (s *SessionRepo) Create(ctx context.Context, tokenHash string, expiresAt time.Time) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO sessions (token_hash, expires_at) VALUES (?, ?)`,
		tokenHash, storeTime(expiresAt))
	return err
}

// GetValid reports whether tokenHash names a live session: present in the
// table AND not yet expired (strict comparison — a row whose expiry equals
// now is already invalid). Expired-but-present rows are deliberately left
// in place for the lazy sweep to remove.
func (s *SessionRepo) GetValid(ctx context.Context, tokenHash string) (bool, error) {
	var n int
	err := s.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM sessions WHERE token_hash = ? AND expires_at > ?`,
		tokenHash, storeTime(time.Now())).Scan(&n)
	if err != nil {
		return false, err
	}
	return n > 0, nil
}

// Delete removes a session row. Deleting an unknown or already-deleted
// hash is a no-op so logout stays idempotent.
func (s *SessionRepo) Delete(ctx context.Context, tokenHash string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM sessions WHERE token_hash = ?`, tokenHash)
	return err
}

// DeleteExpired prunes rows whose expiry has passed. It backs the lazy
// sweep performed on successful logins (chosen over a background ticker:
// one indexed DELETE per login is simpler and plenty for a single-admin
// homelab app).
func (s *SessionRepo) DeleteExpired(ctx context.Context) error {
	_, err := s.db.ExecContext(ctx,
		`DELETE FROM sessions WHERE expires_at <= ?`, storeTime(time.Now()))
	return err
}
