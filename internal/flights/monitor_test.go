package flights

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/sergeknystautas/schmux/internal/chat"
	"github.com/sergeknystautas/schmux/internal/config"
	"github.com/sergeknystautas/schmux/internal/schmuxdir"
)

func monitorRuntime(t *testing.T, id string, lines ...string) (*chat.Runtime, chat.Paths) {
	t.Helper()
	paths := chat.PathsFor(t.TempDir())
	if err := paths.Ensure(); err != nil {
		t.Fatal(err)
	}
	log, err := chat.OpenLog(paths.Conversation)
	if err != nil {
		t.Fatal(err)
	}
	for _, line := range lines {
		if err := log.Append(chat.NewHarness([]byte(line))); err != nil {
			t.Fatal(err)
		}
	}
	proto, err := chat.ProtocolFor(chat.ProtocolClaude)
	if err != nil {
		t.Fatal(err)
	}
	rt, err := chat.NewRuntime(id, proto, paths, "", "", nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	rt.Start()
	t.Cleanup(rt.Stop)
	return rt, paths
}

func monitorEvents(m *Manager, flightID string) <-chan Flight {
	events := make(chan Flight, 128)
	m.deps.Notify = func() { f, _ := m.Get(flightID); events <- f }
	return events
}

// Await manager persistence broadcasts, with a deadline only as a failure backstop.
func awaitMonitor(t *testing.T, events <-chan Flight, claim string, ready func(Flight) bool) Flight {
	t.Helper()
	deadline := time.NewTimer(5 * time.Second)
	defer deadline.Stop()
	var last Flight
	for {
		select {
		case last = <-events:
			if ready(last) {
				return last
			}
		case <-deadline.C:
			evidence, _ := json.Marshal(last)
			t.Fatalf("%s: no persistence event within 5s; last flight: %s", claim, evidence)
		}
	}
}

func startTestMonitor(t *testing.T, m *Manager) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	m.StartMonitoring(ctx)
	t.Cleanup(func() {
		cancel()
		select {
		case <-m.monitor.done:
		case <-time.After(5 * time.Second):
			t.Error("monitor did not release subscriptions after cancellation")
		}
	})
}

