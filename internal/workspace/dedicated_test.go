package workspace

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/sergeknystautas/schmux/internal/config"
	"github.com/sergeknystautas/schmux/internal/state"
)

// dedicatedFixtureWithRepos builds a manager over the given repos and returns
// it with its state store.
func dedicatedFixtureWithRepos(t *testing.T, repos []config.Repo) (*Manager, *state.State) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	statePath := filepath.Join(t.TempDir(), "state.json")
	st := state.New(statePath, nil)
	cfg := &config.Config{}
	cfg.WorkspacePath = t.TempDir()
	cfg.WorktreeBasePath = t.TempDir()
	cfg.Repos = repos
	return New(cfg, st, statePath, testLogger()), st
}

func TestEnsureDedicated_CreatesAndReturnsSame(t *testing.T) {
	t.Parallel()
	repoDir := gitTestWorkTree(t)
	m, st := dedicatedFixtureWithRepos(t, []config.Repo{testRepoWithBarePath(t, "test", repoDir)})
	m.RegisterDedicated("test/a", DedicatedRegistration{RepoURL: repoDir, Branch: "fix/a"})

	ctx := context.Background()
	ws, err := m.EnsureDedicated(ctx, "test/a")
	if err != nil {
		t.Fatalf("EnsureDedicated: %v", err)
	}
	if ws.Dedicated != "test/a" {
		t.Errorf("Dedicated = %q", ws.Dedicated)
	}
	if ws.Status != state.WorkspaceStatusRunning {
		t.Errorf("Status = %q, want running", ws.Status)
	}
	if ws.Label != "test/a" {
		t.Errorf("Label = %q, want %q", ws.Label, "test/a")
	}

	again, err := m.EnsureDedicated(ctx, "test/a")
	if err != nil {
		t.Fatalf("EnsureDedicated second call: %v", err)
	}
	if again.ID != ws.ID {
		t.Errorf("second call returned %s, want %s", again.ID, ws.ID)
	}

	carrying := 0
	for _, w := range st.GetWorkspaces() {
		if w.Dedicated == "test/a" {
			carrying++
		}
	}
	if carrying != 1 {
		t.Errorf("%d workspaces carry the name, want 1", carrying)
	}
}

func TestEnsureDedicated_ExplicitLabel(t *testing.T) {
	t.Parallel()
	repoDir := gitTestWorkTree(t)
	m, _ := dedicatedFixtureWithRepos(t, []config.Repo{testRepoWithBarePath(t, "test", repoDir)})
	m.RegisterDedicated("test/a", DedicatedRegistration{RepoURL: repoDir, Branch: "fix/a", Label: "build-monitor-x"})

	ws, err := m.EnsureDedicated(context.Background(), "test/a")
	if err != nil {
		t.Fatalf("EnsureDedicated: %v", err)
	}
	if ws.Label != "build-monitor-x" {
		t.Errorf("Label = %q, want build-monitor-x", ws.Label)
	}
}

func TestEnsureDedicated_UnregisteredNameErrors(t *testing.T) {
	t.Parallel()
	repoDir := gitTestWorkTree(t)
	m, _ := dedicatedFixtureWithRepos(t, []config.Repo{testRepoWithBarePath(t, "test", repoDir)})

	_, err := m.EnsureDedicated(context.Background(), "nope")
	if err == nil {
		t.Fatal("expected an error for an unregistered name")
	}
	if !strings.Contains(err.Error(), "not registered") {
		t.Errorf("error = %v, want it to mention 'not registered'", err)
	}
}

