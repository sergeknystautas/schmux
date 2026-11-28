package workspace

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/sergeknystautas/schmux/internal/config"
	"github.com/sergeknystautas/schmux/internal/repo"
	"github.com/sergeknystautas/schmux/internal/state"
)

// newRemoveRepoManager builds a manager over a fresh temp dir with the given
// entry seeded. Entries must carry a BarePath unique to the test: the query
// directory lives under the shared test HOME.
func newRemoveRepoManager(t *testing.T, entry state.Repo) (*Manager, *config.Config, state.StateStore) {
	t.Helper()
	tmpDir := t.TempDir()
	statePath := filepath.Join(tmpDir, "state.json")
	st := state.New(statePath, nil)
	cfg := &config.Config{}
	cfg.WorkspacePath = filepath.Join(tmpDir, "workspaces")
	cfg.WorktreeBasePath = filepath.Join(tmpDir, "repos")
	m := New(cfg, st, repo.New(cfg, st, nil), statePath, testLogger())
	seedRepos(t, st, []state.Repo{entry})
	return m, cfg, st
}

// makeRepoClones creates the entry's worktree-base and query directories, each
// holding a marker file, and returns both paths.
func makeRepoClones(t *testing.T, cfg *config.Config, entry state.Repo) (string, string) {
	t.Helper()
	dirs := []string{
		filepath.Join(cfg.GetWorktreeBasePath(), entry.BarePath),
		filepath.Join(cfg.GetQueryRepoPath(), entry.BarePath),
	}
	for _, d := range dirs {
		if err := os.MkdirAll(d, 0755); err != nil {
			t.Fatalf("mkdir %s: %v", d, err)
		}
		if err := os.WriteFile(filepath.Join(d, "HEAD"), []byte("ref: refs/heads/main\n"), 0644); err != nil {
			t.Fatalf("write marker in %s: %v", d, err)
		}
		t.Cleanup(func() { os.RemoveAll(d) })
	}
	return dirs[0], dirs[1]
}

func assertDirExists(t *testing.T, dir string) {
	t.Helper()
	if _, err := os.Stat(dir); err != nil {
		t.Errorf("expected %s to exist: %v", dir, err)
	}
}

func assertDirGone(t *testing.T, dir string) {
	t.Helper()
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Errorf("expected %s to be gone, stat err = %v", dir, err)
	}
}

func assertEntryGone(t *testing.T, m *Manager, name string) {
	t.Helper()
	if _, found := m.repos.Get(name); found {
		t.Errorf("repo entry %q should be removed", name)
	}
}

func TestRemoveRepo_RejectsWhileWorkspaceUsesURL(t *testing.T) {
	t.Parallel()
	entry := testRepoWithBarePath(t, "test", "https://example.com/test.git")
	m, cfg, st := newRemoveRepoManager(t, entry)
	repoDir, queryDir := makeRepoClones(t, cfg, entry)
	if err := st.AddWorkspace(state.Workspace{ID: "test-001", Repo: entry.URL, Path: t.TempDir()}); err != nil {
		t.Fatalf("AddWorkspace: %v", err)
	}

	err := m.RemoveRepo(entry.Name)
	if !errors.Is(err, ErrInUse) {
		t.Fatalf("RemoveRepo error = %v, want ErrInUse", err)
	}
	if !strings.Contains(err.Error(), "test-001") {
		t.Errorf("error should name the workspace, got %q", err.Error())
	}
	if _, found := m.repos.Get(entry.Name); !found {
		t.Errorf("repo entry should remain")
	}
	assertDirExists(t, repoDir)
	assertDirExists(t, queryDir)
}

func TestRemoveRepo_IgnoresOtherReposWorkspaces(t *testing.T) {
	t.Parallel()
	entry := testRepoWithBarePath(t, "test", "https://example.com/test.git")
	m, cfg, st := newRemoveRepoManager(t, entry)
	repoDir, queryDir := makeRepoClones(t, cfg, entry)
	if err := st.AddWorkspace(state.Workspace{ID: "other-001", Repo: "https://example.com/other.git", Path: t.TempDir()}); err != nil {
		t.Fatalf("AddWorkspace: %v", err)
	}

	if err := m.RemoveRepo(entry.Name); err != nil {
		t.Fatalf("RemoveRepo: %v", err)
	}
	assertEntryGone(t, m, entry.Name)
	assertDirGone(t, repoDir)
	assertDirGone(t, queryDir)
}

func TestRemoveRepo_DeletesBothClones(t *testing.T) {
	t.Parallel()
	entry := testRepoWithBarePath(t, "test", "https://example.com/test.git")
	m, cfg, _ := newRemoveRepoManager(t, entry)
	repoDir, queryDir := makeRepoClones(t, cfg, entry)

	if err := m.RemoveRepo(entry.Name); err != nil {
		t.Fatalf("RemoveRepo: %v", err)
	}
	assertEntryGone(t, m, entry.Name)
	assertDirGone(t, repoDir)
	assertDirGone(t, queryDir)
}

