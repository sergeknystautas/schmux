package events

import (
	"context"
	"encoding/json"
	"testing"
)

func TestMonitorHandler_ForwardsAllEventTypes(t *testing.T) {
	var received []struct {
		sessionID string
		rawType   string
		data      []byte
	}

	h := NewMonitorHandler(func(sessionID string, raw RawEvent, data []byte) {
		received = append(received, struct {
			sessionID string
			rawType   string
			data      []byte
		}{sessionID, raw.Type, data})
	})

	events := []struct {
		eventType string
		payload   any
	}{
		{"status", StatusEvent{Ts: "2024-01-01T00:00:00Z", Type: "status", State: "working"}},
		{"failure", FailureEvent{Ts: "2024-01-01T00:00:01Z", Type: "failure", Tool: "bash", Error: "not found"}},
		{"reflection", ReflectionEvent{Ts: "2024-01-01T00:00:02Z", Type: "reflection", Text: "use X instead"}},
		{"friction", FrictionEvent{Ts: "2024-01-01T00:00:03Z", Type: "friction", Text: "slow build"}},
	}

	for _, e := range events {
		data, _ := json.Marshal(e.payload)
		raw := RawEvent{Ts: "2024-01-01T00:00:00Z", Type: e.eventType}
		h.HandleEvent(context.Background(), "s1", raw, data)
	}

	if len(received) != 4 {
		t.Fatalf("expected 4 events forwarded, got %d", len(received))
	}

	for i, e := range events {
		if received[i].sessionID != "s1" {
			t.Errorf("event %d: sessionID = %q, want %q", i, received[i].sessionID, "s1")
		}
		if received[i].rawType != e.eventType {
			t.Errorf("event %d: type = %q, want %q", i, received[i].rawType, e.eventType)
		}
	}
}
