//go:build !nobuildmonitor

package buildmonitor

import (
	"context"
	"errors"
	"os"
	"reflect"
	"sync/atomic"
	"testing"
	"time"

	"github.com/sergeknystautas/schmux/internal/github"
)

// passFakeActions implements Actions with scripted responses. Call counts are
// tracked so tests can assert dedup / skip behavior.
type passFakeActions struct {
	workflows         []github.Workflow
	runsByBranch      map[string][]github.WorkflowRun
	jobs              map[int64][]github.WorkflowJob
	err               error
	rateLimit         bool
	listWorkflowsCall atomic.Int64
	listRunsCalls     atomic.Int64
	latestRunCalls    atomic.Int64
}

// LatestWorkflowRun returns the first run for workflowID in the branch's
// scripted runs (scripted newest first).
func (f *passFakeActions) LatestWorkflowRun(_ context.Context, _ string, _ github.RepoInfo, workflowID int64, branch string) (*github.WorkflowRun, error) {
	f.latestRunCalls.Add(1)
	if f.err != nil {
		return nil, f.err
	}
	if f.rateLimit {
		return nil, &github.RateLimitError{}
	}
	for _, r := range f.runsByBranch[branch] {
		if r.WorkflowID == workflowID {
			run := r
			return &run, nil
		}
	}
	return nil, nil
}

func (f *passFakeActions) ListWorkflows(_ context.Context, _ string, _ github.RepoInfo) ([]github.Workflow, error) {
	f.listWorkflowsCall.Add(1)
	if f.err != nil {
		return nil, f.err
	}
	return f.workflows, nil
}

func (f *passFakeActions) ListRepoRuns(_ context.Context, _ string, _ github.RepoInfo, branch string) ([]github.WorkflowRun, error) {
	f.listRunsCalls.Add(1)
	if f.err != nil {
		return nil, f.err
	}
	if f.rateLimit {
		return nil, &github.RateLimitError{}
	}
	return f.runsByBranch[branch], nil
}

func (f *passFakeActions) ListRunJobs(_ context.Context, _ string, _ github.RepoInfo, runID int64) ([]github.WorkflowJob, error) {
	return f.jobs[runID], nil
}

func testUnit(dir string) UnitInput {
	return UnitInput{
		Slug: "r", RepoName: "acme/app", Branch: "main", Token: "tok",
		Info: github.RepoInfo{Owner: "acme", Repo: "app"}, HeadSHA: "h1",
		StatePath: dir + "/state.json",
	}
}

func TestCheckPass_UnitHeadNoRunsQueued(t *testing.T) {
	dir := t.TempDir()
	actions := &passFakeActions{
		workflows:    []github.Workflow{{ID: 1, Name: "CI", Path: ".github/workflows/ci.yml", State: "active"}},
		runsByBranch: map[string][]github.WorkflowRun{},
	}
	m := NewMonitor(time.Now, "")
	res := m.CheckPass(context.Background(), actions, true, []UnitInput{testUnit(dir)}, nil)
	if !res.Changed {
		t.Errorf("expected Changed=true (first pass persists)")
	}
	// Head with no runs → recorded as queued.
	st, _, ok := m.Status(gh("acme", "app"), "main", "h1")
	if !ok || st != StatusQueued {
		t.Errorf("status = (%q, %v), want (queued, true)", st, ok)
	}
	if st := res.UnitStates["r"]; st == nil {
		t.Fatalf("missing unit state in result")
	} else if len(st.Workflows) != 1 || st.Workflows[0].RunID != 0 {
		t.Errorf("workflow row = %+v, want empty-run row", st.Workflows)
	}
}

func TestCheckPass_DefaultBranchQueuedRunWithRunningJobIsInProgress(t *testing.T) {
	dir := t.TempDir()
	actions := &passFakeActions{
		workflows: []github.Workflow{{ID: 1, Name: "CI", State: "active"}},
		runsByBranch: map[string][]github.WorkflowRun{
			"main": {{ID: 7, WorkflowID: 1, Status: "queued", HeadSHA: "h1", HTMLURL: "https://run/7"}},
		},
		jobs: map[int64][]github.WorkflowJob{
			7: {{ID: 70, Name: "test", Status: "in_progress"}},
		},
	}
	m := NewMonitor(time.Now, "")

	res := m.CheckPass(context.Background(), actions, true, []UnitInput{testUnit(dir)}, nil)

	wf := res.UnitStates["r"].Workflows[0]
	if wf.Status != "in_progress" {
		t.Fatalf("default-branch workflow status = %q, want in_progress", wf.Status)
	}
	if got, url, ok := m.Status(gh("acme", "app"), "main", "h1"); !ok || got != StatusInProgress || url != "https://run/7" {
		t.Fatalf("default-branch status = (%q, %q, %v), want (in_progress, https://run/7, true)", got, url, ok)
	}
}

func TestCheckPass_HeadFetchDedupedPerRepoBranch(t *testing.T) {
	dir := t.TempDir()
	actions := &passFakeActions{
		workflows: []github.Workflow{{ID: 1, Name: "CI", State: "active"}},
		runsByBranch: map[string][]github.WorkflowRun{
			"main":    {{ID: 7, WorkflowID: 1, Status: "completed", Conclusion: "success", HeadSHA: "h1"}},
			"feature": {{ID: 8, WorkflowID: 1, Status: "completed", Conclusion: "success", HeadSHA: "f1"}},
		},
	}
	m := NewMonitor(time.Now, "")
	heads := []HeadInput{
		{Info: gh("acme", "app"), Branch: "feature", SHA: "f1", Token: "tok"},
		{Info: gh("acme", "app"), Branch: "feature", SHA: "f1", Token: "tok"},
	}
	_ = m.CheckPass(context.Background(), actions, true, []UnitInput{testUnit(dir)}, heads)
	// The unit fetches per workflow; the two identical heads share one
	// "feature" listing.
	if got := actions.listRunsCalls.Load(); got != 1 {
		t.Errorf("ListRepoRuns calls = %d, want 1", got)
	}
}

