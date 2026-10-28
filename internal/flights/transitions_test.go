package flights

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/sergeknystautas/schmux/internal/config"
	"github.com/sergeknystautas/schmux/internal/state"
)

var bg = context.Background()

// gitInit makes dir a git repository so ignore checks have a repo to ask. The
// developer's global and system git config must not change the answer: the
// daemon's `git check-ignore` honours core.excludesFile, so both are turned
// off for the test process.
func gitInit(t *testing.T, dir string) {
	t.Helper()
	t.Setenv("GIT_CONFIG_GLOBAL", os.DevNull)
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	if out, err := exec.Command("git", "init", "-q", dir).CombinedOutput(); err != nil {
		t.Fatalf("git init %s: %v: %s", dir, err, out)
	}
}

// reportPlanning writes a spec and plan where the skills' defaults put them
// and records a planner turn reporting both, through the monitor's own
// report path. It returns the flight after the report is retained.
func (e *testEnv) reportPlanning(t *testing.T, f Flight) Flight {
	t.Helper()
	ws := e.fk.workspaces.path
	gitInit(t, ws)
	spec := filepath.Join(ws, "docs", "superpowers", "specs", "2026-10-19-thing-design.md")
	plan := filepath.Join(ws, "docs", "superpowers", "plans", "2026-10-19-thing.md")
	for _, p := range []string{spec, plan} {
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		writePlan(t, p, "# "+filepath.Base(p)+"\n")
	}
	return e.recordReport(t, f, "Both are written.\nSpec: "+spec+"\nPlan: "+plan)
}

// recordReport records a completed planner turn with text and runs the
// monitor's artifact check on it.
func (e *testEnv) recordReport(t *testing.T, f Flight, text string) Flight {
	t.Helper()
	planner := sessionFor(t, f, RolePlanner)
	current, _ := e.m.Get(f.ID)
	var turns int
	if current.Monitor != nil {
		turns = len(current.Monitor.Turns)
	}
	turn := PlannerTurn{SessionID: planner.SessionID, StartSeq: uint64(turns*10 + 1), EndSeq: uint64(turns*10 + 9), Outcome: "completed", Text: text}
	id, err := e.m.recordPlannerTurn(f.ID, turn)
	if err != nil {
		t.Fatal(err)
	}
	retained, ok := e.m.findMonitoredTurn(f.ID, id)
	if !ok {
		t.Fatalf("turn %s not retained", id)
	}
	e.m.recordArtifactReport(bg, f.ID, retained)
	got, _ := e.m.Get(f.ID)
	return got
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
	f := e.reportPlanning(t, takeoff(t, e))
	f, err := e.m.EnterExecution(bg, f.ID)
	if err != nil {
		t.Fatalf("EnterExecution() error = %v", err)
	}
	return f
}

// review puts an executing flight into reviewing by entering the review phase.
func review(t *testing.T, e *testEnv) Flight {
	t.Helper()
	f := execute(t, e)
	f, err := e.m.EnterReview(bg, f.ID)
	if err != nil {
		t.Fatalf("EnterReview() error = %v", err)
	}
	return f
}

