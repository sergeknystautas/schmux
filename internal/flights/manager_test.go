package flights

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/charmbracelet/log"
)

// fakeTargets treats every target as chat-capable except the ones listed.
type fakeTargets struct{ bad map[string]string }

func (f fakeTargets) CheckChat(names []string) error {
	for _, n := range names {
		if msg, bad := f.bad[n]; bad {
			return errors.New(msg)
		}
	}
	return nil
}

type testEnv struct {
	m       *Manager
	path    string
	notices *atomic.Int32
	ids     *atomic.Int32
	fk      *fakes
}

const testRepo = "https://example.com/r.git"

func discard() *log.Logger { return log.New(io.Discard) }

func writeFileT(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

func newManager(t *testing.T, path string, notices *atomic.Int32) (*Manager, *atomic.Int32) {
	t.Helper()
	return newManagerWith(t, path, notices, newFakes(t))
}

func newManagerWith(t *testing.T, path string, notices *atomic.Int32, fk *fakes) (*Manager, *atomic.Int32) {
	t.Helper()
	ids := &atomic.Int32{}
	clock := &atomic.Int64{}
	m := New(Dependencies{
		Repos:   fk.repos,
		Targets: fakeTargets{bad: map[string]string{"no-chat": "target no-chat has no chat mode"}},
		Now: func() time.Time {
			return t0.Add(time.Duration(clock.Add(1)) * time.Second)
		},
		NewID:      func() string { return fmt.Sprintf("fl-%d", ids.Add(1)) },
		Notify:     func() { notices.Add(1) },
		Workspaces: fk.workspaces,
		Spawner:    fk.spawner,
		Settings:   fk.settings,
		Cleaner:    fk.cleaner,
		Namer:      fk.namer,
		Pilot:      fk.pilot,
		Chats:      fk.chats,
	}, discard())
	if err := m.Load(path); err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	return m, ids
}

func newEnv(t *testing.T) *testEnv {
	t.Helper()
	path := filepath.Join(t.TempDir(), "flights.json")
	notices := &atomic.Int32{}
	fk := newFakes(t)
	m, ids := newManagerWith(t, path, notices, fk)
	return &testEnv{m: m, path: path, notices: notices, ids: ids, fk: fk}
}

// reload builds a second manager over the same file, as a daemon restart would.
func (e *testEnv) reload(t *testing.T) *Manager {
	t.Helper()
	m, _ := newManagerWith(t, e.path, &atomic.Int32{}, e.fk)
	return m
}

func goodCreate() CreateRequest {
	return CreateRequest{Repo: testRepo, Prompt: "Build the thing", PlannerModel: "planner", ExecutorModel: "executor"}
}

func mustCreate(t *testing.T, m *Manager, req CreateRequest) Flight {
	t.Helper()
	f, err := m.Create(context.Background(), req)
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	return f
}

func ids(fl []Flight) []string {
	out := make([]string, len(fl))
	for i, f := range fl {
		out[i] = f.ID
	}
	return out
}

func TestCreate_RecordsBacklogFlightAndPersistsIt(t *testing.T) {
	e := newEnv(t)
	req := goodCreate()
	req.Prompt = "  line one\n\nline two  \n"

	got := mustCreate(t, e.m, req)

	if got.ID != "fl-1" || got.WorkflowState != StateBacklog || got.CleanupState != CleanupIdle || got.Order != 0 {
		t.Errorf("flight = %+v", got)
	}
	if got.Prompt != req.Prompt {
		t.Errorf("prompt = %q, want it preserved verbatim %q", got.Prompt, req.Prompt)
	}
	if got.Repo != testRepo || got.PlannerModel != "planner" || got.ExecutorModel != "executor" {
		t.Errorf("fields = %+v", got)
	}
	if got.Workspace != nil || len(got.Sessions) != 0 {
		t.Errorf("new flight has assignments: %+v", got)
	}
	if e.notices.Load() != 1 {
		t.Errorf("notify count = %d, want 1", e.notices.Load())
	}
	reloaded, ok := e.reload(t).Get("fl-1")
	if !ok || reloaded.Prompt != req.Prompt {
		t.Errorf("reloaded = %+v, ok=%v", reloaded, ok)
	}
}

func TestCreate_AppendsToBacklogOrder(t *testing.T) {
	e := newEnv(t)

	a := mustCreate(t, e.m, goodCreate())
	b := mustCreate(t, e.m, goodCreate())

	if a.Order != 0 || b.Order != 1 {
		t.Errorf("orders = %d, %d, want 0, 1", a.Order, b.Order)
	}
}

func TestCreate_ValidationFailuresPersistNothing(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*CreateRequest)
		want   string
	}{
		{"unconfigured repository", func(r *CreateRequest) { r.Repo = "https://example.com/other.git" }, "repository"},
		{"empty repository", func(r *CreateRequest) { r.Repo = "" }, "repository"},
		{"empty prompt", func(r *CreateRequest) { r.Prompt = "" }, "prompt"},
		{"whitespace prompt", func(r *CreateRequest) { r.Prompt = " \n\t " }, "prompt"},
		{"empty planner", func(r *CreateRequest) { r.PlannerModel = "" }, "planner_model"},
		{"planner without chat mode", func(r *CreateRequest) { r.PlannerModel = "no-chat" }, "planner_model"},
		{"empty executor", func(r *CreateRequest) { r.ExecutorModel = "" }, "executor_model"},
		{"executor without chat mode", func(r *CreateRequest) { r.ExecutorModel = "no-chat" }, "executor_model"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := newEnv(t)
			req := goodCreate()
			tt.mutate(&req)

			_, err := e.m.Create(context.Background(), req)

			if !errors.Is(err, ErrInvalid) || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("Create() error = %v, want ErrInvalid mentioning %q", err, tt.want)
			}
			if n := len(e.m.List()); n != 0 {
				t.Errorf("flights after rejected create = %d, want 0", n)
			}
			if e.notices.Load() != 0 {
				t.Errorf("rejected create notified")
			}
		})
	}
}

