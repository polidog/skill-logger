package projectname

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

const unknown = "(unknown)"

// Aggregate is the result of folding raw (cwd, count) pairs into display buckets.
type Aggregate struct {
	Display string
	Count   int64
}

// Fold collapses rows that share the same Shorten(cwd) — e.g. a repo root
// and one of its subdirectories — into a single bucket, sorted by count desc.
func Fold[T any](items []T, cwd func(T) string, count func(T) int64) []Aggregate {
	idx := make(map[string]int)
	var out []Aggregate
	for _, it := range items {
		d := Shorten(cwd(it))
		if i, ok := idx[d]; ok {
			out[i].Count += count(it)
			continue
		}
		idx[d] = len(out)
		out = append(out, Aggregate{Display: d, Count: count(it)})
	}
	for i := 1; i < len(out); i++ {
		for j := i; j > 0 && out[j].Count > out[j-1].Count; j-- {
			out[j], out[j-1] = out[j-1], out[j]
		}
	}
	return out
}

var (
	cacheMu sync.Mutex
	cache   = map[string]string{}
)

// Shorten returns a human-friendly label for a recorded cwd:
//   - empty cwd               -> "(unknown)"
//   - cwd inside a git repo   -> the repo's top-level path (so subdirs collapse)
//   - paths under $HOME       -> "~/..." form
//
// Results are cached, since the same cwd shows up many times in aggregates.
func Shorten(cwd string) string {
	if cwd == "" {
		return unknown
	}
	cacheMu.Lock()
	if v, ok := cache[cwd]; ok {
		cacheMu.Unlock()
		return v
	}
	cacheMu.Unlock()

	resolved := resolveGitRoot(cwd)
	if resolved == "" {
		resolved = cwd
	}
	display := tildeify(resolved)

	cacheMu.Lock()
	cache[cwd] = display
	cacheMu.Unlock()
	return display
}

func resolveGitRoot(cwd string) string {
	info, err := os.Stat(cwd)
	if err != nil || !info.IsDir() {
		return ""
	}
	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()
	cmd := exec.CommandContext(ctx, "git", "-C", cwd, "rev-parse", "--show-toplevel")
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

func tildeify(p string) string {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return p
	}
	if p == home {
		return "~"
	}
	if rel, err := filepath.Rel(home, p); err == nil && !strings.HasPrefix(rel, "..") {
		return "~" + string(filepath.Separator) + rel
	}
	return p
}
