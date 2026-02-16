package server

import (
	"bytes"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/phinze/sophon/store"
	"github.com/phinze/sophon/transcript"
)

// mockNodeOps implements NodeOps for testing.
type mockNodeOps struct {
	focused     bool
	sentKeys    []string
	transcripts map[string]*transcript.Transcript // keyed by sessionID
}

func (m *mockNodeOps) PaneFocused(nodeName, pane string) bool {
	return m.focused
}

func (m *mockNodeOps) SendKeys(nodeName, pane, text string) error {
	m.sentKeys = append(m.sentKeys, text)
	return nil
}

func (m *mockNodeOps) ReadTranscript(nodeName, sessionID, cwd string) (*transcript.Transcript, error) {
	if m.transcripts != nil {
		if tr, ok := m.transcripts[sessionID]; ok {
			return tr, nil
		}
	}
	return &transcript.Transcript{}, nil
}

// testHarness sets up a Server with an in-memory store, a mock ntfy endpoint,
// and a mockNodeOps.
type testHarness struct {
	server     *Server
	store      *store.Store
	ntfyReqs   []*http.Request
	ntfyBodies []string
	mockOps    mockNodeOps
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
	h.mockOps = mockNodeOps{transcripts: make(map[string]*transcript.Transcript)}
	h.server.nodeOps = &h.mockOps

	return h
}

func (h *testHarness) createSession(t *testing.T, id, pane, cwd string) {
	t.Helper()
	body, _ := json.Marshal(map[string]string{
		"session_id": id,
		"tmux_pane":  pane,
		"cwd":        cwd,
		"node_name":  "test-node",
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
		"node_name":         "test-node",
	})
	req := httptest.NewRequest("POST", "/api/sessions/"+id+"/notify", bytes.NewReader(body))
	req.SetPathValue("id", id)
	w := httptest.NewRecorder()
	h.server.handleNotify(w, req)
	return w.Code
}

func (h *testHarness) turnEnd(t *testing.T, id string) int {
	t.Helper()
	body, _ := json.Marshal(map[string]string{"node_name": "test-node"})
	req := httptest.NewRequest("POST", "/api/sessions/"+id+"/activity", bytes.NewReader(body))
	req.SetPathValue("id", id)
	w := httptest.NewRecorder()
	h.server.handleActivity(w, req)
	return w.Code
}

func (h *testHarness) endSession(t *testing.T, id string) int {
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
	h.mockOps.focused = false

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
	h.mockOps.focused = true

	h.notify(t, "s1", "permission_prompt", "Allow Bash?")

	if h.ntfyCount() != 0 {
		t.Fatalf("expected 0 ntfy requests when focused, got %d", h.ntfyCount())
	}
}

func TestTurnEndNotifySendsWhenPaneNotFocused(t *testing.T) {
	h := newTestHarness(t)
	h.createSession(t, "s1", "%5", "/home/user/project")
	h.mockOps.focused = false

	// Backdate LastActivityAt so MinSessionAge is met
	sess, _ := h.store.GetSession("s1")
	sess.LastActivityAt = time.Now().Add(-10 * time.Minute)
	h.store.UpdateSession(sess)

	h.turnEnd(t, "s1")

	if h.ntfyCount() != 1 {
		t.Fatalf("expected 1 ntfy request, got %d", h.ntfyCount())
	}
	if h.ntfyReqs[0].Header.Get("Tags") != "white_check_mark" {
		t.Errorf("expected stop notification tags, got %q", h.ntfyReqs[0].Header.Get("Tags"))
	}

	// Session should NOT be marked as stopped
	sess, _ = h.store.GetSession("s1")
	if !sess.StoppedAt.IsZero() {
		t.Errorf("StoppedAt should be zero after turn end, got %v", sess.StoppedAt)
	}
}

