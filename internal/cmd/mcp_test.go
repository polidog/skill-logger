package cmd

import "testing"

func TestParseMCPTool(t *testing.T) {
	cases := []struct {
		in                string
		wantSvr, wantTool string
		wantOK            bool
	}{
		{"mcp__claude-in-chrome__tabs_context_mcp", "claude-in-chrome", "tabs_context_mcp", true},
		{"mcp__plugin_figma_figma__use_figma", "plugin_figma_figma", "use_figma", true},
		{"mcp__claude_ai_Gmail__create_draft", "claude_ai_Gmail", "create_draft", true},
		{"Skill", "", "", false},
		{"Read", "", "", false},
		{"mcp__noseparator", "", "", false},
		{"mcp____tool", "", "", false},
		{"mcp__server__", "", "", false},
	}
	for _, tc := range cases {
		svr, tool, ok := parseMCPTool(tc.in)
		if ok != tc.wantOK || svr != tc.wantSvr || tool != tc.wantTool {
			t.Errorf("parseMCPTool(%q) = (%q, %q, %v), want (%q, %q, %v)",
				tc.in, svr, tool, ok, tc.wantSvr, tc.wantTool, tc.wantOK)
		}
	}
}

func TestMatchMCPIgnore(t *testing.T) {
	patterns := []string{
		"claude_ai_Gmail",
		"figma/*",
		"*/authenticate",
		"  ", // whitespace-only ignored
	}
	cases := []struct {
		server, tool string
		want         bool
	}{
		{"claude_ai_Gmail", "create_draft", true}, // server shorthand
		{"claude_ai_Gmail", "list_labels", true},  // server shorthand
		{"figma", "get_metadata", true},           // glob
		{"figma", "get_screenshot", true},         // glob
		{"figjam", "anything", false},             // glob doesn't match
		{"slack", "authenticate", true},           // tail glob
		{"slack", "send", false},                  // not matched
	}
	for _, tc := range cases {
		got := matchMCPIgnore(tc.server, tc.tool, patterns)
		if got != tc.want {
			t.Errorf("matchMCPIgnore(%q, %q) = %v, want %v", tc.server, tc.tool, got, tc.want)
		}
	}
}
