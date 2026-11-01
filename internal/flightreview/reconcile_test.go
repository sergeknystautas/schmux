package flightreview

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/sergeknystautas/schmux/internal/flighthistory"
	"github.com/sergeknystautas/schmux/internal/flights"
	"github.com/sergeknystautas/schmux/internal/state"
)

type reconcileEnv struct {
	t     *testing.T
	st    *state.State
	svc   *Service
	clock time.Time
}

func newReconcileEnv(t *testing.T) *reconcileEnv {
	t.Helper()
	isolate(t)
	e := &reconcileEnv{t: t, st: state.New("", nil), clock: t0.Add(time.Hour)}
	e.svc = e.newService()
	return e
}

// newService builds a Service over the same state and clock, as a daemon
// restart would.
func (e *reconcileEnv) newService() *Service {
	return NewService(Options{State: e.st, Now: func() time.Time { return e.clock }})
}

func (e *reconcileEnv) advance(d time.Duration) { e.clock = e.clock.Add(d) }

func (e *reconcileEnv) addWorkspace(id string) {
	e.t.Helper()
	if err := e.st.AddWorkspace(state.Workspace{ID: id, Repo: "https://example.com/r.git", Branch: "feature/x", Path: "/ws/" + id}); err != nil {
		e.t.Fatal(err)
	}
}

func (e *reconcileEnv) addSession(id, workspaceID string) {
	e.t.Helper()
	if err := e.st.AddSession(state.Session{ID: id, WorkspaceID: workspaceID}); err != nil {
		e.t.Fatal(err)
	}
}

func (e *reconcileEnv) reconcile(m *flights.Manager) bool {
	e.t.Helper()
	changed, err := e.svc.Reconcile(m)
	if err != nil {
		e.t.Fatalf("Reconcile: %v", err)
	}
	return changed
}

func reconciliationEvents(t *testing.T) []flighthistory.ReconciliationEvent {
	t.Helper()
	events, malformed, err := flighthistory.ReadReconciliation(flighthistory.ReconciliationPath())
	if err != nil || len(malformed) != 0 {
		t.Fatalf("read reconciliation: err=%v malformed=%v", err, malformed)
	}
	return events
}

func executingRow(id string, sessions ...[2]string) flights.Flight {
	row := startedRow(id, flights.StateExecuting)
	for _, s := range sessions {
		assign(&row, flights.SessionRole(s[0]), s[1])
	}
	return row
}

func TestReconcileRecordsMissingWorkspace(t *testing.T) {
	e := newReconcileEnv(t)
	m := testManager(t, executingRow("fl-1"))

	if !e.reconcile(m) {
		t.Fatal("Reconcile reported no change for a missing workspace")
	}

	events := reconciliationEvents(t)
	if len(events) != 1 {
		t.Fatalf("events = %+v", events)
	}
	got := events[0]
	if got.Kind != flighthistory.ReconcileResourceMissing || got.FlightID != "fl-1" || got.ResourceKind != "workspace" ||
		got.ResourceID != "ws-fl-1" || got.Source != "unknown" || !got.RecordedAt.Equal(e.clock) {
		t.Errorf("event = %+v", got)
	}
	var last struct {
		Path string `json:"path"`
	}
	if err := json.Unmarshal(got.LastKnown, &last); err != nil || last.Path != "/ws/fl-1" {
		t.Errorf("last known = %s (err %v), want the workspace assignment", got.LastKnown, err)
	}
	if mode := modeOf(t, flighthistory.ReconciliationPath()); mode != 0o600 {
		t.Errorf("ledger mode = %v, want 0600", mode)
	}
}

func TestReconcileRecordsMissingCurrentSessions(t *testing.T) {
	e := newReconcileEnv(t)
	e.addWorkspace("ws-fl-1")
	e.addSession("s-planner", "ws-fl-1")
	m := testManager(t, executingRow("fl-1", [2]string{"planner", "s-planner"}, [2]string{"executor", "s-exec"}))

	e.reconcile(m)

	events := reconciliationEvents(t)
	if len(events) != 1 || events[0].ResourceKind != "session" || events[0].ResourceID != "s-exec" ||
		events[0].Kind != flighthistory.ReconcileResourceMissing || events[0].Source != "unknown" {
		t.Fatalf("events = %+v, want only the executor session missing", events)
	}
}

func TestReconcileRecordsReviewerReplacement(t *testing.T) {
	e := newReconcileEnv(t)
	e.addWorkspace("ws-fl-1")
	e.addSession("s-r1", "ws-fl-1")
	e.addSession("s-r2", "ws-fl-1")

	if e.reconcile(testManager(t, executingRow("fl-1", [2]string{"reviewer", "s-r1"}))) {
		t.Fatal("a healthy first observation was reported as a change")
	}
	e.advance(time.Minute)
	if !e.reconcile(testManager(t, executingRow("fl-1", [2]string{"reviewer", "s-r2"}))) {
		t.Fatal("Reconcile reported no change for a replaced reviewer")
	}

	events := reconciliationEvents(t)
	if len(events) != 1 {
		t.Fatalf("events = %+v", events)
	}
	got := events[0]
	if got.Kind != flighthistory.ReconcileResourceReplaced || got.ResourceKind != "session" || got.ResourceID != "s-r2" {
		t.Errorf("event = %+v", got)
	}
	var last struct {
		Role      string `json:"role"`
		PreviousS string `json:"previous_session_id"`
	}
	if err := json.Unmarshal(got.LastKnown, &last); err != nil || last.Role != "reviewer" || last.PreviousS != "s-r1" {
		t.Errorf("last known = %s (err %v), want reviewer / s-r1", got.LastKnown, err)
	}
}

