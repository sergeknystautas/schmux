package flights

import (
	"errors"
	"strings"
	"testing"

	"github.com/sergeknystautas/schmux/internal/disposal"
)

func confirm(f Flight) DisposeConfirmation {
	return DisposeConfirmation{WorkspaceID: f.Workspace.WorkspaceID, Confirmed: true}
}

func TestDispose_CleansEverythingAndRemovesTheRecord(t *testing.T) {
	e := newEnv(t)
	executing := execute(t, e)
	// Collect the flight's session IDs (sess-1, sess-2) so the test's expectation
	// of the disposal Request includes them.
	var flightSessionIDs []string
	for _, s := range executing.Sessions {
		if s.SessionID != "" {
			flightSessionIDs = append(flightSessionIDs, s.SessionID)
		}
	}
	e.fk.cleaner.outcomes = []disposal.ResourceOutcome{
		{Kind: "session", ID: "sess-1"}, {Kind: "session", ID: "sess-2"},
		{Kind: "chat_storage", ID: "repo-001"}, {Kind: "workspace", ID: "repo-001"}, {Kind: "preview", ID: "repo-001"},
	}
	e.fk.cleaner.result = disposal.Result{SessionsDisposed: []string{"sess-1", "sess-2"}, ChatDirectoriesDeleted: len(flightSessionIDs), WorkspacePurged: true}

	got, err := e.m.Dispose(bg, executing.ID, confirm(executing))

	if err != nil {
		t.Fatalf("Dispose() error = %v", err)
	}
	if len(e.fk.cleaner.calls) != 1 {
		t.Fatalf("cleaner calls = %d, want 1", len(e.fk.cleaner.calls))
	}
	gotReq := e.fk.cleaner.calls[0]
	if gotReq.WorkspaceID != "repo-001" || !gotReq.ForcePurge ||
		gotReq.BranchRepo != testRepo || gotReq.BranchName != executing.Workspace.Branch {
		t.Errorf("cleanup request fields = %+v", gotReq)
	}
	if len(gotReq.ChatSessionIDs) != len(flightSessionIDs) {
		t.Errorf("chat session ids = %v, want %d entries", gotReq.ChatSessionIDs, len(flightSessionIDs))
	}
	gotSet := make(map[string]bool, len(gotReq.ChatSessionIDs))
	for _, sid := range gotReq.ChatSessionIDs {
		gotSet[sid] = true
	}
	for _, want := range flightSessionIDs {
		if !gotSet[want] {
			t.Errorf("cleanup request missing chat session id %q (have %v)", want, gotReq.ChatSessionIDs)
		}
	}
	if !got.Removed || len(got.Cleanup.SessionsDisposed) != 2 {
		t.Errorf("result = %+v", got)
	}
	if got.Flight.ID != executing.ID || got.Flight.CleanupState != CleanupDisposing {
		t.Errorf("result flight = %+v", got.Flight)
	}
	if op := lastOp(t, got.Flight, ActionDispose); op.Status != StatusSucceeded || len(op.Resources) != 5 {
		t.Errorf("dispose operation = %+v", op)
	}
	if _, ok := e.m.Get(executing.ID); ok {
		t.Error("record still present after successful cleanup")
	}
	if _, ok := e.reload(t).Get(executing.ID); ok {
		t.Error("record resurrected after restart")
	}
}

// TestDispose_DeletesOnlyThisFlightsConversations covers the new guarantee:
// the disposal request names only this flight's chat session IDs, even when
// the same workspace holds other sessions recorded as prior attempts.
func TestDispose_DeletesOnlyThisFlightsConversations(t *testing.T) {
	e := newEnv(t)
	f := takeoff(t, e)

	// Reload with a prior session id recorded against the planner, simulating
	// an earlier retry that left a different sessionID behind in the same workspace.
	reloaded, _ := e.m.Get(f.ID)
	reloaded.Sessions[0].PriorSessionIDs = []string{"sess-other"}
	// Update's API doesn't expose Sessions, so seed a fresh manager over a
	// snapshot that contains the modified flight.
	writeSnapshot(t, e.path, snapshot{Flights: []Flight{reloaded}})
	e.m, _ = newManagerWith(t, e.path, e.notices, e.fk)

	e.fk.cleaner.result = disposal.Result{ChatDirectoriesDeleted: 2, WorkspacePurged: true}

	if _, err := e.m.Dispose(bg, f.ID, confirm(reloaded)); err != nil {
		t.Fatalf("Dispose() error = %v", err)
	}
	gotReq := e.fk.cleaner.calls[0]
	gotSet := make(map[string]bool)
	for _, sid := range gotReq.ChatSessionIDs {
		gotSet[sid] = true
	}
	for _, want := range []string{"sess-1", "sess-other"} {
		if !gotSet[want] {
			t.Errorf("cleanup request missing chat session id %q (have %v)", want, gotReq.ChatSessionIDs)
		}
	}
	if gotSet["unrelated-session"] {
		t.Errorf("unrelated-session was in chat session ids %v", gotReq.ChatSessionIDs)
	}
}

