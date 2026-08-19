// Package workspacestatus polls GitHub for per-workspace CI status and open
// PRs, caching results for the dashboard sessions broadcast.
package workspacestatus

import "github.com/sergeknystautas/schmux/internal/github"

// CI status values surfaced to the dashboard.
const (
	CINone    = "none"
	CIPending = "pending"
	CIFailure = "failure"
	CISuccess = "success"
)

// Aggregate reduces a branch's workflow runs (newest first, GitHub API order)
// to one CI status for the given head commit. Only runs for headSHA count;
// per workflow only the newest run counts. Pending dominates failure, failure
// dominates success. The URL is the newest matching run's HTMLURL.
func Aggregate(runs []github.WorkflowRun, headSHA string) (string, string) {
	seen := map[int64]bool{}
	url := ""
	status := CINone
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
		switch {
		case r.Status != "completed":
			return CIPending, url
		case r.Conclusion == "failure" || r.Conclusion == "timed_out":
			status = CIFailure
		case status == CINone:
			status = CISuccess
		}
	}
	return status, url
}
