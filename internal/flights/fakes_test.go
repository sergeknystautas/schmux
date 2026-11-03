package flights

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/sergeknystautas/schmux/internal/chat"
	"github.com/sergeknystautas/schmux/internal/config"
	"github.com/sergeknystautas/schmux/internal/disposal"
	"github.com/sergeknystautas/schmux/internal/spawnlaunch"
	"github.com/sergeknystautas/schmux/internal/state"
)

// concurrentResults awaits each worker's completion exactly once. Five seconds
// bounds local fixture I/O and lock acquisition; expiry fails rather than
// retrying an operation or any correctness assertion.
func concurrentResults(t *testing.T, operation string, workers int, run func() error) []error {
	t.Helper()
	results := make(chan error, workers)
	deadline := time.NewTimer(5 * time.Second)
	defer deadline.Stop()
	for i := 0; i < workers; i++ {
		go func() { results <- run() }()
	}
	observed := make([]error, 0, workers)
	for len(observed) < workers {
		select {
		case err := <-results:
			observed = append(observed, err)
		case <-deadline.C:
			t.Fatalf("%s: completed %d/%d workers within 5s; results = %v", operation, len(observed), workers, observed)
		}
	}
	return observed
}

type fakeRepos map[string]config.Repo

func (r fakeRepos) FindRepoByURL(url string) (config.Repo, bool) {
	repo, ok := r[url]
	return repo, ok
}

type allocCall struct{ Repo, Branch, Label string }

// fakeWorkspaces allocates a workspace rooted in a test temp dir.
type fakeWorkspaces struct {
	mu    sync.Mutex
	calls []allocCall
	// errs is consumed one entry per call; a nil entry (or an empty queue) succeeds.
	errs         []error
	path         string
	vcs          string
	branchSuffix string // appended to the requested branch to model the manager choosing a unique one
	// nextID, when set, overrides the next call's returned ID. The default
	// behavior (always returning "repo-001") matches existing assertions.
	nextID string
	// owners maps a workspace ID to the flight that stamped it.
	owners map[string]string
	// stampErr makes SetOwnerFlight fail.
	stampErr error
	// unpushed maps a workspace ID to the number of unpushed commits it
	// reports. A workspace that is absent from the map reports zero.
	unpushed map[string]int
	// unpushedErr makes UnpushedCommits fail.
	unpushedErr error
	// unpushedCalls counts the UnpushedCommits calls.
	unpushedCalls int
}

func (f *fakeWorkspaces) GetOrCreateWithLabel(_ context.Context, repo, branch, label string) (*state.Workspace, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, allocCall{repo, branch, label})
	if len(f.errs) > 0 {
		err := f.errs[0]
		f.errs = f.errs[1:]
		if err != nil {
			return nil, err
		}
	}
	id := "repo-001"
	if f.nextID != "" {
		id = f.nextID
		f.nextID = ""
	}
	return &state.Workspace{ID: id, Repo: repo, Branch: branch + f.branchSuffix, Path: f.path, VCS: f.vcs, Label: label}, nil
}

func (f *fakeWorkspaces) callCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.calls)
}

func (f *fakeWorkspaces) SetOwnerFlight(workspaceID, flightID string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.stampErr != nil {
		return f.stampErr
	}
	if f.owners == nil {
		f.owners = map[string]string{}
	}
	f.owners[workspaceID] = flightID
	return nil
}

func (f *fakeWorkspaces) OwnedWorkspace(flightID string) (state.Workspace, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for wsID, owner := range f.owners {
		if owner == flightID {
			return state.Workspace{ID: wsID, OwnerFlightID: flightID}, true
		}
	}
	return state.Workspace{}, false
}

func (f *fakeWorkspaces) UnpushedCommits(_ context.Context, workspaceID string) (int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.unpushedCalls++
	if f.unpushedErr != nil {
		return 0, f.unpushedErr
	}
	return f.unpushed[workspaceID], nil
}

// unpushedCallCount reports how many times UnpushedCommits was called.
func (f *fakeWorkspaces) unpushedCallCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.unpushedCalls
}

// fakeSpawner records local-target spawns and hands out sequential session IDs.
type fakeSpawner struct {
	mu    sync.Mutex
	calls []spawnlaunch.LocalTargetRequest
	errs  []error // consumed per call; nil entry or empty queue succeeds
	n     int
	// refuseIDs: spawning for any of these workspace IDs fails with the
	// matching error and yields an empty session ID — modeling a manager
	// that no longer recognizes a workspace that was removed by hand.
	refuseIDs map[string]error
	// onSpawn runs when a spawn begins, before it returns, so a test can
	// observe the manager while the spawn is still open.
	onSpawn func(req spawnlaunch.LocalTargetRequest)
	// block holds a spawn open until the channel is closed.
	block chan struct{}
}