func TestEnsureDedicated_AfterDisposalCreatesNew(t *testing.T) {
	t.Parallel()
	repoDir := gitTestWorkTree(t)
	m, st := dedicatedFixtureWithRepos(t, []config.Repo{testRepoWithBarePath(t, "test", repoDir)})
	m.RegisterDedicated("test/a", DedicatedRegistration{RepoURL: repoDir, Branch: "fix/a"})

	ctx := context.Background()
	first, err := m.EnsureDedicated(ctx, "test/a")
	if err != nil {
		t.Fatalf("EnsureDedicated: %v", err)
	}
	if err := m.Dispose(ctx, first.ID); err != nil {
		t.Fatalf("Dispose: %v", err)
	}
	if _, found := st.GetWorkspace(first.ID); found {
		t.Fatalf("disposal left the workspace record %s in state", first.ID)
	}

	second, err := m.EnsureDedicated(ctx, "test/a")
	if err != nil {
		t.Fatalf("EnsureDedicated after dispose: %v", err)
	}
	if second.Status != state.WorkspaceStatusRunning {
		t.Errorf("Status = %q, want running", second.Status)
	}
	if second.Dedicated != "test/a" {
		t.Errorf("Dedicated = %q, want test/a", second.Dedicated)
	}
	if _, err := os.Stat(second.Path); err != nil {
		t.Errorf("the new workspace has no directory: %v", err)
	}
	stored, found := st.GetWorkspace(second.ID)
	if !found {
		t.Fatalf("workspace %s missing from state", second.ID)
	}
	if stored.Dedicated != "test/a" {
		t.Errorf("Dedicated = %q, want test/a", stored.Dedicated)
	}
}

func TestEnsureDedicated_ReusedNumberNeverReturned(t *testing.T) {
	t.Parallel()
	repoDir := gitTestWorkTree(t)
	m, st := dedicatedFixtureWithRepos(t, []config.Repo{testRepoWithBarePath(t, "test", repoDir)})
	m.RegisterDedicated("test/a", DedicatedRegistration{RepoURL: repoDir, Branch: "fix/a"})

	ctx := context.Background()
	first, err := m.EnsureDedicated(ctx, "test/a")
	if err != nil {
		t.Fatalf("EnsureDedicated: %v", err)
	}
	if err := m.Dispose(ctx, first.ID); err != nil {
		t.Fatalf("Dispose: %v", err)
	}

	// An ordinary spawn takes the freed number.
	reused, err := m.GetOrCreate(ctx, repoDir, "main")
	if err != nil {
		t.Fatalf("GetOrCreate: %v", err)
	}
	if reused.ID != first.ID {
		t.Fatalf("expected the freed number %s to be reused, got %s", first.ID, reused.ID)
	}

	again, err := m.EnsureDedicated(ctx, "test/a")
	if err != nil {
		t.Fatalf("EnsureDedicated after reuse: %v", err)
	}
	if again.ID == reused.ID {
		t.Fatalf("EnsureDedicated returned the reused workspace %s", again.ID)
	}
	stored, _ := st.GetWorkspace(again.ID)
	if stored.Dedicated != "test/a" {
		t.Errorf("Dedicated = %q, want test/a", stored.Dedicated)
	}
	reusedStored, _ := st.GetWorkspace(reused.ID)
	if reusedStored.Dedicated != "" {
		t.Errorf("the reused workspace carries %q, want it unnamed", reusedStored.Dedicated)
	}
}