func TestDispose_PersistsDisposingStateAndEachOutcomeBeforeCleanupFinishes(t *testing.T) {
	e := newEnv(t)
	executing := execute(t, e)
	var midCleanup Flight
	e.fk.cleaner.outcomes = []disposal.ResourceOutcome{{Kind: "session", ID: "sess-1"}, {Kind: "workspace", ID: "repo-001"}}
	e.fk.cleaner.onCall = func() {
		// What a daemon restarted right now would see on disk.
		midCleanup, _ = e.reload(t).Get(executing.ID)
	}

	if _, err := e.m.Dispose(bg, executing.ID, confirm(executing)); err != nil {
		t.Fatalf("Dispose() error = %v", err)
	}

	if midCleanup.CleanupState != CleanupDisposing || midCleanup.WorkflowState != StateExecuting {
		t.Errorf("mid-cleanup state = %s/%s, want executing/disposing", midCleanup.WorkflowState, midCleanup.CleanupState)
	}
	op := lastOp(t, midCleanup, ActionDispose)
	if op.Status != StatusPending || len(op.Resources) != 2 || op.Resources[0].Kind != "session" || op.Resources[1].Kind != "workspace" {
		t.Errorf("mid-cleanup dispose operation = %+v", op)
	}
}

func TestDispose_RejectsUnconfirmedRequestsWithoutTouchingAnything(t *testing.T) {
	tests := []struct {
		name string
		conf func(Flight) DisposeConfirmation
	}{
		{"not confirmed", func(f Flight) DisposeConfirmation {
			return DisposeConfirmation{WorkspaceID: f.Workspace.WorkspaceID, Confirmed: false}
		}},
		{"wrong workspace", func(f Flight) DisposeConfirmation {
			return DisposeConfirmation{WorkspaceID: "repo-999", Confirmed: true}
		}},
		{"missing workspace", func(f Flight) DisposeConfirmation { return DisposeConfirmation{Confirmed: true} }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := newEnv(t)
			planned := takeoff(t, e)

			_, err := e.m.Dispose(bg, planned.ID, tt.conf(planned))

			if !errors.Is(err, ErrConflict) {
				t.Fatalf("Dispose() error = %v, want ErrConflict", err)
			}
			got, _ := e.m.Get(planned.ID)
			if got.CleanupState != CleanupIdle || len(got.Operations) != len(planned.Operations) {
				t.Errorf("rejected dispose changed the flight: %+v", got)
			}
			if e.fk.cleaner.callCount() != 0 {
				t.Error("cleanup ran without confirmation")
			}
		})
	}
}

func TestDispose_RemovesAWorkspacelessFlightWithoutCleanup(t *testing.T) {
	e := newEnv(t)
	backlog := mustCreate(t, e.m, goodCreate())

	res, err := e.m.Dispose(bg, backlog.ID, DisposeConfirmation{Confirmed: true})
	if err != nil {
		t.Fatalf("Dispose() error = %v", err)
	}
	if !res.Removed {
		t.Error("Removed = false, want true")
	}
	if e.fk.cleaner.callCount() != 0 {
		t.Errorf("cleaner called %d times, want 0", e.fk.cleaner.callCount())
	}
	if _, ok := e.m.Get(backlog.ID); ok {
		t.Errorf("Get(%q) after dispose returned a flight, want ErrNotFound", backlog.ID)
	}
}

