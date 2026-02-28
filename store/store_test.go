package store

import (
	"testing"
	"time"
)

func openTestStore(t *testing.T) *Store {
	t.Helper()
	s, err := Open(":memory:")
	if err != nil {
		t.Fatalf("Open(:memory:): %v", err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func TestCreateAndGetSession(t *testing.T) {
	s := openTestStore(t)

	now := time.Now().Truncate(time.Second)
	sess := &Session{
		ID:        "sess-1",
		TmuxPane:  "%5",
		Cwd:       "/home/user/project",
		Project:   "user/project",
		StartedAt: now,
	}

	if err := s.CreateSession(sess); err != nil {
		t.Fatalf("CreateSession: %v", err)
	}

	got, err := s.GetSession("sess-1")
	if err != nil {
		t.Fatalf("GetSession: %v", err)
	}

	if got.ID != sess.ID {
		t.Errorf("ID = %q, want %q", got.ID, sess.ID)
	}
	if got.TmuxPane != sess.TmuxPane {
		t.Errorf("TmuxPane = %q, want %q", got.TmuxPane, sess.TmuxPane)
	}
	if got.Project != sess.Project {
		t.Errorf("Project = %q, want %q", got.Project, sess.Project)
	}
	if !got.StartedAt.Equal(sess.StartedAt) {
		t.Errorf("StartedAt = %v, want %v", got.StartedAt, sess.StartedAt)
	}
	if !got.StoppedAt.IsZero() {
		t.Errorf("StoppedAt should be zero, got %v", got.StoppedAt)
	}
}

func TestGetSessionNotFound(t *testing.T) {
	s := openTestStore(t)

	_, err := s.GetSession("nonexistent")
	if err != ErrNotFound {
		t.Errorf("GetSession(nonexistent) = %v, want ErrNotFound", err)
	}
}

func TestCreateSessionReplace(t *testing.T) {
	s := openTestStore(t)

	now := time.Now().Truncate(time.Second)
	sess := &Session{
		ID:        "sess-1",
		TmuxPane:  "%5",
		Project:   "old/project",
		StartedAt: now,
	}
	if err := s.CreateSession(sess); err != nil {
		t.Fatalf("CreateSession: %v", err)
	}

	sess.Project = "new/project"
	sess.TmuxPane = "%10"
	if err := s.CreateSession(sess); err != nil {
		t.Fatalf("CreateSession (replace): %v", err)
	}

	got, err := s.GetSession("sess-1")
	if err != nil {
		t.Fatalf("GetSession: %v", err)
	}
	if got.Project != "new/project" {
		t.Errorf("Project = %q, want %q", got.Project, "new/project")
	}
	if got.TmuxPane != "%10" {
		t.Errorf("TmuxPane = %q, want %q", got.TmuxPane, "%10")
	}
}

func TestUpdateSession(t *testing.T) {
	s := openTestStore(t)

	now := time.Now().Truncate(time.Second)
	sess := &Session{
		ID:        "sess-1",
		Project:   "user/project",
		StartedAt: now,
	}
	if err := s.CreateSession(sess); err != nil {
		t.Fatalf("CreateSession: %v", err)
	}

	sess.NotificationType = "permission_prompt"
	sess.NotifyMessage = "Allow Bash?"
	sess.NotifiedAt = now.Add(time.Minute)
	if err := s.UpdateSession(sess); err != nil {
		t.Fatalf("UpdateSession: %v", err)
	}

	got, err := s.GetSession("sess-1")
	if err != nil {
		t.Fatalf("GetSession: %v", err)
	}
	if got.NotificationType != "permission_prompt" {
		t.Errorf("NotificationType = %q, want %q", got.NotificationType, "permission_prompt")
	}
	if got.NotifyMessage != "Allow Bash?" {
		t.Errorf("NotifyMessage = %q, want %q", got.NotifyMessage, "Allow Bash?")
	}
}

func TestUpdateSessionNotFound(t *testing.T) {
	s := openTestStore(t)

	err := s.UpdateSession(&Session{ID: "nonexistent", StartedAt: time.Now()})
	if err != ErrNotFound {
		t.Errorf("UpdateSession(nonexistent) = %v, want ErrNotFound", err)
	}
}

func TestListActiveSessions(t *testing.T) {
	s := openTestStore(t)

	now := time.Now().Truncate(time.Second)

	// Create 3 sessions: 2 active, 1 stopped
	for i, id := range []string{"a", "b", "c"} {
		sess := &Session{
			ID:        id,
			Project:   id + "/project",
			StartedAt: now.Add(time.Duration(i) * time.Minute),
		}
		if id == "b" {
			sess.StoppedAt = now.Add(5 * time.Minute)
		}
		if err := s.CreateSession(sess); err != nil {
			t.Fatalf("CreateSession(%s): %v", id, err)
		}
	}

	active, err := s.ListActiveSessions()
	if err != nil {
		t.Fatalf("ListActiveSessions: %v", err)
	}
	if len(active) != 2 {
		t.Fatalf("len(active) = %d, want 2", len(active))
	}
	// Should be ordered by started_at DESC
	if active[0].ID != "c" {
		t.Errorf("active[0].ID = %q, want %q", active[0].ID, "c")
	}
	if active[1].ID != "a" {
		t.Errorf("active[1].ID = %q, want %q", active[1].ID, "a")
	}
}

func TestListRecentSessions(t *testing.T) {
	s := openTestStore(t)

	now := time.Now().Truncate(time.Second)

	// Create 3 stopped sessions and 1 active
	for i, id := range []string{"a", "b", "c", "d"} {
		sess := &Session{
			ID:        id,
			Project:   id + "/project",
			StartedAt: now,
		}
		if id != "d" {
			sess.StoppedAt = now.Add(time.Duration(i) * time.Minute)
		}
		if err := s.CreateSession(sess); err != nil {
			t.Fatalf("CreateSession(%s): %v", id, err)
		}
	}

	recent, err := s.ListRecentSessions(2)
	if err != nil {
		t.Fatalf("ListRecentSessions: %v", err)
	}
	if len(recent) != 2 {
		t.Fatalf("len(recent) = %d, want 2", len(recent))
	}
	// Should be ordered by stopped_at DESC
	if recent[0].ID != "c" {
		t.Errorf("recent[0].ID = %q, want %q", recent[0].ID, "c")
	}
	if recent[1].ID != "b" {
		t.Errorf("recent[1].ID = %q, want %q", recent[1].ID, "b")
	}
}

func TestReapStoppedSessions(t *testing.T) {
	s := openTestStore(t)

	now := time.Now().Truncate(time.Second)

	// Create sessions: one stopped recently, one stopped long ago, one active
	sessions := []struct {
		id      string
		stopped time.Time
	}{
		{"recent", now.Add(-1 * time.Hour)},
		{"old", now.Add(-25 * time.Hour)},
		{"active", time.Time{}},
	}

	for _, tc := range sessions {
		sess := &Session{
			ID:        tc.id,
			StartedAt: now.Add(-48 * time.Hour),
			StoppedAt: tc.stopped,
		}
		if err := s.CreateSession(sess); err != nil {
			t.Fatalf("CreateSession(%s): %v", tc.id, err)
		}
	}

	reaped, err := s.ReapStoppedSessions(24 * time.Hour)
	if err != nil {
		t.Fatalf("ReapStoppedSessions: %v", err)
	}

	if len(reaped) != 1 || reaped[0] != "old" {
		t.Errorf("reaped = %v, want [old]", reaped)
	}

	// Verify "recent" and "active" still exist
	if _, err := s.GetSession("recent"); err != nil {
		t.Errorf("GetSession(recent) after reap: %v", err)
	}
	if _, err := s.GetSession("active"); err != nil {
		t.Errorf("GetSession(active) after reap: %v", err)
	}
	if _, err := s.GetSession("old"); err != ErrNotFound {
		t.Errorf("GetSession(old) after reap = %v, want ErrNotFound", err)
	}
}

func TestProjectFromCwd(t *testing.T) {
	tests := []struct {
		cwd  string
		want string
	}{
		{"/home/user/project", "user/project"},
		{"/home/user/project/", "user/project"},
		{"/a/b/c/d", "c/d"},
		{"single", "single"},
		{"", "unknown"},
	}
	for _, tc := range tests {
		got := ProjectFromCwd(tc.cwd)
		if got != tc.want {
			t.Errorf("ProjectFromCwd(%q) = %q, want %q", tc.cwd, got, tc.want)
		}
	}
}

func TestListActiveSessionsByNode(t *testing.T) {
	s := openTestStore(t)
	now := time.Now().Truncate(time.Second)

	// Create sessions on different nodes, one stopped
	sessions := []struct {
		id, node string
		stopped  bool
	}{
		{"a", "node1", false},
		{"b", "node1", false},
		{"c", "node2", false},
		{"d", "node1", true},
	}
	for i, tc := range sessions {
		sess := &Session{
			ID:        tc.id,
			NodeName:  tc.node,
			StartedAt: now.Add(time.Duration(i) * time.Minute),
		}
		if tc.stopped {
			sess.StoppedAt = now.Add(5 * time.Minute)
		}
		if err := s.CreateSession(sess); err != nil {
			t.Fatalf("CreateSession(%s): %v", tc.id, err)
		}
	}

	got, err := s.ListActiveSessionsByNode("node1")
	if err != nil {
		t.Fatalf("ListActiveSessionsByNode: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("len = %d, want 2", len(got))
	}
	// DESC order
	if got[0].ID != "b" || got[1].ID != "a" {
		t.Errorf("got [%s, %s], want [b, a]", got[0].ID, got[1].ID)
	}

	got, err = s.ListActiveSessionsByNode("node2")
	if err != nil {
		t.Fatalf("ListActiveSessionsByNode(node2): %v", err)
	}
	if len(got) != 1 || got[0].ID != "c" {
		t.Errorf("node2 sessions: %v", got)
	}
}

func TestStopSessions(t *testing.T) {
	s := openTestStore(t)
	now := time.Now().Truncate(time.Second)

	for _, id := range []string{"a", "b", "c"} {
		if err := s.CreateSession(&Session{ID: id, StartedAt: now}); err != nil {
			t.Fatalf("CreateSession(%s): %v", id, err)
		}
	}

	if err := s.StopSessions([]string{"a", "c"}); err != nil {
		t.Fatalf("StopSessions: %v", err)
	}

	for _, tc := range []struct {
		id      string
		stopped bool
	}{
		{"a", true}, {"b", false}, {"c", true},
	} {
		sess, _ := s.GetSession(tc.id)
		if tc.stopped && sess.StoppedAt.IsZero() {
			t.Errorf("session %s should be stopped", tc.id)
		}
		if !tc.stopped && !sess.StoppedAt.IsZero() {
			t.Errorf("session %s should not be stopped", tc.id)
		}
	}

	// Empty list is a no-op
	if err := s.StopSessions(nil); err != nil {
		t.Fatalf("StopSessions(nil): %v", err)
	}
}

func TestStopSessionsByPane(t *testing.T) {
	s := openTestStore(t)
	now := time.Now().Truncate(time.Second)

	sessions := []struct {
		id, node, pane string
	}{
		{"old1", "node1", "%5"},
		{"old2", "node1", "%5"},
		{"keep", "node1", "%5"}, // the new session (excluded)
		{"other-pane", "node1", "%10"},
		{"other-node", "node2", "%5"},
	}
	for _, tc := range sessions {
		if err := s.CreateSession(&Session{ID: tc.id, NodeName: tc.node, TmuxPane: tc.pane, StartedAt: now}); err != nil {
			t.Fatalf("CreateSession(%s): %v", tc.id, err)
		}
	}

	stopped, err := s.StopSessionsByPane("node1", "%5", "keep")
	if err != nil {
		t.Fatalf("StopSessionsByPane: %v", err)
	}
	if len(stopped) != 2 {
		t.Fatalf("stopped %d sessions, want 2: %v", len(stopped), stopped)
	}

	// Verify: old1 and old2 stopped, keep/other-pane/other-node still active
	for _, tc := range []struct {
		id      string
		stopped bool
	}{
		{"old1", true}, {"old2", true}, {"keep", false}, {"other-pane", false}, {"other-node", false},
	} {
		sess, _ := s.GetSession(tc.id)
		if tc.stopped && sess.StoppedAt.IsZero() {
			t.Errorf("session %s should be stopped", tc.id)
		}
		if !tc.stopped && !sess.StoppedAt.IsZero() {
			t.Errorf("session %s should not be stopped", tc.id)
		}
	}

	// Empty pane is a no-op
	stopped, err = s.StopSessionsByPane("node1", "", "keep")
	if err != nil {
		t.Fatalf("StopSessionsByPane empty pane: %v", err)
	}
	if len(stopped) != 0 {
		t.Errorf("expected no stops for empty pane, got %v", stopped)
	}
}

func TestMigrationIdempotent(t *testing.T) {
	s := openTestStore(t)

	// Running migrate again should be fine
	if err := s.migrate(); err != nil {
		t.Fatalf("second migrate: %v", err)
	}

	// Should still work
	sess := &Session{ID: "test", StartedAt: time.Now()}
	if err := s.CreateSession(sess); err != nil {
		t.Fatalf("CreateSession after re-migrate: %v", err)
	}
}
