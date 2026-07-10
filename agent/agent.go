package agent

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log/slog"
	"net"
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
	paneFocused    func(pane string) bool
	sendKeys       func(pane, text string) error
	listAgentPanes func() (map[string]bool, error)
	listPaneTitles func() (map[string]string, error)
}

// New creates a new Agent.
func New(cfg Config, logger *slog.Logger) *Agent {
	return &Agent{
		cfg:            cfg,
		logger:         logger,
		paneFocused:    tmux.PaneFocused,
		sendKeys:       tmux.SendKeys,
		listAgentPanes: tmux.ListAgentPanes,
		listPaneTitles: tmux.ListPaneTitles,
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

	path := a.transcriptPath(r.URL.Query().Get("path"), cwd, sessionID)
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

	path := a.transcriptPath(r.URL.Query().Get("path"), cwd, sessionID)
	tr, err := transcript.Read(path)
	if err != nil {
		a.logger.Debug("summary transcript read failed", "path", path, "error", err)
		tr = &transcript.Transcript{}
	}

	summary := transcript.ExtractSummary(tr)
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(summary)
}

// transcriptPath returns the JSONL path to read. It prefers the path Claude
// Code reported via its hooks (provided), falling back to recomputing it from
// the cwd slug for sessions registered before the path was captured.
func (a *Agent) transcriptPath(provided, cwd, sessionID string) string {
	if provided != "" {
		return provided
	}
	return transcript.TranscriptPath(a.cfg.ClaudeDir, cwd, sessionID)
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

// resolveAdvertiseURL returns the URL the daemon should use to reach this agent.
// It resolves the configured advertise hostname to a concrete IPv4 address,
// because the daemon may run somewhere (e.g. a Miren container) that can route
// tailscale IPs but cannot resolve MagicDNS hostnames. It falls back to
// localhost when no advertise URL is configured, and to the configured value if
// the host is already an IP or cannot be resolved to an IPv4 address.
func resolveAdvertiseURL(raw string, port int, lookup func(string) ([]net.IP, error), logger *slog.Logger) string {
	if raw == "" {
		return fmt.Sprintf("http://127.0.0.1:%d", port)
	}
	u, err := url.Parse(raw)
	if err != nil {
		logger.Warn("could not parse advertise URL; advertising as-is", "url", raw, "error", err)
		return raw
	}
	host := u.Hostname()
	if host == "" || net.ParseIP(host) != nil {
		return raw // nothing to resolve: already an IP or no host
	}
	ips, err := lookup(host)
	if err != nil {
		logger.Warn("could not resolve advertise host; advertising hostname", "host", host, "error", err)
		return raw
	}
	for _, ip := range ips {
		if v4 := ip.To4(); v4 != nil {
			if p := u.Port(); p != "" {
				u.Host = net.JoinHostPort(v4.String(), p)
			} else {
				u.Host = v4.String()
			}
			return u.String()
		}
	}
	logger.Warn("no IPv4 for advertise host; advertising hostname", "host", host)
	return raw
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
	NodeName   string            `json:"node_name"`
	URL        string            `json:"url"`
	AlivePanes []string          `json:"alive_panes,omitempty"`
	PaneTitles map[string]string `json:"pane_titles,omitempty"`
}

func (a *Agent) register() {
	if a.cfg.DaemonURL == "" {
		return
	}

	agentURL := resolveAdvertiseURL(a.cfg.AdvertiseURL, a.cfg.Port, net.LookupIP, a.logger)

	payload := heartbeatPayload{
		NodeName: a.cfg.NodeName,
		URL:      agentURL,
	}

	// Detect supported agent panes; if detection fails, omit alive_panes.
	if a.listAgentPanes != nil {
		panes, err := a.listAgentPanes()
		if err != nil {
			a.logger.Debug("failed to list agent panes", "error", err)
		} else {
			// Always include the field (even empty) to signal "agent checked"
			payload.AlivePanes = make([]string, 0, len(panes))
			for paneID := range panes {
				payload.AlivePanes = append(payload.AlivePanes, paneID)
			}

			// Get pane titles for alive agent panes
			if a.listPaneTitles != nil && len(panes) > 0 {
				allTitles, err := a.listPaneTitles()
				if err != nil {
					a.logger.Debug("failed to list pane titles", "error", err)
				} else {
					payload.PaneTitles = make(map[string]string, len(panes))
					for paneID := range panes {
						if title, ok := allTitles[paneID]; ok {
							payload.PaneTitles[paneID] = title
						}
					}
				}
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
