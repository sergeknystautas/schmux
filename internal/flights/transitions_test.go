package flights

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sergeknystautas/schmux/internal/config"
	"github.com/sergeknystautas/schmux/internal/state"
)

var bg = context.Background()

func (e *testEnv) artifact(id string) string {
	return filepath.Join(e.fk.workspaces.path, ".schmux", "flights", id, "plan.md")
}

func takeoff(t *testing.T, e *testEnv) Flight {
	t.Helper()
	created := mustCreate(t, e.m, goodCreate())
	f, err := e.m.Takeoff(bg, created.ID)
	if err != nil {
		t.Fatalf("Takeoff() error = %v", err)
	}
	return f
}

func writePlan(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func execute(t *testing.T, e *testEnv) Flight {
	t.Helper()
	f := takeoff(t, e)
	writePlan(t, e.artifact(f.ID), "# the plan\n")
	f, err := e.m.EnterExecution(bg, f.ID)
	if err != nil {
		t.Fatalf("EnterExecution() error = %v", err)
	}
	return f
}

func complete(t *testing.T, e *testEnv) Flight {
	t.Helper()
	f := execute(t, e)
	f, err := e.m.EnterCompletion(bg, f.ID)
	if err != nil {
		t.Fatalf("EnterCompletion() error = %v", err)
	}
	return f
}

func sessionFor(t *testing.T, f Flight, role SessionRole) SessionAssignment {
	t.Helper()
	for _, s := range f.Sessions {
		if s.Role == role {
			return s
		}
	}
	t.Fatalf("flight %s has no %s assignment: %+v", f.ID, role, f.Sessions)
	return SessionAssignment{}
}

func lastOp(t *testing.T, f Flight, action OperationAction) Operation {
	t.Helper()
	for i := len(f.Operations) - 1; i >= 0; i-- {
		if f.Operations[i].Action == action {
			return f.Operations[i]
		}
	}
	t.Fatalf("flight %s has no %s operation: %+v", f.ID, action, f.Operations)
	return Operation{}
}

func TestTakeoff_AllocatesWorkspaceAndSpawnsPlanner(t *testing.T) {
	e := newEnv(t)
	e.fk.workspaces.branchSuffix = "-2" // the workspace manager chose a unique branch
	req := goodCreate()
	created := mustCreate(t, e.m, req)
	if err := e.m.editFlight(created.ID, func(f *Flight) error { f.ShortName = "Fix login"; return nil }); err != nil {
		t.Fatal(err)
	}

	f, err := e.m.Takeoff(bg, created.ID)

	if err != nil {
		t.Fatalf("Takeoff() error = %v", err)
	}
	if f.WorkflowState != StatePlanning || f.CleanupState != CleanupIdle {
		t.Errorf("state = %s/%s, want planning/idle", f.WorkflowState, f.CleanupState)
	}
	if calls := e.fk.workspaces.calls; len(calls) != 1 || calls[0].Repo != testRepo || calls[0].Branch != "schmux/flight/fl-1" || calls[0].Label != "" {
		t.Errorf("workspace requests = %+v", calls)
	}
	ws := f.Workspace
	if ws == nil || ws.RequestedBranch != "schmux/flight/fl-1" || ws.Branch != "schmux/flight/fl-1-2" ||
		ws.WorkspaceID != "repo-001" || ws.Path != e.fk.workspaces.path || ws.VCS != "git" || ws.Outcome != StatusSucceeded {
		t.Errorf("workspace assignment = %+v", ws)
	}
	artifact := e.artifact("fl-1")
	if f.ArtifactPath != artifact {
		t.Errorf("ArtifactPath = %q, want %q", f.ArtifactPath, artifact)
	}
	if info, err := os.Stat(filepath.Dir(artifact)); err != nil || !info.IsDir() {
		t.Errorf("artifact directory not created: %v", err)
	}
	if e.fk.spawner.callCount() != 1 {
		t.Fatalf("spawns = %d, want 1", e.fk.spawner.callCount())
	}
	spawn := e.fk.spawner.last()
	if spawn.WorkspaceID != "repo-001" || spawn.TargetName != "planner" || spawn.Kind != state.SessionKindChat || spawn.Fence {
		t.Errorf("planner spawn = %+v", spawn)
	}
	if spawn.Prompt != PlannerPrompt(created.Prompt, artifact) {
		t.Errorf("planner prompt = %q", spawn.Prompt)
	}
	planner := sessionFor(t, f, RolePlanner)
	if planner.SessionID != "sess-1" || planner.Target != "planner" || planner.Outcome != StatusSucceeded || planner.Error != "" {
		t.Errorf("planner assignment = %+v", planner)
	}
	op := lastOp(t, f, ActionTakeoff)
	if op.Status != StatusSucceeded || op.CorrelationID == "" || len(op.Resources) != 2 {
		t.Errorf("takeoff operation = %+v", op)
	}
	if reloaded, _ := e.reload(t).Get(f.ID); reloaded.WorkflowState != StatePlanning || sessionFor(t, reloaded, RolePlanner).SessionID != "sess-1" {
		t.Errorf("takeoff not persisted: %+v", reloaded)
	}
	if got := e.m.List(); len(got) != 1 || got[0].ID != "fl-1" {
		t.Errorf("List() = %v", ids(got))
	}
}

func TestPlannerAndExecutorHonorDefaultFence(t *testing.T) {
	for _, tt := range []struct {
		name      string
		mode      string
		installed bool
		want      bool
	}{
		{"default on", config.FenceModeOptionalOn, true, true},
		{"default off", config.FenceModeOptionalOff, true, false},
		{"disabled", config.FenceModeDisabled, true, false},
		{"not installed", config.FenceModeOptionalOn, false, false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			e := newEnv(t)
			e.fk.settings.fenceMode = tt.mode
			e.fk.settings.fenceInstalled = tt.installed
			planned := takeoff(t, e)
			if got := e.fk.spawner.last().Fence; got != tt.want {
				t.Fatalf("planner Fence = %v, want %v", got, tt.want)
			}
			writePlan(t, planned.ArtifactPath, "# plan\n")
			if _, err := e.m.EnterExecution(bg, planned.ID); err != nil {
				t.Fatal(err)
			}
			if got := e.fk.spawner.last().Fence; got != tt.want {
				t.Fatalf("executor Fence = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestTakeoff_SaplingRepoDoesNotRequestABranch(t *testing.T) {
	e := newEnv(t)
	e.fk.repos[testRepo] = config.Repo{Name: "r", URL: testRepo, VCS: "sapling"}
	e.fk.workspaces.vcs = "sapling"

	req := goodCreate()
	created := mustCreate(t, e.m, req)
	if err := e.m.editFlight(created.ID, func(f *Flight) error { f.ShortName = "Fix login"; return nil }); err != nil {
		t.Fatal(err)
	}
	f, err := e.m.Takeoff(bg, created.ID)
	if err != nil {
		t.Fatal(err)
	}

	if calls := e.fk.workspaces.calls; len(calls) != 1 || calls[0].Branch != "" || calls[0].Label != "Fix login" {
		t.Errorf("workspace requests = %+v, want an empty branch and display label for sapling", calls)
	}
	if want := filepath.Join(e.fk.workspaces.path, ".sl", "schmux", "flights", f.ID, "plan.md"); f.ArtifactPath != want {
		t.Errorf("ArtifactPath = %q, want %q", f.ArtifactPath, want)
	}
}

func TestTakeoff_WorkspaceFailureStaysInBacklogAndRestartsOnRetry(t *testing.T) {
	e := newEnv(t)
	e.fk.workspaces.errs = []error{errors.New("clone failed")}
	created := mustCreate(t, e.m, goodCreate())

	_, err := e.m.Takeoff(bg, created.ID)

	if err == nil || !strings.Contains(err.Error(), "clone failed") {
		t.Fatalf("Takeoff() error = %v, want clone failure", err)
	}
	got, _ := e.m.Get(created.ID)
	if got.WorkflowState != StateBacklog || got.Workspace != nil || len(got.Sessions) != 0 {
		t.Errorf("flight after failed workspace = %+v", got)
	}
	if op := lastOp(t, got, ActionTakeoff); op.Status != StatusFailed || !strings.Contains(op.Error, "clone failed") {
		t.Errorf("takeoff operation = %+v", op)
	}
	if e.fk.spawner.callCount() != 0 {
		t.Error("planner spawned without a workspace")
	}
	// Still a backlog flight: editable, and a retry starts over.
	prompt := "edited after failure"
	if _, err := e.m.Update(bg, created.ID, UpdateRequest{Prompt: &prompt}); err != nil {
		t.Errorf("Update() after failed takeoff error = %v", err)
	}
	retried, err := e.m.Takeoff(bg, created.ID)
	if err != nil {
		t.Fatalf("retry Takeoff() error = %v", err)
	}
	if retried.WorkflowState != StatePlanning || e.fk.workspaces.callCount() != 2 {
		t.Errorf("retry: state %s, workspace requests %d", retried.WorkflowState, e.fk.workspaces.callCount())
	}
}

func TestTakeoff_PlannerFailureKeepsWorkspaceAndRetryReusesIt(t *testing.T) {
	e := newEnv(t)
	e.fk.spawner.errs = []error{errors.New("tmux exploded")}
	created := mustCreate(t, e.m, goodCreate())

	_, err := e.m.Takeoff(bg, created.ID)

	if err == nil || !strings.Contains(err.Error(), "tmux exploded") {
		t.Fatalf("Takeoff() error = %v", err)
	}
	partial, _ := e.m.Get(created.ID)
	if partial.WorkflowState != StatePlanning || partial.Workspace == nil || partial.Workspace.Outcome != StatusSucceeded {
		t.Fatalf("partial takeoff = %+v", partial)
	}
	failed := sessionFor(t, partial, RolePlanner)
	if failed.Outcome != StatusFailed || !strings.Contains(failed.Error, "tmux exploded") || failed.SessionID != "" {
		t.Errorf("planner assignment after failure = %+v", failed)
	}
	if op := lastOp(t, partial, ActionTakeoff); op.Status != StatusFailed {
		t.Errorf("takeoff operation = %+v", op)
	}
	// Editing the prompt leaves the workspace available for the retry.
	prompt := "x"
	if _, err := e.m.Update(bg, created.ID, UpdateRequest{Prompt: &prompt}); err != nil {
		t.Errorf("Update() in planning error = %v", err)
	}

	retried, err := e.m.Takeoff(bg, created.ID)

	if err != nil {
		t.Fatalf("retry Takeoff() error = %v", err)
	}
	if e.fk.workspaces.callCount() != 2 {
		t.Errorf("workspace requests = %d, want the manager called twice (idempotent re-allocation)", e.fk.workspaces.callCount())
	}
	if e.fk.spawner.callCount() != 2 || e.fk.spawner.last().WorkspaceID != "repo-001" {
		t.Errorf("planner spawns = %d (last %+v)", e.fk.spawner.callCount(), e.fk.spawner.last())
	}
	planner := sessionFor(t, retried, RolePlanner)
	if planner.ID != failed.ID || planner.Outcome != StatusSucceeded || planner.SessionID != "sess-1" || planner.Error != "" {
		t.Errorf("planner assignment after retry = %+v (was %+v)", planner, failed)
	}
	if len(retried.Sessions) != 1 || retried.Workspace.ID != partial.Workspace.ID {
		t.Errorf("retry duplicated assignments: %+v", retried)
	}
}

func TestTakeoff_RecordedWorkspaceGoneAllocatesAgain(t *testing.T) {
	e := newEnv(t)
	e.fk.workspaces.branchSuffix = "-2" // the workspace manager chose a unique branch
	created := mustCreate(t, e.m, goodCreate())

	// First takeoff succeeds, recording the originally allocated workspace.
	if _, err := e.m.Takeoff(bg, created.ID); err != nil {
		t.Fatalf("Takeoff() error = %v", err)
	}
	// Workspace manager then "forgets" the workspace it returned, and the spawner
	// refuses to spawn into that gone workspace. A retry must re-ask the manager
	// and use whatever it returns next.
	e.fk.workspaces.nextID = "repo-002"
	e.fk.spawner.refuseIDs["repo-001"] = errors.New("workspace not found: repo-001")

	got, err := e.m.Takeoff(bg, created.ID)

	if err != nil {
		t.Fatalf("Takeoff() retry error = %v", err)
	}
	if got.Workspace == nil || got.Workspace.WorkspaceID != "repo-002" {
		t.Errorf("workspace = %+v, want repo-002", got.Workspace)
	}
	if got.WorkflowState != StatePlanning {
		t.Errorf("workflow = %s, want planning", got.WorkflowState)
	}
	planner := sessionFor(t, got, RolePlanner)
	if planner.Outcome != StatusSucceeded || planner.SessionID == "" {
		t.Errorf("planner = %+v, want succeeded with a session ID", planner)
	}
	op := lastOp(t, got, ActionTakeoff)
	if op.Status != StatusSucceeded {
		t.Errorf("takeoff operation = %+v, want succeeded", op)
	}
}

func TestTakeoff_BacklogWithAssignmentMovesToPlanning(t *testing.T) {
	e := newEnv(t)
	e.fk.workspaces.branchSuffix = "-2" // match the manager's branch-collision suffix
	created := mustCreate(t, e.m, goodCreate())

	if _, err := e.m.Takeoff(bg, created.ID); err != nil {
		t.Fatalf("Takeoff() error = %v", err)
	}
	if _, err := e.m.Update(bg, created.ID, UpdateRequest{WorkflowState: ptrWorkflowState(StateBacklog)}); err != nil {
		t.Fatalf("Update(backlog) error = %v", err)
	}

	got, err := e.m.Takeoff(bg, created.ID)

	if err != nil {
		t.Fatalf("Takeoff() after stage-backlog error = %v", err)
	}
	if got.WorkflowState != StatePlanning {
		t.Errorf("workflow = %s, want planning", got.WorkflowState)
	}
	if got.Workspace == nil {
		t.Fatal("workspace unassigned after retry")
	}
	if e.fk.workspaces.callCount() != 2 {
		t.Errorf("workspace requests = %d, want 2 (takeoff calls the manager every time)", e.fk.workspaces.callCount())
	}
	planner := sessionFor(t, got, RolePlanner)
	if planner.Outcome != StatusSucceeded || planner.SessionID == "" {
		t.Errorf("planner = %+v, want succeeded with a session ID", planner)
	}
}

func ptrWorkflowState(s WorkflowState) *WorkflowState { return &s }

// startedFlightNoWorkspace returns a flight in the requested workflow state but
// without a workspace assignment; the spec allows external stage edits that
// leave a started flight with no recorded workspace, and every later
// operation must surface that as an error rather than panic.
func startedFlightNoWorkspace(id string, state WorkflowState) Flight {
	f := backlogFlight(id, 0)
	f.WorkflowState = state
	return f
}

func TestOperationsWithoutWorkspaceReturnConflict(t *testing.T) {
	// Plan: seed flights in planning, executing, completing with Workspace == nil,
	// call the operation appropriate to each state, and ensure none panic and
	// each surfaces ErrConflict with a "no workspace" message.
	//
	// Dispose's contract is different: a flight with no workspace removes its
	// record at once. Workflow operations (EnterExecution, EnterCompletion)
	// still require a workspace and surface that as ErrConflict.

	t.Run("planning/EnterExecution", func(t *testing.T) {
		f := startedFlightNoWorkspace("fl-pe", StatePlanning)
		f.Sessions = []SessionAssignment{{
			ID: "sa-1", Role: RolePlanner, Target: "planner",
			SessionID: "sess-1", Outcome: StatusSucceeded, CreatedAt: t0, UpdatedAt: t0,
		}}
		m, _ := seedStarted(t, f)

		var execErr error
		func() {
			defer func() {
				if r := recover(); r != nil {
					t.Fatalf("EnterExecution panicked: %v", r)
				}
			}()
			_, execErr = m.EnterExecution(bg, "fl-pe")
		}()
		if !errors.Is(execErr, ErrConflict) || !strings.Contains(execErr.Error(), "no workspace") {
			t.Errorf("EnterExecution error = %v, want ErrConflict mentioning no workspace", execErr)
		}
	})

	t.Run("executing/EnterCompletion", func(t *testing.T) {
		f := startedFlightNoWorkspace("fl-ec", StateExecuting)
		m, _ := seedStarted(t, f)

		var completeErr error
		func() {
			defer func() {
				if r := recover(); r != nil {
					t.Fatalf("EnterCompletion panicked: %v", r)
				}
			}()
			_, completeErr = m.EnterCompletion(bg, "fl-ec")
		}()
		if !errors.Is(completeErr, ErrConflict) || !strings.Contains(completeErr.Error(), "no workspace") {
			t.Errorf("EnterCompletion error = %v, want ErrConflict mentioning no workspace", completeErr)
		}
	})
}

func TestTakeoff_RejectsOtherStates(t *testing.T) {
	e := newEnv(t)
	if _, err := e.m.Takeoff(bg, "fl-404"); !errors.Is(err, ErrNotFound) {
		t.Errorf("Takeoff(unknown) error = %v, want ErrNotFound", err)
	}
	for _, st := range []WorkflowState{StateExecuting, StateCompleting, StateDone, StateCancelled} {
		m, _ := seedStarted(t, startedFlight("fl-s", st))
		if _, err := m.Takeoff(bg, "fl-s"); !errors.Is(err, ErrConflict) {
			t.Errorf("Takeoff() from %s error = %v, want ErrConflict", st, err)
		}
	}
}

func TestTransitionsAreRejectedWhileDisposing(t *testing.T) {
	f := startedFlight("fl-s", StatePlanning)
	f.CleanupState = CleanupDisposing
	m, _ := seedStarted(t, f)

	if _, err := m.Takeoff(bg, "fl-s"); !errors.Is(err, ErrConflict) {
		t.Errorf("Takeoff() error = %v", err)
	}
	if _, err := m.EnterExecution(bg, "fl-s"); !errors.Is(err, ErrConflict) {
		t.Errorf("EnterExecution() error = %v", err)
	}
	if _, err := m.Cancel(bg, "fl-s", "why"); !errors.Is(err, ErrConflict) {
		t.Errorf("Cancel() error = %v", err)
	}
}

func TestEnterExecution_StartsExecutorFromTheArtifact(t *testing.T) {
	e := newEnv(t)
	planned := takeoff(t, e)
	writePlan(t, planned.ArtifactPath, "# plan\n1. do it\n")

	f, err := e.m.EnterExecution(bg, planned.ID)

	if err != nil {
		t.Fatalf("EnterExecution() error = %v", err)
	}
	if f.WorkflowState != StateExecuting {
		t.Errorf("state = %s, want executing", f.WorkflowState)
	}
	spawn := e.fk.spawner.last()
	if e.fk.spawner.callCount() != 2 || spawn.WorkspaceID != "repo-001" || spawn.TargetName != "executor" || spawn.Kind != state.SessionKindChat {
		t.Errorf("executor spawn = %+v (calls %d)", spawn, e.fk.spawner.callCount())
	}
	if spawn.Prompt != ExecutorPrompt(goodCreate().Prompt, planned.ArtifactPath) {
		t.Errorf("executor prompt = %q", spawn.Prompt)
	}
	exec := sessionFor(t, f, RoleExecutor)
	if exec.SessionID != "sess-2" || exec.Outcome != StatusSucceeded {
		t.Errorf("executor assignment = %+v", exec)
	}
	if planner := sessionFor(t, f, RolePlanner); planner.SessionID != "sess-1" {
		t.Errorf("planner assignment disturbed: %+v", planner)
	}
	if op := lastOp(t, f, ActionExecution); op.Status != StatusSucceeded {
		t.Errorf("execution operation = %+v", op)
	}
}

func TestEnterExecution_ArtifactProblemsLeaveFlightInPlanning(t *testing.T) {
	tests := []struct {
		name  string
		setup func(t *testing.T, path string)
		want  string
	}{
		{"missing", func(t *testing.T, path string) {}, "plan.md"},
		{"empty", func(t *testing.T, path string) { writePlan(t, path, "") }, "empty"},
		{"a directory", func(t *testing.T, path string) {
			if err := os.Mkdir(path, 0o755); err != nil {
				t.Fatal(err)
			}
		}, "plan.md"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := newEnv(t)
			planned := takeoff(t, e)
			tt.setup(t, planned.ArtifactPath)

			_, err := e.m.EnterExecution(bg, planned.ID)

			if !errors.Is(err, ErrConflict) || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("EnterExecution() error = %v, want ErrConflict mentioning %q", err, tt.want)
			}
			got, _ := e.m.Get(planned.ID)
			if got.WorkflowState != StatePlanning {
				t.Errorf("state = %s, want planning", got.WorkflowState)
			}
			if e.fk.spawner.callCount() != 1 {
				t.Errorf("executor spawned despite artifact problem")
			}
			if op := lastOp(t, got, ActionExecution); op.Status != StatusFailed || op.Error == "" {
				t.Errorf("execution operation = %+v", op)
			}
			if len(got.Sessions) != 1 {
				t.Errorf("executor assignment created without a spawn: %+v", got.Sessions)
			}
		})
	}
}

func TestEnterExecution_RequiresASuccessfulPlanner(t *testing.T) {
	e := newEnv(t)
	e.fk.spawner.errs = []error{errors.New("tmux exploded")}
	created := mustCreate(t, e.m, goodCreate())
	if _, err := e.m.Takeoff(bg, created.ID); err == nil {
		t.Fatal("setup: Takeoff() should have failed")
	}
	writePlan(t, e.artifact(created.ID), "# plan")

	_, err := e.m.EnterExecution(bg, created.ID)

	if !errors.Is(err, ErrConflict) || !strings.Contains(err.Error(), "planner") {
		t.Fatalf("EnterExecution() error = %v, want ErrConflict about the planner", err)
	}
}

func TestEnterExecution_SpawnFailureIsRetryable(t *testing.T) {
	e := newEnv(t)
	planned := takeoff(t, e)
	writePlan(t, planned.ArtifactPath, "# plan")
	e.fk.spawner.errs = []error{errors.New("no capacity")}

	_, err := e.m.EnterExecution(bg, planned.ID)

	if err == nil || !strings.Contains(err.Error(), "no capacity") {
		t.Fatalf("EnterExecution() error = %v", err)
	}
	got, _ := e.m.Get(planned.ID)
	failed := sessionFor(t, got, RoleExecutor)
	if got.WorkflowState != StatePlanning || failed.Outcome != StatusFailed || !strings.Contains(failed.Error, "no capacity") {
		t.Fatalf("after failure: state %s, executor %+v", got.WorkflowState, failed)
	}

	retried, err := e.m.EnterExecution(bg, planned.ID)

	if err != nil {
		t.Fatalf("retry EnterExecution() error = %v", err)
	}
	exec := sessionFor(t, retried, RoleExecutor)
	if retried.WorkflowState != StateExecuting || exec.ID != failed.ID || exec.Outcome != StatusSucceeded || exec.Error != "" {
		t.Errorf("after retry: state %s, executor %+v", retried.WorkflowState, exec)
	}
}

func TestEnterExecution_RejectsOtherStates(t *testing.T) {
	for _, st := range []WorkflowState{StateBacklog, StateExecuting, StateCompleting, StateDone, StateCancelled} {
		f := startedFlight("fl-s", st)
		if st == StateBacklog {
			f = backlogFlight("fl-s", 0)
		}
		m, _ := seedStarted(t, f)
		if _, err := m.EnterExecution(bg, "fl-s"); !errors.Is(err, ErrConflict) {
			t.Errorf("EnterExecution() from %s error = %v, want ErrConflict", st, err)
		}
	}
	e := newEnv(t)
	if _, err := e.m.EnterExecution(bg, "fl-404"); !errors.Is(err, ErrNotFound) {
		t.Errorf("EnterExecution(unknown) error = %v, want ErrNotFound", err)
	}
}

func TestEnterCompletion_StartsAnAgentRunningTheCommitSkill(t *testing.T) {
	e := newEnv(t)
	executing := execute(t, e)

	f, err := e.m.EnterCompletion(bg, executing.ID)

	if err != nil {
		t.Fatalf("EnterCompletion() error = %v", err)
	}
	if f.WorkflowState != StateCompleting {
		t.Errorf("state = %s, want completing", f.WorkflowState)
	}
	spawn := e.fk.spawner.last()
	// The commit agent is the executor's harness, in the flight's workspace,
	// told to run the repository's commit skill. A chat session, like the
	// planner and executor.
	if spawn.WorkspaceID != "repo-001" || spawn.TargetName != executing.ExecutorModel || spawn.Kind != state.SessionKindChat || spawn.Nickname != "commit" {
		t.Errorf("commit spawn = %+v", spawn)
	}
	if spawn.Prompt != CommitPrompt {
		t.Errorf("commit prompt = %q, want %q", spawn.Prompt, CommitPrompt)
	}
	commit := sessionFor(t, f, RoleCommit)
	if commit.SessionID != "sess-3" || commit.Target != executing.ExecutorModel || commit.Outcome != StatusSucceeded {
		t.Errorf("commit assignment = %+v", commit)
	}
	if op := lastOp(t, f, ActionCompletion); op.Status != StatusSucceeded {
		t.Errorf("completion operation = %+v", op)
	}
}

func TestEnterCompletion_HonorsDefaultFenceLikePlannerAndExecutor(t *testing.T) {
	for _, tt := range []struct {
		name      string
		mode      string
		installed bool
		want      bool
	}{
		{"default on", config.FenceModeOptionalOn, true, true},
		{"default off", config.FenceModeOptionalOff, true, false},
		{"disabled", config.FenceModeDisabled, true, false},
		{"not installed", config.FenceModeOptionalOn, false, false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			e := newEnv(t)
			executing := execute(t, e)
			e.fk.settings.fenceMode = tt.mode
			e.fk.settings.fenceInstalled = tt.installed

			if _, err := e.m.EnterCompletion(bg, executing.ID); err != nil {
				t.Fatalf("EnterCompletion() error = %v", err)
			}
			if got := e.fk.spawner.last().Fence; got != tt.want {
				t.Errorf("commit spawn Fence = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestEnterCompletion_SpawnFailureIsRetryable(t *testing.T) {
	e := newEnv(t)
	executing := execute(t, e)
	e.fk.spawner.errs = []error{errors.New("no capacity")}

	if _, err := e.m.EnterCompletion(bg, executing.ID); err == nil {
		t.Fatal("EnterCompletion() error = nil")
	}
	got, _ := e.m.Get(executing.ID)
	failed := sessionFor(t, got, RoleCommit)
	if got.WorkflowState != StateExecuting || failed.Outcome != StatusFailed {
		t.Fatalf("after failure: state %s, commit %+v", got.WorkflowState, failed)
	}

	retried, err := e.m.EnterCompletion(bg, executing.ID)

	if err != nil {
		t.Fatalf("retry EnterCompletion() error = %v", err)
	}
	commit := sessionFor(t, retried, RoleCommit)
	if retried.WorkflowState != StateCompleting || commit.ID != failed.ID || commit.Outcome != StatusSucceeded {
		t.Errorf("after retry: state %s, commit %+v", retried.WorkflowState, commit)
	}
}

func TestEnterCompletion_RejectsOtherStates(t *testing.T) {
	for _, st := range []WorkflowState{StateBacklog, StatePlanning, StateCompleting, StateDone, StateCancelled} {
		f := startedFlight("fl-s", st)
		if st == StateBacklog {
			f = backlogFlight("fl-s", 0)
		}
		m, _ := seedStarted(t, f)
		if _, err := m.EnterCompletion(bg, "fl-s"); !errors.Is(err, ErrConflict) {
			t.Errorf("EnterCompletion() from %s error = %v, want ErrConflict", st, err)
		}
	}
}

func TestMarkDone(t *testing.T) {
	e := newEnv(t)
	completing := complete(t, e)
	spawns, allocs := e.fk.spawner.callCount(), e.fk.workspaces.callCount()

	f, err := e.m.MarkDone(bg, completing.ID)

	if err != nil {
		t.Fatalf("MarkDone() error = %v", err)
	}
	if f.WorkflowState != StateDone || f.DoneAt == nil {
		t.Errorf("flight = %+v", f)
	}
	if op := lastOp(t, f, ActionDone); op.Status != StatusSucceeded {
		t.Errorf("done operation = %+v", op)
	}
	if e.fk.spawner.callCount() != spawns || e.fk.workspaces.callCount() != allocs {
		t.Error("MarkDone touched live resources")
	}
	if _, err := e.m.MarkDone(bg, completing.ID); !errors.Is(err, ErrConflict) {
		t.Errorf("repeat MarkDone() error = %v, want ErrConflict", err)
	}
}

func TestMarkDone_RejectsOtherStates(t *testing.T) {
	for _, st := range []WorkflowState{StateBacklog, StatePlanning, StateExecuting, StateCancelled} {
		f := startedFlight("fl-s", st)
		if st == StateBacklog {
			f = backlogFlight("fl-s", 0)
		}
		m, _ := seedStarted(t, f)
		if _, err := m.MarkDone(bg, "fl-s"); !errors.Is(err, ErrConflict) {
			t.Errorf("MarkDone() from %s error = %v, want ErrConflict", st, err)
		}
	}
}

func TestCancel(t *testing.T) {
	for _, st := range []WorkflowState{StatePlanning, StateExecuting, StateCompleting} {
		t.Run(string(st), func(t *testing.T) {
			e := newEnv(t)
			var f Flight
			switch st {
			case StatePlanning:
				f = takeoff(t, e)
			case StateExecuting:
				f = execute(t, e)
			default:
				f = complete(t, e)
			}
			spawns, allocs := e.fk.spawner.callCount(), e.fk.workspaces.callCount()

			got, err := e.m.Cancel(bg, f.ID, "  changed my mind  ")

			if err != nil {
				t.Fatalf("Cancel() error = %v", err)
			}
			if got.WorkflowState != StateCancelled || got.CancelReason != "changed my mind" || got.CancelledAt == nil {
				t.Errorf("flight = %+v", got)
			}
			if got.Workspace == nil || len(got.Sessions) != len(f.Sessions) {
				t.Errorf("cancel dropped resource assignments: %+v", got)
			}
			if e.fk.spawner.callCount() != spawns || e.fk.workspaces.callCount() != allocs {
				t.Error("Cancel touched live resources")
			}
			if op := lastOp(t, got, ActionCancel); op.Status != StatusSucceeded {
				t.Errorf("cancel operation = %+v", op)
			}
		})
	}
}

func TestCancel_AcceptsABlankReason(t *testing.T) {
	e := newEnv(t)
	planned := takeoff(t, e)
	got, err := e.m.Cancel(bg, planned.ID, "")
	if err != nil {
		t.Fatalf("Cancel(blank reason) error = %v", err)
	}
	if got.CancelReason != "" {
		t.Errorf("CancelReason = %q, want blank", got.CancelReason)
	}
}

func TestCancel_Rejections(t *testing.T) {
	for _, st := range []WorkflowState{StateBacklog, StateDone, StateCancelled} {
		f := startedFlight("fl-s", st)
		if st == StateBacklog {
			f = backlogFlight("fl-s", 0)
		}
		m, _ := seedStarted(t, f)
		if _, err := m.Cancel(bg, "fl-s", "reason"); !errors.Is(err, ErrConflict) {
			t.Errorf("Cancel() from %s error = %v, want ErrConflict", st, err)
		}
	}
}

func TestConcurrentTakeoffAllocatesOnce(t *testing.T) {
	e := newEnv(t)
	created := mustCreate(t, e.m, goodCreate())
	results := concurrentResults(t, "Takeoff", 8, func() error {
		_, err := e.m.Takeoff(bg, created.ID)
		return err
	})

	for _, err := range results {
		if err != nil && !errors.Is(err, ErrConflict) {
			t.Errorf("unexpected error %v", err)
		}
	}
	if e.fk.workspaces.callCount() == 0 || e.fk.spawner.callCount() == 0 {
		t.Errorf("no takeoffs ran: workspaces=%d spawns=%d", e.fk.workspaces.callCount(), e.fk.spawner.callCount())
	}
	got, ok := e.m.Get(created.ID)
	if !ok {
		t.Fatal("flight missing after concurrent takeoffs")
	}
	if got.Workspace == nil {
		t.Error("workspace not assigned")
	}
	planner := sessionFor(t, got, RolePlanner)
	if planner.Outcome != StatusSucceeded {
		t.Errorf("planner outcome = %s, want succeeded after concurrent takeoffs", planner.Outcome)
	}
}

func TestFullWorkflowSurvivesRestart(t *testing.T) {
	e := newEnv(t)
	completing := complete(t, e)
	if _, err := e.m.MarkDone(bg, completing.ID); err != nil {
		t.Fatal(err)
	}

	got, ok := e.reload(t).Get(completing.ID)

	if !ok || got.WorkflowState != StateDone || got.Workspace == nil || len(got.Sessions) != 3 || len(got.Operations) != 4 {
		t.Errorf("reloaded flight = %+v", got)
	}
}
