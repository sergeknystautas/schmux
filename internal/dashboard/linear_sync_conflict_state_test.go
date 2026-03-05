package dashboard

import (
	"encoding/json"
	"sync"
	"testing"
)

func TestAddStep(t *testing.T) {
	s := &LinearSyncResolveConflictState{}

	idx := s.AddStep(LinearSyncResolveConflictStep{
		Action: "rebase",
		Status: "in_progress",
	})

	if idx != 0 {
		t.Errorf("first AddStep returned index %d, want 0", idx)
	}
	if len(s.Steps) != 1 {
		t.Fatalf("expected 1 step, got %d", len(s.Steps))
	}
	if s.Steps[0].Action != "rebase" {
		t.Errorf("step action = %q, want %q", s.Steps[0].Action, "rebase")
	}
	if s.Steps[0].At == "" {
		t.Error("step At should be auto-filled when empty")
	}
}

func TestAddStep_PreservesExplicitAt(t *testing.T) {
	s := &LinearSyncResolveConflictState{}

	s.AddStep(LinearSyncResolveConflictStep{
		Action: "test",
		Status: "in_progress",
		At:     "2024-01-01T00:00:00Z",
	})

	if s.Steps[0].At != "2024-01-01T00:00:00Z" {
		t.Errorf("expected explicit At preserved, got %q", s.Steps[0].At)
	}
}

func TestAddStep_MultipleSteps(t *testing.T) {
	s := &LinearSyncResolveConflictState{}

	idx0 := s.AddStep(LinearSyncResolveConflictStep{Action: "step1", Status: "done"})
	idx1 := s.AddStep(LinearSyncResolveConflictStep{Action: "step2", Status: "in_progress"})

	if idx0 != 0 || idx1 != 1 {
		t.Errorf("indices = (%d, %d), want (0, 1)", idx0, idx1)
	}
	if len(s.Steps) != 2 {
		t.Fatalf("expected 2 steps, got %d", len(s.Steps))
	}
}

func TestUpdateStep(t *testing.T) {
	s := &LinearSyncResolveConflictState{}
	s.AddStep(LinearSyncResolveConflictStep{Action: "rebase", Status: "in_progress"})

	s.UpdateStep(0, func(step *LinearSyncResolveConflictStep) {
		step.Status = "done"
		step.Message = []string{"completed"}
	})

	if s.Steps[0].Status != "done" {
		t.Errorf("status = %q, want %q", s.Steps[0].Status, "done")
	}
	if len(s.Steps[0].Message) != 1 || s.Steps[0].Message[0] != "completed" {
		t.Errorf("message = %v, want [completed]", s.Steps[0].Message)
	}
}

func TestUpdateStep_OutOfBounds(t *testing.T) {
	s := &LinearSyncResolveConflictState{}

	// Should not panic on out-of-bounds
	s.UpdateStep(-1, func(step *LinearSyncResolveConflictStep) {
		t.Error("callback should not be called for negative index")
	})
	s.UpdateStep(0, func(step *LinearSyncResolveConflictStep) {
		t.Error("callback should not be called for empty steps")
	})
	s.UpdateStep(100, func(step *LinearSyncResolveConflictStep) {
		t.Error("callback should not be called for out-of-bounds index")
	})
}

func TestUpdateLastMatchingStep(t *testing.T) {
	s := &LinearSyncResolveConflictState{}
	s.AddStep(LinearSyncResolveConflictStep{Action: "resolve", Status: "done", LocalCommit: "abc123"})
	s.AddStep(LinearSyncResolveConflictStep{Action: "resolve", Status: "in_progress", LocalCommit: "def456"})
	s.AddStep(LinearSyncResolveConflictStep{Action: "resolve", Status: "in_progress", LocalCommit: "abc123"})

	// Should match the last in_progress step with matching action and localCommit
	found := s.UpdateLastMatchingStep("resolve", "abc123", func(step *LinearSyncResolveConflictStep) {
		step.Status = "done"
	})

	if !found {
		t.Error("expected to find matching step")
	}
	// Step at index 2 should be updated (last match)
	if s.Steps[2].Status != "done" {
		t.Errorf("step[2] status = %q, want %q", s.Steps[2].Status, "done")
	}
	// Step at index 1 should remain in_progress
	if s.Steps[1].Status != "in_progress" {
		t.Errorf("step[1] status = %q, want %q", s.Steps[1].Status, "in_progress")
	}
}

func TestUpdateLastMatchingStep_NoLocalCommitFilter(t *testing.T) {
	s := &LinearSyncResolveConflictState{}
	s.AddStep(LinearSyncResolveConflictStep{Action: "resolve", Status: "in_progress", LocalCommit: "abc"})

	// Empty localCommit should match any step with matching action
	found := s.UpdateLastMatchingStep("resolve", "", func(step *LinearSyncResolveConflictStep) {
		step.Status = "done"
	})

	if !found {
		t.Error("expected to find matching step with empty localCommit filter")
	}
	if s.Steps[0].Status != "done" {
		t.Errorf("status = %q, want %q", s.Steps[0].Status, "done")
	}
}

func TestUpdateLastMatchingStep_NoMatch(t *testing.T) {
	s := &LinearSyncResolveConflictState{}
	s.AddStep(LinearSyncResolveConflictStep{Action: "resolve", Status: "done"})

	// Should not match because status is "done", not "in_progress"
	found := s.UpdateLastMatchingStep("resolve", "", func(step *LinearSyncResolveConflictStep) {
		t.Error("callback should not be called when no match")
	})

	if found {
		t.Error("expected no match for non-in_progress step")
	}
}

