package flights

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/sergeknystautas/schmux/internal/chat"
)

// Delivery of a guidance decision to the planner and the ways it can fail
// after the decision parsed. Decision-shape validation lives in
// TestPilotDecisionValidation.
func TestPilotDecisionValidationAndDirectDelivery(t *testing.T) {
	for _, tc := range []struct {
		name, response, outcome                string
		wrongSource, stopped, wrongWorkspace   bool
		wantError, wantGuidance, wantNotice    bool
		stopBeforeDelivery, moveBeforeDelivery bool
		wantErrText                            string
	}{
		{name: "prose reply to the user", response: "I stopped directing the executor."},
		{name: "guidance", response: `{"to":"planner","guidance":"Check the spec.\n\n"}`, wantGuidance: true},
		{name: "planner stops before delivery", response: `{"to":"planner","guidance":"Check the spec.\n\n"}`, wantGuidance: true, stopBeforeDelivery: true},
		{name: "workspace changes before delivery", response: `{"to":"planner","guidance":"Check the spec.\n\n"}`, wantGuidance: true, moveBeforeDelivery: true},
		{name: "leave planner alone", response: `{"to":"planner","guidance":null}`},
		{name: "startup acknowledgement", response: `{"guidance":null}`},
		{name: "unknown role", response: `{"to":"pilot","guidance":"x"}`, wantError: true, wantErrText: `requires "to"`},
		{name: "to with null", response: `{"to":"executor","guidance":null}`},
		{name: "executor during planning", response: `{"to":"executor","guidance":"x"}`, wantError: true, wantErrText: `requires "to": planner`},
		{name: "copied notification id", response: `{"notification_id":"NOTIFICATION","to":"planner","guidance":"Check the spec.\n\n"}`, wantGuidance: true},
		{name: "stale copied notification id", response: `{"notification_id":"missing","to":"planner","guidance":"Check the spec.\n\n"}`, wantGuidance: true},
		{name: "unassigned source", response: `{"to":"planner","guidance":"Check the spec."}`, wrongSource: true, wantError: true},
		{name: "stopped planner", response: `{"to":"planner","guidance":"Check the spec."}`, stopped: true, wantError: true, wantNotice: true},
		{name: "different workspace", response: `{"to":"planner","guidance":"Check the spec."}`, wrongWorkspace: true, wantError: true, wantNotice: true},
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
			if tc.wantErrText != "" && !strings.Contains(processed.Attempts[0].Error, tc.wantErrText) {
				t.Fatalf("decision error = %q, want containing %q", processed.Attempts[0].Error, tc.wantErrText)
			}
			got, _ := e.m.Get(f.ID)
			// A decision error is recorded on the turn and sends nothing: the
			// only messages are the notification and, on success, the guidance.
			// A valid send that fails before queuing also notifies the pilot.
			wantMessages := 1
			if tc.wantGuidance || tc.wantNotice {
				wantMessages++
			}
			if len(got.Monitor.Messages) != wantMessages {
				t.Fatalf("messages=%+v want %d", got.Monitor.Messages, wantMessages)
			}
			if tc.wantGuidance {
				message := got.Monitor.Messages[1]
				if message.SourceTurnID != turnID || message.SenderSessionID != pilot || message.RecipientSessionID != planner || message.Text != "Check the spec.\n\n" {
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

// pilotExchange wires runtimes for every assigned session of an executing
// flight, delivers an executor notification to the pilot, and plays pilot
// decisions.
type pilotExchange struct {
	t                 *testing.T
	e                 *testEnv
	flightID          string
	pilot             string
	nextSeq           uint64
	pilotConversation string
	plannerInputPath  string
	executorInput     string
}

func newPilotExchange(t *testing.T) *pilotExchange {
	t.Helper()
	e := newEnv(t)
	f := execute(t, e)
	x := &pilotExchange{t: t, e: e, flightID: f.ID, nextSeq: 10}
	x.pilot = f.sessionFor(RolePilot).SessionID
	planner, executor := f.sessionFor(RolePlanner).SessionID, f.sessionFor(RoleExecutor).SessionID
	pilotRT, pilotPaths := monitorRuntime(t, x.pilot)
	plannerRT, plannerPaths := monitorRuntime(t, planner)
	executorRT, executorPaths := monitorRuntime(t, executor)
	for id, rt := range map[string]*chat.Runtime{x.pilot: pilotRT, planner: plannerRT, executor: executorRT} {
		e.fk.chats.setRuntime(id, rt)
		e.fk.chats.set(id, true)
	}
	x.pilotConversation, x.plannerInputPath, x.executorInput = pilotPaths.Conversation, plannerPaths.Input, executorPaths.Input
	source, err := e.m.recordPlannerTurn(f.ID, PlannerTurn{SessionID: executor, StartSeq: 1, EndSeq: 2, Text: "Implemented.", Outcome: "completed"})
	if err != nil {
		t.Fatal(err)
	}
	if err := e.m.recordMonitorAttempt(f.ID, source, MonitorAttempt{}); err != nil {
		t.Fatal(err)
	}
	notification, err := e.m.recordFlightMessage(f.ID, FlightMessage{Kind: "notification", SourceTurnID: source, RecipientSessionID: x.pilot, Text: "Executor finished"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := e.m.deliverFlightMessage(bg, f.ID, notification.ID); err != nil {
		t.Fatal(err)
	}
	return x
}

// decide records the pilot's decision as a completed pilot turn, processes it,
// and delivers the guidance it produced.
func (x *pilotExchange) decide(to, guidance string) {
	x.t.Helper()
	response, err := json.Marshal(map[string]any{"to": to, "guidance": guidance})
	if err != nil {
		x.t.Fatal(err)
	}
	if processed := x.respond(string(response)); processed.Status == MonitoredTurnFailed {
		x.t.Fatalf("decision to %s failed: %+v", to, processed.Attempts)
	}
	got, _ := x.e.m.Get(x.flightID)
	message := got.Monitor.Messages[len(got.Monitor.Messages)-1]
	if message.Kind != "pilot_message" {
		x.t.Fatalf("decision to %s produced %+v", to, message)
	}
	if _, err := x.e.m.deliverFlightMessage(bg, x.flightID, message.ID); err != nil {
		x.t.Fatal(err)
	}
}

// respond records response as the pilot's next completed turn, processes it,
// and returns the processed turn.
func (x *pilotExchange) respond(response string) MonitoredTurn {
	x.t.Helper()
	x.nextSeq += 2
	id, err := x.e.m.recordPlannerTurn(x.flightID, PlannerTurn{SessionID: x.pilot, StartSeq: x.nextSeq - 1, EndSeq: x.nextSeq, Text: response, FinalText: response, Outcome: "completed"})
	if err != nil {
		x.t.Fatal(err)
	}
	turn, _ := x.e.m.findMonitoredTurn(x.flightID, id)
	x.e.m.processPilotTurn(bg, x.flightID, turn)
	processed, _ := x.e.m.findMonitoredTurn(x.flightID, id)
	return processed
}

// A decision that cannot be applied is a visible error on the pilot's turn
// and nothing else: nothing is sent to the pilot or to anyone. The pilot's
// next valid decision is delivered as usual.
func TestDecisionErrorIsRecordedAndSendsNothing(t *testing.T) {
	x := newPilotExchange(t)
	before, _ := x.e.m.Get(x.flightID)

	failed := x.respond(`{"guidance":"Fix the test."}`)

	if failed.Status != MonitoredTurnFailed || !strings.Contains(failed.Attempts[0].Error, `requires "to"`) {
		t.Fatalf("turn=%+v; want a failed turn naming the missing recipient", failed)
	}
	got, _ := x.e.m.Get(x.flightID)
	if len(got.Monitor.Messages) != len(before.Monitor.Messages) {
		t.Fatalf("messages=%+v; want no new message from a decision error", got.Monitor.Messages)
	}
	if conversation := x.input(x.pilotConversation); strings.Contains(conversation, "rejected") {
		t.Fatalf("the pilot was sent a rejection: %s", conversation)
	}

	x.decide("executor", "Fix the test.")
	if got := x.input(x.executorInput); !strings.Contains(got, "[Pilot] Fix the test.") {
		t.Fatalf("executor input = %s", got)
	}
}

func (x *pilotExchange) input(path string) string {
	x.t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		x.t.Fatal(err)
	}
	return string(data)
}

// Guidance goes to the session the pilot names and to no one else; the
// planner stays addressable while the flight executes.
func TestPilotGuidanceReachesEitherSessionDuringExecution(t *testing.T) {
	x := newPilotExchange(t)

	x.decide("executor", "Fix the test.")
	if got := x.input(x.executorInput); !strings.Contains(got, "[Pilot] Fix the test.") {
		t.Fatalf("executor input = %s", got)
	}
	if got := x.input(x.plannerInputPath); strings.Contains(got, pilotPrefix) {
		t.Fatalf("planner received guidance addressed to the executor: %s", got)
	}

	x.decide("planner", "The executor changed the plan's scope; revise the plan.")
	if got := x.input(x.plannerInputPath); !strings.Contains(got, "[Pilot] The executor changed the plan's scope; revise the plan.") {
		t.Fatalf("planner input = %s", got)
	}
	if got := x.input(x.executorInput); strings.Contains(got, "revise the plan") {
		t.Fatalf("executor received guidance addressed to the planner: %s", got)
	}
}

func TestAgentMessageDeliveryFailureNotifiesSender(t *testing.T) {
	for _, tc := range []struct {
		name                      string
		senderRole, recipientRole SessionRole
		recipientRunning          bool
	}{
		{name: "planner to running executor", senderRole: RolePlanner, recipientRole: RoleExecutor, recipientRunning: true},
		{name: "planner to stopped executor", senderRole: RolePlanner, recipientRole: RoleExecutor},
		{name: "executor to running planner", senderRole: RoleExecutor, recipientRole: RolePlanner, recipientRunning: true},
		{name: "executor to stopped planner", senderRole: RoleExecutor, recipientRole: RolePlanner},
	} {
		t.Run(tc.name, func(t *testing.T) {
			x := newPilotExchange(t)
			f, _ := x.e.m.Get(x.flightID)
			senderID := f.sessionFor(tc.senderRole).SessionID
			recipientID := f.sessionFor(tc.recipientRole).SessionID
			x.e.fk.chats.set(recipientID, tc.recipientRunning)
			original, err := x.e.m.recordFlightMessage(x.flightID, FlightMessage{
				Kind:               "pilot_message",
				SourceTurnID:       "turn-agent-message",
				SenderSessionID:    senderID,
				RecipientSessionID: recipientID,
				Text:               "Continue.",
				Status:             MessageStatusPending,
			})
			if err != nil {
				t.Fatal(err)
			}
			want := fmt.Sprintf(
				"Your message to %s was not delivered: flight state conflict: %s session is not running.",
				recipientID, tc.recipientRole,
			)
			delivered, err := x.e.m.deliverFlightMessage(bg, x.flightID, original.ID)
			if tc.recipientRunning {
				if err != nil || delivered.Status != MessageStatusAccepted {
					t.Fatalf("delivery=%+v err=%v; want accepted", delivered, err)
				}
				got, _ := x.e.m.Get(x.flightID)
				for _, msg := range got.Monitor.Messages {
					if msg.Text == want {
						t.Fatalf("successful delivery recorded failure notice=%+v", msg)
					}
				}
				return
			}

			if err == nil || delivered.Status != MessageStatusFailed {
				t.Fatalf("delivery=%+v err=%v; want failed", delivered, err)
			}
			got, _ := x.e.m.Get(x.flightID)
			var notices []FlightMessage
			for _, msg := range got.Monitor.Messages {
				if msg.Text == want {
					notices = append(notices, msg)
				}
			}
			if len(notices) != 1 {
				t.Fatalf("failure notices=%+v; want exactly one", notices)
			}
			notice := notices[0]
			if notice.Kind != "notification" || notice.Status != MessageStatusPending || notice.SourceTurnID != original.SourceTurnID || notice.RecipientSessionID != senderID || notice.SenderSessionID != "" {
				t.Fatalf("failure notice=%+v; want pending notice to original sender %s", notice, senderID)
			}
			deliveredNotice, err := x.e.m.deliverFlightMessage(bg, x.flightID, notice.ID)
			if err != nil || deliveredNotice.Status != MessageStatusAccepted {
				t.Fatalf("notice delivery=%+v err=%v; want accepted", deliveredNotice, err)
			}
			inputPath := x.executorInput
			if tc.senderRole == RolePlanner {
				inputPath = x.plannerInputPath
			}
			if input := x.input(inputPath); !strings.Contains(input, want) {
				t.Fatalf("sender input=%q; want notification %q", input, want)
			}
		})
	}
}

func TestDeliveryFailureNoticeRequiresAgentSender(t *testing.T) {
	x := newPilotExchange(t)
	before, _ := x.e.m.Get(x.flightID)

	x.e.m.recordDeliveryFailureNotice(x.flightID, FlightMessage{
		Kind:               "notification",
		SourceTurnID:       "turn-senderless",
		RecipientSessionID: x.pilot,
		Text:               "System notice.",
	}, fmt.Errorf("send failed"))

	after, _ := x.e.m.Get(x.flightID)
	if len(after.Monitor.Messages) != len(before.Monitor.Messages) {
		t.Fatalf("messages=%+v; want no failure notice without an agent sender", after.Monitor.Messages)
	}
}

// Guidance recorded while planning is still pending when execution starts. The
// planner stays assigned, so the message must reach it rather than fail.
func TestPendingPlannerGuidanceSurvivesEnterExecution(t *testing.T) {
	e := newEnv(t)
	f := e.reportPlanning(t, takeoff(t, e))
	pilot, planner := f.sessionFor(RolePilot).SessionID, f.sessionFor(RolePlanner).SessionID
	plannerRT, paths := monitorRuntime(t, planner)
	e.fk.chats.setRuntime(planner, plannerRT)
	e.fk.chats.set(planner, true)
	message, err := e.m.recordFlightMessage(f.ID, FlightMessage{Kind: "pilot_message", SenderSessionID: pilot, RecipientSessionID: planner, Text: "Mention the edge case.", Status: MessageStatusPending})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := e.m.EnterExecution(bg, f.ID); err != nil {
		t.Fatalf("EnterExecution() error = %v", err)
	}
	delivered, err := e.m.deliverFlightMessage(bg, f.ID, message.ID)
	if err != nil || delivered.Status != MessageStatusAccepted {
		t.Fatalf("delivery = %+v, err = %v; want accepted", delivered, err)
	}
	input, err := os.ReadFile(paths.Input)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(input), "[Pilot] Mention the edge case.") {
		t.Fatalf("planner input = %s", input)
	}
}