func TestCheckPass_HeadOnUnitBranchUsesUnitResult(t *testing.T) {
	dir := t.TempDir()
	actions := &passFakeActions{
		workflows: []github.Workflow{{ID: 1, Name: "CI", State: "active"}},
		runsByBranch: map[string][]github.WorkflowRun{
			"main": {{ID: 7, WorkflowID: 1, Status: "completed", Conclusion: "success", HeadSHA: "h1"}},
		},
	}
	m := NewMonitor(time.Now, "")
	heads := []HeadInput{{Info: gh("acme", "app"), Branch: "main", SHA: "h1", Token: "tok"}}
	_ = m.CheckPass(context.Background(), actions, true, []UnitInput{testUnit(dir)}, heads)
	// The unit pass recorded main@h1 as a fresh terminal result, so the
	// watched head on the same commit needs no listing of its own.
	if got := actions.listRunsCalls.Load(); got != 0 {
		t.Errorf("ListRepoRuns calls = %d, want 0 (unit result reused)", got)
	}
	if st, _, ok := m.Status(gh("acme", "app"), "main", "h1"); !ok || st != StatusSuccess {
		t.Errorf("status = (%q, %v), want (success, true)", st, ok)
	}
}

func TestCheckPass_SameCommitOnDifferentBranchesKeepsSeparateStatuses(t *testing.T) {
	dir := t.TempDir()
	actions := &passFakeActions{
		workflows: []github.Workflow{{ID: 1, Name: "CI", State: "active"}},
		runsByBranch: map[string][]github.WorkflowRun{
			"main":      {{ID: 7, WorkflowID: 1, Status: "completed", Conclusion: "success", HeadSHA: "shared-head"}},
			"feature-a": {{ID: 8, WorkflowID: 1, Status: "queued", HeadSHA: "shared-head"}},
			"feature-b": {{ID: 9, WorkflowID: 1, Status: "in_progress", HeadSHA: "shared-head"}},
		},
	}
	m := NewMonitor(time.Now, "")
	unit := testUnit(dir)
	unit.HeadSHA = "shared-head"
	heads := []HeadInput{
		{Info: gh("acme", "app"), Branch: "feature-a", SHA: "shared-head", Token: "tok"},
		{Info: gh("acme", "app"), Branch: "feature-b", SHA: "shared-head", Token: "tok"},
	}

	res := m.CheckPass(context.Background(), actions, true, []UnitInput{unit}, heads)

	if len(res.Events) != 0 {
		t.Fatalf("non-default branch runs produced default-branch transition events: %+v", res.Events)
	}
	m.mu.Lock()
	stored := len(m.commits)
	m.mu.Unlock()
	if stored != 3 {
		t.Fatalf("stored commit statuses = %d, want 3 (same SHA on three branches)", stored)
	}
	assertStatus := func(branch, want string) {
		t.Helper()
		if got, _, ok := m.Status(gh("acme", "app"), branch, "shared-head"); !ok || got != want {
			t.Errorf("%s status = (%q, %v), want (%q, true)", branch, got, ok, want)
		}
	}
	assertStatus("main", StatusSuccess)
	assertStatus("feature-a", StatusQueued)
	assertStatus("feature-b", StatusInProgress)

	// A later pass must advance a queued remote branch independently of the
	// successful default-branch run for the same commit.
	actions.runsByBranch["feature-a"] = []github.WorkflowRun{{ID: 8, WorkflowID: 1, Status: "in_progress", HeadSHA: "shared-head"}}
	_ = m.CheckPass(context.Background(), actions, true, []UnitInput{unit}, heads)
	assertStatus("main", StatusSuccess)
	assertStatus("feature-a", StatusInProgress)
	assertStatus("feature-b", StatusInProgress)
	if got := actions.listRunsCalls.Load(); got != 4 {
		t.Errorf("ListRepoRuns calls = %d, want 4 (both feature branches on both passes; main is fetched per workflow)", got)
	}
}

func TestCheckPass_QueuedRunAdvancesWhenAJobStarts(t *testing.T) {
	dir := t.TempDir()
	actions := &passFakeActions{
		workflows: []github.Workflow{{ID: 1, Name: "CI", State: "active"}},
		runsByBranch: map[string][]github.WorkflowRun{
			"main":    {{ID: 7, WorkflowID: 1, Status: "completed", Conclusion: "success", HeadSHA: "main-head"}},
			"feature": {{ID: 8, WorkflowID: 1, Status: "queued", HeadSHA: "feature-head", HTMLURL: "https://run/8"}},
		},
		jobs: map[int64][]github.WorkflowJob{
			8: {{ID: 80, Name: "test", Status: "queued"}},
		},
	}
	m := NewMonitor(time.Now, "")
	unit := testUnit(dir)
	unit.HeadSHA = "main-head"
	heads := []HeadInput{{Info: gh("acme", "app"), Branch: "feature", SHA: "feature-head", Token: "tok"}}

	_ = m.CheckPass(context.Background(), actions, true, []UnitInput{unit}, heads)
	if got, _, ok := m.Status(gh("acme", "app"), "feature", "feature-head"); !ok || got != StatusQueued {
		t.Fatalf("initial feature status = (%q, %v), want (queued, true)", got, ok)
	}

	// GitHub can leave the workflow run itself queued after jobs have begun.
	// A manual check must observe the job state and advance the branch badge.
	actions.jobs[8] = []github.WorkflowJob{{ID: 80, Name: "test", Status: "in_progress"}}
	_ = m.CheckPass(context.Background(), actions, true, []UnitInput{unit}, heads)
	if got, url, ok := m.Status(gh("acme", "app"), "feature", "feature-head"); !ok || got != StatusInProgress || url != "https://run/8" {
		t.Fatalf("refreshed feature status = (%q, %q, %v), want (in_progress, https://run/8, true)", got, url, ok)
	}
}

