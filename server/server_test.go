package server

import (
	"bytes"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/phinze/sophon/store"
)

// testHarness sets up a Server with an in-memory store, a mock ntfy endpoint,
// and injectable tmux stubs.
type testHarness struct {
	server    *Server
	store     *store.Store
	ntfyReqs  []*http.Request
	ntfyBodies []string
	focused   bool // what paneFocused returns
	sentKeys  []string
}

func newTestHarness(t *testing.T) *testHarness {
	t.Helper()

	st, err := store.Open(":memory:")
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	t.Cleanup(func() { st.Close() })

	h := &testHarness{store: st}

	ntfySrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		h.ntfyReqs = append(h.ntfyReqs, r)
		h.ntfyBodies = append(h.ntfyBodies, string(body))
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(ntfySrv.Close)

	cfg := Config{
		NtfyURL:       ntfySrv.URL,
		BaseURL:       "https://example.com",
		MinSessionAge: 120,
	}

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	h.server = New(cfg, st, logger)
	h.server.paneFocused = func(pane string) bool { return h.focused }
	h.server.sendKeys = func(pane, text string) error {
		h.sentKeys = append(h.sentKeys, text)
		return nil
	}

	return h
}

func (h *testHarness) createSession(t *testing.T, id, pane, cwd string) {
	t.Helper()
	body, _ := json.Marshal(map[string]string{
		"session_id": id,
		"tmux_pane":  pane,
		"cwd":        cwd,
	})
	req := httptest.NewRequest("POST", "/api/sessions", bytes.NewReader(body))
	w := httptest.NewRecorder()
	h.server.handleCreateSession(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("createSession: got %d, want %d", w.Code, http.StatusCreated)
	}
}

func (h *testHarness) notify(t *testing.T, id, notifType, message string) int {
	t.Helper()
	body, _ := json.Marshal(map[string]string{
		"notification_type": notifType,
		"title":             "test",
		"message":           message,
		"cwd":               "/home/user/project",
	})
	req := httptest.NewRequest("POST", "/api/sessions/"+id+"/notify", bytes.NewReader(body))
	req.SetPathValue("id", id)
	w := httptest.NewRecorder()
	h.server.handleNotify(w, req)
	return w.Code
}

func (h *testHarness) stopSession(t *testing.T, id string) int {
	t.Helper()
	req := httptest.NewRequest("DELETE", "/api/sessions/"+id, nil)
	req.SetPathValue("id", id)
	w := httptest.NewRecorder()
	h.server.handleDeleteSession(w, req)
	return w.Code
}

func (h *testHarness) ntfyCount() int {
	return len(h.ntfyReqs)
}

func TestNotifySendsWhenPaneNotFocused(t *testing.T) {
	h := newTestHarness(t)
	h.createSession(t, "s1", "%5", "/home/user/project")
	h.focused = false

	h.notify(t, "s1", "permission_prompt", "Allow Bash?")

	if h.ntfyCount() != 1 {
		t.Fatalf("expected 1 ntfy request, got %d", h.ntfyCount())
	}
	if h.ntfyReqs[0].Header.Get("Title") != "[user/project] Needs approval" {
		t.Errorf("title = %q", h.ntfyReqs[0].Header.Get("Title"))
	}
}

func TestNotifySuppressedWhenPaneFocused(t *testing.T) {
	h := newTestHarness(t)
	h.createSession(t, "s1", "%5", "/home/user/project")
	h.focused = true

	h.notify(t, "s1", "permission_prompt", "Allow Bash?")

	if h.ntfyCount() != 0 {
		t.Fatalf("expected 0 ntfy requests when focused, got %d", h.ntfyCount())
	}
}

func TestStopNotifySendsWhenPaneNotFocused(t *testing.T) {
	h := newTestHarness(t)
	h.createSession(t, "s1", "%5", "/home/user/project")
	h.focused = false

	// Backdate LastActivityAt so MinSessionAge is met
	sess, _ := h.store.GetSession("s1")
	sess.LastActivityAt = time.Now().Add(-10 * time.Minute)
	h.store.UpdateSession(sess)

	h.stopSession(t, "s1")

	if h.ntfyCount() != 1 {
		t.Fatalf("expected 1 ntfy request, got %d", h.ntfyCount())
	}
	if h.ntfyReqs[0].Header.Get("Tags") != "white_check_mark" {
		t.Errorf("expected stop notification tags, got %q", h.ntfyReqs[0].Header.Get("Tags"))
	}
}

