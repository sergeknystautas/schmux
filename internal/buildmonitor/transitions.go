//go:build !nobuildmonitor

package buildmonitor

// Transition kinds reported by ApplyTransitions.
const (
	TransitionEnteredFailure = "entered_failure"
	TransitionRecovered      = "recovered"
)

// TransitionEvent records one workflow's failure-state transition between
// two consecutive checks of a unit.
type TransitionEvent struct {
	WorkflowID  int64
	Kind        string // TransitionEnteredFailure | TransitionRecovered
	FromUnknown bool   // no prior observation of this workflow existed
	RunID       int64
}

// isFailing reports whether a workflow's latest completed run is a hard
// failure. This is an allowlist of one: any other conclusion (cancelled,
// timed_out, skipped, …) is not failing.
func isFailing(w *WorkflowState) bool {
	return w.Conclusion == "failure"
}

// ApplyTransitions compares the previous unit state with a fresh snapshot,
// stamps FirstFailureRunID on next's workflows, and returns the transition
// events plus whether anything observable changed (CheckedAt excluded).
// prev may be nil (first check ever); next must not be nil.
func ApplyTransitions(prev, next *UnitState) ([]TransitionEvent, bool) {
	var events []TransitionEvent

	prevByID := prevWorkflowsByID(prev)

	for i := range next.Workflows {
		nw := &next.Workflows[i]
		pw := prevByID[nw.WorkflowID]
		switch {
		case isFailing(nw) && pw == nil:
			nw.FirstFailureRunID = nw.RunID
			events = append(events, TransitionEvent{WorkflowID: nw.WorkflowID, Kind: TransitionEnteredFailure, FromUnknown: true, RunID: nw.RunID})
		case isFailing(nw) && !isFailing(pw):
			nw.FirstFailureRunID = nw.RunID
			events = append(events, TransitionEvent{WorkflowID: nw.WorkflowID, Kind: TransitionEnteredFailure, RunID: nw.RunID})
		case isFailing(nw): // && isFailing(pw)
			nw.FirstFailureRunID = pw.FirstFailureRunID
		case pw != nil && isFailing(pw):
			events = append(events, TransitionEvent{WorkflowID: nw.WorkflowID, Kind: TransitionRecovered, RunID: nw.RunID})
		}
	}

	return events, unitChanged(prev, next, prevByID)
}

// prevWorkflowsByID indexes a prior unit state's workflows by workflow ID.
// Workflows with ID 0 (state written before workflow IDs were recorded) are
// skipped; they re-baseline as unknown on the next check.
func prevWorkflowsByID(prev *UnitState) map[int64]*WorkflowState {
	byID := map[int64]*WorkflowState{}
	if prev != nil {
		for i := range prev.Workflows {
			if prev.Workflows[i].WorkflowID != 0 {
				byID[prev.Workflows[i].WorkflowID] = &prev.Workflows[i]
			}
		}
	}
	return byID
}

// unitChanged reports whether observable state differs between prev and
// next, ignoring CheckedAt (a check that found nothing new is not a change).
func unitChanged(prev, next *UnitState, prevByID map[int64]*WorkflowState) bool {
	if prev == nil {
		return true
	}
	if prev.LastError != next.LastError || prev.Branch != next.Branch {
		return true
	}
	if len(prev.Workflows) != len(next.Workflows) {
		return true
	}
	for i := range next.Workflows {
		nw := &next.Workflows[i]
		pw, ok := prevByID[nw.WorkflowID]
		if !ok {
			return true
		}
		if pw.Name != nw.Name || pw.Path != nw.Path {
			return true
		}
		if pw.RunID != nw.RunID || pw.Status != nw.Status || pw.Conclusion != nw.Conclusion {
			return true
		}
		if len(pw.FailedJobs) != len(nw.FailedJobs) {
			return true
		}
		for j := range nw.FailedJobs {
			if pw.FailedJobs[j] != nw.FailedJobs[j] {
				return true
			}
		}
	}
	return false
}
