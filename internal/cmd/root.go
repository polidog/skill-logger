package cmd

import (
	"context"
	"fmt"
	"os"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/spf13/cobra"

	"github.com/polidog/skill-logger/internal/config"
	"github.com/polidog/skill-logger/internal/store"
	"github.com/polidog/skill-logger/internal/tui"
)

var (
	flagDBPath     string
	flagConfigPath string
)

func New() *cobra.Command {
	root := &cobra.Command{
		Use:   "skill-logger",
		Short: "Record and view Claude Code / Codex skill and slash command usage",
		Long: `skill-logger records which Skills and slash commands you use in Claude Code
(and optionally Codex) and lets you browse the stats in a terminal UI.

Run "skill-logger" with no arguments to launch the TUI.

Configure your ~/.claude/settings.json hooks so that PreToolUse(Skill) and
UserPromptSubmit pipe their JSON payload into "skill-logger record" — see the
README for a copy-pasteable snippet.

Storage backend (local SQLite vs Turso Embedded Replicas) is selected via
~/.skill-logger/config.toml. See the README for fields.`,
		SilenceUsage: true,
		RunE:         runTUI,
	}
	root.PersistentFlags().StringVar(&flagDBPath, "db", "", "override db_path from config (file path)")
	root.PersistentFlags().StringVar(&flagConfigPath, "config", "", "path to config.toml (default: $SKILL_LOGGER_CONFIG or ~/.skill-logger/config.toml)")

	root.AddCommand(newRecordCmd())
	root.AddCommand(newStatsCmd())
	root.AddCommand(newSyncCmd())
	root.AddCommand(newVersionCmd())
	root.AddCommand(newInitCmd())
	return root
}

func runTUI(cmd *cobra.Command, args []string) error {
	ctx := cmd.Context()
	if ctx == nil {
		ctx = context.Background()
	}
	s, err := openStore(ctx)
	if err != nil {
		return err
	}
	defer s.Close()

	p := tea.NewProgram(tui.New(s), tea.WithAltScreen(), tea.WithContext(ctx))
	_, err = p.Run()
	return err
}

func loadConfig() (*config.Config, error) {
	cfg, err := config.Load(flagConfigPath)
	if err != nil {
		return nil, err
	}
	if flagDBPath != "" {
		cfg.DBPath = flagDBPath
	}
	return cfg, nil
}

func openStore(ctx context.Context) (*store.Store, error) {
	cfg, err := loadConfig()
	if err != nil {
		return nil, err
	}
	s, err := store.Open(ctx, cfg)
	if err != nil {
		return nil, err
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
