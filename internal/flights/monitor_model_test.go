package flights

import (
	"errors"
	"path/filepath"
	"strings"
	"testing"
)

func newTempPath(t *testing.T) string {
	t.Helper()
	return filepath.Join(t.TempDir(), "flights.json")
}

func TestRecordPlannerTurn_IdempotentOnSessionAndEndSeq(t *testing.T) {
	e := newEnv(t)
	created := mustCreate(t, e.m, goodCreate())
	takeoff(t, e)

	turn := PlannerTurn{SessionID: "sess-1", StartSeq: 1, EndSeq: 7, Text: "hi", Outcome: "completed"}
	id, err := e.m.recordPlannerTurn(created.ID, turn)
	if err != nil {
		t.Fatalf("first call error = %v", err)
	}
	if id == "" {
		t.Fatal("id empty")
	}
	again, err := e.m.recordPlannerTurn(created.ID, turn)
	if err != nil {
		t.Fatalf("replay call error = %v", err)
	}
	if again != id {
		t.Errorf("replay id = %s, want %s", again, id)
	}
	got, _ := e.m.Get(created.ID)
	if got.Monitor == nil || len(got.Monitor.Turns) != 1 {
		t.Fatalf("monitor turns = %+v, want 1", got.Monitor)
	}
	if got.Monitor.Turns[0].Status != MonitoredTurnPending {
		t.Errorf("status = %s, want pending", got.Monitor.Turns[0].Status)
	}

	// A second turn with the same key is a no-op; a different end sequence
	// is appended.
	second := turn
	second.EndSeq = 8
	second.Text = "second"
	if _, err := e.m.recordPlannerTurn(created.ID, second); err != nil {
		t.Fatalf("second call: %v", err)
	}
	got, _ = e.m.Get(created.ID)
	if len(got.Monitor.Turns) != 2 {
		t.Errorf("turns = %d, want 2", len(got.Monitor.Turns))
	}
}

func TestRecordMonitorAttempt_UpdatesStatus(t *testing.T) {
	e := newEnv(t)
	created := mustCreate(t, e.m, goodCreate())
	takeoff(t, e)

	turn := PlannerTurn{SessionID: "sess-1", StartSeq: 1, EndSeq: 1, Text: "hi"}
	id, _ := e.m.recordPlannerTurn(created.ID, turn)

	conf := 0.6
	if err := e.m.recordMonitorAttempt(created.ID, id, MonitorAttempt{
		Classification: &Classification{Model: "m", Answers: []MonitorAnswer{{ID: "a", Choice: "x", Confidence: &conf}}},
	}); err != nil {
		t.Fatalf("classified attempt: %v", err)
	}
	got, _ := e.m.findMonitoredTurn(created.ID, id)
	if got.Status != MonitoredTurnClassified {
		t.Errorf("status after classified = %s, want classified", got.Status)
	}
	if len(got.Attempts) != 1 {
		t.Errorf("attempts = %d, want 1", len(got.Attempts))
	}

	// A second attempt (e.g. after a retry) appends and overwrites status.
	if err := e.m.recordMonitorAttempt(created.ID, id, MonitorAttempt{Error: "jev down"}); err != nil {
		t.Fatalf("failed attempt: %v", err)
	}
	got, _ = e.m.findMonitoredTurn(created.ID, id)
	if got.Status != MonitoredTurnFailed {
		t.Errorf("status after failed = %s, want failed", got.Status)
	}
	if len(got.Attempts) != 2 {
		t.Errorf("attempts = %d, want 2", len(got.Attempts))
	}
}

