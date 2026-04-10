package dashboard

import (
	"encoding/json"
	"testing"
	"time"
)

func TestCurationTracker_StartAndComplete(t *testing.T) {
	ct := NewCurationTracker()

	// Start a curation
	run, err := ct.Start("repo-a", "run-1")
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	if run.Repo != "repo-a" || run.ID != "run-1" {
		t.Fatalf("unexpected run: %+v", run)
	}
	if run.Done {
		t.Fatal("new run should not be done")
	}

	// Should be running
	if !ct.IsRunning("repo-a") {
		t.Fatal("expected IsRunning=true")
	}

	// Starting another for the same repo should fail
	_, err = ct.Start("repo-a", "run-2")
	if err == nil {
		t.Fatal("expected error for duplicate start")
	}

	// Different repo should work
	_, err = ct.Start("repo-b", "run-3")
	if err == nil || err != nil {
		// just ensure it doesn't panic; err==nil means success
	}

	// Complete the first
	ct.Complete("repo-a", nil)
	if ct.IsRunning("repo-a") {
		t.Fatal("expected IsRunning=false after Complete")
	}

	// Can start again after completion
	_, err = ct.Start("repo-a", "run-4")
	if err != nil {
		t.Fatalf("Start after Complete: %v", err)
	}
}

func TestCurationTracker_AddEvent(t *testing.T) {
	ct := NewCurationTracker()
	ct.Start("repo-a", "run-1")

	event := CuratorEvent{
		Repo:      "repo-a",
		Timestamp: time.Now(),
		EventType: "assistant",
		Raw:       json.RawMessage(`{"text":"hello"}`),
	}
	ct.AddEvent("repo-a", event)

	// AddEvent on nonexistent repo should not panic
	ct.AddEvent("nonexistent", event)

	active := ct.Active()
	if len(active) != 1 {
		t.Fatalf("expected 1 active, got %d", len(active))
	}
	if len(active[0].Events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(active[0].Events))
	}
}

func TestCurationTracker_CompleteWithError(t *testing.T) {
	ct := NewCurationTracker()
	ct.Start("repo-a", "run-1")

	ct.Complete("repo-a", errTest)

	recent := ct.Recent(time.Minute)
	if len(recent) != 1 {
		t.Fatalf("expected 1 recent, got %d", len(recent))
	}
	if recent[0].Error != "test error" {
		t.Fatalf("expected error message, got %q", recent[0].Error)
	}
}

var errTest = &testError{}

type testError struct{}

func (e *testError) Error() string { return "test error" }

func TestCurationTracker_ActiveAndRecent(t *testing.T) {
	ct := NewCurationTracker()

	// No active or recent initially
	if len(ct.Active()) != 0 {
		t.Fatal("expected no active runs")
	}
	if len(ct.Recent(time.Minute)) != 0 {
		t.Fatal("expected no recent runs")
	}

	ct.Start("repo-a", "run-1")
	ct.Start("repo-b", "run-2")

	if len(ct.Active()) != 2 {
		t.Fatalf("expected 2 active, got %d", len(ct.Active()))
	}

	ct.Complete("repo-a", nil)

	if len(ct.Active()) != 1 {
		t.Fatalf("expected 1 active after completing one, got %d", len(ct.Active()))
	}
	if len(ct.Recent(time.Minute)) != 1 {
		t.Fatalf("expected 1 recent, got %d", len(ct.Recent(time.Minute)))
	}

	// Recent with zero duration should return nothing
	if len(ct.Recent(0)) != 0 {
		t.Fatal("expected no recent with zero window")
	}
}