func saveMonitorKey(t *testing.T, key string) {
	t.Helper()
	if err := os.MkdirAll(schmuxdir.Get(), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := config.SaveTypesafeAPIKey(key); err != nil {
		t.Fatal(err)
	}
}

// The daemon starts monitoring long before the dashboard server binds chat
// sessions, so the first reconcile finds no Chats; Bind must queue a
// reconcile rather than leave the monitor idle until the next tick.
func TestBindWakesIdleMonitor(t *testing.T) {
	m := New(Dependencies{}, discard())
	if len(m.monitor.kick) != 0 {
		t.Fatalf("kick queued before Bind")
	}
	m.Bind(Binding{Chats: newFakes(t).chats})
	if len(m.monitor.kick) != 1 {
		t.Fatalf("Bind did not queue a monitor reconcile")
	}
}

func TestMonitorConstructBindRoundTripAndRetry(t *testing.T) {
	t.Setenv("TYPESAFE_API_KEY", "")
	t.Setenv("HOME", t.TempDir())
	saveMonitorKey(t, "test-key")
	e := newEnv(t)
	f := takeoff(t, e)
	pilot, planner := f.sessionFor(RolePilot).SessionID, f.sessionFor(RolePlanner).SessionID
	pilotRT, pilotPaths := monitorRuntime(t, pilot)
	// The planner reports a spec and plan placed where the skills' defaults
	// put them; the monitor must verify and adopt the report.
	gitInit(t, e.fk.workspaces.path)
	specPath := filepath.Join(e.fk.workspaces.path, "docs", "superpowers", "specs", "s.md")
	planPath := filepath.Join(e.fk.workspaces.path, "docs", "superpowers", "plans", "p.md")
	for _, p := range []string{specPath, planPath} {
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		writePlan(t, p, "# doc\n")
	}
	report := "Plan ready; please review.\nSpec: " + specPath + "\nPlan: " + planPath
	fullResponse := "I checked the existing implementation.\n" + report
	reportJSON, err := json.Marshal(report)
	if err != nil {
		t.Fatal(err)
	}
	plannerRT, plannerPaths := monitorRuntime(t, planner,
		`{"type":"assistant","message":{"id":"one","content":[{"type":"text","text":"I checked the existing implementation."},{"type":"tool_use","id":"read","name":"Read","input":{}}]}}`,
		`{"type":"user","message":{"content":[{"type":"tool_result","tool_use_id":"read","content":"Tool output is excluded."}]}}`,
		`{"type":"assistant","message":{"id":"two","content":[{"type":"text","text":`+string(reportJSON)+`}]}}`,
		`{"type":"result","subtype":"success"}`)
	e.fk.chats.setRuntime(pilot, pilotRT)
	e.fk.chats.setRuntime(planner, plannerRT)
	e.fk.chats.set(planner, true)
	var requests atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Questions map[string]struct {
				Type     string            `json:"type"`
				Criteria map[string]string `json:"criteria"`
			} `json:"questions"`
			State string `json:"state"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil || len(body.Questions["attention"].Criteria) != 2 {
			t.Errorf("invalid Jev request: %+v; %v", body, err)
		}
		if body.State != fullResponse {
			t.Errorf("classifier input=%q; want full completed turn %q", body.State, fullResponse)
		}
		if requests.Add(1) == 1 {
			http.Error(w, "temporary classifier failure", http.StatusServiceUnavailable)
			return
		}
		_, _ = w.Write([]byte(`{"model":"test-jev","answers":{"attention":{"choice":"needs_attention","confidence":0.9},"planning_artifacts":{"choice":"plan","confidence":0.9}}}`))
	}))
	defer srv.Close()
	old := monitorEndpoint
	monitorEndpoint = srv.URL
	t.Cleanup(func() { monitorEndpoint = old })
	// Match daemon ordering: construct without Chats, load, start monitoring,
	// then Bind when the dashboard server starts. Bind must wake the monitor;
	// waiting for the next reconcile tick would exceed awaitMonitor's deadline.
	m := New(Dependencies{Repos: e.fk.repos, Workspaces: e.fk.workspaces}, discard())
	if err := m.Load(e.path); err != nil {
		t.Fatal(err)
	}
	if got, want := m.monitorConfigPath(), filepath.Join(schmuxdir.Get(), "flight-monitor.yaml"); got != want {
		t.Fatalf("config path=%q want %q", got, want)
	}
	startTestMonitor(t, m)
	events := make(chan Flight, 128)
	m.Bind(Binding{Chats: e.fk.chats, Notify: func() { f, _ := m.Get(f.ID); events <- f }})
	failed := awaitMonitor(t, events, "first classification failed", func(f Flight) bool {
		return f.Monitor != nil && len(f.Monitor.Turns) == 1 && f.Monitor.Turns[0].Status == MonitoredTurnFailed
	})
	turnID := failed.Monitor.Turns[0].ID
	if err := m.RetryMonitor(bg, f.ID, turnID); err != nil {
		t.Fatal(err)
	}
	delivered := awaitMonitor(t, events, "classification delivered to pilot", func(f Flight) bool {
		return f.Monitor != nil && len(f.Monitor.Messages) == 1 && f.Monitor.Messages[0].Status == MessageStatusAccepted
	})
	if requests.Load() != 2 || len(delivered.Monitor.Turns[0].Attempts) != 2 {
		t.Fatalf("requests=%d monitor=%+v", requests.Load(), delivered.Monitor)
	}
	if delivered.Monitor.Turns[0].Turn.SessionID != planner {
		t.Fatalf("source=%+v", delivered.Monitor.Turns[0])
	}
	notification := delivered.Monitor.Messages[0]
	input, err := os.ReadFile(pilotPaths.Input)
	if err != nil {
		t.Fatal(err)
	}
	var pilotInput struct {
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
	}
	if err := json.Unmarshal(input, &pilotInput); err != nil {
		t.Fatalf("decode pilot input: %v; input=%s", err, input)
	}
	if delivered.SpecPath != specPath || delivered.PlanPath != planPath {
		t.Errorf("flight spec/plan = %q/%q, want the verified report %q/%q", delivered.SpecPath, delivered.PlanPath, specPath, planPath)
	}
	for _, want := range []string{fullResponse, f.ID, "Spec/plan check: passed", "conversation.jsonl", "Does this planner response"} {
		if !strings.Contains(pilotInput.Message.Content, want) {
			t.Errorf("pilot input lacks %q: %s", want, input)
		}
	}
	decision, err := json.Marshal(map[string]any{"notification_id": notification.ID, "guidance": "Please check the spec.\n\n"})
	if err != nil {
		t.Fatal(err)
	}
	writePilotResponse(t, pilotPaths, string(decision))
	guided := awaitMonitor(t, events, "pilot response delivered directly to planner", func(f Flight) bool {
		return len(f.Monitor.Messages) == 2 && f.Monitor.Messages[1].Status == MessageStatusAccepted
	})
	sent := guided.Monitor.Messages[1]
	if sent.SenderSessionID != pilot || sent.RecipientSessionID != planner || sent.Text != "Please check the spec.\n\n" {
		t.Fatalf("pilot guidance=%+v", sent)
	}
	input, err = os.ReadFile(plannerPaths.Input)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(input), "[Pilot] Please check the spec.") {
		t.Fatalf("planner input=%s", input)
	}
	writePilotResponse(t, pilotPaths, string(decision))
	awaitMonitor(t, events, "second pilot response queues while planner is busy", func(f Flight) bool {
		return len(f.Monitor.Messages) == 3 && f.Monitor.Messages[2].Status == MessageStatusQueued
	})
}

func writePilotResponse(t *testing.T, paths chat.Paths, text string) {
	t.Helper()
	output, err := os.OpenFile(paths.Output, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	defer output.Close()
	encoder := json.NewEncoder(output)
	for _, block := range []string{"Reviewing source evidence.", text} {
		if err := encoder.Encode(map[string]any{
			"type": "assistant", "message": map[string]any{"content": []map[string]string{{"type": "text", "text": block}}},
		}); err != nil {
			t.Fatal(err)
		}
	}
	if err := encoder.Encode(map[string]string{"type": "result", "subtype": "success", "result": text}); err != nil {
		t.Fatal(err)
	}
}

func TestMonitorRecoveryRetainedTurnAndAmbiguousDelivery(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("TYPESAFE_API_KEY", "ignored-environment-key")
	e := newEnv(t)
	f := takeoff(t, e)
	pilot, planner := f.sessionFor(RolePilot).SessionID, f.sessionFor(RolePlanner).SessionID
	pilotRT, _ := monitorRuntime(t, pilot)
	plannerRT, _ := monitorRuntime(t, planner)
	e.fk.chats.setRuntime(pilot, pilotRT)
	e.fk.chats.setRuntime(planner, plannerRT)
	id, err := e.m.recordPlannerTurn(f.ID, PlannerTurn{SessionID: planner, StartSeq: 1, EndSeq: 2, Text: "Retained before crash", Outcome: "completed"})
	if err != nil {
		t.Fatal(err)
	}
	msg, err := e.m.recordFlightMessage(f.ID, FlightMessage{Kind: "pilot_message", SenderSessionID: pilot, RecipientSessionID: planner, Text: "ambiguous", Status: MessageStatusSending})
	if err != nil {
		t.Fatal(err)
	}
	m := e.reload(t)
	got, _ := m.findMonitoredMessage(f.ID, msg.ID)
	if got.Status != MessageStatusUnknown {
		t.Fatalf("recovered message=%+v", got)
	}
	if err := m.RetryMonitor(bg, f.ID, msg.ID); !errors.Is(err, ErrConflict) {
		t.Fatalf("retry unknown err=%v", err)
	}
	m.monitor.configPath = filepath.Join(t.TempDir(), "monitor.yaml")
	events := monitorEvents(m, f.ID)
	startTestMonitor(t, m)
	recovered := awaitMonitor(t, events, "retained work attempted without new output", func(f Flight) bool { return f.Monitor.Turns[0].Status == MonitoredTurnFailed })
	attempt := recovered.Monitor.Turns[0].Attempts[0]
	if !strings.Contains(attempt.Error, "TypeSafe API key is not configured") || recovered.Monitor.Turns[0].ID != id {
		t.Fatalf("recovered turn=%+v", recovered.Monitor.Turns[0])
	}
}

func TestMonitorRoutineFailureAndDeliveryRetry(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	saveMonitorKey(t, "test-key")
	e := newEnv(t)
	f := takeoff(t, e)
	pilot, planner := f.sessionFor(RolePilot).SessionID, f.sessionFor(RolePlanner).SessionID
	e.m.monitor.configPath = filepath.Join(t.TempDir(), "monitor.yaml")
	if err := ensureMonitorConfig(e.m.monitorConfigPath()); err != nil {
		t.Fatal(err)
	}
	var requests atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		_, _ = w.Write([]byte(`{"answers":{"attention":{"choice":"routine","confidence":0.9},"planning_artifacts":{"choice":"neither","confidence":0.9}}}`))
	}))
	defer srv.Close()
	old := monitorEndpoint
	monitorEndpoint = srv.URL
	t.Cleanup(func() { monitorEndpoint = old })
	_, err := e.m.recordPlannerTurn(f.ID, PlannerTurn{SessionID: planner, StartSeq: 1, EndSeq: 2, Text: "Routine", Outcome: "completed"})
	if err != nil {
		t.Fatal(err)
	}
	e.m.processMonitorWork(bg, f.ID, pilot, planner)
	got, _ := e.m.Get(f.ID)
	if requests.Load() != 1 || got.Monitor.Turns[0].Status != MonitoredTurnClassified || len(got.Monitor.Messages) != 0 {
		t.Fatalf("routine=%+v requests=%d", got.Monitor, requests.Load())
	}
	_, err = e.m.recordPlannerTurn(f.ID, PlannerTurn{SessionID: planner, StartSeq: 3, EndSeq: 4, Text: "Partial", Outcome: "interrupted"})
	if err != nil {
		t.Fatal(err)
	}
	e.m.processMonitorWork(bg, f.ID, pilot, planner)
	got, _ = e.m.Get(f.ID)
	if requests.Load() != 1 || len(got.Monitor.Messages) != 1 || got.Monitor.Messages[0].Status != MessageStatusFailed {
		t.Fatalf("interrupted=%+v requests=%d", got.Monitor, requests.Load())
	}
	rt, paths := monitorRuntime(t, pilot)
	e.fk.chats.setRuntime(pilot, rt)
	if err := e.m.RetryMonitor(bg, f.ID, got.Monitor.Messages[0].ID); err != nil {
		t.Fatal(err)
	}
	e.m.processMonitorWork(bg, f.ID, pilot, planner)
	got, _ = e.m.Get(f.ID)
	if got.Monitor.Messages[0].Status != MessageStatusAccepted {
		t.Fatalf("delivery retry=%+v", got.Monitor.Messages)
	}
	input, _ := os.ReadFile(paths.Input)
	if !strings.Contains(string(input), "interrupted") {
		t.Fatalf("pilot input=%s", input)
	}
	// Replaying the retained turn and restarting work must not send another notification.
	e.m.processMonitorWork(bg, f.ID, pilot, planner)
	again, _ := os.ReadFile(paths.Input)
	if string(again) != string(input) {
		t.Fatalf("duplicate delivery: %s", again)
	}
}

func TestMonitorIngestsWhileClassificationWaitsAndReplacesPlanner(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	saveMonitorKey(t, "key")
	e := newEnv(t)
	f := takeoff(t, e)
	pilot, planner := f.sessionFor(RolePilot).SessionID, f.sessionFor(RolePlanner).SessionID
	pilotRT, _ := monitorRuntime(t, pilot)
	plannerRT, paths := monitorRuntime(t, planner, `{"type":"assistant","message":{"id":"one","content":[{"type":"text","text":"First"}]}}`, `{"type":"result","subtype":"success"}`)
	e.fk.chats.setRuntime(pilot, pilotRT)
	e.fk.chats.setRuntime(planner, plannerRT)
	e.m.monitor.configPath = filepath.Join(t.TempDir(), "monitor.yaml")
	started, release := make(chan struct{}), make(chan struct{})
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if calls.Add(1) == 1 {
			close(started)
			select {
			case <-release:
			case <-r.Context().Done():
				return
			}
		}
		_, _ = w.Write([]byte(`{"answers":{"attention":{"choice":"routine","confidence":0.9},"planning_artifacts":{"choice":"neither","confidence":0.9}}}`))
	}))
	defer srv.Close()
	// Always release a blocked handler before server cleanup, including on assertion failure.
	defer func() {
		select {
		case <-release:
		default:
			close(release)
		}
	}()
	old := monitorEndpoint
	monitorEndpoint = srv.URL
	t.Cleanup(func() { monitorEndpoint = old })
	events := monitorEvents(e.m, f.ID)
	startTestMonitor(t, e.m)
	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("first Jev request did not start")
	}
	output, err := os.OpenFile(paths.Output, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	_, err = output.WriteString("{\"type\":\"assistant\",\"message\":{\"id\":\"two\",\"content\":[{\"type\":\"text\",\"text\":\"Second\"}]}}\n{\"type\":\"result\",\"subtype\":\"success\"}\n")
	closeErr := output.Close()
	if err != nil || closeErr != nil {
		t.Fatalf("write output: %v %v", err, closeErr)
	}
	ingested := awaitMonitor(t, events, "second turn ingested while Jev blocked", func(f Flight) bool { return f.Monitor != nil && len(f.Monitor.Turns) == 2 })
	if ingested.Monitor.Turns[0].Status != MonitoredTurnPending || calls.Load() != 1 {
		t.Fatalf("classification was not blocked: %+v calls=%d", ingested.Monitor, calls.Load())
	}
	close(release)
	awaitMonitor(t, events, "both turns classified in order", func(f Flight) bool {
		return len(f.Monitor.Turns) == 2 && f.Monitor.Turns[1].Status == MonitoredTurnClassified
	})
	replacement, _ := monitorRuntime(t, "replacement", `{"type":"assistant","message":{"id":"new","content":[{"type":"text","text":"New planner"}]}}`, `{"type":"result","subtype":"success"}`)
	e.fk.chats.setRuntime("replacement", replacement)
	if err := e.m.editFlight(f.ID, func(f *Flight) error {
		f.upsertSession(RolePlanner, f.PlannerModel, "replacement", nil, e.m.deps.Now())
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	replaced := awaitMonitor(t, events, "replacement starts at its own cursor", func(f Flight) bool {
		return len(f.Monitor.Turns) == 3 && f.Monitor.Turns[2].Status == MonitoredTurnClassified
	})
	if replaced.Monitor.Turns[2].Turn.SessionID != "replacement" || replaced.Monitor.Turns[2].Turn.EndSeq != 2 {
		t.Fatalf("replacement source=%+v", replaced.Monitor.Turns[2])
	}
}

func TestMonitorRetryReadsChangedDefinitions(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	saveMonitorKey(t, "key")
	e := newEnv(t)
	f := takeoff(t, e)
	pilot, planner := f.sessionFor(RolePilot).SessionID, f.sessionFor(RolePlanner).SessionID
	e.m.monitor.configPath = filepath.Join(t.TempDir(), "monitor.yaml")
	writeFileT(t, e.m.monitorConfigPath(), "invalid: [")
	id, err := e.m.recordPlannerTurn(f.ID, PlannerTurn{SessionID: planner, StartSeq: 1, EndSeq: 2, Text: "response", Outcome: "completed"})
	if err != nil {
		t.Fatal(err)
	}
	e.m.processMonitorWork(bg, f.ID, pilot, planner)
	failed, _ := e.m.findMonitoredTurn(f.ID, id)
	if failed.Status != MonitoredTurnFailed || !strings.Contains(failed.Attempts[0].Error, "parse") {
		t.Fatalf("invalid YAML attempt=%+v", failed)
	}
	yaml := strings.Replace(string(monitorTemplateYAML), "Does this planner response", "Changed question: does this planner response", 1)
	writeFileT(t, e.m.monitorConfigPath(), yaml)
	var captured string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Questions map[string]struct {
				Instructions string `json:"instructions"`
			} `json:"questions"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
		}
		captured = body.Questions["attention"].Instructions
		_, _ = w.Write([]byte(`{"answers":{"attention":{"choice":"routine","confidence":0.9},"planning_artifacts":{"choice":"neither","confidence":0.9}}}`))
	}))
	defer srv.Close()
	old := monitorEndpoint
	monitorEndpoint = srv.URL
	t.Cleanup(func() { monitorEndpoint = old })
	if err := e.m.RetryMonitor(bg, f.ID, id); err != nil {
		t.Fatal(err)
	}
	e.m.processMonitorWork(bg, f.ID, pilot, planner)
	retried, _ := e.m.findMonitoredTurn(f.ID, id)
	if len(retried.Attempts) != 2 || retried.Status != MonitoredTurnClassified || !strings.Contains(captured, "Changed question") {
		t.Fatalf("retry=%+v question=%q", retried, captured)
	}
}

