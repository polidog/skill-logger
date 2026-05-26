package cmd

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"

	"github.com/polidog/skill-logger/internal/store"
)

func newStatsCmd() *cobra.Command {
	var (
		kind   string
		source string
		since  string
		limit  int
		daily  bool
	)
	cmd := &cobra.Command{
		Use:   "stats",
		Short: "Print usage statistics to stdout",
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx, cancel := context.WithTimeout(cmd.Context(), 10*time.Second)
			defer cancel()

			s, err := openStore(ctx)
			if err != nil {
				return err
			}
			defer s.Close()

			sinceTime, err := parseSince(since)
			if err != nil {
				return err
			}
			f := store.Filter{
				Source: store.Source(source),
				Kind:   store.Kind(kind),
				Since:  sinceTime,
				Limit:  limit,
			}

			tw := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 0, 2, ' ', 0)
			defer tw.Flush()

			if daily {
				points, err := s.Daily(ctx, f)
				if err != nil {
					return err
				}
				fmt.Fprintln(tw, "DAY\tCOUNT")
				for _, p := range points {
					fmt.Fprintf(tw, "%s\t%d\n", p.Day, p.Count)
				}
				return nil
			}

			ranks, err := s.Ranking(ctx, f)
			if err != nil {
				return err
			}
			fmt.Fprintln(tw, "RANK\tNAME\tCOUNT")
			for i, r := range ranks {
				fmt.Fprintf(tw, "%d\t%s\t%d\n", i+1, r.Name, r.Count)
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&kind, "kind", "", "filter by kind (skill|command)")
	cmd.Flags().StringVar(&source, "source", "", "filter by source (claude|codex)")
	cmd.Flags().StringVar(&since, "since", "", "filter to events newer than this (e.g. 7d, 24h, 30m, or RFC3339 timestamp)")
	cmd.Flags().IntVar(&limit, "limit", 20, "max rows to show in ranking")
	cmd.Flags().BoolVar(&daily, "daily", false, "show a per-day timeline instead of a ranking")
	return cmd
}

func newSyncCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "sync",
		Short: "Force a Turso embedded-replica sync (no-op for local SQLite)",
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx, cancel := context.WithTimeout(cmd.Context(), 30*time.Second)
			defer cancel()
			s, err := openStore(ctx)
			if err != nil {
				return err
			}
			defer s.Close()
			return s.Sync()
		},
	}
}

func newVersionCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print version information",
		Run: func(cmd *cobra.Command, args []string) {
			fmt.Fprintln(cmd.OutOrStdout(), "skill-logger 0.1.0")
		},
	}
}

func parseSince(s string) (time.Time, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return time.Time{}, nil
	}
	if t, err := time.Parse(time.RFC3339, s); err == nil {
		return t.UTC(), nil
	}
	if len(s) < 2 {
		return time.Time{}, fmt.Errorf("invalid --since %q", s)
	}
	unit := s[len(s)-1]
	nStr := s[:len(s)-1]
	n, err := strconv.Atoi(nStr)
	if err != nil {
		return time.Time{}, fmt.Errorf("invalid --since %q: %w", s, err)
	}
	now := time.Now().UTC()
	switch unit {
	case 'd':
		return now.Add(-time.Duration(n) * 24 * time.Hour), nil
	case 'h':
		return now.Add(-time.Duration(n) * time.Hour), nil
	case 'm':
		return now.Add(-time.Duration(n) * time.Minute), nil
	case 'w':
		return now.Add(-time.Duration(n) * 7 * 24 * time.Hour), nil
	}
	return time.Time{}, fmt.Errorf("invalid --since unit in %q (use d/h/m/w or RFC3339)", s)
}
