//go:build !nobuildmonitor && !nogithub

package dashboard

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/log"
	"github.com/go-chi/chi/v5"
	"github.com/sergeknystautas/schmux/internal/api/contracts"
	"github.com/sergeknystautas/schmux/internal/buildmonitor"
	"github.com/sergeknystautas/schmux/internal/config"
	"github.com/sergeknystautas/schmux/internal/github"
	"github.com/sergeknystautas/schmux/internal/models"
	"github.com/sergeknystautas/schmux/internal/schmuxdir"
	"github.com/sergeknystautas/schmux/internal/session"
	"github.com/sergeknystautas/schmux/internal/state"
	"github.com/sergeknystautas/schmux/internal/workspace"
)

// buildMonitorLaunchFixture wires a server whose monitored repo "repo-a" has
// a dedicated workspace per workflow, a stubbed spawn, and a stubbed focus
// pull.
type buildMonitorLaunchFixture struct {
	server *Server
	cfg    *config.Config
	st     *state.State
	// spawns records every spawn request the build monitor made.
	spawns []SpawnRequest
	// pulled records the sessions the dashboards were pulled to.
	pulled []string
}

// newBuildMonitorLaunchFixture writes the unit state of the monitored repo,
// creates a dedicated workspace per workflow, and registers the workspaces.
func newBuildMonitorLaunchFixture(t *testing.T, unit *buildmonitor.UnitState) *buildMonitorLaunchFixture {
	t.Helper()
	schmuxdir.Set(t.TempDir())
	t.Cleanup(func() { schmuxdir.Set("") })

	if err := config.SaveGitHubIdentity("octocat", "test-token", "repo"); err != nil {
		t.Fatal(err)
	}

	configPath := filepath.Join(t.TempDir(), "config.json")
	cfg := config.CreateDefault(configPath)
	cfg.WorkspacePath = t.TempDir()
	cfg.WorktreeBasePath = t.TempDir()
	cfg.Repos = []config.Repo{{Name: "Repo A", URL: "https://github.com/o/r"}}
	cfg.BuildMonitor = &config.BuildMonitorConfig{
		Enabled: true, Target: "claude",
		Repos: map[string]config.BuildMonitorRepoConfig{"repo-a": {Enabled: true, GitHubLogin: "octocat"}},
	}
	if err := cfg.Save(); err != nil {
		t.Fatal(err)
	}

	statePath := filepath.Join(t.TempDir(), "state.json")
	stStore := state.New(statePath, nil)
	logger := log.NewWithOptions(io.Discard, log.Options{})
	wm := workspace.New(cfg, stStore, statePath, logger)
	sm := session.New(cfg, stStore, statePath, wm, nil, logger)
	shutdownCtx, shutdownCancel := context.WithCancel(context.Background())
	t.Cleanup(shutdownCancel)
	server := NewServer(cfg, stStore, statePath, sm, wm, github.NewDiscovery(nil), logger, contracts.GitHubStatus{}, nil, ServerOptions{ShutdownCtx: shutdownCtx})
	server.SetModelManager(models.New(cfg, nil, "", logger))
	t.Cleanup(func() { server.CloseForTest() })

	f := &buildMonitorLaunchFixture{server: server, cfg: cfg, st: stStore}
	server.buildMonitorSpawn = f.spawn
	server.broadcastPendingNavigation = f.navigate

	for _, wf := range unit.Workflows {
		if err := stStore.AddWorkspace(state.Workspace{
			ID:        "ws-" + buildmonitor.WorkflowSlug(wf.Name),
			Repo:      "https://github.com/o/r",
			Branch:    buildmonitor.WorkspaceBranch(wf.Name),
			Path:      t.TempDir(),
			Status:    state.WorkspaceStatusRunning,
			Dedicated: buildmonitor.WorkspaceName("repo-a", wf.Name),
		}); err != nil {
			t.Fatal(err)
		}
	}

	if err := buildmonitor.WriteState(buildMonitorUnitStatePath("repo-a"), unit); err != nil {
		t.Fatal(err)
	}
	server.syncBuildMonitorWorkspaces()
	return f
}

// spawn stands in for the real spawn: it records the request and creates the
// session the spawn would have created.
func (f *buildMonitorLaunchFixture) spawn(req SpawnRequest) ([]SessionResult, int, error) {
	f.spawns = append(f.spawns, req)
	sid := fmt.Sprintf("sess-%d", len(f.spawns))
	if err := f.st.AddSession(state.Session{
		ID: sid, WorkspaceID: req.WorkspaceID, Target: "claude", Status: state.SessionStatusRunning,
	}); err != nil {
		return nil, http.StatusInternalServerError, err
	}
	return []SessionResult{{SessionID: sid, WorkspaceID: req.WorkspaceID, Target: "claude"}}, http.StatusOK, nil
}

// navigate stands in for the focus pull.
func (f *buildMonitorLaunchFixture) navigate(navType, id1, _ string) {
	if navType == "session" {
		f.pulled = append(f.pulled, id1)
	}
}

// directive builds the launch directive for one workflow row.
func (f *buildMonitorLaunchFixture) directive(wf buildmonitor.WorkflowState) launchDirective {
	return launchDirective{
		slug: "repo-a", repoName: "Repo A",
		repo: "o/r", info: github.RepoInfo{Owner: "o", Repo: "r"},
		login: "octocat", workflow: wf,
	}
}

