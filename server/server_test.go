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
