package server

import (
	"fmt"
	"os/exec"
	"strings"
)

// tmuxPaneFocused checks whether a tmux pane is currently visible and active.
// Returns true only if the pane is the active pane in the active window of an
// attached session — i.e., the user is looking at it right now.
// Returns false on any error (pane gone, tmux not running, etc.).
func tmuxPaneFocused(pane string) bool {
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

// tmuxSendKeys sends text to a tmux pane followed by Enter.
func tmuxSendKeys(pane, text string) error {
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