func TestMutationsBeforeLoadFail(t *testing.T) {
	m := New(Dependencies{
		Repos: fakeRepos{testRepo: {}}, Targets: fakeTargets{},
		Now: time.Now, NewID: func() string { return "fl-x" }, Notify: func() {},
	}, discard())

	if _, err := m.Create(context.Background(), goodCreate()); err == nil {
		t.Fatal("Create() before Load() error = nil, want error")
	}
}

func TestLoad_InvalidRowStaysInactive(t *testing.T) {
	path := filepath.Join(t.TempDir(), "flights.json")
	writeFileT(t, path, `{"flights":[{"id":""}]}`)
	m := New(Dependencies{Repos: fakeRepos{}, Targets: fakeTargets{}, Now: time.Now, NewID: func() string { return "x" }, Notify: func() {}}, discard())

	if err := m.Load(path); err != nil {
		t.Fatalf("Load() error = %v, want nil", err)
	}
	if got := m.List(); len(got) != 0 {
		t.Fatalf("invalid flight became active: %+v", got)
	}
}

func TestEditKeepsInvalidRows(t *testing.T) {
	path := filepath.Join(t.TempDir(), "flights.json")
	raw := `{"future_top":{"keep":true},"flights":[{"id":"fl-good","repo":"https://example.com/r.git","prompt":"p","planner_model":"planner","executor_model":"executor","workflow_state":"backlog","cleanup_state":"idle","order":0,"created_at":"2026-01-01T00:00:00Z","updated_at":"2026-01-01T00:00:00Z"},{"id":"fl-typo","repo":"https://example.com/r.git","prompt":"p","planner_model":"planner","executor_model":"executor","workflow_state":"typo","cleanup_state":"idle","order":1,"created_at":"2026-01-01T00:00:00Z","updated_at":"2026-01-01T00:00:00Z"}]}`
	writeFileT(t, path, raw)
	m, _ := newManager(t, path, &atomic.Int32{})
	prompt := "changed"

	if _, err := m.Update(context.Background(), "fl-good", UpdateRequest{Prompt: &prompt}); err != nil {
		t.Fatalf("Update() error = %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var saved struct {
		FutureTop json.RawMessage `json:"future_top"`
		Flights   []struct {
			ID            string `json:"id"`
			WorkflowState string `json:"workflow_state"`
			Prompt        string `json:"prompt"`
		} `json:"flights"`
	}
	if err := json.Unmarshal(data, &saved); err != nil {
		t.Fatal(err)
	}
	var futureTop struct {
		Keep bool `json:"keep"`
	}
	if err := json.Unmarshal(saved.FutureTop, &futureTop); err != nil {
		t.Fatalf("decode future top-level field: %v\n%s", err, data)
	}
	if len(saved.Flights) != 2 || !futureTop.Keep || saved.Flights[0].ID != "fl-good" || saved.Flights[0].Prompt != "changed" ||
		saved.Flights[1].ID != "fl-typo" || saved.Flights[1].WorkflowState != "typo" {
		t.Fatalf("invalid row or valid edit lost: %s", data)
	}
}

func TestEditRefusesAfterExternalSnapshotChange(t *testing.T) {
	path := filepath.Join(t.TempDir(), "flights.json")
	m, _ := newManager(t, path, &atomic.Int32{})
	created := mustCreate(t, m, goodCreate())
	uploaded := `{"flights":[{"id":"fl-uploaded","workflow_state":"typo","cleanup_state":"idle"}]}`
	writeFileT(t, path, uploaded)
	prompt := "External upload"

	_, err := m.Update(context.Background(), created.ID, UpdateRequest{Prompt: &prompt})
	if !errors.Is(err, ErrConflict) {
		t.Fatalf("Update() error = %v, want ErrConflict", err)
	}
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(after) != uploaded {
		t.Fatalf("edit overwrote external snapshot: %s", after)
	}
}

func TestUpdate(t *testing.T) {
	e := newEnv(t)
	created := mustCreate(t, e.m, goodCreate())
	e.notices.Store(0)
	newPrompt := "A different prompt\n"
	newPlanner := "planner-2"

	got, err := e.m.Update(context.Background(), created.ID, UpdateRequest{Prompt: &newPrompt, PlannerModel: &newPlanner})

	if err != nil {
		t.Fatalf("Update() error = %v", err)
	}
	if got.Prompt != newPrompt || got.PlannerModel != "planner-2" {
		t.Errorf("updated fields = %+v", got)
	}
	if got.Repo != testRepo || got.ExecutorModel != "executor" {
		t.Errorf("untouched fields changed: %+v", got)
	}
	if !got.UpdatedAt.After(created.UpdatedAt) {
		t.Errorf("UpdatedAt not advanced: %v vs %v", got.UpdatedAt, created.UpdatedAt)
	}
	if e.notices.Load() != 1 {
		t.Errorf("notify count = %d, want 1", e.notices.Load())
	}
	if reloaded, _ := e.reload(t).Get(created.ID); reloaded.Prompt != newPrompt {
		t.Errorf("update not persisted: %+v", reloaded)
	}
}

func TestUpdateStage(t *testing.T) {
	for _, stage := range []WorkflowState{StateBacklog, StatePlanning, StateExecuting, StateCompleting, StateDone} {
		t.Run(string(stage), func(t *testing.T) {
			original := startedFlight("fl-s", StateExecuting)
			m, path := seedStarted(t, original, backlogFlight("fl-b", 0))
			got, err := m.Update(context.Background(), original.ID, UpdateRequest{WorkflowState: &stage})
			if err != nil {
				t.Fatal(err)
			}
			reloaded, _ := newManager(t, path, &atomic.Int32{})
			persisted, _ := reloaded.Get(original.ID)
			if got.WorkflowState != stage || persisted.WorkflowState != stage {
				t.Fatalf("stage = %s, persisted = %s; want %s", got.WorkflowState, persisted.WorkflowState, stage)
			}
			if got.Workspace.ID != original.Workspace.ID || len(got.Sessions) != len(original.Sessions) || len(got.Operations) != len(original.Operations) {
				t.Fatalf("stage edit changed resources or operations: %+v", got)
			}
			if stage == StateBacklog {
				if got.Order != 1 {
					t.Fatalf("backlog order = %d, want 1", got.Order)
				}
				// Stage-edit to backlog keeps the workspace assignment; the
				// record is removed via Dispose, which the dedicated test
				// covers.
			}
		})
	}
}

func TestUpdateStageRejectsUnknownValue(t *testing.T) {
	e := newEnv(t)
	f := mustCreate(t, e.m, goodCreate())
	stage := WorkflowState("invalid")
	if _, err := e.m.Update(context.Background(), f.ID, UpdateRequest{WorkflowState: &stage}); !errors.Is(err, ErrInvalid) {
		t.Fatalf("Update() error = %v, want ErrInvalid", err)
	}
	got, _ := e.m.Get(f.ID)
	if got.WorkflowState != StateBacklog {
		t.Fatalf("rejected edit changed stage to %s", got.WorkflowState)
	}
}

func TestUpdate_Rejections(t *testing.T) {
	e := newEnv(t)
	created := mustCreate(t, e.m, goodCreate())
	badRepo := "https://example.com/other.git"
	blank := "  "
	noChat := "no-chat"

	tests := []struct {
		name string
		id   string
		req  UpdateRequest
		want error
	}{
		{"unknown flight", "fl-404", UpdateRequest{}, ErrNotFound},
		{"unconfigured repository", created.ID, UpdateRequest{Repo: &badRepo}, ErrInvalid},
		{"blank prompt", created.ID, UpdateRequest{Prompt: &blank}, ErrInvalid},
		{"executor without chat mode", created.ID, UpdateRequest{ExecutorModel: &noChat}, ErrInvalid},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := e.m.Update(context.Background(), tt.id, tt.req)
			if !errors.Is(err, tt.want) {
				t.Fatalf("Update() error = %v, want %v", err, tt.want)
			}
		})
	}
	if got, _ := e.m.Get(created.ID); got.Repo != testRepo || got.Prompt != "Build the thing" {
		t.Errorf("rejected updates changed the flight: %+v", got)
	}
}