func TestEnsureDedicated_MismatchReplaced(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		prepare func(t *testing.T, m *Manager, st *state.State, ws *state.Workspace, repoDir, otherRepo string)
	}{
		{
			name: "wrong repo",
			prepare: func(t *testing.T, m *Manager, st *state.State, ws *state.Workspace, repoDir, otherRepo string) {
				m.RegisterDedicated("test/a", DedicatedRegistration{RepoURL: otherRepo, Branch: "fix/a"})
			},
		},
		{
			name: "disposing",
			prepare: func(t *testing.T, m *Manager, st *state.State, ws *state.Workspace, repoDir, otherRepo string) {
				stored, _ := st.GetWorkspace(ws.ID)
				stored.Status = state.WorkspaceStatusDisposing
				st.UpdateWorkspace(stored)
			},
		},
		{
			name: "missing directory",
			prepare: func(t *testing.T, m *Manager, st *state.State, ws *state.Workspace, repoDir, otherRepo string) {
				if err := os.RemoveAll(ws.Path); err != nil {
					t.Fatalf("RemoveAll: %v", err)
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			repoDir := gitTestWorkTree(t)
			otherRepo := gitTestWorkTree(t)
			repos := []config.Repo{
				testRepoWithBarePath(t, "test", repoDir),
				testRepoWithBarePath(t, "other", otherRepo),
			}
			m, st := dedicatedFixtureWithRepos(t, repos)
			m.RegisterDedicated("test/a", DedicatedRegistration{RepoURL: repoDir, Branch: "fix/a"})

			ctx := context.Background()
			first, err := m.EnsureDedicated(ctx, "test/a")
			if err != nil {
				t.Fatalf("EnsureDedicated: %v", err)
			}
			tt.prepare(t, m, st, first, repoDir, otherRepo)

			second, err := m.EnsureDedicated(ctx, "test/a")
			if err != nil {
				t.Fatalf("EnsureDedicated after mismatch: %v", err)
			}
			if second.ID == first.ID {
				t.Fatalf("expected a new workspace, got the mismatched %s", first.ID)
			}
			old, found := st.GetWorkspace(first.ID)
			if !found {
				t.Fatalf("the old workspace %s vanished from state", first.ID)
			}
			if old.Dedicated != "" {
				t.Errorf("the old workspace still carries %q, want it unnamed", old.Dedicated)
			}
			newStored, _ := st.GetWorkspace(second.ID)
			if newStored.Dedicated != "test/a" {
				t.Errorf("the new workspace carries %q, want test/a", newStored.Dedicated)
			}
		})
	}
}

func TestEnsureDedicated_NoAdoptionAfterUnstamp(t *testing.T) {
	t.Parallel()
	repoDir := gitTestWorkTree(t)
	m, st := dedicatedFixtureWithRepos(t, []config.Repo{testRepoWithBarePath(t, "test", repoDir)})
	m.RegisterDedicated("test/a", DedicatedRegistration{RepoURL: repoDir, Branch: "fix/a"})

	ctx := context.Background()
	first, err := m.EnsureDedicated(ctx, "test/a")
	if err != nil {
		t.Fatalf("EnsureDedicated: %v", err)
	}
	marker := filepath.Join(first.Path, "uncommitted.txt")
	if err := os.WriteFile(marker, []byte("work"), 0644); err != nil {
		t.Fatalf("write marker: %v", err)
	}

	if err := m.UnregisterDedicated("test/a"); err != nil {
		t.Fatalf("UnregisterDedicated: %v", err)
	}
	m.RegisterDedicated("test/a", DedicatedRegistration{RepoURL: repoDir, Branch: "fix/a"})

	second, err := m.EnsureDedicated(ctx, "test/a")
	if err != nil {
		t.Fatalf("EnsureDedicated after unstamp: %v", err)
	}
	if second.ID == first.ID {
		t.Fatalf("expected a new workspace, got the unstamped %s", first.ID)
	}
	if _, err := os.Stat(marker); err != nil {
		t.Errorf("the unstamped workspace's file was destroyed: %v", err)
	}
	old, found := st.GetWorkspace(first.ID)
	if !found {
		t.Fatalf("the old workspace %s vanished from state", first.ID)
	}
	if old.Dedicated != "" {
		t.Errorf("the old workspace still carries %q, want it unnamed", old.Dedicated)
	}
}

