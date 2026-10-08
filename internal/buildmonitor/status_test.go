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
		{"startup_failure is failure", []github.WorkflowRun{run(1, "completed", "startup_failure", "a")}, "a", StatusFailure, true},
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

func wfRow(id, runID int64, status, conclusion string) WorkflowState {
	return WorkflowState{WorkflowID: id, RunID: runID, Status: status, Conclusion: conclusion}
}

func TestAggregateWorkflows(t *testing.T) {
	cases := []struct {
		name       string
		rows       []WorkflowState
		wantStatus string
		wantOK     bool
	}{
		{"no rows", nil, "", false},
		{"only rows without a run", []WorkflowState{{WorkflowID: 1}}, "", false},
		{"row without a run contributes nothing", []WorkflowState{{WorkflowID: 1}, wfRow(2, 21, "completed", "success")}, StatusSuccess, true},
		{"all success", []WorkflowState{wfRow(1, 11, "completed", "success"), wfRow(2, 21, "completed", "success")}, StatusSuccess, true},
		// Workflows are independent: CI mid-run on the newest push must not
		// hide that the scheduled Performance workflow is red.
		{"failure beats in_progress", []WorkflowState{wfRow(1, 11, "in_progress", ""), wfRow(2, 21, "completed", "failure")}, StatusFailure, true},
		{"failure beats queued", []WorkflowState{wfRow(1, 11, "queued", ""), wfRow(2, 21, "completed", "failure")}, StatusFailure, true},
		{"in_progress beats queued", []WorkflowState{wfRow(1, 11, "queued", ""), wfRow(2, 21, "in_progress", "")}, StatusInProgress, true},
		{"queued beats success", []WorkflowState{wfRow(1, 11, "queued", ""), wfRow(2, 21, "completed", "success")}, StatusQueued, true},
		{"waiting groups as queued", []WorkflowState{wfRow(1, 11, "waiting", "")}, StatusQueued, true},
		{"pending groups as queued", []WorkflowState{wfRow(1, 11, "pending", "")}, StatusQueued, true},
		{"requested groups as queued", []WorkflowState{wfRow(1, 11, "requested", "")}, StatusQueued, true},
		{"timed_out is failure", []WorkflowState{wfRow(1, 11, "completed", "timed_out")}, StatusFailure, true},
		{"startup_failure is failure", []WorkflowState{wfRow(1, 11, "completed", "startup_failure")}, StatusFailure, true},
		{"cancelled is not failure", []WorkflowState{wfRow(1, 11, "completed", "cancelled")}, StatusSuccess, true},
		{"skipped is not failure", []WorkflowState{wfRow(1, 11, "completed", "skipped")}, StatusSuccess, true},
		{"action_required is not failure", []WorkflowState{wfRow(1, 11, "completed", "action_required")}, StatusSuccess, true},
		{"neutral is not failure", []WorkflowState{wfRow(1, 11, "completed", "neutral")}, StatusSuccess, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			status, ok := AggregateWorkflows(tc.rows)
			if status != tc.wantStatus || ok != tc.wantOK {
				t.Fatalf("got (%q, %v), want (%q, %v)", status, ok, tc.wantStatus, tc.wantOK)
			}
		})
	}
}