// unitState reads the durable unit state of the monitored repo.
func (f *buildMonitorLaunchFixture) unitState(t *testing.T) *buildmonitor.UnitState {
	t.Helper()
	st, err := buildmonitor.ReadState(buildMonitorUnitStatePath("repo-a"))
	if err != nil || st == nil {
		t.Fatalf("ReadState = %v, %v", st, err)
	}
	return st
}

// failingWorkflow builds a failing workflow row with a claimed run.
func failingWorkflow(id int64, name string, runID int64, headSHA string) buildmonitor.WorkflowState {
	return buildmonitor.WorkflowState{
		WorkflowID: id, Name: name, RunID: runID, Status: "completed",
		Conclusion: "failure", FirstFailureRunID: runID, HeadSHA: headSHA,
	}
}

// A failure with no agent working launches one in the workflow's dedicated
// workspace, and the row and the ledger record it.
func TestBuildMonitorLaunch_NoAgentLaunchesInDedicatedWorkspace(t *testing.T) {
	unit := &buildmonitor.UnitState{
		RepoName: "Repo A", Repo: "o/r", Branch: "main", HeadSHA: "abc",
		Workflows: []buildmonitor.WorkflowState{failingWorkflow(1, "CI", 11, "abc")},
		RecentRemediations: []buildmonitor.RemediationRecord{
			{WorkflowID: 1, RunID: 11, HeadSHA: "abc", Status: buildmonitor.RemediationClaimed},
		},
	}
	f := newBuildMonitorLaunchFixture(t, unit)

	f.server.launchBuildFailureSession(f.directive(unit.Workflows[0]))

	if len(f.spawns) != 1 {
		t.Fatalf("spawns = %d, want 1: %+v", len(f.spawns), f.spawns)
	}
	if got := f.spawns[0].WorkspaceID; got != "ws-ci" {
		t.Errorf("spawn workspace = %q, want the dedicated workspace ws-ci", got)
	}
	st := f.unitState(t)
	if got := st.Workflows[0].SessionID; got != "sess-1" {
		t.Errorf("row session = %q, want the launched session", got)
	}
	if len(st.RecentRemediations) != 1 {
		t.Fatalf("ledger = %+v, want one record", st.RecentRemediations)
	}
	rec := st.RecentRemediations[0]
	if rec.SessionID != "sess-1" || rec.Status != buildmonitor.RemediationLaunched {
		t.Errorf("ledger record = %+v, want a launched record for sess-1", rec)
	}
}

// An agent already working on the workflow owns it: the run is recorded
// against that agent and nothing launches.
func TestBuildMonitorLaunch_WorkingAgentLaunchesNothing(t *testing.T) {
	unit := &buildmonitor.UnitState{
		RepoName: "Repo A", Repo: "o/r", Branch: "main", HeadSHA: "abc",
		Workflows: []buildmonitor.WorkflowState{
			func() buildmonitor.WorkflowState {
				w := failingWorkflow(1, "CI", 11, "abc")
				w.SessionID = "s1"
				return w
			}(),
		},
		RecentRemediations: []buildmonitor.RemediationRecord{
			{WorkflowID: 1, RunID: 11, HeadSHA: "abc", Status: buildmonitor.RemediationClaimed},
		},
	}
	f := newBuildMonitorLaunchFixture(t, unit)
	if err := f.st.AddSession(state.Session{ID: "s1", WorkspaceID: "ws-ci", Target: "claude"}); err != nil {
		t.Fatal(err)
	}

	f.server.launchBuildFailureSession(f.directive(unit.Workflows[0]))

	if len(f.spawns) != 0 {
		t.Fatalf("spawns = %d, want none: %+v", len(f.spawns), f.spawns)
	}
	st := f.unitState(t)
	if len(st.RecentRemediations) != 1 {
		t.Fatalf("ledger = %+v, want one record", st.RecentRemediations)
	}
	rec := st.RecentRemediations[0]
	if rec.SessionID != "s1" || rec.Status != buildmonitor.RemediationJoined {
		t.Errorf("ledger record = %+v, want a joined record for s1", rec)
	}
}

// A workflow that fails again while its agent is still working launches
// nothing: the agent outlives the failure episode.
func TestBuildMonitorLaunch_RecoveredThenFailedWithLiveAgentLaunchesNothing(t *testing.T) {
	failing := failingWorkflow(1, "CI", 11, "abc")
	failing.SessionID = "s1"
	prev := &buildmonitor.UnitState{Workflows: []buildmonitor.WorkflowState{failing}}
	recovered := &buildmonitor.UnitState{Workflows: []buildmonitor.WorkflowState{
		{WorkflowID: 1, Name: "CI", RunID: 12, Status: "completed", Conclusion: "success", HeadSHA: "abc"},
	}}
	buildmonitor.ApplyTransitions(prev, recovered)
	refailed := &buildmonitor.UnitState{Workflows: []buildmonitor.WorkflowState{
		{WorkflowID: 1, Name: "CI", RunID: 13, Status: "completed", Conclusion: "failure", HeadSHA: "abc"},
	}}
	buildmonitor.ApplyTransitions(recovered, refailed)
	if refailed.Workflows[0].SessionID != "s1" {
		t.Fatalf("carried session = %q, want s1", refailed.Workflows[0].SessionID)
	}

	refailed.RecentRemediations = []buildmonitor.RemediationRecord{
		{WorkflowID: 1, RunID: 13, HeadSHA: "abc", Status: buildmonitor.RemediationClaimed},
	}
	f := newBuildMonitorLaunchFixture(t, refailed)
	if err := f.st.AddSession(state.Session{ID: "s1", WorkspaceID: "ws-ci", Target: "claude"}); err != nil {
		t.Fatal(err)
	}

	f.server.launchBuildFailureSession(f.directive(refailed.Workflows[0]))

	if len(f.spawns) != 0 {
		t.Fatalf("spawns = %d, want none: %+v", len(f.spawns), f.spawns)
	}
}

