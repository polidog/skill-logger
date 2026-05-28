package cmd

import (
	"path"
	"strings"
)

// Claude Code exposes MCP tools through the regular Pre/PostToolUse hook with
// tool_name set to `mcp__<server>__<tool>`. Server and tool names may contain
// single underscores; the double-underscore is the delimiter.

// parseMCPTool splits "mcp__<server>__<tool>" into (server, tool). Returns
// ok=false for any tool_name that isn't an MCP call. The first "__" boundary
// after the "mcp__" prefix separates server from tool, so tool names that
// themselves contain "__" stay intact.
func parseMCPTool(toolName string) (server, tool string, ok bool) {
	const prefix = "mcp__"
	if !strings.HasPrefix(toolName, prefix) {
		return "", "", false
	}
	rest := toolName[len(prefix):]
	idx := strings.Index(rest, "__")
	if idx <= 0 || idx == len(rest)-2 {
		// No separator, or empty server / empty tool.
		return "", "", false
	}
	return rest[:idx], rest[idx+2:], true
}

// mcpDisplayName is the canonical form stored in events.name for MCP rows.
// Keeping "server/tool" instead of the raw "mcp__server__tool" makes ranking
// output read nicely and lets the ignore globs use familiar path syntax.
func mcpDisplayName(server, tool string) string {
	return server + "/" + tool
}

// matchMCPIgnore reports whether "server/tool" matches any of the given glob
// patterns. Patterns use path.Match semantics ("*" matches one path segment),
// and a bare "server" pattern is treated as "server/*" so the common case of
// blocking an entire MCP server stays terse in config.toml.
func matchMCPIgnore(server, tool string, patterns []string) bool {
	name := mcpDisplayName(server, tool)
	for _, p := range patterns {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		if !strings.Contains(p, "/") {
			// Server-only shorthand.
			if ok, _ := path.Match(p, server); ok {
				return true
			}
			continue
		}
		if ok, _ := path.Match(p, name); ok {
			return true
		}
	}
	return false
}
