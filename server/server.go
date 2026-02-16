package server

import (
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"html/template"
	"log/slog"
	"net/http"
	"time"

	"github.com/phinze/sophon/store"
	"github.com/phinze/sophon/transcript"
)

//go:embed templates/*.html
var templateFS embed.FS

var tmpl = template.Must(
	template.New("").Funcs(template.FuncMap{
		"timeAgo": timeAgo,
	}).ParseFS(templateFS, "templates/*.html"),
)

// Config holds server configuration.
type Config struct {
	Port          int
	NtfyURL       string
	BaseURL       string
	MinSessionAge int    // seconds before Stop sends notification
	ClaudeDir     string // Claude Code config directory (for reading transcripts)
}

// Server is the sophon HTTP server.
type Server struct {
	cfg   Config
	store *store.Store
	logger *slog.Logger

	// Injectable for testing
	paneFocused func(pane string) bool
	sendKeys    func(pane, text string) error
}

// New creates a new Server.
func New(cfg Config, st *store.Store, logger *slog.Logger) *Server {
	return &Server{
		cfg:         cfg,
		store:       st,
		logger:      logger,
		paneFocused: tmuxPaneFocused,
		sendKeys:    tmuxSendKeys,
	}
}

const stoppedSessionTTL = 24 * time.Hour

// Run starts the HTTP server.
func (s *Server) Run() error {
	go s.reapSessions()

	mux := http.NewServeMux()

	// API routes
	mux.HandleFunc("POST /api/sessions", s.handleCreateSession)
	mux.HandleFunc("POST /api/sessions/{id}/notify", s.handleNotify)
	mux.HandleFunc("DELETE /api/sessions/{id}", s.handleDeleteSession)
	mux.HandleFunc("POST /api/respond/{id}", s.handleRespond)
	mux.HandleFunc("GET /api/sessions/{id}/transcript", s.handleTranscript)

	// Web UI
	mux.HandleFunc("GET /sophon/respond/{id}", s.handleRespondPage)
	mux.HandleFunc("GET /sophon/", s.handleSessionsPage)

	// Health check
	mux.HandleFunc("GET /sophon/health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		fmt.Fprintln(w, "ok")
	})

	addr := fmt.Sprintf("127.0.0.1:%d", s.cfg.Port)
	s.logger.Info("starting sophon daemon", "addr", addr)
	return http.ListenAndServe(addr, mux)
}

