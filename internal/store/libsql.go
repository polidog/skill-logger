//go:build turso

package store

import (
	"context"
	"database/sql"
	"os"

	"github.com/tursodatabase/go-libsql"
)

// Open opens a local libsql database. When TURSO_DATABASE_URL and
// TURSO_AUTH_TOKEN are set, it uses embedded replicas that sync against the
// remote primary. Otherwise it falls back to a pure-local libsql file.
func Open(ctx context.Context, path string) (*Store, error) {
	url := os.Getenv("TURSO_DATABASE_URL")
	token := os.Getenv("TURSO_AUTH_TOKEN")
	if url != "" {
		var opts []libsql.Option
		if token != "" {
			opts = append(opts, libsql.WithAuthToken(token))
		}
		connector, err := libsql.NewEmbeddedReplicaConnector(path, url, opts...)
		if err != nil {
			return nil, err
		}
		db := sql.OpenDB(connector)
		if err := db.PingContext(ctx); err != nil {
			_ = db.Close()
			return nil, err
		}
		return &Store{db: db, sync: func() error { _, err := connector.Sync(); return err }}, nil
	}
	db, err := sql.Open("libsql", "file:"+path)
	if err != nil {
		return nil, err
	}
	if err := db.PingContext(ctx); err != nil {
		_ = db.Close()
		return nil, err
	}
	return &Store{db: db}, nil
}
