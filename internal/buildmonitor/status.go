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
		case r.Conclusion == "failure" || r.Conclusion == "timed_out":
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