func TestCheckPass_OnlyDefaultBranchFailuresProduceTransitionEvents(t *testing.T) {
	dir := t.TempDir()
	actions := &passFakeActions{
		workflows: []github.Workflow{{ID: 1, Name: "CI", State: "active"}},
		runsByBranch: map[string][]github.WorkflowRun{
			"main":    {{ID: 7, WorkflowID: 1, Status: "completed", Conclusion: "success", HeadSHA: "main-ok"}},
			"feature": {{ID: 8, WorkflowID: 1, Status: "queued", HeadSHA: "feature-head"}},
		},
	}
	m := NewMonitor(time.Now, "")
	unit := testUnit(dir)
	unit.HeadSHA = "main-ok"
	heads := []HeadInput{{Info: gh("acme", "app"), Branch: "feature", SHA: "feature-head", Token: "tok"}}

	baseline := m.CheckPass(context.Background(), actions, true, []UnitInput{unit}, heads)
	if len(baseline.Events) != 0 {
		t.Fatalf("baseline produced transition events: %+v", baseline.Events)
	}

	actions.runsByBranch["feature"] = []github.WorkflowRun{{ID: 8, WorkflowID: 1, Status: "completed", Conclusion: "failure", HeadSHA: "feature-head"}}
	featureFailure := m.CheckPass(context.Background(), actions, true, []UnitInput{unit}, heads)
	if len(featureFailure.Events) != 0 {
		t.Fatalf("non-default branch failure produced transition events: %+v", featureFailure.Events)
	}
	if got, _, ok := m.Status(gh("acme", "app"), "feature", "feature-head"); !ok || got != StatusFailure {
		t.Fatalf("feature status = (%q, %v), want (failure, true)", got, ok)
	}

	unit.HeadSHA = "main-fail"
	actions.runsByBranch["main"] = []github.WorkflowRun{{ID: 9, WorkflowID: 1, Status: "completed", Conclusion: "failure", HeadSHA: "main-fail"}}
	defaultFailure := m.CheckPass(context.Background(), actions, true, []UnitInput{unit}, heads)
	events := defaultFailure.Events[unit.Slug]
	if len(events) != 1 || events[0].Kind != TransitionEnteredFailure || events[0].RunID != 9 {
		t.Fatalf("default-branch failure events = %+v, want one entered_failure for run 9", events)
	}
}

// A scheduled workflow never runs on push, so its newest run trails the head.
// The row reports that run; the head's commit chip stays commit-scoped.
func TestCheckPass_RowFollowsNewestRunOnBranch(t *testing.T) {
	dir := t.TempDir()
	actions := &passFakeActions{
		workflows: []github.Workflow{{ID: 1, Name: "Performance", State: "active"}},
		runsByBranch: map[string][]github.WorkflowRun{
			"main": {{ID: 7, WorkflowID: 1, Status: "completed", Conclusion: "failure", HeadSHA: "h_old", HTMLURL: "https://run/7"}},
		},
	}
	m := NewMonitor(time.Now, "")
	unit := testUnit(dir)
	unit.HeadSHA = "h_new"

	res := m.CheckPass(context.Background(), actions, true, []UnitInput{unit}, nil)

	wf := res.UnitStates["r"].Workflows[0]
	if wf.RunID != 7 || wf.Conclusion != "failure" || wf.HeadSHA != "h_old" {
		t.Fatalf("row = %+v, want run 7 / failure / h_old", wf)
	}
	// First observation ever is a baseline: FromUnknown, so no launch.
	want := []TransitionEvent{{WorkflowID: 1, Kind: TransitionEnteredFailure, FromUnknown: true, RunID: 7}}
	if got := res.Events["r"]; !reflect.DeepEqual(got, want) {
		t.Fatalf("events = %+v, want %+v", got, want)
	}
	if st, _, ok := m.Status(gh("acme", "app"), "main", "h_new"); !ok || st != StatusQueued {
		t.Errorf("head chip = (%q, %v), want (queued, true)", st, ok)
	}
}

// CI runs on every push; Performance runs on a cron. Each row tracks its own
// workflow, and the unit roll-up keeps Performance's failure visible while CI
// is mid-run on the newest push.
func TestCheckPass_IndependentWorkflowsOnMain(t *testing.T) {
	dir := t.TempDir()
	actions := &passFakeActions{
		workflows: []github.Workflow{
			{ID: 1, Name: "CI", State: "active"},
			{ID: 2, Name: "Performance", State: "active"},
		},
		runsByBranch: map[string][]github.WorkflowRun{
			"main": {
				{ID: 30, WorkflowID: 1, Status: "in_progress", HeadSHA: "h2", HTMLURL: "https://run/30"},
				{ID: 21, WorkflowID: 1, Status: "completed", Conclusion: "success", HeadSHA: "h1"},
				{ID: 20, WorkflowID: 2, Status: "completed", Conclusion: "failure", HeadSHA: "h1"},
			},
		},
	}
	m := NewMonitor(time.Now, "")
	unit := testUnit(dir)
	unit.HeadSHA = "h2"

	res := m.CheckPass(context.Background(), actions, true, []UnitInput{unit}, nil)

	rows := res.UnitStates["r"].Workflows
	if rows[0].RunID != 30 || rows[0].Status != "in_progress" || rows[0].HeadSHA != "h2" {
		t.Errorf("CI row = %+v, want run 30 in_progress on h2", rows[0])
	}
	if rows[1].RunID != 20 || rows[1].Conclusion != "failure" || rows[1].HeadSHA != "h1" {
		t.Errorf("Performance row = %+v, want run 20 failure on h1", rows[1])
	}
	if got, ok := AggregateWorkflows(rows); !ok || got != StatusFailure {
		t.Errorf("unit roll-up = (%q, %v), want (failure, true)", got, ok)
	}
	if got, _, ok := m.Status(gh("acme", "app"), "main", "h2"); !ok || got != StatusInProgress {
		t.Errorf("head chip = (%q, %v), want (in_progress, true)", got, ok)
	}
}

