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
	"os/exec"
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

// SuggestBranch stands in for branch suggestion: it names a branch without
// calling a model.
func (f *flightResources) SuggestBranch(_ context.Context, _ string) (string, error) {
	return "feature/flight-work", nil
}

func (f *flightResources) DisposeAll(_ context.Context, req disposal.Request, observe func(disposal.ResourceOutcome)) (disposal.Result, error) {
	if f.cleanErr != nil {
		observe(disposal.ResourceOutcome{Kind: "workspace", ID: req.WorkspaceID, Err: f.cleanErr})
		return disposal.Result{}, f.cleanErr
	}
	f.st.RemoveWorkspace(req.WorkspaceID)
	return disposal.Result{WorkspacePurged: true}, nil
}

type flightEnv struct {
	flightsPath string
	server      *Server
	cfg         *config.Config
	st          *state.State
	res         *flightResources
	mgr         *flights.Manager
	router      http.Handler
}

func newFlightEnv(t *testing.T) *flightEnv {
	t.Helper()
	server, cfg, st := newTestServer(t)
	cfg.Repos = []config.Repo{{Name: "r", URL: flightTestRepo, BarePath: "r.git"}}
	cfg.FlightPilot = &config.FlightPilotConfig{Target: "pilot-model"}
	res := &flightResources{st: st, wsDir: t.TempDir()}
	mgr := flights.New(flights.Dependencies{Repos: cfg, Workspaces: res, Pilot: cfg, Branches: res}, server.logger)
	flightsPath := filepath.Join(t.TempDir(), "flights.json")
	if err := mgr.Load(flightsPath); err != nil {
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
	return &flightEnv{flightsPath: flightsPath, server: server, cfg: cfg, st: st, res: res, mgr: mgr, router: r}
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

// reportPlanning places a spec and plan where the planning skills' defaults
// put them and persists the planner's verified report, then reloads the
// snapshot as a daemon restart would. The report itself arrives through chat
// monitoring, which these HTTP tests do not run.
func (e *flightEnv) reportPlanning(t *testing.T, f contracts.FlightDetailResponse) {
	t.Helper()
	if out, err := exec.Command("git", "init", "-q", e.res.wsDir).CombinedOutput(); err != nil {
		t.Fatalf("git init: %v: %s", err, out)
	}
	paths := map[string]string{
		"spec_path": filepath.Join(e.res.wsDir, "docs", "superpowers", "specs", "s.md"),
		"plan_path": filepath.Join(e.res.wsDir, "docs", "superpowers", "plans", "p.md"),
	}
	for _, p := range paths {
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte("# doc\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	raw, err := os.ReadFile(e.flightsPath)
	if err != nil {
		t.Fatal(err)
	}
	var snap struct {
		Flights []map[string]any `json:"flights"`
	}
	if err := json.Unmarshal(raw, &snap); err != nil {
		t.Fatal(err)
	}
	for _, fl := range snap.Flights {
		if fl["id"] == f.ID {
			for k, v := range paths {
				fl[k] = v
			}
		}
	}
	if raw, err = json.Marshal(snap); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(e.flightsPath, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := e.mgr.Load(e.flightsPath); err != nil {
		t.Fatal(err)
	}
}

// writeFlightSnapshot rewrites one flight's row in the snapshot file and
// reloads it, as a daemon restart would.
func (e *flightEnv) writeFlightSnapshot(t *testing.T, flightID string, mutate func(flight map[string]any)) {
	t.Helper()
	raw, err := os.ReadFile(e.flightsPath)
	if err != nil {
		t.Fatal(err)
	}
	var snap struct {
		Flights []map[string]any `json:"flights"`
	}
	if err := json.Unmarshal(raw, &snap); err != nil {
		t.Fatal(err)
	}
	for _, fl := range snap.Flights {
		if fl["id"] == flightID {
			mutate(fl)
		}
	}
	out, err := json.Marshal(snap)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(e.flightsPath, out, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := e.mgr.Load(e.flightsPath); err != nil {
		t.Fatal(err)
	}
}

// flightPhaseVisit reads a flight's current phase visit from the snapshot.
func (e *flightEnv) flightPhaseVisit(t *testing.T, flightID string) uint64 {
	t.Helper()
	raw, err := os.ReadFile(e.flightsPath)
	if err != nil {
		t.Fatal(err)
	}
	var snap struct {
		Flights []map[string]any `json:"flights"`
	}
	if err := json.Unmarshal(raw, &snap); err != nil {
		t.Fatal(err)
	}
	for _, fl := range snap.Flights {
		if fl["id"] != flightID {
			continue
		}
		if v, ok := fl["phase_visit"].(float64); ok {
			return uint64(v)
		}
	}
	t.Fatalf("flight %s has no phase visit", flightID)
	return 0
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
		{http.MethodPost, "/flights/fl-old/review"},
		{http.MethodPost, "/flights/fl-old/completion"},
		{http.MethodPost, "/flights/fl-old/dispose"},
		{http.MethodPost, "/flights/fl-old/monitor/turn-1/retry"},
	} {
		t.Run(tc.method+" "+tc.path, func(t *testing.T) {
			rr := httptest.NewRecorder()
			r.ServeHTTP(rr, httptest.NewRequest(tc.method, tc.path, nil))
			if rr.Code != http.StatusServiceUnavailable || !strings.Contains(rr.Body.String(), `{"error":"flights unavailable`) {
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
		{"patch removed stage", http.MethodPatch, "/api/flights/" + started.ID, map[string]string{"workflow_state": "cancelled"}, 400, "workflow_state"},
		{"order with unknown id", http.MethodPost, "/api/flights/order", contracts.ReorderFlightsRequest{FlightIDs: []string{"fl-nope"}}, 400, "unknown"},
		{"order missing the backlog flight", http.MethodPost, "/api/flights/order", contracts.ReorderFlightsRequest{}, 400, "backlog"},
		{"order with started flight", http.MethodPost, "/api/flights/order", contracts.ReorderFlightsRequest{FlightIDs: []string{f.ID, started.ID}}, 409, "backlog"},
		{"takeoff unknown flight", http.MethodPost, "/api/flights/fl-nope/takeoff", nil, 404, "not found"},
		{"completion from planning", http.MethodPost, "/api/flights/" + started.ID + "/completion", nil, 409, "planning"},
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

// The pilot model is daemon configuration, read at each takeoff; a flight
// carries none. Unset, takeoff is refused before any workspace or session.
func TestFlightsAPI_TakeoffRequiresConfiguredPilot(t *testing.T) {
	e := newFlightEnv(t)
	e.cfg.FlightPilot = nil
	f := e.create(t)
	rr := e.transition(t, f.ID, "takeoff", nil)
	body := decode[struct{ Error string }](t, rr)
	if rr.Code != http.StatusConflict || !strings.Contains(body.Error, "Settings > Flights > Pilot Supervision") {
		t.Fatalf("takeoff without pilot = %d %s, want 409 naming the setting", rr.Code, rr.Body.String())
	}
	if len(e.st.GetWorkspaces()) != 0 || e.res.sessionSeq != 0 {
		t.Errorf("refused takeoff allocated workspaces=%d sessions=%d", len(e.st.GetWorkspaces()), e.res.sessionSeq)
	}
}

func TestFlightsAPI_FullWorkflow(t *testing.T) {
	e := newFlightEnv(t)
	f := e.planned(t)
	if f.WorkflowState != contracts.FlightStatePlanning || f.Workspace == nil || !f.Workspace.Available || f.SpecPath != "" || f.PlanPath != "" {
		t.Fatalf("after takeoff = %+v", f)
	}
	if len(f.Sessions) != 2 {
		t.Errorf("planner+pilot assignment = %+v, want 2", f.Sessions)
	}
	for _, assigned := range f.Sessions {
		if (assigned.Role != contracts.FlightRolePlanner && assigned.Role != contracts.FlightRolePilot) || !assigned.Available || assigned.SessionID == "" {
			t.Errorf("pilot/planner assignment = %+v", assigned)
		}
		if assigned.Role == contracts.FlightRolePilot && assigned.Target != "pilot-model" {
			t.Errorf("pilot target = %q, want the configured flight_pilot target", assigned.Target)
		}
	}
	e.reportPlanning(t, f)

	exec := e.transition(t, f.ID, "execution", nil)
	if exec.Code != http.StatusOK || decode[contracts.FlightTransitionResponse](t, exec).Flight.WorkflowState != contracts.FlightStateExecuting {
		t.Fatalf("execution = %d %s", exec.Code, exec.Body.String())
	}

	// Review takes no body: it starts the reviewer in the workspace.
	review := e.transition(t, f.ID, "review", nil)
	reviewing := decode[contracts.FlightTransitionResponse](t, review).Flight
	if review.Code != http.StatusOK || reviewing.WorkflowState != contracts.FlightStateReviewing || len(reviewing.Sessions) != 4 {
		t.Fatalf("review = %d %+v", review.Code, reviewing)
	}
	if reviewer := reviewing.Sessions[3]; reviewer.Role != contracts.FlightRoleReviewer || reviewer.Target != reviewing.PlannerModel || !reviewer.Available {
		t.Errorf("reviewer assignment = %+v, want the planner model's session", reviewer)
	}

	// Completion takes no body: it starts the commit agent in the workspace.
	comp := e.transition(t, f.ID, "completion", nil)
	completing := decode[contracts.FlightTransitionResponse](t, comp).Flight
	if comp.Code != http.StatusOK || completing.WorkflowState != contracts.FlightStateCompleting || len(completing.Sessions) != 5 {
		t.Fatalf("completion = %d %+v", comp.Code, completing)
	}
	if commit := completing.Sessions[4]; commit.Role != contracts.FlightRoleCommit || commit.Target != completing.ExecutorModel || !commit.Available {
		t.Errorf("commit assignment = %+v, want the executor model's session", commit)
	}

	// Completing is the last phase: there is no transition after it, only dispose.
	if rr := e.do(t, http.MethodPost, "/api/flights/"+f.ID+"/done", nil); rr.Code != http.StatusNotFound {
		t.Fatalf("POST /done = %d, want 404 (Mark done was removed)", rr.Code)
	}
	detail := decode[contracts.FlightDetailResponse](t, e.do(t, http.MethodGet, "/api/flights/"+f.ID, nil))
	actions := make([]string, len(detail.Operations))
	for i, op := range detail.Operations {
		actions[i] = op.Action + ":" + op.Status
	}
	if got := strings.Join(actions, ","); got != "takeoff:succeeded,execution:succeeded,review:succeeded,completion:succeeded" {
		t.Errorf("operation history = %s", got)
	}
}

func TestFlightsAPI_CompletionIgnoresAnyBody(t *testing.T) {
	e := newFlightEnv(t)
	f := e.planned(t)
	e.reportPlanning(t, f)
	e.transition(t, f.ID, "execution", nil)
	e.transition(t, f.ID, "review", nil)

	rr := e.transition(t, f.ID, "completion", map[string]any{"files": []string{"a.go"}})

	got := decode[contracts.FlightTransitionResponse](t, rr).Flight
	if rr.Code != http.StatusOK || got.WorkflowState != contracts.FlightStateCompleting {
		t.Fatalf("completion = %d %+v", rr.Code, got)
	}
}

func TestFlightsAPI_TakeoffIsNotAPlanningAction(t *testing.T) {
	e := newFlightEnv(t)
	f := e.planned(t)
	rr := e.transition(t, f.ID, "takeoff", nil)
	if rr.Code != http.StatusConflict {
		t.Fatalf("repeated takeoff = %d %s, want 409", rr.Code, rr.Body.String())
	}
	after := decode[contracts.FlightDetailResponse](t, e.do(t, http.MethodGet, "/api/flights/"+f.ID, nil))
	if len(after.Operations) != len(f.Operations) || e.res.sessionSeq != 2 {
		t.Fatalf("rejected takeoff created work: operations=%+v sessions=%d", after.Operations, e.res.sessionSeq)
	}
}

func TestFlightsAPI_DisposeIsTheOnlyRemovalPath(t *testing.T) {
	e := newFlightEnv(t)
	f := e.planned(t)
	path := "/api/flights/" + f.ID
	before := e.do(t, http.MethodGet, path, nil).Body.String()
	for _, tc := range []struct {
		method, path string
		want         int
	}{
		{http.MethodDelete, path, http.StatusMethodNotAllowed},
		{http.MethodPost, path + "/cancel", http.StatusNotFound},
		{http.MethodPost, path + "/terminate", http.StatusNotFound},
	} {
		t.Run(tc.method+" "+tc.path, func(t *testing.T) {
			rr := e.do(t, tc.method, tc.path, map[string]bool{"confirm": true})
			if rr.Code != tc.want {
				t.Fatalf("%s %s = %d %s, want %d", tc.method, tc.path, rr.Code, rr.Body.String(), tc.want)
			}
			if after := e.do(t, http.MethodGet, path, nil).Body.String(); after != before {
				t.Fatalf("removed action changed flight: before=%s after=%s", before, after)
			}
			if _, exists := e.st.GetWorkspace(f.Workspace.WorkspaceID); !exists {
				t.Fatal("removed action disposed the workspace")
			}
		})
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
	if len(got.Sessions) != 2 || got.Sessions[0].Available || got.Sessions[0].SessionID == "" {
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

func TestFlightMonitorExposesSavedQuestions(t *testing.T) {
	confidence := 0.9
	retained := flights.Flight{Sessions: []flights.SessionAssignment{{Role: flights.RolePlanner, SessionID: "planner"}}, Monitor: &flights.FlightMonitor{Turns: []flights.MonitoredTurn{{
		ID: "turn-planner-2", Turn: flights.PlannerTurn{SessionID: "planner", StartSeq: 1, EndSeq: 2},
		Attempts: []flights.MonitorAttempt{{Config: &flights.MonitorConfig{Model: "configured-model", Roles: map[string]flights.MonitorRole{
			"planner": {Classifiers: []flights.MonitorClassifier{{ID: "attention", Question: "Original question", Choices: []flights.MonitorChoice{{ID: "routine", Description: "Original choice"}}, NotifyOn: []string{"routine"}, MinConfidence: 0.7}}},
		}}, Classification: &flights.Classification{Model: "actual-model", Answers: []flights.MonitorAnswer{{ID: "attention", Choice: "routine", Confidence: &confidence}}}}},
	}}}}
	out := flightMonitor(retained)
	attempt := out.Turns[0].Attempts[0]
	if len(attempt.Questions) != 1 || attempt.Questions[0].Question != "Original question" || attempt.Questions[0].Choices[0].Description != "Original choice" || attempt.Model != "actual-model" {
		t.Fatalf("monitor API evidence=%+v", attempt)
	}
}

// executionEvidence patches the retained flight as the daemon's monitor would
// leave it during execution, then reloads the snapshot.
func (e *flightEnv) executionEvidence(t *testing.T, f contracts.FlightDetailResponse, executorSession string) {
	t.Helper()
	raw, err := os.ReadFile(e.flightsPath)
	if err != nil {
		t.Fatal(err)
	}
	var snap struct {
		Flights []map[string]any `json:"flights"`
	}
	if err := json.Unmarshal(raw, &snap); err != nil {
		t.Fatal(err)
	}
	for _, fl := range snap.Flights {
		if fl["id"] != f.ID {
			continue
		}
		fl["execution_started_at"] = "2026-10-20T12:00:00Z"
		fl["report_target"] = "/ws/docs/plans/p-execution.md"
		fl["report_path"] = "/ws/docs/plans/p-execution.md"
		fl["monitor"] = map[string]any{"turns": []map[string]any{{
			"id":         "turn-" + executorSession + "-2",
			"turn":       map[string]any{"session_id": executorSession, "start_seq": 1, "end_seq": 2, "text": "Done.", "outcome": "completed"},
			"execution":  map[string]any{"skill": "executing-plans invoked at record 21", "report_path": "/ws/docs/plans/p-execution.md", "report_verified": true},
			"status":     "skipped",
			"created_at": "2026-10-20T12:05:00Z", "updated_at": "2026-10-20T12:05:00Z",
		}}}
	}
	if raw, err = json.Marshal(snap); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(e.flightsPath, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := e.mgr.Load(e.flightsPath); err != nil {
		t.Fatal(err)
	}
}

func TestFlightsAPI_DetailCarriesExecutionEvidence(t *testing.T) {
	e := newFlightEnv(t)
	f := e.planned(t)
	e.executionEvidence(t, f, f.Sessions[1].SessionID)

	got := decode[contracts.FlightDetailResponse](t, e.do(t, http.MethodGet, "/api/flights/"+f.ID, nil))

	if got.ExecutionStartedAt != "2026-10-20T12:00:00Z" || got.ReportTarget != "/ws/docs/plans/p-execution.md" || got.ReportPath != "/ws/docs/plans/p-execution.md" {
		t.Errorf("execution fields = %q / %q / %q", got.ExecutionStartedAt, got.ReportTarget, got.ReportPath)
	}
	if got.Monitor == nil || len(got.Monitor.Turns) != 1 {
		t.Fatalf("monitor = %+v", got.Monitor)
	}
	want := contracts.FlightExecutionCheck{Skill: "executing-plans invoked at record 21", ReportPath: "/ws/docs/plans/p-execution.md", ReportVerified: true}
	if x := got.Monitor.Turns[0].Execution; x == nil || *x != want {
		t.Errorf("turn execution = %+v, want %+v", x, want)
	}
	list := decode[contracts.FlightsResponse](t, e.do(t, http.MethodGet, "/api/flights", nil))
	if s := list.Flights[0]; s.ExecutionStartedAt != got.ExecutionStartedAt || s.ReportTarget != got.ReportTarget || s.ReportPath != got.ReportPath {
		t.Errorf("summary execution fields = %q / %q / %q", s.ExecutionStartedAt, s.ReportTarget, s.ReportPath)
	}
}

// An executor turn's saved questions come from the executor role's classifiers,
// not the planner's.
func TestFlightMonitorExposesExecutorQuestions(t *testing.T) {
	retained := flights.Flight{
		Sessions: []flights.SessionAssignment{
			{Role: flights.RolePlanner, SessionID: "planner"},
			{Role: flights.RoleExecutor, SessionID: "executor", PriorSessionIDs: []string{"executor-old"}},
		},
		Monitor: &flights.FlightMonitor{Turns: []flights.MonitoredTurn{
			{ID: "turn-executor-2", Turn: flights.PlannerTurn{SessionID: "executor", StartSeq: 1, EndSeq: 2}, Attempts: []flights.MonitorAttempt{{Config: monitorConfigWithRoles()}}},
			{ID: "turn-executor-old-4", Turn: flights.PlannerTurn{SessionID: "executor-old", StartSeq: 3, EndSeq: 4}, Attempts: []flights.MonitorAttempt{{Config: monitorConfigWithRoles()}}},
		}},
	}

	out := flightMonitor(retained)

	for _, turn := range out.Turns {
		if len(turn.Attempts[0].Questions) != 1 || turn.Attempts[0].Questions[0].ID != "execution_state" {
			t.Errorf("turn %s questions = %+v, want the executor's execution_state", turn.ID, turn.Attempts[0].Questions)
		}
	}
}

func monitorConfigWithRoles() *flights.MonitorConfig {
	classifier := func(id string) flights.MonitorRole {
		return flights.MonitorRole{Classifiers: []flights.MonitorClassifier{{ID: id, Question: id + "?", Choices: []flights.MonitorChoice{{ID: "a", Description: "a"}, {ID: "b", Description: "b"}}}}}
	}
	return &flights.MonitorConfig{Model: "m", Roles: map[string]flights.MonitorRole{"planner": classifier("attention"), "executor": classifier("execution_state")}}
}

// The review route moves an executing flight into review and starts a
// reviewer with the planner's model.
func TestFlightsAPI_ReviewStartsAReviewer(t *testing.T) {
	e := newFlightEnv(t)
	f := e.planned(t)
	e.reportPlanning(t, f)
	e.transition(t, f.ID, "execution", nil)

	rr := e.transition(t, f.ID, "review", nil)

	got := decode[contracts.FlightTransitionResponse](t, rr).Flight
	if rr.Code != http.StatusOK || got.WorkflowState != contracts.FlightStateReviewing {
		t.Fatalf("review = %d %+v", rr.Code, got)
	}
	var reviewer *contracts.FlightSessionAssignment
	for i := range got.Sessions {
		if got.Sessions[i].Role == contracts.FlightRoleReviewer {
			reviewer = &got.Sessions[i]
		}
	}
	if reviewer == nil || reviewer.Target != got.PlannerModel || reviewer.SessionID == "" {
		t.Fatalf("reviewer assignment = %+v, want the planner's model", reviewer)
	}
	if got.ReviewStartedAt == "" {
		t.Error("review start was not recorded")
	}
}

// Review is refused from any phase but executing.
func TestFlightsAPI_ReviewIsRefusedOutsideExecuting(t *testing.T) {
	e := newFlightEnv(t)
	f := e.planned(t)

	rr := e.transition(t, f.ID, "review", nil)

	if rr.Code != http.StatusConflict {
		t.Fatalf("review from planning = %d %s, want 409", rr.Code, rr.Body.String())
	}
}

// The pilot's stop is visible while the flight is in the phase it was made
// in, and hidden once the phase changes.
func TestFlightsAPI_PilotStopVisibility(t *testing.T) {
	e := newFlightEnv(t)
	f := e.planned(t)
	stop := &flights.PilotStop{
		WorkflowState: flights.StatePlanning,
		Reason:        "the planner keeps changing the same file",
		SourceTurnID:  "turn-pilot-2",
		CreatedAt:     time.Now().UTC(),
	}
	raw, err := os.ReadFile(e.flightsPath)
	if err != nil {
		t.Fatal(err)
	}
	var snap struct {
		Flights []map[string]any `json:"flights"`
	}
	if err := json.Unmarshal(raw, &snap); err != nil {
		t.Fatal(err)
	}
	for _, fl := range snap.Flights {
		if fl["id"] == f.ID {
			fl["pilot_stop"] = stop
			fl["monitor"] = map[string]any{
				"turns": []map[string]any{{
					"id":     "turn-pilot-2",
					"turn":   map[string]any{"session_id": "pilot", "start_seq": 1, "end_seq": 2, "outcome": "completed"},
					"status": "classified",
				}},
				"messages": []map[string]any{{
					"id": "msg-1", "kind": "notification", "source_turn_id": "turn-pilot-2",
					"recipient_session_id": "pilot", "status": "accepted",
				}, {
					"id": "msg-2", "kind": "pilot_message", "source_turn_id": "turn-pilot-2", "sender_session_id": "pilot",
					"recipient_session_id": "planner", "text": "Write the acceptance criteria.", "status": "accepted",
				}},
			}
		}
	}
	if raw, err = json.Marshal(snap); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(e.flightsPath, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := e.mgr.Load(e.flightsPath); err != nil {
		t.Fatal(err)
	}

	got := decode[contracts.FlightDetailResponse](t, e.do(t, http.MethodGet, "/api/flights/"+f.ID, nil))
	if got.PilotStop == nil || got.PilotStop.Reason != stop.Reason {
		t.Fatalf("pilot stop = %+v, want the recorded reason", got.PilotStop)
	}
	list := decode[contracts.FlightsResponse](t, e.do(t, http.MethodGet, "/api/flights", nil))
	if s := list.Flights[0]; s.PilotStop == nil || s.PilotStop.Reason != stop.Reason {
		t.Fatalf("summary pilot stop = %+v, want the recorded reason", s.PilotStop)
	}
	// The pilot's latest guidance rides on the same payloads.
	if g := got.PilotGuidance; g == nil || g.Text != "Write the acceptance criteria." {
		t.Fatalf("detail pilot guidance = %+v, want the delivered message", g)
	}
	if g := list.Flights[0].PilotGuidance; g == nil || g.Text != "Write the acceptance criteria." {
		t.Fatalf("summary pilot guidance = %+v, want the delivered message", g)
	}
}

// The review reading is derived from the daemon's own classification of the
// reviewer's last turn.
func TestFlightsAPI_ReviewStatusIsDerivedFromTheReviewerTurn(t *testing.T) {
	f := flights.Flight{
		WorkflowState: flights.StateReviewing,
		Sessions: []flights.SessionAssignment{
			{Role: flights.RoleReviewer, SessionID: "reviewer"},
		},
		Monitor: &flights.FlightMonitor{Turns: []flights.MonitoredTurn{{
			ID:   "turn-reviewer-1",
			Turn: flights.PlannerTurn{SessionID: "reviewer", StartSeq: 1, EndSeq: 2},
			Attempts: []flights.MonitorAttempt{{
				Classification: &flights.Classification{Model: "m", Answers: []flights.MonitorAnswer{
					{ID: "review_state", Choice: "finished"},
					{ID: "tests", Choice: "run_passed"},
					{ID: "test_rules_review", Choice: "compliant"},
					{ID: "changed_work", Choice: "unchanged"},
					{ID: "artifacts_named", Choice: "named"},
				}},
			}},
			UpdatedAt: time.Date(2026, 10, 26, 12, 0, 0, 0, time.UTC),
		}}},
	}

	got := flightReviewStatus(f)

	if got == nil {
		t.Fatal("review status was not derived")
	}
	if got.State != "finished" || got.Tests != "run_passed" || got.TestRulesReview != "compliant" ||
		got.ChangedWork != "unchanged" || got.ArtifactsNamed != "named" {
		t.Fatalf("review status = %+v", got)
	}
	if got.ReviewerSessionID != "reviewer" || got.UpdatedAt != "2026-10-26T12:00:00Z" {
		t.Fatalf("review status = %+v", got)
	}
}

// When the pilot starts the next reviewer, the reading stays the one that
// explains why: the superseded reviewer's last classified turn, until the new
// reviewer has a classified turn of its own.
func TestFlightsAPI_ReviewStatusSpansPriorReviewers(t *testing.T) {
	classified := func(id, session, changed string, at int) flights.MonitoredTurn {
		return flights.MonitoredTurn{
			ID:   id,
			Turn: flights.PlannerTurn{SessionID: session, StartSeq: 1, EndSeq: 2},
			Attempts: []flights.MonitorAttempt{{
				Classification: &flights.Classification{Model: "m", Answers: []flights.MonitorAnswer{
					{ID: "review_state", Choice: "finished"},
					{ID: "changed_work", Choice: changed},
				}},
			}},
			UpdatedAt: time.Date(2026, 10, 26, at, 0, 0, 0, time.UTC),
		}
	}
	f := flights.Flight{
		WorkflowState: flights.StateReviewing,
		Sessions: []flights.SessionAssignment{
			{Role: flights.RolePlanner, SessionID: "planner"},
			{Role: flights.RoleReviewer, SessionID: "reviewer-2", PriorSessionIDs: []string{"reviewer-1"}},
		},
		Monitor: &flights.FlightMonitor{Turns: []flights.MonitoredTurn{
			classified("turn-reviewer-1", "reviewer-1", "changed", 10),
			// A turn from another role never counts as a review reading.
			classified("turn-planner", "planner", "unchanged", 11),
		}},
	}

	got := flightReviewStatus(f)
	if got == nil || got.ReviewerSessionID != "reviewer-1" || got.ChangedWork != "changed" {
		t.Fatalf("before the new reviewer speaks, review status = %+v, want reviewer-1's changed reading", got)
	}

	f.Monitor.Turns = append(f.Monitor.Turns, classified("turn-reviewer-2", "reviewer-2", "unchanged", 12))
	got = flightReviewStatus(f)
	if got == nil || got.ReviewerSessionID != "reviewer-2" || got.ChangedWork != "unchanged" {
		t.Fatalf("after the new reviewer speaks, review status = %+v, want reviewer-2's reading", got)
	}
}

// The list and detail show the pilot's latest guidance that reached its
// recipient or is held for it. Failed, unknown, pending, and sending messages
// and messages of other kinds are not guidance the recipient has.
func TestFlightPilotGuidanceIsTheLatestDeliveredOrQueuedPilotMessage(t *testing.T) {
	at := func(h int) time.Time { return time.Date(2026, 10, 26, h, 0, 0, 0, time.UTC) }
	msg := func(id, kind, to, text string, status flights.FlightMessageStatus, h int) flights.FlightMessage {
		return flights.FlightMessage{ID: id, Kind: kind, SenderSessionID: "pilot", RecipientSessionID: to, Text: text, Status: status, CreatedAt: at(h)}
	}
	f := flights.Flight{
		WorkflowState: flights.StateReviewing,
		Sessions: []flights.SessionAssignment{
			{Role: flights.RolePilot, SessionID: "pilot"},
			{Role: flights.RoleReviewer, SessionID: "reviewer-2", PriorSessionIDs: []string{"reviewer-1"}},
		},
		Monitor: &flights.FlightMonitor{Messages: []flights.FlightMessage{
			msg("m1", "pilot_message", "reviewer-1", "Rerun the tests after the fix.", flights.MessageStatusAccepted, 9),
			msg("m2", "notification", "pilot", "Reviewer turn finished.", flights.MessageStatusAccepted, 10),
			msg("m3", "pilot_message", "reviewer-2", "never sent", flights.MessageStatusFailed, 11),
			msg("m4", "next_reviewer", "reviewer-2", "Re-check the parser.", flights.MessageStatusAccepted, 12),
		}},
	}

	got := flightPilotGuidance(f)
	if got == nil || got.Text != "Rerun the tests after the fix." || got.RecipientRole != contracts.FlightRoleReviewer ||
		got.RecipientSessionID != "reviewer-1" || got.Status != "accepted" || got.CreatedAt != "2026-10-26T09:00:00Z" {
		t.Fatalf("pilot guidance = %+v, want the accepted message to reviewer-1", got)
	}

	f.Monitor.Messages = append(f.Monitor.Messages, msg("m5", "pilot_message", "reviewer-2", "Name the summary path.", flights.MessageStatusQueued, 13))
	if got = flightPilotGuidance(f); got == nil || got.Text != "Name the summary path." || got.Status != "queued" {
		t.Fatalf("pilot guidance = %+v, want the queued message to reviewer-2", got)
	}

	if got := flightPilotGuidance(flights.Flight{}); got != nil {
		t.Fatalf("pilot guidance without monitor = %+v, want nil", got)
	}
}

// A reviewer turn carries the daemon's skill evidence.
func TestFlightMonitorExposesReviewEvidence(t *testing.T) {
	retained := flights.Flight{
		Sessions: []flights.SessionAssignment{{Role: flights.RoleReviewer, SessionID: "reviewer"}},
		Monitor: &flights.FlightMonitor{Turns: []flights.MonitoredTurn{{
			ID:     "turn-reviewer-1",
			Turn:   flights.PlannerTurn{SessionID: "reviewer", StartSeq: 1, EndSeq: 2},
			Review: &flights.ReviewCheck{Skill: "test-rules-review invoked at record 21"},
		}}},
	}

	out := flightMonitor(retained)

	if out.Turns[0].Review == nil || out.Turns[0].Review.Skill != "test-rules-review invoked at record 21" {
		t.Fatalf("turn review = %+v, want the skill evidence", out.Turns[0].Review)
	}
}

// A stop from an earlier phase visit does not apply to the current one, even
// when the phase name matches. Both the summary and the detail hide it.
func TestFlightStopCurrentVisit(t *testing.T) {
	e := newFlightEnv(t)
	f := e.planned(t)

	// The stop was recorded in the flight's first visit; the flight is in its
	// second.
	stop := map[string]any{
		"workflow_state": string(flights.StatePlanning),
		"reason":         "the planner keeps changing the same file",
		"source_turn_id": "turn-pilot-2",
		"phase_visit":    1,
		"created_at":     time.Now().UTC().Format(time.RFC3339),
	}
	e.writeFlightSnapshot(t, f.ID, func(fl map[string]any) {
		fl["pilot_stop"] = stop
		fl["monitor"] = map[string]any{
			"turns": []map[string]any{{
				"id":     "turn-pilot-2",
				"turn":   map[string]any{"session_id": "pilot", "start_seq": 1, "end_seq": 2, "outcome": "completed"},
				"status": "classified",
			}},
		}
	})

	detail := decode[contracts.FlightDetailResponse](t, e.do(t, http.MethodGet, "/api/flights/"+f.ID, nil))
	if detail.PilotStop != nil {
		t.Fatalf("detail shows a stop from an earlier visit: %+v", detail.PilotStop)
	}
	list := decode[contracts.FlightsResponse](t, e.do(t, http.MethodGet, "/api/flights", nil))
	if list.Flights[0].PilotStop != nil {
		t.Fatalf("summary shows a stop from an earlier visit: %+v", list.Flights[0].PilotStop)
	}

	// The same stop in the current visit is visible in both.
	current := e.flightPhaseVisit(t, f.ID)
	e.writeFlightSnapshot(t, f.ID, func(fl map[string]any) {
		stop["phase_visit"] = current
		fl["pilot_stop"] = stop
		fl["monitor"] = map[string]any{
			"turns": []map[string]any{{
				"id":     "turn-pilot-2",
				"turn":   map[string]any{"session_id": "pilot", "start_seq": 1, "end_seq": 2, "outcome": "completed"},
				"status": "classified",
			}},
		}
	})
	detail = decode[contracts.FlightDetailResponse](t, e.do(t, http.MethodGet, "/api/flights/"+f.ID, nil))
	if detail.PilotStop == nil || detail.PilotStop.Reason != stop["reason"] {
		t.Fatalf("detail pilot stop = %+v, want the recorded reason", detail.PilotStop)
	}
	list = decode[contracts.FlightsResponse](t, e.do(t, http.MethodGet, "/api/flights", nil))
	if list.Flights[0].PilotStop == nil {
		t.Fatal("summary hides a stop from the current visit")
	}
}

// An operation the pilot asked for names the deciding turn in the detail
// response; a manual operation does not.
func TestFlightOperationProvenanceIsExposed(t *testing.T) {
	e := newFlightEnv(t)
	f := e.planned(t)

	var pilotSession string
	raw, err := os.ReadFile(e.flightsPath)
	if err != nil {
		t.Fatal(err)
	}
	var snap struct {
		Flights []map[string]any `json:"flights"`
	}
	if err := json.Unmarshal(raw, &snap); err != nil {
		t.Fatal(err)
	}
	for _, fl := range snap.Flights {
		if fl["id"] != f.ID {
			continue
		}
		for _, s := range fl["sessions"].([]any) {
			assignment, ok := s.(map[string]any)
			if !ok {
				continue
			}
			if assignment["role"] == "pilot" {
				pilotSession, _ = assignment["session_id"].(string)
			}
		}
	}
	if pilotSession == "" {
		t.Fatal("the flight has no pilot session")
	}
	turnID := "turn-" + pilotSession + "-2"

	e.writeFlightSnapshot(t, f.ID, func(fl map[string]any) {
		fl["operations"] = append(fl["operations"].([]any), map[string]any{
			"id": "op-auto", "action": "execution", "correlation_id": "corr-2", "status": "succeeded",
			"source_turn_id": turnID, "phase_visit": 2,
			"created_at": time.Now().UTC().Format(time.RFC3339), "updated_at": time.Now().UTC().Format(time.RFC3339),
		})
		fl["monitor"] = map[string]any{
			"turns": []map[string]any{{
				"id":     turnID,
				"turn":   map[string]any{"session_id": pilotSession, "start_seq": 1, "end_seq": 2, "outcome": "completed"},
				"status": "classified",
			}},
		}
	})

	detail := decode[contracts.FlightDetailResponse](t, e.do(t, http.MethodGet, "/api/flights/"+f.ID, nil))
	var automatic *contracts.FlightOperation
	manual := 0
	for i := range detail.Operations {
		switch detail.Operations[i].ID {
		case "op-auto":
			automatic = &detail.Operations[i]
		default:
			manual++
			if detail.Operations[i].SourceTurnID != "" {
				t.Fatalf("manual operation %s names a source turn: %q", detail.Operations[i].ID, detail.Operations[i].SourceTurnID)
			}
		}
	}
	if automatic == nil {
		t.Fatalf("operations = %+v, want the automatic one", detail.Operations)
	}
	if automatic.SourceTurnID != turnID {
		t.Fatalf("automatic operation source turn = %q, want %q", automatic.SourceTurnID, turnID)
	}
	if manual == 0 {
		t.Fatalf("operations = %+v, want the manual one", detail.Operations)
	}
}
