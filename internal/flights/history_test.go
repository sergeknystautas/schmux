package flights

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sergeknystautas/schmux/internal/flighthistory"
)

// rowDigest is the digest the ledger records for f.
func rowDigest(t *testing.T, f Flight) string {
	t.Helper()
	digest, err := historyDigest(f)
	if err != nil {
		t.Fatal(err)
	}
	return digest
}

// assertRecorded checks that ev names f: its digest and its history state.
func assertRecorded(t *testing.T, ev flighthistory.Event, f Flight) {
	t.Helper()
	if ev.FlightID != f.ID {
		t.Errorf("event flight_id = %q, want %q", ev.FlightID, f.ID)
	}
	if ev.RowSHA256 != rowDigest(t, f) {
		t.Errorf("event row digest = %s, want the digest of the row", ev.RowSHA256)
	}
	want, _ := json.Marshal(f.HistoryState())
	got, _ := json.Marshal(ev.State)
	if !bytes.Equal(got, want) {
		t.Errorf("event state = %s, want %s", got, want)
	}
}

func snapshotDigest(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func TestEditAsRecordsCreatedUpdatedAndRemovedRows(t *testing.T) {
	e := newEnv(t)
	a := mustCreate(t, e.m, goodCreate())
	c := mustCreate(t, e.m, goodCreate())
	before := len(e.historyEvents(t))

	audit := AuditContext{Actor: flighthistory.ActorUser, Cause: "test_multi", CorrelationID: "corr-multi"}
	err := e.m.editAs(audit, func(flights *[]Flight) error {
		var kept []Flight
		for _, f := range *flights {
			switch f.ID {
			case a.ID:
				f.Prompt = "edited prompt"
				kept = append(kept, f)
			case c.ID:
				// removed
			default:
				kept = append(kept, f)
			}
		}
		b := a
		b.ID = "fl-new"
		kept = append(kept, b)
		*flights = kept
		return nil
	})
	if err != nil {
		t.Fatalf("editAs: %v", err)
	}

	events := e.historyEvents(t)[before:]
	if len(events) != 3 {
		t.Fatalf("events = %d, want 3: %+v", len(events), events)
	}
	want := map[string]flighthistory.Change{
		a.ID:     flighthistory.ChangeUpdated,
		"fl-new": flighthistory.ChangeCreated,
		c.ID:     flighthistory.ChangeRemoved,
	}
	digest := snapshotDigest(t, e.path)
	for _, ev := range events {
		id := ev.FlightID
		if want[id] != ev.Change {
			t.Errorf("event for %s change = %s, want %s", id, ev.Change, want[id])
		}
		if ev.EventID != events[0].EventID || !ev.RecordedAt.Equal(events[0].RecordedAt) {
			t.Errorf("related events must share id and timestamp: %+v vs %+v", ev, events[0])
		}
		if ev.SnapshotSHA256 != digest {
			t.Errorf("snapshot digest = %s, want %s", ev.SnapshotSHA256, digest)
		}
		if ev.Actor != flighthistory.ActorUser || ev.Cause != "test_multi" || ev.CorrelationID != "corr-multi" {
			t.Errorf("attribution = %s/%s/%s", ev.Actor, ev.Cause, ev.CorrelationID)
		}
	}
	for _, ev := range events {
		switch ev.FlightID {
		case a.ID:
			assertRecorded(t, ev, mustGet(t, e.m, a.ID))
		case c.ID:
			assertRecorded(t, ev, c)
		}
		line, _ := json.Marshal(ev)
		if len(line) > 1024 {
			t.Errorf("event for %s is %d bytes; an event must not carry the row", ev.FlightID, len(line))
		}
	}
	info, err := os.Stat(e.historyPath)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Errorf("history mode = %v, want 0600", info.Mode().Perm())
	}
}

// A session that no longer exists is live monitor status, retried every tick;
// it is not flight history. Recording or clearing it appends nothing, and no
// ledger row carries it.
func TestMonitorSessionErrorIsNotRecordedInHistory(t *testing.T) {
	e := newEnv(t)
	f := takeoff(t, e)
	planner := f.sessionFor(RolePlanner).SessionID
	before := len(e.historyEvents(t))

	if err := e.m.recordMonitorSessionError(f.ID, planner, errors.New("session not found: "+planner)); err != nil {
		t.Fatal(err)
	}
	if got := mustGet(t, e.m, f.ID); got.Monitor == nil || got.Monitor.Error == "" {
		t.Fatal("the session error was not retained on the flight")
	}
	if err := e.m.clearMonitorSessionError(f.ID, planner); err != nil {
		t.Fatal(err)
	}
	if got := e.historyEvents(t)[before:]; len(got) != 0 {
		t.Fatalf("session error appended %d history events; want none: %+v", len(got), got)
	}

	if err := e.m.recordMonitorSessionError(f.ID, planner, errors.New("session not found: "+planner)); err != nil {
		t.Fatal(err)
	}
	if _, err := e.m.Update(context.Background(), f.ID, UpdateRequest{Prompt: ptrString("a later real change")}); err != nil {
		t.Fatal(err)
	}
	events := e.historyEvents(t)[before:]
	if len(events) != 1 {
		t.Fatalf("events = %d, want 1 for the real change", len(events))
	}
	withError := mustGet(t, e.m, f.ID)
	if withError.Monitor == nil || withError.Monitor.Error == "" {
		t.Fatal("the session error was not retained across the real change")
	}
	cleared := withError.clone()
	cleared.Monitor.Error = ""
	if events[0].RowSHA256 != rowDigest(t, cleared) || rowDigest(t, withError) != rowDigest(t, cleared) {
		t.Fatalf("history digest depends on the monitor session error")
	}
}

