package flights

import (
	"strings"
	"testing"
	"time"
)

// The advance decision has exactly two valid destinations. Everything else —
// a null, a number, a blank, another phase, a second form, or a recipient —
// is rejected before anything happens.
func TestPilotAdvanceDecision(t *testing.T) {
	for _, tc := range []struct {
		name     string
		response string
		wantErr  string
	}{
		{name: "advance to executing", response: `{"advance":"executing"}`},
		{name: "advance to reviewing", response: `{"advance":"reviewing"}`},
		{name: "null advance", response: `{"advance":null}`, wantErr: "pilot advance must be a string"},
		{name: "number advance", response: `{"advance":7}`, wantErr: "pilot advance must be a string"},
		{name: "blank advance", response: `{"advance":"  "}`, wantErr: "pilot advance must be nonblank"},
		{name: "completion is not automatic", response: `{"advance":"completing"}`, wantErr: "pilot advance must be executing or reviewing"},
		{name: "backlog is not automatic", response: `{"advance":"backlog"}`, wantErr: "pilot advance must be executing or reviewing"},
		{name: "advance with a recipient", response: `{"advance":"executing","to":"executor"}`, wantErr: `must not name a "to" recipient`},
		{name: "advance and guidance", response: `{"advance":"executing","guidance":null}`, wantErr: "exactly one of guidance, next_reviewer, stop, or advance"},
		{name: "advance and next reviewer", response: `{"advance":"executing","next_reviewer":"y"}`, wantErr: "exactly one of guidance, next_reviewer, stop, or advance"},
		{name: "advance and stop", response: `{"advance":"executing","stop":"z"}`, wantErr: "exactly one of guidance, next_reviewer, stop, or advance"},
		{name: "unknown field", response: `{"advance":"executing","recipient":"x"}`, wantErr: "not a valid JSON decision"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, form, err := decodePilotDecision(tc.response)
			if tc.wantErr == "" {
				if err != nil {
					t.Fatalf("decodePilotDecision(%s) error = %v", tc.response, err)
				}
				if form != formAdvance {
					t.Fatalf("form = %v, want formAdvance", form)
				}
				return
			}
			if err == nil {
				t.Fatalf("decodePilotDecision(%s) accepted the decision", tc.response)
			}
			if !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("error = %q, want containing %q", err, tc.wantErr)
			}
			if form != formNone {
				t.Fatalf("form = %v, want formNone", form)
			}
		})
	}
}

// assertAdvanceRejected checks that an advance decision was rejected with a
// visible error on the pilot's turn and started nothing.
func assertAdvanceRejected(t *testing.T, x *decisionExchange, processed MonitoredTurn, wantErr string, spawns int) {
	t.Helper()
	if processed.Status != MonitoredTurnFailed {
		t.Fatalf("the advance was accepted: %+v", processed.Attempts)
	}
	if len(processed.Attempts) == 0 || !strings.Contains(processed.Attempts[0].Error, wantErr) {
		t.Fatalf("decision error = %+v, want containing %q", processed.Attempts, wantErr)
	}
	if got := x.e.fk.spawner.callCount(); got != spawns {
		t.Fatalf("spawns = %d, want %d", got, spawns)
	}
}

