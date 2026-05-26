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
	actionFinishSkill
	actionFinishTurn
)

type insertSpec struct {
	kind store.Kind
	name string
}

type classifyResult struct {
	inserts  []insertSpec
	finalize recordAction
}

func newRecordCmd() *cobra.Command {
	var (
		source  string
		kindOvr string
		nameOvr string
		hostOvr string
		userOvr string
		quiet   bool
	)
	cmd := &cobra.Command{
		Use:   "record",
		Short: "Read a hook JSON payload from stdin and append/finalize an event",
		Long: `Reads a single JSON object from stdin (the payload Claude Code or Codex
passes to a hook) and records the relevant event.

Auto-detected payloads:
  * PreToolUse + tool_name=Skill                  -> insert  kind=skill
                                                     (Claude Code only; Codex
                                                     does not expose Skill as
                                                     a tool)
  * UserPromptSubmit where prompt starts with "/" -> insert  kind=command
  * UserPromptSubmit with $name mentions (Codex)  -> insert  kind=skill per
                                                     mention (only when
                                                     --source=codex)
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
			user := userOvr
			if user == "" {
				user = cfg.ResolveUser()
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

			res := classify(p, source, kindOvr, nameOvr)
			if len(res.inserts) == 0 && res.finalize == actionNone {
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

			rawPayload := ""
			if cfg.ShouldShareRaw() {
				rawPayload = string(data)
			}

			for _, item := range res.inserts {
				ev := store.Event{
					Source:    store.Source(source),
					Timestamp: payloadTS,
					Host:      host,
					User:      user,
					Raw:       rawPayload,
					SessionID: p.SessionID,
					Cwd:       p.Cwd,
					Kind:      item.kind,
					Name:      item.name,
					ToolUseID: p.ToolUseID,
				}
				if err := s.Insert(ctx, ev); err != nil {
					return fmt.Errorf("insert: %w", err)
				}
				if !quiet {
					fmt.Fprintf(os.Stderr, "skill-logger: recorded %s/%s = %s\n", ev.Source, ev.Kind, ev.Name)
				}
			}

			switch res.finalize {
			case actionFinishSkill:
				u, _ := transcript.LatestUsage(p.TranscriptPath, source)
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
				pending, err := s.PendingRows(ctx, p.SessionID)
				if err != nil {
					return fmt.Errorf("lookup pending rows: %w", err)
				}
				if len(pending) == 0 {
					return nil
				}
				u, _ := transcript.LatestUsage(p.TranscriptPath, source)
				var finalized int64
				for _, row := range pending {
					dur := now.Sub(row.Timestamp).Milliseconds()
					if dur < 0 {
						dur = 0
					}
					n, err := s.FinalizeRow(ctx, row.ID, dur, u)
					if err != nil {
						return fmt.Errorf("finalize row %d: %w", row.ID, err)
					}
					finalized += n
				}
				if !quiet && finalized > 0 {
					fmt.Fprintf(os.Stderr, "skill-logger: finalized %d row(s) in %s (ctx=%d)\n",
						finalized, p.SessionID, u.InputTokens+u.CacheReadTokens+u.CacheCreationTokens)
				}
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&source, "source", "claude", "source tool (claude|codex)")
	cmd.Flags().StringVar(&kindOvr, "kind", "", "override detected kind (skill|command)")
	cmd.Flags().StringVar(&nameOvr, "name", "", "override detected name")
	cmd.Flags().StringVar(&hostOvr, "host", "", "override host (default: config.hostname > $SKILL_LOGGER_HOSTNAME > os.Hostname())")
	cmd.Flags().StringVar(&userOvr, "user", "", "override user (default: config.user > $SKILL_LOGGER_USER > git config user.email)")
	cmd.Flags().BoolVarP(&quiet, "quiet", "q", false, "suppress stderr messages")
	return cmd
}

// classify inspects the hook payload (plus optional CLI overrides) and decides
// what record should do. Returns the list of rows to insert plus an optional
// finalize action. For Codex sources, UserPromptSubmit prompts are also
// scanned for `$skill-name` mentions since Codex injects skills via prompt
// mentions rather than a dedicated Skill tool.
func classify(p hookPayload, source, kindOvr, nameOvr string) classifyResult {
	switch p.HookEventName {
	case "PreToolUse":
		if p.ToolName != "Skill" {
			return classifyResult{}
		}
		name := nameOvr
		if name == "" && len(p.ToolInput) > 0 {
			var ti skillToolInput
			if err := json.Unmarshal(p.ToolInput, &ti); err == nil {
				name = ti.Skill
			}
		}
		if name == "" {
			return classifyResult{}
		}
		kind := store.KindSkill
		if kindOvr != "" {
			kind = store.Kind(kindOvr)
		}
		return classifyResult{inserts: []insertSpec{{kind: kind, name: name}}}

	case "UserPromptSubmit":
		prompt := strings.TrimSpace(p.Prompt)
		var inserts []insertSpec

		if strings.HasPrefix(prompt, "/") {
			name := nameOvr
			if name == "" {
				name = strings.SplitN(prompt, " ", 2)[0]
			}
			kind := store.KindCommand
			if kindOvr != "" {
				kind = store.Kind(kindOvr)
			}
			inserts = append(inserts, insertSpec{kind: kind, name: name})
		}

		if source == string(store.SourceCodex) {
			for _, m := range extractCodexMentions(prompt) {
				kind := store.KindSkill
				if kindOvr != "" {
					kind = store.Kind(kindOvr)
				}
				inserts = append(inserts, insertSpec{kind: kind, name: m})
			}
		}

		if len(inserts) == 0 && nameOvr != "" {
			kind := store.KindCommand
			if kindOvr != "" {
				kind = store.Kind(kindOvr)
			}
			inserts = append(inserts, insertSpec{kind: kind, name: nameOvr})
		}
		return classifyResult{inserts: inserts}

	case "PostToolUse":
		if p.ToolName == "Skill" && p.ToolUseID != "" {
			return classifyResult{finalize: actionFinishSkill}
		}
		return classifyResult{}

	case "Stop":
		return classifyResult{finalize: actionFinishTurn}
	}

	// Fallback: bare --kind/--name with no recognizable payload still inserts
	// (preserves previous behavior for ad-hoc CLI use).
	if kindOvr != "" && nameOvr != "" {
		return classifyResult{inserts: []insertSpec{{kind: store.Kind(kindOvr), name: nameOvr}}}
	}
	return classifyResult{}
}