// An event names the row and its workflow state; it never carries the row's
// monitor history, so its size stays bounded as turns and messages accumulate.
func TestHistoryEventSizeDoesNotGrowWithMonitorHistory(t *testing.T) {
	e := newEnv(t)
	f := takeoff(t, e)
	planner := f.sessionFor(RolePlanner).SessionID
	for i := 1; i <= 20; i++ {
		turn := PlannerTurn{SessionID: planner, StartSeq: uint64(i), EndSeq: uint64(i), Text: strings.Repeat("x", 4096)}
		if _, err := e.m.recordPlannerTurn(f.ID, turn); err != nil {
			t.Fatal(err)
		}
	}
	events := e.historyEvents(t)
	last := events[len(events)-1]
	if last.Cause != "record_monitor_turn" || last.State.MonitorTurns != 20 {
		t.Fatalf("last event = %+v, want record_monitor_turn with 20 turns", last)
	}
	line, _ := json.Marshal(last)
	if len(line) > 1024 {
		t.Fatalf("event is %d bytes after 20 turns; want under 1024", len(line))
	}
	assertRecorded(t, last, mustGet(t, e.m, f.ID))
}

func TestPilotTransitionIsAttributedToPilot(t *testing.T) {
	x := newDecisionExchange(t, StatePlanning)

	processed := x.decide(map[string]any{"advance": "executing"})
	if processed.Status == MonitoredTurnFailed {
		t.Fatalf("the advance was rejected: %+v", processed.Attempts)
	}

	var found *flighthistory.Event
	events := x.e.historyEvents(t)
	for i := range events {
		if events[i].Cause == string(ActionExecution) {
			found = &events[i]
			break
		}
	}
	if found == nil {
		t.Fatalf("no %s event among %d events", ActionExecution, len(events))
	}
	if found.Actor != flighthistory.ActorPilot || found.SourceTurnID != processed.ID {
		t.Errorf("actor=%s source turn=%q, want pilot/%q", found.Actor, found.SourceTurnID, processed.ID)
	}
	op := lastOp(t, mustGet(t, x.e.m, x.flightID), ActionExecution)
	if found.CorrelationID != op.CorrelationID {
		t.Errorf("correlation = %q, want the operation's %q", found.CorrelationID, op.CorrelationID)
	}
}

func TestManualTransitionIsAttributedToUser(t *testing.T) {
	e := newEnv(t)
	f := e.reportPlanning(t, takeoff(t, e))

	if _, err := e.m.EnterExecution(bg, f.ID); err != nil {
		t.Fatalf("EnterExecution: %v", err)
	}

	var found *flighthistory.Event
	events := e.historyEvents(t)
	for i := range events {
		if events[i].Cause == string(ActionExecution) {
			found = &events[i]
			break
		}
	}
	if found == nil {
		t.Fatalf("no %s event among %d events", ActionExecution, len(events))
	}
	if found.Actor != flighthistory.ActorUser || found.SourceTurnID != "" {
		t.Errorf("actor=%s source turn=%q, want user/empty", found.Actor, found.SourceTurnID)
	}
}

func TestLoadImportsExistingFlightsAsBaselines(t *testing.T) {
	m, path := seedStarted(t, startedFlight("fl-a", StatePlanning), backlogFlight("fl-b", 0))
	e := &testEnv{m: m, path: path, historyPath: historyPathFor(path), fk: newFakes(t)}

	events := e.historyEvents(t)
	if len(events) != 2 {
		t.Fatalf("baseline events = %d, want 2: %+v", len(events), events)
	}
	digest := snapshotDigest(t, path)
	for _, ev := range events {
		if ev.Change != flighthistory.ChangeBaseline || ev.Actor != flighthistory.ActorSystem || ev.SnapshotSHA256 != digest {
			t.Errorf("baseline event = %+v", ev)
		}
	}

	e.reload(t)
	if got := len(e.historyEvents(t)); got != 2 {
		t.Errorf("a second load appended events: %d, want 2", got)
	}
}

