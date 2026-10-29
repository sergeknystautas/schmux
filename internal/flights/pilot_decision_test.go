package flights

import (
	"encoding/json"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/sergeknystautas/schmux/internal/chat"
)

// errNoCapacity is the spawn failure the tests script.
var errNoCapacity = errors.New("no capacity")

// decisionExchange wires runtimes for the sessions of a flight in a phase,
// delivers a notification from the phase's working session to the pilot, and
// plays pilot decisions.
type decisionExchange struct {
	t          *testing.T
	e          *testEnv
	flightID   string
	pilot      string
	sourceRole SessionRole
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
	if _, err := e.m.deliverFlightMessage(bg, f.ID, notification.ID); err != nil {
		t.Fatal(err)
	}
	return x
}

// respond records response as the pilot's next completed turn and processes
// it. The turn begins inside the flight's current phase visit, so the daemon
// can bind it to that visit.
func (x *decisionExchange) respond(response string) MonitoredTurn {
	x.t.Helper()
	return x.respondTurn(response, "completed", "", x.trustedStart())
}

// respondTurn records the pilot's next turn with the given outcome, harness
// error, and start boundary, and processes it.
func (x *decisionExchange) respondTurn(response, outcome, turnErr string, startedAt time.Time) MonitoredTurn {
	x.t.Helper()
	x.nextSeq += 2
	id, err := x.e.m.recordPlannerTurn(x.flightID, PlannerTurn{
		SessionID: x.pilot, StartSeq: x.nextSeq - 1, EndSeq: x.nextSeq,
		Text: response, FinalText: response, Outcome: outcome, Error: turnErr,
		StartedAt: startedAt,
	})
	if err != nil {
		x.t.Fatal(err)
	}
	turn, _ := x.e.m.findMonitoredTurn(x.flightID, id)
	x.e.m.processPilotTurn(bg, x.flightID, turn)
	processed, _ := x.e.m.findMonitoredTurn(x.flightID, id)
	return processed
}

// trustedStart is a start boundary inside the flight's current phase visit:
// later than the visit's boundary and not later than the moment the turn is
// retained.
func (x *decisionExchange) trustedStart() time.Time {
	x.e.advanceClock(10)
	return x.e.now()
}

