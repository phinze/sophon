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

func TestHasClaudeDescendant(t *testing.T) {
	procs := []process{
		{pid: 100, ppid: 1, comm: "bash"},     // pane shell
		{pid: 200, ppid: 100, comm: "node"},    // intermediate
		{pid: 300, ppid: 200, comm: "claude"},  // claude is grandchild
		{pid: 400, ppid: 1, comm: "unrelated"},
	}

	if !hasClaudeDescendant(100, procs) {
		t.Error("expected claude descendant of 100")
	}
	if hasClaudeDescendant(400, procs) {
		t.Error("should not find claude descendant of 400")
	}
	if hasClaudeDescendant(999, procs) {
		t.Error("nonexistent PID should have no descendants")
	}
}

func TestHasClaudeDescendantDirectChild(t *testing.T) {
	procs := []process{
		{pid: 100, ppid: 1, comm: "fish"},
		{pid: 200, ppid: 100, comm: "claude"},
	}

	if !hasClaudeDescendant(100, procs) {
		t.Error("expected direct claude child of 100")
	}
}

func TestHasClaudeDescendantWrappedBinary(t *testing.T) {
	// On NixOS, claude's comm name is ".claude-unwrapp" (truncated)
	procs := []process{
		{pid: 100, ppid: 1, comm: "fish"},
		{pid: 200, ppid: 100, comm: ".claude-unwrapp"},
	}

	if !hasClaudeDescendant(100, procs) {
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

func TestFindClaudePanes(t *testing.T) {
	tmuxOutput := "%0 100\n%1 200\n%2 300\n"
	psOutput := `  100     1 bash
  150   100 claude
  200     1 fish
  250   200 vim
  300     1 zsh
  350   300 node
`

	result := findClaudePanes(tmuxOutput, psOutput)
	if !result["%0"] {
		t.Error("pane %0 should have claude")
	}
	if result["%1"] {
		t.Error("pane %1 should not have claude")
	}
	if result["%2"] {
		t.Error("pane %2 should not have claude")
	}
}

func TestFindClaudePanesNoPanes(t *testing.T) {
	result := findClaudePanes("", "  100  1 bash\n")
	if len(result) != 0 {
		t.Errorf("expected empty, got %v", result)
	}
}
