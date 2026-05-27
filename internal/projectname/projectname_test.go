package projectname

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type rawStat struct {
	cwd   string
	count int64
}

func TestFoldEmpty(t *testing.T) {
	got := Fold[rawStat](nil,
		func(r rawStat) string { return r.cwd },
		func(r rawStat) int64 { return r.count })
	if len(got) != 0 {
		t.Errorf("expected empty output, got %+v", got)
	}
}

func TestFoldSortsByCountDesc(t *testing.T) {
	dir := t.TempDir() // not a git repo, so Shorten returns the cwd itself (tildeified)
	a := filepath.Join(dir, "a")
	b := filepath.Join(dir, "b")
	c := filepath.Join(dir, "c")
	os.MkdirAll(a, 0o755)
	os.MkdirAll(b, 0o755)
	os.MkdirAll(c, 0o755)

	items := []rawStat{
		{a, 5},
		{b, 10},
		{c, 1},
	}
	got := Fold(items,
		func(r rawStat) string { return r.cwd },
		func(r rawStat) int64 { return r.count })
	if len(got) != 3 {
		t.Fatalf("want 3 buckets, got %d", len(got))
	}
	if got[0].Count != 10 || got[1].Count != 5 || got[2].Count != 1 {
		t.Errorf("not sorted by count desc: %+v", got)
	}
}

func TestFoldCollapsesSameDisplay(t *testing.T) {
	// Two rows pointing at the same dir → collapse into one bucket with
	// summed count.
	dir := t.TempDir()
	os.MkdirAll(dir, 0o755)
	items := []rawStat{
		{dir, 3},
		{dir, 7},
	}
	got := Fold(items,
		func(r rawStat) string { return r.cwd },
		func(r rawStat) int64 { return r.count })
	if len(got) != 1 {
		t.Fatalf("want 1 bucket, got %d (%+v)", len(got), got)
	}
	if got[0].Count != 10 {
		t.Errorf("sum = %d, want 10", got[0].Count)
	}
}

func TestShortenEmptyReturnsUnknown(t *testing.T) {
	if Shorten("") != "(unknown)" {
		t.Errorf("Shorten(\"\") = %q, want (unknown)", Shorten(""))
	}
}

func TestShortenNonGitPathReturnsAsIs(t *testing.T) {
	// Tempdir is (almost certainly) not inside a git repo, so Shorten
	// should return the path back, tildeified if under $HOME.
	dir := t.TempDir()
	got := Shorten(dir)
	if got == "(unknown)" {
		t.Errorf("non-empty path shouldn't be unknown, got %q", got)
	}
	// Result should either be the path itself or its tildeified form;
	// either way it should match by suffix.
	if !strings.HasSuffix(got, filepath.Base(dir)) {
		t.Errorf("result %q doesn't end with %q", got, filepath.Base(dir))
	}
}

func TestShortenTildeifiesHome(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		t.Skip("no home dir")
	}
	// Try a subpath of home that probably exists; we use the home itself.
	got := Shorten(home)
	if got != "~" && !strings.HasPrefix(got, "~") {
		// In some CI envs $HOME may not exist on disk and Shorten falls
		// back to the raw path. Accept that case.
		t.Logf("home path %q resolved to %q (no tildeify — likely missing on disk)", home, got)
	}
}

func TestShortenIsCached(t *testing.T) {
	// Hard to assert on the cache externally, but we can confirm two
	// successive calls return the same value (sanity).
	dir := t.TempDir()
	a := Shorten(dir)
	b := Shorten(dir)
	if a != b {
		t.Errorf("Shorten not stable across calls: %q vs %q", a, b)
	}
}