func TestLoadRecoversSnapshotEventGapWithoutBackdating(t *testing.T) {
	e := newEnv(t)
	f := mustCreate(t, e.m, goodCreate())
	saved, err := os.ReadFile(e.historyPath)
	if err != nil {
		t.Fatal(err)
	}
	prompt := "changed before the crash"
	updated, err := e.m.Update(bg, f.ID, UpdateRequest{Prompt: &prompt})
	if err != nil {
		t.Fatal(err)
	}
	// The crash: the snapshot reached disk but its event did not.
	if err := os.WriteFile(e.historyPath, saved, 0o600); err != nil {
		t.Fatal(err)
	}

	e.reload(t)

	events := e.historyEvents(t)
	last := events[len(events)-1]
	if last.Change != flighthistory.ChangeRecovered || last.Actor != flighthistory.ActorSystem || last.Cause != "recovery" {
		t.Fatalf("last event = %+v, want system flight_recovered/recovery", last)
	}
	if !last.RecordedAt.After(updated.UpdatedAt) {
		t.Errorf("recovery RecordedAt %s was backdated to the original change %s", last.RecordedAt, updated.UpdatedAt)
	}
	if last.SnapshotSHA256 != snapshotDigest(t, e.path) {
		t.Errorf("recovery digest = %s, want current snapshot digest", last.SnapshotSHA256)
	}
	assertRecorded(t, last, updated)
	if len(events) != 2 {
		t.Errorf("events = %d, want created + recovered", len(events))
	}

	e.reload(t)
	if got := len(e.historyEvents(t)); got != len(events) {
		t.Errorf("a clean second load appended events: %d, want %d", got, len(events))
	}
}

func TestHistoryAppendFailureLeavesSnapshotVisibleAndRecoverable(t *testing.T) {
	e := newEnv(t)
	f := mustCreate(t, e.m, goodCreate())
	saved, err := os.ReadFile(e.historyPath)
	if err != nil {
		t.Fatal(err)
	}
	// A directory where the ledger file belongs makes every append fail.
	if err := os.Remove(e.historyPath); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(e.historyPath, 0o700); err != nil {
		t.Fatal(err)
	}

	prompt := "saved without an event"
	if _, err := e.m.Update(bg, f.ID, UpdateRequest{Prompt: &prompt}); err == nil {
		t.Fatal("Update succeeded although the history append failed")
	}

	got, ok := e.m.Get(f.ID)
	if !ok || got.Prompt != prompt {
		t.Fatalf("in-memory flight = %+v, want the saved prompt", got)
	}
	disk, err := os.ReadFile(e.path)
	if err != nil || !bytes.Contains(disk, []byte(prompt)) {
		t.Fatalf("snapshot on disk lost the edit: err=%v", err)
	}

	if err := os.Remove(e.historyPath); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(e.historyPath, saved, 0o600); err != nil {
		t.Fatal(err)
	}
	e.reload(t)

	events := e.historyEvents(t)
	last := events[len(events)-1]
	if last.Change != flighthistory.ChangeRecovered || filepath.Base(e.historyPath) != "events.jsonl" {
		t.Fatalf("last event = %+v, want flight_recovered", last)
	}
	assertRecorded(t, last, got)
}

func TestHistoryAppendFailureIsRecoveredByLaterSuccessfulAppend(t *testing.T) {
	e := newEnv(t)
	f := mustCreate(t, e.m, goodCreate())
	saved, err := os.ReadFile(e.historyPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(e.historyPath); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(e.historyPath, 0o700); err != nil {
		t.Fatal(err)
	}
	lostPrompt := "saved without an event"
	if _, err := e.m.Update(bg, f.ID, UpdateRequest{Prompt: &lostPrompt}); err == nil {
		t.Fatal("Update succeeded although the history append failed")
	}
	lost := mustGet(t, e.m, f.ID)
	if err := os.Remove(e.historyPath); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(e.historyPath, saved, 0o600); err != nil {
		t.Fatal(err)
	}
	laterPrompt := "changed after history recovered"
	later, err := e.m.Update(bg, f.ID, UpdateRequest{Prompt: &laterPrompt})
	if err != nil {
		t.Fatalf("later Update: %v", err)
	}

	events := e.historyEvents(t)
	if len(events) != 3 {
		t.Fatalf("events = %+v, want created, recovered, updated", events)
	}
	recovered := events[1]
	if recovered.Change != flighthistory.ChangeRecovered || recovered.Cause != "recovery" {
		t.Fatalf("recovered event = %+v", recovered)
	}
	assertRecorded(t, recovered, lost)
	assertRecorded(t, events[2], later)
	if recovered.RowSHA256 == events[2].RowSHA256 {
		t.Errorf("the recovered event names the later row, not the lost one")
	}
}

func mustGet(t *testing.T, m *Manager, id string) Flight {
	t.Helper()
	f, ok := m.Get(id)
	if !ok {
		t.Fatalf("flight %s not found", id)
	}
	return f
}
