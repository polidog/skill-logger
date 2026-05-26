package cmd

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/BurntSushi/toml"
)

func TestApplyClaudeSettingsCreatesFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "settings.json")

	summary, err := applyClaudeSettings(path)
	if err != nil {
		t.Fatalf("apply: %v", err)
	}
	if !strings.Contains(summary, "created") {
		t.Errorf("expected 'created' in summary, got %q", summary)
	}

	doc := loadJSON(t, path)
	hooks, _ := doc["hooks"].(map[string]any)
	for _, ev := range []string{"PreToolUse", "UserPromptSubmit", "PostToolUse", "Stop"} {
		entries, _ := hooks[ev].([]any)
		if len(entries) != 1 {
			t.Errorf("%s: want 1 entry, got %d", ev, len(entries))
		}
		if !containsSkillLoggerEntry(entries) {
			t.Errorf("%s: skill-logger entry not detected", ev)
		}
	}
}

func TestApplyClaudeSettingsPreservesExisting(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "settings.json")

	// Pre-existing settings with an unrelated key and an unrelated hook entry.
	initial := `{
  "model": "claude-opus-4-7",
  "hooks": {
    "PreToolUse": [
      { "matcher": "Bash", "hooks": [{ "type": "command", "command": "my-audit-tool" }] }
    ]
  }
}`
	if err := os.WriteFile(path, []byte(initial), 0o600); err != nil {
		t.Fatal(err)
	}

	if _, err := applyClaudeSettings(path); err != nil {
		t.Fatalf("apply: %v", err)
	}
	doc := loadJSON(t, path)

	// Unrelated top-level key preserved.
	if doc["model"] != "claude-opus-4-7" {
		t.Errorf("model key lost: %v", doc["model"])
	}
	// Unrelated existing hook preserved alongside ours.
	hooks := doc["hooks"].(map[string]any)
	pre, _ := hooks["PreToolUse"].([]any)
	if len(pre) != 2 {
		t.Fatalf("PreToolUse: want 2 entries (existing + skill-logger), got %d", len(pre))
	}
	if !hasCommand(pre, "my-audit-tool") {
		t.Errorf("existing my-audit-tool entry was lost")
	}
	if !hasCommand(pre, "skill-logger record") {
		t.Errorf("skill-logger entry not added to PreToolUse")
	}
}

func TestApplyClaudeSettingsIdempotent(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "settings.json")

	if _, err := applyClaudeSettings(path); err != nil {
		t.Fatalf("first apply: %v", err)
	}
	first, _ := os.ReadFile(path)

	summary, err := applyClaudeSettings(path)
	if err != nil {
		t.Fatalf("second apply: %v", err)
	}
	if !strings.Contains(summary, "already up to date") {
		t.Errorf("second run should report no-op, got: %q", summary)
	}
	second, _ := os.ReadFile(path)
	if string(first) != string(second) {
		t.Errorf("second apply changed file content")
	}
}

func TestApplyCodexConfigCreatesFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")

	summary, err := applyCodexConfig(path)
	if err != nil {
		t.Fatalf("apply: %v", err)
	}
	if !strings.Contains(summary, "created") {
		t.Errorf("expected 'created' in summary, got %q", summary)
	}

	doc := loadTOML(t, path)
	hooks, _ := doc["hooks"].(map[string]any)
	for _, ev := range []string{"UserPromptSubmit", "Stop"} {
		entries := asAnySlice(hooks[ev])
		if len(entries) != 1 {
			t.Errorf("%s: want 1 entry, got %d", ev, len(entries))
		}
		if !containsSkillLoggerEntry(entries) {
			t.Errorf("%s: skill-logger entry not detected", ev)
		}
	}
}

func TestApplyCodexConfigPreservesExisting(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")

	initial := `model = "gpt-5"

[[hooks.UserPromptSubmit]]

[[hooks.UserPromptSubmit.hooks]]
type = "command"
command = "my-audit-tool"
`
	if err := os.WriteFile(path, []byte(initial), 0o600); err != nil {
		t.Fatal(err)
	}

	if _, err := applyCodexConfig(path); err != nil {
		t.Fatalf("apply: %v", err)
	}
	doc := loadTOML(t, path)

	if doc["model"] != "gpt-5" {
		t.Errorf("top-level model lost: %v", doc["model"])
	}

	hooks := doc["hooks"].(map[string]any)
	ups := asAnySlice(hooks["UserPromptSubmit"])
	if len(ups) != 2 {
		t.Fatalf("UserPromptSubmit: want 2 (existing + skill-logger), got %d", len(ups))
	}
	if !hasCommand(ups, "my-audit-tool") {
		t.Errorf("existing my-audit-tool entry lost")
	}
	if !hasCommand(ups, "skill-logger record") {
		t.Errorf("skill-logger entry not added")
	}
}

func TestApplyCodexConfigIdempotent(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")

	if _, err := applyCodexConfig(path); err != nil {
		t.Fatalf("first apply: %v", err)
	}
	first, _ := os.ReadFile(path)

	summary, err := applyCodexConfig(path)
	if err != nil {
		t.Fatalf("second apply: %v", err)
	}
	if !strings.Contains(summary, "already up to date") {
		t.Errorf("expected no-op, got: %q", summary)
	}
	second, _ := os.ReadFile(path)
	if string(first) != string(second) {
		t.Errorf("second apply changed file")
	}
}

func TestClaudeSnippetParses(t *testing.T) {
	s, err := claudeSnippet()
	if err != nil {
		t.Fatalf("snippet: %v", err)
	}
	var doc map[string]any
	if err := json.Unmarshal([]byte(s), &doc); err != nil {
		t.Fatalf("snippet is not valid JSON: %v", err)
	}
	hooks, _ := doc["hooks"].(map[string]any)
	if len(hooks) != 4 {
		t.Errorf("expected 4 events in snippet, got %d", len(hooks))
	}
}

func TestCodexSnippetParses(t *testing.T) {
	s, err := codexSnippet()
	if err != nil {
		t.Fatalf("snippet: %v", err)
	}
	var doc map[string]any
	if _, err := toml.Decode(s, &doc); err != nil {
		t.Fatalf("snippet is not valid TOML: %v", err)
	}
	hooks, _ := doc["hooks"].(map[string]any)
	if _, ok := hooks["UserPromptSubmit"]; !ok {
		t.Errorf("UserPromptSubmit missing from codex snippet")
	}
	if _, ok := hooks["Stop"]; !ok {
		t.Errorf("Stop missing from codex snippet")
	}
}

// ---- helpers ----

func loadJSON(t *testing.T, path string) map[string]any {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]any
	if err := json.Unmarshal(b, &doc); err != nil {
		t.Fatalf("parse %s: %v", path, err)
	}
	return doc
}

func loadTOML(t *testing.T, path string) map[string]any {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	doc := map[string]any{}
	if _, err := toml.Decode(string(b), &doc); err != nil {
		t.Fatalf("parse %s: %v", path, err)
	}
	return doc
}

func hasCommand(entries []any, needle string) bool {
	for _, e := range entries {
		em, ok := asStringMap(e)
		if !ok {
			continue
		}
		for _, h := range asAnySlice(em["hooks"]) {
			hm, ok := asStringMap(h)
			if !ok {
				continue
			}
			cmd, _ := hm["command"].(string)
			if strings.Contains(cmd, needle) {
				return true
			}
		}
	}
	return false
}
