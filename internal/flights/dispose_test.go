package flights

import (
	"errors"
	"fmt"
	"path/filepath"
	"slices"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/sergeknystautas/schmux/internal/disposal"
	"github.com/sergeknystautas/schmux/internal/flighthistory"
)

// confirmed is the confirmation every successful dispose carries.
var confirmed = DisposeConfirmation{Confirmed: true}

func TestDispose_CleansEverythingAndRemovesTheRecord(t *testing.T) {
	e := newEnv(t)
	executing := execute(t, e)
	// The flight's own session IDs, in the order dispose must close them.
	var flightSessionIDs []string
	for _, s := range executing.Sessions {
		if s.SessionID != "" {
			flightSessionIDs = append(flightSessionIDs, s.SessionID)
		}
	}
	e.fk.cleaner.outcomes = []disposal.ResourceOutcome{
		{Kind: "workspace", ID: "repo-001"}, {Kind: "preview", ID: "repo-001"},
	}
	e.fk.cleaner.result = disposal.Result{SessionsDisposed: []string{"sess-1", "sess-2"}, WorkspacePurged: true}

	got, err := e.m.Dispose(bg, executing.ID, confirmed)

	if err != nil {
		t.Fatalf("Dispose() error = %v", err)
	}
	if len(e.fk.cleaner.calls) != 1 {
		t.Fatalf("cleaner calls = %d, want 1", len(e.fk.cleaner.calls))
	}
	if len(e.fk.cleaner.sessionCalls) != 1 {
		t.Fatalf("DisposeSessions calls = %d, want 1", len(e.fk.cleaner.sessionCalls))
	}
	if got := strings.Join(e.fk.cleaner.sessionCalls[0], ","); got != strings.Join(flightSessionIDs, ",") {
		t.Errorf("disposed sessions = %q, want %q", got, strings.Join(flightSessionIDs, ","))
	}
	want := disposal.Request{WorkspaceID: "repo-001", ForcePurge: true}
	if gotReq := e.fk.cleaner.calls[0]; gotReq != want {
		t.Errorf("cleanup request = %+v, want %+v", gotReq, want)
	}
	if !got.Removed {
		t.Errorf("result = %+v", got)
	}
	if got := strings.Join(got.Cleanup.SessionsDisposed, ","); got != "sess-1,sess-2,sess-3,sess-1,sess-2" {
		t.Errorf("cleanup sessions disposed = %q", got)
	}
	if got.Flight.ID != executing.ID || got.Flight.CleanupState != CleanupDisposing {
		t.Errorf("result flight = %+v", got.Flight)
	}
	if op := lastOp(t, got.Flight, ActionDispose); op.Status != StatusSucceeded || len(op.Resources) != len(flightSessionIDs)+2 {
		t.Errorf("dispose operation = %+v", op)
	}
	if _, ok := e.m.Get(executing.ID); ok {
		t.Error("record still present after successful cleanup")
	}
	if _, ok := e.reload(t).Get(executing.ID); ok {
		t.Error("record resurrected after restart")
	}
}

func TestDispose_PersistsDisposingStateAndEachOutcomeBeforeCleanupFinishes(t *testing.T) {
	e := newEnv(t)
	executing := execute(t, e)
	var midCleanup Flight
	e.fk.cleaner.outcomes = []disposal.ResourceOutcome{{Kind: "workspace", ID: "repo-001"}}
	e.fk.cleaner.onCall = func() {
		// What a daemon restarted right now would see on disk.
		midCleanup, _ = e.reload(t).Get(executing.ID)
	}

	if _, err := e.m.Dispose(bg, executing.ID, confirmed); err != nil {
		t.Fatalf("Dispose() error = %v", err)
	}

	if midCleanup.CleanupState != CleanupDisposing || midCleanup.WorkflowState != StateExecuting {
		t.Errorf("mid-cleanup state = %s/%s, want executing/disposing", midCleanup.WorkflowState, midCleanup.CleanupState)
	}
	op := lastOp(t, midCleanup, ActionDispose)
	if op.Status != StatusPending || len(op.Resources) != 4 {
		t.Errorf("mid-cleanup dispose operation = %+v", op)
	}
	for i, r := range op.Resources[:3] {
		if r.Kind != "session" {
			t.Errorf("mid-cleanup resource %d = %+v, want a session", i, r)
		}
	}
	if op.Resources[3].Kind != "workspace" {
		t.Errorf("mid-cleanup last resource = %+v, want the workspace", op.Resources[3])
	}
}