// A dashboard restart disposes the planner and re-spawns it seeded from the
// same conversation record, so the replacement shares record sequence numbers.
// Following the restart must resume after the retained cursor instead of
// re-classifying the seeded turn, and classify only the replacement's new turn.
func TestMonitorFollowsSessionRestart(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	saveMonitorKey(t, "key")
	e := newEnv(t)
	f := takeoff(t, e)
	pilot, planner := f.sessionFor(RolePilot).SessionID, f.sessionFor(RolePlanner).SessionID
	seeded := []string{
		`{"type":"assistant","message":{"id":"one","content":[{"type":"text","text":"First"}]}}`,
		`{"type":"result","subtype":"success"}`,
	}
	pilotRT, _ := monitorRuntime(t, pilot)
	plannerRT, _ := monitorRuntime(t, planner, seeded...)
	e.fk.chats.setRuntime(pilot, pilotRT)
	e.fk.chats.setRuntime(planner, plannerRT)
	e.m.monitor.configPath = filepath.Join(t.TempDir(), "monitor.yaml")
	var states []string
	var mu sync.Mutex
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			State string `json:"state"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
		}
		mu.Lock()
		states = append(states, body.State)
		mu.Unlock()
		_, _ = w.Write([]byte(`{"answers":{"attention":{"choice":"routine","confidence":0.9},"planning_artifacts":{"choice":"neither","confidence":0.9}}}`))
	}))
	defer srv.Close()
	old := monitorEndpoint
	monitorEndpoint = srv.URL
	t.Cleanup(func() { monitorEndpoint = old })
	events := monitorEvents(e.m, f.ID)
	startTestMonitor(t, e.m)
	awaitMonitor(t, events, "seeded turn classified on the original planner", func(f Flight) bool {
		return f.Monitor != nil && len(f.Monitor.Turns) == 1 && f.Monitor.Turns[0].Status == MonitoredTurnClassified
	})

	replacement, _ := monitorRuntime(t, "planner-2", append(append([]string(nil), seeded...),
		`{"type":"assistant","message":{"id":"two","content":[{"type":"text","text":"Second"}]}}`,
		`{"type":"result","subtype":"success"}`)...)
	e.fk.chats.setRuntime("planner-2", replacement)
	e.fk.chats.set("planner-2", true)
	if err := e.m.FollowSessionRestart(planner, "planner-2"); err != nil {
		t.Fatal(err)
	}
	followed := awaitMonitor(t, events, "replacement's new turn classified", func(f Flight) bool {
		return len(f.Monitor.Turns) == 2 && f.Monitor.Turns[1].Status == MonitoredTurnClassified
	})
	if got := followed.sessionFor(RolePlanner); got.SessionID != "planner-2" || len(got.PriorSessionIDs) != 1 || got.PriorSessionIDs[0] != planner {
		t.Fatalf("planner assignment = %+v", *got)
	}
	first, second := followed.Monitor.Turns[0], followed.Monitor.Turns[1]
	if first.Turn.SessionID != "planner-2" || first.Turn.EndSeq != 2 || second.Turn.SessionID != "planner-2" || second.Turn.EndSeq != 4 || second.Turn.Text != "Second" {
		t.Fatalf("turns = %+v", followed.Monitor.Turns)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(states) != 2 || states[0] != "First" || states[1] != "Second" {
		t.Fatalf("Jev requests = %q; the seeded turn must not be classified twice", states)
	}
}
