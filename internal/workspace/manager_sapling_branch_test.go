package workspace

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/sergeknystautas/schmux/internal/config"
	"github.com/sergeknystautas/schmux/internal/state"
)

// newTestManagerWithSaplingRepo creates a Manager with a single sapling repo
// configured under the given name. The sapling backend is wired with template
// commands that create/remove a workspace directory using sh -c (sufficient
// to exercise the create() flow without depending on the real `sl` CLI).
//
// The repo URL is "sl:<repoName>"; tests can use that string to call
// GetOrCreate / GetOrCreateWithLabel.
func newTestManagerWithSaplingRepo(t *testing.T, repoName string) (*Manager, func()) {
	t.Helper()
	tmpDir := t.TempDir()
	statePath := filepath.Join(tmpDir, "state.json")
	reposDir := filepath.Join(tmpDir, "repos")
	wsDir := filepath.Join(tmpDir, "workspaces")
	repoURL := "sl:" + repoName

	cfg := &config.Config{}
	cfg.WorkspacePath = wsDir
	cfg.WorktreeBasePath = reposDir
	cfg.Repos = []config.Repo{
		{Name: repoName, URL: repoURL, VCS: "sapling", BarePath: repoName},
	}
	cfg.SaplingCommands = config.SaplingCommands{
		CreateRepoBase: config.ShellCommand{"mkdir", "-p", "{{.BasePath}}"},
		// CreateWorkspace creates the destination directory. The {{.Branch}}
		// template variable receives "main" via the backend boundary
		// substitution — which is what we exercise in TestCreate_*.
		CreateWorkspace: config.ShellCommand{
			"sh", "-c",
			`mkdir -p "$1" && echo "$2" > "$1/.sl-branch"`,
			"_", "{{.DestPath}}", "{{.Branch}}",
		},
		RemoveWorkspace: config.ShellCommand{"rm", "-rf", "{{.WorkspacePath}}"},
	}
	st := state.New(statePath, nil)
	m := New(cfg, st, statePath, testLogger())

	cleanup := func() {
		// t.TempDir() handles directory cleanup; nothing else to release.
	}
	return m, cleanup
}

// TestGetOrCreate_SaplingAcceptsEmptyBranch verifies that GetOrCreate skips
// ValidateBranchName when the resolved repo is sapling and the branch is empty.
// The persisted Workspace.Branch must remain empty — the entire display
// fallback chain depends on this.
func TestGetOrCreate_SaplingAcceptsEmptyBranch(t *testing.T) {
	t.Parallel()
	m, cleanup := newTestManagerWithSaplingRepo(t, "saplingrepo")
	defer cleanup()

	w, err := m.GetOrCreate(context.Background(), "sl:saplingrepo", "")
	if err != nil {
		t.Fatalf("expected empty-branch sapling spawn to succeed, got: %v", err)
	}
	if w.Branch != "" {
		t.Fatalf("expected Workspace.Branch to remain empty for sapling, got %q", w.Branch)
	}
	if w.VCS != "sapling" {
		t.Fatalf("expected Workspace.VCS=sapling, got %q", w.VCS)
	}
}

// TestCreate_SaplingPersistsEmptyBranch is a focused assertion that the
// state-persisted Branch field stays empty even though the sapling backend
// receives "main" via the template substitution at the backend boundary.
func TestCreate_SaplingPersistsEmptyBranch(t *testing.T) {
	t.Parallel()
	m, cleanup := newTestManagerWithSaplingRepo(t, "saplingrepo")
	defer cleanup()

	w, err := m.GetOrCreate(context.Background(), "sl:saplingrepo", "")
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	// Re-read from state to confirm persistence (not just the in-memory return value).
	stored, found := m.state.GetWorkspace(w.ID)
	if !found {
		t.Fatalf("workspace %q not found in state", w.ID)
	}
	if stored.Branch != "" {
		t.Fatalf("expected persisted Branch=\"\", got %q", stored.Branch)
	}
}

// TestCreate_SaplingPersistsLabel verifies that GetOrCreateWithLabel stores
// the supplied label on the resulting workspace.
func TestCreate_SaplingPersistsLabel(t *testing.T) {
	t.Parallel()
	m, cleanup := newTestManagerWithSaplingRepo(t, "saplingrepo")
	defer cleanup()

	w, err := m.GetOrCreateWithLabel(context.Background(), "sl:saplingrepo", "", "Login bug fix")
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if w.Label != "Login bug fix" {
		t.Fatalf("expected Label=%q, got %q", "Login bug fix", w.Label)
	}
	stored, found := m.state.GetWorkspace(w.ID)
	if !found {
		t.Fatalf("workspace %q not found in state", w.ID)
	}
	if stored.Label != "Login bug fix" {
		t.Fatalf("expected persisted Label=%q, got %q", "Login bug fix", stored.Label)
	}
}