func TestTurnEndNotifySuppressedWhenPaneFocused(t *testing.T) {
	h := newTestHarness(t)
	h.createSession(t, "s1", "%5", "/home/user/project")
	h.mockOps.focused = true

	sess, _ := h.store.GetSession("s1")
	sess.LastActivityAt = time.Now().Add(-10 * time.Minute)
	h.store.UpdateSession(sess)

	h.turnEnd(t, "s1")

	if h.ntfyCount() != 0 {
		t.Fatalf("expected 0 ntfy requests when focused, got %d", h.ntfyCount())
	}
}

func TestTurnEndDurationUsesLastActivity(t *testing.T) {
	h := newTestHarness(t)
	h.createSession(t, "s1", "%5", "/home/user/project")
	h.mockOps.focused = false

	// Session started a long time ago, but last activity was 8 minutes ago
	sess, _ := h.store.GetSession("s1")
	sess.StartedAt = time.Now().Add(-12 * time.Hour)
	sess.LastActivityAt = time.Now().Add(-8 * time.Minute)
	h.store.UpdateSession(sess)

	h.turnEnd(t, "s1")

	if h.ntfyCount() != 1 {
		t.Fatalf("expected 1 ntfy request, got %d", h.ntfyCount())
	}
	// Should say ~8m, not ~720m
	body := h.ntfyBodies[0]
	if body != "Finished after 8m" {
		t.Errorf("stop body = %q, want %q", body, "Finished after 8m")
	}
}

func TestTurnEndSuppressedWhenTooYoung(t *testing.T) {
	h := newTestHarness(t)
	h.createSession(t, "s1", "%5", "/home/user/project")
	h.mockOps.focused = false

	// Last activity was 30 seconds ago — below MinSessionAge (120s)
	sess, _ := h.store.GetSession("s1")
	sess.LastActivityAt = time.Now().Add(-30 * time.Second)
	h.store.UpdateSession(sess)

	h.turnEnd(t, "s1")

	if h.ntfyCount() != 0 {
		t.Fatalf("expected 0 ntfy requests for young session, got %d", h.ntfyCount())
	}
}

func TestSessionEndSetsStoppedAt(t *testing.T) {
	h := newTestHarness(t)
	h.createSession(t, "s1", "%5", "/home/user/project")

	h.endSession(t, "s1")

	sess, err := h.store.GetSession("s1")
	if err != nil {
		t.Fatalf("GetSession: %v", err)
	}
	if sess.StoppedAt.IsZero() {
		t.Error("StoppedAt should be set after session end")
	}
}

