//go:build !nobuildmonitor

package buildmonitor

import (
	"context"
	"errors"
	"os"
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
	st, _, ok := m.Status(gh("acme", "app"), "h1")
	if !ok || st != StatusQueued {
		t.Errorf("status = (%q, %v), want (queued, true)", st, ok)
	}
	if st := res.UnitStates["r"]; st == nil {
		t.Fatalf("missing unit state in result")
	} else if len(st.Workflows) != 1 || st.Workflows[0].RunID != 0 {
		t.Errorf("workflow row = %+v, want empty-run row", st.Workflows)
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
	// One call for the unit's default branch (main) and one for "feature".
	if got := actions.listRunsCalls.Load(); got != 2 {
		t.Errorf("ListRepoRuns calls = %d, want 2", got)
	}
}

func TestCheckPass_HeadOnUnitBranchReusesUnitFetch(t *testing.T) {
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
	if got := actions.listRunsCalls.Load(); got != 1 {
		t.Errorf("ListRepoRuns calls = %d, want 1 (unit fetch reused)", got)
	}
	if st, _, ok := m.Status(gh("acme", "app"), "h1"); !ok || st != StatusSuccess {
		t.Errorf("status = (%q, %v), want (success, true)", st, ok)
	}
}

func TestCheckPass_HeadMovedMidFlightRowsHaveNoRun(t *testing.T) {
	dir := t.TempDir()
	// Unit head = "h_new"; runs only describe "h_old".
	actions := &passFakeActions{
		workflows: []github.Workflow{{ID: 1, Name: "CI", State: "active"}},
		runsByBranch: map[string][]github.WorkflowRun{
			"main": {{ID: 7, WorkflowID: 1, Status: "completed", Conclusion: "failure", HeadSHA: "h_old"}},
		},
	}
	m := NewMonitor(time.Now, "")
	unit := testUnit(dir)
	unit.HeadSHA = "h_new"
	res := m.CheckPass(context.Background(), actions, true, []UnitInput{unit}, nil)
	if res.UnitStates["r"].Workflows[0].RunID != 0 {
		t.Errorf("expected empty-run row when head moved, got %+v", res.UnitStates["r"].Workflows[0])
	}
	st, _, ok := m.Status(gh("acme", "app"), "h_new")
	if !ok || st != StatusQueued {
		t.Errorf("status = (%q, %v), want (queued, true)", st, ok)
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
	m.recordCommit(gh("acme", "app"), "h1", StatusSuccess, "u", true)

	actions := &passFakeActions{
		workflows:    []github.Workflow{{ID: 1, Name: "CI", State: "active"}},
		runsByBranch: map[string][]github.WorkflowRun{},
		rateLimit:    true,
	}
	heads := []HeadInput{{Info: gh("acme", "app"), Branch: "main", SHA: "h1", Token: "tok"}}
	_ = m.CheckPass(context.Background(), actions, true, []UnitInput{testUnit(dir)}, heads)
	// Repo has a rate-limit error now → status is absent per spec edge case.
	if _, _, ok := m.Status(gh("acme", "app"), "h1"); ok {
		t.Errorf("status should be absent on rate-limited repo")
	}
	// Commit should still be in the store (still referenced by the inputs).
	m.mu.Lock()
	_, present := m.commits[commitKey{owner: "acme", repo: "app", sha: "h1"}]
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

	// Pass 1: ListWorkflows ok, ListRepoRuns rate-limited → backoff starts.
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
	if _, _, ok := m.Status(gh("acme", "app"), "h1"); !ok {
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

	// Pass 1: unit (main) + head (feature) = 2 fetches.
	_ = m.CheckPass(context.Background(), actions, true, []UnitInput{testUnit(dir)}, heads)
	if got := actions.listRunsCalls.Load(); got != 2 {
		t.Fatalf("ListRepoRuns calls = %d, want 2", got)
	}

	// New runs would say failure, but the terminal result is fresh within the
	// TTL → the head's fetch is skipped and the recorded status kept.
	actions.runsByBranch["feature"] = []github.WorkflowRun{{ID: 9, WorkflowID: 1, Status: "completed", Conclusion: "failure", HeadSHA: "f1"}}
	_ = m.CheckPass(context.Background(), actions, true, []UnitInput{testUnit(dir)}, heads)
	if got := actions.listRunsCalls.Load(); got != 3 {
		t.Errorf("ListRepoRuns calls = %d, want 3 (feature skipped within TTL)", got)
	}
	if st, _, _ := m.Status(gh("acme", "app"), "f1"); st != StatusSuccess {
		t.Errorf("status = %q, want success (fresh terminal kept)", st)
	}

	// Past the TTL the head is refetched and the status updates.
	now = now.Add(terminalResultTTL + time.Minute)
	_ = m.CheckPass(context.Background(), actions, true, []UnitInput{testUnit(dir)}, heads)
	if got := actions.listRunsCalls.Load(); got != 5 {
		t.Errorf("ListRepoRuns calls = %d, want 5 after TTL", got)
	}
	if st, _, _ := m.Status(gh("acme", "app"), "f1"); st != StatusFailure {
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
	if _, _, ok := m.Status(gh("acme", "app"), "h1"); !ok {
		t.Fatal("status should derive while enabled")
	}

	// Disabled pass: fetched knowledge dropped, Changed reported once.
	res := m.CheckPass(context.Background(), actions, false, nil, nil)
	if !res.Changed {
		t.Error("disabled transition should report Changed when data was held")
	}
	if _, _, ok := m.Status(gh("acme", "app"), "h1"); ok {
		t.Error("status should be absent while disabled")
	}

	res = m.CheckPass(context.Background(), actions, false, nil, nil)
	if res.Changed {
		t.Error("repeated disabled pass should report no change")
	}

	// Re-enable: fetches resume.
	_ = m.CheckPass(context.Background(), actions, true, []UnitInput{testUnit(dir)}, nil)
	if _, _, ok := m.Status(gh("acme", "app"), "h1"); !ok {
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
	if _, _, ok := m.Status(gh("acme", "app"), "h1"); ok {
		t.Error("expected status absent on unauthorized repo")
	}
}

func TestCheckPass_PruneKeepsInputReferencedCommits(t *testing.T) {
	dir := t.TempDir()
	m := NewMonitor(time.Now, "")
	m.setEnabled()
	m.recordCommit(gh("acme", "app"), "h1", StatusSuccess, "u", true)

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
	_, present := m.commits[commitKey{owner: "acme", repo: "app", sha: "h1"}]
	m.mu.Unlock()
	if !present {
		t.Error("commit should remain when unit-referenced")
	}
}

func TestCheckPass_PruneDropsUnreferencedCommits(t *testing.T) {
	dir := t.TempDir()
	m := NewMonitor(time.Now, "")
	m.setEnabled()
	m.recordCommit(gh("acme", "app"), "stale", StatusSuccess, "u", true)

	actions := &passFakeActions{
		workflows: []github.Workflow{{ID: 1, Name: "CI", State: "active"}},
		runsByBranch: map[string][]github.WorkflowRun{
			"main": {{ID: 8, WorkflowID: 1, Status: "completed", Conclusion: "success", HeadSHA: "h1"}},
		},
	}
	_ = m.CheckPass(context.Background(), actions, true, []UnitInput{testUnit(dir)}, nil)
	m.mu.Lock()
	_, present := m.commits[commitKey{owner: "acme", repo: "app", sha: "stale"}]
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
	if st, _, ok := m1.Status(gh("acme", "app"), "f1"); !ok || st != StatusSuccess {
		t.Fatalf("pre-restart status = (%q, %v), want (success, true)", st, ok)
	}

	// "Restart": a fresh monitor hydrated from the durable state on disk.
	m2 := NewMonitor(time.Now, storePath)
	prev, err := ReadState(dir + "/state.json")
	if err != nil {
		t.Fatal(err)
	}
	m2.Hydrate(map[string]*UnitState{"r": prev})
	st, url, ok := m2.Status(gh("acme", "app"), "f1")
	if !ok || st != StatusSuccess || url != "https://run8" {
		t.Fatalf("post-restart status = (%q, %q, %v), want (success, https://run8, true)", st, url, ok)
	}
}