func complete(t *testing.T, e *testEnv) Flight {
	t.Helper()
	f := review(t, e)
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
	// The planning skills choose where the spec and plan go; takeoff
	// neither records nor creates a location.
	if f.SpecPath != "" || f.PlanPath != "" {
		t.Errorf("spec/plan = %q/%q at takeoff, want none until the planner reports", f.SpecPath, f.PlanPath)
	}
	if _, err := os.Stat(filepath.Join(e.fk.workspaces.path, ".schmux")); !os.IsNotExist(err) {
		t.Errorf("takeoff created a schmux planning directory in the workspace: %v", err)
	}
	if e.fk.spawner.callCount() != 2 {
		t.Fatalf("spawns = %d, want 2 (pilot + planner)", e.fk.spawner.callCount())
	}
	calls := e.fk.spawner.calls
	pilot := calls[0]
	if pilot.WorkspaceID != "repo-001" || pilot.TargetName != "pilot-model" || pilot.Kind != state.SessionKindChat || pilot.Nickname != "pilot" {
		t.Errorf("pilot spawn = %+v", pilot)
	}
	if pilot.Prompt != PilotPrompt(f) {
		t.Errorf("pilot prompt = %q, want PilotPrompt of the flight after takeoff", pilot.Prompt)
	}
	spawn := calls[1]
	if spawn.WorkspaceID != "repo-001" || spawn.TargetName != "planner" || spawn.Kind != state.SessionKindChat || spawn.Fence {
		t.Errorf("planner spawn = %+v", spawn)
	}
	if spawn.Prompt != PlannerPrompt(created.Prompt) {
		t.Errorf("planner prompt = %q", spawn.Prompt)
	}
	if !strings.Contains(spawn.Prompt, "[Pilot]") {
		t.Errorf("planner prompt missing [Pilot] attribution: %q", spawn.Prompt)
	}
	pilotAssign := sessionFor(t, f, RolePilot)
	if pilotAssign.SessionID != "sess-1" || pilotAssign.Target != "pilot-model" || pilotAssign.Outcome != StatusSucceeded || pilotAssign.Error != "" {
		t.Errorf("pilot assignment = %+v", pilotAssign)
	}
	planner := sessionFor(t, f, RolePlanner)
	if planner.SessionID != "sess-2" || planner.Target != "planner" || planner.Outcome != StatusSucceeded || planner.Error != "" {
		t.Errorf("planner assignment = %+v", planner)
	}
	op := lastOp(t, f, ActionTakeoff)
	if op.Status != StatusSucceeded || op.CorrelationID == "" || len(op.Resources) != 3 {
		t.Errorf("takeoff operation = %+v", op)
	}
	if reloaded, _ := e.reload(t).Get(f.ID); reloaded.WorkflowState != StatePlanning || sessionFor(t, reloaded, RolePlanner).SessionID != "sess-2" {
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
			e.reportPlanning(t, planned)
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
	if _, err := e.m.Takeoff(bg, created.ID); err != nil {
		t.Fatal(err)
	}

	if calls := e.fk.workspaces.calls; len(calls) != 1 || calls[0].Branch != "" || calls[0].Label != "Fix login" {
		t.Errorf("workspace requests = %+v, want an empty branch and display label for sapling", calls)
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

func TestTakeoff_PlannerFailureKeepsWorkspaceAndRejectsAnotherTakeoff(t *testing.T) {
	e := newEnv(t)
	// Pilot succeeds, planner fails. Two spawns are required: the error
	// sits at index 1 so the pilot call (index 0) succeeds.
	e.fk.spawner.errs = []error{nil, errors.New("tmux exploded")}
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
	_, err = e.m.Takeoff(bg, created.ID)
	if !errors.Is(err, ErrConflict) {
		t.Fatalf("second Takeoff() error = %v, want conflict", err)
	}
	after, _ := e.m.Get(created.ID)
	if !reflect.DeepEqual(after, partial) || e.fk.workspaces.callCount() != 1 || e.fk.spawner.callCount() != 2 {
		t.Fatalf("rejected takeoff changed flight or spawned resources: before=%+v after=%+v workspaces=%d spawns=%d", partial, after, e.fk.workspaces.callCount(), e.fk.spawner.callCount())
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

// The pilot model is flight-manager configuration. Without a usable one,
// takeoff is refused before any operation, workspace, or session exists.
func TestTakeoffRequiresConfiguredChatPilot(t *testing.T) {
	for _, tc := range []struct {
		name, target, wantErr string
	}{
		{"unset", "", "Settings > Flights > Pilot Supervision"},
		{"not chat", "no-chat", "has no chat mode"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			e := newEnv(t)
			e.fk.pilot.set(tc.target)
			created := mustCreate(t, e.m, goodCreate())

			_, err := e.m.Takeoff(bg, created.ID)
			if !errors.Is(err, ErrConflict) || !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("Takeoff() error = %v, want conflict containing %q", err, tc.wantErr)
			}
			if n := len(e.fk.workspaces.calls); n != 0 {
				t.Errorf("workspace requests = %d, want 0", n)
			}
			if n := e.fk.spawner.callCount(); n != 0 {
				t.Errorf("spawns = %d, want 0", n)
			}
			got, _ := e.m.Get(created.ID)
			if got.WorkflowState != StateBacklog || len(got.Operations) != 0 || len(got.Sessions) != 0 {
				t.Errorf("flight after refused takeoff = %+v, want untouched backlog", got)
			}
		})
	}
}

func TestTakeoff_PilotFailureDoesNotSpawnPlanner(t *testing.T) {
	e := newEnv(t)
	// Pilot fails immediately, so the planner must not be started: an
	// unsupervised planner violates the spec.
	e.fk.spawner.errs = []error{errors.New("pilot boom")}
	created := mustCreate(t, e.m, goodCreate())

	_, err := e.m.Takeoff(bg, created.ID)
	if err == nil || !strings.Contains(err.Error(), "pilot boom") {
		t.Fatalf("Takeoff() error = %v, want pilot boom", err)
	}
	if e.fk.spawner.callCount() != 1 {
		t.Errorf("spawns = %d, want 1 (pilot only)", e.fk.spawner.callCount())
	}
	got, _ := e.m.Get(created.ID)
	if got.Sessions == nil || len(got.Sessions) != 1 || got.Sessions[0].Role != RolePilot {
		t.Errorf("after pilot failure, sessions = %+v, want a single pilot record", got.Sessions)
	}
	if got.Sessions[0].Outcome != StatusFailed {
		t.Errorf("pilot outcome = %s, want failed", got.Sessions[0].Outcome)
	}
}

func TestTakeoffRejectsPlanningWithoutAllocatingOrSpawning(t *testing.T) {
	for _, name := range []string{"running pilot", "missing workspace", "pilot in another workspace"} {
		t.Run(name, func(t *testing.T) {
			e := newEnv(t)
			f := takeoff(t, e)
			pilot := sessionFor(t, f, RolePilot)
			e.fk.chats.set(pilot.SessionID, true)
			e.fk.workspaces.nextID = "repo-002"
			if name == "missing workspace" {
				e.fk.spawner.refuseIDs["repo-001"] = errors.New("workspace not found: repo-001")
			}
			if name == "pilot in another workspace" {
				e.fk.chats.workspaces = map[string]string{pilot.SessionID: "other-workspace"}
			}
			if _, err := e.m.Takeoff(bg, f.ID); !errors.Is(err, ErrConflict) {
				t.Fatalf("second Takeoff() error=%v, want conflict", err)
			}
			after, _ := e.m.Get(f.ID)
			if !reflect.DeepEqual(after, f) || e.fk.workspaces.callCount() != 1 || e.fk.spawner.callCount() != 2 {
				t.Fatalf("rejected takeoff changed flight or spawned resources: before=%+v after=%+v workspaces=%d spawns=%d", f, after, e.fk.workspaces.callCount(), e.fk.spawner.callCount())
			}
		})
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

	t.Run("reviewing/EnterCompletion", func(t *testing.T) {
		f := startedFlightNoWorkspace("fl-ec", StateReviewing)
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

	t.Run("executing/EnterReview", func(t *testing.T) {
		f := startedFlightNoWorkspace("fl-er", StateExecuting)
		m, _ := seedStarted(t, f)

		var reviewErr error
		func() {
			defer func() {
				if r := recover(); r != nil {
					t.Fatalf("EnterReview panicked: %v", r)
				}
			}()
			_, reviewErr = m.EnterReview(bg, "fl-er")
		}()
		if !errors.Is(reviewErr, ErrConflict) || !strings.Contains(reviewErr.Error(), "no workspace") {
			t.Errorf("EnterReview error = %v, want ErrConflict mentioning no workspace", reviewErr)
		}
	})
}

func TestTakeoff_RejectsOtherStates(t *testing.T) {
	e := newEnv(t)
	if _, err := e.m.Takeoff(bg, "fl-404"); !errors.Is(err, ErrNotFound) {
		t.Errorf("Takeoff(unknown) error = %v, want ErrNotFound", err)
	}
	for _, st := range []WorkflowState{StatePlanning, StateExecuting, StateReviewing, StateCompleting} {
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
}

func TestEnterExecution_StartsExecutorFromTheReportedSpecAndPlan(t *testing.T) {
	e := newEnv(t)
	planned := e.reportPlanning(t, takeoff(t, e))
	if planned.SpecPath == "" || planned.PlanPath == "" {
		t.Fatalf("setup: report not verified: %+v", planned.Monitor)
	}

	f, err := e.m.EnterExecution(bg, planned.ID)

	if err != nil {
		t.Fatalf("EnterExecution() error = %v", err)
	}
	if f.WorkflowState != StateExecuting {
		t.Errorf("state = %s, want executing", f.WorkflowState)
	}
	spawn := e.fk.spawner.last()
	if e.fk.spawner.callCount() != 3 || spawn.WorkspaceID != "repo-001" || spawn.TargetName != "executor" || spawn.Kind != state.SessionKindChat {
		t.Errorf("executor spawn = %+v (calls %d)", spawn, e.fk.spawner.callCount())
	}
	wantReport := strings.TrimSuffix(planned.PlanPath, ".md") + "-execution.md"
	if f.ReportTarget != wantReport || f.ReportPath != "" {
		t.Errorf("report target/path = %q/%q, want %q and no verified path", f.ReportTarget, f.ReportPath, wantReport)
	}
	if spawn.Prompt != ExecutorPrompt(goodCreate().Prompt, planned.SpecPath, planned.PlanPath, wantReport) || !strings.Contains(spawn.Prompt, wantReport) {
		t.Errorf("executor prompt = %q", spawn.Prompt)
	}
	exec := sessionFor(t, f, RoleExecutor)
	if exec.SessionID != "sess-3" || exec.Outcome != StatusSucceeded {
		t.Errorf("executor assignment = %+v", exec)
	}
	if f.ExecutionStartedAt == nil || f.ExecutionStartedAt.After(exec.CreatedAt) {
		t.Errorf("execution started at %v, want a time not after the executor assignment at %v", f.ExecutionStartedAt, exec.CreatedAt)
	}
	var notices []FlightMessage
	for _, msg := range f.Monitor.Messages {
		if msg.Kind == "execution_notice" {
			notices = append(notices, msg)
		}
	}
	if len(notices) != 1 || notices[0].RecipientSessionID != sessionFor(t, f, RolePilot).SessionID ||
		!strings.Contains(notices[0].Text, exec.SessionID) || !strings.Contains(notices[0].Text, wantReport) {
		t.Errorf("execution notices = %+v, want one to the pilot naming the executor session and report", notices)
	}
	if planner := sessionFor(t, f, RolePlanner); planner.SessionID != "sess-2" {
		t.Errorf("planner assignment disturbed: %+v", planner)
	}
	if op := lastOp(t, f, ActionExecution); op.Status != StatusSucceeded {
		t.Errorf("execution operation = %+v", op)
	}
}

// The Spec:/Plan: report is information for the executor, not a gate: the
// user may enter execution without a verified report, and nothing is
// rechecked when one exists.
func TestEnterExecution_DoesNotGateOnTheReport(t *testing.T) {
	t.Run("never reported", func(t *testing.T) {
		e := newEnv(t)
		planned := takeoff(t, e)

		f, err := e.m.EnterExecution(bg, planned.ID)

		if err != nil {
			t.Fatalf("EnterExecution() error = %v", err)
		}
		if f.WorkflowState != StateExecuting || e.fk.spawner.callCount() != 3 {
			t.Fatalf("state = %s, spawns = %d; want executing with the executor spawned", f.WorkflowState, e.fk.spawner.callCount())
		}
		wantReport := filepath.Join(f.Workspace.Path, "review", f.ID+"-execution.md")
		if f.ReportTarget != wantReport {
			t.Errorf("report target = %q, want the review/ fallback %q", f.ReportTarget, wantReport)
		}
		prompt := e.fk.spawner.last().Prompt
		for _, want := range []string{"did not report a verified spec and plan", "Spec: (none recorded", "Plan: (none recorded", "Report: " + wantReport} {
			if !strings.Contains(prompt, want) {
				t.Errorf("executor prompt lacks %q:\n%s", want, prompt)
			}
		}
	})
	t.Run("plan emptied after its report", func(t *testing.T) {
		e := newEnv(t)
		reported := e.reportPlanning(t, takeoff(t, e))
		writePlan(t, reported.PlanPath, "")

		f, err := e.m.EnterExecution(bg, reported.ID)

		if err != nil {
			t.Fatalf("EnterExecution() error = %v", err)
		}
		if f.WorkflowState != StateExecuting || !strings.Contains(e.fk.spawner.last().Prompt, "Plan: "+reported.PlanPath) {
			t.Errorf("state = %s; the executor prompt should carry the recorded plan path unchecked", f.WorkflowState)
		}
	})
}

func TestEnterExecution_RequiresASuccessfulPlanner(t *testing.T) {
	e := newEnv(t)
	e.fk.spawner.errs = []error{errors.New("tmux exploded")}
	created := mustCreate(t, e.m, goodCreate())
	if _, err := e.m.Takeoff(bg, created.ID); err == nil {
		t.Fatal("setup: Takeoff() should have failed")
	}

	_, err := e.m.EnterExecution(bg, created.ID)

	if !errors.Is(err, ErrConflict) || !strings.Contains(err.Error(), "planner") {
		t.Fatalf("EnterExecution() error = %v, want ErrConflict about the planner", err)
	}
}

func TestEnterExecution_SpawnFailureIsRetryable(t *testing.T) {
	e := newEnv(t)
	planned := e.reportPlanning(t, takeoff(t, e))
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
	if got.ExecutionStartedAt != nil || got.ReportTarget != "" {
		t.Errorf("failed spawn recorded execution start %v / report target %q", got.ExecutionStartedAt, got.ReportTarget)
	}
	for _, msg := range got.Monitor.Messages {
		if msg.Kind == "execution_notice" {
			t.Errorf("failed spawn recorded an execution notice: %+v", msg)
		}
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
	for _, st := range []WorkflowState{StateBacklog, StateExecuting, StateReviewing, StateCompleting} {
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
	reviewing := review(t, e)

	f, err := e.m.EnterCompletion(bg, reviewing.ID)

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
	if spawn.WorkspaceID != "repo-001" || spawn.TargetName != reviewing.ExecutorModel || spawn.Kind != state.SessionKindChat || spawn.Nickname != "commit" {
		t.Errorf("commit spawn = %+v", spawn)
	}
	if spawn.Prompt != CommitterPrompt(reviewing) {
		t.Errorf("commit prompt = %q, want the composed committer prompt", spawn.Prompt)
	}
	if !strings.Contains(spawn.Prompt, reviewing.SpecPath) || !strings.Contains(spawn.Prompt, reviewing.PlanPath) {
		t.Errorf("commit prompt lacks the flight's spec or plan path: %q", spawn.Prompt)
	}
	commit := sessionFor(t, f, RoleCommit)
	if commit.SessionID != "sess-5" || commit.Target != reviewing.ExecutorModel || commit.Outcome != StatusSucceeded {
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
			reviewing := review(t, e)
			e.fk.settings.fenceMode = tt.mode
			e.fk.settings.fenceInstalled = tt.installed

			if _, err := e.m.EnterCompletion(bg, reviewing.ID); err != nil {
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
	reviewing := review(t, e)
	e.fk.spawner.errs = []error{errors.New("no capacity")}

	if _, err := e.m.EnterCompletion(bg, reviewing.ID); err == nil {
		t.Fatal("EnterCompletion() error = nil")
	}
	got, _ := e.m.Get(reviewing.ID)
	failed := sessionFor(t, got, RoleCommit)
	if got.WorkflowState != StateReviewing || failed.Outcome != StatusFailed {
		t.Fatalf("after failure: state %s, commit %+v", got.WorkflowState, failed)
	}

	retried, err := e.m.EnterCompletion(bg, reviewing.ID)

	if err != nil {
		t.Fatalf("retry EnterCompletion() error = %v", err)
	}
	commit := sessionFor(t, retried, RoleCommit)
	if retried.WorkflowState != StateCompleting || commit.ID != failed.ID || commit.Outcome != StatusSucceeded {
		t.Errorf("after retry: state %s, commit %+v", retried.WorkflowState, commit)
	}
}

func TestEnterCompletion_RejectsOtherStates(t *testing.T) {
	for _, st := range []WorkflowState{StateBacklog, StatePlanning, StateExecuting, StateCompleting} {
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

// Enter review spawns a fresh reviewer with the planner's model. The
// reviewer gets the spec, the plan, and the verified report; the pilot gets
// a notice naming the reviewer.
func TestEnterReview_StartsAFreshReviewerWithThePlannerModel(t *testing.T) {
	e := newEnv(t)
	executing := execute(t, e)

	f, err := e.m.EnterReview(bg, executing.ID)

	if err != nil {
		t.Fatalf("EnterReview() error = %v", err)
	}
	if f.WorkflowState != StateReviewing {
		t.Errorf("state = %s, want reviewing", f.WorkflowState)
	}
	spawn := e.fk.spawner.last()
	if e.fk.spawner.callCount() != 4 || spawn.WorkspaceID != "repo-001" || spawn.TargetName != executing.PlannerModel || spawn.Kind != state.SessionKindChat || spawn.Nickname != "reviewer" {
		t.Errorf("reviewer spawn = %+v (calls %d)", spawn, e.fk.spawner.callCount())
	}
	if !strings.Contains(spawn.Prompt, "You are the reviewer") {
		t.Errorf("reviewer prompt missing the reviewer role: %q", spawn.Prompt)
	}
	if !strings.Contains(spawn.Prompt, executing.SpecPath) || !strings.Contains(spawn.Prompt, executing.PlanPath) {
		t.Errorf("reviewer prompt missing the spec or plan path: %q", spawn.Prompt)
	}
	reviewer := sessionFor(t, f, RoleReviewer)
	if reviewer.SessionID != "sess-4" || reviewer.Target != executing.PlannerModel || reviewer.Outcome != StatusSucceeded {
		t.Errorf("reviewer assignment = %+v", reviewer)
	}
	if f.ReviewStartedAt == nil {
		t.Error("review start not recorded")
	}
	if op := lastOp(t, f, ActionReview); op.Status != StatusSucceeded {
		t.Errorf("review operation = %+v", op)
	}
	var notices []FlightMessage
	for _, msg := range f.Monitor.Messages {
		if msg.Kind == "review_notice" {
			notices = append(notices, msg)
		}
	}
	if len(notices) != 1 || notices[0].RecipientSessionID != sessionFor(t, f, RolePilot).SessionID ||
		!strings.Contains(notices[0].Text, reviewer.SessionID) || !strings.Contains(notices[0].Text, "needs no decision") {
		t.Errorf("review notices = %+v, want one to the pilot naming the reviewer", notices)
	}
	// The planner and the executor keep their assignments.
	if planner := sessionFor(t, f, RolePlanner); planner.SessionID != "sess-2" {
		t.Errorf("planner assignment disturbed: %+v", planner)
	}
	if executor := sessionFor(t, f, RoleExecutor); executor.SessionID != "sess-3" {
		t.Errorf("executor assignment disturbed: %+v", executor)
	}
}

// The report is named in the prompt only when the daemon verified it.
func TestEnterReview_UnverifiedReportIsNotRequiredAndNotNamed(t *testing.T) {
	e := newEnv(t)
	executing := execute(t, e)
	if executing.ReportPath != "" {
		t.Fatalf("setup: report already verified: %q", executing.ReportPath)
	}

	f, err := e.m.EnterReview(bg, executing.ID)

	if err != nil {
		t.Fatalf("EnterReview() error = %v", err)
	}
	if f.WorkflowState != StateReviewing {
		t.Errorf("state = %s, want reviewing", f.WorkflowState)
	}
	prompt := e.fk.spawner.last().Prompt
	if !strings.Contains(prompt, "no verified execution report exists") {
		t.Errorf("reviewer prompt does not say no report was verified: %q", prompt)
	}
}

// A flight that already reviewed once keeps the earlier reviewer session as
// history when a new reviewer takes over.
func TestEnterReview_KeepsPriorReviewerHistory(t *testing.T) {
	e := newEnv(t)
	executing := execute(t, e)
	if err := e.m.editFlight(executing.ID, func(f *Flight) error {
		f.Sessions = append(f.Sessions, SessionAssignment{
			ID: "sa-reviewer-old", Role: RoleReviewer, Target: executing.PlannerModel,
			SessionID: "sess-old-reviewer", Outcome: StatusSucceeded, CreatedAt: t0, UpdatedAt: t0,
		})
		return nil
	}); err != nil {
		t.Fatal(err)
	}

	f, err := e.m.EnterReview(bg, executing.ID)

	if err != nil {
		t.Fatalf("EnterReview() error = %v", err)
	}
	reviewer := sessionFor(t, f, RoleReviewer)
	if reviewer.SessionID != "sess-4" || len(reviewer.PriorSessionIDs) != 1 || reviewer.PriorSessionIDs[0] != "sess-old-reviewer" {
		t.Errorf("reviewer assignment = %+v, want the earlier reviewer kept as history", reviewer)
	}
}

func TestEnterReview_RejectsOtherStates(t *testing.T) {
	for _, st := range []WorkflowState{StateBacklog, StatePlanning, StateReviewing, StateCompleting} {
		f := startedFlight("fl-s", st)
		if st == StateBacklog {
			f = backlogFlight("fl-s", 0)
		}
		m, _ := seedStarted(t, f)
		if _, err := m.EnterReview(bg, "fl-s"); !errors.Is(err, ErrConflict) {
			t.Errorf("EnterReview() from %s error = %v, want ErrConflict", st, err)
		}
	}
	m, _ := seedStarted(t, startedFlight("fl-s", StateExecuting))
	if _, err := m.EnterReview(bg, "fl-404"); !errors.Is(err, ErrNotFound) {
		t.Errorf("EnterReview(unknown) error = %v, want ErrNotFound", err)
	}
}

func TestEnterReview_SpawnFailureIsRetryable(t *testing.T) {
	e := newEnv(t)
	executing := execute(t, e)
	e.fk.spawner.errs = []error{errors.New("no capacity")}

	if _, err := e.m.EnterReview(bg, executing.ID); err == nil {
		t.Fatal("EnterReview() error = nil")
	}
	got, _ := e.m.Get(executing.ID)
	failed := sessionFor(t, got, RoleReviewer)
	if got.WorkflowState != StateExecuting || failed.Outcome != StatusFailed {
		t.Fatalf("after failure: state %s, reviewer %+v", got.WorkflowState, failed)
	}
	if got.ReviewStartedAt != nil {
		t.Errorf("failed spawn recorded a review start: %v", got.ReviewStartedAt)
	}
	for _, msg := range got.Monitor.Messages {
		if msg.Kind == "review_notice" {
			t.Errorf("failed spawn recorded a review notice: %+v", msg)
		}
	}

	retried, err := e.m.EnterReview(bg, executing.ID)

	if err != nil {
		t.Fatalf("retry EnterReview() error = %v", err)
	}
	reviewer := sessionFor(t, retried, RoleReviewer)
	if retried.WorkflowState != StateReviewing || reviewer.ID != failed.ID || reviewer.Outcome != StatusSucceeded {
		t.Errorf("after retry: state %s, reviewer %+v", retried.WorkflowState, reviewer)
	}
}

func TestEnterReview_RequiresAWorkspace(t *testing.T) {
	f := startedFlightNoWorkspace("fl-rv", StateExecuting)
	m, _ := seedStarted(t, f)

	_, err := m.EnterReview(bg, "fl-rv")

	if !errors.Is(err, ErrConflict) || !strings.Contains(err.Error(), "no workspace") {
		t.Errorf("EnterReview() error = %v, want ErrConflict mentioning no workspace", err)
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

// Completing is the last phase: the commit is ready and the flight stays
// there, with everything it owns, until the user disposes it.
func TestFullWorkflowSurvivesRestart(t *testing.T) {
	e := newEnv(t)
	completing := complete(t, e)

	got, ok := e.reload(t).Get(completing.ID)

	if !ok || got.WorkflowState != StateCompleting || got.Workspace == nil || len(got.Sessions) != 5 || len(got.Operations) != 4 {
		t.Errorf("reloaded flight = %+v", got)
	}
}

func TestExecutionReportPath(t *testing.T) {
	got := executionReportPath("/ws/docs/superpowers/plans/2026-10-19-thing.md")
	if want := "/ws/docs/superpowers/plans/2026-10-19-thing-execution.md"; got != want {
		t.Errorf("executionReportPath() = %q, want %q", got, want)
	}
}