func TestSessionEndDoesNotNotify(t *testing.T) {
	h := newTestHarness(t)
	h.createSession(t, "s1", "%5", "/home/user/project")
	h.mockOps.focused = false

	sess, _ := h.store.GetSession("s1")
	sess.LastActivityAt = time.Now().Add(-10 * time.Minute)
	h.store.UpdateSession(sess)

	h.endSession(t, "s1")

	if h.ntfyCount() != 0 {
		t.Fatalf("expected 0 ntfy requests on session end, got %d", h.ntfyCount())
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

func TestTurnEndNotifyIncludesLastMessage(t *testing.T) {
	h := newTestHarness(t)
	h.createSession(t, "s1", "%5", "/home/user/project")
	h.mockOps.focused = false

	// Simulate a notification, then turn end
	h.notify(t, "s1", "permission_prompt", "Allow Bash: git status")
	h.ntfyReqs = nil // reset so we only capture the turn-end notification
	h.ntfyBodies = nil

	sess, _ := h.store.GetSession("s1")
	sess.LastActivityAt = time.Now().Add(-8 * time.Minute)
	h.store.UpdateSession(sess)

	h.turnEnd(t, "s1")

	if h.ntfyCount() != 1 {
		t.Fatalf("expected 1 ntfy request, got %d", h.ntfyCount())
	}
	want := "Finished after 8m\nAllow Bash: git status"
	if h.ntfyBodies[0] != want {
		t.Errorf("stop body = %q, want %q", h.ntfyBodies[0], want)
	}
}

func TestTurnEndNotifyHasClickURL(t *testing.T) {
	h := newTestHarness(t)
	h.createSession(t, "s1", "%5", "/home/user/project")
	h.mockOps.focused = false

	sess, _ := h.store.GetSession("s1")
	sess.LastActivityAt = time.Now().Add(-10 * time.Minute)
	h.store.UpdateSession(sess)

	h.turnEnd(t, "s1")

	if h.ntfyCount() != 1 {
		t.Fatalf("expected 1 ntfy request, got %d", h.ntfyCount())
	}
	click := h.ntfyReqs[0].Header.Get("Click")
	want := "https://example.com/sophon/respond/s1"
	if click != want {
		t.Errorf("Click = %q, want %q", click, want)
	}
}

func TestTranscriptEndpointReturnsEmptyForNoAgent(t *testing.T) {
	h := newTestHarness(t)
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
	sessionID := "test-sess-1"
	h.createSession(t, sessionID, "%5", "/home/user/project")

	// Set up mock transcript
	h.mockOps.transcripts[sessionID] = &transcript.Transcript{
		Messages: []transcript.Message{
			{
				Role:   "user",
				Blocks: []transcript.Block{{Type: "text", Text: "Hello"}},
			},
			{
				Role:   "assistant",
				Blocks: []transcript.Block{{Type: "text", Text: "Hi there!"}},
			},
		},
	}

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

func TestTurnEndNotifyUsesTranscriptText(t *testing.T) {
	h := newTestHarness(t)
	h.mockOps.focused = false

	sessionID := "s-transcript"
	h.createSession(t, sessionID, "%5", "/home/user/project")

	// Set up mock transcript with assistant text
	h.mockOps.transcripts[sessionID] = &transcript.Transcript{
		Messages: []transcript.Message{
			{
				Role:   "assistant",
				Blocks: []transcript.Block{{Type: "text", Text: "All done! The fix is deployed."}},
			},
		},
	}

	// Also set a NotifyMessage to verify transcript takes priority
	sess, _ := h.store.GetSession(sessionID)
	sess.NotifyMessage = "old notification message"
	sess.LastActivityAt = time.Now().Add(-8 * time.Minute)
	h.store.UpdateSession(sess)

	h.turnEnd(t, sessionID)

	if h.ntfyCount() != 1 {
		t.Fatalf("expected 1 ntfy request, got %d", h.ntfyCount())
	}
	want := "Finished after 8m\nAll done! The fix is deployed."
	if h.ntfyBodies[0] != want {
		t.Errorf("stop body = %q, want %q", h.ntfyBodies[0], want)
	}
}

func TestNodeNameStoredOnCreate(t *testing.T) {
	h := newTestHarness(t)
	h.createSession(t, "s1", "%5", "/home/user/project")

	sess, err := h.store.GetSession("s1")
	if err != nil {
		t.Fatalf("GetSession: %v", err)
	}
	if sess.NodeName != "test-node" {
		t.Errorf("NodeName = %q, want %q", sess.NodeName, "test-node")
	}
}

func TestAgentRegister(t *testing.T) {
	h := newTestHarness(t)

	body, _ := json.Marshal(map[string]string{
		"node_name": "foxtrotbase",
		"url":       "http://127.0.0.1:2588",
	})
	req := httptest.NewRequest("POST", "/api/agents/register", bytes.NewReader(body))
	w := httptest.NewRecorder()
	h.server.handleAgentRegister(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("got %d, want 200", w.Code)
	}

	info, ok := h.server.agents.Get("foxtrotbase")
	if !ok {
		t.Fatal("agent not registered")
	}
	if info.URL != "http://127.0.0.1:2588" {
		t.Errorf("agent URL = %q", info.URL)
	}
}
