package db

import (
	"database/sql"
	"embed"
	"fmt"
	"sort"

	_ "modernc.org/sqlite"
)

//go:embed migrations/*.sql
var migrationFS embed.FS

func Open(path string) (*sql.DB, error) {
	dsn := fmt.Sprintf("file:%s?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)&_pragma=foreign_keys(1)", path)
	conn, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	if _, err := conn.Exec(`CREATE TABLE IF NOT EXISTS schema_meta (version INTEGER NOT NULL)`); err != nil {
		conn.Close()
		return nil, err
	}
	var version int
	if err := conn.QueryRow(`SELECT COALESCE(MAX(version),0) FROM schema_meta`).Scan(&version); err != nil {
		conn.Close()
		return nil, err
	}
	entries, err := migrationFS.ReadDir("migrations")
	if err != nil {
		conn.Close()
		return nil, err
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name() < entries[j].Name() })
	for i, e := range entries {
		v := i + 1
		if v <= version {
			continue
		}
		sqlBytes, err := migrationFS.ReadFile("migrations/" + e.Name())
		if err != nil {
			conn.Close()
			return nil, err
		}
		tx, err := conn.Begin()
		if err != nil {
			conn.Close()
			return nil, err
		}
		if _, err := tx.Exec(string(sqlBytes)); err != nil {
			tx.Rollback()
			conn.Close()
			return nil, fmt.Errorf("migration %d (%s): %w", v, e.Name(), err)
		}
		if _, err := tx.Exec(`INSERT INTO schema_meta (version) VALUES (?)`, v); err != nil {
			tx.Rollback()
			conn.Close()
			return nil, err
		}
		if err := tx.Commit(); err != nil {
			conn.Close()
			return nil, err
		}
	}
	return conn, nil
}