// The bach-godot defect: a scheduled failure must stay on its row after an
// unrelated push moves the head, without a second transition.
func TestCheckPass_ScheduledFailureSurvivesHeadMove(t *testing.T) {
	dir := t.TempDir()
	actions := &passFakeActions{
		workflows: []github.Workflow{{ID: 1, Name: "Performance", State: "active"}},
		runsByBranch: map[string][]github.WorkflowRun{
			"main": {{ID: 10, WorkflowID: 1, Status: "completed", Conclusion: "success", HeadSHA: "h0"}},
		},
	}
	m := NewMonitor(time.Now, "")
	unit := testUnit(dir)
	unit.HeadSHA = "h0"
	_ = m.CheckPass(context.Background(), actions, true, []UnitInput{unit}, nil) // healthy baseline

	// The cron fires on h1 and fails.
	actions.runsByBranch["main"] = append(
		[]github.WorkflowRun{{ID: 20, WorkflowID: 1, Status: "completed", Conclusion: "failure", HeadSHA: "h1"}},
		actions.runsByBranch["main"]...)
	unit.HeadSHA = "h1"
	res := m.CheckPass(context.Background(), actions, true, []UnitInput{unit}, nil)
	want := []TransitionEvent{{WorkflowID: 1, Kind: TransitionEnteredFailure, RunID: 20}}
	if got := res.Events["r"]; !reflect.DeepEqual(got, want) {
		t.Fatalf("failure pass events = %+v, want %+v", got, want)
	}

	// An unrelated push moves the head; the workflow has not run again.
	unit.HeadSHA = "h2"
	res = m.CheckPass(context.Background(), actions, true, []UnitInput{unit}, nil)
	wf := res.UnitStates["r"].Workflows[0]
	if wf.RunID != 20 || wf.Conclusion != "failure" || wf.FirstFailureRunID != 20 {
		t.Fatalf("row after head move = %+v, want run 20 failure, FirstFailureRunID 20", wf)
	}
	if len(res.Events["r"]) != 0 {
		t.Errorf("head move produced events: %+v", res.Events["r"])
	}
	if res.Changed {
		t.Errorf("head move with no new run reported Changed=true")
	}
}

// GitHub can leave a run queued after a job starts. A scheduled run on an
// older commit must still advance its row to in_progress.
func TestCheckPass_UnitQueuedRunAdvancesRegardlessOfHead(t *testing.T) {
	dir := t.TempDir()
	actions := &passFakeActions{
		workflows: []github.Workflow{{ID: 1, Name: "Performance", State: "active"}},
		runsByBranch: map[string][]github.WorkflowRun{
			"main": {{ID: 40, WorkflowID: 1, Status: "queued", HeadSHA: "h_old"}},
		},
		jobs: map[int64][]github.WorkflowJob{
			40: {{ID: 400, Name: "puzzle-scene", Status: "in_progress"}},
		},
	}
	m := NewMonitor(time.Now, "")
	unit := testUnit(dir)
	unit.HeadSHA = "h_new"

	res := m.CheckPass(context.Background(), actions, true, []UnitInput{unit}, nil)

	if wf := res.UnitStates["r"].Workflows[0]; wf.RunID != 40 || wf.Status != "in_progress" {
		t.Fatalf("row = %+v, want run 40 in_progress", wf)
	}
}

// Rows come from one request per workflow for its newest run, not from the
// repo-wide branch listing: that listing mixes every workflow into one
// 100-run window and, served stale or incomplete, broke every row at once.
func TestCheckPass_UnitRowsComeFromPerWorkflowFetch(t *testing.T) {
	dir := t.TempDir()
	actions := &passFakeActions{
		workflows: []github.Workflow{
			{ID: 1, Name: "CI", State: "active"},
			{ID: 2, Name: "Performance", State: "active"},
		},
		runsByBranch: map[string][]github.WorkflowRun{
			"main": {
				{ID: 30, WorkflowID: 1, Status: "completed", Conclusion: "success", HeadSHA: "h1"},
				{ID: 20, WorkflowID: 2, Status: "completed", Conclusion: "failure", HeadSHA: "h0"},
			},
		},
	}
	m := NewMonitor(time.Now, "")

	res := m.CheckPass(context.Background(), actions, true, []UnitInput{testUnit(dir)}, nil)

	if got := actions.listRunsCalls.Load(); got != 0 {
		t.Errorf("ListRepoRuns calls = %d, want 0", got)
	}
	if got := actions.latestRunCalls.Load(); got != 2 {
		t.Errorf("LatestWorkflowRun calls = %d, want 2 (one per workflow)", got)
	}
	rows := res.UnitStates["r"].Workflows
	if rows[0].RunID != 30 || rows[1].RunID != 20 {
		t.Errorf("rows = %+v, want runs 30 and 20", rows)
	}
	if got, _, ok := m.Status(gh("acme", "app"), "main", "h1"); !ok || got != StatusSuccess {
		t.Errorf("head chip = (%q, %v), want (success, true)", got, ok)
	}
}

// A response with no run for a workflow is no newer evidence than the run the
// row records: the row keeps it (Performance showed "No runs" otherwise).
func TestCheckPass_MissingRunKeepsRecordedRun(t *testing.T) {
	dir := t.TempDir()
	actions := &passFakeActions{
		workflows: []github.Workflow{{ID: 2, Name: "Performance", State: "active"}},
		runsByBranch: map[string][]github.WorkflowRun{
			"main": {{ID: 10, WorkflowID: 2, Status: "completed", Conclusion: "success", HeadSHA: "h0", CreatedAt: "2026-10-04T17:06:40Z"}},
		},
	}
	m := NewMonitor(time.Now, "")
	_ = m.CheckPass(context.Background(), actions, true, []UnitInput{testUnit(dir)}, nil) // healthy baseline
	actions.runsByBranch["main"] = []github.WorkflowRun{
		{ID: 20, WorkflowID: 2, Status: "completed", Conclusion: "failure", HeadSHA: "h1", CreatedAt: "2026-10-06T18:39:36Z"},
	}
	_ = m.CheckPass(context.Background(), actions, true, []UnitInput{testUnit(dir)}, nil) // enters failure

	actions.runsByBranch["main"] = nil
	res := m.CheckPass(context.Background(), actions, true, []UnitInput{testUnit(dir)}, nil)

	wf := res.UnitStates["r"].Workflows[0]
	if wf.RunID != 20 || wf.Conclusion != "failure" || wf.FirstFailureRunID != 20 {
		t.Fatalf("row = %+v, want recorded run 20 / failure, episode 20", wf)
	}
	if len(res.Events["r"]) != 0 {
		t.Errorf("missing run produced events: %+v", res.Events["r"])
	}
	wantStale := []StaleRun{{Slug: "r", WorkflowID: 2, Workflow: "Performance", KeptRunID: 20, KeptCreatedAt: "2026-10-06T18:39:36Z"}}
	if !reflect.DeepEqual(res.StaleRuns, wantStale) {
		t.Errorf("StaleRuns = %+v, want %+v", res.StaleRuns, wantStale)
	}
}

