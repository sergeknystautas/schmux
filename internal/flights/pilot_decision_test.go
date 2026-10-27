package flights

import (
	"encoding/json"
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/sergeknystautas/schmux/internal/chat"
)

// errNoCapacity is the spawn failure the tests script.
var errNoCapacity = errors.New("no capacity")

// decisionExchange wires runtimes for the sessions of a flight in a phase and
// plays pilot decisions against a delivered notification from one of them.
type decisionExchange struct {
	t          *testing.T
	e          *testEnv
	flightID   string
	pilot      string
	sourceRole SessionRole
	notifyID   string
	answering  string
	nextSeq    uint64
	inputs     map[string]string // role → input path
}

// newDecisionExchange puts a flight into the given phase and delivers a
// notification about that phase's working session to the pilot.
func newDecisionExchange(t *testing.T, phase WorkflowState) *decisionExchange {
	t.Helper()
	e := newEnv(t)
	var f Flight
	switch phase {
	case StatePlanning:
		f = e.reportPlanning(t, takeoff(t, e))
	case StateExecuting:
		f = execute(t, e)
	case StateReviewing:
		f = review(t, e)
	case StateCompleting:
		f = complete(t, e)
	default:
		t.Fatalf("unsupported phase %s", phase)
	}
	x := &decisionExchange{t: t, e: e, flightID: f.ID, nextSeq: 100, inputs: map[string]string{}}
	x.pilot = f.sessionFor(RolePilot).SessionID
	pilotRT, pilotPaths := monitorRuntime(t, x.pilot)
	e.fk.chats.setRuntime(x.pilot, pilotRT)
	e.fk.chats.set(x.pilot, true)
	x.inputs[string(RolePilot)] = pilotPaths.Input
	switch phase {
	case StatePlanning:
		x.sourceRole = RolePlanner
	case StateExecuting:
		x.sourceRole = RoleExecutor
	case StateReviewing:
		x.sourceRole = RoleReviewer
	case StateCompleting:
		x.sourceRole = RoleCommit
	}
	source := f.sessionFor(x.sourceRole)
	sourceRT, sourcePaths := monitorRuntime(t, source.SessionID)
	e.fk.chats.setRuntime(source.SessionID, sourceRT)
	e.fk.chats.set(source.SessionID, true)
	x.inputs[string(x.sourceRole)] = sourcePaths.Input
	turn, err := e.m.recordPlannerTurn(f.ID, PlannerTurn{SessionID: source.SessionID, StartSeq: 1, EndSeq: 2, Text: "Turn.", Outcome: "completed"})
	if err != nil {
		t.Fatal(err)
	}
	if err := e.m.recordMonitorAttempt(f.ID, turn, MonitorAttempt{}); err != nil {
		t.Fatal(err)
	}
	notification, err := e.m.recordFlightMessage(f.ID, FlightMessage{Kind: "notification", SourceTurnID: turn, RecipientSessionID: x.pilot, Text: "A turn."})
	if err != nil {
		t.Fatal(err)
	}
	notification, err = e.m.deliverFlightMessage(bg, f.ID, notification.ID)
	if err != nil {
		t.Fatal(err)
	}
	x.notifyID, x.answering = notification.ID, notification.ChatMessageID
	return x
}

// respond records response as the pilot's next completed turn, answering the
// schmux message in x.answering, and processes it.
func (x *decisionExchange) respond(response string) MonitoredTurn {
	x.t.Helper()
	x.nextSeq += 2
	id, err := x.e.m.recordPlannerTurn(x.flightID, PlannerTurn{
		SessionID: x.pilot, StartSeq: x.nextSeq - 1, EndSeq: x.nextSeq,
		Text: response, FinalText: response, Outcome: "completed", InputIDs: []string{x.answering},
	})
	if err != nil {
		x.t.Fatal(err)
	}
	turn, _ := x.e.m.findMonitoredTurn(x.flightID, id)
	x.e.m.processPilotTurn(bg, x.flightID, turn)
	processed, _ := x.e.m.findMonitoredTurn(x.flightID, id)
	return processed
}

