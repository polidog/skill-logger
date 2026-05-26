//go:build !turso

package store

import (
	"context"
	"database/sql"
	"os"

	_ "modernc.org/sqlite"
)

// Open opens a local SQLite database. If TURSO_DATABASE_URL is set, the user
// requested Turso embedded replicas — but this build was compiled without the
// `turso` tag, so we return an error explaining how to enable it.
func Open(ctx context.Context, path string) (*Store, error) {
	if os.Getenv("TURSO_DATABASE_URL") != "" {
		return nil, ErrNoTurso
	}
	db, err := sql.Open("sqlite", path+"?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)&_pragma=foreign_keys(on)")
	if err != nil {
		return nil, err
	}
	if err := db.PingContext(ctx); err != nil {
		_ = db.Close()
		return nil, err
	}
	return &Store{db: db}, nil
}
