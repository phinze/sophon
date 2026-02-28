package server

import (
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"net/http"
	"time"

	"github.com/phinze/sophon/store"
	"github.com/phinze/sophon/transcript"
)

//go:embed templates/*.html
var templateFS embed.FS

//go:embed all:static
var staticFS embed.FS

var appHTML []byte

func init() {
	var err error
	appHTML, err = templateFS.ReadFile("templates/app.html")
	if err != nil {
		panic("missing templates/app.html: " + err.Error())
	}
}

// Config holds server configuration.
type Config struct {
	Port          int
	BaseURL       string
	MinSessionAge int // seconds since last activity before turn-end sends notification
}

// NodeOps abstracts per-node operations that may be proxied to a remote agent.
type NodeOps interface {
	PaneFocused(nodeName, pane string) bool
	SendKeys(nodeName, pane, text string) error
	ReadTranscript(nodeName, sessionID, cwd string) (*transcript.Transcript, error)
}

// Server is the sophon HTTP server.
type Server struct {
	cfg     Config
	store   *store.Store
	logger  *slog.Logger
	agents  *AgentRegistry
	nodeOps NodeOps
	events  *EventHub
}

// New creates a new Server.
func New(cfg Config, st *store.Store, logger *slog.Logger) *Server {
	s := &Server{
		cfg:    cfg,
		store:  st,
		logger: logger,
		agents: NewAgentRegistry(),
		events: NewEventHub(),
	}
	s.nodeOps = &agentProxyOps{
		agents: s.agents,
		client: newAgentClient(),
		logger: logger,
	}
	return s
}

// agentProxyOps implements NodeOps by proxying to registered agents.
type agentProxyOps struct {
	agents *AgentRegistry
	client *agentClient
	logger *slog.Logger
}

func (o *agentProxyOps) PaneFocused(nodeName, pane string) bool {
	info, ok := o.agents.Get(nodeName)
	if !ok || !o.agents.IsHealthy(nodeName) {
		o.logger.Debug("no healthy agent for pane focus check", "node", nodeName)
		return false
	}
	focused, err := o.client.PaneFocused(info.URL, pane)
	if err != nil {
		o.logger.Debug("agent pane-focused error", "node", nodeName, "error", err)
		return false
	}
	return focused
}

func (o *agentProxyOps) SendKeys(nodeName, pane, text string) error {
	info, ok := o.agents.Get(nodeName)
	if !ok || !o.agents.IsHealthy(nodeName) {
		return fmt.Errorf("no healthy agent for node %q", nodeName)
	}
	return o.client.SendKeys(info.URL, pane, text)
}

func (o *agentProxyOps) ReadTranscript(nodeName, sessionID, cwd string) (*transcript.Transcript, error) {
	info, ok := o.agents.Get(nodeName)
	if !ok || !o.agents.IsHealthy(nodeName) {
		return &transcript.Transcript{}, nil
	}
	tr, err := o.client.GetTranscript(info.URL, sessionID, cwd)
	if err != nil {
		o.logger.Debug("agent transcript error", "node", nodeName, "error", err)
		return &transcript.Transcript{}, nil
	}
	return tr, nil
}

const stoppedSessionTTL = 24 * time.Hour

// Run starts the HTTP server.
func (s *Server) Run() error {
	go s.reapSessions()

	mux := http.NewServeMux()

	// API routes
	mux.HandleFunc("POST /api/sessions", s.handleCreateSession)
	mux.HandleFunc("POST /api/sessions/{id}/notify", s.handleNotify)
	mux.HandleFunc("POST /api/sessions/{id}/activity", s.handleActivity)
	mux.HandleFunc("DELETE /api/sessions/{id}", s.handleDeleteSession)
	mux.HandleFunc("POST /api/respond/{id}", s.handleRespond)
	mux.HandleFunc("GET /api/sessions/{id}/transcript", s.handleTranscript)
	mux.HandleFunc("GET /api/sessions/{id}/events", s.handleSSE)
	mux.HandleFunc("GET /api/events", s.handleGlobalSSE)
	mux.HandleFunc("GET /api/sessions/{id}", s.handleGetSession)
	mux.HandleFunc("GET /api/sessions", s.handleSessionsAPI)
	mux.HandleFunc("POST /api/agents/register", s.handleAgentRegister)

	// Static assets
	staticSub, _ := fs.Sub(staticFS, "static")
	mux.Handle("GET /static/", http.StripPrefix("/static/", http.FileServerFS(staticSub)))

	// Web UI — SPA catch-all
	mux.HandleFunc("GET /respond/{id}", s.handleSPA)
	mux.HandleFunc("GET /", s.handleSPA)

	// Health check
	mux.HandleFunc("GET /health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		fmt.Fprintln(w, "ok")
	})

	addr := fmt.Sprintf("0.0.0.0:%d", s.cfg.Port)
	s.logger.Info("starting sophon daemon", "addr", addr)
	return http.ListenAndServe(addr, mux)
}