func TestGetOrCreate_SkipsDedicatedWorkspace(t *testing.T) {
	t.Parallel()
	repoDir := gitTestWorkTree(t)
	m, _ := dedicatedFixtureWithRepos(t, []config.Repo{testRepoWithBarePath(t, "test", repoDir)})
	m.RegisterDedicated("test/a", DedicatedRegistration{RepoURL: repoDir, Branch: "fix/a"})

	ctx := context.Background()
	dedicated, err := m.EnsureDedicated(ctx, "test/a")
	if err != nil {
		t.Fatalf("EnsureDedicated: %v", err)
	}
	marker := filepath.Join(dedicated.Path, "uncommitted.txt")
	if err := os.WriteFile(marker, []byte("work"), 0644); err != nil {
		t.Fatalf("write marker: %v", err)
	}

	spawned, err := m.GetOrCreate(ctx, repoDir, "fix/a")
	if err != nil {
		t.Fatalf("GetOrCreate: %v", err)
	}
	if spawned.ID == dedicated.ID {
		t.Fatalf("GetOrCreate took over the dedicated workspace %s", dedicated.ID)
	}
	if _, err := os.Stat(marker); err != nil {
		t.Errorf("the dedicated workspace's file was destroyed: %v", err)
	}
}

func TestUnregisterDedicated_KeepsWorkspace(t *testing.T) {
	t.Parallel()
	repoDir := gitTestWorkTree(t)
	m, st := dedicatedFixtureWithRepos(t, []config.Repo{testRepoWithBarePath(t, "test", repoDir)})
	m.RegisterDedicated("test/a", DedicatedRegistration{RepoURL: repoDir, Branch: "fix/a"})

	ctx := context.Background()
	ws, err := m.EnsureDedicated(ctx, "test/a")
	if err != nil {
		t.Fatalf("EnsureDedicated: %v", err)
	}
	if err := m.UnregisterDedicated("test/a"); err != nil {
		t.Fatalf("UnregisterDedicated: %v", err)
	}

	stored, found := st.GetWorkspace(ws.ID)
	if !found {
		t.Fatalf("workspace %s vanished from state", ws.ID)
	}
	if stored.Path != ws.Path {
		t.Errorf("path = %q, want %q", stored.Path, ws.Path)
	}
	if stored.Dedicated != "" {
		t.Errorf("Dedicated = %q, want it unnamed", stored.Dedicated)
	}
	if _, err := os.Stat(ws.Path); err != nil {
		t.Errorf("the workspace directory is gone: %v", err)
	}
}

func TestRegisteredDedicated_ListsPrefix(t *testing.T) {
	t.Parallel()
	repoDir := gitTestWorkTree(t)
	m, _ := dedicatedFixtureWithRepos(t, []config.Repo{testRepoWithBarePath(t, "test", repoDir)})
	m.RegisterDedicated("test/a", DedicatedRegistration{RepoURL: repoDir, Branch: "fix/a"})
	m.RegisterDedicated("test/b", DedicatedRegistration{RepoURL: repoDir, Branch: "fix/b"})
	m.RegisterDedicated("other", DedicatedRegistration{RepoURL: repoDir, Branch: "fix/o"})

	ctx := context.Background()
	a, err := m.EnsureDedicated(ctx, "test/a")
	if err != nil {
		t.Fatalf("EnsureDedicated test/a: %v", err)
	}
	b, err := m.EnsureDedicated(ctx, "test/b")
	if err != nil {
		t.Fatalf("EnsureDedicated test/b: %v", err)
	}
	if a.ID == b.ID {
		t.Fatalf("two names share one workspace %s", a.ID)
	}
	// The name outside the prefix exists too, so the listing below proves the
	// prefix filters it out rather than the name never having existed.
	other, err := m.EnsureDedicated(ctx, "other")
	if err != nil {
		t.Fatalf("EnsureDedicated other: %v", err)
	}
	if other.ID == a.ID || other.ID == b.ID {
		t.Fatalf("the third name shares a workspace: %s", other.ID)
	}

	got := m.RegisteredDedicated("test/")
	want := []string{"test/a", "test/b"}
	if len(got) != len(want) {
		t.Fatalf("RegisteredDedicated = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("RegisteredDedicated = %v, want %v", got, want)
		}
	}
}