func TestRecordFlightMessage_DeduplicatesBySourceTurn(t *testing.T) {
	e := newEnv(t)
	created := mustCreate(t, e.m, goodCreate())
	takeoff(t, e)

	msg := FlightMessage{
		Kind: "notification", SourceTurnID: "turn-1", RecipientSessionID: "pilot-1",
		Text: "interesting",
	}
	first, err := e.m.recordFlightMessage(created.ID, msg)
	if err != nil {
		t.Fatalf("first: %v", err)
	}
	second, err := e.m.recordFlightMessage(created.ID, msg)
	if err != nil {
		t.Fatalf("second: %v", err)
	}
	if first.ID != second.ID {
		t.Errorf("dedup mismatch: first=%s second=%s", first.ID, second.ID)
	}
	got, _ := e.m.Get(created.ID)
	if len(got.Monitor.Messages) != 1 {
		t.Errorf("messages = %d, want 1", len(got.Monitor.Messages))
	}

	// A different source turn produces a different message.
	msg2 := msg
	msg2.SourceTurnID = "turn-2"
	if _, err := e.m.recordFlightMessage(created.ID, msg2); err != nil {
		t.Fatal(err)
	}
	got, _ = e.m.Get(created.ID)
	if len(got.Monitor.Messages) != 2 {
		t.Errorf("messages = %d, want 2", len(got.Monitor.Messages))
	}
}

func TestRecordFlightMessage_RejectsInvalidStatus(t *testing.T) {
	e := newEnv(t)
	created := mustCreate(t, e.m, goodCreate())
	takeoff(t, e)

	msg := FlightMessage{Kind: "notification", RecipientSessionID: "pilot-1", Text: "x", Status: "bogus"}
	if _, err := e.m.recordFlightMessage(created.ID, msg); err == nil {
		t.Fatal("bogus status should error")
	}
}

