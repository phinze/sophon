package server

import (
	"encoding/json"
	"sync"
)

// EventType identifies the kind of SSE event.
type EventType string

const (
	EventNotification EventType = "notification"
	EventActivity     EventType = "activity"
	EventSessionEnd   EventType = "session_end"
	EventResponse     EventType = "response"
)

// Event is a single server-sent event.
type Event struct {
	Type    EventType       `json:"type"`
	Session string          `json:"session_id"`
	Data    json.RawMessage `json:"data,omitempty"`
}

// EventHub is a fan-out pub/sub hub keyed by session ID.
type EventHub struct {
	mu   sync.Mutex
	subs map[string]map[chan Event]struct{}
}

// NewEventHub creates a new EventHub.
func NewEventHub() *EventHub {
	return &EventHub{
		subs: make(map[string]map[chan Event]struct{}),
	}
}

// Subscribe returns a channel that receives events for the given session and
// an unsubscribe function. The caller must call the returned function when done.
func (h *EventHub) Subscribe(sessionID string) (<-chan Event, func()) {
	ch := make(chan Event, 16)

	h.mu.Lock()
	if h.subs[sessionID] == nil {
		h.subs[sessionID] = make(map[chan Event]struct{})
	}
	h.subs[sessionID][ch] = struct{}{}
	h.mu.Unlock()

	unsub := func() {
		h.mu.Lock()
		delete(h.subs[sessionID], ch)
		if len(h.subs[sessionID]) == 0 {
			delete(h.subs, sessionID)
		}
		h.mu.Unlock()
	}

	return ch, unsub
}

// Publish sends an event to all subscribers for the given session.
// If a subscriber's buffer is full the event is dropped (non-blocking).
func (h *EventHub) Publish(sessionID string, evt Event) {
	h.mu.Lock()
	subscribers := h.subs[sessionID]
	// Copy the set under lock so we can send without holding it.
	chs := make([]chan Event, 0, len(subscribers))
	for ch := range subscribers {
		chs = append(chs, ch)
	}
	h.mu.Unlock()

	for _, ch := range chs {
		select {
		case ch <- evt:
		default:
			// Buffer full — drop event; client can refetch via transcript API.
		}
	}
}

// SubscriberCount returns the number of active subscribers for a session.
func (h *EventHub) SubscriberCount(sessionID string) int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return len(h.subs[sessionID])
}

// mustJSON marshals v to json.RawMessage, panicking on error.
func mustJSON(v interface{}) json.RawMessage {
	data, err := json.Marshal(v)
	if err != nil {
		panic("mustJSON: " + err.Error())
	}
	return json.RawMessage(data)
}
