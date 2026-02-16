package server

import (
	"sync"
	"time"
)

// AgentInfo represents a registered agent.
type AgentInfo struct {
	NodeName string
	URL      string
	LastSeen time.Time
}

const agentStaleTimeout = 90 * time.Second

// AgentRegistry tracks registered agents by node name.
type AgentRegistry struct {
	mu     sync.RWMutex
	agents map[string]*AgentInfo
}

// NewAgentRegistry creates a new AgentRegistry.
func NewAgentRegistry() *AgentRegistry {
	return &AgentRegistry{
		agents: make(map[string]*AgentInfo),
	}
}

// Register adds or updates an agent registration.
func (r *AgentRegistry) Register(nodeName, url string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.agents[nodeName] = &AgentInfo{
		NodeName: nodeName,
		URL:      url,
		LastSeen: time.Now(),
	}
}

// Get returns the agent info for a node, if registered.
func (r *AgentRegistry) Get(nodeName string) (*AgentInfo, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	info, ok := r.agents[nodeName]
	return info, ok
}

// IsHealthy returns true if the agent is registered and was seen recently.
func (r *AgentRegistry) IsHealthy(nodeName string) bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	info, ok := r.agents[nodeName]
	if !ok {
		return false
	}
	return time.Since(info.LastSeen) < agentStaleTimeout
}
