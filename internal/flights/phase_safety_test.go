package flights

import (
	"testing"
	"time"
)

func TestInvalidPilotStopPreservesCurrentStop(t *testing.T) {
	for _, kind := range []string{"missing start", "earlier visit"} {
		t.Run(kind, func(t *testing.T) {
			x := newDecisionExchange(t, StatePlanning)
			var stale MonitoredTurn
			if kind == "earlier visit" {
				stale = x.decide(map[string]any{"stop": "old stop"})
				for _, phase := range []WorkflowState{StateExecuting, StatePlanning} {
					if _, err := x.e.m.Update(bg, x.flightID, UpdateRequest{WorkflowState: &phase}); err != nil {
						t.Fatal(err)
					}
				}
			}
			active := x.decide(map[string]any{"stop": "do not proceed"})
			before, _ := x.e.m.Get(x.flightID)
			if !before.HasCurrentPilotStop() {
				t.Fatal("setup did not establish a current stop")
			}
			var rejected MonitoredTurn
			if kind == "missing start" {
				rejected = x.respondTurn(`{"stop":"unplaceable stop"}`, "completed", "", time.Time{})
			} else {
				x.e.m.processPilotTurn(bg, x.flightID, stale)
				rejected, _ = x.e.m.findMonitoredTurn(x.flightID, stale.ID)
			}
			after, _ := x.e.m.Get(x.flightID)
			if rejected.Status != MonitoredTurnFailed || !after.HasCurrentPilotStop() || after.PilotStop.SourceTurnID != active.ID {
				t.Fatalf("invalid stop status=%s, active stop=%+v; want failure preserving turn %s", rejected.Status, after.PilotStop, active.ID)
			}
			spawns := x.e.fk.spawner.callCount()
			advance := x.decide(map[string]any{"advance": "executing"})
			assertAdvanceRejected(t, x, advance, "pilot stop in this phase", spawns)
		})
	}
}

func TestClockRollbackCannotRebindPilotTurn(t *testing.T) {
	for _, offset := range []time.Duration{-2 * time.Second, 0} {
		t.Run(offset.String(), func(t *testing.T) {
			x := newDecisionExchange(t, StatePlanning)
			base := x.e.now().Add(time.Minute)
			now := base
			x.e.m.deps.Now = func() time.Time { return now }
			// The last persisted observation can be later than phase entry.
			if _, err := x.e.m.Update(bg, x.flightID, UpdateRequest{Prompt: ptrString("updated prompt")}); err != nil {
				t.Fatal(err)
			}
			oldStart := base.Add(time.Second)
			now = base.Add(offset)
			executing := StateExecuting
			if _, err := x.e.m.Update(bg, x.flightID, UpdateRequest{WorkflowState: &executing}); err != nil {
				t.Fatal(err)
			}
			x.e.m = x.e.reload(t)
			x.e.m.deps.Now = func() time.Time { return now }
			planning := StatePlanning
			if _, err := x.e.m.Update(bg, x.flightID, UpdateRequest{WorkflowState: &planning}); err != nil {
				t.Fatal(err)
			}
			// Recovering wall time alone cannot make this visit trustworthy.
			now = base.Add(2 * time.Second)
			spawns := x.e.fk.spawner.callCount()
			advance := x.respondTurn(`{"advance":"executing"}`, "completed", "", oldStart)
			assertAdvanceRejected(t, x, advance, "no phase visit evidence", spawns)
			// A later phase entry after clock recovery establishes a new boundary.
			for _, phase := range []WorkflowState{StateExecuting, StatePlanning} {
				now = now.Add(time.Second)
				if _, err := x.e.m.Update(bg, x.flightID, UpdateRequest{WorkflowState: &phase}); err != nil {
					t.Fatal(err)
				}
			}
			now = now.Add(time.Second)
			advance = x.respondTurn(`{"advance":"executing"}`, "completed", "", now)
			got, _ := x.e.m.Get(x.flightID)
			if advance.Status == MonitoredTurnFailed || got.WorkflowState != StateExecuting || x.e.fk.spawner.callCount() != spawns+1 {
				t.Fatalf("fresh decision after clock recovery: state=%s attempts=%+v spawns=%d", got.WorkflowState, advance.Attempts, x.e.fk.spawner.callCount())
			}
		})
	}
}