func TestRegisterDedicated_RepoChangeRemovesName(t *testing.T) {
	t.Parallel()
	t.Run("repo changed while the daemon ran", func(t *testing.T) {
		t.Parallel()
		repoDir := gitTestWorkTree(t)
		otherRepo := gitTestWorkTree(t)
		repos := []config.Repo{
			testRepoWithBarePath(t, "test", repoDir),
			testRepoWithBarePath(t, "other", otherRepo),
		}
		m, st := dedicatedFixtureWithRepos(t, repos)
		m.RegisterDedicated("test/a", DedicatedRegistration{RepoURL: repoDir, Branch: "fix/a"})

		ctx := context.Background()
		first, err := m.EnsureDedicated(ctx, "test/a")
		if err != nil {
			t.Fatalf("EnsureDedicated: %v", err)
		}

		// The caller points the name at another repo.
		m.RegisterDedicated("test/a", DedicatedRegistration{RepoURL: otherRepo, Branch: "fix/a"})

		old, found := st.GetWorkspace(first.ID)
		if !found {
			t.Fatalf("the old workspace %s vanished from state", first.ID)
		}
		if old.Dedicated != "" {
			t.Errorf("the old workspace still carries %q after the repo changed", old.Dedicated)
		}

		second, err := m.EnsureDedicated(ctx, "test/a")
		if err != nil {
			t.Fatalf("EnsureDedicated after the repo change: %v", err)
		}
		if second.ID == first.ID {
			t.Fatalf("EnsureDedicated returned the old repo's workspace %s", second.ID)
		}
		if second.Repo != otherRepo {
			t.Errorf("repo = %q, want %q", second.Repo, otherRepo)
		}
	})

	t.Run("repo changed while the daemon was down", func(t *testing.T) {
		t.Parallel()
		repoDir := gitTestWorkTree(t)
		otherRepo := gitTestWorkTree(t)
		repos := []config.Repo{
			testRepoWithBarePath(t, "test", repoDir),
			testRepoWithBarePath(t, "other", otherRepo),
		}
		m, st := dedicatedFixtureWithRepos(t, repos)
		// The workspace a previous daemon run left behind, still carrying the name.
		if err := st.AddWorkspace(state.Workspace{
			ID:        "test-001",
			Repo:      repoDir,
			Branch:    "fix/a",
			Path:      t.TempDir(),
			Dedicated: "test/a",
		}); err != nil {
			t.Fatal(err)
		}

		m.RegisterDedicated("test/a", DedicatedRegistration{RepoURL: otherRepo, Branch: "fix/a"})

		stored, found := st.GetWorkspace("test-001")
		if !found {
			t.Fatal("the workspace vanished from state")
		}
		if stored.Dedicated != "" {
			t.Errorf("the old workspace still carries %q after the repo changed", stored.Dedicated)
		}
	})
}

