//go:build !norepofeed

package repofeed

import (
	"context"
	"encoding/json"
	"sync"
	"testing"
	"time"

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

func TestPublisher_LockForPush_BlocksConcurrent(t *testing.T) {
	p := NewPublisher(PublisherConfig{
		DeveloperEmail: "test@example.com",
		DisplayName:    "Test",
	})

	// First lock should succeed
	unlock := p.LockForPush()
	if unlock == nil {
		t.Fatal("first LockForPush should succeed")
	}

	// Second lock should return nil (already locked)
	unlock2 := p.LockForPush()
	if unlock2 != nil {
		t.Fatal("second LockForPush should return nil while first is held")
		unlock2()
	}

	// After unlocking, should be able to lock again
	unlock()
	unlock3 := p.LockForPush()
	if unlock3 == nil {
		t.Fatal("LockForPush should succeed after unlock")
	}
	unlock3()
}

func TestPublisher_LockForPush_ConcurrentSafety(t *testing.T) {
	p := NewPublisher(PublisherConfig{
		DeveloperEmail: "test@example.com",
		DisplayName:    "Test",
	})

	// Run 10 goroutines trying to lock simultaneously — exactly one should win each round
	const rounds = 5
	for round := 0; round < rounds; round++ {
		var wg sync.WaitGroup
		winners := make(chan int, 10)

		for i := 0; i < 10; i++ {
			wg.Add(1)
			go func(id int) {
				defer wg.Done()
				unlock := p.LockForPush()
				if unlock != nil {
					winners <- id
					time.Sleep(time.Millisecond) // hold briefly
					unlock()
				}
			}(i)
		}

		wg.Wait()
		close(winners)

		count := 0
		for range winners {
			count++
		}
		if count != 1 {
			t.Errorf("round %d: expected exactly 1 winner, got %d", round, count)
		}
	}
}

func TestPublisher_LastPushedAt(t *testing.T) {
	p := NewPublisher(PublisherConfig{
		DeveloperEmail: "test@example.com",
		DisplayName:    "Test",
	})

	// Initially zero
	if !p.GetLastPushedAt().IsZero() {
		t.Error("initial lastPushedAt should be zero")
	}

	// Set and get
	now := time.Now()
	p.SetLastPushedAt(now)
	got := p.GetLastPushedAt()
	if !got.Equal(now) {
		t.Errorf("lastPushedAt = %v, want %v", got, now)
	}
}
