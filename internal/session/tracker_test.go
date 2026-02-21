package session

import (
	"testing"

	"github.com/sergeknystautas/schmux/internal/state"
)

func TestSessionTrackerInputResizeWithoutControlMode(t *testing.T) {
	st := state.New("")
	tracker := NewSessionTracker("s1", "tmux-s1", st, "", nil, nil, nil)

	if err := tracker.SendInput("abc"); err == nil {
		t.Fatal("expected error when control mode is not attached")
	}
	err := tracker.Resize(80, 24)
	if err == nil {
		t.Fatal("expected error when control mode is not attached")
	}
}

func TestSubscribeUnsubscribeOutput(t *testing.T) {
	st := state.New("")
	tracker := NewSessionTracker("s1", "tmux-nonexistent", st, "", nil, nil, nil)

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
