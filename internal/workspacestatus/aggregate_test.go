package workspacestatus

import (
	"testing"

	"github.com/sergeknystautas/schmux/internal/github"
)

func TestAggregate(t *testing.T) {
	run := func(wf int64, sha, status, conclusion, url string) github.WorkflowRun {
		return github.WorkflowRun{WorkflowID: wf, HeadSHA: sha, Status: status, Conclusion: conclusion, HTMLURL: url}
	}
	tests := []struct {
		name       string
		runs       []github.WorkflowRun // newest first, GitHub API order
		headSHA    string
		wantStatus string
		wantURL    string
	}{
		{"no runs", nil, "abc", CINone, ""},
		{"runs only for older sha", []github.WorkflowRun{
			run(1, "old", "completed", "success", "u1"),
		}, "abc", CINone, ""},
		{"all success", []github.WorkflowRun{
			run(1, "abc", "completed", "success", "u1"),
			run(2, "abc", "completed", "success", "u2"),
		}, "abc", CISuccess, "u1"},
		{"one failure", []github.WorkflowRun{
			run(1, "abc", "completed", "success", "u1"),
			run(2, "abc", "completed", "failure", "u2"),
		}, "abc", CIFailure, "u1"},
		{"timed_out counts as failure", []github.WorkflowRun{
			run(1, "abc", "completed", "timed_out", "u1"),
		}, "abc", CIFailure, "u1"},
		{"pending dominates failure", []github.WorkflowRun{
			run(1, "abc", "completed", "failure", "u1"),
			run(2, "abc", "in_progress", "", "u2"),
		}, "abc", CIPending, "u1"},
		{"queued is pending", []github.WorkflowRun{
			run(1, "abc", "queued", "", "u1"),
		}, "abc", CIPending, "u1"},
		{"newest run per workflow wins over older failure", []github.WorkflowRun{
			run(1, "abc", "completed", "success", "u-new"),
			run(1, "abc", "completed", "failure", "u-old"),
		}, "abc", CISuccess, "u-new"},
		{"older sha runs ignored in mix", []github.WorkflowRun{
			run(1, "abc", "completed", "success", "u1"),
			run(2, "old", "completed", "failure", "u2"),
		}, "abc", CISuccess, "u1"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			status, url := Aggregate(tt.runs, tt.headSHA)
			if status != tt.wantStatus || url != tt.wantURL {
				t.Errorf("Aggregate() = (%q, %q), want (%q, %q)", status, url, tt.wantStatus, tt.wantURL)
			}
		})
	}
}
