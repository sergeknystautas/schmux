//go:build !nobuildmonitor

package buildmonitor

import "github.com/sergeknystautas/schmux/internal/github"

// CI status values surfaced to the dashboard. Absent (no icon) is represented
// by ok=false at the derivation layer, not by a status value.
const (
	StatusQueued     = "queued"
	StatusInProgress = "in_progress"
	StatusFailure    = "failure"
	StatusSuccess    = "success"
)

// AggregateRuns reduces a branch's workflow runs (newest first, GitHub API
// order) to one CI status for the given head commit. Only runs for headSHA
// count; per workflow only the newest run counts. Precedence: in_progress >
// queued > failure > success. ok=false when no runs match headSHA. The URL is
// the newest matching run's HTMLURL.
func AggregateRuns(runs []github.WorkflowRun, headSHA string) (string, string, bool) {
	seen := map[int64]bool{}
	url := ""
	matched := false
	queued, failure, success := false, false, false
	for _, r := range runs {
		if r.HeadSHA != headSHA {
			continue
		}
		if url == "" {
			url = r.HTMLURL
		}
		if seen[r.WorkflowID] {
			continue
		}
		seen[r.WorkflowID] = true
		matched = true
		switch {
		case r.Status == "in_progress":
			return StatusInProgress, url, true
		case r.Status != "completed":
			queued = true
		case isFailureConclusion(r.Conclusion):
			failure = true
		default:
			success = true
		}
	}
	switch {
	case !matched:
		return "", "", false
	case queued:
		return StatusQueued, url, true
	case failure:
		return StatusFailure, url, true
	case success:
		return StatusSuccess, url, true
	}
	return "", "", false
}

// isFailureConclusion reports whether a completed run's conclusion makes the
// workflow red. Shared by the commit chip (AggregateRuns) and the unit
// roll-up (AggregateWorkflows). The remediation trigger, isFailing in
// transitions.go, is deliberately narrower: failure only.
func isFailureConclusion(conclusion string) bool {
	return conclusion == "failure" || conclusion == "timed_out" || conclusion == "startup_failure"
}

// AggregateWorkflows reduces a unit's workflow rows to one status. Each row
// is one workflow's newest run on the branch, whatever commit it built, so
// rows are independent. Precedence is failure > in_progress > queued >
// success: a workflow still running on the newest push never hides another
// workflow's failure. "queued" groups every not-yet-started GitHub status.
// Rows without a run are skipped; ok=false when every row is skipped.
func AggregateWorkflows(workflows []WorkflowState) (string, bool) {
	inProgress, queued, success := false, false, false
	for _, w := range workflows {
		if w.RunID == 0 {
			continue
		}
		switch {
		case w.Status == "in_progress":
			inProgress = true
		case w.Status != "completed":
			queued = true
		case isFailureConclusion(w.Conclusion):
			return StatusFailure, true
		default:
			success = true
		}
	}
	switch {
	case inProgress:
		return StatusInProgress, true
	case queued:
		return StatusQueued, true
	case success:
		return StatusSuccess, true
	}
	return "", false
}