func TestDispose_WorkspacelessFlightRejectsAWorkspaceIDAndUnconfirmedRequests(t *testing.T) {
	e := newEnv(t)
	backlog := mustCreate(t, e.m, goodCreate())

	for _, conf := range []DisposeConfirmation{{Confirmed: false}, {WorkspaceID: "ws-1", Confirmed: true}} {
		if _, err := e.m.Dispose(bg, backlog.ID, conf); !errors.Is(err, ErrConflict) {
			t.Errorf("Dispose(%+v) error = %v, want ErrConflict", conf, err)
		}
		if _, ok := e.m.Get(backlog.ID); !ok {
			t.Errorf("flight removed after rejected Dispose(%+v)", conf)
		}
	}
}

func TestDispose_UnknownFlightIsNotFound(t *testing.T) {
	e := newEnv(t)
	for _, conf := range []DisposeConfirmation{{Confirmed: true}, {Confirmed: false}} {
		if _, err := e.m.Dispose(bg, "fl-missing", conf); !errors.Is(err, ErrNotFound) {
			t.Errorf("Dispose(%+v) error = %v, want ErrNotFound", conf, err)
		}
	}
}

// A started flight can hold no workspace assignment when its record was
// edited by hand; it owns nothing to clean, so Dispose removes the record.
func TestDispose_RemovesAStartedFlightWithoutAWorkspace(t *testing.T) {
	m, path := seedStarted(t, startedFlightNoWorkspace("fl-pn", StatePlanning))
	e := &testEnv{m: m, path: path, fk: newFakes(t)}

	res, err := m.Dispose(bg, "fl-pn", DisposeConfirmation{Confirmed: true})
	if err != nil {
		t.Fatalf("Dispose() error = %v", err)
	}
	if !res.Removed || res.Flight.WorkflowState != StatePlanning {
		t.Errorf("Dispose() = %+v, want removed planning flight", res)
	}
	if _, ok := e.reload(t).Get("fl-pn"); ok {
		t.Error("flight present after restart")
	}
}

func TestDispose_StampsUpdatedAtWhenCleanupStarts(t *testing.T) {
	e := newEnv(t)
	executing := execute(t, e)
	var midCleanup Flight
	e.fk.cleaner.onCall = func() { midCleanup, _ = e.reload(t).Get(executing.ID) }

	if _, err := e.m.Dispose(bg, executing.ID, confirm(executing)); err != nil {
		t.Fatalf("Dispose() error = %v", err)
	}
	if !midCleanup.UpdatedAt.After(executing.UpdatedAt) {
		t.Errorf("UpdatedAt mid-cleanup = %v, want after %v", midCleanup.UpdatedAt, executing.UpdatedAt)
	}
}

func TestDispose_AllStartedStagesCanBeDisposed(t *testing.T) {
	for _, stage := range []WorkflowState{StatePlanning, StateExecuting, StateCompleting, StateDone} {
		t.Run(string(stage), func(t *testing.T) {
			e := newEnv(t)
			var f Flight
			switch stage {
			case StatePlanning:
				f = takeoff(t, e)
			case StateExecuting:
				f = execute(t, e)
			default:
				f = complete(t, e)
				if stage == StateDone {
					var err error
					f, err = e.m.MarkDone(bg, f.ID)
					if err != nil {
						t.Fatal(err)
					}
				}
			}
			got, err := e.m.Dispose(bg, f.ID, confirm(f))
			if err != nil || !got.Removed {
				t.Fatalf("Dispose() = %+v, error = %v", got, err)
			}
			if got.Flight.WorkflowState != stage {
				t.Errorf("workflow state changed to %s", got.Flight.WorkflowState)
			}
		})
	}
}

