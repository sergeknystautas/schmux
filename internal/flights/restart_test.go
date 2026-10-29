package flights

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestFollowSessionRestart_RepointsAssignmentAndEvidence(t *testing.T) {
	e := newEnv(t)
	f := takeoff(t, e)
	pilot, planner := f.sessionFor(RolePilot).SessionID, f.sessionFor(RolePlanner).SessionID
	turnID, err := e.m.recordPlannerTurn(f.ID, PlannerTurn{SessionID: planner, StartSeq: 1, EndSeq: 7, Text: "Plan ready", Outcome: "completed"})
	if err != nil {
		t.Fatal(err)
	}
	notification, err := e.m.recordFlightMessage(f.ID, FlightMessage{Kind: "notification", SourceTurnID: turnID, RecipientSessionID: pilot, Text: "n", Status: MessageStatusAccepted, ChatMessageID: "chat-1"})
	if err != nil {
		t.Fatal(err)
	}
	guidance, err := e.m.recordFlightMessage(f.ID, FlightMessage{Kind: "pilot_message", SourceTurnID: "turn-" + pilot + "-9", SenderSessionID: pilot, RecipientSessionID: planner, Text: "g", Status: MessageStatusPending})
	if err != nil {
		t.Fatal(err)
	}
	if err := e.m.recordMonitorSessionError(f.ID, planner, errors.New("session not found: "+planner)); err != nil {
		t.Fatal(err)
	}

	if err := e.m.FollowSessionRestart(planner, "planner-2"); err != nil {
		t.Fatalf("FollowSessionRestart() error = %v", err)
	}

	reloaded, ok := e.reload(t).Get(f.ID)
	if !ok {
		t.Fatal("flight missing after reload")
	}
	got := reloaded.sessionFor(RolePlanner)
	if got.SessionID != "planner-2" || len(got.PriorSessionIDs) != 1 || got.PriorSessionIDs[0] != planner || got.Outcome != StatusSucceeded {
		t.Fatalf("planner assignment = %+v; want planner-2 with prior %q", *got, planner)
	}
	if reloaded.sessionFor(RolePilot).SessionID != pilot {
		t.Fatalf("pilot assignment changed: %+v", *reloaded.sessionFor(RolePilot))
	}
	if len(reloaded.Monitor.Turns) != 1 {
		t.Fatalf("turns = %+v; want the one retained turn", reloaded.Monitor.Turns)
	}
	turn := reloaded.Monitor.Turns[0]
	if turn.Turn.SessionID != "planner-2" || turn.Turn.EndSeq != 7 || turn.ID != monitorTurnID(turn.Turn) || turn.ID == turnID {
		t.Fatalf("retained turn = %+v; want session planner-2 under its derived id", turn)
	}
	var byID = map[string]FlightMessage{}
	for _, msg := range reloaded.Monitor.Messages {
		byID[msg.ID] = msg
	}
	if n := byID[notification.ID]; n.RecipientSessionID != pilot || n.ChatMessageID != "chat-1" || n.SourceTurnID != turn.ID {
		t.Fatalf("notification = %+v; pilot recipient untouched, source turn renamed to %s", n, turn.ID)
	}
	if g := byID[guidance.ID]; g.RecipientSessionID != "planner-2" || g.SenderSessionID != pilot || g.Status != MessageStatusPending {
		t.Fatalf("guidance = %+v; want recipient planner-2 and pending delivery", g)
	}
	if reloaded.Monitor.Error != "" {
		t.Fatalf("monitor error naming the replaced session was kept: %q", reloaded.Monitor.Error)
	}
}

func TestFollowSessionRestart_UnknownSessionIsNoOp(t *testing.T) {
	e := newEnv(t)
	f := takeoff(t, e)
	before := e.notices.Load()
	if err := e.m.FollowSessionRestart("not-a-flight-session", "replacement"); err != nil {
		t.Fatalf("FollowSessionRestart() error = %v", err)
	}
	if e.notices.Load() != before {
		t.Fatalf("notifications = %d; want unchanged %d for an unreferenced session", e.notices.Load(), before)
	}
	after, _ := e.m.Get(f.ID)
	if after.sessionFor(RolePlanner).SessionID != f.sessionFor(RolePlanner).SessionID {
		t.Fatalf("planner assignment changed: %+v", *after.sessionFor(RolePlanner))
	}
	for _, bad := range [][2]string{{"", "x"}, {"x", ""}, {"x", "x"}} {
		if err := e.m.FollowSessionRestart(bad[0], bad[1]); err == nil {
			t.Fatalf("FollowSessionRestart(%q, %q) accepted invalid IDs", bad[0], bad[1])
		}
	}
}

