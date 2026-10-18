package flights

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestPromptChange(t *testing.T) {
	base := strings.Repeat("a", 400)
	tests := []struct {
		name string
		a, b string
		want func(got float64) bool
	}{
		{
			name: "identical",
			a:    base,
			b:    base,
			want: func(got float64) bool { return got == 0 },
		},
		{
			name: "one rune diff in 400 is below threshold",
			a:    base,
			b:    "b" + base[1:],
			want: func(got float64) bool { return got < 0.05 },
		},
		{
			name: "added 21 runes hits threshold",
			a:    base,
			b:    base + " Also add retry logic.",
			want: func(got float64) bool { return got >= 0.05 },
		},
		{
			name: "empty vs non-empty",
			a:    "",
			b:    "x",
			want: func(got float64) bool { return got == 1 },
		},
		{
			name: "empty vs empty",
			a:    "",
			b:    "",
			want: func(got float64) bool { return got == 0 },
		},
		{
			name: "multibyte single substitution",
			a:    "héllo",
			b:    "hello",
			want: func(got float64) bool { return got == 0.2 },
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := promptChange(tt.a, tt.b)
			if !tt.want(got) {
				t.Errorf("promptChange(%q, %q) = %v; predicate failed", tt.a, tt.b, got)
			}
		})
	}
}

func TestNeedsName(t *testing.T) {
	base := strings.Repeat("a", 400)
	tests := []struct {
		name string
		f    Flight
		want bool
	}{
		{
			name: "placeholder with hand-written name",
			f:    Flight{Prompt: base, ShortName: "typed"},
			want: true,
		},
		{
			name: "prompt unchanged from NamePrompt",
			f:    Flight{Prompt: base, NamePrompt: base},
			want: false,
		},
		{
			name: "prompt differs by one rune in 400",
			f:    Flight{Prompt: "b" + base[1:], NamePrompt: base},
			want: false,
		},
		{
			name: "prompt differs by 5%",
			f:    Flight{Prompt: base + " Also add retry logic.", NamePrompt: base},
			want: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := needsName(tt.f); got != tt.want {
				t.Errorf("needsName(%+v) = %v, want %v", tt.f, got, tt.want)
			}
		})
	}
}

// namingEnv bundles a manager, fakes, and the notice counter for naming tests.
type namingEnv struct {
	m       *Manager
	fk      *fakes
	notices *atomic.Int32
	clock   *atomic.Int64 // settable: tests Store(n) to jump clock to t0 + n seconds
}

func newNamingEnv(t *testing.T) *namingEnv {
	t.Helper()
	path := tempFlightPath(t)
	notices := &atomic.Int32{}
	fk := newFakes(t)
	m, clock := newNamingManager(t, path, notices, fk)
	return &namingEnv{m: m, fk: fk, notices: notices, clock: clock}
}

func tempFlightPath(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	return dir + "/flights.json"
}

func newNamingManager(t *testing.T, path string, notices *atomic.Int32, fk *fakes) (*Manager, *atomic.Int64) {
	t.Helper()
	ids := &atomic.Int32{}
	base := &atomic.Int64{}
	incr := &atomic.Int64{}
	m := New(Dependencies{
		Repos:   fk.repos,
		Targets: fakeTargets{bad: map[string]string{"no-chat": "target no-chat has no chat mode"}},
		Now: func() time.Time {
			n := incr.Add(1)
			return t0.Add(time.Duration(base.Load()+n) * time.Second)
		},
		NewID:      func() string { return fmt.Sprintf("fl-%d", ids.Add(1)) },
		Notify:     func() { notices.Add(1) },
		Workspaces: fk.workspaces,
		Spawner:    fk.spawner,
		Settings:   fk.settings,
		Cleaner:    fk.cleaner,
		Namer:      fk.namer,
	}, discard())
	if err := m.Load(path); err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	return m, base
}