func TestStopNotifySuppressedWhenPaneFocused(t *testing.T) {
	h := newTestHarness(t)
	h.createSession(t, "s1", "%5", "/home/user/project")
	h.focused = true

	sess, _ := h.store.GetSession("s1")
	sess.LastActivityAt = time.Now().Add(-10 * time.Minute)
	h.store.UpdateSession(sess)

	h.stopSession(t, "s1")

	if h.ntfyCount() != 0 {
		t.Fatalf("expected 0 ntfy requests when focused, got %d", h.ntfyCount())
	}
}

func TestStopDurationUsesLastActivity(t *testing.T) {
	h := newTestHarness(t)
	h.createSession(t, "s1", "%5", "/home/user/project")
	h.focused = false

	// Session started a long time ago, but last activity was 8 minutes ago
	sess, _ := h.store.GetSession("s1")
	sess.StartedAt = time.Now().Add(-12 * time.Hour)
	sess.LastActivityAt = time.Now().Add(-8 * time.Minute)
	h.store.UpdateSession(sess)

	h.stopSession(t, "s1")

	if h.ntfyCount() != 1 {
		t.Fatalf("expected 1 ntfy request, got %d", h.ntfyCount())
	}
	// Should say ~8m, not ~720m
	body := h.ntfyBodies[0]
	if body != "Finished after 8m" {
		t.Errorf("stop body = %q, want %q", body, "Finished after 8m")
	}
}

func TestStopSuppressedWhenTooYoung(t *testing.T) {
	h := newTestHarness(t)
	h.createSession(t, "s1", "%5", "/home/user/project")
	h.focused = false

	// Last activity was 30 seconds ago — below MinSessionAge (120s)
	sess, _ := h.store.GetSession("s1")
	sess.LastActivityAt = time.Now().Add(-30 * time.Second)
	h.store.UpdateSession(sess)

	h.stopSession(t, "s1")

	if h.ntfyCount() != 0 {
		t.Fatalf("expected 0 ntfy requests for young session, got %d", h.ntfyCount())
	}
}

func TestLastActivitySetOnCreate(t *testing.T) {
	h := newTestHarness(t)

	before := time.Now().Add(-2 * time.Second) // tolerance for RFC3339 round-trip
	h.createSession(t, "s1", "%5", "/home/user/project")

	sess, err := h.store.GetSession("s1")
	if err != nil {
		t.Fatalf("GetSession: %v", err)
	}
	if sess.LastActivityAt.Before(before) {
		t.Errorf("LastActivityAt (%v) should be >= create time (%v)", sess.LastActivityAt, before)
	}
	if sess.LastActivityAt.IsZero() {
		t.Error("LastActivityAt should not be zero")
	}
}

func TestLastActivityUpdatedOnNotify(t *testing.T) {
	h := newTestHarness(t)
	h.createSession(t, "s1", "%5", "/home/user/project")

	// Backdate to detect update
	sess, _ := h.store.GetSession("s1")
	old := time.Now().Add(-1 * time.Hour)
	sess.LastActivityAt = old
	h.store.UpdateSession(sess)

	h.notify(t, "s1", "permission_prompt", "Allow Bash?")

	sess, _ = h.store.GetSession("s1")
	if !sess.LastActivityAt.After(old) {
		t.Errorf("LastActivityAt not updated on notify: %v", sess.LastActivityAt)
	}
}

func TestLastActivityUpdatedOnRespond(t *testing.T) {
	h := newTestHarness(t)
	h.createSession(t, "s1", "%5", "/home/user/project")

	// Backdate
	sess, _ := h.store.GetSession("s1")
	old := time.Now().Add(-1 * time.Hour)
	sess.LastActivityAt = old
	h.store.UpdateSession(sess)

	body, _ := json.Marshal(map[string]string{"text": "yes"})
	req := httptest.NewRequest("POST", "/api/respond/s1", bytes.NewReader(body))
	req.SetPathValue("id", "s1")
	w := httptest.NewRecorder()
	h.server.handleRespond(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("respond: got %d", w.Code)
	}

	sess, _ = h.store.GetSession("s1")
	if !sess.LastActivityAt.After(old) {
		t.Errorf("LastActivityAt not updated on respond: %v", sess.LastActivityAt)
	}
}

func TestStopNotifyIncludesLastMessage(t *testing.T) {
	h := newTestHarness(t)
	h.createSession(t, "s1", "%5", "/home/user/project")
	h.focused = false

	// Simulate a notification, then stop
	h.notify(t, "s1", "permission_prompt", "Allow Bash: git status")
	h.ntfyReqs = nil // reset so we only capture the stop notification
	h.ntfyBodies = nil

	sess, _ := h.store.GetSession("s1")
	sess.LastActivityAt = time.Now().Add(-8 * time.Minute)
	h.store.UpdateSession(sess)

	h.stopSession(t, "s1")

	if h.ntfyCount() != 1 {
		t.Fatalf("expected 1 ntfy request, got %d", h.ntfyCount())
	}
	want := "Finished after 8m\nAllow Bash: git status"
	if h.ntfyBodies[0] != want {
		t.Errorf("stop body = %q, want %q", h.ntfyBodies[0], want)
	}
}

