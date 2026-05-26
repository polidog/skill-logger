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
)

type hookPayload struct {
	SessionID     string          `json:"session_id"`
	Cwd           string          `json:"cwd"`
	HookEventName string          `json:"hook_event_name"`
	ToolName      string          `json:"tool_name"`
	ToolInput     json.RawMessage `json:"tool_input"`
	Prompt        string          `json:"prompt"`
	Timestamp     string          `json:"timestamp"`
}

type skillToolInput struct {
	Skill string `json:"skill"`
}

func newRecordCmd() *cobra.Command {
	var (
		source  string
		kindOvr string
		nameOvr string
		quiet   bool
	)
	cmd := &cobra.Command{
		Use:   "record",
		Short: "Read a hook JSON payload from stdin and append it to the events database",
		Long: `Reads a single JSON object from stdin (the payload Claude Code passes to a
hook) and records the relevant event.

Auto-detected payloads:
  * PreToolUse + tool_name=Skill                 -> kind=skill, name=<skill>
  * UserPromptSubmit where prompt starts with "/"-> kind=command, name=<first token>

If --kind and --name are provided, they override the auto-detection. If the
payload doesn't match anything recordable, record exits 0 silently (so it never
blocks the hook).`,
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx, cancel := context.WithTimeout(cmd.Context(), 5*time.Second)
			defer cancel()

			data, err := io.ReadAll(os.Stdin)
			if err != nil {
				return fmt.Errorf("read stdin: %w", err)
			}
			data = []byte(strings.TrimSpace(string(data)))

			ev := store.Event{
				Source:    store.Source(source),
				Timestamp: time.Now().UTC(),
				Raw:       string(data),
			}

			if len(data) > 0 {
				var p hookPayload
				if err := json.Unmarshal(data, &p); err != nil {
					if !quiet {
						fmt.Fprintf(os.Stderr, "skill-logger: ignoring non-JSON stdin: %v\n", err)
					}
					return nil
				}
				ev.SessionID = p.SessionID
				ev.Cwd = p.Cwd
				if p.Timestamp != "" {
					if t, perr := time.Parse(time.RFC3339Nano, p.Timestamp); perr == nil {
						ev.Timestamp = t.UTC()
					}
				}

				kind, name := detectEvent(p)
				if kindOvr != "" {
					kind = store.Kind(kindOvr)
				}
				if nameOvr != "" {
					name = nameOvr
				}
				if kind == "" || name == "" {
					return nil
				}
				ev.Kind = kind
				ev.Name = name
			} else if kindOvr != "" && nameOvr != "" {
				ev.Kind = store.Kind(kindOvr)
				ev.Name = nameOvr
			} else {
				return nil
			}

			s, err := openStore(ctx)
			if err != nil {
				return err
			}
			defer s.Close()
			if err := s.Insert(ctx, ev); err != nil {
				return fmt.Errorf("insert: %w", err)
			}
			if !quiet {
				fmt.Fprintf(os.Stderr, "skill-logger: recorded %s/%s = %s\n", ev.Source, ev.Kind, ev.Name)
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&source, "source", "claude", "source tool (claude|codex)")
	cmd.Flags().StringVar(&kindOvr, "kind", "", "override detected kind (skill|command)")
	cmd.Flags().StringVar(&nameOvr, "name", "", "override detected name")
	cmd.Flags().BoolVarP(&quiet, "quiet", "q", false, "suppress stderr messages")
	return cmd
}

func detectEvent(p hookPayload) (store.Kind, string) {
	switch {
	case p.HookEventName == "PreToolUse" && p.ToolName == "Skill":
		var ti skillToolInput
		if err := json.Unmarshal(p.ToolInput, &ti); err == nil && ti.Skill != "" {
			return store.KindSkill, ti.Skill
		}
	case p.HookEventName == "UserPromptSubmit":
		prompt := strings.TrimSpace(p.Prompt)
		if strings.HasPrefix(prompt, "/") {
			name := strings.SplitN(prompt, " ", 2)[0]
			return store.KindCommand, name
		}
	}
	return "", ""
}