// decide records a decision with the given fields and processes it, then
// delivers whatever the decision produced.
func (x *decisionExchange) decide(fields map[string]any) MonitoredTurn {
	x.t.Helper()
	fields["notification_id"] = x.notifyID
	body, err := json.Marshal(fields)
	if err != nil {
		x.t.Fatal(err)
	}
	processed := x.respond(string(body))
	x.deliverPending()
	return processed
}

// deliverPending delivers the messages the decision left pending.
func (x *decisionExchange) deliverPending() {
	x.t.Helper()
	got, _ := x.e.m.Get(x.flightID)
	if got.Monitor == nil {
		return
	}
	for _, msg := range got.Monitor.Messages {
		if msg.Status != MessageStatusPending {
			continue
		}
		if _, err := x.e.m.deliverFlightMessage(bg, x.flightID, msg.ID); err != nil {
			x.t.Fatalf("deliver %s: %v", msg.ID, err)
		}
	}
}

// input reads a session's chat input.
func (x *decisionExchange) input(role SessionRole) string {
	x.t.Helper()
	data, err := os.ReadFile(x.inputs[string(role)])
	if err != nil {
		x.t.Fatal(err)
	}
	return string(data)
}

// The decision's three forms and their combinations.
func TestPilotDecisionValidation(t *testing.T) {
	for _, tc := range []struct {
		name     string
		response string
		wantErr  string
	}{
		{name: "prose", response: "Check the spec.", wantErr: "prose, not a JSON decision"},
		{name: "malformed JSON", response: `{"guidance":`, wantErr: "not a valid JSON decision"},
		{name: "trailing object", response: `{"guidance":null}{"to":"reviewer","guidance":"x"}`, wantErr: "must contain one JSON object"},
		{name: "unknown field", response: `{"guidance":null,"recipient":"another"}`, wantErr: "not a valid JSON decision"},
		{name: "no form", response: `{"notification_id":"x"}`, wantErr: "requires guidance"},
		{name: "two forms", response: `{"guidance":"x","to":"reviewer","stop":"off the rails"}`, wantErr: "exactly one of guidance, next_reviewer, or stop"},
		{name: "guidance and next reviewer", response: `{"guidance":"x","to":"reviewer","next_reviewer":"y"}`, wantErr: "exactly one of guidance, next_reviewer, or stop"},
		{name: "next reviewer and stop", response: `{"next_reviewer":"y","stop":"z"}`, wantErr: "exactly one of guidance, next_reviewer, or stop"},
		{name: "guidance without to", response: `{"guidance":"x"}`, wantErr: `requires "to"`},
		{name: "blank guidance", response: `{"guidance":"  ","to":"reviewer"}`, wantErr: "must be nonblank or null"},
		{name: "non-string guidance", response: `{"guidance":true,"to":"reviewer"}`, wantErr: "pilot guidance"},
		{name: "next reviewer with to", response: `{"next_reviewer":"y","to":"reviewer"}`, wantErr: `must not name a "to" recipient`},
		{name: "blank next reviewer", response: `{"next_reviewer":"  "}`, wantErr: "next_reviewer must be nonblank"},
		{name: "non-string next reviewer", response: `{"next_reviewer":7}`, wantErr: "next_reviewer must be a string"},
		{name: "stop with to", response: `{"stop":"z","to":"reviewer"}`, wantErr: `must not name a "to" recipient`},
		{name: "blank stop", response: `{"stop":"  "}`, wantErr: "stop must be nonblank"},
		{name: "non-string stop", response: `{"stop":7}`, wantErr: "stop must be a string"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			x := newDecisionExchange(t, StateReviewing)
			processed := x.respond(tc.response)
			if processed.Status != MonitoredTurnFailed {
				t.Fatalf("decision status = %s, want failed", processed.Status)
			}
			if !strings.Contains(processed.Attempts[0].Error, tc.wantErr) {
				t.Fatalf("decision error = %q, want containing %q", processed.Attempts[0].Error, tc.wantErr)
			}
		})
	}
}

