package server

import (
	"embed"
	"encoding/json"
	"fmt"
	"html/template"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"
)

//go:embed templates/*.html
var templateFS embed.FS

var tmpl = template.Must(template.ParseFS(templateFS, "templates/*.html"))

// Session represents a Claude Code session.
type Session struct {
	ID        string    `json:"session_id"`
	TmuxPane  string    `json:"tmux_pane"`
	Cwd       string    `json:"cwd"`
	Project   string    `json:"project"`
	StartedAt time.Time `json:"started_at"`

	// Latest notification context
	NotificationType string `json:"notification_type,omitempty"`
	NotifyTitle      string `json:"notify_title,omitempty"`
	NotifyMessage    string `json:"notify_message,omitempty"`
	NotifiedAt       time.Time `json:"notified_at,omitempty"`
}

// Config holds server configuration.
type Config struct {
	Port          int
	NtfyURL       string
	BaseURL       string
	MinSessionAge int // seconds before Stop sends notification
}

// Server is the sophon HTTP server.
type Server struct {
	cfg      Config
	sessions map[string]*Session
	mu       sync.RWMutex
	logger   *slog.Logger
}

// New creates a new Server.
func New(cfg Config, logger *slog.Logger) *Server {
	return &Server{
		cfg:      cfg,
		sessions: make(map[string]*Session),
		logger:   logger,
	}
}

// Run starts the HTTP server.
func (s *Server) Run() error {
	mux := http.NewServeMux()

	// API routes
	mux.HandleFunc("POST /api/sessions", s.handleCreateSession)
	mux.HandleFunc("POST /api/sessions/{id}/notify", s.handleNotify)
	mux.HandleFunc("DELETE /api/sessions/{id}", s.handleDeleteSession)
	mux.HandleFunc("POST /api/respond/{id}", s.handleRespond)

	// Web UI
	mux.HandleFunc("GET /sophon/respond/{id}", s.handleRespondPage)

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

	project := projectFromCwd(req.Cwd)

	sess := &Session{
		ID:        req.SessionID,
		TmuxPane:  req.TmuxPane,
		Cwd:       req.Cwd,
		Project:   project,
		StartedAt: time.Now(),
	}

	s.mu.Lock()
	s.sessions[req.SessionID] = sess
	s.mu.Unlock()

	s.logger.Info("session registered", "session_id", req.SessionID, "project", project, "pane", req.TmuxPane)
	w.WriteHeader(http.StatusCreated)
}

func (s *Server) handleNotify(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")

	var req struct {
		NotificationType string `json:"notification_type"`
		Title            string `json:"title"`
		Message          string `json:"message"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}

	s.mu.Lock()
	sess, ok := s.sessions[id]
	if ok {
		sess.NotificationType = req.NotificationType
		sess.NotifyTitle = req.Title
		sess.NotifyMessage = req.Message
		sess.NotifiedAt = time.Now()
	}
	s.mu.Unlock()

	if !ok {
		// Create a temporary session for notifications without prior SessionStart
		sess = &Session{
			ID:               id,
			Project:          "unknown",
			StartedAt:        time.Now(),
			NotificationType: req.NotificationType,
			NotifyTitle:      req.Title,
			NotifyMessage:    req.Message,
			NotifiedAt:       time.Now(),
		}
		s.mu.Lock()
		s.sessions[id] = sess
		s.mu.Unlock()
	}

	// Send ntfy notification
	s.sendNotification(sess, req.NotificationType, req.Message)

	s.logger.Info("notification stored", "session_id", id, "type", req.NotificationType)
	w.WriteHeader(http.StatusOK)
}

func (s *Server) handleDeleteSession(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")

	s.mu.Lock()
	sess, ok := s.sessions[id]
	if ok {
		delete(s.sessions, id)
	}
	s.mu.Unlock()

	if ok {
		elapsed := time.Since(sess.StartedAt)
		if int(elapsed.Seconds()) >= s.cfg.MinSessionAge {
			mins := int(elapsed.Minutes())
			s.sendStopNotification(sess, mins)
		}
		s.logger.Info("session removed", "session_id", id, "duration", elapsed.Round(time.Second))
	}

	w.WriteHeader(http.StatusOK)
}

type respondPageData struct {
	Session  *Session
	BaseURL  string
	TimeAgo  string
	HasPerm  bool
}

func (s *Server) handleRespondPage(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")

	s.mu.RLock()
	sess, ok := s.sessions[id]
	s.mu.RUnlock()

	if !ok {
		http.Error(w, "session not found", http.StatusNotFound)
		return
	}

	data := respondPageData{
		Session: sess,
		BaseURL: s.cfg.BaseURL,
		TimeAgo: timeAgo(sess.NotifiedAt),
		HasPerm: sess.NotificationType == "permission_prompt",
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

	s.mu.RLock()
	sess, ok := s.sessions[id]
	s.mu.RUnlock()

	if !ok {
		http.Error(w, "session not found", http.StatusNotFound)
		return
	}

	if err := tmuxSendKeys(sess.TmuxPane, req.Text); err != nil {
		s.logger.Error("tmux send-keys failed", "error", err, "pane", sess.TmuxPane)
		http.Error(w, "failed to send response: "+err.Error(), http.StatusInternalServerError)
		return
	}

	s.logger.Info("response sent", "session_id", id, "pane", sess.TmuxPane, "text_len", len(req.Text))
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]string{"status": "sent"})
}

// projectFromCwd extracts last two path components as project name.
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
