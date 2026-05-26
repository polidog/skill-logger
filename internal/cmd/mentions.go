package cmd

// Codex skills are not invoked as tool calls; they appear as `$skill-name`
// mentions inside the UserPromptSubmit prompt. The character class and
// env-var skip list mirror codex-rs/core-skills/src/injection.rs so we
// recognize the same mentions Codex's own injector would resolve.

// extractCodexMentions returns every `$<name>` mention in text, preserving
// first-seen order and de-duplicating repeats. Common shell env-var names
// (PATH, HOME, …) are skipped so things like `$PATH` don't show up as skills.
func extractCodexMentions(text string) []string {
	const sigil = '$'
	b := []byte(text)
	seen := make(map[string]struct{})
	var out []string
	for i := 0; i < len(b); i++ {
		if rune(b[i]) != sigil {
			continue
		}
		start := i + 1
		if start >= len(b) || !isMentionNameByte(b[start]) {
			continue
		}
		end := start + 1
		for end < len(b) && isMentionNameByte(b[end]) {
			end++
		}
		name := string(b[start:end])
		i = end - 1
		if isCommonEnvVar(name) {
			continue
		}
		if _, ok := seen[name]; ok {
			continue
		}
		seen[name] = struct{}{}
		out = append(out, name)
	}
	return out
}

func isMentionNameByte(c byte) bool {
	switch {
	case c >= 'a' && c <= 'z':
		return true
	case c >= 'A' && c <= 'Z':
		return true
	case c >= '0' && c <= '9':
		return true
	case c == '_' || c == '-' || c == ':':
		return true
	}
	return false
}

// isCommonEnvVar mirrors the codex-rs skip list so we don't treat shell
// variables like $PATH or $HOME as skill mentions.
func isCommonEnvVar(name string) bool {
	upper := make([]byte, len(name))
	for i := 0; i < len(name); i++ {
		c := name[i]
		if c >= 'a' && c <= 'z' {
			c -= 'a' - 'A'
		}
		upper[i] = c
	}
	switch string(upper) {
	case "PATH", "HOME", "USER", "SHELL", "PWD", "LANG", "TERM", "EDITOR",
		"PAGER", "DISPLAY", "TMPDIR", "TMP", "TEMP", "HOSTNAME", "LOGNAME",
		"MAIL", "OLDPWD", "PS1", "PS2", "IFS":
		return true
	}
	return false
}