// A row whose session is gone, or whose session lives in another workspace,
// has no agent: it gets a new one, in its own dedicated workspace.
func TestBuildMonitorLaunch_DeadSessionReplaced(t *testing.T) {
	dead := failingWorkflow(1, "CI", 11, "abc")
	dead.SessionID = "gone"
	foreign := failingWorkflow(2, "CI Image", 12, "abc")
	foreign.SessionID = "s1"
	unit := &buildmonitor.UnitState{
		RepoName: "Repo A", Repo: "o/r", Branch: "main", HeadSHA: "abc",
		Workflows: []buildmonitor.WorkflowState{dead, foreign},
		RecentRemediations: []buildmonitor.RemediationRecord{
			{WorkflowID: 1, RunID: 11, HeadSHA: "abc", Status: buildmonitor.RemediationClaimed},
			{WorkflowID: 2, RunID: 12, HeadSHA: "abc", Status: buildmonitor.RemediationClaimed},
		},
	}
	f := newBuildMonitorLaunchFixture(t, unit)
	if err := f.st.AddWorkspace(state.Workspace{
		ID: "ws-elsewhere", Repo: "https://github.com/o/r", Branch: "other", Path: t.TempDir(),
	}); err != nil {
		t.Fatal(err)
	}
	if err := f.st.AddSession(state.Session{ID: "s1", WorkspaceID: "ws-elsewhere", Target: "claude"}); err != nil {
		t.Fatal(err)
	}

	f.server.launchBuildFailureSession(f.directive(dead))
	f.server.launchBuildFailureSession(f.directive(foreign))

	if len(f.spawns) != 2 {
		t.Fatalf("spawns = %d, want one per row: %+v", len(f.spawns), f.spawns)
	}
	st := f.unitState(t)
	if got := st.Workflows[0].SessionID; got != "sess-1" {
		t.Errorf("CI row session = %q, want the new session", got)
	}
	if got := st.Workflows[1].SessionID; got != "sess-2" {
		t.Errorf("CI Image row session = %q, want the new session", got)
	}
}

// The unit response reports a session only while it works on the workflow's
// own workspace.
func TestBuildMonitorGet_OmitsSessionOutsideDedicatedWorkspace(t *testing.T) {
	foreign := failingWorkflow(2, "CI Image", 12, "abc")
	foreign.SessionID = "s1"
	unit := &buildmonitor.UnitState{
		RepoName: "Repo A", Repo: "o/r", Branch: "main", HeadSHA: "abc",
		Workflows: []buildmonitor.WorkflowState{foreign},
	}
	f := newBuildMonitorLaunchFixture(t, unit)
	if err := f.st.AddWorkspace(state.Workspace{
		ID: "ws-elsewhere", Repo: "https://github.com/o/r", Branch: "other", Path: t.TempDir(),
	}); err != nil {
		t.Fatal(err)
	}
	if err := f.st.AddSession(state.Session{ID: "s1", WorkspaceID: "ws-elsewhere", Target: "claude"}); err != nil {
		t.Fatal(err)
	}

	resp := getBuildMonitor(t, f.server)

	if len(resp.Units) != 1 {
		t.Fatalf("units = %d, want 1: %+v", len(resp.Units), resp.Units)
	}
	if got := resp.Units[0].Workflows[0].SessionID; got != "" {
		t.Errorf("session = %q, want none for a session outside the dedicated workspace", got)
	}
}

// Two workflows of one repo get their own workspaces.
func TestBuildMonitorLaunch_DifferentWorkflowsDifferentWorkspaces(t *testing.T) {
	unit := &buildmonitor.UnitState{
		RepoName: "Repo A", Repo: "o/r", Branch: "main", HeadSHA: "abc",
		Workflows: []buildmonitor.WorkflowState{
			failingWorkflow(1, "CI", 11, "abc"),
			failingWorkflow(2, "CI Image", 12, "abc"),
		},
		RecentRemediations: []buildmonitor.RemediationRecord{
			{WorkflowID: 1, RunID: 11, HeadSHA: "abc", Status: buildmonitor.RemediationClaimed},
			{WorkflowID: 2, RunID: 12, HeadSHA: "abc", Status: buildmonitor.RemediationClaimed},
		},
	}
	f := newBuildMonitorLaunchFixture(t, unit)

	f.server.launchBuildFailureSession(f.directive(unit.Workflows[0]))
	f.server.launchBuildFailureSession(f.directive(unit.Workflows[1]))

	if len(f.spawns) != 2 {
		t.Fatalf("spawns = %d, want 2: %+v", len(f.spawns), f.spawns)
	}
	if f.spawns[0].WorkspaceID == f.spawns[1].WorkspaceID {
		t.Errorf("both launches used workspace %q, want one workspace per workflow", f.spawns[0].WorkspaceID)
	}
	if f.spawns[0].WorkspaceID != "ws-ci" || f.spawns[1].WorkspaceID != "ws-ci-image" {
		t.Errorf("workspaces = %q, %q, want ws-ci and ws-ci-image", f.spawns[0].WorkspaceID, f.spawns[1].WorkspaceID)
	}
}