// seedStarted writes a snapshot holding the given flights and loads a manager over it.
func seedStarted(t *testing.T, fl ...Flight) (*Manager, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "flights.json")
	writeSnapshot(t, path, snapshot{Flights: fl})
	m, _ := newManager(t, path, &atomic.Int32{})
	return m, path
}

func TestEditingAfterWorkspaceAssignment(t *testing.T) {
	m, _ := seedStarted(t, startedFlight("fl-s", StatePlanning), backlogFlight("fl-b", 0))
	prompt := "new"
	planner, executor := "planner-2", "executor-2"

	got, updateErr := m.Update(context.Background(), "fl-s", UpdateRequest{Prompt: &prompt, PlannerModel: &planner, ExecutorModel: &executor})
	if updateErr != nil {
		t.Fatal(updateErr)
	}
	if got.Prompt != prompt || got.PlannerModel != planner || got.ExecutorModel != executor || got.Workspace.ID != "wa-fl-s" {
		t.Fatalf("edited flight = %+v", got)
	}
	reorderErr := m.ReorderBacklog(context.Background(), []string{"fl-b"})

	if reorderErr != nil {
		t.Errorf("Reorder() error = %v, want nil", reorderErr)
	}
}

func TestReorderBacklog(t *testing.T) {
	e := newEnv(t)
	a := mustCreate(t, e.m, goodCreate())
	b := mustCreate(t, e.m, goodCreate())
	c := mustCreate(t, e.m, goodCreate())

	if err := e.m.ReorderBacklog(context.Background(), []string{c.ID, a.ID, b.ID}); err != nil {
		t.Fatalf("ReorderBacklog() error = %v", err)
	}

	want := []string{c.ID, a.ID, b.ID}
	if got := ids(e.m.List()); fmt.Sprint(got) != fmt.Sprint(want) {
		t.Errorf("List() order = %v, want %v", got, want)
	}
	if got := ids(e.reload(t).List()); fmt.Sprint(got) != fmt.Sprint(want) {
		t.Errorf("order after restart = %v, want %v", got, want)
	}
}