func TestRecordMonitorSnapshot_RoundTrip(t *testing.T) {
	e := newEnv(t)
	created := mustCreate(t, e.m, goodCreate())
	if _, err := e.m.Takeoff(bg, created.ID); err != nil {
		t.Fatalf("Takeoff: %v", err)
	}

	turn := PlannerTurn{SessionID: "sess-1", StartSeq: 1, EndSeq: 1, Text: "hi"}
	turnID, _ := e.m.recordPlannerTurn(created.ID, turn)

	conf := 0.8
	if err := e.m.recordMonitorAttempt(created.ID, turnID, MonitorAttempt{
		Classification: &Classification{Model: "m", Answers: []MonitorAnswer{{ID: "a", Choice: "x", Confidence: &conf}}},
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := e.m.recordFlightMessage(created.ID, FlightMessage{
		Kind: "notification", SourceTurnID: turnID, RecipientSessionID: "pilot-1", Text: "wake up",
	}); err != nil {
		t.Fatal(err)
	}

	// Reload through a fresh manager (simulates a daemon restart).
	reloaded := e.reload(t)
	got, ok := reloaded.Get(created.ID)
	if !ok {
		t.Fatal("flight missing after reload")
	}
	if got.Monitor == nil {
		t.Fatal("monitor missing after reload")
	}
	if len(got.Monitor.Turns) != 1 || got.Monitor.Turns[0].Status != MonitoredTurnClassified {
		t.Errorf("reloaded turns = %+v", got.Monitor.Turns)
	}
	if len(got.Monitor.Messages) != 1 || got.Monitor.Messages[0].Text != "wake up" {
		t.Errorf("reloaded messages = %+v", got.Monitor.Messages)
	}

	// Caller-side mutations do not leak into the manager.
	listed := reloaded.List()
	if len(listed) != 1 {
		t.Fatalf("list returned %d entries, want 1", len(listed))
	}
	if listed[0].Monitor == nil {
		t.Fatal("list returned no monitor")
	}
	if listed[0].Monitor == nil || len(listed[0].Monitor.Turns) == 0 {
		t.Fatal("list returned no monitor turns")
	}
	listed[0].Monitor.Turns[0].Status = MonitoredTurnFailed
	again := reloaded.List()
	if again[0].Monitor.Turns[0].Status != MonitoredTurnClassified {
		t.Errorf("caller mutation leaked: %s", again[0].Monitor.Turns[0].Status)
	}
}

func TestRetryMonitor_OnlyFailedItemsAreEligible(t *testing.T) {
	e := newEnv(t)
	created := mustCreate(t, e.m, goodCreate())
	takeoff(t, e)
	turn := PlannerTurn{SessionID: "sess-1", StartSeq: 1, EndSeq: 1, Text: "x"}
	turnID, _ := e.m.recordPlannerTurn(created.ID, turn)
	if err := e.m.recordMonitorAttempt(created.ID, turnID, MonitorAttempt{Error: "boom"}); err != nil {
		t.Fatal(err)
	}
	if err := e.m.RetryMonitor(bg, created.ID, turnID); err != nil {
		t.Fatalf("RetryMonitor on failed turn: %v", err)
	}
	got, _ := e.m.findMonitoredTurn(created.ID, turnID)
	if got.Status != MonitoredTurnPending {
		t.Errorf("status after retry = %s, want pending", got.Status)
	}

	// A turn that is now pending cannot be retried again.
	if err := e.m.RetryMonitor(bg, created.ID, turnID); err == nil {
		t.Fatal("retry on a pending turn should be a conflict")
	}

	// Unknown item: not found.
	if err := e.m.RetryMonitor(bg, created.ID, "nope"); err == nil {
		t.Fatal("retry on unknown item should error")
	}

	// Unknown flight: not found.
	if err := e.m.RetryMonitor(bg, "fl-404", turnID); !errors.Is(err, ErrNotFound) {
		t.Errorf("retry on unknown flight err = %v, want ErrNotFound", err)
	}
}

func TestRetryMonitor_UnknownMessageRequiresInspection(t *testing.T) {
	e := newEnv(t)
	created := mustCreate(t, e.m, goodCreate())
	takeoff(t, e)
	msg, _ := e.m.recordFlightMessage(created.ID, FlightMessage{
		Kind: "notification", RecipientSessionID: "pilot-1", Text: "x", Status: MessageStatusUnknown,
	})
	err := e.m.RetryMonitor(bg, created.ID, msg.ID)
	if err == nil || !strings.Contains(err.Error(), "unknown") {
		t.Errorf("retry on unknown delivery err = %v, want unknown", err)
	}
}

func TestSnapshotAcceptsExistingFlightsWithoutMonitor(t *testing.T) {
	// Pre-monitor flights load with nil Monitor and the absence of a pilot
	// assignment; the new role is recognized as valid.
	path := newTempPath(t)
	writeSnapshot(t, path, snapshot{Flights: []Flight{backlogFlight("fl-old", 0)}})
	m, _ := newManager(t, path, nil)
	got, ok := m.Get("fl-old")
	if !ok {
		t.Fatal("old flight missing")
	}
	if got.Monitor != nil {
		t.Errorf("pre-monitor flight gained a monitor: %+v", got.Monitor)
	}
}

func TestMonitorEvidenceIsCopiedOnInsertAndRead(t *testing.T) {
	e := newEnv(t)
	f := takeoff(t, e)
	id, err := e.m.recordPlannerTurn(f.ID, PlannerTurn{SessionID: "planner", StartSeq: 1, EndSeq: 2, Outcome: "completed"})
	if err != nil {
		t.Fatal(err)
	}
	cfg := twoClassifierConfig()
	confidence := 0.8
	classification := Classification{Answers: []MonitorAnswer{{ID: "a", Choice: "x", Confidence: &confidence, Probabilities: map[string]float64{"x": 0.8}}}}
	if err := e.m.recordMonitorAttempt(f.ID, id, MonitorAttempt{Config: &cfg, Classification: &classification}); err != nil {
		t.Fatal(err)
	}
	cfg.Roles["planner"].Classifiers[0].Question = "external mutation"
	classification.Answers[0].Probabilities["x"] = 0
	confidence = 0
	copy, _ := e.m.Get(f.ID)
	attempt := copy.Monitor.Turns[0].Attempts[0]
	if attempt.Config.Roles["planner"].Classifiers[0].Question != "q" || *attempt.Classification.Answers[0].Confidence != 0.8 || attempt.Classification.Answers[0].Probabilities["x"] != 0.8 {
		t.Fatalf("evidence changed after insertion: %+v", attempt)
	}
	attempt.Config.Roles["planner"].Classifiers[0].Choices[0].Description = "changed"
	attempt.Classification.Answers[0].Probabilities["x"] = 0
	*attempt.Classification.Answers[0].Confidence = 0
	again, _ := e.m.Get(f.ID)
	retained := again.Monitor.Turns[0].Attempts[0]
	if retained.Config.Roles["planner"].Classifiers[0].Choices[0].Description != "x" || *retained.Classification.Answers[0].Confidence != 0.8 || retained.Classification.Answers[0].Probabilities["x"] != 0.8 {
		t.Fatalf("Get returned mutable manager evidence: %+v", retained)
	}
}
