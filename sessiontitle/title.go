// Package sessiontitle turns terminal-agent pane titles into stable labels for
// Sophon's UI and notifications.
package sessiontitle

import "strings"

// Parse peels an agent state glyph from a tmux pane title and filters the
// generic titles shown before an agent has named its task. Claude Code uses a
// resting star or an animated braille frame; Codex and Antigravity currently
// publish plain titles.
func Parse(title string) string {
	title = strings.TrimSpace(title)
	runes := []rune(title)
	if len(runes) > 0 && (runes[0] == '✳' || (runes[0] >= 0x2800 && runes[0] <= 0x28ff)) {
		title = strings.TrimSpace(string(runes[1:]))
	}

	switch title {
	case "Claude Code", "Codex", "Antigravity":
		return ""
	default:
		return title
	}
}