func (s *Server) handleCreateSession(w http.ResponseWriter, r *http.Request) {
	var req struct {
		SessionID string `json:"session_id"`
		TmuxPane  string `json:"tmux_pane"`
		Cwd       string `json:"cwd"`
		NodeName  string `json:"node_name"`
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
		NodeName:       req.NodeName,
		StartedAt:      now,
		LastActivityAt: now,
	}

	if err := s.store.CreateSession(sess); err != nil {
		s.logger.Error("failed to create session", "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	// Same-pane dedup: stop prior active sessions on the same node+pane
	if req.TmuxPane != "" {
		stopped, err := s.store.StopSessionsByPane(req.NodeName, req.TmuxPane, req.SessionID)
		if err != nil {
			s.logger.Error("failed to dedup same-pane sessions", "error", err)
		}
		for _, id := range stopped {
			s.events.Publish(id, Event{Type: EventSessionEnd, Session: id})
			s.logger.Info("auto-stopped same-pane session", "stopped_id", id, "new_id", req.SessionID)
		}
	}

	s.events.Publish(req.SessionID, Event{Type: EventSessionStart, Session: req.SessionID})

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
		NodeName         string `json:"node_name"`
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
			NodeName:  req.NodeName,
			StartedAt: time.Now(),
		}
	} else if err != nil {
		s.logger.Error("failed to get session", "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	} else {
		// Backfill project/cwd/node_name if missing
		if sess.Project == "" && req.Cwd != "" {
			sess.Cwd = req.Cwd
			sess.Project = store.ProjectFromCwd(req.Cwd)
		}
		if sess.NodeName == "" && req.NodeName != "" {
			sess.NodeName = req.NodeName
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

	s.events.Publish(id, Event{
		Type:    EventNotification,
		Session: id,
		Data:    mustJSON(map[string]string{"type": req.NotificationType, "message": req.Message, "title": req.Title}),
	})

	s.logger.Info("notification stored", "session_id", id, "type", req.NotificationType)
	w.WriteHeader(http.StatusOK)
}

// handleActivity records a turn completion (Stop hook) and optionally sends a
// notification.  It does NOT mark the session as stopped — that only happens on
// SessionEnd via handleDeleteSession.
func (s *Server) handleActivity(w http.ResponseWriter, r *http.Request) {
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

	// Duration = time since last meaningful activity (not total session age)
	activityRef := sess.LastActivityAt
	if activityRef.IsZero() {
		activityRef = sess.StartedAt
	}
	elapsed := now.Sub(activityRef)

	sess.LastActivityAt = now
	if err := s.store.UpdateSession(sess); err != nil {
		s.logger.Error("failed to update session", "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	s.events.Publish(id, Event{Type: EventActivity, Session: id})

	s.logger.Info("turn ended", "session_id", id, "elapsed_since_last_activity", elapsed.Round(time.Second))

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

	sess.StoppedAt = time.Now()
	if err := s.store.UpdateSession(sess); err != nil {
		s.logger.Error("failed to update session", "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	s.events.Publish(id, Event{Type: EventSessionEnd, Session: id})

	s.logger.Info("session ended", "session_id", id)
	w.WriteHeader(http.StatusOK)
}

func (s *Server) handleSPA(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Write(appHTML)
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

	if err := s.nodeOps.SendKeys(sess.NodeName, sess.TmuxPane, req.Text); err != nil {
		s.logger.Error("tmux send-keys failed", "error", err, "pane", sess.TmuxPane, "node", sess.NodeName)
		http.Error(w, "failed to send response: "+err.Error(), http.StatusInternalServerError)
		return
	}

	// User responding = new activity; update timestamp so next stop duration is accurate
	sess.LastActivityAt = time.Now()
	if err := s.store.UpdateSession(sess); err != nil {
		s.logger.Error("failed to update last activity", "error", err)
	}

	s.events.Publish(id, Event{Type: EventResponse, Session: id})

	s.logger.Info("response sent", "session_id", id, "pane", sess.TmuxPane, "text_len", len(req.Text))
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]string{"status": "sent"})
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

	tr, err := s.nodeOps.ReadTranscript(sess.NodeName, id, sess.Cwd)
	if err != nil {
		s.logger.Debug("transcript read failed", "error", err)
		tr = &transcript.Transcript{}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(tr)
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

func (s *Server) handleAgentRegister(w http.ResponseWriter, r *http.Request) {
	var req struct {
		NodeName   string   `json:"node_name"`
		URL        string   `json:"url"`
		AlivePanes *[]string `json:"alive_panes,omitempty"` // nil = agent couldn't check
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}

	s.agents.Register(req.NodeName, req.URL)

	// Reconcile sessions if agent reported alive panes
	if req.AlivePanes != nil {
		s.reconcileSessions(req.NodeName, *req.AlivePanes)
	}

	s.logger.Debug("agent registered", "node", req.NodeName, "url", req.URL)
	w.WriteHeader(http.StatusOK)
}

// reconcileSessions stops active sessions whose tmux pane is not in the alive set.
func (s *Server) reconcileSessions(nodeName string, alivePanes []string) {
	aliveSet := make(map[string]bool, len(alivePanes))
	for _, p := range alivePanes {
		aliveSet[p] = true
	}

	sessions, err := s.store.ListActiveSessionsByNode(nodeName)
	if err != nil {
		s.logger.Error("failed to list sessions for reconciliation", "error", err, "node", nodeName)
		return
	}

	var toStop []string
	for _, sess := range sessions {
		if sess.TmuxPane == "" {
			continue // can't reconcile sessions without pane info
		}
		if !aliveSet[sess.TmuxPane] {
			toStop = append(toStop, sess.ID)
		}
	}

	if len(toStop) == 0 {
		return
	}

	if err := s.store.StopSessions(toStop); err != nil {
		s.logger.Error("failed to stop reconciled sessions", "error", err)
		return
	}

	for _, id := range toStop {
		s.events.Publish(id, Event{Type: EventSessionEnd, Session: id})
		s.logger.Info("session reconciled (claude not running)", "session_id", id, "node", nodeName)
	}
}

func (s *Server) handleSSE(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")

	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming not supported", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")

	ch, unsub := s.events.Subscribe(id)
	defer unsub()

	// Send initial connection event
	fmt.Fprintf(w, "event: connected\ndata: {}\n\n")
	flusher.Flush()

	ctx := r.Context()
	for {
		select {
		case <-ctx.Done():
			return
		case evt, ok := <-ch:
			if !ok {
				return
			}
			data, _ := json.Marshal(evt.Data)
			fmt.Fprintf(w, "event: %s\ndata: %s\n\n", evt.Type, data)
			flusher.Flush()
		}
	}
}

func (s *Server) handleGlobalSSE(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming not supported", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")

	ch, unsub := s.events.SubscribeGlobal()
	defer unsub()

	fmt.Fprintf(w, "event: connected\ndata: {}\n\n")
	flusher.Flush()

	ctx := r.Context()
	for {
		select {
		case <-ctx.Done():
			return
		case evt, ok := <-ch:
			if !ok {
				return
			}
			data, _ := json.Marshal(evt)
			fmt.Fprintf(w, "event: %s\ndata: %s\n\n", evt.Type, data)
			flusher.Flush()
		}
	}
}

// sessionResponse extends Session with agent health info for the API.
type sessionResponse struct {
	*store.Session
	AgentOnline *bool `json:"agent_online,omitempty"` // only set for active sessions
}

func (s *Server) handleSessionsAPI(w http.ResponseWriter, r *http.Request) {
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

	// Enrich active sessions with agent_online status
	activeResp := make([]sessionResponse, len(active))
	for i, sess := range active {
		online := s.agents.IsHealthy(sess.NodeName)
		activeResp[i] = sessionResponse{Session: sess, AgentOnline: &online}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{
		"active": activeResp,
		"recent": recent,
	})
}

func (s *Server) handleGetSession(w http.ResponseWriter, r *http.Request) {
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

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(sess)
}