func TestStopNotifyHasClickURL(t *testing.T) {
	h := newTestHarness(t)
	h.createSession(t, "s1", "%5", "/home/user/project")
	h.focused = false

	sess, _ := h.store.GetSession("s1")
	sess.LastActivityAt = time.Now().Add(-10 * time.Minute)
	h.store.UpdateSession(sess)

	h.stopSession(t, "s1")

	if h.ntfyCount() != 1 {
		t.Fatalf("expected 1 ntfy request, got %d", h.ntfyCount())
	}
	click := h.ntfyReqs[0].Header.Get("Click")
	want := "https://example.com/sophon/respond/s1"
	if click != want {
		t.Errorf("Click = %q, want %q", click, want)
	}
}

func TestTranscriptEndpointReturnsEmptyForMissingFile(t *testing.T) {
	h := newTestHarness(t)
	h.server.cfg.ClaudeDir = t.TempDir() // no JSONL files here
	h.createSession(t, "s1", "%5", "/home/user/project")

	req := httptest.NewRequest("GET", "/api/sessions/s1/transcript", nil)
	req.SetPathValue("id", "s1")
	w := httptest.NewRecorder()
	h.server.handleTranscript(w, req)

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

func TestTranscriptEndpointWithData(t *testing.T) {
	h := newTestHarness(t)
	claudeDir := t.TempDir()
	h.server.cfg.ClaudeDir = claudeDir

	cwd := "/home/user/project"
	sessionID := "test-sess-1"
	h.createSession(t, sessionID, "%5", cwd)

	// Create the JSONL file where TranscriptPath expects it
	// slug: /home/user/project -> -home-user-project
	projectDir := filepath.Join(claudeDir, "projects", "-home-user-project")
	os.MkdirAll(projectDir, 0o755)
	jsonlPath := filepath.Join(projectDir, sessionID+".jsonl")
	jsonl := `{"type":"user","timestamp":"2026-01-01T00:00:00.000Z","message":{"role":"user","content":"Hello"}}
{"type":"assistant","timestamp":"2026-01-01T00:00:01.000Z","message":{"role":"assistant","content":[{"type":"text","text":"Hi there!"}]}}
`
	os.WriteFile(jsonlPath, []byte(jsonl), 0o644)

	req := httptest.NewRequest("GET", "/api/sessions/"+sessionID+"/transcript", nil)
	req.SetPathValue("id", sessionID)
	w := httptest.NewRecorder()
	h.server.handleTranscript(w, req)

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
	if result.Messages[0].Role != "user" {
		t.Errorf("msg 0 role = %q", result.Messages[0].Role)
	}
	if result.Messages[1].Blocks[0].Text != "Hi there!" {
		t.Errorf("msg 1 text = %q", result.Messages[1].Blocks[0].Text)
	}
}

func TestStopNotifyUsesTranscriptText(t *testing.T) {
	h := newTestHarness(t)
	claudeDir := t.TempDir()
	h.server.cfg.ClaudeDir = claudeDir
	h.focused = false

	cwd := "/home/user/project"
	sessionID := "s-transcript"
	h.createSession(t, sessionID, "%5", cwd)

	// Create transcript with assistant text
	projectDir := filepath.Join(claudeDir, "projects", "-home-user-project")
	os.MkdirAll(projectDir, 0o755)
	jsonlPath := filepath.Join(projectDir, sessionID+".jsonl")
	jsonl := `{"type":"assistant","timestamp":"2026-01-01T00:00:01.000Z","message":{"role":"assistant","content":[{"type":"text","text":"All done! The fix is deployed."}]}}
`
	os.WriteFile(jsonlPath, []byte(jsonl), 0o644)

	// Also set a NotifyMessage to verify transcript takes priority
	sess, _ := h.store.GetSession(sessionID)
	sess.NotifyMessage = "old notification message"
	sess.LastActivityAt = time.Now().Add(-8 * time.Minute)
	h.store.UpdateSession(sess)

	h.stopSession(t, sessionID)

	if h.ntfyCount() != 1 {
		t.Fatalf("expected 1 ntfy request, got %d", h.ntfyCount())
	}
	want := "Finished after 8m\nAll done! The fix is deployed."
	if h.ntfyBodies[0] != want {
		t.Errorf("stop body = %q, want %q", h.ntfyBodies[0], want)
	}
}
