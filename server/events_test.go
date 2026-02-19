package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestEventHubPubSub(t *testing.T) {
	hub := NewEventHub()
	ch, unsub := hub.Subscribe("s1")
	defer unsub()

	evt := Event{Type: EventNotification, Session: "s1", Data: mustJSON(map[string]string{"msg": "hello"})}
	hub.Publish("s1", evt)

	select {
	case got := <-ch:
		if got.Type != EventNotification {
			t.Errorf("type = %q, want %q", got.Type, EventNotification)
		}
		if got.Session != "s1" {
			t.Errorf("session = %q, want %q", got.Session, "s1")
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for event")
	}
}

func TestEventHubUnsubscribe(t *testing.T) {
	hub := NewEventHub()
	_, unsub := hub.Subscribe("s1")

	if hub.SubscriberCount("s1") != 1 {
		t.Fatalf("subscriber count = %d, want 1", hub.SubscriberCount("s1"))
	}

	unsub()

	if hub.SubscriberCount("s1") != 0 {
		t.Fatalf("subscriber count = %d after unsub, want 0", hub.SubscriberCount("s1"))
	}

	// After unsub, publishing should not panic
	hub.Publish("s1", Event{Type: EventActivity, Session: "s1"})
}

func TestEventHubNoBlockOnFullBuffer(t *testing.T) {
	hub := NewEventHub()
	ch, unsub := hub.Subscribe("s1")
	defer unsub()

	// Publish 20 events — buffer is 16, so 4 should be dropped
	for i := 0; i < 20; i++ {
		hub.Publish("s1", Event{Type: EventActivity, Session: "s1"})
	}

	count := 0
	for {
		select {
		case <-ch:
			count++
		default:
			goto done
		}
	}
done:
	if count != 16 {
		t.Errorf("received %d events, want 16 (buffer size)", count)
	}
}

func TestEventHubIsolation(t *testing.T) {
	hub := NewEventHub()
	ch1, unsub1 := hub.Subscribe("s1")
	defer unsub1()
	ch2, unsub2 := hub.Subscribe("s2")
	defer unsub2()

	hub.Publish("s1", Event{Type: EventNotification, Session: "s1"})

	select {
	case <-ch1:
		// expected
	case <-time.After(time.Second):
		t.Fatal("s1 subscriber didn't receive event")
	}

	select {
	case <-ch2:
		t.Fatal("s2 subscriber should not receive s1 event")
	case <-time.After(50 * time.Millisecond):
		// expected
	}
}

func TestSSEEndpoint(t *testing.T) {
	h := newTestHarness(t)
	h.createSession(t, "s1", "%5", "/home/user/project")

	// Use cancellable context so we can stop the SSE handler
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	req := httptest.NewRequest("GET", "/api/sessions/s1/events", nil).WithContext(ctx)
	req.SetPathValue("id", "s1")
	w := httptest.NewRecorder()

	done := make(chan struct{})
	go func() {
		h.server.handleSSE(w, req)
		close(done)
	}()

	// Wait for the subscriber to register
	for i := 0; i < 50; i++ {
		if h.server.events.SubscriberCount("s1") > 0 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	// Publish an event
	h.server.events.Publish("s1", Event{
		Type:    EventNotification,
		Session: "s1",
		Data:    mustJSON(map[string]string{"msg": "test"}),
	})

	// Give the handler time to write
	time.Sleep(50 * time.Millisecond)

	// Cancel context to stop the handler
	cancel()
	<-done

	// Verify we got SSE output
	body := w.Body.String()
	if !strings.Contains(body, "event: connected") {
		t.Errorf("missing connected event in SSE output: %q", body)
	}
	if !strings.Contains(body, "event: notification") {
		t.Errorf("missing notification event in SSE output: %q", body)
	}

	// Verify content type
	ct := w.Header().Get("Content-Type")
	if ct != "text/event-stream" {
		t.Errorf("Content-Type = %q, want text/event-stream", ct)
	}
}

func TestNotifyPublishesSSEEvent(t *testing.T) {
	h := newTestHarness(t)
	h.createSession(t, "s1", "%5", "/home/user/project")

	ch, unsub := h.server.events.Subscribe("s1")
	defer unsub()

	h.notify(t, "s1", "permission_prompt", "Allow Bash?")

	select {
	case evt := <-ch:
		if evt.Type != EventNotification {
			t.Errorf("type = %q, want %q", evt.Type, EventNotification)
		}
		var data map[string]string
		json.Unmarshal(evt.Data, &data)
		if data["type"] != "permission_prompt" {
			t.Errorf("data type = %q", data["type"])
		}
		if data["message"] != "Allow Bash?" {
			t.Errorf("data message = %q", data["message"])
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for SSE event")
	}
}

func TestActivityPublishesSSEEvent(t *testing.T) {
	h := newTestHarness(t)
	h.createSession(t, "s1", "%5", "/home/user/project")

	ch, unsub := h.server.events.Subscribe("s1")
	defer unsub()

	h.turnEnd(t, "s1")

	select {
	case evt := <-ch:
		if evt.Type != EventActivity {
			t.Errorf("type = %q, want %q", evt.Type, EventActivity)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for SSE event")
	}
}

func TestDeleteSessionPublishesSSEEvent(t *testing.T) {
	h := newTestHarness(t)
	h.createSession(t, "s1", "%5", "/home/user/project")

	ch, unsub := h.server.events.Subscribe("s1")
	defer unsub()

	h.endSession(t, "s1")

	select {
	case evt := <-ch:
		if evt.Type != EventSessionEnd {
			t.Errorf("type = %q, want %q", evt.Type, EventSessionEnd)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for SSE event")
	}
}

func TestRespondPublishesSSEEvent(t *testing.T) {
	h := newTestHarness(t)
	h.createSession(t, "s1", "%5", "/home/user/project")

	ch, unsub := h.server.events.Subscribe("s1")
	defer unsub()

	body, _ := json.Marshal(map[string]string{"text": "yes"})
	req := httptest.NewRequest("POST", "/api/respond/s1", strings.NewReader(string(body)))
	req.SetPathValue("id", "s1")
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	h.server.handleRespond(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("respond: got %d", w.Code)
	}

	select {
	case evt := <-ch:
		if evt.Type != EventResponse {
			t.Errorf("type = %q, want %q", evt.Type, EventResponse)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for SSE event")
	}
}