func TestMonitorSessionError_RecordedOnceAndClearedPerSession(t *testing.T) {
	e := newEnv(t)
	f := takeoff(t, e)
	pilot, planner := f.sessionFor(RolePilot).SessionID, f.sessionFor(RolePlanner).SessionID
	before := e.notices.Load()
	err := errors.New("session not found: " + planner)
	if err := e.m.recordMonitorSessionError(f.ID, planner, err); err != nil {
		t.Fatal(err)
	}
	if err := e.m.recordMonitorSessionError(f.ID, planner, err); err != nil {
		t.Fatal(err)
	}
	if got := e.notices.Load() - before; got != 1 {
		t.Fatalf("identical error persisted %d times; want once", got)
	}
	recorded, _ := e.m.Get(f.ID)
	if !strings.Contains(recorded.Monitor.Error, planner) || !strings.Contains(recorded.Monitor.Error, "session not found") {
		t.Fatalf("monitor error = %q", recorded.Monitor.Error)
	}
	if err := e.m.clearMonitorSessionError(f.ID, pilot); err != nil {
		t.Fatal(err)
	}
	if kept, _ := e.m.Get(f.ID); kept.Monitor.Error == "" {
		t.Fatal("clearing the pilot's error removed the planner's error")
	}
	if err := e.m.clearMonitorSessionError(f.ID, planner); err != nil {
		t.Fatal(err)
	}
	if cleared, _ := e.m.Get(f.ID); cleared.Monitor.Error != "" {
		t.Fatalf("monitor error = %q; want cleared", cleared.Monitor.Error)
	}
	if got := e.notices.Load() - before; got != 2 {
		t.Fatalf("persisted %d times; want record once and clear once", got)
	}
}

// A session restart renames the retained turn and every reference to it. The
// visit and the start evidence stay where they were.
func TestRestartPreservesAdvanceProvenance(t *testing.T) {
	e := newEnv(t)
	f := takeoff(t, e)
	pilot := f.sessionFor(RolePilot).SessionID
	e.advanceClock(1000)
	start := t0.Add(500 * time.Second)
	turnID, err := e.m.recordPlannerTurn(f.ID, PlannerTurn{
		SessionID: pilot, StartSeq: 1, EndSeq: 2, StartedAt: start, Outcome: "completed",
	})
	if err != nil {
		t.Fatal(err)
	}
	turn, _ := e.m.findMonitoredTurn(f.ID, turnID)
	if err := e.m.editFlight(f.ID, func(f *Flight) error {
		f.Operations = append(f.Operations, Operation{
			ID: "op-auto", Action: ActionExecution, CorrelationID: "corr-1", Status: StatusSucceeded,
			SourceTurnID: turnID, PhaseVisit: f.PhaseVisit, CreatedAt: e.now(), UpdatedAt: e.now(),
		})
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if err := e.m.recordPilotStop(f.ID, turn, pilotDecision{Stop: json.RawMessage(`"the agent is looping"`)}); err != nil {
		t.Fatalf("recordPilotStop() error = %v", err)
	}

	if err := e.m.FollowSessionRestart(pilot, "pilot-2"); err != nil {
		t.Fatalf("FollowSessionRestart() error = %v", err)
	}

	reloaded, ok := e.reload(t).Get(f.ID)
	if !ok {
		t.Fatal("flight missing after reload")
	}
	renamed := monitorTurnID(PlannerTurn{SessionID: "pilot-2", StartSeq: 1, EndSeq: 2})
	if reloaded.Monitor.Turns[0].ID != renamed {
		t.Fatalf("retained turn id = %q, want %q", reloaded.Monitor.Turns[0].ID, renamed)
	}
	op := reloaded.Operations[len(reloaded.Operations)-1]
	if op.SourceTurnID != renamed {
		t.Fatalf("operation source turn = %q, want %q", op.SourceTurnID, renamed)
	}
	if reloaded.PilotStop == nil || reloaded.PilotStop.SourceTurnID != renamed {
		t.Fatalf("stop = %+v, want the renamed source turn %q", reloaded.PilotStop, renamed)
	}
	if op.PhaseVisit != reloaded.PhaseVisit {
		t.Fatalf("operation visit = %d, want %d", op.PhaseVisit, reloaded.PhaseVisit)
	}
	if reloaded.PilotStop.PhaseVisit != reloaded.PhaseVisit {
		t.Fatalf("stop visit = %d, want %d", reloaded.PilotStop.PhaseVisit, reloaded.PhaseVisit)
	}
	if !reloaded.Monitor.Turns[0].Turn.StartedAt.Equal(start) {
		t.Fatalf("turn start = %v, want %v", reloaded.Monitor.Turns[0].Turn.StartedAt, start)
	}
}
