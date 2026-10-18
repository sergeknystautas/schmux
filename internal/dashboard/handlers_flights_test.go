package dashboard

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/gorilla/websocket"

	"github.com/sergeknystautas/schmux/internal/api/contracts"
	"github.com/sergeknystautas/schmux/internal/config"
	"github.com/sergeknystautas/schmux/internal/detect"
	"github.com/sergeknystautas/schmux/internal/disposal"
	"github.com/sergeknystautas/schmux/internal/flights"
	"github.com/sergeknystautas/schmux/internal/spawnlaunch"
	"github.com/sergeknystautas/schmux/internal/state"
)

const flightTestRepo = "https://example.com/r.git"

// flightResources stands in for every collaborator the flights manager drives,
// writing to the real test state store so availability joins have something to join.
type flightResources struct {
	mu         sync.Mutex
	st         *state.State
	wsDir      string
	allocErr   error
	spawnErr   error
	cleanErr   error
	sessionSeq int
}

func (f *flightResources) CheckChat(names []string) error {
	for _, n := range names {
		if n == "no-chat" {
			return errors.New("target no-chat has no chat mode")
		}
	}
	return nil
}

func (f *flightResources) GetOrCreateWithLabel(_ context.Context, repo, branch, label string) (*state.Workspace, error) {
	if f.allocErr != nil {
		return nil, f.allocErr
	}
	ws := state.Workspace{ID: "repo-001", Repo: repo, Branch: branch, Path: f.wsDir, VCS: "git"}
	f.st.AddWorkspace(ws)
	return &ws, nil
}

func (f *flightResources) LocalTarget(_ context.Context, req spawnlaunch.LocalTargetRequest) (spawnlaunch.LocalTargetResult, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.spawnErr != nil {
		return spawnlaunch.LocalTargetResult{}, f.spawnErr
	}
	f.sessionSeq++
	id := fmt.Sprintf("repo-001-s%d", f.sessionSeq)
	f.st.AddSession(state.Session{ID: id, WorkspaceID: req.WorkspaceID, Target: req.TargetName})
	return spawnlaunch.LocalTargetResult{SessionID: id, WorkspaceID: req.WorkspaceID, Target: req.TargetName}, nil
}

func (f *flightResources) FenceMode() string    { return "enabled" }
func (f *flightResources) FenceInstalled() bool { return false }

func (f *flightResources) DisposeAll(_ context.Context, req disposal.Request, observe func(disposal.ResourceOutcome)) (disposal.Result, error) {
	if f.cleanErr != nil {
		observe(disposal.ResourceOutcome{Kind: "workspace", ID: req.WorkspaceID, Err: f.cleanErr})
		return disposal.Result{}, f.cleanErr
	}
	f.st.RemoveWorkspace(req.WorkspaceID)
	return disposal.Result{WorkspacePurged: true}, nil
}

type flightEnv struct {
	server *Server
	cfg    *config.Config
	st     *state.State
	res    *flightResources
	mgr    *flights.Manager
	router http.Handler
}

func newFlightEnv(t *testing.T) *flightEnv {
	t.Helper()
	server, cfg, st := newTestServer(t)
	cfg.Repos = []config.Repo{{Name: "r", URL: flightTestRepo, BarePath: "r.git"}}
	res := &flightResources{st: st, wsDir: t.TempDir()}
	mgr := flights.New(flights.Dependencies{Repos: cfg, Workspaces: res}, server.logger)
	if err := mgr.Load(filepath.Join(t.TempDir(), "flights.json")); err != nil {
		t.Fatal(err)
	}
	mgr.Bind(flights.Binding{Targets: res, Spawner: res, Cleaner: res, Settings: res, Notify: server.BroadcastSessions})
	server.flights = mgr

	h := &FlightHandlers{manager: mgr, state: st, logger: server.logger}
	r := chi.NewRouter()
	r.Route("/api", func(r chi.Router) {
		h.registerReadRoutes(r)
		r.Group(func(r chi.Router) {
			r.Use(server.csrfMiddleware)
			h.registerMutatingRoutes(r)
		})
	})
	return &flightEnv{server: server, cfg: cfg, st: st, res: res, mgr: mgr, router: r}
}