// runPassWithin runs one naming pass and fails if it does not return within
// 5s. An in-memory pass with a fake namer takes milliseconds, so not
// returning means the namer callback is blocked on a lock the pass holds.
func runPassWithin(t *testing.T, m *Manager) {
	t.Helper()
	done := make(chan error, 1)
	go func() { done <- m.runNamingPass(context.Background()) }()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("runNamingPass: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("naming pass did not return within 5s: the namer callback is blocked on a lock the pass holds")
	}
}

func mustCreateFlight(t *testing.T, m *Manager, prompt string) Flight {
	t.Helper()
	f, err := m.Create(context.Background(), CreateRequest{
		Repo: testRepo, Prompt: prompt, PlannerModel: "planner", ExecutorModel: "executor",
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	return f
}

func TestNaming_CreateKicksAndPassNamesFlight(t *testing.T) {
	e := newNamingEnv(t)
	flight := mustCreateFlight(t, e.m, "Fix the login bug")
	e.fk.namer.names[flight.Prompt] = "Login fix"

	// After Create, the kick channel should have one pending signal.
	select {
	case <-e.m.nameKick:
	default:
		t.Errorf("expected kick after Create, got none")
	}

	e.notices.Store(0)
	if err := e.m.runNamingPass(context.Background()); err != nil {
		t.Fatalf("runNamingPass: %v", err)
	}
	got, ok := e.m.Get(flight.ID)
	if !ok {
		t.Fatalf("flight gone")
	}
	if got.ShortName != "Login fix" {
		t.Errorf("ShortName = %q, want %q", got.ShortName, "Login fix")
	}
	if got.NamePrompt != flight.Prompt {
		t.Errorf("NamePrompt = %q, want %q", got.NamePrompt, flight.Prompt)
	}
	if got.NameError != "" {
		t.Errorf("NameError = %q, want empty", got.NameError)
	}
	if e.notices.Load() < 1 {
		t.Errorf("notices = %d, want >=1", e.notices.Load())
	}
}

func TestNaming_SmallEditDoesNotRename(t *testing.T) {
	e := newNamingEnv(t)
	prompt := strings.Repeat("a", 400)
	flight := mustCreateFlight(t, e.m, prompt)
	e.fk.namer.names[flight.Prompt] = "First name"
	if err := e.m.runNamingPass(context.Background()); err != nil {
		t.Fatalf("first pass: %v", err)
	}

	// Update with a single rune change.
	newPrompt := "b" + prompt[1:]
	if _, err := e.m.Update(context.Background(), flight.ID, UpdateRequest{Prompt: &newPrompt}); err != nil {
		t.Fatalf("Update: %v", err)
	}

	before := e.fk.namer.callCount()
	if err := e.m.runNamingPass(context.Background()); err != nil {
		t.Fatalf("second pass: %v", err)
	}
	after := e.fk.namer.callCount()
	if after != before {
		t.Errorf("Name called %d times, want unchanged at %d", after, before)
	}
	got, _ := e.m.Get(flight.ID)
	if got.ShortName != "First name" {
		t.Errorf("ShortName = %q, want unchanged %q", got.ShortName, "First name")
	}
}

func TestNaming_LargeEditRenames(t *testing.T) {
	e := newNamingEnv(t)
	base := strings.Repeat("a", 400)
	flight := mustCreateFlight(t, e.m, base)
	e.fk.namer.names[flight.Prompt] = "First name"
	if err := e.m.runNamingPass(context.Background()); err != nil {
		t.Fatalf("first pass: %v", err)
	}

	newPrompt := base + " Also add retry logic."
	if _, err := e.m.Update(context.Background(), flight.ID, UpdateRequest{Prompt: &newPrompt}); err != nil {
		t.Fatalf("Update: %v", err)
	}
	e.fk.namer.names[newPrompt] = "Second name"
	if err := e.m.runNamingPass(context.Background()); err != nil {
		t.Fatalf("second pass: %v", err)
	}
	got, _ := e.m.Get(flight.ID)
	if got.ShortName != "Second name" {
		t.Errorf("ShortName = %q, want %q", got.ShortName, "Second name")
	}
	if got.NamePrompt != newPrompt {
		t.Errorf("NamePrompt = %q, want %q", got.NamePrompt, newPrompt)
	}
}

func TestNaming_ManySmallEditsAccumulate(t *testing.T) {
	e := newNamingEnv(t)
	base := strings.Repeat("a", 400)
	flight := mustCreateFlight(t, e.m, base)
	e.fk.namer.names[flight.Prompt] = "First name"
	if err := e.m.runNamingPass(context.Background()); err != nil {
		t.Fatalf("first pass: %v", err)
	}

	// Each iteration mutates a unique position with 'X'. With 400 chars, the
	// 5% threshold lands at 20 differing positions. The 21st edit crosses it.
	current := base
	prev := "First name"
	renames := 0
	for i := 0; i < 25; i++ {
		newPrompt := current[:i] + "X" + current[i+1:]
		e.fk.namer.names[newPrompt] = fmt.Sprintf("Name-%d", i)
		if _, err := e.m.Update(context.Background(), flight.ID, UpdateRequest{Prompt: &newPrompt}); err != nil {
			t.Fatalf("Update %d: %v", i, err)
		}
		current = newPrompt
		if err := e.m.runNamingPass(context.Background()); err != nil {
			t.Fatalf("pass %d: %v", i, err)
		}
		got, _ := e.m.Get(flight.ID)
		if got.ShortName != prev {
			renames++
			prev = got.ShortName
		}
	}
	if renames != 1 {
		t.Errorf("renames = %d, want exactly 1", renames)
	}
}

func TestNaming_PromptEditDuringCallDropsStaleName(t *testing.T) {
	e := newNamingEnv(t)
	flight := mustCreateFlight(t, e.m, "Initial prompt")
	e.fk.namer.names["Initial prompt"] = "Old name"

	updateErr := make(chan error, 1)
	e.fk.namer.setOnCall(func(prompt string) {
		newPrompt := "Different prompt"
		_, err := e.m.Update(context.Background(), flight.ID, UpdateRequest{Prompt: &newPrompt})
		updateErr <- err
	})

	runPassWithin(t, e.m)
	if err := <-updateErr; err != nil {
		t.Fatalf("Update during namer call: %v", err)
	}

	got, _ := e.m.Get(flight.ID)
	if got.ShortName != "" {
		t.Errorf("ShortName = %q, want empty", got.ShortName)
	}
	if got.NamePrompt != "" {
		t.Errorf("NamePrompt = %q, want empty", got.NamePrompt)
	}
}

func TestNaming_DoesNotHoldFlightOpLock(t *testing.T) {
	e := newNamingEnv(t)
	flight := mustCreateFlight(t, e.m, "Initial prompt")
	e.fk.namer.names[flight.Prompt] = "Some name"

	// A workspaceless flight disposes at once, under the per-flight op-lock.
	disposeErr := make(chan error, 1)
	e.fk.namer.setOnCall(func(prompt string) {
		_, err := e.m.Dispose(context.Background(), flight.ID, DisposeConfirmation{Confirmed: true})
		disposeErr <- err
	})

	runPassWithin(t, e.m)
	if err := <-disposeErr; err != nil {
		t.Fatalf("Dispose during namer call: %v", err)
	}
	if _, ok := e.m.Get(flight.ID); ok {
		t.Errorf("flight still present after dispose")
	}
}

func TestNaming_FailureKeepsOldNameAndRecordsError(t *testing.T) {
	e := newNamingEnv(t)
	flight := mustCreateFlight(t, e.m, "Original prompt")
	e.fk.namer.names[flight.Prompt] = "Old"
	if err := e.m.runNamingPass(context.Background()); err != nil {
		t.Fatalf("first pass: %v", err)
	}

	e.fk.namer.setErr(errors.New("boom"))
	newPrompt := flight.Prompt + strings.Repeat(" x", 11)
	if _, err := e.m.Update(context.Background(), flight.ID, UpdateRequest{Prompt: &newPrompt}); err != nil {
		t.Fatalf("Update: %v", err)
	}
	e.notices.Store(0)
	if err := e.m.runNamingPass(context.Background()); err != nil {
		t.Fatalf("failing pass: %v", err)
	}

	got, _ := e.m.Get(flight.ID)
	if got.ShortName != "Old" {
		t.Errorf("ShortName = %q, want unchanged %q", got.ShortName, "Old")
	}
	if got.NameError != "boom" {
		t.Errorf("NameError = %q, want %q", got.NameError, "boom")
	}
}

func TestNaming_RetryClearsError(t *testing.T) {
	e := newNamingEnv(t)
	flight := mustCreateFlight(t, e.m, "Original prompt")
	e.fk.namer.names[flight.Prompt] = "Old"
	if err := e.m.runNamingPass(context.Background()); err != nil {
		t.Fatalf("first pass: %v", err)
	}
	e.fk.namer.setErr(errors.New("boom"))
	newPrompt := flight.Prompt + strings.Repeat(" x", 11)
	if _, err := e.m.Update(context.Background(), flight.ID, UpdateRequest{Prompt: &newPrompt}); err != nil {
		t.Fatalf("Update: %v", err)
	}
	if err := e.m.runNamingPass(context.Background()); err != nil {
		t.Fatalf("failing pass: %v", err)
	}

	// Clear error and force a retry by jumping the clock past the 1m backoff.
	e.fk.namer.setErr(nil)
	e.fk.namer.names[newPrompt] = "New"
	e.clock.Store(120) // 120s past t0

	if err := e.m.runNamingPass(context.Background()); err != nil {
		t.Fatalf("retry pass: %v", err)
	}
	got, _ := e.m.Get(flight.ID)
	if got.NameError != "" {
		t.Errorf("NameError = %q, want empty after retry", got.NameError)
	}
	if got.ShortName != "New" {
		t.Errorf("ShortName = %q, want %q", got.ShortName, "New")
	}
}

func TestNextBackoff(t *testing.T) {
	tests := []struct {
		failures int
		want     time.Duration
	}{
		{0, 0},
		{1, time.Minute},
		{2, 2 * time.Minute},
		{3, 4 * time.Minute},
		{6, 32 * time.Minute},
		{7, time.Hour}, // 64m capped
		{50, time.Hour},
	}
	for _, tt := range tests {
		if got := nextBackoff(tt.failures); got != tt.want {
			t.Errorf("nextBackoff(%d) = %v, want %v", tt.failures, got, tt.want)
		}
	}
}

// passAt jumps the fake clock to t0+seconds and reports whether the pass
// called the namer. The clock also ticks 1s per Now call, so callers leave
// tens of seconds of margin around each backoff deadline.
func passAt(t *testing.T, e *namingEnv, seconds int64) bool {
	t.Helper()
	e.clock.Store(seconds)
	before := e.fk.namer.callCount()
	if err := e.m.runNamingPass(context.Background()); err != nil {
		t.Fatalf("runNamingPass at %ds: %v", seconds, err)
	}
	return e.fk.namer.callCount() > before
}

// failThreeTimes fails the flight's naming at 0s, 100s, and 300s; the third
// failure schedules the next attempt about 4m later.
func failThreeTimes(t *testing.T, e *namingEnv) {
	t.Helper()
	e.fk.namer.setErr(errors.New("boom"))
	for _, at := range []int64{0, 100, 300} {
		if !passAt(t, e, at) {
			t.Fatalf("pass at %ds made no namer call", at)
		}
	}
}

func TestNaming_FailedFlightWaitsOutItsBackoff(t *testing.T) {
	e := newNamingEnv(t)
	mustCreateFlight(t, e.m, "Original prompt")
	e.fk.namer.setErr(errors.New("boom"))

	steps := []struct {
		at     int64
		called bool
	}{
		{0, true},    // failure 1: next try ~1m out
		{40, false},  // inside 1m
		{100, true},  // failure 2: next try ~2m out
		{180, false}, // inside 2m
		{300, true},  // failure 3: next try ~4m out
		{480, false}, // inside 4m
		{600, true},
	}
	for _, s := range steps {
		if got := passAt(t, e, s.at); got != s.called {
			t.Fatalf("pass at %ds called namer = %v, want %v", s.at, got, s.called)
		}
	}
}

func TestNaming_PromptChangeRestartsBackoff(t *testing.T) {
	e := newNamingEnv(t)
	flight := mustCreateFlight(t, e.m, "Original prompt")
	failThreeTimes(t, e)

	newPrompt := flight.Prompt + " plus a new requirement"
	if _, err := e.m.Update(context.Background(), flight.ID, UpdateRequest{Prompt: &newPrompt}); err != nil {
		t.Fatalf("Update: %v", err)
	}
	if !passAt(t, e, 310) {
		t.Fatal("pass right after the prompt edit made no namer call")
	}
	// That failure is the first for the new prompt, so the wait is 1m, not 8m.
	if !passAt(t, e, 420) {
		t.Fatal("pass 110s after the first failure for the new prompt made no namer call")
	}
}

func TestNaming_SuccessRestartsBackoff(t *testing.T) {
	e := newNamingEnv(t)
	flight := mustCreateFlight(t, e.m, "Original prompt")
	failThreeTimes(t, e)

	e.fk.namer.setErr(nil)
	e.fk.namer.names[flight.Prompt] = "Named"
	if !passAt(t, e, 600) {
		t.Fatal("pass after the backoff made no namer call")
	}
	if got, _ := e.m.Get(flight.ID); got.ShortName != "Named" {
		t.Fatalf("ShortName = %q, want Named", got.ShortName)
	}

	newPrompt := flight.Prompt + " plus a new requirement"
	if _, err := e.m.Update(context.Background(), flight.ID, UpdateRequest{Prompt: &newPrompt}); err != nil {
		t.Fatalf("Update: %v", err)
	}
	e.fk.namer.setErr(errors.New("429"))
	if !passAt(t, e, 610) {
		t.Fatal("pass right after the prompt edit made no namer call")
	}
	// The success cleared the earlier failures, so this first failure waits 1m.
	if !passAt(t, e, 720) {
		t.Fatal("pass 110s after the first failure since the success made no namer call")
	}
}

func TestNaming_RepeatedIdenticalFailureDoesNotPersist(t *testing.T) {
	e := newNamingEnv(t)
	flight := mustCreateFlight(t, e.m, "Original prompt")
	e.fk.namer.setErr(errors.New("boom"))
	newPrompt := flight.Prompt + strings.Repeat(" x", 11)
	if _, err := e.m.Update(context.Background(), flight.ID, UpdateRequest{Prompt: &newPrompt}); err != nil {
		t.Fatalf("Update: %v", err)
	}
	if err := e.m.runNamingPass(context.Background()); err != nil {
		t.Fatalf("first failing pass: %v", err)
	}

	// Snapshot bytes and mod time should be unchanged by the next identical failure.
	firstInfo, err := os.Stat(e.m.path)
	if err != nil {
		t.Fatalf("Stat: %v", err)
	}
	firstBytes, err := os.ReadFile(e.m.path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if !strings.Contains(string(firstBytes), "boom") {
		t.Fatalf("first snapshot missing boom: %s", firstBytes)
	}

	e.notices.Store(0)
	e.clock.Store(120)
	if err := e.m.runNamingPass(context.Background()); err != nil {
		t.Fatalf("second failing pass: %v", err)
	}
	if e.notices.Load() != 0 {
		t.Errorf("notices after identical failure = %d, want 0", e.notices.Load())
	}
	secondInfo, err := os.Stat(e.m.path)
	if err != nil {
		t.Fatalf("Stat: %v", err)
	}
	secondBytes, err := os.ReadFile(e.m.path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if !secondInfo.ModTime().Equal(firstInfo.ModTime()) {
		t.Errorf("snapshot mod time changed: %v -> %v", firstInfo.ModTime(), secondInfo.ModTime())
	}
	if string(firstBytes) != string(secondBytes) {
		t.Errorf("snapshot bytes changed")
	}
}

func TestNaming_NamingOffWritesNothing(t *testing.T) {
	e := newNamingEnv(t)
	flight := mustCreateFlight(t, e.m, "Original prompt")
	e.fk.namer.setErr(ErrNamingOff)
	e.notices.Store(0)
	if err := e.m.runNamingPass(context.Background()); err != nil {
		t.Fatalf("naming-off pass: %v", err)
	}
	got, _ := e.m.Get(flight.ID)
	if got.NameError != "" {
		t.Errorf("NameError = %q, want empty", got.NameError)
	}
	if got.ShortName != "" {
		t.Errorf("ShortName = %q, want empty", got.ShortName)
	}
	if e.notices.Load() != 0 {
		t.Errorf("notices = %d, want 0", e.notices.Load())
	}

	// Re-enable naming; no backoff was set so the flight gets named now.
	e.fk.namer.setErr(nil)
	e.fk.namer.names[flight.Prompt] = "Recovered"
	if err := e.m.runNamingPass(context.Background()); err != nil {
		t.Fatalf("recovery pass: %v", err)
	}
	got, _ = e.m.Get(flight.ID)
	if got.ShortName != "Recovered" {
		t.Errorf("ShortName = %q, want %q", got.ShortName, "Recovered")
	}
}

func TestNaming_PlaceholderWithHandWrittenNameIsRenamed(t *testing.T) {
	path := tempFlightPath(t)
	writeFileT(t, path, `{"version":1,"flights":[`+
		`{"id":"fl-1","repo":"https://example.com/r.git","prompt":"Build the thing",`+
		`"short_name":"typed","planner_model":"planner","executor_model":"executor",`+
		`"workflow_state":"backlog","cleanup_state":"idle","order":0,`+
		`"created_at":"2026-01-01T00:00:00Z","updated_at":"2026-01-01T00:00:00Z"}]}`)

	notices := &atomic.Int32{}
	fk := newFakes(t)
	fk.namer.names["Build the thing"] = "Auto"
	m, _ := newNamingManager(t, path, notices, fk)

	if err := m.runNamingPass(context.Background()); err != nil {
		t.Fatalf("pass: %v", err)
	}
	got, ok := m.Get("fl-1")
	if !ok {
		t.Fatalf("flight gone")
	}
	if got.ShortName != "Auto" {
		t.Errorf("ShortName = %q, want %q", got.ShortName, "Auto")
	}
}

func TestNaming_RemovedTargetKeepsName(t *testing.T) {
	e := newNamingEnv(t)
	flight := mustCreateFlight(t, e.m, "Original prompt")
	e.fk.namer.names[flight.Prompt] = "Real"
	if err := e.m.runNamingPass(context.Background()); err != nil {
		t.Fatalf("first pass: %v", err)
	}

	e.fk.namer.setErr(ErrNamingOff)
	if err := e.m.runNamingPass(context.Background()); err != nil {
		t.Fatalf("naming-off pass: %v", err)
	}
	got, _ := e.m.Get(flight.ID)
	if got.ShortName != "Real" {
		t.Errorf("ShortName = %q, want preserved %q", got.ShortName, "Real")
	}
}
