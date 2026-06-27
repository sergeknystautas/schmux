package workspace

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sergeknystautas/schmux/internal/config"
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
	cfg.Repos = []config.Repo{
		{Name: "bach", URL: remoteB, BarePath: "bach.git"},
	}
	st := state.New(statePath, nil)
	m := New(cfg, st, statePath, testLogger())

	_, err := m.gitBackend.EnsureRepoBase(context.Background(), remoteB, "")
	if err == nil {
		t.Fatal("EnsureRepoBase expected error (base origin differs from requested URL), got nil")
	}
	if !strings.Contains(err.Error(), "duplicate repo URLs") {
		t.Errorf("error should explain the duplicate repo URL conflict, got: %v", err)
	}

	// Must not register remote B against the stale A base — that is the
	// corruption that poisoned subsequent spawns.
	if rb, found := st.GetRepoBaseByURL(remoteB); found {
		t.Errorf("EnsureRepoBase must not register a base for B at the stale A path, got %s", rb.Path)
	}
}

// TestEnsureRepoBase_ErrorsWhenStateBaseOriginDiffers covers the corrupted-
// state flavor: state already records a base for B that actually points at
// remote A (the bogus entry created by an earlier unguarded spawn). The
// state-hit reuse path must verify origin, not trust the recorded path.
func TestEnsureRepoBase_ErrorsWhenStateBaseOriginDiffers(t *testing.T) {
	t.Parallel()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}

	tmpDir := t.TempDir()
	reposDir := filepath.Join(tmpDir, "repos")
	statePath := filepath.Join(tmpDir, "state.json")

	remoteA := filepath.Join(tmpDir, "remote-a")
	remoteB := filepath.Join(tmpDir, "remote-b")

	basePath := filepath.Join(reposDir, "bach.git")
	if err := os.MkdirAll(reposDir, 0o755); err != nil {
		t.Fatalf("mkdir repos: %v", err)
	}
	runGit(t, "", "init", "--bare", basePath)
	runGit(t, basePath, "remote", "add", "origin", remoteA)

	cfg := &config.Config{}
	cfg.WorkspacePath = tmpDir
	cfg.WorktreeBasePath = reposDir
	cfg.Repos = []config.Repo{
		{Name: "bach", URL: remoteB, BarePath: "bach.git"},
	}
	st := state.New(statePath, nil)
	// Pre-corrupt state: B recorded against A's base (exactly what the bug wrote).
	if err := st.AddRepoBase(state.RepoBase{RepoURL: remoteB, Path: basePath}); err != nil {
		t.Fatalf("AddRepoBase: %v", err)
	}
	m := New(cfg, st, statePath, testLogger())

	_, err := m.gitBackend.EnsureRepoBase(context.Background(), remoteB, "")
	if err == nil {
		t.Fatal("EnsureRepoBase expected error (state base origin differs from requested URL), got nil")
	}
	if !strings.Contains(err.Error(), "duplicate repo URLs") {
		t.Errorf("error should explain the duplicate repo URL conflict, got: %v", err)
	}
}