// A repo change that arrives while an Ensure holds the name applies to whatever
// the Ensure leaves behind, not to what was there when the change arrived.
func TestRegisterDedicated_WaitsForInFlightEnsure(t *testing.T) {
	t.Parallel()
	const oldRepo, newRepo = "https://example.com/old.git", "https://example.com/new.git"
	tests := []struct {
		name     string
		before   []state.Workspace
		inFlight func(t *testing.T, st *state.State) // what the Ensure does while it holds the name
		want     map[string]string                   // workspace ID → Dedicated afterwards
	}{
		{
			name:   "the Ensure replaced the old repo's workspace",
			before: []state.Workspace{{ID: "ws-old", Repo: oldRepo, Dedicated: "test/a"}},
			inFlight: func(t *testing.T, st *state.State) {
				if err := st.UpdateWorkspace(state.Workspace{ID: "ws-old", Repo: oldRepo}); err != nil {
					t.Error(err)
				}
				if err := st.AddWorkspace(state.Workspace{ID: "ws-new", Repo: newRepo, Dedicated: "test/a"}); err != nil {
					t.Error(err)
				}
			},
			want: map[string]string{"ws-old": "", "ws-new": "test/a"},
		},
		{
			name: "the Ensure created a workspace in the old repo",
			inFlight: func(t *testing.T, st *state.State) {
				if err := st.AddWorkspace(state.Workspace{ID: "ws-old", Repo: oldRepo, Dedicated: "test/a"}); err != nil {
					t.Error(err)
				}
			},
			want: map[string]string{"ws-old": ""},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			m, st := dedicatedFixtureWithRepos(t, nil)
			m.RegisterDedicated("test/a", DedicatedRegistration{RepoURL: oldRepo, Branch: "fix/a"})
			for _, ws := range tt.before {
				if err := st.AddWorkspace(ws); err != nil {
					t.Fatal(err)
				}
			}

			// The name lock stands in for an Ensure in flight.
			lock := m.dedicatedLock("test/a")
			lock.Lock()
			done := make(chan struct{})
			started := time.Now()
			go func() {
				m.RegisterDedicated("test/a", DedicatedRegistration{RepoURL: newRepo, Branch: "fix/a"})
				close(done)
			}()
			select {
			case <-done:
				lock.Unlock()
				t.Fatalf("RegisterDedicated returned after %s while the name was in use", time.Since(started))
			case <-time.After(200 * time.Millisecond):
				// Bounded window: the claim is that register waits for the
				// in-flight ensure. One that raced past it would be through
				// in microseconds.
			}
			tt.inFlight(t, st)
			lock.Unlock()
			select {
			case <-done:
			case <-time.After(30 * time.Second):
				t.Fatalf("RegisterDedicated did not return %s after it started", time.Since(started))
			}

			for id, want := range tt.want {
				ws, found := st.GetWorkspace(id)
				if !found {
					t.Fatalf("workspace %s missing from state", id)
				}
				if ws.Dedicated != want {
					t.Errorf("%s carries %q, want %q", id, ws.Dedicated, want)
				}
			}
		})
	}
}

// The settings form saves the whole config on every edit, so registering what
// is already registered must not wait for a workspace being created.
func TestRegisterDedicated_UnchangedDoesNotWait(t *testing.T) {
	t.Parallel()
	m, _ := dedicatedFixtureWithRepos(t, nil)
	reg := DedicatedRegistration{RepoURL: "https://example.com/a.git", Branch: "fix/a"}
	m.RegisterDedicated("test/a", reg)

	lock := m.dedicatedLock("test/a")
	lock.Lock()
	defer lock.Unlock()
	done := make(chan struct{})
	go func() {
		m.RegisterDedicated("test/a", reg)
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(30 * time.Second): // failure backstop
		t.Fatal("an unchanged registration waited for the name lock")
	}
}

func TestUnregisterDedicated_WaitsForInFlightEnsure(t *testing.T) {
	t.Parallel()
	repoDir := gitTestWorkTree(t)
	m, st := dedicatedFixtureWithRepos(t, []config.Repo{testRepoWithBarePath(t, "test", repoDir)})
	m.RegisterDedicated("test/a", DedicatedRegistration{RepoURL: repoDir, Branch: "fix/a"})

	// The workspace an in-flight Ensure would have stamped.
	if err := st.AddWorkspace(state.Workspace{
		ID:        "test-001",
		Repo:      repoDir,
		Branch:    "fix/a",
		Path:      t.TempDir(),
		Dedicated: "test/a",
	}); err != nil {
		t.Fatal(err)
	}

	// The name lock stands in for an Ensure in flight: it holds the name while
	// it creates and stamps the workspace.
	lock := m.dedicatedLock("test/a")
	lock.Lock()

	done := make(chan error, 1)
	started := time.Now()
	go func() { done <- m.UnregisterDedicated("test/a") }()

	select {
	case err := <-done:
		lock.Unlock()
		t.Fatalf("UnregisterDedicated returned %v after %s while the name was in use", err, time.Since(started))
	case <-time.After(200 * time.Millisecond):
		// Bounded window: the claim is that unregister waits for the in-flight
		// ensure rather than racing past it.
	}

	lock.Unlock()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("UnregisterDedicated: %v", err)
		}
	case <-time.After(30 * time.Second):
		t.Fatalf("UnregisterDedicated did not return %s after the name was released", time.Since(started))
	}

	stored, found := st.GetWorkspace("test-001")
	if !found {
		t.Fatal("the workspace vanished from state")
	}
	if stored.Dedicated != "" {
		t.Errorf("the workspace still carries %q after unregistering", stored.Dedicated)
	}
}

