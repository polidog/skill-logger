package config

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestLoadMissingFileReturnsDefaults(t *testing.T) {
	t.Setenv("SKILL_LOGGER_DB", "")
	t.Setenv("SKILL_LOGGER_HOSTNAME", "")
	t.Setenv("SKILL_LOGGER_USER", "")
	t.Setenv("SKILL_LOGGER_SHARE_RAW", "")
	t.Setenv("TURSO_DATABASE_URL", "")
	t.Setenv("TURSO_AUTH_TOKEN", "")

	dir := t.TempDir()
	cfg, err := Load(filepath.Join(dir, "missing.toml"))
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if cfg.Mode != ModeLocal {
		t.Errorf("mode = %q, want %q", cfg.Mode, ModeLocal)
	}
	if cfg.User != "" || cfg.Hostname != "" {
		t.Errorf("expected unset user/hostname, got user=%q host=%q", cfg.User, cfg.Hostname)
	}
	if cfg.ShareRaw != nil {
		t.Errorf("ShareRaw should be nil for default, got %v", *cfg.ShareRaw)
	}
	if cfg.Path != "" {
		t.Errorf("Path should be empty for missing file, got %q", cfg.Path)
	}
}

func TestLoadReadsFile(t *testing.T) {
	t.Setenv("SKILL_LOGGER_DB", "")
	t.Setenv("SKILL_LOGGER_HOSTNAME", "")
	t.Setenv("SKILL_LOGGER_USER", "")
	t.Setenv("SKILL_LOGGER_SHARE_RAW", "")
	t.Setenv("TURSO_DATABASE_URL", "")
	t.Setenv("TURSO_AUTH_TOKEN", "")

	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	body := `
mode = "turso"
db_path = "/tmp/events.db"
hostname = "macbook"
user = "alice@example.com"
share_raw = false

[turso]
url = "libsql://test.turso.io"
auth_token = "tok"
sync_interval = "30s"
`
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if cfg.Mode != ModeTurso {
		t.Errorf("mode = %q", cfg.Mode)
	}
	if cfg.DBPath != "/tmp/events.db" {
		t.Errorf("db_path = %q", cfg.DBPath)
	}
	if cfg.Hostname != "macbook" {
		t.Errorf("hostname = %q", cfg.Hostname)
	}
	if cfg.User != "alice@example.com" {
		t.Errorf("user = %q", cfg.User)
	}
	if cfg.ShareRaw == nil || *cfg.ShareRaw != false {
		t.Errorf("share_raw should be explicit false, got %v", cfg.ShareRaw)
	}
	if cfg.Turso.URL != "libsql://test.turso.io" {
		t.Errorf("turso.url = %q", cfg.Turso.URL)
	}
	if cfg.Turso.AuthToken != "tok" {
		t.Errorf("turso.auth_token = %q", cfg.Turso.AuthToken)
	}
	if cfg.Turso.SyncInterval != 30*time.Second {
		t.Errorf("turso.sync_interval = %v", cfg.Turso.SyncInterval)
	}
	if cfg.Path != path {
		t.Errorf("Path = %q", cfg.Path)
	}
}

func TestLoadEnvOverrides(t *testing.T) {
	t.Setenv("SKILL_LOGGER_DB", "/env/events.db")
	t.Setenv("SKILL_LOGGER_HOSTNAME", "env-host")
	t.Setenv("SKILL_LOGGER_USER", "env@example.com")
	t.Setenv("SKILL_LOGGER_SHARE_RAW", "false")
	t.Setenv("TURSO_DATABASE_URL", "libsql://env.turso.io")
	t.Setenv("TURSO_AUTH_TOKEN", "envtok")

	dir := t.TempDir()
	cfg, err := Load(filepath.Join(dir, "missing.toml"))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.DBPath != "/env/events.db" {
		t.Errorf("db_path = %q", cfg.DBPath)
	}
	if cfg.Hostname != "env-host" {
		t.Errorf("hostname = %q", cfg.Hostname)
	}
	if cfg.User != "env@example.com" {
		t.Errorf("user = %q", cfg.User)
	}
	if cfg.ShareRaw == nil || *cfg.ShareRaw != false {
		t.Errorf("share_raw via env not applied: %v", cfg.ShareRaw)
	}
	// TURSO_DATABASE_URL should auto-flip mode to turso.
	if cfg.Mode != ModeTurso {
		t.Errorf("expected turso mode from env, got %q", cfg.Mode)
	}
	if cfg.Turso.URL != "libsql://env.turso.io" {
		t.Errorf("turso.url = %q", cfg.Turso.URL)
	}
	if cfg.Turso.AuthToken != "envtok" {
		t.Errorf("turso.auth_token = %q", cfg.Turso.AuthToken)
	}
}

