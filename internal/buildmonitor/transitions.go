//go:build !nobuildmonitor

package buildmonitor

import "slices"

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
	carryRecentRemediations(prev, next)

	for i := range next.Workflows {
		nw := &next.Workflows[i]
		pw := prevByID[nw.WorkflowID]
		wasFailing := pw != nil && pw.FirstFailureRunID != 0
		isPending := nw.RunID != 0 && nw.Status != "completed"

		// The agent works in the workflow's dedicated workspace, which
		// outlives the failure episode, so the row keeps pointing at it
		// whenever the previous row exists.
		if pw != nil {
			nw.SessionID = pw.SessionID
		}

		switch {
		case nw.RunID == 0:
			// The runs endpoint is a bounded snapshot. Absence means unknown,
			// not recovery; retain an active failure episode until a completed
			// non-failing run is actually observed.
			if wasFailing {
				nw.FirstFailureRunID = pw.FirstFailureRunID
				nw.LaunchError = pw.LaunchError
			}
		case isPending:
			if wasFailing {
				nw.FirstFailureRunID = pw.FirstFailureRunID
				nw.LaunchError = pw.LaunchError
			}
		case isFailing(nw) && !wasFailing:
			nw.FirstFailureRunID = nw.RunID
			fromUnknown := pw == nil
			events = append(events, TransitionEvent{WorkflowID: nw.WorkflowID, Kind: TransitionEnteredFailure, FromUnknown: fromUnknown, RunID: nw.RunID})
		case isFailing(nw) && wasFailing:
			nw.FirstFailureRunID = pw.FirstFailureRunID
			nw.LaunchError = pw.LaunchError
		case !isFailing(nw) && wasFailing:
			events = append(events, TransitionEvent{WorkflowID: nw.WorkflowID, Kind: TransitionRecovered, RunID: nw.RunID})
		}
	}

	return events, unitChanged(prev, next, prevByID)
}

// carryRecentRemediations preserves the durable ledger across fresh API
// snapshots and migrates active pre-ledger launch state on first read.
func carryRecentRemediations(prev, next *UnitState) {
	if prev == nil {
		return
	}
	next.RecentRemediations = append([]RemediationRecord(nil), prev.RecentRemediations...)
	for i := range prev.Workflows {
		w := &prev.Workflows[i]
		if w.FirstFailureRunID == 0 || (w.SessionID == "" && w.LaunchError == "") || remediationIndex(next, w.WorkflowID, w.FirstFailureRunID) >= 0 {
			continue
		}
		status := RemediationLaunched
		if w.LaunchError != "" {
			status = RemediationFailed
		}
		next.RecentRemediations = append(next.RecentRemediations, RemediationRecord{
			WorkflowID: w.WorkflowID, WorkflowName: w.Name, RunID: w.FirstFailureRunID,
			HeadSHA: w.HeadSHA, Status: status,
			SessionID: w.SessionID, LaunchError: w.LaunchError,
		})
	}
	if len(next.RecentRemediations) > maxRecentRemediations {
		next.RecentRemediations = append([]RemediationRecord(nil), next.RecentRemediations[len(next.RecentRemediations)-maxRecentRemediations:]...)
	}
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
	if !slices.Equal(prev.RecentRemediations, next.RecentRemediations) {
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
		if pw.HeadSHA != nw.HeadSHA || pw.SessionID != nw.SessionID || pw.LaunchError != nw.LaunchError {
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