func TestRemoveRepo_MissingClonesStillRemovesEntry(t *testing.T) {
	t.Parallel()
	entry := testRepoWithBarePath(t, "test", "https://example.com/test.git")
	m, _, _ := newRemoveRepoManager(t, entry)

	if err := m.RemoveRepo(entry.Name); err != nil {
		t.Fatalf("RemoveRepo: %v", err)
	}
	assertEntryGone(t, m, entry.Name)
}

func TestRemoveRepo_UnknownNameIsNotFound(t *testing.T) {
	t.Parallel()
	entry := testRepoWithBarePath(t, "test", "https://example.com/test.git")
	m, _, _ := newRemoveRepoManager(t, entry)

	err := m.RemoveRepo("nope")
	if !errors.Is(err, repo.ErrNotFound) {
		t.Fatalf("RemoveRepo error = %v, want repo.ErrNotFound", err)
	}
}

func TestRemoveRepo_SkipsSaplingAndLocal(t *testing.T) {
	t.Parallel()
	safe := strings.ReplaceAll(t.Name(), "/", "_")
	tests := []struct {
		name  string
		entry state.Repo
	}{
		{"sapling", state.Repo{Name: "sl", URL: "sl-id", VCS: "sapling", BarePath: safe + "-sl"}},
		{"local", state.Repo{Name: "scratch", URL: "local:scratch", BarePath: safe + "-scratch.git"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			entry := tc.entry
			entry.BarePath = strings.ReplaceAll(t.Name(), "/", "_") + "-" + entry.Name
			m, cfg, _ := newRemoveRepoManager(t, entry)
			repoDir, queryDir := makeRepoClones(t, cfg, entry)

			if err := m.RemoveRepo(entry.Name); err != nil {
				t.Fatalf("RemoveRepo: %v", err)
			}
			assertEntryGone(t, m, entry.Name)
			assertDirExists(t, repoDir)
			assertDirExists(t, queryDir)
		})
	}
}

func TestRemoveRepo_WaitsForSpawnLock(t *testing.T) {
	t.Parallel()
	entry := testRepoWithBarePath(t, "test", "https://example.com/test.git")
	m, cfg, st := newRemoveRepoManager(t, entry)
	repoDir, queryDir := makeRepoClones(t, cfg, entry)

	lock := m.repoLock(entry.URL)
	lock.Lock()
	unlocked := false
	defer func() {
		if !unlocked {
			lock.Unlock()
		}
	}()

	done := make(chan error, 1)
	go func() { done <- m.RemoveRepo(entry.Name) }()

	// Negative window (testing.md exception): proves RemoveRepo blocks on repoLock; 200ms is far above a tempdir removal.
	windowStart := time.Now()
	select {
	case err := <-done:
		t.Fatalf("RemoveRepo returned %v after %s while a spawn held repoLock (window 200ms)", err, time.Since(windowStart))
	case <-time.After(200 * time.Millisecond):
	}

	// The spawn's workspace lands in state while it still holds the lock.
	if err := st.AddWorkspace(state.Workspace{ID: "test-001", Repo: entry.URL, Path: t.TempDir()}); err != nil {
		t.Fatalf("AddWorkspace: %v", err)
	}
	lock.Unlock()
	unlocked = true

	// Failure backstop only: with the lock free, RemoveRepo is one state read
	// and two tempdir checks, so 10s means it is stuck, not slow.
	var err error
	select {
	case err = <-done:
	case <-time.After(10 * time.Second):
		_, entryPresent := m.repos.Get(entry.Name)
		t.Fatalf("RemoveRepo did not return within 10s of the lock release; entry present=%v, workspaces=%d", entryPresent, len(st.GetWorkspaces()))
	}
	if !errors.Is(err, ErrInUse) {
		t.Fatalf("RemoveRepo error = %v, want ErrInUse", err)
	}
	assertDirExists(t, repoDir)
	assertDirExists(t, queryDir)
}

func TestRemoveRepo_LaterSpawnFailsWithoutDirectory(t *testing.T) {
	t.Parallel()
	entry := testRepoWithBarePath(t, "test", "https://example.com/test.git")
	m, cfg, _ := newRemoveRepoManager(t, entry)
	makeRepoClones(t, cfg, entry)

	if err := m.RemoveRepo(entry.Name); err != nil {
		t.Fatalf("RemoveRepo: %v", err)
	}
	if _, err := m.GetOrCreate(context.Background(), entry.URL, "main"); err == nil {
		t.Fatal("GetOrCreate should fail for a removed repo")
	}

	dirEntries, err := os.ReadDir(cfg.WorkspacePath)
	if err != nil && !os.IsNotExist(err) {
		t.Fatalf("ReadDir: %v", err)
	}
	for _, de := range dirEntries {
		if strings.HasPrefix(de.Name(), entry.Name) {
			t.Errorf("workspace directory %q should not exist", de.Name())
		}
	}
}