// The pilot advances the flight from planning to executing and from executing
// to reviewing, and each advance starts one session and records one operation
// that names the deciding turn.
func TestPilotAdvanceFlow(t *testing.T) {
	t.Run("planning advances to executing", func(t *testing.T) {
		x := newDecisionExchange(t, StatePlanning)
		before := x.e.fk.spawner.callCount()

		processed := x.decide(map[string]any{"advance": "executing"})

		if processed.Status == MonitoredTurnFailed {
			t.Fatalf("the advance was rejected: %+v", processed.Attempts)
		}
		got, _ := x.e.m.Get(x.flightID)
		if got.WorkflowState != StateExecuting {
			t.Fatalf("state = %s, want executing", got.WorkflowState)
		}
		executor := got.sessionFor(RoleExecutor)
		if executor == nil || executor.SessionID == "" || executor.Target != "executor" {
			t.Fatalf("executor assignment = %+v, want the executor's model", executor)
		}
		if got := x.e.fk.spawner.callCount(); got != before+1 {
			t.Fatalf("spawns = %d, want %d", got, before+1)
		}
		op := lastOp(t, got, ActionExecution)
		if op.SourceTurnID != processed.ID {
			t.Fatalf("operation source turn = %q, want %q", op.SourceTurnID, processed.ID)
		}
		if op.PhaseVisit == 0 {
			t.Fatal("the operation records no phase visit")
		}
	})

	t.Run("executing advances to reviewing with the planner's model", func(t *testing.T) {
		x := newDecisionExchange(t, StateExecuting)
		before := x.e.fk.spawner.callCount()

		processed := x.decide(map[string]any{"advance": "reviewing"})

		if processed.Status == MonitoredTurnFailed {
			t.Fatalf("the advance was rejected: %+v", processed.Attempts)
		}
		got, _ := x.e.m.Get(x.flightID)
		if got.WorkflowState != StateReviewing {
			t.Fatalf("state = %s, want reviewing", got.WorkflowState)
		}
		reviewer := got.sessionFor(RoleReviewer)
		if reviewer == nil || reviewer.SessionID == "" || reviewer.Target != "planner" {
			t.Fatalf("reviewer assignment = %+v, want the planner's model", reviewer)
		}
		if got := x.e.fk.spawner.callCount(); got != before+1 {
			t.Fatalf("spawns = %d, want %d", got, before+1)
		}
		op := lastOp(t, got, ActionReview)
		if op.SourceTurnID != processed.ID || op.PhaseVisit == 0 {
			t.Fatalf("operation = %+v, want the deciding turn and its visit", op)
		}
	})

	t.Run("a replayed decision starts nothing", func(t *testing.T) {
		x := newDecisionExchange(t, StatePlanning)
		processed := x.decide(map[string]any{"advance": "executing"})
		spawns := x.e.fk.spawner.callCount()

		turn, _ := x.e.m.findMonitoredTurn(x.flightID, processed.ID)
		x.e.m.processPilotTurn(bg, x.flightID, turn)

		if got := x.e.fk.spawner.callCount(); got != spawns {
			t.Fatalf("spawns = %d, want %d", got, spawns)
		}
	})

	t.Run("a failed turn advances nothing", func(t *testing.T) {
		x := newDecisionExchange(t, StatePlanning)
		spawns := x.e.fk.spawner.callCount()

		processed := x.respondTurn(`{"advance":"executing"}`, "failed", "the harness stopped", x.trustedStart())

		assertAdvanceRejected(t, x, processed, "pilot turn failed", spawns)
	})

	t.Run("an interrupted turn advances nothing", func(t *testing.T) {
		x := newDecisionExchange(t, StatePlanning)
		spawns := x.e.fk.spawner.callCount()

		processed := x.respondTurn(`{"advance":"executing"}`, "interrupted", "the user interrupted", x.trustedStart())

		assertAdvanceRejected(t, x, processed, "pilot turn interrupted", spawns)
	})

	t.Run("a turn from another session advances nothing", func(t *testing.T) {
		x := newDecisionExchange(t, StatePlanning)
		spawns := x.e.fk.spawner.callCount()
		x.nextSeq += 2
		id, err := x.e.m.recordPlannerTurn(x.flightID, PlannerTurn{
			SessionID: "sess-not-the-pilot", StartSeq: x.nextSeq - 1, EndSeq: x.nextSeq,
			Text: `{"advance":"executing"}`, FinalText: `{"advance":"executing"}`, Outcome: "completed",
			StartedAt: x.trustedStart(),
		})
		if err != nil {
			t.Fatal(err)
		}
		turn, _ := x.e.m.findMonitoredTurn(x.flightID, id)
		x.e.m.processPilotTurn(bg, x.flightID, turn)
		processed, _ := x.e.m.findMonitoredTurn(x.flightID, id)

		assertAdvanceRejected(t, x, processed, "not from the assigned pilot", spawns)
	})

	t.Run("a pilot outside the flight workspace advances nothing", func(t *testing.T) {
		x := newDecisionExchange(t, StatePlanning)
		spawns := x.e.fk.spawner.callCount()
		x.e.fk.chats.workspaces = map[string]string{x.pilot: "repo-elsewhere"}

		processed := x.decide(map[string]any{"advance": "executing"})

		assertAdvanceRejected(t, x, processed, "same workspace", spawns)
	})

	t.Run("advancing from the wrong phase advances nothing", func(t *testing.T) {
		x := newDecisionExchange(t, StateReviewing)
		spawns := x.e.fk.spawner.callCount()

		processed := x.decide(map[string]any{"advance": "executing"})

		assertAdvanceRejected(t, x, processed, "only a planning flight can advance to executing", spawns)
	})

	t.Run("a turn without start evidence advances nothing", func(t *testing.T) {
		x := newDecisionExchange(t, StatePlanning)
		spawns := x.e.fk.spawner.callCount()

		processed := x.respondTurn(`{"advance":"executing"}`, "completed", "", time.Time{})

		assertAdvanceRejected(t, x, processed, "no phase visit evidence", spawns)
	})

	t.Run("a turn from an earlier visit advances nothing", func(t *testing.T) {
		x := newDecisionExchange(t, StatePlanning)
		spawns := x.e.fk.spawner.callCount()
		// The turn is retained in the current visit; the flight then leaves
		// that visit and comes back, so the decision is stale.
		x.nextSeq += 2
		id, err := x.e.m.recordPlannerTurn(x.flightID, PlannerTurn{
			SessionID: x.pilot, StartSeq: x.nextSeq - 1, EndSeq: x.nextSeq,
			Text: `{"advance":"executing"}`, FinalText: `{"advance":"executing"}`, Outcome: "completed",
			StartedAt: x.trustedStart(),
		})
		if err != nil {
			t.Fatal(err)
		}
		turn, _ := x.e.m.findMonitoredTurn(x.flightID, id)
		if turn.PhaseVisit == 0 {
			t.Fatal("the turn was not bound to the current visit")
		}
		executing := StateExecuting
		planning := StatePlanning
		if _, err := x.e.m.Update(bg, x.flightID, UpdateRequest{WorkflowState: &executing}); err != nil {
			t.Fatal(err)
		}
		if _, err := x.e.m.Update(bg, x.flightID, UpdateRequest{WorkflowState: &planning}); err != nil {
			t.Fatal(err)
		}
		x.e.m.processPilotTurn(bg, x.flightID, turn)
		processed, _ := x.e.m.findMonitoredTurn(x.flightID, id)

		assertAdvanceRejected(t, x, processed, "belongs to phase visit", spawns)
	})

	t.Run("a stop in the current visit blocks the advance", func(t *testing.T) {
		x := newDecisionExchange(t, StatePlanning)
		spawns := x.e.fk.spawner.callCount()
		stop := x.decide(map[string]any{"stop": "the planner is looping"})
		if stop.Status == MonitoredTurnFailed {
			t.Fatalf("the stop was rejected: %+v", stop.Attempts)
		}

		processed := x.decide(map[string]any{"advance": "executing"})

		assertAdvanceRejected(t, x, processed, "pilot stop in this phase", spawns)
	})

	t.Run("an unresolved operation blocks the advance", func(t *testing.T) {
		x := newDecisionExchange(t, StatePlanning)
		spawns := x.e.fk.spawner.callCount()
		// A takeoff that never settled leaves a pending operation.
		if err := x.e.m.editFlight(x.flightID, func(f *Flight) error {
			f.Operations = append(f.Operations, Operation{
				ID: "op-pending", Action: ActionExecution, CorrelationID: "corr-1", Status: StatusPending,
				CreatedAt: x.e.now(), UpdatedAt: x.e.now(),
			})
			return nil
		}); err != nil {
			t.Fatal(err)
		}

		processed := x.decide(map[string]any{"advance": "executing"})

		assertAdvanceRejected(t, x, processed, "unresolved", spawns)
	})

	t.Run("cleanup blocks the advance", func(t *testing.T) {
		x := newDecisionExchange(t, StatePlanning)
		spawns := x.e.fk.spawner.callCount()
		if err := x.e.m.editFlight(x.flightID, func(f *Flight) error {
			f.CleanupState = CleanupDisposing
			return nil
		}); err != nil {
			t.Fatal(err)
		}

		processed := x.decide(map[string]any{"advance": "executing"})

		assertAdvanceRejected(t, x, processed, "disposing", spawns)
	})

	t.Run("guidance still works without start evidence", func(t *testing.T) {
		x := newDecisionExchange(t, StateReviewing)

		processed := x.respondTurn(`{"to":"reviewer","guidance":"Re-check the parser."}`, "completed", "", time.Time{})

		if processed.Status == MonitoredTurnFailed {
			t.Fatalf("guidance was rejected: %+v", processed.Attempts)
		}
		x.deliverPending()
		if got := x.input(RoleReviewer); !strings.Contains(got, "[Pilot] Re-check the parser.") {
			t.Fatalf("reviewer input = %s", got)
		}
	})
}

