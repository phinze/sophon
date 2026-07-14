package sessiontitle

import "testing"

func TestParse(t *testing.T) {
	tests := []struct {
		name  string
		title string
		want  string
	}{
		{name: "claude resting", title: "✳ Replace emoji in story", want: "Replace emoji in story"},
		{name: "claude working", title: "⠂ Add non-rig tmux sessions to radar", want: "Add non-rig tmux sessions to radar"},
		{name: "codex task", title: "Audit reviewagent verbosity", want: "Audit reviewagent verbosity"},
		{name: "claude placeholder", title: "✳ Claude Code"},
		{name: "codex placeholder", title: "Codex"},
		{name: "antigravity placeholder", title: "Antigravity"},
		{name: "whitespace", title: "  A tidy title  ", want: "A tidy title"},
		{name: "empty"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := Parse(tt.title); got != tt.want {
				t.Errorf("Parse(%q) = %q, want %q", tt.title, got, tt.want)
			}
		})
	}
}