func (e *flightEnv) do(t *testing.T, method, path string, body any) *httptest.ResponseRecorder {
	t.Helper()
	var buf bytes.Buffer
	switch b := body.(type) {
	case nil:
	case string:
		buf.WriteString(b)
	default:
		if err := json.NewEncoder(&buf).Encode(b); err != nil {
			t.Fatal(err)
		}
	}
	req := httptest.NewRequest(method, path, &buf)
	rr := httptest.NewRecorder()
	e.router.ServeHTTP(rr, req)
	return rr
}

func decode[T any](t *testing.T, rr *httptest.ResponseRecorder) T {
	t.Helper()
	var v T
	if err := json.Unmarshal(rr.Body.Bytes(), &v); err != nil {
		t.Fatalf("decode %q: %v", rr.Body.String(), err)
	}
	return v
}

func (e *flightEnv) create(t *testing.T) contracts.FlightDetailResponse {
	t.Helper()
	rr := e.do(t, http.MethodPost, "/api/flights", contracts.CreateFlightRequest{
		Repo: flightTestRepo, Prompt: "Build it", PlannerModel: "planner", ExecutorModel: "executor",
	})
	if rr.Code != http.StatusCreated {
		t.Fatalf("create status = %d: %s", rr.Code, rr.Body.String())
	}
	return decode[contracts.FlightDetailResponse](t, rr)
}

func (e *flightEnv) transition(t *testing.T, id, action string, body any) *httptest.ResponseRecorder {
	t.Helper()
	return e.do(t, http.MethodPost, "/api/flights/"+id+"/"+action, body)
}

func (e *flightEnv) planned(t *testing.T) contracts.FlightDetailResponse {
	t.Helper()
	f := e.create(t)
	rr := e.transition(t, f.ID, "takeoff", nil)
	if rr.Code != http.StatusOK {
		t.Fatalf("takeoff status = %d: %s", rr.Code, rr.Body.String())
	}
	return decode[contracts.FlightTransitionResponse](t, rr).Flight
}