func TestReconcileDoesNotDuplicateLatestMissingResource(t *testing.T) {
	e := newReconcileEnv(t)
	m := testManager(t, executingRow("fl-1"))

	e.reconcile(m)
	e.advance(time.Minute)
	if e.reconcile(m) {
		t.Error("the same missing workspace was reported twice")
	}
	if got := len(reconciliationEvents(t)); got != 1 {
		t.Fatalf("events after repeat = %d, want 1", got)
	}

	// Workspace numbers are reused: a workspace with the same id is another
	// flight's replacement, not this flight's resource. Its changes are not
	// this flight's changes and its later loss is not a second loss.
	e.addWorkspace("ws-fl-1")
	e.advance(time.Minute)
	if e.reconcile(m) {
		t.Error("a reused workspace id was reported as this flight's")
	}
	ws, _ := e.st.GetWorkspace("ws-fl-1")
	ws.Dirty, ws.FilesChanged = true, 3
	if err := e.st.UpdateWorkspace(ws); err != nil {
		t.Fatal(err)
	}
	e.advance(time.Minute)
	if e.reconcile(m) {
		t.Error("a change in a reused workspace was attributed to the flight that lost the id")
	}
	if err := e.st.RemoveWorkspace("ws-fl-1"); err != nil {
		t.Fatal(err)
	}
	e.advance(time.Minute)
	e.reconcile(m)
	if got := len(reconciliationEvents(t)); got != 1 {
		t.Errorf("events after a reused id came and went = %d, want 1", got)
	}

	// A restarted service must not re-report what the ledger already holds,
	// nor adopt a reused id the ledger says this flight lost.
	e.svc = e.newService()
	e.addWorkspace("ws-fl-1")
	e.advance(time.Minute)
	if e.reconcile(m) {
		t.Error("a restarted service re-reported a missing workspace already in the ledger")
	}
	ws, _ = e.st.GetWorkspace("ws-fl-1")
	ws.Dirty, ws.FilesChanged = true, 1
	if err := e.st.UpdateWorkspace(ws); err != nil {
		t.Fatal(err)
	}
	e.advance(time.Minute)
	if e.reconcile(m) {
		t.Error("a restarted service attributed a reused workspace's change to the flight that lost the id")
	}
}

func TestReconcileRecordsExternalWorkspaceChangeOnlyAfterAssignedActivity(t *testing.T) {
	run := func(t *testing.T, sessionOutputBeforeChange, passBetweenOutputAndChange bool) []flighthistory.ReconciliationEvent {
		e := newReconcileEnv(t)
		e.addWorkspace("ws-fl-1")
		e.addSession("s-exec", "ws-fl-1")
		m := testManager(t, executingRow("fl-1", [2]string{"executor", "s-exec"}))

		if !sessionOutputBeforeChange {
			e.st.UpdateSessionLastOutput("s-exec", e.clock.Add(-time.Hour))
		}
		e.reconcile(m) // first observation: nothing is a change yet
		e.advance(10 * time.Minute)
		if sessionOutputBeforeChange {
			// The agent produced output after the workspace was last seen in
			// this state, so it explains the workspace change that follows.
			e.st.UpdateSessionLastOutput("s-exec", e.clock.Add(-time.Minute))
		}
		if passBetweenOutputAndChange {
			// Output triggers a dashboard broadcast, and so a pass, before
			// git status reflects the edit. That pass sees no change.
			if e.reconcile(m) {
				t.Fatal("a pass with an unchanged workspace recorded something")
			}
			e.advance(time.Minute)
		}
		ws, _ := e.st.GetWorkspace("ws-fl-1")
		ws.Dirty, ws.FilesChanged = true, 3
		if err := e.st.UpdateWorkspace(ws); err != nil {
			t.Fatal(err)
		}
		e.advance(time.Minute)
		e.reconcile(m)
		return reconciliationEvents(t)
	}

	t.Run("a session active since the workspace was last seen explains the change", func(t *testing.T) {
		if events := run(t, true, false); len(events) != 0 {
			t.Fatalf("events = %+v, want none", events)
		}
	})
	t.Run("a pass between the output and the change does not unexplain it", func(t *testing.T) {
		if events := run(t, true, true); len(events) != 0 {
			t.Fatalf("events = %+v, want none", events)
		}
	})
	t.Run("a stale session leaves the change unexplained", func(t *testing.T) {
		events := run(t, false, true)
		if len(events) != 1 {
			t.Fatalf("events = %+v, want one", events)
		}
		got := events[0]
		if got.Kind != flighthistory.ReconcileWorkspaceChanged || got.ResourceKind != "workspace" || got.ResourceID != "ws-fl-1" || got.FlightID != "fl-1" {
			t.Errorf("event = %+v", got)
		}
	})
}

func TestReconcileIgnoresBacklogAndDisposingFlights(t *testing.T) {
	e := newReconcileEnv(t)
	disposing := executingRow("fl-d")
	disposing.CleanupState = flights.CleanupDisposing
	m := testManager(t, backlogRow("fl-b"), disposing)

	if e.reconcile(m) {
		t.Error("Reconcile reported a change for flights with nothing to reconcile")
	}
	if _, err := os.Stat(filepath.Join(flighthistory.Root(), "reconciliation.jsonl")); !os.IsNotExist(err) {
		t.Errorf("a reconciliation ledger was created with nothing to record: %v", err)
	}
}
