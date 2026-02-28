package tmux

import (
	"fmt"
	"os/exec"
	"strconv"
	"strings"
)

// PaneFocused checks whether a tmux pane is currently visible and active.
// Returns true only if the pane is the active pane in the active window of an
// attached session — i.e., the user is looking at it right now.
// Returns false on any error (pane gone, tmux not running, etc.).
func PaneFocused(pane string) bool {
	if pane == "" {
		return false
	}
	cmd := exec.Command("tmux", "display-message", "-t", pane, "-p",
		"#{pane_active} #{window_active} #{session_attached}")
	output, err := cmd.Output()
	if err != nil {
		return false
	}
	fields := strings.Fields(strings.TrimSpace(string(output)))
	if len(fields) != 3 {
		return false
	}
	return fields[0] == "1" && fields[1] == "1" && fields[2] != "0"
}

// process holds parsed process info from ps output.
type process struct {
	pid  int
	ppid int
	comm string
}

// parseProcesses parses `ps -eo pid=,ppid=,comm=` output into a slice of processes.
func parseProcesses(output string) []process {
	var procs []process
	for _, line := range strings.Split(output, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 3 {
			continue
		}
		pid, err := strconv.Atoi(fields[0])
		if err != nil {
			continue
		}
		ppid, err := strconv.Atoi(fields[1])
		if err != nil {
			continue
		}
		comm := fields[2]
		procs = append(procs, process{pid: pid, ppid: ppid, comm: comm})
	}
	return procs
}

// hasClaudeDescendant checks if paneShellPID has a descendant process named "claude".
func hasClaudeDescendant(paneShellPID int, procs []process) bool {
	// Build children map
	children := make(map[int][]int)
	commByPid := make(map[int]string)
	for _, p := range procs {
		children[p.ppid] = append(children[p.ppid], p.pid)
		commByPid[p.pid] = p.comm
	}

	// BFS from paneShellPID
	queue := children[paneShellPID]
	for len(queue) > 0 {
		pid := queue[0]
		queue = queue[1:]
		if commByPid[pid] == "claude" {
			return true
		}
		queue = append(queue, children[pid]...)
	}
	return false
}

// parseTmuxPanes parses `tmux list-panes -a -F "#{pane_id} #{pane_pid}"` output.
func parseTmuxPanes(output string) map[string]int {
	panes := make(map[string]int)
	for _, line := range strings.Split(output, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) != 2 {
			continue
		}
		pid, err := strconv.Atoi(fields[1])
		if err != nil {
			continue
		}
		panes[fields[0]] = pid
	}
	return panes
}

// ListClaudePanes returns the set of tmux pane IDs that have a running claude process.
func ListClaudePanes() (map[string]bool, error) {
	// Get all tmux panes with their shell PIDs
	tmuxOut, err := exec.Command("tmux", "list-panes", "-a", "-F", "#{pane_id} #{pane_pid}").Output()
	if err != nil {
		return nil, fmt.Errorf("tmux list-panes: %w", err)
	}

	// Get full process tree
	psOut, err := exec.Command("ps", "-eo", "pid=,ppid=,comm=").Output()
	if err != nil {
		return nil, fmt.Errorf("ps: %w", err)
	}

	return findClaudePanes(string(tmuxOut), string(psOut)), nil
}

// findClaudePanes is the testable core: given tmux and ps output, returns pane IDs with claude.
func findClaudePanes(tmuxOutput, psOutput string) map[string]bool {
	panes := parseTmuxPanes(tmuxOutput)
	procs := parseProcesses(psOutput)

	result := make(map[string]bool)
	for paneID, shellPID := range panes {
		if hasClaudeDescendant(shellPID, procs) {
			result[paneID] = true
		}
	}
	return result
}

// SendKeys sends text to a tmux pane followed by Enter.
func SendKeys(pane, text string) error {
	if pane == "" {
		return fmt.Errorf("no tmux pane specified for session")
	}

	// Send the text literally (-l prevents interpreting key names in the text)
	cmd := exec.Command("tmux", "send-keys", "-t", pane, "-l", text)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("sending text: %w: %s", err, string(output))
	}

	// Then send Enter as a key press
	cmd = exec.Command("tmux", "send-keys", "-t", pane, "Enter")
	output, err = cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("sending Enter: %w: %s", err, string(output))
	}
	return nil
}