// One broken push pulls the dashboards to an agent once, however many
// workflows it breaks.
func TestBuildMonitorLaunch_OneFocusPullPerCommit(t *testing.T) {
	unit := &buildmonitor.UnitState{
		RepoName: "Repo A", Repo: "o/r", Branch: "main", HeadSHA: "abc",
		Workflows: []buildmonitor.WorkflowState{
			failingWorkflow(1, "CI", 11, "abc"),
			failingWorkflow(2, "CI Image", 12, "abc"),
		},
		RecentRemediations: []buildmonitor.RemediationRecord{
			{WorkflowID: 1, RunID: 11, HeadSHA: "abc", Status: buildmonitor.RemediationClaimed},
			{WorkflowID: 2, RunID: 12, HeadSHA: "abc", Status: buildmonitor.RemediationClaimed},
		},
	}
	f := newBuildMonitorLaunchFixture(t, unit)

	f.server.launchBuildFailureSession(f.directive(unit.Workflows[0]))
	f.server.launchBuildFailureSession(f.directive(unit.Workflows[1]))

	if len(f.pulled) != 1 {
		t.Fatalf("focus pulls = %d, want 1 for one broken push: %+v", len(f.pulled), f.pulled)
	}

	// A later push breaks the same workflow again.
	later := &buildmonitor.UnitState{
		RepoName: "Repo A", Repo: "o/r", Branch: "main", HeadSHA: "def",
		Workflows: []buildmonitor.WorkflowState{failingWorkflow(1, "CI", 13, "def")},
		RecentRemediations: []buildmonitor.RemediationRecord{
			{WorkflowID: 1, RunID: 13, HeadSHA: "def", Status: buildmonitor.RemediationClaimed},
		},
	}
	if err := buildmonitor.WriteState(buildMonitorUnitStatePath("repo-a"), later); err != nil {
		t.Fatal(err)
	}
	f.server.launchBuildFailureSession(f.directive(later.Workflows[0]))

	if len(f.pulled) != 2 {
		t.Fatalf("focus pulls = %d, want a second one for the later push: %+v", len(f.pulled), f.pulled)
	}
}

// The ledger still prevents a second launch for the same run.
func TestBuildMonitorLaunch_LedgerStillDedupes(t *testing.T) {
	unit := &buildmonitor.UnitState{
		RepoName: "Repo A", Repo: "o/r", Branch: "main", HeadSHA: "abc",
		Workflows: []buildmonitor.WorkflowState{failingWorkflow(1, "CI", 11, "abc")},
		RecentRemediations: []buildmonitor.RemediationRecord{
			{WorkflowID: 1, RunID: 11, HeadSHA: "abc", Status: buildmonitor.RemediationClaimed},
		},
	}
	f := newBuildMonitorLaunchFixture(t, unit)

	f.server.launchBuildFailureSession(f.directive(unit.Workflows[0]))
	f.server.launchBuildFailureSession(f.directive(unit.Workflows[0]))

	if len(f.spawns) != 1 {
		t.Fatalf("spawns = %d, want 1 for a run claimed twice: %+v", len(f.spawns), f.spawns)
	}
}

// A manual launch while the workflow's agent works returns that agent and
// starts nothing.
func TestBuildMonitorLaunch_ManualWithWorkingAgentLaunchesNothing(t *testing.T) {
	unit := &buildmonitor.UnitState{
		RepoName: "Repo A", Repo: "o/r", Branch: "main", HeadSHA: "abc",
		Workflows: []buildmonitor.WorkflowState{
			func() buildmonitor.WorkflowState {
				w := failingWorkflow(1, "CI", 11, "abc")
				w.SessionID = "s1"
				return w
			}(),
		},
	}
	f := newBuildMonitorLaunchFixture(t, unit)
	if err := f.st.AddSession(state.Session{ID: "s1", WorkspaceID: "ws-ci", Target: "claude"}); err != nil {
		t.Fatal(err)
	}

	w, r := launchRequest(t, "repo-a", "11")
	f.server.handleBuildMonitorLaunch(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("code = %d, want 200: %s", w.Code, w.Body.String())
	}
	var resp contracts.BuildMonitorLaunchResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if resp.SessionID != "s1" {
		t.Errorf("session = %q, want the working agent s1", resp.SessionID)
	}
	if len(f.spawns) != 0 {
		t.Fatalf("spawns = %d, want none: %+v", len(f.spawns), f.spawns)
	}
}

