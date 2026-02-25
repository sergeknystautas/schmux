package events

import (
	"context"
	"sync"
	"testing"
)

func TestDashboardHandlerStatusEvent(t *testing.T) {
	var mu sync.Mutex
	var capturedID string
	var capturedState string
	var capturedMessage string

	handler := NewDashboardHandler(func(sessionID, state, message, intent, blockers string) {
		mu.Lock()
		defer mu.Unlock()
		capturedID = sessionID
		capturedState = state
		capturedMessage = message
	})

	data := []byte(`{"ts":"2026-02-18T14:30:00Z","type":"status","state":"completed","message":"done"}`)
	raw := RawEvent{Ts: "2026-02-18T14:30:00Z", Type: "status"}

	handler.HandleEvent(context.Background(), "session-1", raw, data)

	mu.Lock()
	defer mu.Unlock()
	if capturedID != "session-1" {
		t.Errorf("sessionID = %v, want session-1", capturedID)
	}
	if capturedState != "completed" {
		t.Errorf("state = %v, want completed", capturedState)
	}
	if capturedMessage != "done" {
		t.Errorf("message = %v, want done", capturedMessage)
	}
}

func TestDashboardHandlerIgnoresNonStatus(t *testing.T) {
	called := false
	handler := NewDashboardHandler(func(sessionID, state, message, intent, blockers string) {
		called = true
	})

	data := []byte(`{"ts":"2026-02-18T14:30:00Z","type":"failure","tool":"Bash"}`)
	raw := RawEvent{Ts: "2026-02-18T14:30:00Z", Type: "failure"}

	handler.HandleEvent(context.Background(), "session-1", raw, data)

	if called {
		t.Error("handler should not be called for non-status events")
	}
}

func TestDashboardHandlerAllFields(t *testing.T) {
	var capturedIntent, capturedBlockers string

	handler := NewDashboardHandler(func(sessionID, state, message, intent, blockers string) {
		capturedIntent = intent
		capturedBlockers = blockers
	})

	data := []byte(`{"ts":"2026-02-18T14:30:00Z","type":"status","state":"needs_input","message":"approve?","intent":"get approval","blockers":"waiting on review"}`)
	raw := RawEvent{Ts: "2026-02-18T14:30:00Z", Type: "status"}

	handler.HandleEvent(context.Background(), "session-1", raw, data)

	if capturedIntent != "get approval" {
		t.Errorf("intent = %v, want 'get approval'", capturedIntent)
	}
	if capturedBlockers != "waiting on review" {
		t.Errorf("blockers = %v, want 'waiting on review'", capturedBlockers)
	}
}
