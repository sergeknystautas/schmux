package flights

import (
	"testing"
	"time"
)

func TestWorkflowStateValid(t *testing.T) {
	for _, tt := range []struct {
		state WorkflowState
		want  bool
	}{
		{StateBacklog, true},
		{StatePlanning, true},
		{StateExecuting, true},
		{StateReviewing, true},
		{StateCompleting, true},
		{StateDone, true},
		{"flying", false},
		{"", false},
	} {
		if got := tt.state.valid(); got != tt.want {
			t.Errorf("WorkflowState(%q).valid() = %v, want %v", tt.state, got, tt.want)
		}
	}
	if StateReviewing != "reviewing" {
		t.Errorf("StateReviewing = %q, want %q", StateReviewing, "reviewing")
	}
}

func TestSessionRoleValid(t *testing.T) {
	for _, tt := range []struct {
		role SessionRole
		want bool
	}{
		{RolePlanner, true},
		{RoleExecutor, true},
		{RoleCommit, true},
		{RolePilot, true},
		{RoleReviewer, true},
		{"navigator", false},
		{"", false},
	} {
		if got := tt.role.valid(); got != tt.want {
			t.Errorf("SessionRole(%q).valid() = %v, want %v", tt.role, got, tt.want)
		}
	}
	if RoleReviewer != "reviewer" {
		t.Errorf("RoleReviewer = %q, want %q", RoleReviewer, "reviewer")
	}
}

func TestOperationActionValid(t *testing.T) {
	for _, tt := range []struct {
		action OperationAction
		want   bool
	}{
		{ActionTakeoff, true},
		{ActionExecution, true},
		{ActionReview, true},
		{ActionCompletion, true},
		{ActionDone, true},
		{ActionDispose, true},
		{"launch", false},
		{"", false},
	} {
		if got := tt.action.valid(); got != tt.want {
			t.Errorf("OperationAction(%q).valid() = %v, want %v", tt.action, got, tt.want)
		}
	}
	if ActionReview != "review" {
		t.Errorf("ActionReview = %q, want %q", ActionReview, "review")
	}
}

// A cloned flight must not share the stop record or the review check with the
// original: the manager hands copies to callers that must not mutate the
// retained evidence.
func TestFlightCloneCopiesPilotStopAndReviewCheck(t *testing.T) {
	started := time.Date(2026, 10, 26, 9, 0, 0, 0, time.UTC)
	f := startedFlight("fl-clone", StateReviewing)
	f.ReviewStartedAt = &started
	f.PilotStop = &PilotStop{
		WorkflowState:        StateReviewing,
		Reason:               "the reviewer changed the same file three times",
		SourceTurnID:         "turn-pilot-2",
		SourceNotificationID: "msg-notify-1",
		CreatedAt:            started,
	}
	f.Monitor = &FlightMonitor{Turns: []MonitoredTurn{{
		ID:     "turn-reviewer-1",
		Turn:   PlannerTurn{SessionID: "sess-reviewer", StartSeq: 1, EndSeq: 2, Outcome: "completed"},
		Review: &ReviewCheck{Skill: "test-rules-review invoked at record 7"},
		Status: MonitoredTurnClassified,
	}}}

	clone := f.clone()

	if clone.ReviewStartedAt == nil || !clone.ReviewStartedAt.Equal(started) {
		t.Errorf("ReviewStartedAt = %v, want %v", clone.ReviewStartedAt, started)
	}
	if clone.ReviewStartedAt == f.ReviewStartedAt {
		t.Error("ReviewStartedAt shares one pointer with the original")
	}
	if clone.PilotStop == nil {
		t.Fatal("PilotStop not cloned")
	}
	if clone.PilotStop == f.PilotStop {
		t.Error("PilotStop shares one pointer with the original")
	}
	if *clone.PilotStop != *f.PilotStop {
		t.Errorf("PilotStop = %+v, want %+v", *clone.PilotStop, *f.PilotStop)
	}
	review := clone.Monitor.Turns[0].Review
	if review == nil {
		t.Fatal("Review check not cloned")
	}
	if review == f.Monitor.Turns[0].Review {
		t.Error("Review check shares one pointer with the original")
	}
	if *review != *f.Monitor.Turns[0].Review {
		t.Errorf("Review check = %+v, want %+v", *review, *f.Monitor.Turns[0].Review)
	}
}