// A manual launch that arrives while an automatic one is still spawning waits
// for it and returns its agent: one workspace never gets two agents.
func TestBuildMonitorLaunch_ManualDuringAutomaticStartsOneAgent(t *testing.T) {
	unit := &buildmonitor.UnitState{
		RepoName: "Repo A", Repo: "o/r", Branch: "main", HeadSHA: "abc",
		Workflows: []buildmonitor.WorkflowState{failingWorkflow(1, "CI", 11, "abc")},
		RecentRemediations: []buildmonitor.RemediationRecord{
			{WorkflowID: 1, RunID: 11, HeadSHA: "abc", Status: buildmonitor.RemediationClaimed},
		},
	}
	f := newBuildMonitorLaunchFixture(t, unit)
	started := make(chan struct{}, 2)
	release := make(chan struct{})
	f.server.buildMonitorSpawn = func(req SpawnRequest) ([]SessionResult, int, error) {
		started <- struct{}{}
		<-release
		return f.spawn(req)
	}
	await := func(what string, ch <-chan struct{}) {
		t.Helper()
		select {
		case <-ch:
		case <-time.After(30 * time.Second): // failure backstop
			t.Fatalf("%s did not happen", what)
		}
	}

	automatic := make(chan struct{})
	go func() {
		defer close(automatic)
		f.server.runBuildMonitorLaunches([]launchDirective{f.directive(unit.Workflows[0])})
	}()
	await("the automatic spawn", started)

	w, r := launchRequest(t, "repo-a", "11")
	manual := make(chan struct{})
	go func() {
		defer close(manual)
		f.server.handleBuildMonitorLaunch(w, r)
	}()
	select {
	case <-started:
		t.Error("the manual launch spawned while the automatic one was in flight")
	case <-manual:
		t.Errorf("the manual launch answered while the automatic one was in flight: %s", w.Body.String())
	case <-time.After(200 * time.Millisecond):
		// Bounded window: the claim is that the manual launch waits. One that
		// did not wait would reach the stubbed spawn in milliseconds.
	}
	close(release)
	await("the automatic launch's end", automatic)
	await("the manual launch's end", manual)

	if len(f.spawns) != 1 {
		t.Fatalf("spawns = %d, want 1: %+v", len(f.spawns), f.spawns)
	}
	var resp contracts.BuildMonitorLaunchResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("code = %d body = %s: %v", w.Code, w.Body.String(), err)
	}
	if w.Code != http.StatusOK || resp.SessionID != "sess-1" {
		t.Errorf("code = %d session = %q, want 200 and the automatic launch's sess-1", w.Code, resp.SessionID)
	}
}

// A run that joins a working agent starts nothing, so it does not use up the
// commit's focus pull.
func TestBuildMonitorLaunch_JoinedRunLeavesTheFocusPull(t *testing.T) {
	working := failingWorkflow(1, "CI", 11, "abc")
	working.SessionID = "s1"
	unit := &buildmonitor.UnitState{
		RepoName: "Repo A", Repo: "o/r", Branch: "main", HeadSHA: "abc",
		Workflows: []buildmonitor.WorkflowState{working, failingWorkflow(2, "CI Image", 12, "abc")},
		RecentRemediations: []buildmonitor.RemediationRecord{
			{WorkflowID: 1, RunID: 11, HeadSHA: "abc", Status: buildmonitor.RemediationClaimed},
			{WorkflowID: 2, RunID: 12, HeadSHA: "abc", Status: buildmonitor.RemediationClaimed},
		},
	}
	f := newBuildMonitorLaunchFixture(t, unit)
	if err := f.st.AddSession(state.Session{ID: "s1", WorkspaceID: "ws-ci", Target: "claude"}); err != nil {
		t.Fatal(err)
	}

	f.server.launchBuildFailureSession(f.directive(unit.Workflows[0]))
	f.server.launchBuildFailureSession(f.directive(unit.Workflows[1]))

	if len(f.pulled) != 1 || f.pulled[0] != "sess-1" {
		t.Fatalf("focus pulls = %+v, want one, to the agent that was launched", f.pulled)
	}
}

// Every workflow row carries the name of its dedicated workspace, and the
// unit no longer carries a remediation workspace.
func TestBuildMonitorGet_WorkflowCarriesWorkspaceName(t *testing.T) {
	unit := &buildmonitor.UnitState{
		RepoName: "Repo A", Repo: "o/r", Branch: "main", HeadSHA: "abc",
		Workflows: []buildmonitor.WorkflowState{failingWorkflow(1, "CI", 11, "abc")},
	}
	f := newBuildMonitorLaunchFixture(t, unit)

	resp := getBuildMonitor(t, f.server)

	if len(resp.Units) != 1 || len(resp.Units[0].Workflows) != 1 {
		t.Fatalf("units = %+v, want one unit with one workflow", resp.Units)
	}
	if got := resp.Units[0].Workflows[0].Workspace; got != "build-monitor/repo-a/ci" {
		t.Errorf("workspace = %q, want build-monitor/repo-a/ci", got)
	}
	raw, err := json.Marshal(resp)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "remediation_workspace_id") {
		t.Errorf("unit response still carries the remediation workspace: %s", raw)
	}
}

func TestApplyBuildMonitor_ConvertsNameKeysToSlug(t *testing.T) {
	cfg := &config.Config{}
	req := &contracts.BuildMonitorConfig{
		Enabled:                     true,
		IntervalSeconds:             90,
		Target:                      " claude ",
		AutoWorkspaceOnFirstFailure: true,
		Chat:                        true,
		Repos: map[string]contracts.BuildMonitorRepoConfig{
			"My Repo": {Enabled: true, GitHubLogin: "octocat"},
		},
	}
	applyBuildMonitor(cfg, req)
	if _, ok := cfg.BuildMonitor.Repos["my-repo"]; !ok {
		t.Fatalf("expected slug key 'my-repo', got keys %v", cfg.BuildMonitor.Repos)
	}
	if cfg.BuildMonitor.Repos["my-repo"].GitHubLogin != "octocat" {
		t.Fatalf("expected GitHubLogin octocat, got %q", cfg.BuildMonitor.Repos["my-repo"].GitHubLogin)
	}
	if cfg.BuildMonitor.IntervalSeconds != 90 {
		t.Fatalf("expected IntervalSeconds 90, got %d", cfg.BuildMonitor.IntervalSeconds)
	}
	if cfg.BuildMonitor.Target != "claude" {
		t.Fatalf("expected trimmed Target claude, got %q", cfg.BuildMonitor.Target)
	}
	if !cfg.BuildMonitor.AutoWorkspaceOnFirstFailure {
		t.Fatal("expected AutoWorkspaceOnFirstFailure true")
	}
	if !cfg.BuildMonitor.Chat {
		t.Fatal("expected Chat true")
	}
}

