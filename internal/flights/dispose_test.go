package flights

import (
	"errors"
	"strings"
	"testing"

	"github.com/sergeknystautas/schmux/internal/disposal"
)

func confirm(f Flight) TerminateConfirmation {
	return TerminateConfirmation{WorkspaceID: f.Workspace.WorkspaceID, Confirmed: true}
}

func TestTerminate_CleansEverythingAndRemovesTheRecord(t *testing.T) {
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

	got, err := e.m.Terminate(bg, executing.ID, confirm(executing))

	if err != nil {
		t.Fatalf("Terminate() error = %v", err)
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
	if got.Flight.ID != executing.ID || got.Flight.CleanupState != CleanupTerminating {
		t.Errorf("result flight = %+v", got.Flight)
	}
	if op := lastOp(t, got.Flight, ActionTerminate); op.Status != StatusSucceeded || len(op.Resources) != 5 {
		t.Errorf("terminate operation = %+v", op)
	}
	if _, ok := e.m.Get(executing.ID); ok {
		t.Error("record still present after successful cleanup")
	}
	if _, ok := e.reload(t).Get(executing.ID); ok {
		t.Error("record resurrected after restart")
	}
}

// TestTerminate_DeletesOnlyThisFlightsConversations covers the new guarantee:
// the disposal request names only this flight's chat session IDs, even when
// the same workspace holds other sessions recorded as prior attempts.
func TestTerminate_DeletesOnlyThisFlightsConversations(t *testing.T) {
	e := newEnv(t)
	f := takeoff(t, e)

	// Reload with a prior session id recorded against the planner, simulating
	// an earlier retry that left a different sessionID behind in the same workspace.
	reloaded, _ := e.m.Get(f.ID)
	reloaded.Sessions[0].PriorSessionIDs = []string{"sess-other"}
	// Update's API doesn't expose Sessions, so seed a fresh manager over a
	// snapshot that contains the modified flight.
	if err := writeSnapshot(e.path, snapshot{Flights: []Flight{reloaded}}); err != nil {
		t.Fatal(err)
	}
	e.m, _ = newManagerWith(t, e.path, e.notices, e.fk)

	e.fk.cleaner.result = disposal.Result{ChatDirectoriesDeleted: 2, WorkspacePurged: true}

	if _, err := e.m.Terminate(bg, f.ID, confirm(reloaded)); err != nil {
		t.Fatalf("Terminate() error = %v", err)
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

func TestTerminate_PersistsTerminatingStateAndEachOutcomeBeforeCleanupFinishes(t *testing.T) {
	e := newEnv(t)
	executing := execute(t, e)
	var midCleanup Flight
	e.fk.cleaner.outcomes = []disposal.ResourceOutcome{{Kind: "session", ID: "sess-1"}, {Kind: "workspace", ID: "repo-001"}}
	e.fk.cleaner.onCall = func() {
		// What a daemon restarted right now would see on disk.
		midCleanup, _ = e.reload(t).Get(executing.ID)
	}

	if _, err := e.m.Terminate(bg, executing.ID, confirm(executing)); err != nil {
		t.Fatalf("Terminate() error = %v", err)
	}

	if midCleanup.CleanupState != CleanupTerminating || midCleanup.WorkflowState != StateExecuting {
		t.Errorf("mid-cleanup state = %s/%s, want executing/terminating", midCleanup.WorkflowState, midCleanup.CleanupState)
	}
	op := lastOp(t, midCleanup, ActionTerminate)
	if op.Status != StatusPending || len(op.Resources) != 2 || op.Resources[0].Kind != "session" || op.Resources[1].Kind != "workspace" {
		t.Errorf("mid-cleanup terminate operation = %+v", op)
	}
}

func TestTerminate_RejectsUnconfirmedRequestsWithoutTouchingAnything(t *testing.T) {
	tests := []struct {
		name string
		conf func(Flight) TerminateConfirmation
	}{
		{"not confirmed", func(f Flight) TerminateConfirmation {
			return TerminateConfirmation{WorkspaceID: f.Workspace.WorkspaceID, Confirmed: false}
		}},
		{"wrong workspace", func(f Flight) TerminateConfirmation {
			return TerminateConfirmation{WorkspaceID: "repo-999", Confirmed: true}
		}},
		{"missing workspace", func(f Flight) TerminateConfirmation { return TerminateConfirmation{Confirmed: true} }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := newEnv(t)
			planned := takeoff(t, e)

			_, err := e.m.Terminate(bg, planned.ID, tt.conf(planned))

			if !errors.Is(err, ErrConflict) {
				t.Fatalf("Terminate() error = %v, want ErrConflict", err)
			}
			got, _ := e.m.Get(planned.ID)
			if got.CleanupState != CleanupIdle || len(got.Operations) != len(planned.Operations) {
				t.Errorf("rejected terminate changed the flight: %+v", got)
			}
			if e.fk.cleaner.callCount() != 0 {
				t.Error("cleanup ran without confirmation")
			}
		})
	}
}

func TestTerminate_RejectsBacklogAndUnknownFlights(t *testing.T) {
	e := newEnv(t)
	backlog := mustCreate(t, e.m, goodCreate())

	_, backlogErr := e.m.Terminate(bg, backlog.ID, TerminateConfirmation{WorkspaceID: "repo-001", Confirmed: true})
	_, unknownErr := e.m.Terminate(bg, "fl-404", TerminateConfirmation{WorkspaceID: "repo-001", Confirmed: true})

	if !errors.Is(backlogErr, ErrConflict) || !strings.Contains(backlogErr.Error(), "delete") {
		t.Errorf("backlog Terminate() error = %v, want ErrConflict pointing at delete", backlogErr)
	}
	if !errors.Is(unknownErr, ErrNotFound) {
		t.Errorf("unknown Terminate() error = %v, want ErrNotFound", unknownErr)
	}
	if e.fk.cleaner.callCount() != 0 {
		t.Error("cleanup ran")
	}
}

func TestTerminate_DoneAndCancelledFlightsCanBeTerminated(t *testing.T) {
	for _, finish := range []string{"done", "cancelled"} {
		t.Run(finish, func(t *testing.T) {
			e := newEnv(t)
			var f Flight
			var err error
			if finish == "done" {
				f = complete(t, e)
				f, err = e.m.MarkDone(bg, f.ID)
			} else {
				f = execute(t, e)
				f, err = e.m.Cancel(bg, f.ID, "abandoned")
			}
			if err != nil {
				t.Fatal(err)
			}

			got, err := e.m.Terminate(bg, f.ID, confirm(f))

			if err != nil || !got.Removed {
				t.Fatalf("Terminate() = %+v, error = %v", got, err)
			}
			if got.Flight.WorkflowState != WorkflowState(finish) {
				t.Errorf("workflow state changed to %s", got.Flight.WorkflowState)
			}
		})
	}
}

func TestTerminate_PartialFailureKeepsTheRecordAndIsRetryable(t *testing.T) {
	e := newEnv(t)
	executing := execute(t, e)
	e.fk.cleaner.outcomes = []disposal.ResourceOutcome{
		{Kind: "session", ID: "sess-1"},
		{Kind: "session", ID: "sess-2", Err: errors.New("tmux stuck")},
	}
	e.fk.cleaner.err = errors.New("dispose session sess-2: tmux stuck")

	_, err := e.m.Terminate(bg, executing.ID, confirm(executing))

	if err == nil || !strings.Contains(err.Error(), "tmux stuck") || errors.Is(err, ErrConflict) {
		t.Fatalf("Terminate() error = %v, want the cleanup failure", err)
	}
	failed, ok := e.reload(t).Get(executing.ID)
	if !ok {
		t.Fatal("record removed despite failed cleanup")
	}
	if failed.CleanupState != CleanupTerminating || failed.WorkflowState != StateExecuting {
		t.Errorf("state after failure = %s/%s", failed.WorkflowState, failed.CleanupState)
	}
	op := lastOp(t, failed, ActionTerminate)
	if op.Status != StatusFailed || !strings.Contains(op.Error, "tmux stuck") || len(op.Resources) != 2 ||
		op.Resources[0].Status != StatusSucceeded || op.Resources[1].Status != StatusFailed || !strings.Contains(op.Resources[1].Error, "tmux stuck") {
		t.Errorf("failed terminate operation = %+v", op)
	}

	// The retry gets a fresh operation and, once cleanup succeeds, removes the record.
	e.fk.cleaner.err = nil
	e.fk.cleaner.outcomes = []disposal.ResourceOutcome{{Kind: "workspace", ID: "repo-001"}}
	var retryOps []Operation
	e.fk.cleaner.onCall = func() {
		f, _ := e.m.Get(executing.ID)
		retryOps = f.Operations
	}

	got, err := e.m.Terminate(bg, executing.ID, confirm(executing))

	if err != nil || !got.Removed {
		t.Fatalf("retry Terminate() = %+v, error = %v", got, err)
	}
	terminates := 0
	for _, o := range retryOps {
		if o.Action == ActionTerminate {
			terminates++
		}
	}
	if terminates != 2 || retryOps[len(retryOps)-1].Status != StatusPending {
		t.Errorf("operations during retry = %+v, want the failed attempt kept and a new pending one", retryOps)
	}
	if e.fk.cleaner.callCount() != 2 {
		t.Errorf("cleaner calls = %d, want 2", e.fk.cleaner.callCount())
	}
}

func TestTerminate_AlreadyAbsentResourcesCountAsCleaned(t *testing.T) {
	e := newEnv(t)
	planned := takeoff(t, e)
	e.fk.cleaner.result = disposal.Result{WorkspaceMissing: true}

	got, err := e.m.Terminate(bg, planned.ID, confirm(planned))

	if err != nil || !got.Removed || !got.Cleanup.WorkspaceMissing {
		t.Fatalf("Terminate() = %+v, error = %v", got, err)
	}
	if _, ok := e.m.Get(planned.ID); ok {
		t.Error("record kept although every resource was already gone")
	}
}

func TestTerminate_TerminatingStateSurvivesRestartWithoutResumingCleanup(t *testing.T) {
	e := newEnv(t)
	executing := execute(t, e)
	e.fk.cleaner.err = errors.New("workspace purge failed")
	if _, err := e.m.Terminate(bg, executing.ID, confirm(executing)); err == nil {
		t.Fatal("setup: Terminate() should have failed")
	}
	callsBefore := e.fk.cleaner.callCount()

	reloaded := e.reload(t)

	got, ok := reloaded.Get(executing.ID)
	if !ok || got.CleanupState != CleanupTerminating {
		t.Fatalf("reloaded flight = %+v, ok = %v", got, ok)
	}
	if e.fk.cleaner.callCount() != callsBefore {
		t.Error("loading the snapshot started a cleanup attempt")
	}
}

func TestTerminate_ConcurrentRequestsCleanOnce(t *testing.T) {
	e := newEnv(t)
	planned := takeoff(t, e)
	results := concurrentResults(t, "Terminate", 6, func() error {
		_, err := e.m.Terminate(bg, planned.ID, confirm(planned))
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
