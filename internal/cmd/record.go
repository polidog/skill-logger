package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/polidog/skill-logger/internal/store"
	"github.com/polidog/skill-logger/internal/transcript"
)

type hookPayload struct {
	SessionID      string          `json:"session_id"`
	TranscriptPath string          `json:"transcript_path"`
	Cwd            string          `json:"cwd"`
	HookEventName  string          `json:"hook_event_name"`
	ToolName       string          `json:"tool_name"`
	ToolInput      json.RawMessage `json:"tool_input"`
	ToolUseID      string          `json:"tool_use_id"`
	Prompt         string          `json:"prompt"`
	Timestamp      string          `json:"timestamp"`
}

type skillToolInput struct {
	Skill string `json:"skill"`
}

type recordAction int

const (
	actionNone recordAction = iota
	actionInsertSkill
	actionInsertCommand
	actionFinishSkill
	actionFinishTurn
)

func newRecordCmd() *cobra.Command {
	var (
		source  string
		kindOvr string
		nameOvr string
		hostOvr string
		quiet   bool
	)
	cmd := &cobra.Command{
		Use:   "record",
		Short: "Read a hook JSON payload from stdin and append/finalize an event",
		Long: `Reads a single JSON object from stdin (the payload Claude Code passes to a
hook) and records the relevant event.

Auto-detected payloads:
  * PreToolUse + tool_name=Skill                  -> insert  kind=skill
  * UserPromptSubmit where prompt starts with "/" -> insert  kind=command
  * PostToolUse + tool_name=Skill                 -> finalize the matching skill row
                                                     (writes duration_ms + token usage)
  * Stop                                          -> finalize the latest pending command
                                                     in the session

If --kind and --name are provided on an insert event, they override the
auto-detection. Payloads that don't match anything recordable cause record to
exit 0 silently (so it never blocks the hook).`,
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx, cancel := context.WithTimeout(cmd.Context(), 5*time.Second)
			defer cancel()

			cfg, err := loadConfig()
			if err != nil {
				return err
			}
			host := hostOvr
			if host == "" {
				host = cfg.ResolveHostname()
			}

			data, err := io.ReadAll(os.Stdin)
			if err != nil {
				return fmt.Errorf("read stdin: %w", err)
			}
			data = []byte(strings.TrimSpace(string(data)))

			var p hookPayload
			if len(data) > 0 {
				if err := json.Unmarshal(data, &p); err != nil {
					if !quiet {
						fmt.Fprintf(os.Stderr, "skill-logger: ignoring non-JSON stdin: %v\n", err)
					}
					return nil
				}
			}

			action, kind, name := classify(p, kindOvr, nameOvr)
			if action == actionNone {
				return nil
			}

			s, err := openStore(ctx)
			if err != nil {
				return err
			}
			defer s.Close()

			now := time.Now().UTC()
			payloadTS := now
			if p.Timestamp != "" {
				if t, perr := time.Parse(time.RFC3339Nano, p.Timestamp); perr == nil {
					payloadTS = t.UTC()
				}
			}

			switch action {
			case actionInsertSkill, actionInsertCommand:
				ev := store.Event{
					Source:    store.Source(source),
					Timestamp: payloadTS,
					Host:      host,
					Raw:       string(data),
					SessionID: p.SessionID,
					Cwd:       p.Cwd,
					Kind:      kind,
					Name:      name,
					ToolUseID: p.ToolUseID,
				}
				if err := s.Insert(ctx, ev); err != nil {
					return fmt.Errorf("insert: %w", err)
				}
				if !quiet {
					fmt.Fprintf(os.Stderr, "skill-logger: recorded %s/%s = %s\n", ev.Source, ev.Kind, ev.Name)
				}

			case actionFinishSkill:
				u, _ := transcript.LatestUsage(p.TranscriptPath)
				start, ok, err := s.StartTime(ctx, p.ToolUseID)
				if err != nil {
					return fmt.Errorf("lookup start: %w", err)
				}
				if !ok {
					// No matching insert (hook installed mid-session). Nothing to update.
					return nil
				}
				dur := now.Sub(start).Milliseconds()
				if dur < 0 {
					dur = 0
				}
				n, err := s.UpdateBySkillToolUseID(ctx, p.ToolUseID, dur, u)
				if err != nil {
					return fmt.Errorf("update skill: %w", err)
				}
				if !quiet && n > 0 {
					fmt.Fprintf(os.Stderr, "skill-logger: finalized skill %s (%dms, ctx=%d)\n",
						p.ToolUseID, dur, u.InputTokens+u.CacheReadTokens+u.CacheCreationTokens)
				}

			case actionFinishTurn:
				if p.SessionID == "" {
					return nil
				}
				start, ok, err := s.StartTimeForPendingCommand(ctx, p.SessionID)
				if err != nil {
					return fmt.Errorf("lookup pending command: %w", err)
				}
				if !ok {
					return nil
				}
				u, _ := transcript.LatestUsage(p.TranscriptPath)
				dur := now.Sub(start).Milliseconds()
				if dur < 0 {
					dur = 0
				}
				n, err := s.UpdateLatestPendingCommand(ctx, p.SessionID, dur, u)
				if err != nil {
					return fmt.Errorf("update command: %w", err)
				}
				if !quiet && n > 0 {
					fmt.Fprintf(os.Stderr, "skill-logger: finalized command in %s (%dms, ctx=%d)\n",
						p.SessionID, dur, u.InputTokens+u.CacheReadTokens+u.CacheCreationTokens)
				}
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&source, "source", "claude", "source tool (claude|codex)")
	cmd.Flags().StringVar(&kindOvr, "kind", "", "override detected kind (skill|command)")
	cmd.Flags().StringVar(&nameOvr, "name", "", "override detected name")
	cmd.Flags().StringVar(&hostOvr, "host", "", "override host (default: config.hostname > $SKILL_LOGGER_HOSTNAME > os.Hostname())")
	cmd.Flags().BoolVarP(&quiet, "quiet", "q", false, "suppress stderr messages")
	return cmd
}

// classify inspects the hook payload (plus optional CLI overrides) and decides
// what record should do. For insert actions it also resolves kind+name.
func classify(p hookPayload, kindOvr, nameOvr string) (recordAction, store.Kind, string) {
	switch {
	case p.HookEventName == "PreToolUse" && p.ToolName == "Skill":
		var ti skillToolInput
		name := nameOvr
		if name == "" && len(p.ToolInput) > 0 {
			if err := json.Unmarshal(p.ToolInput, &ti); err == nil {
				name = ti.Skill
			}
		}
		kind := store.KindSkill
		if kindOvr != "" {
			kind = store.Kind(kindOvr)
		}
		if name == "" {
			return actionNone, "", ""
		}
		return actionInsertSkill, kind, name

	case p.HookEventName == "UserPromptSubmit":
		prompt := strings.TrimSpace(p.Prompt)
		if !strings.HasPrefix(prompt, "/") && nameOvr == "" {
			return actionNone, "", ""
		}
		name := nameOvr
		if name == "" {
			name = strings.SplitN(prompt, " ", 2)[0]
		}
		kind := store.KindCommand
		if kindOvr != "" {
			kind = store.Kind(kindOvr)
		}
		return actionInsertCommand, kind, name

	case p.HookEventName == "PostToolUse" && p.ToolName == "Skill" && p.ToolUseID != "":
		return actionFinishSkill, "", ""

	case p.HookEventName == "Stop":
		return actionFinishTurn, "", ""
	}

	// Fallback: bare --kind/--name with no recognizable payload still inserts
	// (preserves previous behavior for ad-hoc CLI use).
	if kindOvr != "" && nameOvr != "" {
		return actionInsertSkill, store.Kind(kindOvr), nameOvr
	}
	return actionNone, "", ""
}
