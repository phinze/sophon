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

	// Use tmux send-keys to type the text and press Enter
	cmd := exec.Command("tmux", "send-keys", "-t", pane, text, "Enter")
	return cmd.Run()
}
