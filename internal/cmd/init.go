package cmd

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/BurntSushi/toml"
	"github.com/spf13/cobra"
)

// agentTracerCommandMarkers identifies command-type hook entries that this
// tool owns. Any entry whose `command` field contains one of these markers is
// treated as already-installed so re-running `init --write` stays idempotent.
// The legacy "skill-logger record" marker is preserved so existing users with
// the old binary name still get the "already up to date" path during the
// rename transition.
var agentTracerCommandMarkers = []string{"agent-tracer record", "skill-logger record"}

// Recommended commands wired into each event.
const (
	claudeRecordCmd = "agent-tracer record --quiet"
	codexRecordCmd  = "agent-tracer record --quiet --source codex"
)

// claudeToolMatcher is the Pre/PostToolUse matcher we install. Claude Code
// resolves matcher as a regular expression, so this catches both the Skill
// tool and every MCP tool (whose name is `mcp__<server>__<tool>`).
const claudeToolMatcher = "Skill|mcp__.*"

func newInitCmd() *cobra.Command {
	var (
		target         string
		write          bool
		claudeSettings string
		codexConfig    string
	)
	cmd := &cobra.Command{
		Use:   "init",
		Short: "Print or apply Claude Code / Codex hook configuration for agent-tracer",
		Long: `Print or apply hook configuration so that Claude Code and/or Codex pipe
their hook payloads into "agent-tracer record".

Without --write the recommended snippets are printed to stdout so you can copy
them manually. With --write the existing files are parsed, the agent-tracer
hook entries are merged in (idempotent — re-running won't duplicate them, and
legacy "skill-logger record" entries also register as already-installed),
and the result is written back.

Files touched:
  Claude Code: ~/.claude/settings.json
  Codex:       ~/.codex/config.toml

Merging preserves other hooks and unrelated settings, but file re-serialization
may not preserve original key order or TOML comments. Run without --write
first to preview the snippets.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			doClaude := target == "claude" || target == "both"
			doCodex := target == "codex" || target == "both"
			if !doClaude && !doCodex {
				return fmt.Errorf(`--target must be "claude", "codex", or "both"`)
			}

			out := cmd.OutOrStdout()

			if doClaude {
				path, err := resolveClaudePath(claudeSettings)
				if err != nil {
					return err
				}
				if write {
					summary, err := applyClaudeSettings(path)
					if err != nil {
						return err
					}
					fmt.Fprintln(out, summary)
				} else {
					fmt.Fprintf(out, "# Claude Code hooks → %s\n", path)
					snippet, err := claudeSnippet()
					if err != nil {
						return err
					}
					fmt.Fprintln(out, snippet)
				}
			}

			if doCodex {
				path, err := resolveCodexPath(codexConfig)
				if err != nil {
					return err
				}
				if write {
					summary, err := applyCodexConfig(path)
					if err != nil {
						return err
					}
					fmt.Fprintln(out, summary)
				} else {
					fmt.Fprintf(out, "# Codex hooks → %s\n", path)
					snippet, err := codexSnippet()
					if err != nil {
						return err
					}
					fmt.Fprintln(out, snippet)
				}
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&target, "target", "both", `which config to touch: "claude", "codex", or "both"`)
	cmd.Flags().BoolVar(&write, "write", false, "apply changes to disk (default: print snippets only)")
	cmd.Flags().StringVar(&claudeSettings, "claude-settings", "", "override Claude settings.json path (default: ~/.claude/settings.json)")
	cmd.Flags().StringVar(&codexConfig, "codex-config", "", "override Codex config.toml path (default: ~/.codex/config.toml)")
	return cmd
}

// ----- path resolution -----

func resolveClaudePath(override string) (string, error) {
	if override != "" {
		return expandHome(override)
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".claude", "settings.json"), nil
}

func resolveCodexPath(override string) (string, error) {
	if override != "" {
		return expandHome(override)
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".codex", "config.toml"), nil
}

func expandHome(p string) (string, error) {
	if !strings.HasPrefix(p, "~") {
		return p, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	if p == "~" {
		return home, nil
	}
	if strings.HasPrefix(p, "~/") {
		return filepath.Join(home, p[2:]), nil
	}
	return p, nil
}

// ----- snippets (used both for stdout printing and for apply seeding) -----

func claudeSnippet() (string, error) {
	doc := map[string]any{"hooks": claudeRecommendedHooks()}
	b, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return "", err
	}
	return string(b), nil
}

func claudeRecommendedHooks() map[string]any {
	return map[string]any{
		"PreToolUse":       []any{makeClaudeEntry(claudeToolMatcher, claudeRecordCmd)},
		"UserPromptSubmit": []any{makeClaudeEntry("", claudeRecordCmd)},
		"PostToolUse":      []any{makeClaudeEntry(claudeToolMatcher, claudeRecordCmd)},
		"Stop":             []any{makeClaudeEntry("", claudeRecordCmd)},
	}
}

func makeClaudeEntry(matcher, command string) map[string]any {
	entry := map[string]any{
		"hooks": []any{
			map[string]any{"type": "command", "command": command},
		},
	}
	if matcher != "" {
		entry["matcher"] = matcher
	}
	return entry
}

func codexSnippet() (string, error) {
	doc := map[string]any{"hooks": codexRecommendedHooks()}
	var buf bytes.Buffer
	enc := toml.NewEncoder(&buf)
	if err := enc.Encode(doc); err != nil {
		return "", err
	}
	return strings.TrimRight(buf.String(), "\n"), nil
}

func codexRecommendedHooks() map[string]any {
	entry := map[string]any{
		"hooks": []any{
			map[string]any{"type": "command", "command": codexRecordCmd},
		},
	}
	return map[string]any{
		"UserPromptSubmit": []any{entry},
		"Stop":             []any{entry},
	}
}

// ----- apply (Claude) -----

func applyClaudeSettings(path string) (string, error) {
	data, existed, err := readIfExists(path)
	if err != nil {
		return "", err
	}
	doc := map[string]any{}
	if len(bytes.TrimSpace(data)) > 0 {
		if err := json.Unmarshal(data, &doc); err != nil {
			return "", fmt.Errorf("parse %s: %w", path, err)
		}
	}

	hooks, _ := doc["hooks"].(map[string]any)
	if hooks == nil {
		hooks = map[string]any{}
	}

	added := 0
	mergeClaudeEvent(hooks, "PreToolUse", claudeToolMatcher, &added)
	mergeClaudeEvent(hooks, "UserPromptSubmit", "", &added)
	mergeClaudeEvent(hooks, "PostToolUse", claudeToolMatcher, &added)
	mergeClaudeEvent(hooks, "Stop", "", &added)
	doc["hooks"] = hooks

	if added == 0 {
		return fmt.Sprintf("%s: already up to date", path), nil
	}

	out, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return "", err
	}
	if err := writeFileSafe(path, append(out, '\n')); err != nil {
		return "", err
	}
	verb := "updated"
	if !existed {
		verb = "created"
	}
	return fmt.Sprintf("%s: %s (%d entr%s added)", path, verb, added, plural(added)), nil
}

func mergeClaudeEvent(hooks map[string]any, event, matcher string, added *int) {
	existing := asAnySlice(hooks[event])
	if containsAgentTracerEntry(existing) {
		return
	}
	existing = append(existing, makeClaudeEntry(matcher, claudeRecordCmd))
	hooks[event] = existing
	*added++
}

// ----- apply (Codex) -----

func applyCodexConfig(path string) (string, error) {
	data, existed, err := readIfExists(path)
	if err != nil {
		return "", err
	}
	doc := map[string]any{}
	if len(bytes.TrimSpace(data)) > 0 {
		if _, err := toml.Decode(string(data), &doc); err != nil {
			return "", fmt.Errorf("parse %s: %w", path, err)
		}
	}

	hooks, _ := doc["hooks"].(map[string]any)
	if hooks == nil {
		hooks = map[string]any{}
	}

	added := 0
	mergeCodexEvent(hooks, "UserPromptSubmit", &added)
	mergeCodexEvent(hooks, "Stop", &added)
	doc["hooks"] = hooks

	if added == 0 {
		return fmt.Sprintf("%s: already up to date", path), nil
	}

	var buf bytes.Buffer
	enc := toml.NewEncoder(&buf)
	if err := enc.Encode(doc); err != nil {
		return "", err
	}
	if err := writeFileSafe(path, buf.Bytes()); err != nil {
		return "", err
	}
	verb := "updated"
	if !existed {
		verb = "created"
	}
	return fmt.Sprintf("%s: %s (%d entr%s added)", path, verb, added, plural(added)), nil
}

func mergeCodexEvent(hooks map[string]any, event string, added *int) {
	existing := asAnySlice(hooks[event])
	if containsAgentTracerEntry(existing) {
		return
	}
	entry := map[string]any{
		"hooks": []any{
			map[string]any{"type": "command", "command": codexRecordCmd},
		},
	}
	existing = append(existing, entry)
	hooks[event] = existing
	*added++
}

// ----- shared helpers -----

// asAnySlice coerces a value that may be []any (from JSON), []map[string]any
// (from BurntSushi/toml's array-of-tables), or nil into a []any so we can
// inspect and append uniformly. Returns nil for unrecognized shapes.
func asAnySlice(v any) []any {
	switch x := v.(type) {
	case nil:
		return nil
	case []any:
		return x
	case []map[string]any:
		out := make([]any, len(x))
		for i, e := range x {
			out[i] = e
		}
		return out
	}
	return nil
}

// containsAgentTracerEntry reports whether any hook in the given event group
// already runs an `agent-tracer record …` (or legacy `skill-logger record …`)
// command. We match by substring so minor flag tweaks (e.g. `--quiet` vs no
// flag, or a different --source) all register as "already installed" —
// re-running init won't duplicate them.
func containsAgentTracerEntry(entries []any) bool {
	for _, e := range entries {
		em, ok := asStringMap(e)
		if !ok {
			continue
		}
		inner := asAnySlice(em["hooks"])
		for _, h := range inner {
			hm, ok := asStringMap(h)
			if !ok {
				continue
			}
			cmd, _ := hm["command"].(string)
			for _, marker := range agentTracerCommandMarkers {
				if strings.Contains(cmd, marker) {
					return true
				}
			}
		}
	}
	return false
}

// asStringMap handles both map[string]any (JSON) and map[string]any decoded
// by BurntSushi/toml (which also uses map[string]any for inline tables and
// array-of-tables elements).
func asStringMap(v any) (map[string]any, bool) {
	if m, ok := v.(map[string]any); ok {
		return m, true
	}
	return nil, false
}

func readIfExists(path string) ([]byte, bool, error) {
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, false, nil
		}
		return nil, false, fmt.Errorf("open %s: %w", path, err)
	}
	defer f.Close()
	data, err := io.ReadAll(f)
	if err != nil {
		return nil, true, fmt.Errorf("read %s: %w", path, err)
	}
	return data, true, nil
}

func writeFileSafe(path string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("mkdir %s: %w", filepath.Dir(path), err)
	}
	return os.WriteFile(path, data, 0o644)
}

func plural(n int) string {
	if n == 1 {
		return "y"
	}
	return "ies"
}