func TestDispose_RejectsUnconfirmedRequestsWithoutTouchingAnything(t *testing.T) {
	e := newEnv(t)
	planned := takeoff(t, e)

	_, err := e.m.Dispose(bg, planned.ID, DisposeConfirmation{Confirmed: false})

	if !errors.Is(err, ErrConflict) {
		t.Fatalf("Dispose() error = %v, want ErrConflict", err)
	}
	got, _ := e.m.Get(planned.ID)
	if got.CleanupState != CleanupIdle || len(got.Operations) != len(planned.Operations) {
		t.Errorf("rejected dispose changed the flight: %+v", got)
	}
	if e.fk.cleaner.callCount() != 0 || len(e.fk.cleaner.sessionCalls) != 0 {
		t.Error("cleanup ran without confirmation")
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
	e := &testEnv{m: m, path: path, historyPath: historyPathFor(path), fk: newFakes(t)}

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

	if _, err := e.m.Dispose(bg, executing.ID, confirmed); err != nil {
		t.Fatalf("Dispose() error = %v", err)
	}
	if !midCleanup.UpdatedAt.After(executing.UpdatedAt) {
		t.Errorf("UpdatedAt mid-cleanup = %v, want after %v", midCleanup.UpdatedAt, executing.UpdatedAt)
	}
}

func TestDispose_AllStartedStagesCanBeDisposed(t *testing.T) {
	for _, stage := range []WorkflowState{StatePlanning, StateExecuting, StateReviewing, StateCompleting} {
		t.Run(string(stage), func(t *testing.T) {
			e := newEnv(t)
			var f Flight
			switch stage {
			case StatePlanning:
				f = takeoff(t, e)
			case StateExecuting:
				f = execute(t, e)
			case StateReviewing:
				f = review(t, e)
			default:
				f = complete(t, e)
			}
			got, err := e.m.Dispose(bg, f.ID, confirmed)
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
		{Kind: "workspace", ID: "repo-001", Err: errors.New("workspace purge failed")},
	}
	e.fk.cleaner.err = errors.New("workspace purge failed")

	_, err := e.m.Dispose(bg, executing.ID, confirmed)

	if err == nil || !strings.Contains(err.Error(), "workspace purge failed") || errors.Is(err, ErrConflict) {
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
	if op.Status != StatusFailed || !strings.Contains(op.Error, "workspace purge failed") || len(op.Resources) != 4 {
		t.Errorf("failed dispose operation = %+v", op)
	}
	for i, r := range op.Resources[:3] {
		if r.Kind != "session" || r.Status != StatusSucceeded {
			t.Errorf("failed dispose resource %d = %+v, want a succeeded session", i, r)
		}
	}
	if op.Resources[3].Status != StatusFailed || !strings.Contains(op.Resources[3].Error, "workspace purge failed") {
		t.Errorf("failed dispose last resource = %+v, want the failed workspace", op.Resources[3])
	}

	// The retry gets a fresh operation and, once cleanup succeeds, removes the record.
	e.fk.cleaner.err = nil
	e.fk.cleaner.outcomes = []disposal.ResourceOutcome{{Kind: "workspace", ID: "repo-001"}}
	var retryOps []Operation
	e.fk.cleaner.onCall = func() {
		f, _ := e.m.Get(executing.ID)
		retryOps = f.Operations
	}

	got, err := e.m.Dispose(bg, executing.ID, confirmed)

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

	got, err := e.m.Dispose(bg, planned.ID, confirmed)

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
	if _, err := e.m.Dispose(bg, executing.ID, confirmed); err == nil {
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
		_, err := e.m.Dispose(bg, planned.ID, confirmed)
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

func TestDisposeArchivesEvidenceBeforeCleanup(t *testing.T) {
	e := newEnv(t)
	executing := execute(t, e)
	var sequence []string
	e.fk.evidence.onCapture = func(f Flight, final bool) {
		events := e.historyEvents(t)
		if len(events) == 0 {
			t.Fatalf("no history event recorded at capture(final=%v); ledger = %+v", final, events)
		}
		last := events[len(events)-1]
		sequence = append(sequence, fmt.Sprintf("capture(final=%v) after %s, state %s", final, last.Cause, f.CleanupState))
	}
	e.fk.cleaner.onCall = func() { sequence = append(sequence, "DisposeAll") }

	if _, err := e.m.Dispose(bg, executing.ID, confirmed); err != nil {
		t.Fatalf("Dispose() error = %v", err)
	}

	want := []string{
		"capture(final=false) after dispose_started, state disposing",
		"DisposeAll",
		"capture(final=true) after dispose_succeeded, state disposing",
	}
	if !slices.Equal(sequence, want) {
		t.Fatalf("sequence = %q, want %q", sequence, want)
	}
	events := e.historyEvents(t)
	if len(events) == 0 {
		t.Fatalf("no history event recorded; ledger = %+v", events)
	}
	last := events[len(events)-1]
	if last.Cause != "dispose_removed" || last.Change != flighthistory.ChangeRemoved {
		t.Errorf("last event = %s/%s, want dispose_removed/flight_removed", last.Change, last.Cause)
	}
	if len(e.fk.evidence.captures) != 2 {
		t.Fatalf("captures = %+v, want pre-clean evidence and a final row", e.fk.evidence.captures)
	}
	for i, capture := range e.fk.evidence.captures {
		if capture.Flight.ID != executing.ID {
			t.Errorf("capture %d flight = %q, want %q", i, capture.Flight.ID, executing.ID)
		}
	}
	if op := lastOp(t, e.fk.evidence.captures[1].Flight, ActionDispose); op.Status != StatusSucceeded {
		t.Errorf("final captured operation = %s, want succeeded", op.Status)
	}
	// The removal event names the archived final row; the row itself lives
	// only in the archive.
	assertRecorded(t, last, e.fk.evidence.captures[1].Flight)
}

func TestDisposeArchivalFailureBlocksCleanup(t *testing.T) {
	e := newEnv(t)
	executing := execute(t, e)
	e.fk.evidence.err = errors.New("archive disk full")

	_, err := e.m.Dispose(bg, executing.ID, confirmed)

	if err == nil || !strings.Contains(err.Error(), "archive disk full") {
		t.Fatalf("Dispose() error = %v, want the archival failure", err)
	}
	if e.fk.cleaner.callCount() != 0 {
		t.Errorf("DisposeAll ran %d times after archival failed", e.fk.cleaner.callCount())
	}
	got, ok := e.reload(t).Get(executing.ID)
	if !ok || got.CleanupState != CleanupDisposing {
		t.Fatalf("reloaded flight = %+v, ok = %v; want it retained and disposing", got, ok)
	}
	if op := lastOp(t, got, ActionDispose); op.Status != StatusPending {
		t.Errorf("dispose operation status = %s, want pending", op.Status)
	}
}

func TestDisposeFinalRowArchivalFailureLeavesFlightRetryable(t *testing.T) {
	e := newEnv(t)
	executing := execute(t, e)
	e.fk.cleaner.onCall = func() {
		e.fk.evidence.err = errors.New("final archive disk full")
	}

	_, err := e.m.Dispose(bg, executing.ID, confirmed)

	if err == nil || !strings.Contains(err.Error(), "final archive disk full") {
		t.Fatalf("Dispose error = %v, want the final-row archival failure", err)
	}
	got, ok := e.reload(t).Get(executing.ID)
	if !ok {
		t.Fatal("the flight row was removed although its final archive failed")
	}
	if op := lastOp(t, got, ActionDispose); op.Status != StatusSucceeded {
		t.Fatalf("last operation = %+v, want a succeeded cleanup awaiting archival retry", op)
	}
}

func TestDisposeWorkspacelessFlightArchivesBeforeRemoval(t *testing.T) {
	fk := newFakes(t)
	path := filepath.Join(t.TempDir(), "flights.json")
	writeSnapshot(t, path, snapshot{Flights: []Flight{startedFlightNoWorkspace("fl-pn", StatePlanning)}})
	m, _ := newManagerWith(t, path, &atomic.Int32{}, fk)
	rowPresent := false
	fk.evidence.onCapture = func(Flight, bool) { _, rowPresent = m.Get("fl-pn") }

	if _, err := m.Dispose(bg, "fl-pn", DisposeConfirmation{Confirmed: true}); err != nil {
		t.Fatalf("Dispose() error = %v", err)
	}

	// Every dispose runs the same steps: evidence before cleanup, then the
	// final row before removal, even when there is nothing to clean.
	if n := fk.evidence.captureCount(); n != 2 || fk.evidence.captures[0].Final || !fk.evidence.captures[1].Final {
		t.Fatalf("captures = %+v, want pre-clean evidence then a final row", fk.evidence.captures)
	}
	if !rowPresent {
		t.Error("the flight row was already gone when its evidence was captured")
	}
}

func TestDisposeRefusedWithoutConfirmationCapturesNothing(t *testing.T) {
	fk := newFakes(t)
	path := filepath.Join(t.TempDir(), "flights.json")
	writeSnapshot(t, path, snapshot{Flights: []Flight{startedFlightNoWorkspace("fl-pn", StatePlanning)}})
	m, _ := newManagerWith(t, path, &atomic.Int32{}, fk)

	if _, err := m.Dispose(bg, "fl-pn", DisposeConfirmation{}); err == nil {
		t.Fatal("Dispose() succeeded without confirmation")
	}

	if fk.evidence.captureCount() != 0 {
		t.Errorf("an unconfirmed disposal captured final evidence: %+v", fk.evidence.captures)
	}
}

func TestDispose_SkipsAWorkspaceThatIsNotStamped(t *testing.T) {
	e := newEnv(t)
	executing := execute(t, e)
	delete(e.fk.workspaces.owners, "repo-001") // the flight's workspace is gone; "repo-001" may now be someone else's

	got, err := e.m.Dispose(bg, executing.ID, confirmed)

	if err != nil || !got.Removed {
		t.Fatalf("Dispose() = %+v, error = %v", got, err)
	}
	if e.fk.cleaner.callCount() != 0 {
		t.Fatalf("DisposeAll called %d times, want 0: nothing carries this flight's id", e.fk.cleaner.callCount())
	}
	if len(e.fk.cleaner.sessionCalls) != 1 {
		t.Errorf("DisposeSessions calls = %d, want 1 (the flight's own sessions still close)", len(e.fk.cleaner.sessionCalls))
	}
	op := lastOp(t, got.Flight, ActionDispose)
	var ws []ResourceOutcome
	for _, r := range op.Resources {
		if r.Kind == "workspace" {
			ws = append(ws, r)
		}
	}
	if len(ws) != 1 || ws[0].ID != "" || ws[0].Status != StatusSucceeded {
		t.Errorf("workspace outcomes = %+v, want one empty-ID success", ws)
	}
}

func TestDispose_FindsOnlyItsOwnStampedWorkspace(t *testing.T) {
	e := newEnv(t)
	a := execute(t, e)
	e.fk.workspaces.nextID = "repo-002"
	b := takeoff(t, e)

	if _, err := e.m.Dispose(bg, b.ID, confirmed); err != nil {
		t.Fatal(err)
	}
	if req := e.fk.cleaner.calls[0]; req.WorkspaceID != "repo-002" {
		t.Errorf("flight b purged %q, want repo-002", req.WorkspaceID)
	}
	if _, ok := e.fk.workspaces.OwnedWorkspace(a.ID); !ok {
		t.Error("flight a's workspace lost its stamp")
	}
}

func TestDispose_PurgesTheStampedWorkspaceWithOnlyWorkspaceIDAndForcePurge(t *testing.T) {
	e := newEnv(t)
	executing := execute(t, e)

	if _, err := e.m.Dispose(bg, executing.ID, confirmed); err != nil {
		t.Fatal(err)
	}
	want := disposal.Request{WorkspaceID: "repo-001", ForcePurge: true}
	if got := e.fk.cleaner.calls[0]; got != want {
		t.Errorf("request = %+v, want %+v (no chat, no branch, no remote)", got, want)
	}
}

func TestDispose_OwnSessionFailureStopsBeforeTheWorkspace(t *testing.T) {
	e := newEnv(t)
	executing := execute(t, e)
	e.fk.cleaner.sessionErr = errors.New("dispose session sess-2: tmux stuck")

	_, err := e.m.Dispose(bg, executing.ID, confirmed)

	if err == nil || !strings.Contains(err.Error(), "tmux stuck") {
		t.Fatalf("error = %v", err)
	}
	if e.fk.cleaner.callCount() != 0 {
		t.Error("DisposeAll ran although a listed session failed")
	}
	failed, _ := e.reload(t).Get(executing.ID)
	if failed.CleanupState != CleanupDisposing {
		t.Errorf("cleanup state = %s", failed.CleanupState)
	}
	e.fk.cleaner.sessionErr = nil
	if got, err := e.m.Dispose(bg, executing.ID, confirmed); err != nil || !got.Removed {
		t.Fatalf("retry = %+v, %v", got, err)
	}
}
