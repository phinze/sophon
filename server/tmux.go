package server

import (
	"fmt"
	"os/exec"
)

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
