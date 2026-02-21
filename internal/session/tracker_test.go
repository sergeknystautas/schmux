package session

import (
	"testing"

	"github.com/sergeknystautas/schmux/internal/state"
)

func TestSessionTrackerInputResizeWithoutControlMode(t *testing.T) {
	st := state.New("")
	tracker := NewSessionTracker("s1", "tmux-s1", st, "", nil, nil)

	if err := tracker.SendInput("abc"); err == nil {
		t.Fatal("expected error when control mode is not attached")
	}
	err := tracker.Resize(80, 24)
	if err == nil {
		t.Fatal("expected error when control mode is not attached")
	}
}

func TestTrackerCounters_Increment(t *testing.T) {
	var c TrackerCounters
	c.EventsDelivered.Add(5)
	c.BytesDelivered.Add(1024)
	c.Reconnects.Add(1)

	if c.EventsDelivered.Load() != 5 {
		t.Errorf("EventsDelivered = %d, want 5", c.EventsDelivered.Load())
	}
	if c.BytesDelivered.Load() != 1024 {
		t.Errorf("BytesDelivered = %d, want 1024", c.BytesDelivered.Load())
	}
	if c.Reconnects.Load() != 1 {
		t.Errorf("Reconnects = %d, want 1", c.Reconnects.Load())
	}
}

func TestSubscribeUnsubscribeOutput(t *testing.T) {
	st := state.New("")
	tracker := NewSessionTracker("s1", "tmux-s1", st, "", nil, nil)

	// Subscribe creates a channel that stays open (survives reconnections)
	ch := tracker.SubscribeOutput()

	// Verify it's in the subscriber list
	tracker.subsMu.Lock()
	if len(tracker.subs) != 1 {
		t.Fatalf("expected 1 subscriber, got %d", len(tracker.subs))
	}
	tracker.subsMu.Unlock()

	// Unsubscribe removes it and closes the channel
	tracker.UnsubscribeOutput(ch)

	tracker.subsMu.Lock()
	if len(tracker.subs) != 0 {
		t.Fatalf("expected 0 subscribers after unsubscribe, got %d", len(tracker.subs))
	}
	tracker.subsMu.Unlock()

	// Channel should be closed after unsubscribe
	select {
	case _, ok := <-ch:
		if ok {
			t.Fatal("expected channel to be closed after unsubscribe")
		}
	default:
		t.Fatal("expected channel to be readable (closed)")
	}
}
