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
	transcripts map[string]*transcript.Transcript       // keyed by sessionID
	summaries   map[string]*transcript.SessionSummary    // keyed by sessionID
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

func (m *mockNodeOps) ReadSummary(nodeName, sessionID, cwd string) (*transcript.SessionSummary, error) {
	if m.summaries != nil {
		if s, ok := m.summaries[sessionID]; ok {
			return s, nil
		}
	}
	return nil, nil
}

// testHarness sets up a Server with an in-memory store and a mockNodeOps.
type testHarness struct {
	server  *Server
	store   *store.Store
	mockOps mockNodeOps
}

func newTestHarness(t *testing.T) *testHarness {
	t.Helper()

	st, err := store.Open(":memory:")
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	t.Cleanup(func() { st.Close() })

	h := &testHarness{store: st}

	cfg := Config{
		BaseURL:       "https://example.com",
		MinSessionAge: 120,
	}

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	h.server = New(cfg, st, logger)
	h.mockOps = mockNodeOps{
		transcripts: make(map[string]*transcript.Transcript),
		summaries:   make(map[string]*transcript.SessionSummary),
	}
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

func (h *testHarness) toolActivity(t *testing.T, id, hookEventName, toolName string) int {
	t.Helper()
	body, _ := json.Marshal(map[string]string{
		"hook_event_name": hookEventName,
		"tool_name":       toolName,
		"node_name":       "test-node",
	})
	req := httptest.NewRequest("POST", "/api/sessions/"+id+"/tool-activity", bytes.NewReader(body))
	req.SetPathValue("id", id)
	w := httptest.NewRecorder()
	h.server.handleToolActivity(w, req)
	return w.Code
}

func TestNotifyStoresSessionState(t *testing.T) {
	h := newTestHarness(t)
	h.createSession(t, "s1", "%5", "/home/user/project")

	h.notify(t, "s1", "permission_prompt", "Allow Bash?")

	sess, err := h.store.GetSession("s1")
	if err != nil {
		t.Fatalf("GetSession: %v", err)
	}
	if sess.NotificationType != "permission_prompt" {
		t.Errorf("NotificationType = %q, want %q", sess.NotificationType, "permission_prompt")
	}
	if sess.NotifyMessage != "Allow Bash?" {
		t.Errorf("NotifyMessage = %q, want %q", sess.NotifyMessage, "Allow Bash?")
	}
	if sess.NotifiedAt.IsZero() {
		t.Error("NotifiedAt should be set")
	}
}

func TestTurnEndUpdatesLastActivity(t *testing.T) {
	h := newTestHarness(t)
	h.createSession(t, "s1", "%5", "/home/user/project")

	// Backdate LastActivityAt so MinSessionAge is met
	sess, _ := h.store.GetSession("s1")
	old := time.Now().Add(-10 * time.Minute)
	sess.LastActivityAt = old
	h.store.UpdateSession(sess)

	h.turnEnd(t, "s1")

	sess, _ = h.store.GetSession("s1")
	if !sess.LastActivityAt.After(old) {
		t.Errorf("LastActivityAt not updated on turn end: %v", sess.LastActivityAt)
	}
	// Session should NOT be marked as stopped
	if !sess.StoppedAt.IsZero() {
		t.Errorf("StoppedAt should be zero after turn end, got %v", sess.StoppedAt)
	}
}

func TestTurnEndSuppressedWhenTooYoung(t *testing.T) {
	h := newTestHarness(t)
	h.createSession(t, "s1", "%5", "/home/user/project")

	// Last activity was 30 seconds ago — below MinSessionAge (120s)
	sess, _ := h.store.GetSession("s1")
	sess.LastActivityAt = time.Now().Add(-30 * time.Second)
	h.store.UpdateSession(sess)

	code := h.turnEnd(t, "s1")
	if code != http.StatusOK {
		t.Fatalf("turnEnd: got %d, want 200", code)
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

func TestGetSession(t *testing.T) {
	h := newTestHarness(t)

	// 404 for non-existent session
	req := httptest.NewRequest("GET", "/api/sessions/nope", nil)
	req.SetPathValue("id", "nope")
	w := httptest.NewRecorder()
	h.server.handleGetSession(w, req)
	if w.Code != http.StatusNotFound {
		t.Fatalf("got %d, want 404", w.Code)
	}

	// Create session and fetch it
	h.createSession(t, "s1", "%5", "/home/user/project")

	req = httptest.NewRequest("GET", "/api/sessions/s1", nil)
	req.SetPathValue("id", "s1")
	w = httptest.NewRecorder()
	h.server.handleGetSession(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("got %d, want 200", w.Code)
	}

	var sess store.Session
	json.NewDecoder(w.Body).Decode(&sess)
	if sess.ID != "s1" {
		t.Errorf("session ID = %q, want %q", sess.ID, "s1")
	}
	if sess.Project != "user/project" {
		t.Errorf("Project = %q, want %q", sess.Project, "user/project")
	}
}

func TestSamePaneDedup(t *testing.T) {
	h := newTestHarness(t)

	// Create two sessions on the same pane
	h.createSession(t, "s1", "%5", "/home/user/project")
	h.createSession(t, "s2", "%5", "/home/user/project")

	// s1 should be auto-stopped
	s1, _ := h.store.GetSession("s1")
	if s1.StoppedAt.IsZero() {
		t.Error("s1 should be auto-stopped when s2 starts on same pane")
	}

	// s2 should still be active
	s2, _ := h.store.GetSession("s2")
	if !s2.StoppedAt.IsZero() {
		t.Error("s2 should still be active")
	}

	// Different pane should not be affected
	h.createSession(t, "s3", "%10", "/home/user/project")
	s2, _ = h.store.GetSession("s2")
	if !s2.StoppedAt.IsZero() {
		t.Error("s2 should not be stopped by s3 on different pane")
	}
}

func TestReconcileSessionsStopsDeadPanes(t *testing.T) {
	h := newTestHarness(t)

	h.createSession(t, "alive", "%0", "/home/user/proj")
	h.createSession(t, "dead1", "%1", "/home/user/proj")
	h.createSession(t, "dead2", "%2", "/home/user/proj")
	h.createSession(t, "no-pane", "", "/home/user/proj") // no pane, should be left alone

	// Reconcile: only %0 is alive
	h.server.reconcileSessions("test-node", []string{"%0"})

	alive, _ := h.store.GetSession("alive")
	if !alive.StoppedAt.IsZero() {
		t.Error("alive session should not be stopped")
	}

	dead1, _ := h.store.GetSession("dead1")
	if dead1.StoppedAt.IsZero() {
		t.Error("dead1 should be stopped")
	}

	dead2, _ := h.store.GetSession("dead2")
	if dead2.StoppedAt.IsZero() {
		t.Error("dead2 should be stopped")
	}

	nopane, _ := h.store.GetSession("no-pane")
	if !nopane.StoppedAt.IsZero() {
		t.Error("no-pane session should not be stopped")
	}
}

func TestReconcileSessionsEmptyAlivePanesStopsAll(t *testing.T) {
	h := newTestHarness(t)

	h.createSession(t, "s1", "%0", "/home/user/proj")
	h.createSession(t, "s2", "%1", "/home/user/proj")

	h.server.reconcileSessions("test-node", []string{})

	s1, _ := h.store.GetSession("s1")
	s2, _ := h.store.GetSession("s2")
	if s1.StoppedAt.IsZero() || s2.StoppedAt.IsZero() {
		t.Error("all sessions should be stopped when alive panes is empty")
	}
}

func TestReconcileOnlyAffectsTargetNode(t *testing.T) {
	h := newTestHarness(t)

	// Create session on different node
	sess := &store.Session{
		ID:        "other-node-sess",
		TmuxPane:  "%5",
		NodeName:  "other-node",
		StartedAt: time.Now(),
	}
	h.store.CreateSession(sess)

	h.createSession(t, "test-sess", "%0", "/home/user/proj")

	// Reconcile test-node with no alive panes
	h.server.reconcileSessions("test-node", []string{})

	other, _ := h.store.GetSession("other-node-sess")
	if !other.StoppedAt.IsZero() {
		t.Error("session on other node should not be affected")
	}

	testSess, _ := h.store.GetSession("test-sess")
	if testSess.StoppedAt.IsZero() {
		t.Error("session on test-node should be stopped")
	}
}

func TestAgentRegisterWithAlivePanes(t *testing.T) {
	h := newTestHarness(t)

	h.createSession(t, "s1", "%0", "/home/user/proj")
	h.createSession(t, "s2", "%1", "/home/user/proj")

	// Register agent with alive_panes reporting only %0
	body, _ := json.Marshal(map[string]any{
		"node_name":   "test-node",
		"url":         "http://127.0.0.1:2588",
		"alive_panes": []string{"%0"},
	})
	req := httptest.NewRequest("POST", "/api/agents/register", bytes.NewReader(body))
	w := httptest.NewRecorder()
	h.server.handleAgentRegister(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("got %d, want 200", w.Code)
	}

	s1, _ := h.store.GetSession("s1")
	if !s1.StoppedAt.IsZero() {
		t.Error("s1 should still be active")
	}
	s2, _ := h.store.GetSession("s2")
	if s2.StoppedAt.IsZero() {
		t.Error("s2 should be stopped by reconciliation")
	}
}

func TestAgentRegisterWithoutAlivePanesSkipsReconciliation(t *testing.T) {
	h := newTestHarness(t)

	h.createSession(t, "s1", "%0", "/home/user/proj")

	// Register without alive_panes
	body, _ := json.Marshal(map[string]string{
		"node_name": "test-node",
		"url":       "http://127.0.0.1:2588",
	})
	req := httptest.NewRequest("POST", "/api/agents/register", bytes.NewReader(body))
	w := httptest.NewRecorder()
	h.server.handleAgentRegister(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("got %d, want 200", w.Code)
	}

	s1, _ := h.store.GetSession("s1")
	if !s1.StoppedAt.IsZero() {
		t.Error("s1 should still be active when alive_panes is omitted")
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

func TestToolActivityUnknownSessionReturns200(t *testing.T) {
	h := newTestHarness(t)
	code := h.toolActivity(t, "nonexistent", "PreToolUse", "Bash")
	if code != http.StatusOK {
		t.Fatalf("got %d, want 200", code)
	}
}

func TestToolActivityPublishesSSE(t *testing.T) {
	h := newTestHarness(t)
	h.createSession(t, "s1", "%5", "/home/user/project")

	// Subscribe after session creation so we don't need to drain session_start
	ch, unsub := h.server.events.Subscribe("s1")
	defer unsub()

	code := h.toolActivity(t, "s1", "PreToolUse", "Bash")
	if code != http.StatusOK {
		t.Fatalf("got %d, want 200", code)
	}

	select {
	case evt := <-ch:
		if evt.Type != "tool_activity" {
			t.Errorf("event type = %q, want %q", evt.Type, "tool_activity")
		}
		var data map[string]string
		json.Unmarshal(evt.Data, &data)
		if data["hook_event_name"] != "PreToolUse" {
			t.Errorf("hook_event_name = %q, want %q", data["hook_event_name"], "PreToolUse")
		}
		if data["tool_name"] != "Bash" {
			t.Errorf("tool_name = %q, want %q", data["tool_name"], "Bash")
		}
	default:
		t.Error("expected tool_activity event but got none")
	}
}

func TestActivityUpdatesSummary(t *testing.T) {
	h := newTestHarness(t)
	h.createSession(t, "s1", "%5", "/home/user/project")

	// Set up mock summary
	h.mockOps.summaries["s1"] = &transcript.SessionSummary{
		Topic:       "Fix the bug",
		PlanSummary: "Refactor error handling",
	}

	h.turnEnd(t, "s1")

	// The summary is fetched asynchronously — give the goroutine time to complete
	time.Sleep(50 * time.Millisecond)

	sess, err := h.store.GetSession("s1")
	if err != nil {
		t.Fatalf("GetSession: %v", err)
	}
	if sess.Topic != "Fix the bug" {
		t.Errorf("Topic = %q, want %q", sess.Topic, "Fix the bug")
	}
	if sess.PlanSummary != "Refactor error handling" {
		t.Errorf("PlanSummary = %q, want %q", sess.PlanSummary, "Refactor error handling")
	}
}

func TestActivityNoSummaryDoesNotOverwrite(t *testing.T) {
	h := newTestHarness(t)
	h.createSession(t, "s1", "%5", "/home/user/project")

	// Pre-set topic on session
	sess, _ := h.store.GetSession("s1")
	sess.Topic = "Existing topic"
	h.store.UpdateSession(sess)

	// No summary configured in mock — ReadSummary returns nil

	h.turnEnd(t, "s1")
	time.Sleep(50 * time.Millisecond)

	sess, _ = h.store.GetSession("s1")
	if sess.Topic != "Existing topic" {
		t.Errorf("Topic = %q, want %q (should not be overwritten)", sess.Topic, "Existing topic")
	}
}

func TestToolActivityDoesNotUpdateLastActivity(t *testing.T) {
	h := newTestHarness(t)
	h.createSession(t, "s1", "%5", "/home/user/project")

	// Backdate LastActivityAt
	sess, _ := h.store.GetSession("s1")
	old := time.Now().Add(-10 * time.Minute)
	sess.LastActivityAt = old
	h.store.UpdateSession(sess)

	h.toolActivity(t, "s1", "PostToolUse", "Read")

	sess, _ = h.store.GetSession("s1")
	if sess.LastActivityAt.After(old) {
		t.Errorf("LastActivityAt should not be updated by tool activity, was %v, expected %v", sess.LastActivityAt, old)
	}
}
