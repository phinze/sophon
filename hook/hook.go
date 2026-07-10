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
	HookEventName    string          `json:"hook_event_name"`
	SessionID        string          `json:"session_id"`
	Cwd              string          `json:"cwd"`
	NotificationType string          `json:"notification_type"`
	Message          string          `json:"message"`
	ToolName         string          `json:"tool_name"`
	ToolInput        json.RawMessage `json:"tool_input"`
	TranscriptPath   string          `json:"transcript_path"`

	// Antigravity uses a separate, camelCase hook contract. These fields are
	// normalized into the Claude/Codex-shaped fields above before dispatch.
	ConversationID      string   `json:"conversationId"`
	WorkspacePaths      []string `json:"workspacePaths"`
	AGTranscriptPath    string   `json:"transcriptPath"`
	InvocationNum       int      `json:"invocationNum"`
	ExecutionNum        int      `json:"executionNum"`
	TerminationReason   string   `json:"terminationReason"`
	FullyIdle           bool     `json:"fullyIdle"`
	AntigravityToolCall struct {
		Name string          `json:"name"`
		Args json.RawMessage `json:"args"`
	} `json:"toolCall"`
}

// Config holds hook configuration.
type Config struct {
	DaemonURL     string
	NodeName      string
	MinSessionAge int
	Provider      string
	EventName     string
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
	event = normalizeEvent(cfg, event)

	// Try to get the tmux pane from the environment
	tmuxPane := os.Getenv("TMUX_PANE")

	switch event.HookEventName {
	case "SessionStart":
		// Antigravity invokes PreInvocation before every model call. Invocation
		// zero is the conversation start; subsequent calls are activity only.
		if event.ConversationID != "" && event.InvocationNum != 0 {
			return handleToolActivity(cfg, event)
		}
		return handleSessionStart(cfg, event, tmuxPane)
	case "Notification":
		return handleNotification(cfg, event)
	case "PermissionRequest":
		return handlePermissionRequest(cfg, event)
	case "Stop":
		return handleTurnEnd(cfg, event)
	case "SessionEnd":
		return handleSessionEnd(cfg, event)
	case "PreToolUse":
		return handlePreToolUse(cfg, event)
	default:
		return handleToolActivity(cfg, event)
	}
}

// normalizeEvent adapts Antigravity's camelCase lifecycle payload to the
// Claude-compatible schema shared by Claude Code and Codex.
func normalizeEvent(cfg Config, event HookEvent) HookEvent {
	if cfg.Provider != "antigravity" && event.ConversationID == "" {
		return event
	}

	event.SessionID = event.ConversationID
	event.TranscriptPath = event.AGTranscriptPath
	if len(event.WorkspacePaths) > 0 {
		event.Cwd = event.WorkspacePaths[0]
	}
	event.HookEventName = cfg.EventName
	switch cfg.EventName {
	case "PreInvocation":
		event.HookEventName = "SessionStart"
	case "PostInvocation":
		event.HookEventName = "PostToolUse"
	case "Stop":
		event.HookEventName = "Stop"
	case "PreToolUse", "PostToolUse":
		event.ToolName = event.AntigravityToolCall.Name
		event.ToolInput = event.AntigravityToolCall.Args
	}
	return event
}

// handlePreToolUse forwards the plan to the daemon when Claude is about to exit
// plan mode (the plan markdown lives in the tool input), then records tool
// activity like any other tool call.
func handlePreToolUse(cfg Config, event HookEvent) error {
	if event.ToolName == "ExitPlanMode" {
		// Best-effort: a failed plan post shouldn't drop the activity event.
		_ = handlePlan(cfg, event)
	}
	return handleToolActivity(cfg, event)
}

func handlePlan(cfg Config, event HookEvent) error {
	var input struct {
		Plan string `json:"plan"`
	}
	if len(event.ToolInput) > 0 {
		_ = json.Unmarshal(event.ToolInput, &input)
	}
	if input.Plan == "" {
		return nil
	}
	body := map[string]interface{}{
		"plan":      input.Plan,
		"node_name": cfg.NodeName,
	}
	return postJSON(cfg.DaemonURL+"/api/sessions/"+event.SessionID+"/plan", body)
}

func handleSessionStart(cfg Config, event HookEvent, tmuxPane string) error {
	body := map[string]interface{}{
		"session_id":      event.SessionID,
		"tmux_pane":       tmuxPane,
		"cwd":             event.Cwd,
		"node_name":       cfg.NodeName,
		"transcript_path": event.TranscriptPath,
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

func handlePermissionRequest(cfg Config, event HookEvent) error {
	repo := repoFromCwd(event.Cwd)
	message := event.ToolName
	if message == "" {
		message = "Codex is waiting for approval"
	}
	body := map[string]interface{}{
		"notification_type": "permission_prompt",
		"title":             repo + " · Needs approval",
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