func (f *fakeSpawner) LocalTarget(_ context.Context, req spawnlaunch.LocalTargetRequest) (spawnlaunch.LocalTargetResult, error) {
	f.mu.Lock()
	f.calls = append(f.calls, req)
	if err, ok := f.refuseIDs[req.WorkspaceID]; ok {
		f.mu.Unlock()
		return spawnlaunch.LocalTargetResult{}, err
	}
	if len(f.errs) > 0 {
		err := f.errs[0]
		f.errs = f.errs[1:]
		f.mu.Unlock()
		return spawnlaunch.LocalTargetResult{}, err
	}
	f.n++
	result := spawnlaunch.LocalTargetResult{
		SessionID: fmt.Sprintf("sess-%d", f.n), WorkspaceID: req.WorkspaceID,
		Target: req.TargetName, Prompt: req.Prompt, Nickname: req.Nickname,
	}
	onSpawn, block := f.onSpawn, f.block
	f.mu.Unlock()
	if onSpawn != nil {
		onSpawn(req)
	}
	if block != nil {
		<-block
	}
	return result, nil
}

func (f *fakeSpawner) callCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.calls)
}

func (f *fakeSpawner) last() spawnlaunch.LocalTargetRequest {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls[len(f.calls)-1]
}

// fakeBranches answers branch-name requests with a scripted name or error and
// records every prompt it was asked about.
type fakeBranches struct {
	mu      sync.Mutex
	branch  string
	err     error
	prompts []string
}

func (f *fakeBranches) SuggestBranch(_ context.Context, prompt string) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.prompts = append(f.prompts, prompt)
	return f.branch, f.err
}

type fakeSettings struct {
	fenceMode      string
	fenceInstalled bool
}

func (s *fakeSettings) FenceMode() string    { return s.fenceMode }
func (s *fakeSettings) FenceInstalled() bool { return s.fenceInstalled }

// fakePilot is the flight manager's pilot configuration.
type fakePilot struct {
	mu     sync.Mutex
	target string
}

func (p *fakePilot) GetFlightPilotTarget() string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.target
}

func (p *fakePilot) set(target string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.target = target
}

// fakes bundles every resource collaborator so a reloaded manager sees the same ones.
type fakes struct {
	repos      fakeRepos
	workspaces *fakeWorkspaces
	spawner    *fakeSpawner
	settings   *fakeSettings
	cleaner    *fakeCleaner
	namer      *fakeNamer
	pilot      *fakePilot
	chats      *fakeChats
	branches   *fakeBranches
	evidence   *fakeEvidence
	// clock is the shared manager clock: seconds past t0. Every manager built
	// over these fakes reads it, so a reloaded manager keeps the same time
	// and a test can jump the clock between two snapshots.
	clock *atomic.Int64
}

func newFakes(t *testing.T) *fakes {
	t.Helper()
	return &fakes{
		repos:      fakeRepos{testRepo: {Name: "r", URL: testRepo}},
		workspaces: &fakeWorkspaces{path: t.TempDir(), vcs: "git"},
		spawner:    &fakeSpawner{refuseIDs: map[string]error{}},
		settings:   &fakeSettings{fenceMode: "enabled"},
		cleaner:    &fakeCleaner{result: disposal.Result{WorkspacePurged: true}},
		namer:      &fakeNamer{names: map[string]string{}},
		pilot:      &fakePilot{target: "pilot-model"},
		chats:      &fakeChats{running: map[string]bool{}},
		branches:   &fakeBranches{branch: "feature/build-the-thing"},
		evidence:   &fakeEvidence{},
		clock:      &atomic.Int64{},
	}
}

// fakeChats treats every session it has been told about as running. Tests
// that need a different answer set Running(false) before calling Takeoff.
type fakeChats struct {
	mu         sync.Mutex
	running    map[string]bool
	runtimes   map[string]*chat.Runtime
	workspaces map[string]string
}

func (f *fakeChats) SessionWorkspaceID(id string) string {
	f.mu.Lock()
	defer f.mu.Unlock()
	if ws, ok := f.workspaces[id]; ok {
		return ws
	}
	return "repo-001"
}

func (f *fakeChats) IsRunning(_ context.Context, id string) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.running[id]
}

