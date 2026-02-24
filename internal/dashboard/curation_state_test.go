package dashboard

import (
	"encoding/json"
	"fmt"
	"sync"
	"testing"
	"time"
)

func TestCurationTracker_StartAndGet(t *testing.T) {
	ct := NewCurationTracker()

	run, err := ct.Start("myrepo", "cur-001")
	if err != nil {
		t.Fatalf("Start failed: %v", err)
	}
	if run.ID != "cur-001" {
		t.Errorf("ID = %q, want %q", run.ID, "cur-001")
	}
	if run.Repo != "myrepo" {
		t.Errorf("Repo = %q, want %q", run.Repo, "myrepo")
	}
	if run.Done {
		t.Error("new run should not be done")
	}

	got := ct.Get("myrepo")
	if got == nil {
		t.Fatal("Get returned nil")
	}
	if got.ID != "cur-001" {
		t.Errorf("Get().ID = %q, want %q", got.ID, "cur-001")
	}
}

func TestCurationTracker_DuplicateStartReturnsError(t *testing.T) {
	ct := NewCurationTracker()

	_, err := ct.Start("myrepo", "cur-001")
	if err != nil {
		t.Fatalf("first Start failed: %v", err)
	}

	_, err = ct.Start("myrepo", "cur-002")
	if err == nil {
		t.Fatal("expected error on duplicate start")
	}
}

func TestCurationTracker_StartAfterComplete(t *testing.T) {
	ct := NewCurationTracker()

	_, err := ct.Start("myrepo", "cur-001")
	if err != nil {
		t.Fatalf("first Start failed: %v", err)
	}

	ct.Complete("myrepo", nil)

	// Should be able to start a new run after completion
	run, err := ct.Start("myrepo", "cur-002")
	if err != nil {
		t.Fatalf("second Start after complete failed: %v", err)
	}
	if run.ID != "cur-002" {
		t.Errorf("ID = %q, want %q", run.ID, "cur-002")
	}
}

func TestCurationTracker_AddEvent(t *testing.T) {
	ct := NewCurationTracker()
	ct.Start("myrepo", "cur-001")

	ct.AddEvent("myrepo", CuratorEvent{
		Repo:      "myrepo",
		EventType: "assistant",
		Raw:       json.RawMessage(`{"type":"assistant"}`),
	})
	ct.AddEvent("myrepo", CuratorEvent{
		Repo:      "myrepo",
		EventType: "result",
		Raw:       json.RawMessage(`{"type":"result"}`),
	})

	run := ct.Get("myrepo")
	if len(run.Events) != 2 {
		t.Fatalf("expected 2 events, got %d", len(run.Events))
	}
	if run.Events[0].EventType != "assistant" {
		t.Errorf("event[0].EventType = %q, want %q", run.Events[0].EventType, "assistant")
	}
}

func TestCurationTracker_Complete(t *testing.T) {
	ct := NewCurationTracker()
	ct.Start("myrepo", "cur-001")

	ct.Complete("myrepo", nil)
	run := ct.Get("myrepo")
	if !run.Done {
		t.Error("run should be done after Complete")
	}
	if run.Error != "" {
		t.Errorf("Error should be empty, got %q", run.Error)
	}
	if run.CompletedAt.IsZero() {
		t.Error("CompletedAt should be set after Complete")
	}
}

func TestCurationTracker_CompleteWithError(t *testing.T) {
	ct := NewCurationTracker()
	ct.Start("myrepo", "cur-001")

	ct.Complete("myrepo", fmt.Errorf("something went wrong"))
	run := ct.Get("myrepo")
	if !run.Done {
		t.Error("run should be done after Complete")
	}
	if run.Error != "something went wrong" {
		t.Errorf("Error = %q, want %q", run.Error, "something went wrong")
	}
}

func TestCurationTracker_Active(t *testing.T) {
	ct := NewCurationTracker()
	ct.Start("repo1", "cur-001")
	ct.Start("repo2", "cur-002")
	ct.Complete("repo1", nil)

	active := ct.Active()
	if len(active) != 1 {
		t.Fatalf("expected 1 active run, got %d", len(active))
	}
	if active[0].Repo != "repo2" {
		t.Errorf("active[0].Repo = %q, want %q", active[0].Repo, "repo2")
	}
}

func TestCurationTracker_IsRunning(t *testing.T) {
	ct := NewCurationTracker()

	if ct.IsRunning("myrepo") {
		t.Error("IsRunning should be false for unknown repo")
	}

	ct.Start("myrepo", "cur-001")
	if !ct.IsRunning("myrepo") {
		t.Error("IsRunning should be true after Start")
	}

	ct.Complete("myrepo", nil)
	if ct.IsRunning("myrepo") {
		t.Error("IsRunning should be false after Complete")
	}
}

func TestCurationTracker_GetNonexistent(t *testing.T) {
	ct := NewCurationTracker()
	if ct.Get("nonexistent") != nil {
		t.Error("Get should return nil for nonexistent repo")
	}
}

func TestCurationTracker_ConcurrentAccess(t *testing.T) {
	ct := NewCurationTracker()
	ct.Start("myrepo", "cur-001")

	var wg sync.WaitGroup
	// Run 100 concurrent AddEvent calls
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			ct.AddEvent("myrepo", CuratorEvent{
				Repo:      "myrepo",
				EventType: "assistant",
				Raw:       json.RawMessage(fmt.Sprintf(`{"i":%d}`, i)),
			})
		}(i)
	}
	// Concurrent reads
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			ct.IsRunning("myrepo")
			ct.Get("myrepo")
			ct.Active()
		}()
	}
	wg.Wait()

	run := ct.Get("myrepo")
	if len(run.Events) != 100 {
		t.Errorf("expected 100 events, got %d", len(run.Events))
	}
}

func TestCurationTracker_Recent(t *testing.T) {
	ct := NewCurationTracker()
	ct.Start("repo1", "cur-001")
	ct.Complete("repo1", nil)

	ct.Start("repo2", "cur-002")
	ct.Complete("repo2", fmt.Errorf("some error"))

	// Both should be recent
	recent := ct.Recent(5 * time.Second)
	if len(recent) != 2 {
		t.Fatalf("expected 2 recent runs, got %d", len(recent))
	}

	// Active should be empty
	active := ct.Active()
	if len(active) != 0 {
		t.Fatalf("expected 0 active runs, got %d", len(active))
	}

	// Still-running should not be in Recent
	ct.Start("repo3", "cur-003")
	recent = ct.Recent(5 * time.Second)
	if len(recent) != 2 {
		t.Fatalf("expected 2 recent runs (active run excluded), got %d", len(recent))
	}
}

func TestCurationTracker_CleanupOnStart(t *testing.T) {
	ct := NewCurationTracker()
	ct.Start("old-repo", "cur-old")
	ct.Complete("old-repo", nil)

	// Manually backdate the CompletedAt to trigger cleanup
	ct.mu.Lock()
	ct.runs["old-repo"].CompletedAt = time.Now().Add(-10 * time.Minute)
	ct.mu.Unlock()

	// Starting a new repo should clean up the old one
	ct.Start("new-repo", "cur-new")

	if ct.Get("old-repo") != nil {
		t.Error("old completed run should have been cleaned up")
	}
	if ct.Get("new-repo") == nil {
		t.Error("new run should exist")
	}
}
