package store

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/tursodatabase/go-libsql"

	"github.com/polidog/skill-logger/internal/config"
)

// Open opens the events database according to cfg. Mode=local opens a plain
// libsql file; Mode=turso opens an Embedded Replica that syncs against the
// configured primary URL.
func Open(ctx context.Context, cfg *config.Config) (*Store, error) {
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	path, err := cfg.ResolveDBPath()
	if err != nil {
		return nil, err
	}

	switch cfg.Mode {
	case config.ModeTurso:
		return openTurso(ctx, cfg, path)
	default:
		return openLocal(ctx, path)
	}
}

func openLocal(ctx context.Context, path string) (*Store, error) {
	db, err := sql.Open("libsql", "file:"+path)
	if err != nil {
		return nil, fmt.Errorf("open local %s: %w", path, err)
	}
	if err := db.PingContext(ctx); err != nil {
		_ = db.Close()
		return nil, err
	}
	return &Store{db: db}, nil
}

func openTurso(ctx context.Context, cfg *config.Config, path string) (*Store, error) {
	var opts []libsql.Option
	if cfg.Turso.AuthToken != "" {
		opts = append(opts, libsql.WithAuthToken(cfg.Turso.AuthToken))
	}
	if cfg.Turso.SyncInterval > 0 {
		opts = append(opts, libsql.WithSyncInterval(cfg.Turso.SyncInterval))
	}
	connector, err := libsql.NewEmbeddedReplicaConnector(path, cfg.Turso.URL, opts...)
	if err != nil {
		return nil, fmt.Errorf("open turso %s: %w", cfg.Turso.URL, err)
	}
	db := sql.OpenDB(connector)
	if err := db.PingContext(ctx); err != nil {
		_ = db.Close()
		return nil, err
	}
	return &Store{
		db: db,
		// go-libsql's Sync is deprecated in favor of a future tursogo package,
		// but tursogo isn't released yet — migrate once it lands.
		sync: func() error { _, err := connector.Sync(); return err }, //nolint:staticcheck
	}, nil
}