// Review Focus 1: state written by the head-scoped code left a row empty. If
// that workflow's newest branch run is a failure with no episode, the fix
// surfaces it as a real transition exactly once.
func TestCheckPass_UpgradeRevealsHiddenFailureOnce(t *testing.T) {
	dir := t.TempDir()
	prev := &UnitState{
		RepoName: "acme/app", Repo: "acme/app", Branch: "main", HeadSHA: "h2",
		Workflows: []WorkflowState{{Name: "CI image", WorkflowID: 1}},
		CheckedAt: "2026-10-06T00:00:00Z",
	}
	if err := WriteState(dir+"/state.json", prev); err != nil {
		t.Fatal(err)
	}
	actions := &passFakeActions{
		workflows: []github.Workflow{{ID: 1, Name: "CI image", State: "active"}},
		runsByBranch: map[string][]github.WorkflowRun{
			"main": {{ID: 60, WorkflowID: 1, Status: "completed", Conclusion: "failure", HeadSHA: "h1"}},
		},
	}
	m := NewMonitor(time.Now, "")
	unit := testUnit(dir)
	unit.HeadSHA = "h2"

	first := m.CheckPass(context.Background(), actions, true, []UnitInput{unit}, nil)
	want := []TransitionEvent{{WorkflowID: 1, Kind: TransitionEnteredFailure, RunID: 60}}
	if got := first.Events["r"]; !reflect.DeepEqual(got, want) {
		t.Fatalf("first pass events = %+v, want %+v", got, want)
	}
	second := m.CheckPass(context.Background(), actions, true, []UnitInput{unit}, nil)
	if len(second.Events["r"]) != 0 {
		t.Errorf("second pass re-emitted events: %+v", second.Events["r"])
	}
}

func TestCheckPass_FailingRunCollectsFailedJobs(t *testing.T) {
	dir := t.TempDir()
	actions := &passFakeActions{
		workflows: []github.Workflow{{ID: 1, Name: "CI", Path: ".github/workflows/ci.yml", State: "active"}},
		runsByBranch: map[string][]github.WorkflowRun{
			"main": {{ID: 7, WorkflowID: 1, Status: "completed", Conclusion: "failure", HeadSHA: "h1", HTMLURL: "u"}},
		},
		jobs: map[int64][]github.WorkflowJob{
			7: {
				{ID: 99, Name: "test", Conclusion: "failure", HTMLURL: "j"},
				{ID: 100, Name: "build", Conclusion: "success"},
			},
		},
	}
	m := NewMonitor(time.Now, "")
	res := m.CheckPass(context.Background(), actions, true, []UnitInput{testUnit(dir)}, nil)
	wf := res.UnitStates["r"].Workflows[0]
	if wf.Conclusion != "failure" || len(wf.FailedJobs) != 1 || wf.FailedJobs[0].Name != "test" {
		t.Fatalf("wf=%+v", wf)
	}
	if wf.FailedJobs[0].ID != 99 {
		t.Errorf("FailedJobs[0].ID = %d, want 99", wf.FailedJobs[0].ID)
	}
}

func TestCheckPass_TransitionsRecovered(t *testing.T) {
	dir := t.TempDir()
	prev := &UnitState{
		RepoName: "acme/app", Repo: "acme/app", Branch: "main",
		Workflows: []WorkflowState{{Name: "CI", Path: ".github/workflows/ci.yml", WorkflowID: 1, RunID: 7, Conclusion: "failure", HeadSHA: "h1", FirstFailureRunID: 7}},
		CheckedAt: "2026-01-01T00:00:00Z",
	}
	if err := WriteState(dir+"/state.json", prev); err != nil {
		t.Fatal(err)
	}

	actions := &passFakeActions{
		workflows: []github.Workflow{{ID: 1, Name: "CI", State: "active"}},
		runsByBranch: map[string][]github.WorkflowRun{
			"main": {{ID: 8, WorkflowID: 1, Status: "completed", Conclusion: "success", HeadSHA: "h1"}},
		},
	}
	m := NewMonitor(time.Now, "")
	res := m.CheckPass(context.Background(), actions, true, []UnitInput{testUnit(dir)}, nil)
	events, ok := res.Events["r"]
	if !ok {
		t.Fatal("expected events for r")
	}
	found := false
	for _, e := range events {
		if e.Kind == TransitionRecovered {
			found = true
		}
	}
	if !found {
		t.Errorf("expected a recovered event, got %+v", events)
	}
}

func TestCheckPass_RateLimitKeepsPriorCommit(t *testing.T) {
	dir := t.TempDir()
	m := NewMonitor(time.Now, "")
	m.setEnabled()
	// Pre-seed a terminal commit; the pass's inputs keep it referenced.
	m.recordCommit(gh("acme", "app"), "main", "h1", StatusSuccess, "u", true)

	actions := &passFakeActions{
		workflows:    []github.Workflow{{ID: 1, Name: "CI", State: "active"}},
		runsByBranch: map[string][]github.WorkflowRun{},
		rateLimit:    true,
	}
	heads := []HeadInput{{Info: gh("acme", "app"), Branch: "main", SHA: "h1", Token: "tok"}}
	_ = m.CheckPass(context.Background(), actions, true, []UnitInput{testUnit(dir)}, heads)
	// Repo has a rate-limit error now → status is absent per spec edge case.
	if _, _, ok := m.Status(gh("acme", "app"), "main", "h1"); ok {
		t.Errorf("status should be absent on rate-limited repo")
	}
	// Commit should still be in the store (still referenced by the inputs).
	m.mu.Lock()
	_, present := m.commits[commitKey{owner: "acme", repo: "app", branch: "main", sha: "h1"}]
	m.mu.Unlock()
	if !present {
		t.Error("commit should be retained when rate-limited")
	}
}

