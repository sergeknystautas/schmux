//go:build !norepofeed

package repofeed

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/sergeknystautas/schmux/internal/events"
)

func TestPublisher_HandleSpawnEvent(t *testing.T) {
	p := NewPublisher(PublisherConfig{
		DeveloperEmail: "test@example.com",
		DisplayName:    "Test",
	})

	// Simulate a spawn event (state=working, intent=prompt)
	evt := events.StatusEvent{
		Type:    "status",
		State:   "working",
		Message: "Session spawned",
		Intent:  "Fix the auth bug in session manager",
	}
	data, _ := json.Marshal(evt)
	raw := events.RawEvent{Type: "status"}

	p.HandleEvent(context.Background(), "ws-001-session-abc", raw, data)

	// Check that an activity was created
	devFile := p.GetCurrentState()
	if devFile == nil {
		t.Fatal("dev file should not be nil after spawn event")
	}

	// The session ID format is "<workspace-id>-<uuid>" — we need the repo
	// For now, activities are tracked by session ID
	found := false
	for _, repo := range devFile.Repos {
		for _, act := range repo.Activities {
			if act.Intent == "Fix the auth bug in session manager" {
				found = true
				if act.Status != StatusActive {
					t.Errorf("status: got %q, want %q", act.Status, StatusActive)
				}
				if act.SessionCount != 1 {
					t.Errorf("session_count: got %d, want 1", act.SessionCount)
				}
			}
		}
	}
	if !found {
		t.Error("activity not found after spawn event")
	}
}

func TestPublisher_HandleCompletedEvent(t *testing.T) {
	p := NewPublisher(PublisherConfig{
		DeveloperEmail: "test@example.com",
		DisplayName:    "Test",
	})

	// Spawn
	spawnEvt := events.StatusEvent{Type: "status", State: "working", Message: "Session spawned", Intent: "Fix auth"}
	data, _ := json.Marshal(spawnEvt)
	p.HandleEvent(context.Background(), "ws-001-session-abc", events.RawEvent{Type: "status"}, data)

	// Complete
	completeEvt := events.StatusEvent{Type: "status", State: "completed", Message: "Done"}
	data, _ = json.Marshal(completeEvt)
	p.HandleEvent(context.Background(), "ws-001-session-abc", events.RawEvent{Type: "status"}, data)

	devFile := p.GetCurrentState()
	for _, repo := range devFile.Repos {
		for _, act := range repo.Activities {
			if act.SessionCount != 0 {
				t.Errorf("session_count should be 0 after completion, got %d", act.SessionCount)
			}
		}
	}
}