// Null guidance sends nothing and is not an error.
func TestPilotNullGuidanceSendsNothing(t *testing.T) {
	x := newDecisionExchange(t, StateReviewing)

	processed := x.respond(`{"guidance":null}`)

	if processed.Status == MonitoredTurnFailed {
		t.Fatalf("null guidance was rejected: %+v", processed.Attempts)
	}
	got, _ := x.e.m.Get(x.flightID)
	for _, msg := range got.Monitor.Messages {
		if msg.Kind == "pilot_message" {
			t.Fatalf("null guidance sent a message: %+v", msg)
		}
	}
}

// Guidance reaches the reviewer while the flight reviews.
func TestPilotGuidanceReachesTheReviewer(t *testing.T) {
	x := newDecisionExchange(t, StateReviewing)

	processed := x.decide(map[string]any{"to": "reviewer", "guidance": "Re-check the parser."})

	if processed.Status == MonitoredTurnFailed {
		t.Fatalf("guidance was rejected: %+v", processed.Attempts)
	}
	if got := x.input(RoleReviewer); !strings.Contains(got, "[Pilot] Re-check the parser.") {
		t.Fatalf("reviewer input = %s", got)
	}
}

// Guidance reaches the committer while the flight completes.
func TestPilotGuidanceReachesTheCommitter(t *testing.T) {
	x := newDecisionExchange(t, StateCompleting)

	processed := x.decide(map[string]any{"to": "commit", "guidance": "Do not rerun the gates."})

	if processed.Status == MonitoredTurnFailed {
		t.Fatalf("guidance was rejected: %+v", processed.Attempts)
	}
	if got := x.input(RoleCommit); !strings.Contains(got, "[Pilot] Do not rerun the gates.") {
		t.Fatalf("committer input = %s", got)
	}
}

// The pilot may not address a role the phase does not run.
func TestPilotGuidanceRejectsRolesThePhaseDoesNotRun(t *testing.T) {
	for _, tc := range []struct {
		phase WorkflowState
		to    string
	}{
		{StatePlanning, "executor"},
		{StateExecuting, "planner"},
		{StateReviewing, "executor"},
		{StateCompleting, "executor"},
	} {
		t.Run(string(tc.phase)+"-to-"+tc.to, func(t *testing.T) {
			x := newDecisionExchange(t, tc.phase)
			processed := x.decide(map[string]any{"to": tc.to, "guidance": "Do the thing."})
			if processed.Status != MonitoredTurnFailed {
				t.Fatalf("guidance to %s during %s was accepted", tc.to, tc.phase)
			}
			if !strings.Contains(processed.Attempts[0].Error, `requires "to"`) {
				t.Fatalf("decision error = %q, want the valid recipients", processed.Attempts[0].Error)
			}
		})
	}
}

// A next-reviewer decision starts a fresh reviewer and keeps the old one as
// history.
func TestPilotNextReviewerStartsAFreshReviewer(t *testing.T) {
	x := newDecisionExchange(t, StateReviewing)
	old := x.e.m.List()[0].sessionFor(RoleReviewer).SessionID

	processed := x.decide(map[string]any{"next_reviewer": "The previous reviewer changed the parser; re-check it."})

	if processed.Status == MonitoredTurnFailed {
		t.Fatalf("next_reviewer was rejected: %+v", processed.Attempts)
	}
	got, _ := x.e.m.Get(x.flightID)
	reviewer := got.sessionFor(RoleReviewer)
	if reviewer.SessionID == old {
		t.Fatalf("reviewer session = %q, want a fresh one", reviewer.SessionID)
	}
	if len(reviewer.PriorSessionIDs) != 1 || reviewer.PriorSessionIDs[0] != old {
		t.Fatalf("reviewer history = %v, want the superseded reviewer", reviewer.PriorSessionIDs)
	}
	if got.WorkflowState != StateReviewing {
		t.Fatalf("state = %s, want reviewing", got.WorkflowState)
	}
	op := lastOp(t, got, ActionNextReview)
	if op.Status != StatusSucceeded {
		t.Fatalf("next-review operation = %+v", op)
	}
	// The new reviewer receives the pilot's paragraph.
	spawn := x.e.fk.spawner.last()
	if !strings.Contains(spawn.Prompt, "The previous reviewer changed the parser; re-check it.") {
		t.Fatalf("reviewer prompt lacks the pilot's paragraph: %q", spawn.Prompt)
	}
	if !strings.Contains(spawn.Prompt, "Review the whole change again") {
		t.Fatalf("reviewer prompt does not tell the later reviewer to re-review: %q", spawn.Prompt)
	}
	// The decision is retained in the monitor history.
	var decisions []FlightMessage
	for _, msg := range got.Monitor.Messages {
		if msg.Kind == "next_reviewer" {
			decisions = append(decisions, msg)
		}
	}
	if len(decisions) != 1 || !strings.Contains(decisions[0].Text, "changed the parser") {
		t.Fatalf("retained next-reviewer decisions = %+v", decisions)
	}
}

