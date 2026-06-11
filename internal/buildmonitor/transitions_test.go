//go:build !nobuildmonitor

package buildmonitor

import (
	"reflect"
	"testing"
)

// twf builds a completed-run workflow snapshot.
func twf(id, runID int64, conclusion string) WorkflowState {
	return WorkflowState{WorkflowID: id, RunID: runID, Status: "completed", Conclusion: conclusion}
}

// twfFailing builds a workflow already in the failing state.
func twfFailing(id, runID, firstFailureRunID int64) WorkflowState {
	w := twf(id, runID, "failure")
	w.FirstFailureRunID = firstFailureRunID
	return w
}

func TestApplyTransitions(t *testing.T) {
	tests := []struct {
		name             string
		prev             *UnitState
		next             *UnitState
		wantEvents       []TransitionEvent
		wantChanged      bool
		wantFirstFailure map[int64]int64 // workflow ID → expected FirstFailureRunID on next
	}{
		{
			name:        "first check ever is a change with no failure events",
			prev:        nil,
			next:        &UnitState{Workflows: []WorkflowState{twf(1, 10, "success")}},
			wantChanged: true,
		},
		{
			name:             "first check ever of a failing workflow is entered_failure from unknown",
			prev:             nil,
			next:             &UnitState{Workflows: []WorkflowState{twf(1, 11, "failure")}},
			wantEvents:       []TransitionEvent{{WorkflowID: 1, Kind: TransitionEnteredFailure, FromUnknown: true, RunID: 11}},
			wantChanged:      true,
			wantFirstFailure: map[int64]int64{1: 11},
		},
		{
			name:             "success to failure enters failing",
			prev:             &UnitState{Workflows: []WorkflowState{twf(1, 10, "success")}},
			next:             &UnitState{Workflows: []WorkflowState{twf(1, 11, "failure")}},
			wantEvents:       []TransitionEvent{{WorkflowID: 1, Kind: TransitionEnteredFailure, RunID: 11}},
			wantChanged:      true,
			wantFirstFailure: map[int64]int64{1: 11},
		},
		{
			name:             "failure to failure carries FirstFailureRunID, no event",
			prev:             &UnitState{Workflows: []WorkflowState{twfFailing(1, 11, 11)}},
			next:             &UnitState{Workflows: []WorkflowState{twf(1, 12, "failure")}},
			wantChanged:      true, // new run ID is an observable change
			wantFirstFailure: map[int64]int64{1: 11},
		},
		{
			name:             "failure to failure on the same run is no change",
			prev:             &UnitState{Workflows: []WorkflowState{twfFailing(1, 11, 11)}},
			next:             &UnitState{Workflows: []WorkflowState{twf(1, 11, "failure")}},
			wantChanged:      false,
			wantFirstFailure: map[int64]int64{1: 11},
		},
		{
			name:             "failure to success recovers and clears FirstFailureRunID",
			prev:             &UnitState{Workflows: []WorkflowState{twfFailing(1, 11, 11)}},
			next:             &UnitState{Workflows: []WorkflowState{twf(1, 12, "success")}},
			wantEvents:       []TransitionEvent{{WorkflowID: 1, Kind: TransitionRecovered, RunID: 12}},
			wantChanged:      true,
			wantFirstFailure: map[int64]int64{1: 0},
		},
		{
			name:             "new failing workflow alongside a known one is entered_failure from unknown",
			prev:             &UnitState{Workflows: []WorkflowState{twf(2, 20, "success")}},
			next:             &UnitState{Workflows: []WorkflowState{twf(2, 20, "success"), twf(1, 11, "failure")}},
			wantEvents:       []TransitionEvent{{WorkflowID: 1, Kind: TransitionEnteredFailure, FromUnknown: true, RunID: 11}},
			wantChanged:      true,
			wantFirstFailure: map[int64]int64{1: 11},
		},
		{
			name:        "cancelled is not a failure (hard-failure allowlist)",
			prev:        &UnitState{Workflows: []WorkflowState{twf(1, 10, "success")}},
			next:        &UnitState{Workflows: []WorkflowState{twf(1, 11, "cancelled")}},
			wantChanged: true,
		},
		{
			name:             "failure to cancelled recovers",
			prev:             &UnitState{Workflows: []WorkflowState{twfFailing(1, 11, 11)}},
			next:             &UnitState{Workflows: []WorkflowState{twf(1, 12, "cancelled")}},
			wantEvents:       []TransitionEvent{{WorkflowID: 1, Kind: TransitionRecovered, RunID: 12}},
			wantChanged:      true,
			wantFirstFailure: map[int64]int64{1: 0},
		},
		{
			name:        "workflow removed is a change",
			prev:        &UnitState{Workflows: []WorkflowState{twf(1, 10, "success"), twf(2, 20, "success")}},
			next:        &UnitState{Workflows: []WorkflowState{twf(1, 10, "success")}},
			wantChanged: true,
		},
		{
			name:        "workflow renamed with the same run is a change",
			prev:        &UnitState{Workflows: []WorkflowState{{WorkflowID: 1, Name: "CI", Path: ".github/workflows/ci.yml", RunID: 10, Status: "completed", Conclusion: "success"}}},
			next:        &UnitState{Workflows: []WorkflowState{{WorkflowID: 1, Name: "CI v2", Path: ".github/workflows/ci.yml", RunID: 10, Status: "completed", Conclusion: "success"}}},
			wantChanged: true,
		},
		{
			name:        "error appearing is a change",
			prev:        &UnitState{Workflows: []WorkflowState{twf(1, 10, "success")}},
			next:        &UnitState{Workflows: []WorkflowState{twf(1, 10, "success")}, LastError: "unauthorized"},
			wantChanged: true,
		},
		{
			name:        "error clearing is a change",
			prev:        &UnitState{Workflows: []WorkflowState{twf(1, 10, "success")}, LastError: "unauthorized"},
			next:        &UnitState{Workflows: []WorkflowState{twf(1, 10, "success")}},
			wantChanged: true,
		},
		{
			name:             "failed jobs differing on the same run is a change",
			prev:             &UnitState{Workflows: []WorkflowState{{WorkflowID: 1, RunID: 11, Status: "completed", Conclusion: "failure", FirstFailureRunID: 11, FailedJobs: []FailedJob{{Name: "test", HTMLURL: "j1"}}}}},
			next:             &UnitState{Workflows: []WorkflowState{{WorkflowID: 1, RunID: 11, Status: "completed", Conclusion: "failure", FailedJobs: []FailedJob{{Name: "test", HTMLURL: "j1"}, {Name: "build", HTMLURL: "j2"}}}}},
			wantChanged:      true,
			wantFirstFailure: map[int64]int64{1: 11},
		},
		{
			name:        "only CheckedAt differing is no change",
			prev:        &UnitState{Workflows: []WorkflowState{twf(1, 10, "success")}, CheckedAt: "2026-06-10T10:00:00Z"},
			next:        &UnitState{Workflows: []WorkflowState{twf(1, 10, "success")}, CheckedAt: "2026-06-10T10:05:00Z"},
			wantChanged: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			events, changed := ApplyTransitions(tt.prev, tt.next)
			if changed != tt.wantChanged {
				t.Errorf("changed = %v, want %v", changed, tt.wantChanged)
			}
			if !reflect.DeepEqual(events, tt.wantEvents) {
				t.Errorf("events = %+v, want %+v", events, tt.wantEvents)
			}
			for id, want := range tt.wantFirstFailure {
				found := false
				for i := range tt.next.Workflows {
					if tt.next.Workflows[i].WorkflowID == id {
						found = true
						if got := tt.next.Workflows[i].FirstFailureRunID; got != want {
							t.Errorf("workflow %d FirstFailureRunID = %d, want %d", id, got, want)
						}
					}
				}
				if !found {
					t.Errorf("workflow %d not present in next", id)
				}
			}
		})
	}
}
