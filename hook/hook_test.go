package hook

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestNormalizeAntigravityEvent(t *testing.T) {
	var event HookEvent
	err := json.Unmarshal([]byte(`{
		"conversationId":"conversation-1",
		"workspacePaths":["/workspace/project"],
		"transcriptPath":"/tmp/transcript.jsonl",
		"invocationNum":0
	}`), &event)
	if err != nil {
		t.Fatal(err)
	}

	event = normalizeEvent(Config{Provider: "antigravity", EventName: "PreInvocation"}, event)
	if event.HookEventName != "SessionStart" {
		t.Errorf("HookEventName = %q, want SessionStart", event.HookEventName)
	}
	if event.SessionID != "conversation-1" || event.Cwd != "/workspace/project" {
		t.Errorf("normalized event = %+v", event)
	}
	if event.TranscriptPath != "/tmp/transcript.jsonl" {
		t.Errorf("TranscriptPath = %q", event.TranscriptPath)
	}
}

func TestNormalizeAntigravityToolEvent(t *testing.T) {
	var event HookEvent
	if err := json.Unmarshal([]byte(`{
		"conversationId":"conversation-1",
		"toolCall":{"name":"run_command","args":{"CommandLine":"go test ./..."}}
	}`), &event); err != nil {
		t.Fatal(err)
	}

	event = normalizeEvent(Config{Provider: "antigravity", EventName: "PreToolUse"}, event)
	if event.ToolName != "run_command" || string(event.ToolInput) != `{"CommandLine":"go test ./..."}` {
		t.Errorf("normalized tool = %q %s", event.ToolName, event.ToolInput)
	}
}

func TestPermissionRequestPostsNotification(t *testing.T) {
	var path string
	var body map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path = r.URL.Path
		defer r.Body.Close()
		data, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(data, &body)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	err := handlePermissionRequest(Config{DaemonURL: server.URL, NodeName: "node-1"}, HookEvent{
		SessionID: "session-1",
		Cwd:       "/workspace/project",
		ToolName:  "functions.exec",
	})
	if err != nil {
		t.Fatal(err)
	}
	if path != "/api/sessions/session-1/notify" {
		t.Errorf("path = %q", path)
	}
	if body["notification_type"] != "permission_prompt" || body["message"] != "functions.exec" {
		t.Errorf("body = %#v", body)
	}
}
