package workspace

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sergeknystautas/schmux/internal/api/contracts"
	"github.com/sergeknystautas/schmux/internal/config"
	"github.com/sergeknystautas/schmux/internal/state"
)

// TestCleanup_SkipsNonGitWorkspaces verifies that Cleanup returns nil
// for a sapling workspace without attempting git operations.
func TestCleanup_SkipsNonGitWorkspaces(t *testing.T) {
	t.Parallel()
	tmpDir := t.TempDir()
	statePath := filepath.Join(tmpDir, "state.json")
	cfg := &config.Config{
		WorkspacePath:    filepath.Join(tmpDir, "workspaces"),
		WorktreeBasePath: filepath.Join(tmpDir, "repos"),
		Repos: []config.Repo{
			{Name: "sl-repo", URL: "sl-repo-id", VCS: "sapling"},
		},
	}
	st := state.New(statePath, nil)
	m := New(cfg, st, statePath, testLogger())

	// Add a sapling workspace to state. The path does not need to be a
	// real sapling repo -- the guard should return before touching it.
	wsPath := filepath.Join(tmpDir, "sl-repo-001")
	if err := os.MkdirAll(wsPath, 0755); err != nil {
		t.Fatalf("failed to create workspace dir: %v", err)
	}
	st.AddWorkspace(state.Workspace{
		ID:     "sl-repo-001",
		Repo:   "sl-repo-id",
		Branch: "main",
		Path:   wsPath,
		VCS:    "sapling",
	})

	err := m.Cleanup(context.Background(), "sl-repo-001")
	if err != nil {
		t.Errorf("Cleanup() returned error for sapling workspace: %v", err)
	}
}

// TestScan_SkipsNonGitWorkspaces verifies that Scan does not remove
// sapling workspaces from state even though they lack a .git directory.
func TestScan_SkipsNonGitWorkspaces(t *testing.T) {
	t.Parallel()
	tmpDir := t.TempDir()
	statePath := filepath.Join(tmpDir, "state.json")
	cfg := &config.Config{
		WorkspacePath:    tmpDir,
		WorktreeBasePath: filepath.Join(tmpDir, "repos"),
		Repos: []config.Repo{
			{Name: "sl-repo", URL: "sl-repo-id", VCS: "sapling"},
		},
	}
	st := state.New(statePath, nil)

	// Create a workspace directory without .git (sapling uses .sl instead).
	wsPath := filepath.Join(tmpDir, "sl-repo-001")
	if err := os.MkdirAll(wsPath, 0755); err != nil {
		t.Fatalf("failed to create workspace dir: %v", err)
	}

	st.AddWorkspace(state.Workspace{
		ID:     "sl-repo-001",
		Repo:   "sl-repo-id",
		Branch: "main",
		Path:   wsPath,
		VCS:    "sapling",
	})

	m := New(cfg, st, statePath, testLogger())

	result, err := m.Scan()
	if err != nil {
		t.Fatalf("Scan() error = %v", err)
	}

	// The sapling workspace must NOT appear in Removed.
	for _, ws := range result.Removed {
		if ws.ID == "sl-repo-001" {
			t.Error("Scan() should not remove sapling workspaces that lack .git")
		}
	}

	// The workspace must still be in state.
	if _, found := st.GetWorkspace("sl-repo-001"); !found {
		t.Error("sapling workspace should remain in state after Scan()")
	}
}

// TestCheckoutPR_RejectsNonGitRepos verifies that CheckoutPR returns an
// error containing "not supported" when called for a sapling repo.
func TestCheckoutPR_RejectsNonGitRepos(t *testing.T) {
	t.Parallel()
	tmpDir := t.TempDir()
	statePath := filepath.Join(tmpDir, "state.json")
	cfg := &config.Config{
		WorkspacePath:    filepath.Join(tmpDir, "workspaces"),
		WorktreeBasePath: filepath.Join(tmpDir, "repos"),
		Repos: []config.Repo{
			{Name: "sl-repo", URL: "sl-repo-id", VCS: "sapling"},
		},
	}
	st := state.New(statePath, nil)
	m := New(cfg, st, statePath, testLogger())

	pr := contracts.PullRequest{
		Number:       42,
		RepoURL:      "sl-repo-id",
		SourceBranch: "feature-branch",
	}

	_, err := m.CheckoutPR(context.Background(), pr)
	if err == nil {
		t.Fatal("CheckoutPR() should return error for sapling repo")
	}
	if !strings.Contains(err.Error(), "not supported") {
		t.Errorf("CheckoutPR() error = %q, want error containing 'not supported'", err.Error())
	}
}

// TestUpdateVCSStatus_RoutesSaplingThroughBackend verifies that
// UpdateVCSStatus for a sapling workspace routes through the
// SaplingBackend rather than running git commands. Since `sl` is
// unlikely to be installed in the test environment, the backend
// will fail to get status, but the code path handles the error
// gracefully and returns the workspace with stale data.
func TestUpdateVCSStatus_RoutesSaplingThroughBackend(t *testing.T) {
	t.Parallel()
	tmpDir := t.TempDir()
	statePath := filepath.Join(tmpDir, "state.json")
	cfg := &config.Config{
		WorkspacePath:    filepath.Join(tmpDir, "workspaces"),
		WorktreeBasePath: filepath.Join(tmpDir, "repos"),
		Repos: []config.Repo{
			{Name: "sl-repo", URL: "sl-repo-id", VCS: "sapling"},
		},
	}
	st := state.New(statePath, nil)
	m := New(cfg, st, statePath, testLogger())

	// Create a real directory for the workspace (needed to avoid stat errors).
	wsPath := filepath.Join(tmpDir, "sl-repo-001")
	if err := os.MkdirAll(wsPath, 0755); err != nil {
		t.Fatalf("failed to create workspace dir: %v", err)
	}

	st.AddWorkspace(state.Workspace{
		ID:     "sl-repo-001",
		Repo:   "sl-repo-id",
		Branch: "main",
		Path:   wsPath,
		VCS:    "sapling",
	})

	ws, err := m.UpdateVCSStatus(context.Background(), "sl-repo-001")
	if err != nil {
		t.Fatalf("UpdateVCSStatus() returned unexpected error: %v", err)
	}
	if ws == nil {
		t.Fatal("UpdateVCSStatus() returned nil workspace")
	}
	// The workspace should still be identified as sapling.
	if ws.VCS != "sapling" {
		t.Errorf("workspace VCS = %q, want 'sapling'", ws.VCS)
	}
}