func (f *fakeChats) GetChatRuntime(id string) (*chat.Runtime, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	rt, ok := f.runtimes[id]
	if !ok {
		return nil, errors.New("fakeChats: no runtime for " + id)
	}
	return rt, nil
}

func (f *fakeChats) set(id string, running bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.running[id] = running
}

func (f *fakeChats) setRuntime(id string, rt *chat.Runtime) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.runtimes == nil {
		f.runtimes = map[string]*chat.Runtime{}
	}
	f.runtimes[id] = rt
}

// fakeCleaner stands in for the shared disposal service. During a call it
// reports its scripted outcomes through observe, runs onCall (to inspect the
// manager mid-cleanup), and then returns result/err.
type fakeCleaner struct {
	mu       sync.Mutex
	calls    []disposal.Request
	outcomes []disposal.ResourceOutcome
	onCall   func()
	result   disposal.Result
	err      error
	// sessionCalls records the session ID lists handed to DisposeSessions.
	sessionCalls [][]string
	// sessionErr makes DisposeSessions fail.
	sessionErr error
	// sessionOutcomes are played to the observer by DisposeSessions; when
	// nil, one success outcome per ID is played.
	sessionOutcomes []disposal.ResourceOutcome
}

func (f *fakeCleaner) DisposeAll(_ context.Context, req disposal.Request, observe func(disposal.ResourceOutcome)) (disposal.Result, error) {
	f.mu.Lock()
	f.calls = append(f.calls, req)
	outcomes, onCall, result, err := f.outcomes, f.onCall, f.result, f.err
	f.mu.Unlock()
	for _, o := range outcomes {
		observe(o)
	}
	if onCall != nil {
		onCall()
	}
	return result, err
}

func (f *fakeCleaner) DisposeSessions(_ context.Context, ids []string, observe func(disposal.ResourceOutcome)) ([]string, error) {
	f.mu.Lock()
	f.sessionCalls = append(f.sessionCalls, ids)
	outcomes, err := f.sessionOutcomes, f.sessionErr
	f.mu.Unlock()
	if outcomes == nil {
		for _, id := range ids {
			observe(disposal.ResourceOutcome{Kind: "session", ID: id})
		}
	} else {
		for _, o := range outcomes {
			observe(o)
		}
	}
	return ids, err
}

func (f *fakeCleaner) callCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.calls)
}

// fakeNamer is a stub Namer that lets tests script names per prompt, or one
// error for every call. onCall runs inside Name, outside mu, before Name
// returns, so it can act on the manager while the naming call is in flight.
type fakeNamer struct {
	mu     sync.Mutex
	names  map[string]string
	err    error
	calls  []string
	onCall func(prompt string)
}

func (f *fakeNamer) Name(_ context.Context, prompt string) (string, error) {
	f.mu.Lock()
	f.calls = append(f.calls, prompt)
	onCall, err := f.onCall, f.err
	name, ok := f.names[prompt]
	f.mu.Unlock()
	if onCall != nil {
		onCall(prompt)
	}
	if err != nil {
		return "", err
	}
	if !ok {
		return "", errors.New("fakeNamer: no scripted name")
	}
	return name, nil
}

func (f *fakeNamer) callCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.calls)
}

func (f *fakeNamer) setErr(err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.err = err
}

func (f *fakeNamer) setOnCall(fn func(prompt string)) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.onCall = fn
}

// fakeEvidence is an EvidenceArchiver that records each capture. onCapture
// runs inside Capture, so a test can observe the manager at that instant.
type fakeEvidence struct {
	mu        sync.Mutex
	captures  []evidenceCall
	err       error
	onCapture func(flight Flight, final bool)
}

type evidenceCall struct {
	Flight Flight
	Final  bool
}

func (f *fakeEvidence) Capture(_ context.Context, flight Flight, final bool) error {
	f.mu.Lock()
	f.captures = append(f.captures, evidenceCall{flight, final})
	err, onCapture := f.err, f.onCapture
	f.mu.Unlock()
	if onCapture != nil {
		onCapture(flight, final)
	}
	return err
}

func (f *fakeEvidence) CaptureFinalRow(flight Flight) error {
	f.mu.Lock()
	f.captures = append(f.captures, evidenceCall{flight, true})
	err, onCapture := f.err, f.onCapture
	f.mu.Unlock()
	if onCapture != nil {
		onCapture(flight, true)
	}
	return err
}

func (f *fakeEvidence) captureCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.captures)
}