func TestEnsureDedicated_ConcurrentCallsCreateOne(t *testing.T) {
	t.Parallel()
	repoDir := gitTestWorkTree(t)
	m, st := dedicatedFixtureWithRepos(t, []config.Repo{testRepoWithBarePath(t, "test", repoDir)})
	m.RegisterDedicated("test/a", DedicatedRegistration{RepoURL: repoDir, Branch: "fix/a"})

	ctx := context.Background()
	ids := make([]string, 5)
	errs := make([]error, 5)
	var wg sync.WaitGroup
	for i := range ids {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			ws, err := m.EnsureDedicated(ctx, "test/a")
			if err != nil {
				errs[i] = err
				return
			}
			ids[i] = ws.ID
		}(i)
	}
	wg.Wait()

	for i, err := range errs {
		if err != nil {
			t.Fatalf("EnsureDedicated #%d: %v", i, err)
		}
	}
	seen := map[string]bool{}
	for _, id := range ids {
		if id == "" {
			t.Fatal("a concurrent call returned no workspace")
		}
		seen[id] = true
	}
	if len(seen) != 1 {
		t.Fatalf("concurrent calls created %d workspaces: %v", len(seen), seen)
	}
	carrying := 0
	for _, w := range st.GetWorkspaces() {
		if w.Dedicated == "test/a" {
			carrying++
		}
	}
	if carrying != 1 {
		t.Errorf("%d workspaces carry the name, want 1", carrying)
	}
}

// TestDedicatedRegistration_LabelDefaultsToName pins the default label rule.
func TestDedicatedRegistration_LabelDefaultsToName(t *testing.T) {
	t.Parallel()
	repoDir := gitTestWorkTree(t)
	m, _ := dedicatedFixtureWithRepos(t, []config.Repo{testRepoWithBarePath(t, "test", repoDir)})
	m.RegisterDedicated("test/a", DedicatedRegistration{RepoURL: repoDir, Branch: "fix/a"})

	ws, err := m.EnsureDedicated(context.Background(), "test/a")
	if err != nil {
		t.Fatalf("EnsureDedicated: %v", err)
	}
	if ws.Label != "test/a" {
		t.Errorf("Label = %q, want test/a", ws.Label)
	}
}

// TestEnsureDedicated_RegisteredNamesSorted pins that the listing is sorted.
func TestEnsureDedicated_RegisteredNamesSorted(t *testing.T) {
	t.Parallel()
	repoDir := gitTestWorkTree(t)
	m, _ := dedicatedFixtureWithRepos(t, []config.Repo{testRepoWithBarePath(t, "test", repoDir)})
	for _, name := range []string{"test/c", "test/a", "test/b"} {
		m.RegisterDedicated(name, DedicatedRegistration{RepoURL: repoDir, Branch: "fix/" + name[len("test/"):]})
		if _, err := m.EnsureDedicated(context.Background(), name); err != nil {
			t.Fatalf("EnsureDedicated %s: %v", name, err)
		}
	}
	got := m.RegisteredDedicated("test/")
	want := []string{"test/a", "test/b", "test/c"}
	if !sort.StringsAreSorted(got) {
		t.Fatalf("RegisteredDedicated = %v, want sorted", got)
	}
	for i := range want {
		if i >= len(got) || got[i] != want[i] {
			t.Fatalf("RegisteredDedicated = %v, want %v", got, want)
		}
	}
}
