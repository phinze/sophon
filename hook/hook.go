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
}

// Config holds hook configuration.
type Config struct {
	DaemonURL     string
	NtfyURL       string
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
		// Unknown event, ignore
		return nil
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
	project := projectFromCwd(event.Cwd)

	var title string
	switch event.NotificationType {
	case "permission_prompt":
		title = fmt.Sprintf("[%s] Needs approval", project)
	default:
		title = fmt.Sprintf("[%s] Waiting for input", project)
	}

	body := map[string]interface{}{
		"notification_type": event.NotificationType,
		"title":             title,
		"message":           event.Message,
		"cwd":               event.Cwd,
		"node_name":         cfg.NodeName,
	}

	err := postJSON(cfg.DaemonURL+"/api/sessions/"+event.SessionID+"/notify", body)
	if err != nil {
		// Fallback: send ntfy directly if daemon is unreachable
		return sendNtfyDirect(cfg, event)
	}
	return nil
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

// sendNtfyDirect sends a notification directly to ntfy as a fallback.
func sendNtfyDirect(cfg Config, event HookEvent) error {
	if cfg.NtfyURL == "" {
		return nil
	}

	project := projectFromCwd(event.Cwd)

	var title, priority, tags string
	switch event.NotificationType {
	case "permission_prompt":
		title = fmt.Sprintf("[%s] Needs approval", project)
		priority = "high"
		tags = "lock"
	default:
		title = fmt.Sprintf("[%s] Waiting for input", project)
		priority = "default"
		tags = "hourglass_flowing_sand"
	}

	req, err := http.NewRequest("POST", cfg.NtfyURL, strings.NewReader(event.Message))
	if err != nil {
		return err
	}
	req.Header.Set("Title", title)
	req.Header.Set("Priority", priority)
	req.Header.Set("Tags", tags)

	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		// Best effort - don't fail the hook
		return nil
	}
	resp.Body.Close()
	return nil
}

func projectFromCwd(cwd string) string {
	parts := strings.Split(strings.TrimRight(cwd, "/"), "/")
	if len(parts) >= 2 {
		return parts[len(parts)-2] + "/" + parts[len(parts)-1]
	}
	if len(parts) == 1 {
		return parts[0]
	}
	return "unknown"
}