func TestCheckPass_RateLimitBackoffSkipsFetches(t *testing.T) {
	dir := t.TempDir()
	now := time.Now()
	m := NewMonitor(func() time.Time { return now }, "")
	actions := &passFakeActions{
		workflows:    []github.Workflow{{ID: 1, Name: "CI", State: "active"}},
		runsByBranch: map[string][]github.WorkflowRun{"main": {}},
		rateLimit:    true,
	}
	unit := testUnit(dir)

	// Pass 1: ListWorkflows ok, LatestWorkflowRun rate-limited → backoff starts.
	_ = m.CheckPass(context.Background(), actions, true, []UnitInput{unit}, nil)
	if got := actions.listWorkflowsCall.Load(); got != 1 {
		t.Fatalf("ListWorkflows calls = %d, want 1", got)
	}

	// Pass 2 (inside backoff window): the unit is skipped entirely.
	_ = m.CheckPass(context.Background(), actions, true, []UnitInput{unit}, nil)
	if got := actions.listWorkflowsCall.Load(); got != 1 {
		t.Errorf("ListWorkflows calls = %d, want 1 (backoff skips fetch)", got)
	}

	// After the backoff window and with the limit lifted, fetches resume.
	now = now.Add(maxRateLimitBackoff + time.Minute)
	actions.rateLimit = false
	_ = m.CheckPass(context.Background(), actions, true, []UnitInput{unit}, nil)
	if got := actions.listWorkflowsCall.Load(); got != 2 {
		t.Errorf("ListWorkflows calls = %d, want 2 after backoff", got)
	}
	if _, _, ok := m.Status(gh("acme", "app"), "main", "h1"); !ok {
		t.Error("status should derive again after backoff clears")
	}
}

func TestCheckPass_TerminalResultTTLSkipsRefetch(t *testing.T) {
	dir := t.TempDir()
	now := time.Now()
	m := NewMonitor(func() time.Time { return now }, "")
	actions := &passFakeActions{
		workflows: []github.Workflow{{ID: 1, Name: "CI", State: "active"}},
		runsByBranch: map[string][]github.WorkflowRun{
			"main":    {{ID: 7, WorkflowID: 1, Status: "completed", Conclusion: "success", HeadSHA: "h1"}},
			"feature": {{ID: 8, WorkflowID: 1, Status: "completed", Conclusion: "success", HeadSHA: "f1"}},
		},
	}
	heads := []HeadInput{{Info: gh("acme", "app"), Branch: "feature", SHA: "f1", Token: "tok"}}

	// Pass 1: the head (feature) is listed once; the unit fetches per workflow.
	_ = m.CheckPass(context.Background(), actions, true, []UnitInput{testUnit(dir)}, heads)
	if got := actions.listRunsCalls.Load(); got != 1 {
		t.Fatalf("ListRepoRuns calls = %d, want 1", got)
	}

	// New runs would say failure, but the terminal result is fresh within the
	// TTL → the head's fetch is skipped and the recorded status kept.
	actions.runsByBranch["feature"] = []github.WorkflowRun{{ID: 9, WorkflowID: 1, Status: "completed", Conclusion: "failure", HeadSHA: "f1"}}
	_ = m.CheckPass(context.Background(), actions, true, []UnitInput{testUnit(dir)}, heads)
	if got := actions.listRunsCalls.Load(); got != 1 {
		t.Errorf("ListRepoRuns calls = %d, want 1 (feature skipped within TTL)", got)
	}
	if st, _, _ := m.Status(gh("acme", "app"), "feature", "f1"); st != StatusSuccess {
		t.Errorf("status = %q, want success (fresh terminal kept)", st)
	}

	// Past the TTL the head is refetched and the status updates.
	now = now.Add(terminalResultTTL + time.Minute)
	_ = m.CheckPass(context.Background(), actions, true, []UnitInput{testUnit(dir)}, heads)
	if got := actions.listRunsCalls.Load(); got != 2 {
		t.Errorf("ListRepoRuns calls = %d, want 2 after TTL", got)
	}
	if st, _, _ := m.Status(gh("acme", "app"), "feature", "f1"); st != StatusFailure {
		t.Errorf("status = %q, want failure after TTL refetch", st)
	}
}

func TestCheckPass_DisabledTransition(t *testing.T) {
	dir := t.TempDir()
	actions := &passFakeActions{
		workflows: []github.Workflow{{ID: 1, Name: "CI", State: "active"}},
		runsByBranch: map[string][]github.WorkflowRun{
			"main": {{ID: 7, WorkflowID: 1, Status: "completed", Conclusion: "success", HeadSHA: "h1"}},
		},
	}
	m := NewMonitor(time.Now, "")
	_ = m.CheckPass(context.Background(), actions, true, []UnitInput{testUnit(dir)}, nil)
	if _, _, ok := m.Status(gh("acme", "app"), "main", "h1"); !ok {
		t.Fatal("status should derive while enabled")
	}

	// Disabled pass: fetched knowledge dropped, Changed reported once.
	res := m.CheckPass(context.Background(), actions, false, nil, nil)
	if !res.Changed {
		t.Error("disabled transition should report Changed when data was held")
	}
	if _, _, ok := m.Status(gh("acme", "app"), "main", "h1"); ok {
		t.Error("status should be absent while disabled")
	}

	res = m.CheckPass(context.Background(), actions, false, nil, nil)
	if res.Changed {
		t.Error("repeated disabled pass should report no change")
	}

	// Re-enable: fetches resume.
	_ = m.CheckPass(context.Background(), actions, true, []UnitInput{testUnit(dir)}, nil)
	if _, _, ok := m.Status(gh("acme", "app"), "main", "h1"); !ok {
		t.Error("status should derive after re-enable")
	}
}

func TestCheckPass_UnauthorizedSetsRepoMeta(t *testing.T) {
	dir := t.TempDir()
	actions := &passFakeActions{
		workflows: []github.Workflow{},
		err:       errors.New("wrapped: " + github.ErrUnauthorized.Error()),
	}
	m := NewMonitor(time.Now, "")
	_ = m.CheckPass(context.Background(), actions, true, []UnitInput{testUnit(dir)}, nil)
	// Unauthorized → repo meta has an error → status absent.
	if _, _, ok := m.Status(gh("acme", "app"), "main", "h1"); ok {
		t.Error("expected status absent on unauthorized repo")
	}
}

