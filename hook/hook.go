package hook

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"
)

// HookEvent represents the JSON input from Claude Code hooks.
type HookEvent struct {
	HookEventName    string `json:"hook_event_name"`
	SessionID        string `json:"session_id"`
	Cwd              string `json:"cwd"`
	NotificationType string `json:"notification_type"`
	Message          string `json:"message"`
	ToolName         string `json:"tool_name"`
}

// Config holds hook configuration.
type Config struct {
	DaemonURL     string
	NodeName      string
	MinSessionAge int
}

// Run reads a hook event from stdin and forwards it to the daemon.
func Run(cfg Config) error {
	input, err := io.ReadAll(os.Stdin)
	if err != nil {
		return fmt.Errorf("reading stdin: %w", err)
	}

	var event HookEvent
	if err := json.Unmarshal(input, &event); err != nil {
		return fmt.Errorf("parsing hook JSON: %w", err)
	}

	// Try to get the tmux pane from the environment
	tmuxPane := os.Getenv("TMUX_PANE")

	switch event.HookEventName {
	case "SessionStart":
		return handleSessionStart(cfg, event, tmuxPane)
	case "Notification":
		return handleNotification(cfg, event)
	case "Stop":
		return handleTurnEnd(cfg, event)
	case "SessionEnd":
		return handleSessionEnd(cfg, event)
	default:
		return handleToolActivity(cfg, event)
	}
}

func handleSessionStart(cfg Config, event HookEvent, tmuxPane string) error {
	body := map[string]interface{}{
		"session_id": event.SessionID,
		"tmux_pane":  tmuxPane,
		"cwd":        event.Cwd,
		"node_name":  cfg.NodeName,
	}
	return postJSON(cfg.DaemonURL+"/api/sessions", body)
}

func handleNotification(cfg Config, event HookEvent) error {
	repo := repoFromCwd(event.Cwd)

	var title, message string
	switch event.NotificationType {
	case "permission_prompt":
		title = repo + " · Needs approval"
		message = event.Message // keep actual permission details
	default:
		title = repo + " · Waiting for input"
		message = "" // suppress generic "Claude is waiting for your input"
	}

	body := map[string]interface{}{
		"notification_type": event.NotificationType,
		"title":             title,
		"message":           message,
		"cwd":               event.Cwd,
		"node_name":         cfg.NodeName,
	}

	return postJSON(cfg.DaemonURL+"/api/sessions/"+event.SessionID+"/notify", body)
}

func handleTurnEnd(cfg Config, event HookEvent) error {
	body := map[string]interface{}{
		"node_name": cfg.NodeName,
	}
	err := postJSON(cfg.DaemonURL+"/api/sessions/"+event.SessionID+"/activity", body)
	if err != nil {
		// Daemon down, nothing to do for turn end
		return nil
	}
	return nil
}

func handleSessionEnd(cfg Config, event HookEvent) error {
	client := &http.Client{Timeout: 5 * time.Second}
	url := cfg.DaemonURL + "/api/sessions/" + event.SessionID
	if cfg.NodeName != "" {
		url += "?node_name=" + cfg.NodeName
	}
	req, err := http.NewRequest("DELETE", url, nil)
	if err != nil {
		return err
	}
	resp, err := client.Do(req)
	if err != nil {
		// Daemon down, nothing to do for session end
		return nil
	}
	resp.Body.Close()
	return nil
}

func handleToolActivity(cfg Config, event HookEvent) error {
	body := map[string]interface{}{
		"hook_event_name": event.HookEventName,
		"tool_name":       event.ToolName,
		"node_name":       cfg.NodeName,
	}
	err := postJSON(cfg.DaemonURL+"/api/sessions/"+event.SessionID+"/tool-activity", body)
	if err != nil {
		// Daemon down, nothing to do for tool activity
		return nil
	}
	return nil
}

func postJSON(url string, body interface{}) error {
	data, err := json.Marshal(body)
	if err != nil {
		return err
	}

	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Post(url, "application/json", bytes.NewReader(data))
	if err != nil {
		return err
	}
	resp.Body.Close()

	if resp.StatusCode >= 400 {
		return fmt.Errorf("daemon returned %d", resp.StatusCode)
	}
	return nil
}

// repoFromCwd returns just the last path component (repo name) for compact display.
func repoFromCwd(cwd string) string {
	parts := strings.Split(strings.TrimRight(cwd, "/"), "/")
	if len(parts) >= 1 && parts[len(parts)-1] != "" {
		return parts[len(parts)-1]
	}
	return "unknown"
}
