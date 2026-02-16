package agent

import (
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func newTestAgent(t *testing.T) *Agent {
	t.Helper()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	cfg := Config{
		ClaudeDir: t.TempDir(),
		NodeName:  "test-node",
	}
	a := New(cfg, logger)
	return a
}

func TestHealthEndpoint(t *testing.T) {
	a := newTestAgent(t)
	req := httptest.NewRequest("GET", "/api/health", nil)
	w := httptest.NewRecorder()
	a.handleHealth(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("got %d, want 200", w.Code)
	}
	if body := strings.TrimSpace(w.Body.String()); body != "ok" {
		t.Errorf("body = %q, want %q", body, "ok")
	}
}

func TestPaneFocusedEndpoint(t *testing.T) {
	a := newTestAgent(t)
	a.paneFocused = func(pane string) bool { return pane == "%5" }

	req := httptest.NewRequest("GET", "/api/pane-focused?pane=%255", nil)
	w := httptest.NewRecorder()
	a.handlePaneFocused(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("got %d, want 200", w.Code)
	}

	var result struct {
		Focused bool `json:"focused"`
	}
	json.NewDecoder(w.Body).Decode(&result)
	if !result.Focused {
		t.Error("expected focused=true")
	}
}

func TestSendKeysEndpoint(t *testing.T) {
	a := newTestAgent(t)
	var sentPane, sentText string
	a.sendKeys = func(pane, text string) error {
		sentPane = pane
		sentText = text
		return nil
	}

	body := strings.NewReader(`{"pane":"%5","text":"hello"}`)
	req := httptest.NewRequest("POST", "/api/send-keys", body)
	w := httptest.NewRecorder()
	a.handleSendKeys(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("got %d, want 200", w.Code)
	}
	if sentPane != "%5" {
		t.Errorf("pane = %q, want %%5", sentPane)
	}
	if sentText != "hello" {
		t.Errorf("text = %q, want hello", sentText)
	}
}

func TestSendKeysEndpointError(t *testing.T) {
	a := newTestAgent(t)
	a.sendKeys = func(pane, text string) error {
		return fmt.Errorf("tmux not running")
	}

	body := strings.NewReader(`{"pane":"%5","text":"hello"}`)
	req := httptest.NewRequest("POST", "/api/send-keys", body)
	w := httptest.NewRecorder()
	a.handleSendKeys(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("got %d, want 500", w.Code)
	}
}

func TestTranscriptEndpoint(t *testing.T) {
	a := newTestAgent(t)

	// Create a JSONL transcript file
	cwd := "/home/user/project"
	sessionID := "test-sess"
	// slug: /home/user/project -> -home-user-project
	projectDir := filepath.Join(a.cfg.ClaudeDir, "projects", "-home-user-project")
	os.MkdirAll(projectDir, 0o755)
	jsonlPath := filepath.Join(projectDir, sessionID+".jsonl")
	jsonl := `{"type":"user","timestamp":"2026-01-01T00:00:00.000Z","message":{"role":"user","content":"Hello"}}
{"type":"assistant","timestamp":"2026-01-01T00:00:01.000Z","message":{"role":"assistant","content":[{"type":"text","text":"Hi there!"}]}}
`
	os.WriteFile(jsonlPath, []byte(jsonl), 0o644)

	req := httptest.NewRequest("GET", "/api/transcript/"+sessionID+"?cwd="+cwd, nil)
	req.SetPathValue("session_id", sessionID)
	w := httptest.NewRecorder()
	a.handleTranscript(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("got %d, want 200", w.Code)
	}

	var result struct {
		Messages []struct {
			Role   string `json:"role"`
			Blocks []struct {
				Type string `json:"type"`
				Text string `json:"text"`
			} `json:"blocks"`
		} `json:"messages"`
	}
	json.NewDecoder(w.Body).Decode(&result)
	if len(result.Messages) != 2 {
		t.Fatalf("got %d messages, want 2", len(result.Messages))
	}
	if result.Messages[1].Blocks[0].Text != "Hi there!" {
		t.Errorf("msg 1 text = %q", result.Messages[1].Blocks[0].Text)
	}
}

func TestTranscriptEndpointMissingFile(t *testing.T) {
	a := newTestAgent(t)

	req := httptest.NewRequest("GET", "/api/transcript/nonexistent?cwd=/tmp/test", nil)
	req.SetPathValue("session_id", "nonexistent")
	w := httptest.NewRecorder()
	a.handleTranscript(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("got %d, want 200", w.Code)
	}

	var result struct {
		Messages []any `json:"messages"`
	}
	json.NewDecoder(w.Body).Decode(&result)
	if result.Messages != nil {
		t.Errorf("expected null/empty messages, got %v", result.Messages)
	}
}
