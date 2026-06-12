package buildmonitor

import (
	"context"
	"testing"

	"github.com/sergeknystautas/schmux/internal/github"
)

type fakeActions struct {
	workflows []github.Workflow
	runs      []github.WorkflowRun
	jobs      []github.WorkflowJob
	err       error
}

func (f fakeActions) ListWorkflows(_ context.Context, _ string, _ github.RepoInfo) ([]github.Workflow, error) {
	return f.workflows, f.err
}

func (f fakeActions) ListRepoRuns(_ context.Context, _ string, _ github.RepoInfo, _ string) ([]github.WorkflowRun, error) {
	return f.runs, f.err
}

func (f fakeActions) ListRunJobs(_ context.Context, _ string, _ github.RepoInfo, _ int64) ([]github.WorkflowJob, error) {
	return f.jobs, nil
}

var testUnit = Unit{Slug: "r", Repo: "o/r", Branch: "main", Token: "tok"}

func TestCheckUnit_FailingCollectsFailedJobs(t *testing.T) {
	f := fakeActions{
		workflows: []github.Workflow{{ID: 1, Name: "CI", Path: ".github/workflows/ci.yml", State: "active"}},
		runs:      []github.WorkflowRun{{ID: 7, WorkflowID: 1, Status: "completed", Conclusion: "failure", HeadSHA: "abc1234def", HTMLURL: "u"}},
		jobs:      []github.WorkflowJob{{ID: 99, Name: "test", Conclusion: "failure", HTMLURL: "j"}, {ID: 100, Name: "build", Conclusion: "success"}},
	}
	got := CheckUnit(context.Background(), f, testUnit)
	if len(got.Workflows) != 1 {
		t.Fatalf("got=%+v", got)
	}
	wf := got.Workflows[0]
	if wf.Conclusion != "failure" || len(wf.FailedJobs) != 1 || wf.FailedJobs[0].Name != "test" {
		t.Fatalf("wf=%+v", wf)
	}
	if wf.HeadSHA != "abc1234def" {
		t.Errorf("HeadSHA = %q, want abc1234def", wf.HeadSHA)
	}
	if wf.FailedJobs[0].ID != 99 {
		t.Errorf("FailedJobs[0].ID = %d, want 99", wf.FailedJobs[0].ID)
	}
}

func TestCheckUnit_OneWorkflowPassingOneFailing(t *testing.T) {
	f := fakeActions{
		workflows: []github.Workflow{
			{ID: 1, Name: "CI", Path: ".github/workflows/ci.yml", State: "active"},
			{ID: 2, Name: "Release", Path: ".github/workflows/release.yml", State: "active"},
		},
		runs: []github.WorkflowRun{
			{ID: 8, WorkflowID: 1, RunNumber: 5, Status: "completed", Conclusion: "success", HTMLURL: "u1"},
			{ID: 9, WorkflowID: 2, Status: "completed", Conclusion: "failure", HTMLURL: "u2"},
		},
	}
	got := CheckUnit(context.Background(), f, testUnit)
	if len(got.Workflows) != 2 {
		t.Fatalf("got=%+v", got)
	}
	if got.Workflows[0].Conclusion != "success" || got.Workflows[0].RunID != 8 || got.Workflows[0].WorkflowID != 1 {
		t.Fatalf("ci=%+v", got.Workflows[0])
	}
	if got.Workflows[1].Conclusion != "failure" {
		t.Fatalf("release=%+v", got.Workflows[1])
	}
}

func TestCheckUnit_SkipsInactiveWorkflows(t *testing.T) {
	f := fakeActions{
		workflows: []github.Workflow{
			{ID: 1, Name: "CI", State: "active"},
			{ID: 2, Name: "Old", State: "disabled_manually"},
		},
	}
	got := CheckUnit(context.Background(), f, testUnit)
	if len(got.Workflows) != 1 || got.Workflows[0].Name != "CI" {
		t.Fatalf("got=%+v", got)
	}
}

func TestCheckUnit_NoRunsForWorkflow(t *testing.T) {
	f := fakeActions{
		workflows: []github.Workflow{{ID: 1, Name: "CI", State: "active"}},
	}
	got := CheckUnit(context.Background(), f, testUnit)
	if len(got.Workflows) != 1 {
		t.Fatalf("got=%+v", got)
	}
	if wf := got.Workflows[0]; wf.Status != "" || wf.Conclusion != "" {
		t.Fatalf("expected empty for no runs, got %+v", wf)
	}
}

func TestCheckUnit_RunningRun(t *testing.T) {
	f := fakeActions{
		workflows: []github.Workflow{{ID: 1, Name: "CI", State: "active"}},
		runs:      []github.WorkflowRun{{ID: 9, WorkflowID: 1, Status: "in_progress", Conclusion: ""}},
	}
	got := CheckUnit(context.Background(), f, testUnit)
	if len(got.Workflows) != 1 || got.Workflows[0].Status != "in_progress" {
		t.Fatalf("got=%+v", got)
	}
}

func TestCheckUnit_LatestCompletedSkipsNewerInProgress(t *testing.T) {
	f := fakeActions{
		workflows: []github.Workflow{{ID: 1, Name: "CI", State: "active"}},
		runs: []github.WorkflowRun{
			{ID: 11, WorkflowID: 1, Status: "in_progress"},
			{ID: 10, WorkflowID: 1, Status: "completed", Conclusion: "success"},
		},
	}
	got := CheckUnit(context.Background(), f, testUnit)
	if wf := got.Workflows[0]; wf.RunID != 10 || wf.Conclusion != "success" {
		t.Fatalf("wf=%+v", wf)
	}
}

func TestCheckUnit_Unauthorized(t *testing.T) {
	f := fakeActions{err: github.ErrUnauthorized}
	got := CheckUnit(context.Background(), f, testUnit)
	if got.LastError != "unauthorized" {
		t.Fatalf("expected unauthorized, got %q", got.LastError)
	}
}