func TestCheckPass_PruneKeepsInputReferencedCommits(t *testing.T) {
	dir := t.TempDir()
	m := NewMonitor(time.Now, "")
	m.setEnabled()
	m.recordCommit(gh("acme", "app"), "main", "h1", StatusSuccess, "u", true)

	actions := &passFakeActions{
		workflows: []github.Workflow{{ID: 1, Name: "CI", State: "active"}},
		runsByBranch: map[string][]github.WorkflowRun{
			"main": {{ID: 8, WorkflowID: 1, Status: "completed", Conclusion: "success", HeadSHA: "h1"}},
		},
	}
	res := m.CheckPass(context.Background(), actions, true, []UnitInput{testUnit(dir)}, nil)
	if res.UnitStates["r"] == nil {
		t.Fatal("missing unit state")
	}
	m.mu.Lock()
	_, present := m.commits[commitKey{owner: "acme", repo: "app", branch: "main", sha: "h1"}]
	m.mu.Unlock()
	if !present {
		t.Error("commit should remain when unit-referenced")
	}
}

func TestCheckPass_PruneDropsUnreferencedCommits(t *testing.T) {
	dir := t.TempDir()
	m := NewMonitor(time.Now, "")
	m.setEnabled()
	m.recordCommit(gh("acme", "app"), "main", "stale", StatusSuccess, "u", true)

	actions := &passFakeActions{
		workflows: []github.Workflow{{ID: 1, Name: "CI", State: "active"}},
		runsByBranch: map[string][]github.WorkflowRun{
			"main": {{ID: 8, WorkflowID: 1, Status: "completed", Conclusion: "success", HeadSHA: "h1"}},
		},
	}
	_ = m.CheckPass(context.Background(), actions, true, []UnitInput{testUnit(dir)}, nil)
	m.mu.Lock()
	_, present := m.commits[commitKey{owner: "acme", repo: "app", branch: "main", sha: "stale"}]
	m.mu.Unlock()
	if present {
		t.Error("commit not referenced by any input should be pruned")
	}
}

func TestCheckPass_EmptyHeadSHAKeepsPriorRows(t *testing.T) {
	dir := t.TempDir()
	prev := &UnitState{
		RepoName: "acme/app", Repo: "acme/app", Branch: "main",
		Workflows: []WorkflowState{{Name: "CI", Path: ".github/workflows/ci.yml", WorkflowID: 1, RunID: 7, Conclusion: "success", HeadSHA: "h1"}},
	}
	if err := WriteState(dir+"/state.json", prev); err != nil {
		t.Fatal(err)
	}
	actions := &passFakeActions{
		workflows:    []github.Workflow{{ID: 1, Name: "CI", State: "active"}},
		runsByBranch: map[string][]github.WorkflowRun{},
	}
	m := NewMonitor(time.Now, "")
	unit := testUnit(dir)
	unit.HeadSHA = ""
	res := m.CheckPass(context.Background(), actions, true, []UnitInput{unit}, nil)
	if res.UnitStates["r"].Workflows[0].RunID != 7 {
		t.Errorf("expected prior rows preserved, got %+v", res.UnitStates["r"].Workflows)
	}
}