func TestApplyBuildMonitor_NilInput(t *testing.T) {
	cfg := &config.Config{}
	applyBuildMonitor(cfg, (*contracts.BuildMonitorConfig)(nil))
	if cfg.BuildMonitor != nil {
		t.Fatal("expected nil BuildMonitor after nil input")
	}
}

func TestConfigGet_BuildMonitorChat(t *testing.T) {
	server, cfg, _ := newTestServer(t)
	cfg.BuildMonitor = &config.BuildMonitorConfig{Enabled: true, Chat: true}
	h := newTestConfigHandlers(server)

	rr := httptest.NewRecorder()
	h.handleConfigGet(rr, httptest.NewRequest(http.MethodGet, "/api/config", nil))
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d (%s)", rr.Code, rr.Body.String())
	}
	var resp contracts.ConfigResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !resp.BuildMonitor.Chat {
		t.Fatalf("build_monitor.chat = false in response, want true: %s", rr.Body.String())
	}
}

func TestCollectUnitDirectives(t *testing.T) {
	st := &buildmonitor.UnitState{Workflows: []buildmonitor.WorkflowState{
		{WorkflowID: 1, Name: "CI", RunID: 11, Status: "completed", Conclusion: "failure", FirstFailureRunID: 11, HeadSHA: "abc"},
		{WorkflowID: 2, Name: "Lint", RunID: 22, Status: "completed", Conclusion: "failure", FirstFailureRunID: 22, HeadSHA: "abc"},
	}}
	events := []buildmonitor.TransitionEvent{
		{WorkflowID: 1, Kind: buildmonitor.TransitionEnteredFailure, RunID: 11},
		{WorkflowID: 2, Kind: buildmonitor.TransitionEnteredFailure, FromUnknown: true, RunID: 22},
	}
	base := launchDirective{slug: "repo-a", repoName: "Repo A", repo: "o/r", login: "octocat"}
	got := collectUnitDirectives(base, events, st, "2026-08-13T08:00:00Z")
	if len(got) != 1 {
		t.Fatalf("got %d directives, want 1 (FromUnknown excluded): %+v", len(got), got)
	}
	if got[0].workflow.WorkflowID != 1 || got[0].workflow.HeadSHA != "abc" || got[0].slug != "repo-a" {
		t.Fatalf("directive = %+v", got[0])
	}
	if got := collectUnitDirectives(base, events, st, "2026-08-13T08:05:00Z"); len(got) != 0 {
		t.Fatalf("duplicate check produced directives: %+v", got)
	}
}