func TestReorderBacklog_RequiresExactlyTheBacklog(t *testing.T) {
	tests := []struct {
		name string
		ids  []string
	}{
		{"missing a backlog flight", []string{"fl-1", "fl-2"}},
		{"duplicate id", []string{"fl-1", "fl-1", "fl-2", "fl-3"}},
		{"unknown id", []string{"fl-1", "fl-2", "fl-404"}},
		{"empty list", nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := newEnv(t)
			for i := 0; i < 3; i++ {
				mustCreate(t, e.m, goodCreate())
			}
			before := ids(e.m.List())

			err := e.m.ReorderBacklog(context.Background(), tt.ids)

			if !errors.Is(err, ErrInvalid) {
				t.Fatalf("ReorderBacklog() error = %v, want ErrInvalid", err)
			}
			if after := ids(e.m.List()); fmt.Sprint(after) != fmt.Sprint(before) {
				t.Errorf("order changed on rejection: %v -> %v", before, after)
			}
		})
	}
}

func TestListOrdersBacklogBeforeStartedFlights(t *testing.T) {
	started := startedFlight("fl-s", StatePlanning)
	m, _ := seedStarted(t, started, backlogFlight("fl-b2", 5), backlogFlight("fl-b1", 2))

	got := ids(m.List())

	want := []string{"fl-b1", "fl-b2", "fl-s"}
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Errorf("List() = %v, want %v", got, want)
	}
}

