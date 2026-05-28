package cmd

import (
	"encoding/json"
	"reflect"
	"testing"

	"github.com/polidog/agent-tracer/internal/store"
)

func TestExtractCodexMentions(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want []string
	}{
		{"empty", "", nil},
		{"single mention", "use $skill-creator please", []string{"skill-creator"}},
		{"multiple mentions", "run $foo then $bar", []string{"foo", "bar"}},
		{"dedup repeats", "$foo and $foo again", []string{"foo"}},
		{"skip env vars", "echo $PATH and $HOME", nil},
		{"mixed env and skill", "$HOME plus $my-skill", []string{"my-skill"}},
		{"sigil without name", "price is $ and $!", nil},
		{"adjacent punctuation", "($foo).bar", []string{"foo"}},
		{"colon allowed", "$ns:tool", []string{"ns:tool"}},
		{"underscore allowed", "$snake_case", []string{"snake_case"}},
		{"price not a mention", "cost $5 dollars", []string{"5"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := extractCodexMentions(tc.in)
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("got %#v, want %#v", got, tc.want)
			}
		})
	}
}

func TestClassifyClaudeSkillTool(t *testing.T) {
	p := hookPayload{
		HookEventName: "PreToolUse",
		ToolName:      "Skill",
		ToolUseID:     "tu_1",
		ToolInput:     json.RawMessage(`{"skill":"verify"}`),
	}
	res := classify(p, "claude", "", "", nil)
	want := []insertSpec{{kind: store.KindSkill, name: "verify"}}
	if !reflect.DeepEqual(res.inserts, want) {
		t.Fatalf("inserts = %#v, want %#v", res.inserts, want)
	}
	if res.finalize != actionNone {
		t.Fatalf("finalize = %v, want actionNone", res.finalize)
	}
}

func TestClassifySlashCommand(t *testing.T) {
	p := hookPayload{
		HookEventName: "UserPromptSubmit",
		Prompt:        "/review please",
	}
	res := classify(p, "claude", "", "", nil)
	want := []insertSpec{{kind: store.KindCommand, name: "/review"}}
	if !reflect.DeepEqual(res.inserts, want) {
		t.Fatalf("inserts = %#v, want %#v", res.inserts, want)
	}
}

func TestClassifyCodexMentionAsSkill(t *testing.T) {
	p := hookPayload{
		HookEventName: "UserPromptSubmit",
		Prompt:        "Please use $skill-creator and $verify",
	}
	res := classify(p, "codex", "", "", nil)
	want := []insertSpec{
		{kind: store.KindSkill, name: "skill-creator"},
		{kind: store.KindSkill, name: "verify"},
	}
	if !reflect.DeepEqual(res.inserts, want) {
		t.Fatalf("inserts = %#v, want %#v", res.inserts, want)
	}
}

func TestClassifyCodexSlashAndMention(t *testing.T) {
	p := hookPayload{
		HookEventName: "UserPromptSubmit",
		Prompt:        "/plan and also $verify",
	}
	res := classify(p, "codex", "", "", nil)
	want := []insertSpec{
		{kind: store.KindCommand, name: "/plan"},
		{kind: store.KindSkill, name: "verify"},
	}
	if !reflect.DeepEqual(res.inserts, want) {
		t.Fatalf("inserts = %#v, want %#v", res.inserts, want)
	}
}

func TestClassifyClaudeIgnoresMentions(t *testing.T) {
	// Plain `$word` in a Claude prompt should NOT be treated as a skill
	// (Codex syntax doesn't apply to Claude Code sessions).
	p := hookPayload{
		HookEventName: "UserPromptSubmit",
		Prompt:        "look at $my-skill thanks",
	}
	res := classify(p, "claude", "", "", nil)
	if len(res.inserts) != 0 {
		t.Fatalf("expected no inserts for Claude prompt, got %#v", res.inserts)
	}
}

func TestClassifyPostToolUseFinalize(t *testing.T) {
	p := hookPayload{
		HookEventName: "PostToolUse",
		ToolName:      "Skill",
		ToolUseID:     "tu_1",
	}
	res := classify(p, "claude", "", "", nil)
	if res.finalize != actionFinishTool {
		t.Fatalf("finalize = %v, want actionFinishTool", res.finalize)
	}
	if len(res.inserts) != 0 {
		t.Fatalf("expected no inserts, got %#v", res.inserts)
	}
}

func TestClassifyStopFinalize(t *testing.T) {
	p := hookPayload{HookEventName: "Stop"}
	res := classify(p, "claude", "", "", nil)
	if res.finalize != actionFinishTurn {
		t.Fatalf("finalize = %v, want actionFinishTurn", res.finalize)
	}
}

func TestClassifyMCPTool(t *testing.T) {
	p := hookPayload{
		HookEventName: "PreToolUse",
		ToolName:      "mcp__claude-in-chrome__tabs_context_mcp",
		ToolUseID:     "tu_mcp",
	}
	res := classify(p, "claude", "", "", nil)
	want := []insertSpec{{kind: store.KindMCP, name: "claude-in-chrome/tabs_context_mcp"}}
	if !reflect.DeepEqual(res.inserts, want) {
		t.Fatalf("inserts = %#v, want %#v", res.inserts, want)
	}
}

func TestClassifyMCPIgnoreServerShorthand(t *testing.T) {
	p := hookPayload{
		HookEventName: "PreToolUse",
		ToolName:      "mcp__claude_ai_Gmail__create_draft",
		ToolUseID:     "tu_gmail",
	}
	res := classify(p, "claude", "", "", []string{"claude_ai_Gmail"})
	if len(res.inserts) != 0 {
		t.Fatalf("expected ignore via server shorthand, got %#v", res.inserts)
	}
}

func TestClassifyMCPIgnoreGlob(t *testing.T) {
	cases := []struct {
		tool    string
		ignore  []string
		blocked bool
	}{
		{"mcp__figma__get_metadata", []string{"figma/*"}, true},
		{"mcp__figma__get_metadata", []string{"figjam/*"}, false},
		{"mcp__claude_ai_Slack__authenticate", []string{"*/authenticate"}, true},
		{"mcp__claude_ai_Slack__list_channels", []string{"*/authenticate"}, false},
	}
	for _, tc := range cases {
		p := hookPayload{
			HookEventName: "PreToolUse",
			ToolName:      tc.tool,
			ToolUseID:     "tu_x",
		}
		res := classify(p, "claude", "", "", tc.ignore)
		gotBlocked := len(res.inserts) == 0
		if gotBlocked != tc.blocked {
			t.Errorf("tool=%s ignore=%v: blocked=%v want %v", tc.tool, tc.ignore, gotBlocked, tc.blocked)
		}
	}
}

func TestClassifyMCPPostToolUseFinalize(t *testing.T) {
	p := hookPayload{
		HookEventName: "PostToolUse",
		ToolName:      "mcp__claude-in-chrome__navigate",
		ToolUseID:     "tu_nav",
	}
	res := classify(p, "claude", "", "", nil)
	if res.finalize != actionFinishTool {
		t.Fatalf("finalize = %v, want actionFinishTool", res.finalize)
	}
}

func TestClassifyOverrides(t *testing.T) {
	p := hookPayload{HookEventName: "UserPromptSubmit", Prompt: "anything"}
	res := classify(p, "claude", "command", "manual-cmd", nil)
	want := []insertSpec{{kind: store.KindCommand, name: "manual-cmd"}}
	if !reflect.DeepEqual(res.inserts, want) {
		t.Fatalf("inserts = %#v, want %#v", res.inserts, want)
	}
}