// decide records a decision with the given fields and processes it, then
// delivers whatever the decision produced.
func (x *decisionExchange) decide(fields map[string]any) MonitoredTurn {
	x.t.Helper()
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

// messages returns the flight's monitor messages of one kind, in order.
func (x *decisionExchange) messages(kind string) []FlightMessage {
	x.t.Helper()
	got, _ := x.e.m.Get(x.flightID)
	var out []FlightMessage
	for _, msg := range got.Monitor.Messages {
		if msg.Kind == kind {
			out = append(out, msg)
		}
	}
	return out
}

// The decision's three forms and their combinations.
func TestPilotDecisionValidation(t *testing.T) {
	for _, tc := range []struct {
		name     string
		response string
		wantErr  string
	}{
		{name: "malformed JSON", response: `{"guidance":`, wantErr: "not a valid JSON decision"},
		{name: "trailing object", response: `{"guidance":null}{"to":"reviewer","guidance":"x"}`, wantErr: "must contain one JSON object"},
		{name: "unknown field", response: `{"guidance":null,"recipient":"another"}`, wantErr: "not a valid JSON decision"},
		{name: "no form", response: `{"notification_id":"x"}`, wantErr: "requires guidance"},
		{name: "two forms", response: `{"guidance":"x","to":"reviewer","stop":"off the rails"}`, wantErr: "exactly one of guidance, next_reviewer, stop, or advance"},
		{name: "guidance and next reviewer", response: `{"guidance":"x","to":"reviewer","next_reviewer":"y"}`, wantErr: "exactly one of guidance, next_reviewer, stop, or advance"},
		{name: "next reviewer and stop", response: `{"next_reviewer":"y","stop":"z"}`, wantErr: "exactly one of guidance, next_reviewer, stop, or advance"},
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

// A response that is not a JSON object is the pilot talking to the user. It
// is retained, not parsed, and sends nothing; it is not an error and nothing
// goes back to the pilot.
func TestPilotProseIsNotADecision(t *testing.T) {
	for name, response := range map[string]string{
		"prose":           "I read the reviewer's conversation; the tests ran after its last change.",
		"prose with json": "Here is what I would send:\n{\"to\":\"reviewer\",\"guidance\":\"x\"}",
	} {
		t.Run(name, func(t *testing.T) {
			x := newDecisionExchange(t, StateReviewing)

			processed := x.respond(response)

			if processed.Status != MonitoredTurnSkipped || processed.Attempts[0].Error != "" {
				t.Fatalf("prose status = %s attempts = %+v; want skipped with no error", processed.Status, processed.Attempts)
			}
			if got := x.messages("rejection"); len(got) != 0 {
				t.Fatalf("prose was returned to the pilot: %+v", got)
			}
			if got := x.messages("pilot_message"); len(got) != 0 {
				t.Fatalf("prose sent a message: %+v", got)
			}
		})
	}
}

// A decision the model wrapped in a code fence despite the prompt is still a
// decision.
func TestPilotFencedDecisionIsApplied(t *testing.T) {
	x := newDecisionExchange(t, StateReviewing)

	processed := x.respond("```json\n{\"to\":\"reviewer\",\"guidance\":\"Re-check the parser.\"}\n```")

	if processed.Status == MonitoredTurnFailed {
		t.Fatalf("fenced decision was rejected: %+v", processed.Attempts)
	}
	x.deliverPending()
	if got := x.input(RoleReviewer); !strings.Contains(got, "[Pilot] Re-check the parser.") {
		t.Fatalf("reviewer input = %s", got)
	}
}

// Null guidance sends nothing and is not an error.
func TestPilotNullGuidanceSendsNothing(t *testing.T) {
	x := newDecisionExchange(t, StateReviewing)

	processed := x.respond(`{"guidance":null}`)

	if processed.Status == MonitoredTurnFailed {
		t.Fatalf("null guidance was rejected: %+v", processed.Attempts)
	}
	if got := x.messages("pilot_message"); len(got) != 0 {
		t.Fatalf("null guidance sent a message: %+v", got)
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

// While the flight completes, guidance may also go to the reviewer.
func TestPilotGuidanceReachesTheReviewerWhileCompleting(t *testing.T) {
	x := newDecisionExchange(t, StateCompleting)
	f, _ := x.e.m.Get(x.flightID)
	reviewer := f.sessionFor(RoleReviewer).SessionID
	reviewerRT, reviewerPaths := monitorRuntime(t, reviewer)
	x.e.fk.chats.setRuntime(reviewer, reviewerRT)
	x.e.fk.chats.set(reviewer, true)
	x.inputs[string(RoleReviewer)] = reviewerPaths.Input

	processed := x.decide(map[string]any{"to": "reviewer", "guidance": "Fix the lint failure."})

	if processed.Status == MonitoredTurnFailed {
		t.Fatalf("guidance was rejected: %+v", processed.Attempts)
	}
	if got := x.input(RoleReviewer); !strings.Contains(got, "[Pilot] Fix the lint failure.") {
		t.Fatalf("reviewer input = %s", got)
	}
	if got := x.input(RoleCommit); strings.Contains(got, pilotPrefix) {
		t.Fatalf("committer received guidance addressed to the reviewer: %s", got)
	}
}

// The pilot may not address a role whose session the flight has not started;
// the rejection names the roles it has.
func TestPilotGuidanceRejectsRolesWithNoStartedSession(t *testing.T) {
	for _, tc := range []struct {
		phase   WorkflowState
		to      string
		started string
	}{
		{StatePlanning, "executor", `requires "to": planner`},
		{StatePlanning, "reviewer", `requires "to": planner`},
		{StateExecuting, "reviewer", `requires "to": planner or executor`},
		{StateReviewing, "commit", `requires "to": planner or executor or reviewer`},
	} {
		t.Run(string(tc.phase)+"-to-"+tc.to, func(t *testing.T) {
			x := newDecisionExchange(t, tc.phase)
			processed := x.decide(map[string]any{"to": tc.to, "guidance": "Do the thing."})
			if processed.Status != MonitoredTurnFailed {
				t.Fatalf("guidance to %s during %s was accepted", tc.to, tc.phase)
			}
			if !strings.Contains(processed.Attempts[0].Error, tc.started) {
				t.Fatalf("decision error = %q, want %q", processed.Attempts[0].Error, tc.started)
			}
		})
	}
}

// The pilot orchestrates every session the flight has started: an earlier
// phase's session stays addressable in a later phase.
func TestPilotGuidanceReachesAnyStartedSession(t *testing.T) {
	x := newDecisionExchange(t, StateCompleting)
	f, _ := x.e.m.Get(x.flightID)
	for _, role := range []SessionRole{RolePlanner, RoleExecutor, RoleReviewer} {
		id := f.sessionFor(role).SessionID
		rt, paths := monitorRuntime(t, id)
		x.e.fk.chats.setRuntime(id, rt)
		x.e.fk.chats.set(id, true)
		x.inputs[string(role)] = paths.Input
	}

	for _, role := range []SessionRole{RolePlanner, RoleExecutor, RoleReviewer, RoleCommit} {
		processed := x.decide(map[string]any{"to": string(role), "guidance": "Note for the " + string(role) + "."})
		if processed.Status == MonitoredTurnFailed {
			t.Fatalf("guidance to the %s while completing was rejected: %+v", role, processed.Attempts)
		}
		if got := x.input(role); !strings.Contains(got, "[Pilot] Note for the "+string(role)+".") {
			t.Fatalf("%s input = %s", role, got)
		}
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
	// The decision is retained in the monitor history, and the pilot is told
	// about the new reviewer the way it was told about the first.
	decisions := x.messages("next_reviewer")
	if len(decisions) != 1 || !strings.Contains(decisions[0].Text, "changed the parser") {
		t.Fatalf("retained next-reviewer decisions = %+v", decisions)
	}
	notices := x.messages("review_notice")
	if len(notices) != 2 || notices[1].RecipientSessionID != x.pilot || !strings.Contains(notices[1].Text, reviewer.SessionID) {
		t.Fatalf("review notices = %+v; want a second one naming %s", notices, reviewer.SessionID)
	}
	// Replaying the same pilot turn starts no second reviewer.
	spawns := x.e.fk.spawner.callCount()
	turn, _ := x.e.m.findMonitoredTurn(x.flightID, processed.ID)
	x.e.m.processPilotTurn(bg, x.flightID, turn)
	replayed, _ := x.e.m.Get(x.flightID)
	if x.e.fk.spawner.callCount() != spawns || replayed.sessionFor(RoleReviewer).SessionID != reviewer.SessionID {
		t.Fatalf("replay spawned again: spawns %d → %d, reviewer %s → %s", spawns, x.e.fk.spawner.callCount(), reviewer.SessionID, replayed.sessionFor(RoleReviewer).SessionID)
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

// The pilot decides; schmux does not tie a decision to the message that
// prompted it. A notification ID the pilot copies, stale or not, is ignored:
// a flight stalled when a next_reviewer naming an executor notification was
// rejected during review.
func TestPilotNextReviewerIgnoresACopiedNotificationID(t *testing.T) {
	x := newDecisionExchange(t, StateReviewing)
	old := x.e.m.List()[0].sessionFor(RoleReviewer).SessionID

	processed := x.respond(`{"notification_id":"msg-from-execution","next_reviewer":"go again"}`)

	if processed.Status == MonitoredTurnFailed {
		t.Fatalf("next_reviewer with a copied notification ID was rejected: %+v", processed.Attempts)
	}
	got, _ := x.e.m.Get(x.flightID)
	reviewer := got.sessionFor(RoleReviewer)
	if reviewer.SessionID == old || len(reviewer.PriorSessionIDs) != 1 || reviewer.PriorSessionIDs[0] != old {
		t.Fatalf("reviewer assignment = %+v; want a fresh reviewer with the old one as history", reviewer)
	}
	if decisions := x.messages("next_reviewer"); len(decisions) != 1 || decisions[0].SourceTurnID != processed.ID {
		t.Fatalf("next-reviewer decisions = %+v; want one from turn %s", decisions, processed.ID)
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
			if got.PilotStop.SourceTurnID != processed.ID {
				t.Errorf("stop source = %+v, want turn %s", got.PilotStop, processed.ID)
			}
			if got.WorkflowState != phase {
				t.Errorf("state = %s, want %s", got.WorkflowState, phase)
			}
			if stops := x.messages("pilot_stop"); len(stops) != 1 || stops[0].SourceTurnID != processed.ID || stops[0].Text != "the agent keeps changing the same file" {
				t.Errorf("stop messages = %+v, want one from turn %s", stops, processed.ID)
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
				// Completing is the last phase; only dispose follows, and a
				// stop never gates it (see TestDispose_AllStartedStagesCanBeDisposed).
			}
		})
	}
}

// A decision error sends nothing to the pilot; it is the turn's visible error.
func TestDecisionErrorSendsNothingToThePilot(t *testing.T) {
	x := newDecisionExchange(t, StateReviewing)

	processed := x.respond(`{"guidance":"x"}`)

	if processed.Status != MonitoredTurnFailed {
		t.Fatal("a decision without \"to\" was accepted")
	}
	if rejections := x.messages("rejection"); len(rejections) != 0 {
		t.Fatalf("a rejection was sent to the pilot: %+v", rejections)
	}
}

func TestPrequeueAgentMessageFailureNotification(t *testing.T) {
	for _, tc := range []struct {
		name, to, wantErr string
		stopped           bool
	}{
		{name: "stopped recipient", to: "executor", stopped: true, wantErr: "executor session is not running"},
		{name: "unknown recipient", to: "foo", wantErr: `requires "to"`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			x := newDecisionExchange(t, StateExecuting)
			f, _ := x.e.m.Get(x.flightID)
			executorID := f.sessionFor(RoleExecutor).SessionID
			if tc.stopped {
				x.e.fk.chats.set(executorID, false)
			}

			processed := x.respond(`{"to":"` + tc.to + `","guidance":"Continue."}`)

			if processed.Status != MonitoredTurnFailed || !strings.Contains(processed.Attempts[0].Error, tc.wantErr) {
				t.Fatalf("turn=%+v; want failed turn containing %q", processed, tc.wantErr)
			}
			if messages := x.messages("pilot_message"); len(messages) != 0 {
				t.Fatalf("failed pre-queue send recorded guidance=%+v", messages)
			}
			got, _ := x.e.m.Get(x.flightID)
			var notices []FlightMessage
			for _, msg := range got.Monitor.Messages {
				if strings.HasPrefix(msg.Text, "Your message to ") {
					notices = append(notices, msg)
				}
			}
			if !tc.stopped {
				if len(notices) != 0 {
					t.Fatalf("validation failure produced delivery notices=%+v", notices)
				}
				return
			}

			want := "Your message to " + executorID + " was not delivered: flight state conflict: executor session is not running."
			if len(notices) != 1 || notices[0].RecipientSessionID != x.pilot || notices[0].SenderSessionID != "" || notices[0].Text != want {
				t.Fatalf("delivery notices=%+v; want one to pilot %s with text %q", notices, x.pilot, want)
			}
			x.deliverPending()
			// The pilot's earlier notification left a turn open, so the notice is
			// held behind it: it is in the pilot's conversation, not yet in its input.
			pilotRT, err := x.e.fk.chats.GetChatRuntime(x.pilot)
			if err != nil {
				t.Fatal(err)
			}
			sub, err := pilotRT.Subscribe(0)
			if err != nil {
				t.Fatal(err)
			}
			defer pilotRT.Unsubscribe(sub.Live)
			delivered := false
			for _, rec := range sub.History {
				if rec.Type == chat.RecordUserMessage && rec.Text == want {
					delivered = true
				}
			}
			if !delivered {
				t.Fatalf("pilot conversation lacks notification %q", want)
			}
		})
	}
}