func (s *Server) handleCreateSession(w http.ResponseWriter, r *http.Request) {
	var req struct {
		SessionID string `json:"session_id"`
		TmuxPane  string `json:"tmux_pane"`
		Cwd       string `json:"cwd"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}

	project := store.ProjectFromCwd(req.Cwd)

	now := time.Now()
	sess := &store.Session{
		ID:             req.SessionID,
		TmuxPane:       req.TmuxPane,
		Cwd:            req.Cwd,
		Project:        project,
		StartedAt:      now,
		LastActivityAt: now,
	}

	if err := s.store.CreateSession(sess); err != nil {
		s.logger.Error("failed to create session", "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	s.logger.Info("session registered", "session_id", req.SessionID, "project", project, "pane", req.TmuxPane)
	w.WriteHeader(http.StatusCreated)
}

func (s *Server) handleNotify(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")

	var req struct {
		NotificationType string `json:"notification_type"`
		Title            string `json:"title"`
		Message          string `json:"message"`
		Cwd              string `json:"cwd"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}

	sess, err := s.store.GetSession(id)
	if errors.Is(err, store.ErrNotFound) {
		// Create a temporary session for notifications without prior SessionStart
		sess = &store.Session{
			ID:        id,
			Cwd:       req.Cwd,
			Project:   store.ProjectFromCwd(req.Cwd),
			StartedAt: time.Now(),
		}
	} else if err != nil {
		s.logger.Error("failed to get session", "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	} else {
		// Revive stopped sessions
		if !sess.StoppedAt.IsZero() {
			s.logger.Info("reviving stopped session", "session_id", id, "stopped_for", time.Since(sess.StoppedAt).Round(time.Second), "notification_type", req.NotificationType)
			sess.StoppedAt = time.Time{}
		}
		// Backfill project/cwd if missing
		if sess.Project == "" && req.Cwd != "" {
			sess.Cwd = req.Cwd
			sess.Project = store.ProjectFromCwd(req.Cwd)
		}
	}

	now := time.Now()
	sess.NotificationType = req.NotificationType
	sess.NotifyTitle = req.Title
	sess.NotifyMessage = req.Message
	sess.NotifiedAt = now
	sess.LastActivityAt = now

	if err := s.store.CreateSession(sess); err != nil {
		s.logger.Error("failed to save session", "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	// Only push if the user isn't already looking at the pane
	if s.paneFocused(sess.TmuxPane) {
		s.logger.Info("notification suppressed (pane focused)", "session_id", id, "type", req.NotificationType)
	} else {
		s.sendNotification(sess, req.NotificationType, req.Message)
	}

	s.logger.Info("notification stored", "session_id", id, "type", req.NotificationType)
	w.WriteHeader(http.StatusOK)
}

func (s *Server) handleDeleteSession(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")

	sess, err := s.store.GetSession(id)
	if errors.Is(err, store.ErrNotFound) {
		w.WriteHeader(http.StatusOK)
		return
	} else if err != nil {
		s.logger.Error("failed to get session", "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	now := time.Now()
	sess.StoppedAt = now

	if err := s.store.UpdateSession(sess); err != nil {
		s.logger.Error("failed to update session", "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	// Duration = time since last meaningful activity (not total session age)
	activityRef := sess.LastActivityAt
	if activityRef.IsZero() {
		activityRef = sess.StartedAt
	}
	elapsed := now.Sub(activityRef)

	if int(elapsed.Seconds()) >= s.cfg.MinSessionAge {
		if s.paneFocused(sess.TmuxPane) {
			s.logger.Info("stop notification suppressed (pane focused)", "session_id", id)
		} else {
			mins := int(elapsed.Minutes())
			lastText := s.readLastAssistantText(sess)
			s.sendStopNotification(sess, mins, lastText)
		}
	}
	s.logger.Info("session stopped", "session_id", id, "work_duration", elapsed.Round(time.Second))

	w.WriteHeader(http.StatusOK)
}

type respondPageData struct {
	Session   *store.Session
	BaseURL   string
	TimeAgo   string
	HasPerm   bool
	IsStopped bool
}

func (s *Server) handleRespondPage(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")

	sess, err := s.store.GetSession(id)
	if errors.Is(err, store.ErrNotFound) {
		http.Error(w, "session not found", http.StatusNotFound)
		return
	} else if err != nil {
		s.logger.Error("failed to get session", "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	stopped := !sess.StoppedAt.IsZero()
	data := respondPageData{
		Session:   sess,
		BaseURL:   s.cfg.BaseURL,
		TimeAgo:   timeAgo(sess.NotifiedAt),
		HasPerm:   sess.NotificationType == "permission_prompt",
		IsStopped: stopped,
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := tmpl.ExecuteTemplate(w, "respond.html", data); err != nil {
		s.logger.Error("template render failed", "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
	}
}

func (s *Server) handleRespond(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")

	var req struct {
		Text string `json:"text"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}

	sess, err := s.store.GetSession(id)
	if errors.Is(err, store.ErrNotFound) {
		http.Error(w, "session not found", http.StatusNotFound)
		return
	} else if err != nil {
		s.logger.Error("failed to get session", "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	if err := s.sendKeys(sess.TmuxPane, req.Text); err != nil {
		s.logger.Error("tmux send-keys failed", "error", err, "pane", sess.TmuxPane)
		http.Error(w, "failed to send response: "+err.Error(), http.StatusInternalServerError)
		return
	}

	// User responding = new activity; update timestamp so next stop duration is accurate
	sess.LastActivityAt = time.Now()
	if err := s.store.UpdateSession(sess); err != nil {
		s.logger.Error("failed to update last activity", "error", err)
	}

	s.logger.Info("response sent", "session_id", id, "pane", sess.TmuxPane, "text_len", len(req.Text))
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]string{"status": "sent"})
}

type sessionsPageData struct {
	Active  []*store.Session
	Recent  []*store.Session
	BaseURL string
}

func (s *Server) handleSessionsPage(w http.ResponseWriter, r *http.Request) {
	active, err := s.store.ListActiveSessions()
	if err != nil {
		s.logger.Error("failed to list active sessions", "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	recent, err := s.store.ListRecentSessions(20)
	if err != nil {
		s.logger.Error("failed to list recent sessions", "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	data := sessionsPageData{
		Active:  active,
		Recent:  recent,
		BaseURL: s.cfg.BaseURL,
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := tmpl.ExecuteTemplate(w, "sessions.html", data); err != nil {
		s.logger.Error("template render failed", "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
	}
}

func (s *Server) handleTranscript(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")

	sess, err := s.store.GetSession(id)
	if errors.Is(err, store.ErrNotFound) {
		http.Error(w, "session not found", http.StatusNotFound)
		return
	} else if err != nil {
		s.logger.Error("failed to get session", "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	path := transcript.TranscriptPath(s.cfg.ClaudeDir, sess.Cwd, id)
	tr, err := transcript.Read(path)
	if err != nil {
		// Return empty transcript on error (file may not exist yet)
		s.logger.Debug("transcript read failed", "path", path, "error", err)
		tr = &transcript.Transcript{}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(tr)
}

// readLastAssistantText attempts to read the last assistant text from a session's transcript.
// Returns "" on any error.
func (s *Server) readLastAssistantText(sess *store.Session) string {
	if s.cfg.ClaudeDir == "" {
		return ""
	}
	path := transcript.TranscriptPath(s.cfg.ClaudeDir, sess.Cwd, sess.ID)
	tr, err := transcript.Read(path)
	if err != nil {
		return ""
	}
	return transcript.LastAssistantText(tr)
}

// reapSessions periodically removes sessions that have been stopped longer than the TTL.
func (s *Server) reapSessions() {
	ticker := time.NewTicker(1 * time.Minute)
	defer ticker.Stop()
	for range ticker.C {
		reaped, err := s.store.ReapStoppedSessions(stoppedSessionTTL)
		if err != nil {
			s.logger.Error("failed to reap sessions", "error", err)
			continue
		}
		for _, id := range reaped {
			s.logger.Info("session reaped", "session_id", id)
		}
	}
}

func timeAgo(t time.Time) string {
	if t.IsZero() {
		return "just now"
	}
	d := time.Since(t)
	switch {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		m := int(d.Minutes())
		if m == 1 {
			return "1m ago"
		}
		return fmt.Sprintf("%dm ago", m)
	default:
		h := int(d.Hours())
		if h == 1 {
			return "1h ago"
		}
		return fmt.Sprintf("%dh ago", h)
	}
}
