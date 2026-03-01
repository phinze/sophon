package agent

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"time"

	"github.com/phinze/sophon/tmux"
	"github.com/phinze/sophon/transcript"
)

// Config holds agent configuration.
type Config struct {
	Port         int
	AdvertiseURL string // URL the daemon should use to reach this agent
	DaemonURL    string
	ClaudeDir    string
	NodeName     string
}

// Agent is the per-node agent HTTP server.
type Agent struct {
	cfg    Config
	logger *slog.Logger

	// Injectable for testing
	paneFocused     func(pane string) bool
	sendKeys        func(pane, text string) error
	listClaudePanes func() (map[string]bool, error)
}

// New creates a new Agent.
func New(cfg Config, logger *slog.Logger) *Agent {
	return &Agent{
		cfg:             cfg,
		logger:          logger,
		paneFocused:     tmux.PaneFocused,
		sendKeys:        tmux.SendKeys,
		listClaudePanes: tmux.ListClaudePanes,
	}
}

// Run starts the agent HTTP server and begins heartbeat registration.
func (a *Agent) Run() error {
	go a.heartbeat()

	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/transcript/{session_id}", a.handleTranscript)
	mux.HandleFunc("GET /api/summary/{session_id}", a.handleSummary)
	mux.HandleFunc("POST /api/send-keys", a.handleSendKeys)
	mux.HandleFunc("GET /api/pane-focused", a.handlePaneFocused)
	mux.HandleFunc("GET /api/health", a.handleHealth)

	addr := fmt.Sprintf("%s:%d", a.listenHost(), a.cfg.Port)
	a.logger.Info("starting sophon agent", "addr", addr, "node", a.cfg.NodeName)
	return http.ListenAndServe(addr, mux)
}

func (a *Agent) handleTranscript(w http.ResponseWriter, r *http.Request) {
	sessionID := r.PathValue("session_id")
	cwd := r.URL.Query().Get("cwd")

	path := transcript.TranscriptPath(a.cfg.ClaudeDir, cwd, sessionID)
	tr, err := transcript.Read(path)
	if err != nil {
		a.logger.Debug("transcript read failed", "path", path, "error", err)
		tr = &transcript.Transcript{}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(tr)
}

func (a *Agent) handleSummary(w http.ResponseWriter, r *http.Request) {
	sessionID := r.PathValue("session_id")
	cwd := r.URL.Query().Get("cwd")

	path := transcript.TranscriptPath(a.cfg.ClaudeDir, cwd, sessionID)
	tr, err := transcript.Read(path)
	if err != nil {
		a.logger.Debug("summary transcript read failed", "path", path, "error", err)
		tr = &transcript.Transcript{}
	}

	summary := transcript.ExtractSummary(tr)
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(summary)
}

func (a *Agent) handleSendKeys(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Pane string `json:"pane"`
		Text string `json:"text"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}

	if err := a.sendKeys(req.Pane, req.Text); err != nil {
		a.logger.Error("send-keys failed", "error", err, "pane", req.Pane)
		http.Error(w, "send-keys failed: "+err.Error(), http.StatusInternalServerError)
		return
	}

	a.logger.Info("send-keys success", "pane", req.Pane, "text_len", len(req.Text))
	w.WriteHeader(http.StatusOK)
}

func (a *Agent) handlePaneFocused(w http.ResponseWriter, r *http.Request) {
	pane := r.URL.Query().Get("pane")
	focused := a.paneFocused(pane)
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]bool{"focused": focused})
}

func (a *Agent) handleHealth(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
	fmt.Fprintln(w, "ok")
}

// listenHost returns the host to bind to. When an advertise URL is configured,
// it extracts the hostname so the agent listens on that interface (e.g. a
// Tailscale address) rather than 0.0.0.0. Falls back to 127.0.0.1.
func (a *Agent) listenHost() string {
	if a.cfg.AdvertiseURL != "" {
		if u, err := url.Parse(a.cfg.AdvertiseURL); err == nil && u.Hostname() != "" {
			return u.Hostname()
		}
	}
	return "127.0.0.1"
}

// heartbeat registers with the daemon periodically.
func (a *Agent) heartbeat() {
	a.register()
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	for range ticker.C {
		a.register()
	}
}

// heartbeatPayload is the JSON body sent during agent registration.
type heartbeatPayload struct {
	NodeName   string   `json:"node_name"`
	URL        string   `json:"url"`
	AlivePanes []string `json:"alive_panes,omitempty"`
}

func (a *Agent) register() {
	if a.cfg.DaemonURL == "" {
		return
	}

	agentURL := a.cfg.AdvertiseURL
	if agentURL == "" {
		agentURL = fmt.Sprintf("http://127.0.0.1:%d", a.cfg.Port)
	}

	payload := heartbeatPayload{
		NodeName: a.cfg.NodeName,
		URL:      agentURL,
	}

	// Detect claude panes; if detection fails, omit alive_panes (skip reconciliation)
	if a.listClaudePanes != nil {
		panes, err := a.listClaudePanes()
		if err != nil {
			a.logger.Debug("failed to list claude panes", "error", err)
		} else {
			// Always include the field (even empty) to signal "agent checked"
			payload.AlivePanes = make([]string, 0, len(panes))
			for paneID := range panes {
				payload.AlivePanes = append(payload.AlivePanes, paneID)
			}
		}
	}

	body, _ := json.Marshal(payload)

	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Post(a.cfg.DaemonURL+"/api/agents/register", "application/json", bytes.NewReader(body))
	if err != nil {
		a.logger.Debug("agent registration failed", "error", err)
		return
	}
	resp.Body.Close()
	a.logger.Debug("agent registered", "daemon", a.cfg.DaemonURL, "alive_panes", len(payload.AlivePanes))
}
