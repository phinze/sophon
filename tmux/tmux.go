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

// agentProcess reports whether a process name belongs to a supported agent.
// Process names are commonly truncated by ps (and wrapped on NixOS), so match
// stable fragments rather than exact executable names.
func agentProcess(comm string) bool {
	comm = strings.ToLower(comm)
	return strings.Contains(comm, "claude") ||
		strings.Contains(comm, "codex") ||
		strings.Contains(comm, "antigravity") ||
		comm == "agy" || strings.Contains(comm, ".agy-")
}

// hasAgentDescendant checks if paneShellPID has a supported agent descendant.
func hasAgentDescendant(paneShellPID int, procs []process) bool {
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
		if agentProcess(commByPid[pid]) {
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

// ListAgentPanes returns tmux panes running Claude Code, Codex, or Antigravity.
func ListAgentPanes() (map[string]bool, error) {
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

	return findAgentPanes(string(tmuxOut), string(psOut)), nil
}

// findAgentPanes is the testable core for supported-agent pane discovery.
func findAgentPanes(tmuxOutput, psOutput string) map[string]bool {
	panes := parseTmuxPanes(tmuxOutput)
	procs := parseProcesses(psOutput)

	result := make(map[string]bool)
	for paneID, shellPID := range panes {
		if hasAgentDescendant(shellPID, procs) {
			result[paneID] = true
		}
	}
	return result
}

// ListPaneTitles returns a map of pane ID to pane title for all tmux panes.
func ListPaneTitles() (map[string]string, error) {
	out, err := exec.Command("tmux", "list-panes", "-a", "-F", "#{pane_id}\t#{pane_title}").Output()
	if err != nil {
		return nil, fmt.Errorf("tmux list-panes: %w", err)
	}
	return parsePaneTitles(string(out)), nil
}

// parsePaneTitles parses tab-separated "pane_id\ttitle" lines.
func parsePaneTitles(output string) map[string]string {
	titles := make(map[string]string)
	for _, line := range strings.Split(output, "\n") {
		if line == "" {
			continue
		}
		parts := strings.SplitN(line, "\t", 2)
		if len(parts) != 2 {
			continue
		}
		titles[parts[0]] = parts[1]
	}
	return titles
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