// The next reviewer is spawned even when the flight is completing, so a
// failed commit gate goes through the same convergence loop.
func TestPilotNextReviewerRepairsAFailedGate(t *testing.T) {
	x := newDecisionExchange(t, StateCompleting)
	old := x.e.m.List()[0].sessionFor(RoleReviewer).SessionID

	processed := x.decide(map[string]any{"next_reviewer": "The commit gate failed on the lint step."})

	if processed.Status == MonitoredTurnFailed {
		t.Fatalf("next_reviewer was rejected: %+v", processed.Attempts)
	}
	got, _ := x.e.m.Get(x.flightID)
	reviewer := got.sessionFor(RoleReviewer)
	if reviewer.SessionID == old || len(reviewer.PriorSessionIDs) != 1 {
		t.Fatalf("reviewer assignment = %+v, want a fresh reviewer with the old one as history", reviewer)
	}
	if got.WorkflowState != StateCompleting {
		t.Fatalf("state = %s, want completing", got.WorkflowState)
	}
}

// A next-reviewer decision must answer a notification about the phase's
// working session.
func TestPilotNextReviewerRejectsTheWrongSource(t *testing.T) {
	x := newDecisionExchange(t, StateReviewing)

	processed := x.respond(`{"notification_id":"msg-missing","next_reviewer":"go again"}`)

	if processed.Status != MonitoredTurnFailed {
		t.Fatalf("next_reviewer with an unknown notification was accepted")
	}
	if !strings.Contains(processed.Attempts[0].Error, "must reference a delivered notification") {
		t.Fatalf("decision error = %q", processed.Attempts[0].Error)
	}
	got, _ := x.e.m.Get(x.flightID)
	for _, op := range got.Operations {
		if op.Action == ActionNextReview {
			t.Fatalf("a rejected decision still recorded a next-review operation: %+v", op)
		}
	}
	if spawns := x.e.fk.spawner.callCount(); spawns != 4 {
		t.Fatalf("spawns = %d, want the four takeoff and phase spawns only", spawns)
	}
}

// A spawn failure leaves the current reviewer and the phase unchanged.
func TestPilotNextReviewerSpawnFailureIsReported(t *testing.T) {
	x := newDecisionExchange(t, StateReviewing)
	old := x.e.m.List()[0].sessionFor(RoleReviewer).SessionID
	x.e.fk.spawner.errs = []error{errNoCapacity}

	processed := x.decide(map[string]any{"next_reviewer": "go again"})

	if processed.Status != MonitoredTurnFailed {
		t.Fatalf("a failed spawn was accepted: %+v", processed.Attempts)
	}
	if !strings.Contains(processed.Attempts[0].Error, errNoCapacity.Error()) {
		t.Fatalf("decision error = %q, want the spawn failure", processed.Attempts[0].Error)
	}
	got, _ := x.e.m.Get(x.flightID)
	reviewer := got.sessionFor(RoleReviewer)
	if reviewer.SessionID != old || reviewer.Outcome != StatusSucceeded {
		t.Fatalf("reviewer assignment = %+v, want the current reviewer unchanged", reviewer)
	}
	if got.WorkflowState != StateReviewing {
		t.Fatalf("state = %s, want reviewing", got.WorkflowState)
	}
	op := lastOp(t, got, ActionNextReview)
	if op.Status != StatusFailed || !strings.Contains(op.Error, errNoCapacity.Error()) {
		t.Fatalf("next-review operation = %+v, want the spawn failure", op)
	}
}