func (e *flightEnv) writePlan(t *testing.T, f contracts.FlightDetailResponse) {
	t.Helper()
	if err := os.WriteFile(f.ArtifactPath, []byte("# plan\n"), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestFlightsAPI_Unavailable(t *testing.T) {
	server, _, _ := newTestServer(t)
	h := &FlightHandlers{state: server.state, logger: server.logger}
	r := chi.NewRouter()
	h.registerReadRoutes(r)
	h.registerMutatingRoutes(r)
	r.Get("/healthz", server.handleHealthz)
	for _, tc := range []struct{ method, path string }{
		{http.MethodGet, "/flights"},
		{http.MethodGet, "/flights/fl-old"},
		{http.MethodPost, "/flights"},
		{http.MethodPost, "/flights/order"},
		{http.MethodPatch, "/flights/fl-old"},
		{http.MethodPost, "/flights/fl-old/takeoff"},
		{http.MethodPost, "/flights/fl-old/execution"},
		{http.MethodPost, "/flights/fl-old/completion"},
		{http.MethodPost, "/flights/fl-old/done"},
		{http.MethodPost, "/flights/fl-old/cancel"},
		{http.MethodPost, "/flights/fl-old/dispose"},
	} {
		t.Run(tc.method+" "+tc.path, func(t *testing.T) {
			rr := httptest.NewRecorder()
			r.ServeHTTP(rr, httptest.NewRequest(tc.method, tc.path, nil))
			if rr.Code != http.StatusServiceUnavailable || !strings.Contains(rr.Body.String(), "flights unavailable") {
				t.Fatalf("response = %d %s", rr.Code, rr.Body.String())
			}
		})
	}
	rr := httptest.NewRecorder()
	r.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/healthz", nil))
	if rr.Code != http.StatusOK {
		t.Fatalf("health response = %d %s", rr.Code, rr.Body.String())
	}
	if msg := server.sessionsMessage(); msg["type"] != "sessions" || msg["workspaces"] == nil {
		t.Fatalf("dashboard snapshot unavailable: %v", msg)
	}
}

func TestFlightsAPI_CreateListAndGet(t *testing.T) {
	e := newFlightEnv(t)

	if rr := e.do(t, http.MethodGet, "/api/flights", nil); rr.Code != http.StatusOK ||
		strings.TrimSpace(rr.Body.String()) != `{"flights":[]}` {
		t.Fatalf("empty list = %d %s", rr.Code, rr.Body.String())
	}

	created := e.create(t)
	if created.WorkflowState != contracts.FlightStateBacklog || created.CleanupState != contracts.FlightCleanupIdle ||
		created.Prompt != "Build it" || created.PlannerModel != "planner" {
		t.Errorf("created = %+v", created)
	}

	list := decode[contracts.FlightsResponse](t, e.do(t, http.MethodGet, "/api/flights", nil))
	if len(list.Flights) != 1 || list.Flights[0].ID != created.ID || list.Flights[0].WorkflowState != contracts.FlightStateBacklog {
		t.Errorf("list = %+v", list)
	}

	got := e.do(t, http.MethodGet, "/api/flights/"+created.ID, nil)
	if got.Code != http.StatusOK || decode[contracts.FlightDetailResponse](t, got).Prompt != "Build it" {
		t.Errorf("get = %d %s", got.Code, got.Body.String())
	}
	if rr := e.do(t, http.MethodGet, "/api/flights/fl-nope", nil); rr.Code != http.StatusNotFound {
		t.Errorf("get unknown = %d, want 404", rr.Code)
	}
}

func TestFlightsAPI_EditStage(t *testing.T) {
	e := newFlightEnv(t)
	f := e.planned(t)
	rr := e.do(t, http.MethodPatch, "/api/flights/"+f.ID, map[string]string{"workflow_state": "executing"})
	if rr.Code != http.StatusOK {
		t.Fatalf("edit stage = %d %s", rr.Code, rr.Body.String())
	}
	got := decode[contracts.FlightDetailResponse](t, rr)
	if got.WorkflowState != contracts.FlightStateExecuting || len(got.Sessions) != len(f.Sessions) {
		t.Fatalf("edited flight = %+v", got)
	}
	rr = e.do(t, http.MethodGet, "/api/flights/"+f.ID, nil)
	if persisted := decode[contracts.FlightDetailResponse](t, rr); persisted.WorkflowState != contracts.FlightStateExecuting {
		t.Fatalf("persisted stage = %s", persisted.WorkflowState)
	}
}

func TestFlightsAPI_ErrorStatuses(t *testing.T) {
	e := newFlightEnv(t)
	f := e.create(t)
	e.planned(t) // second flight exists but we use the first below
	started := e.planned(t)
	validBody := contracts.CreateFlightRequest{Repo: flightTestRepo, Prompt: "p", PlannerModel: "planner", ExecutorModel: "executor"}
	unknownRepo := validBody
	unknownRepo.Repo = "https://example.com/other.git"
	noChat := validBody
	noChat.PlannerModel = "no-chat"

	tests := []struct {
		name   string
		method string
		path   string
		body   any
		want   int
		text   string
	}{
		{"create malformed json", http.MethodPost, "/api/flights", "{not json", 400, "invalid"},
		{"create unconfigured repo", http.MethodPost, "/api/flights", unknownRepo, 400, "repository"},
		{"create non-chat planner", http.MethodPost, "/api/flights", noChat, 400, "planner_model"},
		{"patch unknown flight", http.MethodPatch, "/api/flights/fl-nope", contracts.UpdateFlightRequest{}, 404, "not found"},
		{"patch started flight invalid prompt", http.MethodPatch, "/api/flights/" + started.ID, map[string]string{"prompt": " "}, 400, "prompt"},
		{"order with unknown id", http.MethodPost, "/api/flights/order", contracts.ReorderFlightsRequest{FlightIDs: []string{"fl-nope"}}, 400, "unknown"},
		{"order missing the backlog flight", http.MethodPost, "/api/flights/order", contracts.ReorderFlightsRequest{}, 400, "backlog"},
		{"order with started flight", http.MethodPost, "/api/flights/order", contracts.ReorderFlightsRequest{FlightIDs: []string{f.ID, started.ID}}, 409, "backlog"},
		{"takeoff unknown flight", http.MethodPost, "/api/flights/fl-nope/takeoff", nil, 404, "not found"},
		{"execution without plan", http.MethodPost, "/api/flights/" + started.ID + "/execution", nil, 409, "plan.md"},
		{"completion from planning", http.MethodPost, "/api/flights/" + started.ID + "/completion", nil, 409, "planning"},
		{"done from planning", http.MethodPost, "/api/flights/" + started.ID + "/done", nil, 409, "planning"},
		{"cancel a backlog flight", http.MethodPost, "/api/flights/" + f.ID + "/cancel", contracts.FlightCancelRequest{Reason: "x"}, 409, "delete"},
		{"dispose unconfirmed", http.MethodPost, "/api/flights/" + started.ID + "/dispose", contracts.FlightDisposeRequest{WorkspaceID: "repo-001"}, 409, "confirmation"},
		{"dispose wrong workspace", http.MethodPost, "/api/flights/" + started.ID + "/dispose", contracts.FlightDisposeRequest{WorkspaceID: "repo-999", Confirm: true}, 409, "repo-999"},
		{"dispose a backlog flight naming a workspace", http.MethodPost, "/api/flights/" + f.ID + "/dispose", contracts.FlightDisposeRequest{WorkspaceID: "repo-001", Confirm: true}, 409, "owns no workspace"},
		{"dispose malformed body", http.MethodPost, "/api/flights/" + started.ID + "/dispose", "[]", 400, "invalid"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rr := e.do(t, tt.method, tt.path, tt.body)
			if rr.Code != tt.want {
				t.Fatalf("status = %d, want %d: %s", rr.Code, tt.want, rr.Body.String())
			}
			var body map[string]string
			if err := json.Unmarshal(rr.Body.Bytes(), &body); err != nil || !strings.Contains(strings.ToLower(body["error"]), tt.text) {
				t.Errorf("body = %s, want an error containing %q", rr.Body.String(), tt.text)
			}
		})
	}
}

func TestFlightsAPI_UpdateDisposeAndReorderBacklog(t *testing.T) {
	e := newFlightEnv(t)
	a, b := e.create(t), e.create(t)
	newPrompt := "Rewritten"

	patched := e.do(t, http.MethodPatch, "/api/flights/"+a.ID, contracts.UpdateFlightRequest{Prompt: &newPrompt})
	if patched.Code != http.StatusOK || decode[contracts.FlightDetailResponse](t, patched).Prompt != "Rewritten" {
		t.Fatalf("patch = %d %s", patched.Code, patched.Body.String())
	}

	if rr := e.do(t, http.MethodPost, "/api/flights/order", contracts.ReorderFlightsRequest{FlightIDs: []string{b.ID, a.ID}}); rr.Code != http.StatusOK {
		t.Fatalf("reorder = %d %s", rr.Code, rr.Body.String())
	}
	list := decode[contracts.FlightsResponse](t, e.do(t, http.MethodGet, "/api/flights", nil))
	if len(list.Flights) != 2 || list.Flights[0].ID != b.ID || list.Flights[1].ID != a.ID {
		t.Errorf("order after reorder = %+v", list.Flights)
	}

	disposed := e.do(t, http.MethodPost, "/api/flights/"+a.ID+"/dispose", contracts.FlightDisposeRequest{Confirm: true})
	if disposed.Code != http.StatusOK {
		t.Fatalf("dispose = %d %s", disposed.Code, disposed.Body.String())
	}
	if !decode[contracts.FlightTransitionResponse](t, disposed).Removed {
		t.Error("dispose did not report removed=true")
	}
	if rr := e.do(t, http.MethodGet, "/api/flights/"+a.ID, nil); rr.Code != http.StatusNotFound {
		t.Errorf("disposed flight still served: %d", rr.Code)
	}
}

func TestFlightsAPI_ShortNameInRequestIsIgnored(t *testing.T) {
	e := newFlightEnv(t)

	body := `{"repo":"` + flightTestRepo + `","prompt":"Build it","short_name":"typed","planner_model":"planner","executor_model":"executor"}`
	rr := e.do(t, http.MethodPost, "/api/flights", body)
	if rr.Code != http.StatusCreated {
		t.Fatalf("create status = %d: %s", rr.Code, rr.Body.String())
	}
	var raw map[string]any
	if err := json.Unmarshal(rr.Body.Bytes(), &raw); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if _, ok := raw["short_name"]; ok {
		t.Errorf("create response carried short_name back: %s", rr.Body.String())
	}

	id := decode[contracts.FlightDetailResponse](t, rr).ID

	// PATCH with short_name only is a no-op success.
	prr := e.do(t, http.MethodPatch, "/api/flights/"+id, `{"short_name":"typed2"}`)
	if prr.Code != http.StatusOK {
		t.Fatalf("patch status = %d: %s", prr.Code, prr.Body.String())
	}
	raw = nil
	if err := json.Unmarshal(prr.Body.Bytes(), &raw); err != nil {
		t.Fatalf("decode patch: %v", err)
	}
	if _, ok := raw["short_name"]; ok {
		t.Errorf("patch response carried short_name back: %s", prr.Body.String())
	}
}

func TestFlightsAPI_NameErrorIsInSummaryAndDetail(t *testing.T) {
	e := newFlightEnv(t)
	// Only the daemon's namer writes name_error, so seed it through the snapshot.
	path := filepath.Join(t.TempDir(), "flights.json")
	snapshot := `{"flights":[{"id":"fl-named","repo":"` + flightTestRepo + `","prompt":"Build it",` +
		`"name_error":"boom","planner_model":"planner","executor_model":"executor",` +
		`"workflow_state":"backlog","cleanup_state":"idle","order":0,` +
		`"created_at":"2026-01-01T00:00:00Z","updated_at":"2026-01-01T00:00:00Z"}]}`
	if err := os.WriteFile(path, []byte(snapshot), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := e.mgr.Load(path); err != nil {
		t.Fatalf("Load: %v", err)
	}

	list := decode[contracts.FlightsResponse](t, e.do(t, http.MethodGet, "/api/flights", nil))
	if len(list.Flights) != 1 {
		t.Fatalf("list = %+v, want one flight", list.Flights)
	}
	if list.Flights[0].NameError != "boom" {
		t.Errorf("list name_error = %q, want boom", list.Flights[0].NameError)
	}
	detail := decode[contracts.FlightDetailResponse](t, e.do(t, http.MethodGet, "/api/flights/fl-named", nil))
	if detail.NameError != "boom" {
		t.Errorf("detail name_error = %q, want boom", detail.NameError)
	}
}

func TestFlightsAPI_FullWorkflow(t *testing.T) {
	e := newFlightEnv(t)
	f := e.planned(t)
	if f.WorkflowState != contracts.FlightStatePlanning || f.Workspace == nil || !f.Workspace.Available || f.ArtifactPath == "" {
		t.Fatalf("after takeoff = %+v", f)
	}
	if len(f.Sessions) != 1 || f.Sessions[0].Role != contracts.FlightRolePlanner || !f.Sessions[0].Available || f.Sessions[0].SessionID == "" {
		t.Errorf("planner assignment = %+v", f.Sessions)
	}
	e.writePlan(t, f)

	exec := e.transition(t, f.ID, "execution", nil)
	if exec.Code != http.StatusOK || decode[contracts.FlightTransitionResponse](t, exec).Flight.WorkflowState != contracts.FlightStateExecuting {
		t.Fatalf("execution = %d %s", exec.Code, exec.Body.String())
	}

	// Completion takes no body: it starts the commit agent in the workspace.
	comp := e.transition(t, f.ID, "completion", nil)
	completing := decode[contracts.FlightTransitionResponse](t, comp).Flight
	if comp.Code != http.StatusOK || completing.WorkflowState != contracts.FlightStateCompleting || len(completing.Sessions) != 3 {
		t.Fatalf("completion = %d %+v", comp.Code, completing)
	}
	if commit := completing.Sessions[2]; commit.Role != contracts.FlightRoleCommit || commit.Target != completing.ExecutorModel || !commit.Available {
		t.Errorf("commit assignment = %+v, want the executor model's session", commit)
	}

	done := e.transition(t, f.ID, "done", nil)
	if done.Code != http.StatusOK || decode[contracts.FlightTransitionResponse](t, done).Flight.WorkflowState != contracts.FlightStateDone {
		t.Fatalf("done = %d %s", done.Code, done.Body.String())
	}
	detail := decode[contracts.FlightDetailResponse](t, e.do(t, http.MethodGet, "/api/flights/"+f.ID, nil))
	actions := make([]string, len(detail.Operations))
	for i, op := range detail.Operations {
		actions[i] = op.Action + ":" + op.Status
	}
	if got := strings.Join(actions, ","); got != "takeoff:succeeded,execution:succeeded,completion:succeeded,done:succeeded" {
		t.Errorf("operation history = %s", got)
	}
}

func TestFlightsAPI_CompletionIgnoresAnyBody(t *testing.T) {
	e := newFlightEnv(t)
	f := e.planned(t)
	e.writePlan(t, f)
	e.transition(t, f.ID, "execution", nil)

	rr := e.transition(t, f.ID, "completion", map[string]any{"files": []string{"a.go"}})

	got := decode[contracts.FlightTransitionResponse](t, rr).Flight
	if rr.Code != http.StatusOK || got.WorkflowState != contracts.FlightStateCompleting {
		t.Fatalf("completion = %d %+v", rr.Code, got)
	}
}

func TestFlightsAPI_CancelRecordsTheReasonAndKeepsResources(t *testing.T) {
	e := newFlightEnv(t)
	f := e.planned(t)

	rr := e.transition(t, f.ID, "cancel", contracts.FlightCancelRequest{Reason: "wrong approach"})

	got := decode[contracts.FlightTransitionResponse](t, rr).Flight
	if rr.Code != http.StatusOK || got.WorkflowState != contracts.FlightStateCancelled || got.CancelReason != "wrong approach" || got.CancelledAt == "" {
		t.Fatalf("cancel = %d %+v", rr.Code, got)
	}
	if got.Workspace == nil || !got.Workspace.Available {
		t.Errorf("cancel dropped the workspace: %+v", got.Workspace)
	}

	// Cancel reason is optional: a follow-up flight, cancelled with an empty
	// reason, still records the transition.
	f2 := e.planned(t)
	rr2 := e.transition(t, f2.ID, "cancel", contracts.FlightCancelRequest{Reason: ""})
	got2 := decode[contracts.FlightTransitionResponse](t, rr2).Flight
	if rr2.Code != http.StatusOK || got2.WorkflowState != contracts.FlightStateCancelled || got2.CancelReason != "" || got2.CancelledAt == "" {
		t.Errorf("blank-reason cancel = %d %+v", rr2.Code, got2)
	}
}

func TestFlightsAPI_FailuresAreRecordedAndSurfacedForRetry(t *testing.T) {
	e := newFlightEnv(t)
	e.res.allocErr = errors.New("clone failed")
	f := e.create(t)

	rr := e.transition(t, f.ID, "takeoff", nil)

	if rr.Code != http.StatusInternalServerError || !strings.Contains(rr.Body.String(), "clone failed") {
		t.Fatalf("takeoff = %d %s, want 500 with the failure", rr.Code, rr.Body.String())
	}
	detail := decode[contracts.FlightDetailResponse](t, e.do(t, http.MethodGet, "/api/flights/"+f.ID, nil))
	if detail.WorkflowState != contracts.FlightStateBacklog || !strings.Contains(detail.LastError, "clone failed") {
		t.Errorf("detail after failed takeoff = %+v", detail)
	}
	list := decode[contracts.FlightsResponse](t, e.do(t, http.MethodGet, "/api/flights", nil))
	if !strings.Contains(list.Flights[0].LastError, "clone failed") {
		t.Errorf("summary last_error = %q", list.Flights[0].LastError)
	}

	e.res.allocErr = nil
	retry := e.transition(t, f.ID, "takeoff", nil)
	if retry.Code != http.StatusOK {
		t.Fatalf("retry = %d %s", retry.Code, retry.Body.String())
	}
	if after := decode[contracts.FlightDetailResponse](t, e.do(t, http.MethodGet, "/api/flights/"+f.ID, nil)); after.LastError != "" {
		t.Errorf("last_error after a successful retry = %q", after.LastError)
	}
}

func TestFlightsAPI_MissingLiveResourcesAreMarkedUnavailable(t *testing.T) {
	e := newFlightEnv(t)
	f := e.planned(t)
	e.st.RemoveWorkspace(f.Workspace.WorkspaceID)
	e.st.RemoveSession(f.Sessions[0].SessionID)

	got := decode[contracts.FlightDetailResponse](t, e.do(t, http.MethodGet, "/api/flights/"+f.ID, nil))

	if got.Workspace == nil || got.Workspace.Available || got.Workspace.WorkspaceID == "" {
		t.Errorf("workspace = %+v, want retained but unavailable", got.Workspace)
	}
	if len(got.Sessions) != 1 || got.Sessions[0].Available || got.Sessions[0].SessionID == "" {
		t.Errorf("sessions = %+v, want retained but unavailable", got.Sessions)
	}
}

func TestFlightsAPI_DisposeRemovesTheFlightOnlyAfterCleanupSucceeds(t *testing.T) {
	e := newFlightEnv(t)
	f := e.planned(t)
	body := contracts.FlightDisposeRequest{WorkspaceID: f.Workspace.WorkspaceID, Confirm: true}

	e.res.cleanErr = errors.New("workspace purge failed")
	failed := e.transition(t, f.ID, "dispose", body)
	if failed.Code != http.StatusInternalServerError || !strings.Contains(failed.Body.String(), "workspace purge failed") {
		t.Fatalf("failed dispose = %d %s", failed.Code, failed.Body.String())
	}
	kept := decode[contracts.FlightDetailResponse](t, e.do(t, http.MethodGet, "/api/flights/"+f.ID, nil))
	if kept.CleanupState != contracts.FlightCleanupDisposing || kept.WorkflowState != contracts.FlightStatePlanning || !strings.Contains(kept.LastError, "workspace purge failed") {
		t.Errorf("flight after failed dispose = %+v", kept)
	}

	e.res.cleanErr = nil
	ok := e.transition(t, f.ID, "dispose", body)
	resp := decode[contracts.FlightTransitionResponse](t, ok)
	if ok.Code != http.StatusOK || !resp.Removed || resp.Flight.ID != f.ID {
		t.Fatalf("dispose = %d %s", ok.Code, ok.Body.String())
	}
	if rr := e.do(t, http.MethodGet, "/api/flights/"+f.ID, nil); rr.Code != http.StatusNotFound {
		t.Errorf("disposed flight still served: %d", rr.Code)
	}
}

func TestFlightsAPI_MutatingRoutesRequireCSRFReadRoutesDoNot(t *testing.T) {
	e := newFlightEnv(t)
	f := e.create(t)
	e.cfg.RemoteAccess = &config.RemoteAccessConfig{Enabled: boolPtr(true)}

	mutating := []struct{ method, path string }{
		{http.MethodPost, "/api/flights"},
		{http.MethodPatch, "/api/flights/" + f.ID},
		{http.MethodPost, "/api/flights/order"},
		{http.MethodPost, "/api/flights/" + f.ID + "/takeoff"},
		{http.MethodPost, "/api/flights/" + f.ID + "/execution"},
		{http.MethodPost, "/api/flights/" + f.ID + "/completion"},
		{http.MethodPost, "/api/flights/" + f.ID + "/done"},
		{http.MethodPost, "/api/flights/" + f.ID + "/cancel"},
		{http.MethodPost, "/api/flights/" + f.ID + "/dispose"},
	}
	for _, m := range mutating {
		req := httptest.NewRequest(m.method, m.path, strings.NewReader("{}"))
		req.RemoteAddr = "8.8.8.8:1234"
		rr := httptest.NewRecorder()
		e.router.ServeHTTP(rr, req)
		if rr.Code != http.StatusForbidden {
			t.Errorf("%s %s without CSRF = %d, want 403", m.method, m.path, rr.Code)
		}
	}
	for _, path := range []string{"/api/flights", "/api/flights/" + f.ID} {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		req.RemoteAddr = "8.8.8.8:1234"
		rr := httptest.NewRecorder()
		e.router.ServeHTTP(rr, req)
		if rr.Code != http.StatusOK {
			t.Errorf("GET %s = %d, want 200 without CSRF", path, rr.Code)
		}
	}
	if got, ok := e.mgr.Get(f.ID); !ok || got.WorkflowState != flights.StateBacklog {
		t.Errorf("a rejected request changed the flight: %+v", got)
	}

	// DELETE /api/flights/{id} has no handler. Chi registers PATCH on the
	// same path, so the router returns 405: the path exists but DELETE is the
	// wrong method. What matters is that DELETE never reaches the handler
	// chain — the dispose flow is the only way to remove a flight.
	req := httptest.NewRequest(http.MethodDelete, "/api/flights/"+f.ID, strings.NewReader(""))
	rr := httptest.NewRecorder()
	e.router.ServeHTTP(rr, req)
	if rr.Code != http.StatusMethodNotAllowed {
		t.Errorf("DELETE /api/flights/%s = %d, want 405", f.ID, rr.Code)
	}
}

// readSessionsMsg reads dashboard messages until the next "sessions" snapshot;
// other message types (github_status, ...) interleave with the broadcasts.
func readSessionsMsg(t *testing.T, conn *websocket.Conn) map[string]interface{} {
	t.Helper()
	// Bound the entire wait for a snapshot, including interleaved messages.
	// Five seconds allows the local server's debounced broadcast to arrive.
	deadline := time.Now().Add(5 * time.Second)
	if err := conn.SetReadDeadline(deadline); err != nil {
		t.Fatal(err)
	}
	var observed []interface{}
	for {
		var msg map[string]interface{}
		if err := conn.ReadJSON(&msg); err != nil {
			t.Fatalf("waiting for sessions snapshot until %s: %v; observed message types = %v", deadline.Format(time.RFC3339Nano), err, observed)
		}
		observed = append(observed, msg["type"])
		if msg["type"] == "sessions" {
			return msg
		}
	}
}

func TestSessionsPayload_CarriesTheCompleteFlightsArray(t *testing.T) {
	e := newFlightEnv(t)
	conn, cleanup := dialTestDashboardWS(t, e.server)
	defer cleanup()

	initial := readSessionsMsg(t, conn)
	flightsField, present := initial["flights"]
	if !present {
		t.Fatalf("initial snapshot has no flights field: %v", initial)
	}
	if list, _ := flightsField.([]interface{}); len(list) != 0 {
		t.Errorf("initial flights = %v, want an empty array", flightsField)
	}

	created := e.create(t) // Notify -> BroadcastSessions
	msg := readSessionsMsg(t, conn)
	list, _ := msg["flights"].([]interface{})
	if len(list) != 1 || list[0].(map[string]interface{})["id"] != created.ID {
		t.Fatalf("broadcast flights = %v", msg["flights"])
	}

	e.do(t, http.MethodPost, "/api/flights/"+created.ID+"/dispose", contracts.FlightDisposeRequest{Confirm: true})
	msg = readSessionsMsg(t, conn)
	list, present2 := msg["flights"].([]interface{})
	if !present2 || len(list) != 0 {
		t.Errorf("broadcast after dispose flights = %v, want a full (empty) replacement array", msg["flights"])
	}
}

func TestSessionsPayload_OmitsFlightsWhenNoManagerIsWired(t *testing.T) {
	srv, _, _ := newTestServer(t)
	conn, cleanup := dialTestDashboardWS(t, srv)
	defer cleanup()

	initial := readSessionsMsg(t, conn)

	if _, present := initial["flights"]; present {
		t.Errorf("flights field present without a manager: %v", initial["flights"])
	}
}

func TestFlightCommitSettings(t *testing.T) {
	fenceReport := func(detected bool) func() detect.DependencyReport {
		return func() detect.DependencyReport {
			return detect.DependencyReport{Statuses: []detect.DependencyStatus{
				{Dependency: detect.Dependency{ID: "fence"}, Detected: detected, Command: "fence"},
			}}
		}
	}
	tests := []struct {
		name          string
		configure     func(*config.Config)
		report        func() detect.DependencyReport
		wantInstalled bool
		wantMode      string
	}{
		{
			name: "everything configured and fence installed",
			configure: func(c *config.Config) {
				c.FenceMode = config.FenceModeOptionalOn
			},
			report:        fenceReport(true),
			wantInstalled: true, wantMode: config.FenceModeOptionalOn,
		},
		{
			name: "defaults with fence not detected", configure: func(*config.Config) {},
			report: fenceReport(false), wantMode: config.FenceModeOptionalOff,
		},
		{
			name: "fence missing from the report", configure: func(*config.Config) {},
			report: func() detect.DependencyReport { return detect.DependencyReport{} }, wantMode: config.FenceModeOptionalOff,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &config.Config{}
			tt.configure(cfg)
			s := flightCommitSettings{config: cfg, report: tt.report}

			if got := s.FenceInstalled(); got != tt.wantInstalled {
				t.Errorf("FenceInstalled() = %v, want %v", got, tt.wantInstalled)
			}
			if got := s.FenceMode(); got != tt.wantMode {
				t.Errorf("FenceMode() = %q, want %q", got, tt.wantMode)
			}
		})
	}
}