func TestCheckPass_CorruptStateSurfacesError(t *testing.T) {
	dir := t.TempDir()
	if err := WriteState(dir+"/state.json", &UnitState{RepoName: "x"}); err != nil {
		t.Fatal(err)
	}
	// Corrupt the file.
	if err := os.WriteFile(dir+"/state.json", []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	actions := &passFakeActions{
		workflows: []github.Workflow{{ID: 1, Name: "CI", State: "active"}},
		runsByBranch: map[string][]github.WorkflowRun{
			"main": {{ID: 7, WorkflowID: 1, Status: "completed", Conclusion: "success", HeadSHA: "h1"}},
		},
	}
	m := NewMonitor(time.Now, "")
	res := m.CheckPass(context.Background(), actions, true, []UnitInput{testUnit(dir)}, nil)
	st := res.UnitStates["r"]
	if st == nil {
		t.Fatal("missing unit state")
	}
	if st.LastError == "" {
		t.Error("corrupt prior state should surface as LastError, not be swallowed")
	}
}

// TestCheckPass_CommitStoreSurvivesRestart is the CI-chip restart regression:
// a watched head's recorded status (a feature-branch workspace, not covered by
// any unit snapshot) must survive a daemon restart. A fresh monitor hydrated
// from durable state reports what the previous process knew — not queued.
func TestCheckPass_CommitStoreSurvivesRestart(t *testing.T) {
	dir := t.TempDir()
	storePath := dir + "/commits.json"
	actions := &passFakeActions{
		workflows: []github.Workflow{{ID: 1, Name: "CI", State: "active"}},
		runsByBranch: map[string][]github.WorkflowRun{
			"main":    {{ID: 7, WorkflowID: 1, Status: "completed", Conclusion: "success", HeadSHA: "h1"}},
			"feature": {{ID: 8, WorkflowID: 1, Status: "completed", Conclusion: "success", HeadSHA: "f1", HTMLURL: "https://run8"}},
		},
	}
	m1 := NewMonitor(time.Now, storePath)
	heads := []HeadInput{{Info: gh("acme", "app"), Branch: "feature", SHA: "f1", Token: "tok"}}
	_ = m1.CheckPass(context.Background(), actions, true, []UnitInput{testUnit(dir)}, heads)
	if st, _, ok := m1.Status(gh("acme", "app"), "feature", "f1"); !ok || st != StatusSuccess {
		t.Fatalf("pre-restart status = (%q, %v), want (success, true)", st, ok)
	}

	// "Restart": a fresh monitor hydrated from the durable state on disk.
	m2 := NewMonitor(time.Now, storePath)
	prev, err := ReadState(dir + "/state.json")
	if err != nil {
		t.Fatal(err)
	}
	m2.Hydrate(map[string]*UnitState{"r": prev})
	st, url, ok := m2.Status(gh("acme", "app"), "feature", "f1")
	if !ok || st != StatusSuccess || url != "https://run8" {
		t.Fatalf("post-restart status = (%q, %q, %v), want (success, https://run8, true)", st, url, ok)
	}
}

// GitHub's branch-filtered runs listing intermittently returns an old
// snapshot, or omits a workflow's latest run. A row must never move to a run
// created before the one it records: that turned months-old failures into
// "new" failures and launched remediation agents for them.
func TestCheckPass_RowNeverMovesToAnOlderRun(t *testing.T) {
	dir := t.TempDir()
	actions := &passFakeActions{
		workflows: []github.Workflow{{ID: 1, Name: "CI", State: "active"}},
		runsByBranch: map[string][]github.WorkflowRun{
			"main": {{ID: 20, WorkflowID: 1, Status: "completed", Conclusion: "success", HeadSHA: "h2", CreatedAt: "2026-10-07T05:00:00Z"}},
		},
	}
	m := NewMonitor(time.Now, "")
	unit := testUnit(dir)
	unit.HeadSHA = "h2"
	_ = m.CheckPass(context.Background(), actions, true, []UnitInput{unit}, nil)

	// Bad response: an old snapshot whose newest CI run is a September failure.
	actions.runsByBranch["main"] = []github.WorkflowRun{
		{ID: 10, WorkflowID: 1, Status: "completed", Conclusion: "failure", HeadSHA: "h1", CreatedAt: "2026-09-16T14:06:06Z"},
	}
	res := m.CheckPass(context.Background(), actions, true, []UnitInput{unit}, nil)
	if wf := res.UnitStates["r"].Workflows[0]; wf.RunID != 20 || wf.Conclusion != "success" {
		t.Fatalf("row after stale response = %+v, want the recorded run 20 / success", wf)
	}
	if len(res.Events["r"]) != 0 {
		t.Fatalf("stale response produced transition events: %+v", res.Events["r"])
	}
	wantStale := []StaleRun{{Slug: "r", WorkflowID: 1, Workflow: "CI", FetchedRunID: 10, FetchedCreatedAt: "2026-09-16T14:06:06Z", KeptRunID: 20, KeptCreatedAt: "2026-10-07T05:00:00Z"}}
	if !reflect.DeepEqual(res.StaleRuns, wantStale) {
		t.Errorf("StaleRuns = %+v, want %+v", res.StaleRuns, wantStale)
	}

	// A newer run is accepted.
	actions.runsByBranch["main"] = []github.WorkflowRun{
		{ID: 30, WorkflowID: 1, Status: "completed", Conclusion: "failure", HeadSHA: "h3", CreatedAt: "2026-10-08T09:00:00Z"},
	}
	unit.HeadSHA = "h3"
	res = m.CheckPass(context.Background(), actions, true, []UnitInput{unit}, nil)
	if wf := res.UnitStates["r"].Workflows[0]; wf.RunID != 30 || wf.Conclusion != "failure" {
		t.Fatalf("row after newer run = %+v, want run 30 / failure", wf)
	}
	if len(res.StaleRuns) != 0 {
		t.Errorf("newer run reported as stale: %+v", res.StaleRuns)
	}
}

// The same run seen again (same creation time) still updates the row: a
// queued or running run must be able to complete.
func TestCheckPass_SameRunAdvancesStatus(t *testing.T) {
	dir := t.TempDir()
	actions := &passFakeActions{
		workflows: []github.Workflow{{ID: 1, Name: "CI", State: "active"}},
		runsByBranch: map[string][]github.WorkflowRun{
			"main": {{ID: 20, WorkflowID: 1, Status: "in_progress", HeadSHA: "h1", CreatedAt: "2026-10-07T05:00:00Z"}},
		},
	}
	m := NewMonitor(time.Now, "")
	_ = m.CheckPass(context.Background(), actions, true, []UnitInput{testUnit(dir)}, nil)

	actions.runsByBranch["main"] = []github.WorkflowRun{
		{ID: 20, WorkflowID: 1, Status: "completed", Conclusion: "success", HeadSHA: "h1", CreatedAt: "2026-10-07T05:00:00Z"},
	}
	res := m.CheckPass(context.Background(), actions, true, []UnitInput{testUnit(dir)}, nil)
	if wf := res.UnitStates["r"].Workflows[0]; wf.Status != "completed" || wf.Conclusion != "success" {
		t.Fatalf("row = %+v, want run 20 completed / success", wf)
	}
}

func TestCheckPass_FailingRowLinksOnlyExistingSession(t *testing.T) {
	cases := []struct {
		name        string
		sessionIDs  map[string]bool
		wantSession string
		wantChanged bool
	}{
		{name: "session exists", sessionIDs: map[string]bool{"s1": true}, wantSession: "s1", wantChanged: false},
		{name: "session disposed", sessionIDs: map[string]bool{"other": true}, wantSession: "", wantChanged: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			actions := &passFakeActions{
				workflows: []github.Workflow{{ID: 1, Name: "Performance", State: "active"}},
				runsByBranch: map[string][]github.WorkflowRun{
					"main": {{ID: 20, WorkflowID: 1, Status: "completed", Conclusion: "failure", HeadSHA: "h1", CreatedAt: "2026-10-07T05:00:00Z"}},
				},
			}
			m := NewMonitor(time.Now, "")
			unit := testUnit(dir)
			_ = m.CheckPass(context.Background(), actions, true, []UnitInput{unit}, nil)

			st, err := ReadState(unit.StatePath)
			if err != nil || st == nil {
				t.Fatalf("ReadState = %v, %v", st, err)
			}
			RecordManualLaunch(st, st.Workflows[0], "ws1", "s1", "2026-10-07T06:00:00Z")
			if err := WriteState(unit.StatePath, st); err != nil {
				t.Fatal(err)
			}

			unit.SessionIDs = tc.sessionIDs
			res := m.CheckPass(context.Background(), actions, true, []UnitInput{unit}, nil)

			if got := res.UnitStates["r"].Workflows[0].SessionID; got != tc.wantSession {
				t.Errorf("returned row session = %q, want %q", got, tc.wantSession)
			}
			persisted, err := ReadState(unit.StatePath)
			if err != nil || persisted == nil {
				t.Fatalf("ReadState = %v, %v", persisted, err)
			}
			if got := persisted.Workflows[0].SessionID; got != tc.wantSession {
				t.Errorf("persisted row session = %q, want %q", got, tc.wantSession)
			}
			if res.Changed != tc.wantChanged {
				t.Errorf("Changed = %v, want %v", res.Changed, tc.wantChanged)
			}
		})
	}
}