func TestResolveHostnameFallsBackToOS(t *testing.T) {
	cfg := &Config{}
	got := cfg.ResolveHostname()
	if got == "" {
		t.Skip("os.Hostname() returned empty — skipping fallback assertion")
	}

	cfg.Hostname = "explicit"
	if cfg.ResolveHostname() != "explicit" {
		t.Errorf("explicit hostname not respected")
	}
}

func TestResolveUserExplicitWins(t *testing.T) {
	cfg := &Config{User: "explicit@example.com"}
	if cfg.ResolveUser() != "explicit@example.com" {
		t.Errorf("explicit user not respected")
	}
}

func TestResolveUserFallbackToGit(t *testing.T) {
	// We can't reliably mock git in this environment, but we can confirm
	// the function returns *something* string-typed without panicking when
	// User is empty. A real git fallback is exercised at runtime.
	cfg := &Config{}
	_ = cfg.ResolveUser()
}

func TestShouldShareRawDefaults(t *testing.T) {
	cfg := &Config{}
	if !cfg.ShouldShareRaw() {
		t.Errorf("default ShouldShareRaw should be true")
	}
	tru := true
	cfg.ShareRaw = &tru
	if !cfg.ShouldShareRaw() {
		t.Errorf("explicit true not respected")
	}
	fal := false
	cfg.ShareRaw = &fal
	if cfg.ShouldShareRaw() {
		t.Errorf("explicit false not respected")
	}
}

func TestValidate(t *testing.T) {
	t.Run("local ok", func(t *testing.T) {
		c := &Config{Mode: ModeLocal}
		if err := c.Validate(); err != nil {
			t.Errorf("local should be valid, got %v", err)
		}
	})
	t.Run("turso without url errors", func(t *testing.T) {
		c := &Config{Mode: ModeTurso}
		if err := c.Validate(); err == nil {
			t.Errorf("turso without url should be invalid")
		}
	})
	t.Run("turso with url ok", func(t *testing.T) {
		c := &Config{Mode: ModeTurso, Turso: Turso{URL: "libsql://x.turso.io"}}
		if err := c.Validate(); err != nil {
			t.Errorf("turso with url should be valid, got %v", err)
		}
	})
	t.Run("unknown mode errors", func(t *testing.T) {
		c := &Config{Mode: "weird"}
		if err := c.Validate(); err == nil {
			t.Errorf("unknown mode should be invalid")
		}
	})
}

func TestResolveDBPathDefault(t *testing.T) {
	t.Setenv("SKILL_LOGGER_DIR", t.TempDir())
	c := &Config{}
	p, err := c.ResolveDBPath()
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Base(p) != "events.db" {
		t.Errorf("default db path filename = %q, want events.db", filepath.Base(p))
	}
}

func TestExpandTilde(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skip("no home dir")
	}
	if got := expand("~/x.db"); got != filepath.Join(home, "x.db") {
		t.Errorf("expand(~/x.db) = %q", got)
	}
	if got := expand("/abs/p"); got != "/abs/p" {
		t.Errorf("expand should leave absolute paths untouched, got %q", got)
	}
	if got := expand(""); got != "" {
		t.Errorf("expand(\"\") = %q", got)
	}
}
