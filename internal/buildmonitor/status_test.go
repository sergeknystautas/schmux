//go:build !nobuildmonitor

package buildmonitor

import (
	"testing"

	"github.com/sergeknystautas/schmux/internal/github"
)

func run(wfID int64, status, conclusion, sha string) github.WorkflowRun {
	return github.WorkflowRun{ID: wfID*100 + 1, WorkflowID: wfID, Status: status, Conclusion: conclusion, HeadSHA: sha, HTMLURL: "https://x"}
}

func TestAggregateRuns(t *testing.T) {
	cases := []struct {
		name       string
		runs       []github.WorkflowRun
		sha        string
		wantStatus string
		wantOK     bool
	}{
		{"no runs for sha", []github.WorkflowRun{run(1, "completed", "success", "old")}, "new", "", false},
		{"all success", []github.WorkflowRun{run(1, "completed", "success", "a"), run(2, "completed", "success", "a")}, "a", StatusSuccess, true},
		{"in_progress wins over queued and failure", []github.WorkflowRun{run(1, "queued", "", "a"), run(2, "completed", "failure", "a"), run(3, "in_progress", "", "a")}, "a", StatusInProgress, true},
		{"queued beats failure", []github.WorkflowRun{run(1, "queued", "", "a"), run(2, "completed", "failure", "a")}, "a", StatusQueued, true},
		{"failure beats success", []github.WorkflowRun{run(1, "completed", "failure", "a"), run(2, "completed", "success", "a")}, "a", StatusFailure, true},
		{"timed_out is failure", []github.WorkflowRun{run(1, "completed", "timed_out", "a")}, "a", StatusFailure, true},
		{"only newest run per workflow counts", []github.WorkflowRun{run(1, "completed", "success", "a"), {ID: 9, WorkflowID: 1, Status: "completed", Conclusion: "failure", HeadSHA: "a"}}, "a", StatusSuccess, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			status, _, ok := AggregateRuns(tc.runs, tc.sha)
			if status != tc.wantStatus || ok != tc.wantOK {
				t.Fatalf("got (%q, %v), want (%q, %v)", status, ok, tc.wantStatus, tc.wantOK)
			}
		})
	}
}