func TestListAndGetReturnCopies(t *testing.T) {
	e := newEnv(t)
	created := mustCreate(t, e.m, goodCreate())

	listed := e.m.List()
	listed[0].Prompt = "mutated"
	got, _ := e.m.Get(created.ID)
	got.Prompt = "mutated"

	if again, _ := e.m.Get(created.ID); again.Prompt != "Build the thing" {
		t.Errorf("caller mutation leaked into manager: %q", again.Prompt)
	}
}

func TestConcurrentCreatesAreAllPersistedWithDistinctOrders(t *testing.T) {
	e := newEnv(t)
	const n = 25
	results := concurrentResults(t, "Create", n, func() error {
		_, err := e.m.Create(context.Background(), goodCreate())
		return err
	})
	for _, err := range results {
		if err != nil {
			t.Errorf("Create() error = %v", err)
		}
	}

	for name, m := range map[string]*Manager{"live": e.m, "reloaded": e.reload(t)} {
		list := m.List()
		if len(list) != n {
			t.Fatalf("%s: flights = %d, want %d", name, len(list), n)
		}
		seen := map[int]bool{}
		for _, f := range list {
			seen[f.Order] = true
		}
		if len(seen) != n {
			t.Errorf("%s: distinct orders = %d, want %d", name, len(seen), n)
		}
	}
}

func TestBind_SuppliesCollaboratorsThatExistOnlyAfterTheServer(t *testing.T) {
	fk := newFakes(t)
	path := filepath.Join(t.TempDir(), "flights.json")
	// Match initFlights: config-backed collaborators exist at construction.
	m := New(Dependencies{Repos: fk.repos, Workspaces: fk.workspaces, Pilot: fk.pilot}, discard())
	if err := m.Load(path); err != nil {
		t.Fatal(err)
	}
	var notified atomic.Int32

	m.Bind(Binding{
		Targets: fakeTargets{}, Spawner: fk.spawner, Cleaner: fk.cleaner, Settings: fk.settings,
		Notify: func() { notified.Add(1) },
	})

	f, err := m.Create(bg, goodCreate())
	if err != nil {
		t.Fatalf("Create() after Bind error = %v", err)
	}
	if _, err := m.Takeoff(bg, f.ID); err != nil {
		t.Fatalf("Takeoff() after Bind error = %v", err)
	}
	if fk.spawner.callCount() != 2 || notified.Load() == 0 {
		t.Errorf("spawns = %d, notified = %d", fk.spawner.callCount(), notified.Load())
	}
}
