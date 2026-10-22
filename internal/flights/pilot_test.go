package flights

import (
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/sergeknystautas/schmux/internal/chat"
)

func TestPilotDecisionValidationAndDirectDelivery(t *testing.T) {
	for _, tc := range []struct {
		name, response, outcome                string
		wrongSource, stopped, wrongWorkspace   bool
		wantError, wantGuidance                bool
		stopBeforeDelivery, moveBeforeDelivery bool
		wantErrText                            string
		// input is what the pilot turn answered: the delivered notification
		// by default, "user" for a message the user typed, or "none".
		input string
	}{
		{name: "reply to the user", response: "I stopped directing the executor.", input: "user"},
		{name: "decision-shaped reply to the user", response: `{"notification_id":"NOTIFICATION","to":"planner","guidance":"Check the spec."}`, input: "user"},
		{name: "no recorded input", response: `{"guidance":null}`, input: "none", wantError: true, wantErrText: "no recorded input message"},
		{name: "guidance", response: `{"notification_id":"NOTIFICATION","to":"planner","guidance":"Check the spec.\n\n"}`, wantGuidance: true},
		{name: "planner stops before delivery", response: `{"notification_id":"NOTIFICATION","to":"planner","guidance":"Check the spec.\n\n"}`, wantGuidance: true, stopBeforeDelivery: true},
		{name: "workspace changes before delivery", response: `{"notification_id":"NOTIFICATION","to":"planner","guidance":"Check the spec.\n\n"}`, wantGuidance: true, moveBeforeDelivery: true},
		{name: "leave planner alone", response: `{"notification_id":"NOTIFICATION","guidance":null}`},
		{name: "startup acknowledgement", response: `{"guidance":null}`},
		{name: "ordinary prose", response: "Check the spec.", wantError: true, wantErrText: "the response is prose, not a JSON decision"},
		{name: "malformed JSON", response: `{"guidance":`, wantError: true, wantErrText: "the response is not a valid JSON decision"},
		{name: "missing guidance", response: `{}`, wantError: true},
		{name: "blank guidance", response: `{"notification_id":"NOTIFICATION","to":"planner","guidance":"  "}`, wantError: true},
		{name: "missing to", response: `{"notification_id":"NOTIFICATION","guidance":"Check the spec."}`, wantError: true, wantErrText: `requires "to"`},
		{name: "unknown role", response: `{"notification_id":"NOTIFICATION","to":"pilot","guidance":"x"}`, wantError: true, wantErrText: `requires "to"`},
		{name: "to with null", response: `{"notification_id":"NOTIFICATION","to":"executor","guidance":null}`},
		{name: "executor during planning", response: `{"notification_id":"NOTIFICATION","to":"executor","guidance":"x"}`, wantError: true, wantErrText: "no assigned executor"},
		{name: "non-string guidance", response: `{"guidance":true}`, wantError: true},
		{name: "unknown fields", response: `{"notification_id":"NOTIFICATION","to":"planner","guidance":"Check the spec.","recipient":"another-session"}`, wantError: true},
		{name: "trailing object", response: `{"guidance":null}{"to":"planner","guidance":"Check the spec."}`, wantError: true},
		{name: "missing notification", response: `{"to":"planner","guidance":"Check the spec."}`, wantError: true},
		{name: "unknown notification", response: `{"notification_id":"missing","to":"planner","guidance":"Check the spec."}`, wantError: true},
		{name: "unassigned source", response: `{"notification_id":"NOTIFICATION","to":"planner","guidance":"Check the spec."}`, wrongSource: true, wantError: true},
		{name: "stopped planner", response: `{"notification_id":"NOTIFICATION","to":"planner","guidance":"Check the spec."}`, stopped: true, wantError: true},
		{name: "different workspace", response: `{"notification_id":"NOTIFICATION","to":"planner","guidance":"Check the spec."}`, wrongWorkspace: true, wantError: true},
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
			notification, err = e.m.deliverFlightMessage(bg, f.ID, notification.ID)
			if err != nil {
				t.Fatal(err)
			}
			inputs := []string{notification.ChatMessageID}
			switch tc.input {
			case "user":
				typed, err := pilotRT.Send("what should we do?", nil)
				if err != nil {
					t.Fatal(err)
				}
				inputs = []string{typed.ID}
			case "none":
				inputs = nil
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
			turnID, err := e.m.recordPlannerTurn(f.ID, PlannerTurn{SessionID: sessionID, StartSeq: 3, EndSeq: 4, Text: response, FinalText: response, Outcome: outcome, InputIDs: inputs})
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
			// A completed response to schmux that is rejected goes back to its
			// session; a failed or interrupted response holds no decision to
			// correct, and an unattributed turn is not the pilot's error.
			wantRejection := tc.wantError && outcome == "completed" && tc.input == ""
			wantMessages := 1
			if tc.wantGuidance || wantRejection {
				wantMessages++
			}
			if len(got.Monitor.Messages) != wantMessages {
				t.Fatalf("messages=%+v want %d", got.Monitor.Messages, wantMessages)
			}
			if wantRejection {
				rejection := got.Monitor.Messages[1]
				if rejection.Kind != "rejection" || rejection.SourceTurnID != turnID || rejection.RecipientSessionID != sessionID || !strings.Contains(rejection.Text, "Schmux rejected your response to notification "+notification.ID+" and delivered nothing: "+processed.Attempts[0].Error) {
					t.Fatalf("rejection=%+v; want the error %q returned to %s", rejection, processed.Attempts[0].Error, sessionID)
				}
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

// pilotExchange wires runtimes for every assigned session of an executing
// flight and plays pilot decisions against a delivered executor notification.
type pilotExchange struct {
	t                 *testing.T
	e                 *testEnv
	flightID          string
	pilot             string
	notificationID    string
	answering         string // chat message ID of the schmux message the next pilot turn answers
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
	notification, err = e.m.deliverFlightMessage(bg, f.ID, notification.ID)
	if err != nil {
		t.Fatal(err)
	}
	x.notificationID, x.answering = notification.ID, notification.ChatMessageID
	return x
}

// decide records the pilot's decision as a completed pilot turn, processes it,
// and delivers the guidance it produced.
func (x *pilotExchange) decide(to, guidance string) {
	x.t.Helper()
	response, err := json.Marshal(map[string]any{"notification_id": x.notificationID, "to": to, "guidance": guidance})
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

// respond records response as the pilot's next completed turn, answering the
// schmux message in x.answering, processes it, and returns the processed turn.
func (x *pilotExchange) respond(response string) MonitoredTurn {
	x.t.Helper()
	x.nextSeq += 2
	id, err := x.e.m.recordPlannerTurn(x.flightID, PlannerTurn{SessionID: x.pilot, StartSeq: x.nextSeq - 1, EndSeq: x.nextSeq, Text: response, FinalText: response, Outcome: "completed", InputIDs: []string{x.answering}})
	if err != nil {
		x.t.Fatal(err)
	}
	turn, _ := x.e.m.findMonitoredTurn(x.flightID, id)
	x.e.m.processPilotTurn(bg, x.flightID, turn)
	processed, _ := x.e.m.findMonitoredTurn(x.flightID, id)
	return processed
}

// rejections returns the flight's rejection messages in order.
func (x *pilotExchange) rejections() []FlightMessage {
	x.t.Helper()
	got, _ := x.e.m.Get(x.flightID)
	var out []FlightMessage
	for _, msg := range got.Monitor.Messages {
		if msg.Kind == "rejection" {
			out = append(out, msg)
		}
	}
	return out
}

// A rejected decision reaches the pilot's chat with the reason. A second
// rejected decision in a row is not returned, so a pilot that keeps answering
// badly cannot loop, and its error says why. The next valid decision is
// delivered as usual.
func TestRejectedDecisionGoesBackToThePilotOnce(t *testing.T) {
	x := newPilotExchange(t)
	missingTo := `{"notification_id":"` + x.notificationID + `","guidance":"Fix the test."}`

	first := x.respond(missingTo)
	rejections := x.rejections()
	if first.Status != MonitoredTurnFailed || len(rejections) != 1 || rejections[0].SourceTurnID != first.ID || rejections[0].RecipientSessionID != x.pilot {
		t.Fatalf("first turn=%+v rejections=%+v; want one rejection to the pilot", first, rejections)
	}
	// The pilot is still in the notification's turn, so its chat holds the
	// rejection as queued input.
	delivered, err := x.e.m.deliverFlightMessage(bg, x.flightID, rejections[0].ID)
	if err != nil || delivered.Status != MessageStatusQueued {
		t.Fatalf("delivered=%+v err=%v; want queued in the pilot chat", delivered, err)
	}
	want := `Schmux rejected your response to notification ` + x.notificationID + ` and delivered nothing: pilot decision requires \"to\": \"planner\" or \"executor\".\nAnswer a schmux message with exactly one JSON object and nothing else: {\"notification_id\":\"` + x.notificationID + `\",\"to\":\"executor\",\"guidance\":\"...\"}`
	if got := x.input(x.pilotConversation); !strings.Contains(got, want) {
		t.Fatalf("pilot conversation = %s\nwant containing %s", got, want)
	}

	x.answering = delivered.ChatMessageID
	second := x.respond(missingTo)
	if second.Status != MonitoredTurnFailed || !strings.Contains(second.Attempts[0].Error, "not returned to the pilot because its previous decision was also rejected") {
		t.Fatalf("second turn=%+v; want an error saying it was not returned", second)
	}
	if got := x.rejections(); len(got) != 1 {
		t.Fatalf("rejections=%+v; want the second rejection held back", got)
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

func TestPilotGuidanceReachesEitherSessionDuringExecution(t *testing.T) {
	x := newPilotExchange(t)

	x.decide("executor", "Fix the test.")
	if got := x.input(x.executorInput); !strings.Contains(got, "[Pilot] Fix the test.") {
		t.Fatalf("executor input = %s", got)
	}
	if got := x.input(x.plannerInputPath); strings.Contains(got, pilotPrefix) {
		t.Fatalf("planner received guidance addressed to the executor: %s", got)
	}

	x.decide("planner", "Check the report.")
	if got := x.input(x.plannerInputPath); !strings.Contains(got, "[Pilot] Check the report.") {
		t.Fatalf("planner input = %s", got)
	}
	if got := x.input(x.executorInput); strings.Contains(got, "Check the report.") {
		t.Fatalf("executor received guidance addressed to the planner: %s", got)
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