// A stop records the pilot's reason and leaves every phase action available.
func TestPilotStopRecordsTheReasonAndKeepsActionsAvailable(t *testing.T) {
	for _, phase := range []WorkflowState{StatePlanning, StateExecuting, StateReviewing, StateCompleting} {
		t.Run(string(phase), func(t *testing.T) {
			x := newDecisionExchange(t, phase)

			processed := x.decide(map[string]any{"stop": "the agent keeps changing the same file"})

			if processed.Status == MonitoredTurnFailed {
				t.Fatalf("stop was rejected: %+v", processed.Attempts)
			}
			got, _ := x.e.m.Get(x.flightID)
			if got.PilotStop == nil {
				t.Fatal("stop was not recorded")
			}
			if got.PilotStop.Reason != "the agent keeps changing the same file" {
				t.Errorf("stop reason = %q", got.PilotStop.Reason)
			}
			if got.PilotStop.WorkflowState != phase {
				t.Errorf("stop phase = %s, want %s", got.PilotStop.WorkflowState, phase)
			}
			if got.PilotStop.SourceTurnID == "" || got.PilotStop.SourceNotificationID != x.notifyID {
				t.Errorf("stop source = %+v", got.PilotStop)
			}
			if got.WorkflowState != phase {
				t.Errorf("state = %s, want %s", got.WorkflowState, phase)
			}
			// The stop sends no chat message and cancels nothing.
			if got := x.input(x.sourceRole); strings.Contains(got, pilotPrefix) {
				t.Errorf("the stop sent a chat message: %s", got)
			}
			// The phase's own action is still offered after the stop.
			switch phase {
			case StatePlanning:
				if _, err := x.e.m.EnterExecution(bg, x.flightID); err != nil {
					t.Errorf("Enter execution after a stop: %v", err)
				}
			case StateExecuting:
				if _, err := x.e.m.EnterReview(bg, x.flightID); err != nil {
					t.Errorf("Enter review after a stop: %v", err)
				}
			case StateReviewing:
				if _, err := x.e.m.EnterCompletion(bg, x.flightID); err != nil {
					t.Errorf("Enter completion after a stop: %v", err)
				}
			case StateCompleting:
				if _, err := x.e.m.MarkDone(bg, x.flightID); err != nil {
					t.Errorf("Mark done after a stop: %v", err)
				}
			}
		})
	}
}

// A rejected decision tells the pilot the exact shapes it accepts.
func TestRejectedDecisionNamesTheValidShapes(t *testing.T) {
	x := newDecisionExchange(t, StateReviewing)

	processed := x.respond(`{"notification_id":"` + x.notifyID + `","guidance":"x"}`)

	if processed.Status != MonitoredTurnFailed {
		t.Fatal("a decision without \"to\" was accepted")
	}
	got, _ := x.e.m.Get(x.flightID)
	var rejection *FlightMessage
	for i, msg := range got.Monitor.Messages {
		if msg.Kind == "rejection" {
			rejection = &got.Monitor.Messages[i]
		}
	}
	if rejection == nil {
		t.Fatal("the rejected decision did not reach the pilot")
	}
	for _, want := range []string{`"guidance":"..."`, `"next_reviewer":"..."`, `"stop":"..."`, `{"guidance":null}`} {
		if !strings.Contains(rejection.Text, want) {
			t.Errorf("rejection lacks %q:\n%s", want, rejection.Text)
		}
	}
}

var _ = chat.ProtocolClaude
