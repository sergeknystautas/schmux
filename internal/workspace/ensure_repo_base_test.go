package workspace

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sergeknystautas/schmux/internal/config"
	"github.com/sergeknystautas/schmux/internal/repo"
	"github.com/sergeknystautas/schmux/internal/state"
)

// TestEnsureRepoBase_ErrorsWhenBaseOriginDiffers reproduces the stale-base
// bug: a bare base cloned earlier for remote A sits on disk at the
// name-derived path (<repos>/<name>.git) with origin = A. A later config
// entry reuses the same name but points at remote B. EnsureRepoBase must
// NOT hand back A's base for B — it must error so the spawn aborts instead
// of producing a workspace on the wrong remote.
func TestEnsureRepoBase_ErrorsWhenBaseOriginDiffers(t *testing.T) {
	t.Parallel()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}

	tmpDir := t.TempDir()
	reposDir := filepath.Join(tmpDir, "repos")
	statePath := filepath.Join(tmpDir, "state.json")

	// Two distinct local "remotes" standing in for sergeknystautas/lordbaltogames.
	remoteA := filepath.Join(tmpDir, "remote-a")
	remoteB := filepath.Join(tmpDir, "remote-b")

	// Leftover state: a base for remote A already on disk at the name-derived
	// path, with origin = remote A.
	basePath := filepath.Join(reposDir, "bach.git")
	if err := os.MkdirAll(reposDir, 0o755); err != nil {
		t.Fatalf("mkdir repos: %v", err)
	}
	runGit(t, "", "init", "--bare", basePath)
	runGit(t, basePath, "remote", "add", "origin", remoteA)

	// Config now points the same name at remote B.
	cfg := &config.Config{}
	cfg.WorkspacePath = tmpDir
	cfg.WorktreeBasePath = reposDir
	st := state.New(statePath, nil)
	seedRepos(t, st, []state.Repo{
		{Name: "bach", URL: remoteB, BarePath: "bach.git"},
	})
	m := New(cfg, st, repo.New(cfg, st, nil), statePath, testLogger())

	_, err := m.gitBackend.EnsureRepoBase(context.Background(), remoteB, "")
	if err == nil {
		t.Fatal("EnsureRepoBase expected error (base origin differs from requested URL), got nil")
	}
	if !strings.Contains(err.Error(), "duplicate repo URLs") {
		t.Errorf("error should explain the duplicate repo URL conflict, got: %v", err)
	}
}

// TestEnsureRepoBase_ReusesExistingBaseWithoutClone proves a base already on
// disk at the entry's bare_path, with a matching origin, is reused without
// running git clone again.
func TestEnsureRepoBase_ReusesExistingBaseWithoutClone(t *testing.T) {
	t.Parallel()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}

	tmpDir := t.TempDir()
	reposDir := filepath.Join(tmpDir, "repos")
	statePath := filepath.Join(tmpDir, "state.json")
	remote := gitTestWorkTree(t)

	cfg := &config.Config{}
	cfg.WorkspacePath = tmpDir
	cfg.WorktreeBasePath = reposDir
	st := state.New(statePath, nil)
	seedRepos(t, st, []state.Repo{
		{Name: "test", URL: remote, BarePath: "test.git"},
	})
	m := New(cfg, st, repo.New(cfg, st, nil), statePath, testLogger())
	tel := NewIOWorkspaceTelemetry()
	m.SetIOWorkspaceTelemetry(tel)
	ctx := context.Background()

	first, err := m.gitBackend.EnsureRepoBase(ctx, remote, "")
	if err != nil {
		t.Fatalf("first EnsureRepoBase: %v", err)
	}
	second, err := m.gitBackend.EnsureRepoBase(ctx, remote, "")
	if err != nil {
		t.Fatalf("second EnsureRepoBase: %v", err)
	}
	if second != first {
		t.Errorf("second base = %q, want %q", second, first)
	}
	if clones := tel.Snapshot(false).Counters["git_clone"]; clones != 1 {
		t.Errorf("git clone ran %d times across two calls, want 1", clones)
	}
}
