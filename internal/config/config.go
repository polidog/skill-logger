package config

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/BurntSushi/toml"
)

// Mode selects how the SQLite database is opened.
type Mode string

const (
	ModeLocal Mode = "local"
	ModeTurso Mode = "turso"
)

// Config is the on-disk shape of ~/.skill-logger/config.toml.
type Config struct {
	Mode     Mode   `toml:"mode"`
	DBPath   string `toml:"db_path"`
	Hostname string `toml:"hostname"`
	User     string `toml:"user"`
	// ShareRaw controls whether the raw hook JSON is stored alongside each
	// event. Default (nil) is true for backwards compatibility. Set to false
	// in config.toml when writing to a shared Turso DB so prompts aren't
	// exposed to other team members.
	ShareRaw *bool `toml:"share_raw"`
	Turso    Turso `toml:"turso"`

	// Path is the resolved config file path (empty if no file was loaded).
	Path string `toml:"-"`
}

// Turso holds settings for libsql Embedded Replicas. It is only consulted when
// Mode == ModeTurso.
type Turso struct {
	URL          string        `toml:"url"`
	AuthToken    string        `toml:"auth_token"`
	SyncInterval time.Duration `toml:"sync_interval"`
}

// UnmarshalTOML accepts either a duration string ("60s", "5m") or seconds.
func (t *Turso) UnmarshalTOML(data any) error {
	m, ok := data.(map[string]any)
	if !ok {
		return fmt.Errorf("turso: expected table, got %T", data)
	}
	if v, ok := m["url"].(string); ok {
		t.URL = v
	}
	if v, ok := m["auth_token"].(string); ok {
		t.AuthToken = v
	}
	if v, ok := m["sync_interval"]; ok {
		switch x := v.(type) {
		case string:
			d, err := time.ParseDuration(x)
			if err != nil {
				return fmt.Errorf("turso.sync_interval: %w", err)
			}
			t.SyncInterval = d
		case int64:
			t.SyncInterval = time.Duration(x) * time.Second
		default:
			return fmt.Errorf("turso.sync_interval: unsupported type %T", v)
		}
	}
	return nil
}

// DefaultDir returns the configuration / data directory for skill-logger.
func DefaultDir() (string, error) {
	if v := os.Getenv("SKILL_LOGGER_DIR"); v != "" {
		return expand(v), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".skill-logger"), nil
}

// DefaultPath is the default config file location.
func DefaultPath() (string, error) {
	if v := os.Getenv("SKILL_LOGGER_CONFIG"); v != "" {
		return expand(v), nil
	}
	dir, err := DefaultDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "config.toml"), nil
}

// Load loads the config from `path`. If `path` is empty, DefaultPath() is used.
// A missing file is not an error: defaults are returned with Path="".
//
// Env overrides applied after the file is read:
//   - SKILL_LOGGER_DB           -> DBPath
//   - SKILL_LOGGER_HOSTNAME     -> Hostname (overrides os.Hostname() at record time)
//   - TURSO_DATABASE_URL        -> Turso.URL (also forces Mode=turso if Mode unset)
//   - TURSO_AUTH_TOKEN          -> Turso.AuthToken
func Load(path string) (*Config, error) {
	if path == "" {
		p, err := DefaultPath()
		if err != nil {
			return nil, err
		}
		path = p
	}

	cfg := &Config{Mode: ModeLocal}
	if data, err := os.ReadFile(path); err == nil {
		if _, err := toml.Decode(string(data), cfg); err != nil {
			return nil, fmt.Errorf("parse %s: %w", path, err)
		}
		cfg.Path = path
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}

	if v := os.Getenv("SKILL_LOGGER_DB"); v != "" {
		cfg.DBPath = expand(v)
	}
	if v := os.Getenv("TURSO_DATABASE_URL"); v != "" {
		cfg.Turso.URL = v
		if cfg.Mode == "" || cfg.Mode == ModeLocal {
			cfg.Mode = ModeTurso
		}
	}
	if v := os.Getenv("TURSO_AUTH_TOKEN"); v != "" {
		cfg.Turso.AuthToken = v
	}
	if v := os.Getenv("SKILL_LOGGER_HOSTNAME"); v != "" {
		cfg.Hostname = v
	}
	if v := os.Getenv("SKILL_LOGGER_USER"); v != "" {
		cfg.User = v
	}
	if v := os.Getenv("SKILL_LOGGER_SHARE_RAW"); v != "" {
		if b, err := strconv.ParseBool(v); err == nil {
			cfg.ShareRaw = &b
		}
	}
	if cfg.Mode == "" {
		cfg.Mode = ModeLocal
	}
	cfg.DBPath = expand(cfg.DBPath)
	return cfg, nil
}

// ResolveHostname returns Config.Hostname when set, otherwise falls back to
// os.Hostname(). Errors are swallowed and an empty string is returned so the
// hook never blocks; a `(unknown)` placeholder is the worst case.
func (c *Config) ResolveHostname() string {
	if c.Hostname != "" {
		return c.Hostname
	}
	h, err := os.Hostname()
	if err != nil {
		return ""
	}
	return h
}

// ResolveUser returns Config.User when set, otherwise falls back to
// `git config user.email`. Errors are swallowed so the hook never blocks; an
// empty string just means the event is anonymous (still attributed via host).
func (c *Config) ResolveUser() string {
	if c.User != "" {
		return c.User
	}
	out, err := exec.Command("git", "config", "--get", "user.email").Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

// ShouldShareRaw returns true if the raw hook JSON should be persisted with
// each event. Default (nil ShareRaw) is true; set ShareRaw=false explicitly
// in config.toml to strip prompts before they hit a shared Turso DB.
func (c *Config) ShouldShareRaw() bool {
	if c.ShareRaw == nil {
		return true
	}
	return *c.ShareRaw
}

// ResolveDBPath returns the database file path, falling back to <DefaultDir>/events.db.
// Ensures the parent directory exists.
func (c *Config) ResolveDBPath() (string, error) {
	path := c.DBPath
	if path == "" {
		dir, err := DefaultDir()
		if err != nil {
			return "", err
		}
		path = filepath.Join(dir, "events.db")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return "", err
	}
	return path, nil
}

// Validate checks that turso mode has the URL set.
func (c *Config) Validate() error {
	switch c.Mode {
	case ModeLocal, "":
		return nil
	case ModeTurso:
		if c.Turso.URL == "" {
			return errors.New(`mode = "turso" but turso.url is not set (and TURSO_DATABASE_URL is empty)`)
		}
		return nil
	default:
		return fmt.Errorf(`unknown mode %q (want "local" or "turso")`, c.Mode)
	}
}

func expand(p string) string {
	if p == "" {
		return p
	}
	if strings.HasPrefix(p, "~/") {
		if home, err := os.UserHomeDir(); err == nil {
			return filepath.Join(home, p[2:])
		}
	}
	return p
}
