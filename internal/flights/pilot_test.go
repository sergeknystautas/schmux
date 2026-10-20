package flights

import (
	"os"
	"strings"
	"testing"
)

func TestPilotDecisionValidationAndDirectDelivery(t *testing.T) {
	for _, tc := range []struct {
		name, response, outcome                string
		wrongSource, stopped, wrongWorkspace   bool
		wantError, wantGuidance                bool
		stopBeforeDelivery, moveBeforeDelivery bool
	}{
		{name: "guidance", response: `{"notification_id":"NOTIFICATION","guidance":"Check the spec.\n\n"}`, wantGuidance: true},
		{name: "planner stops before delivery", response: `{"notification_id":"NOTIFICATION","guidance":"Check the spec.\n\n"}`, wantGuidance: true, stopBeforeDelivery: true},
		{name: "workspace changes before delivery", response: `{"notification_id":"NOTIFICATION","guidance":"Check the spec.\n\n"}`, wantGuidance: true, moveBeforeDelivery: true},
		{name: "leave planner alone", response: `{"notification_id":"NOTIFICATION","guidance":null}`},
		{name: "startup acknowledgement", response: `{"guidance":null}`},
		{name: "ordinary prose", response: "Check the spec.", wantError: true},
		{name: "missing guidance", response: `{}`, wantError: true},
		{name: "blank guidance", response: `{"notification_id":"NOTIFICATION","guidance":"  "}`, wantError: true},
		{name: "non-string guidance", response: `{"guidance":true}`, wantError: true},
		{name: "unknown fields", response: `{"notification_id":"NOTIFICATION","guidance":"Check the spec.","recipient":"another-session"}`, wantError: true},
		{name: "trailing object", response: `{"guidance":null}{"guidance":"Check the spec."}`, wantError: true},
		{name: "missing notification", response: `{"guidance":"Check the spec."}`, wantError: true},
		{name: "unknown notification", response: `{"notification_id":"missing","guidance":"Check the spec."}`, wantError: true},
		{name: "unassigned source", response: `{"notification_id":"NOTIFICATION","guidance":"Check the spec."}`, wrongSource: true, wantError: true},
		{name: "stopped planner", response: `{"notification_id":"NOTIFICATION","guidance":"Check the spec."}`, stopped: true, wantError: true},
		{name: "different workspace", response: `{"notification_id":"NOTIFICATION","guidance":"Check the spec."}`, wrongWorkspace: true, wantError: true},
		{name: "failed pilot", outcome: "failed", wantError: true},
		{name: "interrupted pilot", outcome: "interrupted", wantError: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			e := newEnv(t)
			f := takeoff(t, e)
			pilot, planner := f.sessionFor(RolePilot).SessionID, f.sessionFor(RolePlanner).SessionID
			pilotRT, _ := monitorRuntime(t, pilot)
			plannerRT, paths := monitorRuntime(t, planner)
			e.fk.chats.setRuntime(pilot, pilotRT)
			e.fk.chats.setRuntime(planner, plannerRT)
			e.fk.chats.set(planner, !tc.stopped)
			if tc.wrongWorkspace {
				e.fk.chats.workspaces = map[string]string{planner: "other-workspace"}
			}
			source, err := e.m.recordPlannerTurn(f.ID, PlannerTurn{SessionID: planner, StartSeq: 1, EndSeq: 2, Text: "Plan ready", Outcome: "completed"})
			if err != nil {
				t.Fatal(err)
			}
			if err := e.m.recordMonitorAttempt(f.ID, source, MonitorAttempt{}); err != nil {
				t.Fatal(err)
			}
			notification, err := e.m.recordFlightMessage(f.ID, FlightMessage{Kind: "notification", SourceTurnID: source, RecipientSessionID: pilot, Text: "Plan ready"})
			if err != nil {
				t.Fatal(err)
			}
			if _, err := e.m.deliverFlightMessage(bg, f.ID, notification.ID); err != nil {
				t.Fatal(err)
			}
			response := strings.ReplaceAll(tc.response, "NOTIFICATION", notification.ID)
			sessionID := pilot
			if tc.wrongSource {
				sessionID = "unassigned-session"
			}
			outcome := tc.outcome
			if outcome == "" {
				outcome = "completed"
			}
			turnID, err := e.m.recordPlannerTurn(f.ID, PlannerTurn{SessionID: sessionID, StartSeq: 3, EndSeq: 4, Text: response, FinalText: response, Outcome: outcome})
			if err != nil {
				t.Fatal(err)
			}
			turn, _ := e.m.findMonitoredTurn(f.ID, turnID)
			e.m.processPilotTurn(bg, f.ID, turn)
			processed, _ := e.m.findMonitoredTurn(f.ID, turnID)
			if gotError := processed.Status == MonitoredTurnFailed; gotError != tc.wantError {
				t.Fatalf("decision status=%s attempts=%+v; want error=%v", processed.Status, processed.Attempts, tc.wantError)
			}
			got, _ := e.m.Get(f.ID)
			wantMessages := 1
			if tc.wantGuidance {
				wantMessages++
			}
			if len(got.Monitor.Messages) != wantMessages {
				t.Fatalf("messages=%+v want %d", got.Monitor.Messages, wantMessages)
			}
			if tc.wantGuidance {
				message := got.Monitor.Messages[1]
				if message.SourceTurnID != turnID || message.SourceNotificationID != notification.ID || message.SenderSessionID != pilot || message.RecipientSessionID != planner || message.Text != "Check the spec.\n\n" {
					t.Fatalf("guidance evidence=%+v", message)
				}
				if tc.stopBeforeDelivery {
					e.fk.chats.set(planner, false)
				}
				if tc.moveBeforeDelivery {
					e.fk.chats.workspaces = map[string]string{planner: "other-workspace"}
				}
				delivered, err := e.m.deliverFlightMessage(bg, f.ID, message.ID)
				stale := tc.stopBeforeDelivery || tc.moveBeforeDelivery
				if stale {
					if err == nil || delivered.Status != MessageStatusFailed {
						t.Fatalf("stale delivery=%+v err=%v; want failed", delivered, err)
					}
				} else if err != nil {
					t.Fatal(err)
				}
				// Replaying the same source response must retain one exchange and never resend it.
				e.m.processPilotTurn(bg, f.ID, turn)
				if _, err := e.m.deliverFlightMessage(bg, f.ID, message.ID); err != nil {
					t.Fatal(err)
				}
			}
			input, err := os.ReadFile(paths.Input)
			if err != nil {
				t.Fatal(err)
			}
			wantSends := 0
			if tc.wantGuidance && !tc.stopBeforeDelivery && !tc.moveBeforeDelivery {
				wantSends = 1
			}
			if sends := strings.Count(string(input), pilotPrefix); sends != wantSends {
				t.Fatalf("planner input=%s; want %d sends", input, wantSends)
			}
		})
	}
}
