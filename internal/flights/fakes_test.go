package flights

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

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
}

func (f *fakeSpawner) LocalTarget(_ context.Context, req spawnlaunch.LocalTargetRequest) (spawnlaunch.LocalTargetResult, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, req)
	if err, ok := f.refuseIDs[req.WorkspaceID]; ok {
		return spawnlaunch.LocalTargetResult{}, err
	}
	if len(f.errs) > 0 {
		err := f.errs[0]
		f.errs = f.errs[1:]
		if err != nil {
			return spawnlaunch.LocalTargetResult{}, err
		}
	}
	f.n++
	return spawnlaunch.LocalTargetResult{
		SessionID: fmt.Sprintf("sess-%d", f.n), WorkspaceID: req.WorkspaceID,
		Target: req.TargetName, Prompt: req.Prompt, Nickname: req.Nickname,
	}, nil
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

type fakeSettings struct {
	fenceMode      string
	fenceInstalled bool
}

func (s *fakeSettings) FenceMode() string    { return s.fenceMode }
func (s *fakeSettings) FenceInstalled() bool { return s.fenceInstalled }

// fakes bundles every resource collaborator so a reloaded manager sees the same ones.
type fakes struct {
	repos      fakeRepos
	workspaces *fakeWorkspaces
	spawner    *fakeSpawner
	settings   *fakeSettings
	cleaner    *fakeCleaner
	namer      *fakeNamer
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
	}
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
