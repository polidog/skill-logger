package cmd

import (
	"context"
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/polidog/skill-logger/internal/store"
)

var (
	flagDBPath string
)

func New() *cobra.Command {
	root := &cobra.Command{
		Use:   "skill-logger",
		Short: "Record and view Claude Code / Codex skill and slash command usage",
		Long: `skill-logger records which Skills and slash commands you use in Claude Code
(and optionally Codex) and lets you browse the stats in a terminal UI.

Configure your ~/.claude/settings.json hooks so that PreToolUse(Skill) and
UserPromptSubmit pipe their JSON payload into "skill-logger record" — see the
README for a copy-pasteable snippet.`,
		SilenceUsage: true,
	}
	root.PersistentFlags().StringVar(&flagDBPath, "db", "", "path to events database (default: $SKILL_LOGGER_DIR/events.db or ~/.skill-logger/events.db)")

	root.AddCommand(newRecordCmd())
	root.AddCommand(newTUICmd())
	root.AddCommand(newStatsCmd())
	root.AddCommand(newSyncCmd())
	root.AddCommand(newVersionCmd())
	return root
}

func resolveDBPath() (string, error) {
	if flagDBPath != "" {
		return flagDBPath, nil
	}
	return store.DefaultDBPath()
}

func openStore(ctx context.Context) (*store.Store, error) {
	path, err := resolveDBPath()
	if err != nil {
		return nil, err
	}
	s, err := store.Open(ctx, path)
	if err != nil {
		return nil, fmt.Errorf("open %s: %w", path, err)
	}
	if err := s.Migrate(ctx); err != nil {
		_ = s.Close()
		return nil, fmt.Errorf("migrate: %w", err)
	}
	return s, nil
}

func Execute() {
	if err := New().Execute(); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}
