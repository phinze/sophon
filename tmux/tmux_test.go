package tmux

import "testing"

func TestParseProcesses(t *testing.T) {
	input := `    1     0 systemd
  100     1 bash
  200   100 claude
  300     1 fish
`
	procs := parseProcesses(input)
	if len(procs) != 4 {
		t.Fatalf("got %d processes, want 4", len(procs))
	}
	if procs[2].pid != 200 || procs[2].ppid != 100 || procs[2].comm != "claude" {
		t.Errorf("procs[2] = %+v", procs[2])
	}
}

func TestParseProcessesEmpty(t *testing.T) {
	procs := parseProcesses("")
	if len(procs) != 0 {
		t.Errorf("expected empty, got %v", procs)
	}
}

func TestHasAgentDescendant(t *testing.T) {
	procs := []process{
		{pid: 100, ppid: 1, comm: "bash"},     // pane shell
		{pid: 200, ppid: 100, comm: "node"},   // intermediate
		{pid: 300, ppid: 200, comm: "claude"}, // claude is grandchild
		{pid: 400, ppid: 1, comm: "unrelated"},
	}

	if !hasAgentDescendant(100, procs) {
		t.Error("expected claude descendant of 100")
	}
	if hasAgentDescendant(400, procs) {
		t.Error("should not find claude descendant of 400")
	}
	if hasAgentDescendant(999, procs) {
		t.Error("nonexistent PID should have no descendants")
	}
}

func TestHasAgentDescendantDirectChild(t *testing.T) {
	procs := []process{
		{pid: 100, ppid: 1, comm: "fish"},
		{pid: 200, ppid: 100, comm: "claude"},
	}

	if !hasAgentDescendant(100, procs) {
		t.Error("expected direct claude child of 100")
	}
}

func TestHasAgentDescendantWrappedBinary(t *testing.T) {
	// On NixOS, claude's comm name is ".claude-unwrapp" (truncated)
	procs := []process{
		{pid: 100, ppid: 1, comm: "fish"},
		{pid: 200, ppid: 100, comm: ".claude-unwrapp"},
	}

	if !hasAgentDescendant(100, procs) {
		t.Error("expected wrapped claude descendant of 100")
	}
}

func TestParseTmuxPanes(t *testing.T) {
	input := "%0 100\n%1 200\n%5 500\n"
	panes := parseTmuxPanes(input)
	if len(panes) != 3 {
		t.Fatalf("got %d panes, want 3", len(panes))
	}
	if panes["%0"] != 100 {
		t.Errorf("%%0 pid = %d, want 100", panes["%0"])
	}
	if panes["%5"] != 500 {
		t.Errorf("%%5 pid = %d, want 500", panes["%5"])
	}
}

func TestFindAgentPanes(t *testing.T) {
	tmuxOutput := "%0 100\n%1 200\n%2 300\n%3 400\n"
	psOutput := `  100     1 bash
  150   100 claude
  200     1 fish
  250   200 codex
  300     1 zsh
  350   300 .agy-wrapped
  400     1 bash
  450   400 vim
`

	result := findAgentPanes(tmuxOutput, psOutput)
	if !result["%0"] {
		t.Error("pane %0 should have claude")
	}
	if !result["%1"] {
		t.Error("pane %1 should have codex")
	}
	if !result["%2"] {
		t.Error("pane %2 should have antigravity")
	}
	if result["%3"] {
		t.Error("pane %3 should not have an agent")
	}
}

func TestFindAgentPanesNoPanes(t *testing.T) {
	result := findAgentPanes("", "  100  1 bash\n")
	if len(result) != 0 {
		t.Errorf("expected empty, got %v", result)
	}
}

func TestParsePaneTitles(t *testing.T) {
	input := "%0\t✳ Migrate blog to Miren\n%1\tfish\n%5\t⠐ Display pane titles in Sophon\n"
	titles := parsePaneTitles(input)
	if len(titles) != 3 {
		t.Fatalf("got %d titles, want 3", len(titles))
	}
	if titles["%0"] != "✳ Migrate blog to Miren" {
		t.Errorf("%%0 title = %q", titles["%0"])
	}
	if titles["%5"] != "⠐ Display pane titles in Sophon" {
		t.Errorf("%%5 title = %q", titles["%5"])
	}
}

func TestParsePaneTitlesEmpty(t *testing.T) {
	titles := parsePaneTitles("")
	if len(titles) != 0 {
		t.Errorf("expected empty, got %v", titles)
	}
}
