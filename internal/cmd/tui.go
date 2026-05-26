package cmd

import (
	"context"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/spf13/cobra"

	"github.com/polidog/skill-logger/internal/tui"
)

func newTUICmd() *cobra.Command {
	return &cobra.Command{
		Use:   "tui",
		Short: "Launch the terminal UI",
		RunE: func(cmd *cobra.Command, args []string) error {
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
		},
	}
}