// newBuildMonitorGetServer persists st as unit "repo-a" (repo o/r, identity
// octocat), optionally seeds the persisted commit store, and returns a server
// that hydrated from both at startup.
func newBuildMonitorGetServer(t *testing.T, st *buildmonitor.UnitState, commitsJSON string) *Server {
	t.Helper()
	schmuxdir.Set(t.TempDir())
	t.Cleanup(func() { schmuxdir.Set("") })

	if err := buildmonitor.WriteState(buildMonitorUnitStatePath("repo-a"), st); err != nil {
		t.Fatal(err)
	}
	if commitsJSON != "" {
		if err := os.WriteFile(filepath.Join(buildMonitorStateDir(), "commits.json"), []byte(commitsJSON), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	configPath := filepath.Join(t.TempDir(), "config.json")
	cfg := config.CreateDefault(configPath)
	cfg.WorkspacePath = t.TempDir()
	cfg.Repos = []config.Repo{{Name: "Repo A", URL: "https://github.com/o/r"}}
	cfg.BuildMonitor = &config.BuildMonitorConfig{
		Enabled: true,
		Repos:   map[string]config.BuildMonitorRepoConfig{"repo-a": {Enabled: true, GitHubLogin: "octocat"}},
	}
	if err := cfg.Save(); err != nil {
		t.Fatal(err)
	}

	statePath := filepath.Join(t.TempDir(), "state.json")
	stStore := state.New(statePath, nil)
	logger := log.NewWithOptions(io.Discard, log.Options{})
	wm := workspace.New(cfg, stStore, statePath, logger)
	sm := session.New(cfg, stStore, statePath, wm, nil, logger)
	shutdownCtx, shutdownCancel := context.WithCancel(context.Background())
	t.Cleanup(shutdownCancel)
	server := NewServer(cfg, stStore, statePath, sm, wm, github.NewDiscovery(nil), logger, contracts.GitHubStatus{}, nil, ServerOptions{ShutdownCtx: shutdownCtx})
	server.SetModelManager(models.New(cfg, nil, "", logger))
	t.Cleanup(func() { server.CloseForTest() })
	return server
}

// getBuildMonitor calls GET /api/build-monitor and decodes the response.
func getBuildMonitor(t *testing.T, server *Server) contracts.BuildMonitorResponse {
	t.Helper()
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/api/build-monitor", nil)
	server.handleBuildMonitorGet(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("code = %d: %s", w.Code, w.Body.String())
	}
	var resp contracts.BuildMonitorResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	return resp
}

// Review Focus 4. Workflows on main are independent: the head's push-triggered
// CI passed, but the scheduled Performance workflow's newest run, on an
// earlier commit, failed. The unit reads failure, so the toolbar counts it,
// while the head commit's chip stays green.
func TestBuildMonitorGetUnitStatusFollowsWorkflowRows(t *testing.T) {
	st := &buildmonitor.UnitState{
		RepoName: "Repo A", Repo: "o/r", Branch: "main", HeadSHA: "h2",
		CheckedAt: "2026-10-07T06:27:05Z",
		Workflows: []buildmonitor.WorkflowState{
			{WorkflowID: 1, Name: "CI", RunID: 21, Status: "completed", Conclusion: "success", HeadSHA: "h2"},
			{WorkflowID: 2, Name: "Performance", RunID: 14, Status: "completed", Conclusion: "failure", HeadSHA: "h1", FirstFailureRunID: 14},
		},
	}
	server := newBuildMonitorGetServer(t, st, "")

	resp := getBuildMonitor(t, server)
	if len(resp.Units) != 1 {
		t.Fatalf("units = %d, want 1: %+v", len(resp.Units), resp.Units)
	}
	if got := resp.Units[0].Status; got != "failure" {
		t.Errorf("unit status = %q, want failure (Performance row is red)", got)
	}
	info := github.RepoInfo{Owner: "o", Repo: "r"}
	if status, _, ok := server.buildMonitor.Status(info, "main", "h2"); !ok || status != "success" {
		t.Errorf("head chip = (%q, %v), want (success, true)", status, ok)
	}
}

// TestBuildMonitorGetServesHydratedStateAfterRestart is the restart
// regression: the durable unit files must reach the page immediately after
// server construction, before any check pass runs.
func TestBuildMonitorGetServesHydratedStateAfterRestart(t *testing.T) {
	st := &buildmonitor.UnitState{
		RepoName: "Repo A", Repo: "o/r", Branch: "main", HeadSHA: "h1",
		CheckedAt: "2026-08-20T10:00:00Z",
		Workflows: []buildmonitor.WorkflowState{
			{WorkflowID: 1, Name: "CI", RunID: 11, Status: "completed", Conclusion: "success", HeadSHA: "h1", HTMLURL: "u"},
		},
	}
	// A watched feature-branch head recorded by the previous process — it
	// exists in no unit snapshot, only in the persisted commit store.
	commits := `[{"owner":"o","repo":"r","branch":"feature","sha":"f1","status":"success","url":"https://run9","terminal":true,"fetched_at":"2026-08-20T10:00:00Z"}]`
	server := newBuildMonitorGetServer(t, st, commits)

	resp := getBuildMonitor(t, server)
	if len(resp.Units) != 1 {
		t.Fatalf("units = %d, want 1: %+v", len(resp.Units), resp.Units)
	}
	u := resp.Units[0]
	if u.Status != "success" {
		t.Errorf("status = %q, want success (hydrated from durable state)", u.Status)
	}
	if len(u.Workflows) != 1 || u.Workflows[0].RunID != 11 {
		t.Errorf("workflows = %+v, want the persisted CI row", u.Workflows)
	}
	if u.HeadSHA != "h1" || u.Branch != "main" {
		t.Errorf("head = %q branch = %q, want h1/main", u.HeadSHA, u.Branch)
	}

	// The feature-branch head from the persisted commit store must survive
	// the restart too — this is what workspace CI chips read.
	info := github.RepoInfo{Owner: "o", Repo: "r"}
	if status, url, ok := server.buildMonitor.Status(info, "feature", "f1"); !ok || status != "success" || url != "https://run9" {
		t.Errorf("watched head after restart = (%q, %q, %v), want (success, https://run9, true)", status, url, ok)
	}
}

func launchRequest(t *testing.T, slug, runID string) (*httptest.ResponseRecorder, *http.Request) {
	t.Helper()
	r := httptest.NewRequest(http.MethodPost,
		"/api/build-monitor/repos/"+slug+"/failures/"+runID+"/launch-workspace", nil)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("slug", slug)
	rctx.URLParams.Add("runID", runID)
	r = r.WithContext(context.WithValue(r.Context(), chi.RouteCtxKey, rctx))
	return httptest.NewRecorder(), r
}

func TestHandleBuildMonitorLaunch_Validation(t *testing.T) {
	schmuxdir.Set(t.TempDir())
	defer schmuxdir.Set("")

	t.Run("feature disabled is 400", func(t *testing.T) {
		s := &Server{config: &config.Config{}}
		w, r := launchRequest(t, "repo-a", "11")
		s.handleBuildMonitorLaunch(w, r)
		if w.Code != http.StatusBadRequest {
			t.Fatalf("code = %d, want 400", w.Code)
		}
	})

	t.Run("no target is 400", func(t *testing.T) {
		s := &Server{config: &config.Config{ConfigData: config.ConfigData{BuildMonitor: &config.BuildMonitorConfig{Enabled: true}}}}
		w, r := launchRequest(t, "repo-a", "11")
		s.handleBuildMonitorLaunch(w, r)
		if w.Code != http.StatusBadRequest {
			t.Fatalf("code = %d, want 400", w.Code)
		}
	})

	t.Run("unmonitored repo is 404", func(t *testing.T) {
		s := &Server{config: &config.Config{ConfigData: config.ConfigData{BuildMonitor: &config.BuildMonitorConfig{
			Enabled: true, Target: "claude",
		}}}}
		w, r := launchRequest(t, "repo-a", "11")
		s.handleBuildMonitorLaunch(w, r)
		if w.Code != http.StatusNotFound {
			t.Fatalf("code = %d, want 404", w.Code)
		}
	})

	t.Run("run not in failing state is 404", func(t *testing.T) {
		cfg := &config.Config{
			ConfigData: config.ConfigData{
				Repos: []config.Repo{{Name: "Repo A", URL: "https://github.com/o/r"}},
				BuildMonitor: &config.BuildMonitorConfig{
					Enabled: true, Target: "claude",
					Repos: map[string]config.BuildMonitorRepoConfig{
						"repo-a": {Enabled: true, GitHubLogin: "octocat"},
					},
				},
			},
		}
		st := &buildmonitor.UnitState{Workflows: []buildmonitor.WorkflowState{
			{WorkflowID: 1, RunID: 11, Status: "completed", Conclusion: "success"},
		}}
		if err := buildmonitor.WriteState(buildMonitorUnitStatePath("repo-a"), st); err != nil {
			t.Fatal(err)
		}
		s := &Server{config: cfg}
		w, r := launchRequest(t, "repo-a", "11")
		s.handleBuildMonitorLaunch(w, r)
		if w.Code != http.StatusNotFound {
			t.Fatalf("code = %d, want 404: %s", w.Code, w.Body.String())
		}
	})
}

// A manual launch goes through the spawn path, so a failed launch returns
// the spawn path's reason in the error body so the page can show it. The
// launch targets the workflow's dedicated workspace.
func TestHandleBuildMonitorLaunch_FailedLaunchReturnsSpawnPathReason(t *testing.T) {
	st := &buildmonitor.UnitState{
		RepoName: "Repo A", Repo: "o/r", Branch: "main", HeadSHA: "h2",
		Workflows: []buildmonitor.WorkflowState{
			{WorkflowID: 2, Name: "Performance", RunID: 14, Status: "completed", Conclusion: "failure", HeadSHA: "78971fb0", FirstFailureRunID: 14},
		},
	}
	f := newBuildMonitorLaunchFixture(t, st)
	// The real spawn path, not the stub.
	f.server.buildMonitorSpawn = nil
	// A command run target rejects the prompt per target, after validation
	// and before any session is created.
	f.cfg.RunTargets = []config.RunTarget{{Name: "command", Command: "echo command"}}
	f.cfg.BuildMonitor.Target = "command"

	w, r := launchRequest(t, "repo-a", "14")
	f.server.handleBuildMonitorLaunch(w, r)

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("code = %d, want 500: %s", w.Code, w.Body.String())
	}
	var body map[string]string
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(body["error"], "prompt is not allowed for command targets") {
		t.Errorf("error = %q, want the spawn path's reason", body["error"])
	}
	recs := readSpawnLog(t)
	if len(recs) != 1 {
		t.Fatalf("spawn log has %d records, want 1: %+v", len(recs), recs)
	}
	if rec := recs[0]; rec.WorkspaceID != "ws-performance" {
		t.Errorf("record = %+v, want the workflow's dedicated workspace", rec)
	}
	assertStagingEmpty(t, f.server)
}

// TestBuildMonitorInputs_HeadsUseStoredWorkspaceFields pins the single-source
// rule: the watch set is built from the same stored workspace fields the
// sessions read path uses (RemoteHeadSHA + fork repo) — never from a separate
// git derivation — so the monitor and the CI chips cannot ask about
// different commits.
func TestBuildMonitorInputs_HeadsUseStoredWorkspaceFields(t *testing.T) {
	server, cfg, st := newTestServer(t)
	if err := config.SaveGitHubIdentity("octocat", "tok", "repo"); err != nil {
		t.Fatal(err)
	}
	cfg.Repos = []config.Repo{{Name: "Widget", URL: "https://github.com/acme/widget"}}
	cfg.BuildMonitor = &config.BuildMonitorConfig{
		Enabled: true,
		Repos:   map[string]config.BuildMonitorRepoConfig{"widget": {Enabled: true, GitHubLogin: "octocat"}},
	}
	seed := []state.Workspace{
		{ID: "ws-base", Repo: "https://github.com/acme/widget", Branch: "feat", Path: t.TempDir(),
			RemoteBranchExists: true, RemoteHeadSHA: "f1"},
		{ID: "ws-fork", Repo: "https://github.com/acme/widget", Branch: "fix", Path: t.TempDir(),
			RemoteBranchExists: true, RemoteHeadSHA: "f2",
			RemoteBranchIsFork: true, RemoteBranchURL: "https://github.com/forker/widget"},
		{ID: "ws-nosha", Repo: "https://github.com/acme/widget", Branch: "wip", Path: t.TempDir(),
			RemoteBranchExists: true},
	}
	for _, w := range seed {
		if err := st.AddWorkspace(w); err != nil {
			t.Fatal(err)
		}
	}

	_, heads, _, _ := server.buildMonitorInputs(context.Background(), cfg.GetRepos(), cfg.GetBuildMonitorRepos())

	bySHA := map[string]buildmonitor.HeadInput{}
	for _, h := range heads {
		bySHA[h.SHA] = h
	}
	if len(heads) != 2 {
		t.Fatalf("heads = %+v, want exactly the two workspaces with a stored remote head", heads)
	}
	base := bySHA["f1"]
	if base.Info.Owner != "acme" || base.Info.Repo != "widget" || base.Branch != "feat" {
		t.Errorf("base head = %+v, want acme/widget@feat", base)
	}
	fork := bySHA["f2"]
	if fork.Info.Owner != "forker" || fork.Info.Repo != "widget" || fork.Branch != "fix" {
		t.Errorf("fork head = %+v, want forker/widget@fix (fork-resolved from stored RemoteBranchURL)", fork)
	}
}