// The daemon's own checks inform the pilot; they never advance a phase. An
// explicit pilot decision is the only thing that moves the flight forward.
func TestPilotAdvanceRequiresDecision(t *testing.T) {
	e := newEnv(t)
	f := takeoff(t, e)

	// The planner's artifacts are verified by the daemon.
	reported := e.reportPlanning(t, f)
	if reported.SpecPath == "" || reported.PlanPath == "" {
		t.Fatal("the planning artifacts were not verified")
	}
	if reported.WorkflowState != StatePlanning {
		t.Fatalf("state = %s, want planning: a verified artifact advances nothing", reported.WorkflowState)
	}

	// The executor's report is verified by the daemon.
	executing, err := e.m.EnterExecution(bg, reported.ID)
	if err != nil {
		t.Fatal(err)
	}
	writePlan(t, executing.ReportTarget, "# Execution report\n")
	executor := executing.sessionFor(RoleExecutor).SessionID
	id, err := e.m.recordPlannerTurn(executing.ID, PlannerTurn{
		SessionID: executor, StartSeq: 1, EndSeq: 2, Text: "Done.", Outcome: "completed",
	})
	if err != nil {
		t.Fatal(err)
	}
	retained, _ := e.m.findMonitoredTurn(executing.ID, id)
	e.m.recordExecutionCheck(bg, executing.ID, retained)
	checked, _ := e.m.findMonitoredTurn(executing.ID, id)
	if checked.Execution == nil || !checked.Execution.ReportVerified {
		t.Fatalf("execution check = %+v, want a verified report", checked.Execution)
	}
	current, _ := e.m.Get(executing.ID)
	if current.WorkflowState != StateExecuting {
		t.Fatalf("state = %s, want executing: a verified report advances nothing", current.WorkflowState)
	}

	// A clean review leaves the flight in reviewing, with no committer.
	reviewing, err := e.m.EnterReview(bg, executing.ID)
	if err != nil {
		t.Fatal(err)
	}
	reviewer := reviewing.sessionFor(RoleReviewer).SessionID
	rid, err := e.m.recordPlannerTurn(reviewing.ID, PlannerTurn{
		SessionID: reviewer, StartSeq: 1, EndSeq: 2, Text: "The review is clean.", Outcome: "completed",
		Skills: []SkillUse{{Skill: "superpowers:test-rules-review", Seq: 1, Evidence: "invoked"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	retained, _ = e.m.findMonitoredTurn(reviewing.ID, rid)
	e.m.recordReviewCheck(bg, reviewing.ID, retained)
	current, _ = e.m.Get(reviewing.ID)
	if current.WorkflowState != StateReviewing {
		t.Fatalf("state = %s, want reviewing", current.WorkflowState)
	}
	if commit := current.sessionFor(RoleCommit); commit != nil {
		t.Fatalf("a committer was started: %+v", commit)
	}
}

// The pilot's advance decision is read from the completed turn like every
// other decision form: prose is not a decision and sends nothing.
func TestPilotAdvanceProseIsNotADecision(t *testing.T) {
	x := newDecisionExchange(t, StatePlanning)
	spawns := x.e.fk.spawner.callCount()

	processed := x.respond("The plan looks complete; I will start the executor.")

	if processed.Status != MonitoredTurnSkipped || processed.Attempts[0].Error != "" {
		t.Fatalf("prose status = %s attempts = %+v; want skipped with no error", processed.Status, processed.Attempts)
	}
	if got := x.e.fk.spawner.callCount(); got != spawns {
		t.Fatalf("spawns = %d, want %d", got, spawns)
	}
}

// A decision the model wrapped in a code fence is still a decision.
func TestPilotAdvanceFencedDecisionIsApplied(t *testing.T) {
	x := newDecisionExchange(t, StatePlanning)

	processed := x.respond("```json\n{\"advance\":\"executing\"}\n```")

	if processed.Status == MonitoredTurnFailed {
		t.Fatalf("the fenced advance was rejected: %+v", processed.Attempts)
	}
	got, _ := x.e.m.Get(x.flightID)
	if got.WorkflowState != StateExecuting {
		t.Fatalf("state = %s, want executing", got.WorkflowState)
	}
}

// The pilot's stop is not revived by a later visit: it applies to the visit
// it was recorded in.
func TestPilotStopFromAnEarlierVisitDoesNotBlock(t *testing.T) {
	x := newDecisionExchange(t, StatePlanning)
	stop := x.decide(map[string]any{"stop": "the planner is looping"})
	if stop.Status == MonitoredTurnFailed {
		t.Fatalf("the stop was rejected: %+v", stop.Attempts)
	}
	executing := StateExecuting
	planning := StatePlanning
	if _, err := x.e.m.Update(bg, x.flightID, UpdateRequest{WorkflowState: &executing}); err != nil {
		t.Fatal(err)
	}
	if _, err := x.e.m.Update(bg, x.flightID, UpdateRequest{WorkflowState: &planning}); err != nil {
		t.Fatal(err)
	}

	processed := x.decide(map[string]any{"advance": "executing"})

	if processed.Status == MonitoredTurnFailed {
		t.Fatalf("the advance was blocked by an old stop: %+v", processed.Attempts)
	}
	got, _ := x.e.m.Get(x.flightID)
	if got.WorkflowState != StateExecuting {
		t.Fatalf("state = %s, want executing", got.WorkflowState)
	}
}

// A pilot decision that names a phase the flight cannot reach is not applied,
// and nothing is spawned for it.
func TestPilotAdvanceToCompletingIsRejected(t *testing.T) {
	x := newDecisionExchange(t, StateReviewing)
	spawns := x.e.fk.spawner.callCount()

	processed := x.respond(`{"advance":"completing"}`)

	assertAdvanceRejected(t, x, processed, "pilot advance must be executing or reviewing", spawns)
	got, _ := x.e.m.Get(x.flightID)
	if got.sessionFor(RoleCommit) != nil {
		t.Fatal("a committer was started")
	}
}

// The advance decision is a JSON object like every other decision; a
// malformed one is a visible error and not a crash.
func TestPilotAdvanceMalformedDecisionIsRejected(t *testing.T) {
	x := newDecisionExchange(t, StatePlanning)
	spawns := x.e.fk.spawner.callCount()

	processed := x.respond(`{"advance":`)

	assertAdvanceRejected(t, x, processed, "not a valid JSON decision", spawns)
}

// A pilot turn's decision carries the flight it belongs to; a turn from
// another flight's pilot cannot advance it.
func TestPilotAdvanceTurnFromAnotherFlightIsRejected(t *testing.T) {
	x := newDecisionExchange(t, StatePlanning)
	spawns := x.e.fk.spawner.callCount()

	// A turn produced by another flight's pilot, recorded on this flight.
	x.nextSeq += 2
	id, err := x.e.m.recordPlannerTurn(x.flightID, PlannerTurn{
		SessionID: "sess-another-flights-pilot", StartSeq: x.nextSeq - 1, EndSeq: x.nextSeq,
		Text: `{"advance":"executing"}`, FinalText: `{"advance":"executing"}`, Outcome: "completed",
		StartedAt: x.trustedStart(),
	})
	if err != nil {
		t.Fatal(err)
	}
	turn, _ := x.e.m.findMonitoredTurn(x.flightID, id)
	x.e.m.processPilotTurn(bg, x.flightID, turn)
	processed, _ := x.e.m.findMonitoredTurn(x.flightID, id)

	assertAdvanceRejected(t, x, processed, "not from the assigned pilot", spawns)
}
