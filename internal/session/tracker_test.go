package session

import (
	"testing"
	"time"

	"github.com/sergeknystautas/schmux/internal/state"
)

func TestSessionTrackerAttachDetach(t *testing.T) {
	st := state.New("")
	tracker := NewSessionTracker("s1", "tmux-s1", st)

	ch1 := tracker.AttachWebSocket()
	if ch1 == nil {
		t.Fatal("expected first channel")
	}

	ch2 := tracker.AttachWebSocket()
	if ch2 == nil {
		t.Fatal("expected second channel")
	}
	if ch1 == ch2 {
		t.Fatal("expected replacement channel")
	}

	select {
	case _, ok := <-ch1:
		if ok {
			t.Fatal("expected replaced channel to be closed")
		}
	case <-time.After(100 * time.Millisecond):
		t.Fatal("expected replaced channel close signal")
	}

	tracker.DetachWebSocket(ch2)
	select {
	case _, ok := <-ch2:
		if ok {
			t.Fatal("expected detached channel to be closed")
		}
	case <-time.After(100 * time.Millisecond):
		t.Fatal("expected detached channel close signal")
	}
}

func TestSessionTrackerInputResizeWithoutPTY(t *testing.T) {
	st := state.New("")
	tracker := NewSessionTracker("s1", "tmux-s1", st)

	if err := tracker.SendInput("abc"); err == nil {
		t.Fatal("expected error when PTY is not attached")
	}
	err := tracker.Resize(80, 24)
	if err == nil {
		t.Fatal("expected error when PTY is not attached")
	}
}