func TestUpdateLastMatchingStep_WrongAction(t *testing.T) {
	s := &LinearSyncResolveConflictState{}
	s.AddStep(LinearSyncResolveConflictStep{Action: "resolve", Status: "in_progress"})

	found := s.UpdateLastMatchingStep("rebase", "", func(step *LinearSyncResolveConflictStep) {
		t.Error("callback should not be called for wrong action")
	})

	if found {
		t.Error("expected no match for wrong action")
	}
}

func TestFinish(t *testing.T) {
	s := &LinearSyncResolveConflictState{Status: "in_progress"}

	resolutions := []LinearSyncResolveConflictResolution{
		{LocalCommit: "abc", AllResolved: true, Confidence: "high"},
	}
	s.Finish("done", "all resolved", resolutions)

	if s.Status != "done" {
		t.Errorf("status = %q, want %q", s.Status, "done")
	}
	if s.Message != "all resolved" {
		t.Errorf("message = %q, want %q", s.Message, "all resolved")
	}
	if s.FinishedAt == "" {
		t.Error("FinishedAt should be set")
	}
	if len(s.Resolutions) != 1 {
		t.Fatalf("expected 1 resolution, got %d", len(s.Resolutions))
	}
	if s.Resolutions[0].LocalCommit != "abc" {
		t.Errorf("resolution commit = %q, want %q", s.Resolutions[0].LocalCommit, "abc")
	}
}

func TestFinish_Failed(t *testing.T) {
	s := &LinearSyncResolveConflictState{Status: "in_progress"}
	s.Finish("failed", "merge conflict", nil)

	if s.Status != "failed" {
		t.Errorf("status = %q, want %q", s.Status, "failed")
	}
	if s.Resolutions != nil {
		t.Errorf("expected nil resolutions for failure, got %v", s.Resolutions)
	}
}

func TestSetHash(t *testing.T) {
	s := &LinearSyncResolveConflictState{}
	s.SetHash("abc123", "initial commit")

	if s.Hash != "abc123" {
		t.Errorf("hash = %q, want %q", s.Hash, "abc123")
	}
	if s.HashMessage != "initial commit" {
		t.Errorf("hash message = %q, want %q", s.HashMessage, "initial commit")
	}
}

func TestSetHash_SkipsEmpty(t *testing.T) {
	s := &LinearSyncResolveConflictState{}
	s.SetHash("", "should not set")

	if s.Hash != "" {
		t.Errorf("hash should remain empty when called with empty string, got %q", s.Hash)
	}
}

func TestSetHash_DoesNotOverwrite(t *testing.T) {
	s := &LinearSyncResolveConflictState{}
	s.SetHash("first", "first msg")
	s.SetHash("second", "second msg")

	if s.Hash != "first" {
		t.Errorf("hash = %q, want %q (should not overwrite)", s.Hash, "first")
	}
	if s.HashMessage != "first msg" {
		t.Errorf("hash message = %q, want %q", s.HashMessage, "first msg")
	}
}

func TestMarshalJSON(t *testing.T) {
	s := &LinearSyncResolveConflictState{
		Type:        "linear_sync_resolve_conflict",
		WorkspaceID: "ws-1",
		Status:      "in_progress",
		StartedAt:   "2024-01-01T00:00:00Z",
	}
	s.AddStep(LinearSyncResolveConflictStep{
		Action: "rebase",
		Status: "done",
		At:     "2024-01-01T00:00:01Z",
	})

	data, err := json.Marshal(s)
	if err != nil {
		t.Fatalf("MarshalJSON() error: %v", err)
	}

	var decoded map[string]interface{}
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal check failed: %v", err)
	}
	if decoded["type"] != "linear_sync_resolve_conflict" {
		t.Errorf("type = %v, want %q", decoded["type"], "linear_sync_resolve_conflict")
	}
	if decoded["workspace_id"] != "ws-1" {
		t.Errorf("workspace_id = %v, want %q", decoded["workspace_id"], "ws-1")
	}
	steps, ok := decoded["steps"].([]interface{})
	if !ok || len(steps) != 1 {
		t.Errorf("expected 1 step in JSON, got %v", decoded["steps"])
	}
}

func TestConcurrentAccess(t *testing.T) {
	s := &LinearSyncResolveConflictState{
		Status: "in_progress",
	}

	var wg sync.WaitGroup
	// Concurrent AddStep
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			s.AddStep(LinearSyncResolveConflictStep{
				Action: "resolve",
				Status: "in_progress",
			})
		}(i)
	}

	// Concurrent UpdateLastMatchingStep
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			s.UpdateLastMatchingStep("resolve", "", func(step *LinearSyncResolveConflictStep) {
				step.Status = "done"
			})
		}()
	}

	// Concurrent MarshalJSON
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			json.Marshal(s)
		}()
	}

	// Concurrent SetHash
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			s.SetHash("hash", "msg")
		}(i)
	}

	// Concurrent SetTmuxSession / ClearTmuxSession
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			if i%2 == 0 {
				s.SetTmuxSession("sess")
			} else {
				s.ClearTmuxSession()
			}
		}(i)
	}

	wg.Wait()

	// Verify no panic and state is consistent
	if len(s.Steps) != 50 {
		t.Errorf("expected 50 steps, got %d", len(s.Steps))
	}
}
