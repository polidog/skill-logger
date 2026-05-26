package store

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
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
	Host      string
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

type HostStat struct {
	Host  string
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
	host       TEXT NOT NULL DEFAULT '',
	raw        TEXT NOT NULL DEFAULT ''
);
CREATE INDEX IF NOT EXISTS idx_events_ts ON events(ts);
CREATE INDEX IF NOT EXISTS idx_events_kind_name ON events(kind, name);
CREATE INDEX IF NOT EXISTS idx_events_source ON events(source);
CREATE INDEX IF NOT EXISTS idx_events_host ON events(host);
`
	if _, err := s.db.ExecContext(ctx, schema); err != nil {
		return err
	}
	// Idempotent ALTER for DBs that pre-date the host column.
	if _, err := s.db.ExecContext(ctx, `ALTER TABLE events ADD COLUMN host TEXT NOT NULL DEFAULT ''`); err != nil {
		if !strings.Contains(err.Error(), "duplicate column") {
			return err
		}
	}
	if _, err := s.db.ExecContext(ctx, `CREATE INDEX IF NOT EXISTS idx_events_host ON events(host)`); err != nil {
		return err
	}
	return nil
}

func (s *Store) Insert(ctx context.Context, e Event) error {
	if e.Timestamp.IsZero() {
		e.Timestamp = time.Now().UTC()
	}
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO events(ts, source, kind, name, session_id, cwd, host, raw) VALUES(?,?,?,?,?,?,?,?)`,
		e.Timestamp.UTC().Format(time.RFC3339Nano),
		string(e.Source),
		string(e.Kind),
		e.Name,
		e.SessionID,
		e.Cwd,
		e.Host,
		e.Raw,
	)
	return err
}

type Filter struct {
	Source Source
	Kind   Kind
	Host   string
	Since  time.Time
	Limit  int
}

// applyFilter appends WHERE clauses for the non-zero Filter fields. It returns
// the augmented query string and the args slice (extended in place).
func applyFilter(q string, f Filter, args []any) (string, []any) {
	if f.Source != "" {
		q += ` AND source = ?`
		args = append(args, string(f.Source))
	}
	if f.Kind != "" {
		q += ` AND kind = ?`
		args = append(args, string(f.Kind))
	}
	if f.Host != "" {
		q += ` AND host = ?`
		args = append(args, f.Host)
	}
	if !f.Since.IsZero() {
		q += ` AND ts >= ?`
		args = append(args, f.Since.UTC().Format(time.RFC3339Nano))
	}
	return q, args
}

func (s *Store) Ranking(ctx context.Context, f Filter) ([]Ranking, error) {
	q, args := applyFilter(`SELECT name, COUNT(*) AS c FROM events WHERE 1=1`, f, nil)
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
	q, args := applyFilter(`SELECT substr(ts, 1, 10) AS day, COUNT(*) FROM events WHERE 1=1`, f, nil)
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
	q, args := applyFilter(`SELECT id, ts, source, kind, name, session_id, cwd, host, raw FROM events WHERE 1=1`, f, nil)
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
		if err := rows.Scan(&e.ID, &ts, &e.Source, &e.Kind, &e.Name, &e.SessionID, &e.Cwd, &e.Host, &e.Raw); err != nil {
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
	q, args := applyFilter(`SELECT cwd, COUNT(*) AS c FROM events WHERE 1=1`, f, nil)
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

func (s *Store) HostRanking(ctx context.Context, f Filter) ([]HostStat, error) {
	q, args := applyFilter(`SELECT host, COUNT(*) AS c FROM events WHERE 1=1`, f, nil)
	q += ` GROUP BY host ORDER BY c DESC, host ASC`
	if f.Limit > 0 {
		q += fmt.Sprintf(` LIMIT %d`, f.Limit)
	}
	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []HostStat
	for rows.Next() {
		var h HostStat
		if err := rows.Scan(&h.Host, &h.Count); err != nil {
			return nil, err
		}
		out = append(out, h)
	}
	return out, rows.Err()
}

// DistinctHosts returns all distinct host values present in the database, with
// counts. Used by the TUI to populate the host filter chip; counts are not
// limited by the filter so the picker stays stable across other filter toggles.
func (s *Store) DistinctHosts(ctx context.Context) ([]string, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT host FROM events GROUP BY host ORDER BY COUNT(*) DESC, host ASC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var h string
		if err := rows.Scan(&h); err != nil {
			return nil, err
		}
		out = append(out, h)
	}
	return out, rows.Err()
}

func (s *Store) Total(ctx context.Context, f Filter) (int64, error) {
	q, args := applyFilter(`SELECT COUNT(*) FROM events WHERE 1=1`, f, nil)
	var c int64
	if err := s.db.QueryRowContext(ctx, q, args...).Scan(&c); err != nil {
		return 0, err
	}
	return c, nil
}

