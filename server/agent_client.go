package server

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"time"

	"github.com/phinze/sophon/transcript"
)

// agentClient wraps HTTP calls to agent API endpoints.
type agentClient struct {
	transcriptTimeout time.Duration
	actionTimeout     time.Duration
}

func newAgentClient() *agentClient {
	return &agentClient{
		transcriptTimeout: 10 * time.Second,
		actionTimeout:     5 * time.Second,
	}
}

// GetTranscript fetches the transcript from an agent.
func (c *agentClient) GetTranscript(agentURL, sessionID, cwd string) (*transcript.Transcript, error) {
	u := fmt.Sprintf("%s/api/transcript/%s?cwd=%s", agentURL, sessionID, url.QueryEscape(cwd))
	client := &http.Client{Timeout: c.transcriptTimeout}
	resp, err := client.Get(u)
	if err != nil {
		return nil, fmt.Errorf("agent transcript request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("agent transcript returned %d", resp.StatusCode)
	}

	var tr transcript.Transcript
	if err := json.NewDecoder(resp.Body).Decode(&tr); err != nil {
		return nil, fmt.Errorf("decoding agent transcript: %w", err)
	}
	return &tr, nil
}

// SendKeys sends a send-keys request to an agent.
func (c *agentClient) SendKeys(agentURL, pane, text string) error {
	body, _ := json.Marshal(map[string]string{"pane": pane, "text": text})
	client := &http.Client{Timeout: c.actionTimeout}
	resp, err := client.Post(agentURL+"/api/send-keys", "application/json", bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("agent send-keys request: %w", err)
	}
	resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("agent send-keys returned %d", resp.StatusCode)
	}
	return nil
}

// PaneFocused checks if a pane is focused via an agent.
func (c *agentClient) PaneFocused(agentURL, pane string) (bool, error) {
	u := fmt.Sprintf("%s/api/pane-focused?pane=%s", agentURL, url.QueryEscape(pane))
	client := &http.Client{Timeout: c.actionTimeout}
	resp, err := client.Get(u)
	if err != nil {
		return false, fmt.Errorf("agent pane-focused request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return false, fmt.Errorf("agent pane-focused returned %d", resp.StatusCode)
	}

	var result struct {
		Focused bool `json:"focused"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return false, fmt.Errorf("decoding agent pane-focused: %w", err)
	}
	return result.Focused, nil
}