func TestDispose_PartialFailureKeepsTheRecordAndIsRetryable(t *testing.T) {
	e := newEnv(t)
	executing := execute(t, e)
	e.fk.cleaner.outcomes = []disposal.ResourceOutcome{
		{Kind: "session", ID: "sess-1"},
		{Kind: "session", ID: "sess-2", Err: errors.New("tmux stuck")},
	}
	e.fk.cleaner.err = errors.New("dispose session sess-2: tmux stuck")

	_, err := e.m.Dispose(bg, executing.ID, confirm(executing))

	if err == nil || !strings.Contains(err.Error(), "tmux stuck") || errors.Is(err, ErrConflict) {
		t.Fatalf("Dispose() error = %v, want the cleanup failure", err)
	}
	failed, ok := e.reload(t).Get(executing.ID)
	if !ok {
		t.Fatal("record removed despite failed cleanup")
	}
	if failed.CleanupState != CleanupDisposing || failed.WorkflowState != StateExecuting {
		t.Errorf("state after failure = %s/%s", failed.WorkflowState, failed.CleanupState)
	}
	op := lastOp(t, failed, ActionDispose)
	if op.Status != StatusFailed || !strings.Contains(op.Error, "tmux stuck") || len(op.Resources) != 2 ||
		op.Resources[0].Status != StatusSucceeded || op.Resources[1].Status != StatusFailed || !strings.Contains(op.Resources[1].Error, "tmux stuck") {
		t.Errorf("failed dispose operation = %+v", op)
	}

	// The retry gets a fresh operation and, once cleanup succeeds, removes the record.
	e.fk.cleaner.err = nil
	e.fk.cleaner.outcomes = []disposal.ResourceOutcome{{Kind: "workspace", ID: "repo-001"}}
	var retryOps []Operation
	e.fk.cleaner.onCall = func() {
		f, _ := e.m.Get(executing.ID)
		retryOps = f.Operations
	}

	got, err := e.m.Dispose(bg, executing.ID, confirm(executing))

	if err != nil || !got.Removed {
		t.Fatalf("retry Dispose() = %+v, error = %v", got, err)
	}
	disposes := 0
	for _, o := range retryOps {
		if o.Action == ActionDispose {
			disposes++
		}
	}
	if disposes != 2 || retryOps[len(retryOps)-1].Status != StatusPending {
		t.Errorf("operations during retry = %+v, want the failed attempt kept and a new pending one", retryOps)
	}
	if e.fk.cleaner.callCount() != 2 {
		t.Errorf("cleaner calls = %d, want 2", e.fk.cleaner.callCount())
	}
}

func TestDispose_AlreadyAbsentResourcesCountAsCleaned(t *testing.T) {
	e := newEnv(t)
	planned := takeoff(t, e)
	e.fk.cleaner.result = disposal.Result{WorkspaceMissing: true}

	got, err := e.m.Dispose(bg, planned.ID, confirm(planned))

	if err != nil || !got.Removed || !got.Cleanup.WorkspaceMissing {
		t.Fatalf("Dispose() = %+v, error = %v", got, err)
	}
	if _, ok := e.m.Get(planned.ID); ok {
		t.Error("record kept although every resource was already gone")
	}
}

func TestDispose_DisposingStateSurvivesRestartWithoutResumingCleanup(t *testing.T) {
	e := newEnv(t)
	executing := execute(t, e)
	e.fk.cleaner.err = errors.New("workspace purge failed")
	if _, err := e.m.Dispose(bg, executing.ID, confirm(executing)); err == nil {
		t.Fatal("setup: Dispose() should have failed")
	}
	callsBefore := e.fk.cleaner.callCount()

	reloaded := e.reload(t)

	got, ok := reloaded.Get(executing.ID)
	if !ok || got.CleanupState != CleanupDisposing {
		t.Fatalf("reloaded flight = %+v, ok = %v", got, ok)
	}
	if e.fk.cleaner.callCount() != callsBefore {
		t.Error("loading the snapshot started a cleanup attempt")
	}
}

func TestDispose_ConcurrentRequestsCleanOnce(t *testing.T) {
	e := newEnv(t)
	planned := takeoff(t, e)
	results := concurrentResults(t, "Dispose", 6, func() error {
		_, err := e.m.Dispose(bg, planned.ID, confirm(planned))
		return err
	})

	succeeded := 0
	for _, err := range results {
		switch {
		case err == nil:
			succeeded++
		case !errors.Is(err, ErrNotFound):
			t.Errorf("unexpected error %v", err)
		}
	}
	if succeeded != 1 || e.fk.cleaner.callCount() != 1 {
		t.Errorf("succeeded %d, cleaner calls %d; want exactly one of each", succeeded, e.fk.cleaner.callCount())
	}
}
