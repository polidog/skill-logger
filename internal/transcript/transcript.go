// Package transcript reads Claude Code session transcripts (JSONL) and
// extracts token usage information so callers can attribute context size to
// individual hook events.
package transcript

import (
	"bufio"
	"encoding/json"
	"os"

	"github.com/polidog/skill-logger/internal/store"
)

type entry struct {
	Type    string `json:"type"`
	Message struct {
		Usage struct {
			InputTokens              int64 `json:"input_tokens"`
			OutputTokens             int64 `json:"output_tokens"`
			CacheReadInputTokens     int64 `json:"cache_read_input_tokens"`
			CacheCreationInputTokens int64 `json:"cache_creation_input_tokens"`
		} `json:"usage"`
	} `json:"message"`
}

// LatestUsage scans the transcript JSONL backwards and returns the usage block
// of the most recent assistant entry. Returns ok=false if the file can't be
// read or no assistant entry with usage is found.
func LatestUsage(path string) (store.Usage, bool) {
	if path == "" {
		return store.Usage{}, false
	}
	f, err := os.Open(path)
	if err != nil {
		return store.Usage{}, false
	}
	defer f.Close()

	// Transcripts can grow long; collect lines then scan from the end so we
	// don't parse every entry on huge files.
	var lines []string
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 64*1024), 8*1024*1024)
	for sc.Scan() {
		lines = append(lines, sc.Text())
	}
	for i := len(lines) - 1; i >= 0; i-- {
		line := lines[i]
		if line == "" {
			continue
		}
		var e entry
		if err := json.Unmarshal([]byte(line), &e); err != nil {
			continue
		}
		if e.Type != "assistant" {
			continue
		}
		u := e.Message.Usage
		if u.InputTokens == 0 && u.OutputTokens == 0 && u.CacheReadInputTokens == 0 && u.CacheCreationInputTokens == 0 {
			continue
		}
		return store.Usage{
			InputTokens:         u.InputTokens,
			OutputTokens:        u.OutputTokens,
			CacheReadTokens:     u.CacheReadInputTokens,
			CacheCreationTokens: u.CacheCreationInputTokens,
		}, true
	}
	return store.Usage{}, false
}
