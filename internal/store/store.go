package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

type Source string

const (
	SourceClaude Source = "claude"
	SourceCodex  Source = "codex"
)

type Kind string

const (
	KindSkill   Kind = "skill"
	KindCommand Kind = "command"
)

type Event struct {
	ID        int64
	Timestamp time.Time
	Source    Source
	Kind      Kind
	Name      string
	SessionID string
	Cwd       string
	Raw       string
}

type Ranking struct {
	Name  string
	Count int64
}

type DailyPoint struct {
	Day   string
	Count int64
}

type ProjectStat struct {
	Cwd   string
	Count int64
}

type Store struct {
	db   *sql.DB
	sync func() error
}

func (s *Store) DB() *sql.DB { return s.db }

func (s *Store) Close() error { return s.db.Close() }

func (s *Store) Sync() error {
	if s.sync == nil {
		return nil
	}
	return s.sync()
}

func (s *Store) Migrate(ctx context.Context) error {
	const schema = `
CREATE TABLE IF NOT EXISTS events (
	id         INTEGER PRIMARY KEY AUTOINCREMENT,
	ts         TEXT NOT NULL,
	source     TEXT NOT NULL,
	kind       TEXT NOT NULL,
	name       TEXT NOT NULL,
	session_id TEXT NOT NULL DEFAULT '',
	cwd        TEXT NOT NULL DEFAULT '',
	raw        TEXT NOT NULL DEFAULT ''
);
CREATE INDEX IF NOT EXISTS idx_events_ts ON events(ts);
CREATE INDEX IF NOT EXISTS idx_events_kind_name ON events(kind, name);
CREATE INDEX IF NOT EXISTS idx_events_source ON events(source);
`
	_, err := s.db.ExecContext(ctx, schema)
	return err
}

func (s *Store) Insert(ctx context.Context, e Event) error {
	if e.Timestamp.IsZero() {
		e.Timestamp = time.Now().UTC()
	}
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO events(ts, source, kind, name, session_id, cwd, raw) VALUES(?,?,?,?,?,?,?)`,
		e.Timestamp.UTC().Format(time.RFC3339Nano),
		string(e.Source),
		string(e.Kind),
		e.Name,
		e.SessionID,
		e.Cwd,
		e.Raw,
	)
	return err
}

type Filter struct {
	Source Source
	Kind   Kind
	Since  time.Time
	Limit  int
}

func (s *Store) Ranking(ctx context.Context, f Filter) ([]Ranking, error) {
	q := `SELECT name, COUNT(*) AS c FROM events WHERE 1=1`
	var args []any
	if f.Source != "" {
		q += ` AND source = ?`
		args = append(args, string(f.Source))
	}
	if f.Kind != "" {
		q += ` AND kind = ?`
		args = append(args, string(f.Kind))
	}
	if !f.Since.IsZero() {
		q += ` AND ts >= ?`
		args = append(args, f.Since.UTC().Format(time.RFC3339Nano))
	}
	q += ` GROUP BY name ORDER BY c DESC, name ASC`
	if f.Limit > 0 {
		q += fmt.Sprintf(` LIMIT %d`, f.Limit)
	}
	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Ranking
	for rows.Next() {
		var r Ranking
		if err := rows.Scan(&r.Name, &r.Count); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

func (s *Store) Daily(ctx context.Context, f Filter) ([]DailyPoint, error) {
	q := `SELECT substr(ts, 1, 10) AS day, COUNT(*) FROM events WHERE 1=1`
	var args []any
	if f.Source != "" {
		q += ` AND source = ?`
		args = append(args, string(f.Source))
	}
	if f.Kind != "" {
		q += ` AND kind = ?`
		args = append(args, string(f.Kind))
	}
	if !f.Since.IsZero() {
		q += ` AND ts >= ?`
		args = append(args, f.Since.UTC().Format(time.RFC3339Nano))
	}
	q += ` GROUP BY day ORDER BY day ASC`
	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []DailyPoint
	for rows.Next() {
		var p DailyPoint
		if err := rows.Scan(&p.Day, &p.Count); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

func (s *Store) Recent(ctx context.Context, f Filter) ([]Event, error) {
	q := `SELECT id, ts, source, kind, name, session_id, cwd, raw FROM events WHERE 1=1`
	var args []any
	if f.Source != "" {
		q += ` AND source = ?`
		args = append(args, string(f.Source))
	}
	if f.Kind != "" {
		q += ` AND kind = ?`
		args = append(args, string(f.Kind))
	}
	if !f.Since.IsZero() {
		q += ` AND ts >= ?`
		args = append(args, f.Since.UTC().Format(time.RFC3339Nano))
	}
	q += ` ORDER BY id DESC`
	if f.Limit > 0 {
		q += fmt.Sprintf(` LIMIT %d`, f.Limit)
	}
	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Event
	for rows.Next() {
		var e Event
		var ts string
		if err := rows.Scan(&e.ID, &ts, &e.Source, &e.Kind, &e.Name, &e.SessionID, &e.Cwd, &e.Raw); err != nil {
			return nil, err
		}
		t, perr := time.Parse(time.RFC3339Nano, ts)
		if perr != nil {
			t, _ = time.Parse(time.RFC3339, ts)
		}
		e.Timestamp = t
		out = append(out, e)
	}
	return out, rows.Err()
}

func (s *Store) ProjectRanking(ctx context.Context, f Filter) ([]ProjectStat, error) {
	q := `SELECT cwd, COUNT(*) AS c FROM events WHERE 1=1`
	var args []any
	if f.Source != "" {
		q += ` AND source = ?`
		args = append(args, string(f.Source))
	}
	if f.Kind != "" {
		q += ` AND kind = ?`
		args = append(args, string(f.Kind))
	}
	if !f.Since.IsZero() {
		q += ` AND ts >= ?`
		args = append(args, f.Since.UTC().Format(time.RFC3339Nano))
	}
	q += ` GROUP BY cwd ORDER BY c DESC, cwd ASC`
	if f.Limit > 0 {
		q += fmt.Sprintf(` LIMIT %d`, f.Limit)
	}
	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ProjectStat
	for rows.Next() {
		var p ProjectStat
		if err := rows.Scan(&p.Cwd, &p.Count); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

func (s *Store) Total(ctx context.Context, f Filter) (int64, error) {
	q := `SELECT COUNT(*) FROM events WHERE 1=1`
	var args []any
	if f.Source != "" {
		q += ` AND source = ?`
		args = append(args, string(f.Source))
	}
	if f.Kind != "" {
		q += ` AND kind = ?`
		args = append(args, string(f.Kind))
	}
	if !f.Since.IsZero() {
		q += ` AND ts >= ?`
		args = append(args, f.Since.UTC().Format(time.RFC3339Nano))
	}
	var c int64
	if err := s.db.QueryRowContext(ctx, q, args...).Scan(&c); err != nil {
		return 0, err
	}
	return c, nil
}

func DefaultDir() (string, error) {
	if v := os.Getenv("SKILL_LOGGER_DIR"); v != "" {
		return v, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".skill-logger"), nil
}

func DefaultDBPath() (string, error) {
	dir, err := DefaultDir()
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	return filepath.Join(dir, "events.db"), nil
}

var ErrNoTurso = errors.New("turso embedded replicas not enabled in this build (rebuild with -tags turso)")
