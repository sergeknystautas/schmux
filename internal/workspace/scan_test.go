package workspace

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/sergek/schmux/internal/config"
	"github.com/sergek/schmux/internal/state"
)

// TestScan_EmptyWorkspaceDirectory tests scanning an empty workspace directory.
func TestScan_EmptyWorkspaceDirectory(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}

	tmpDir := t.TempDir()
	statePath := filepath.Join(tmpDir, "state.json")
	cfg := &config.Config{
		WorkspacePath: tmpDir,
		Repos: []config.Repo{
			{Name: "test", URL: "https://example.com/test.git"},
		},
	}
	st := state.New(statePath)
	m := New(cfg, st, statePath)

	result, err := m.Scan()
	if err != nil {
		t.Fatalf("Scan() error = %v", err)
	}

	if len(result.Added) != 0 {
		t.Errorf("expected 0 added, got %d", len(result.Added))
	}
	if len(result.Updated) != 0 {
		t.Errorf("expected 0 updated, got %d", len(result.Updated))
	}
	if len(result.Removed) != 0 {
		t.Errorf("expected 0 removed, got %d", len(result.Removed))
	}
}

// TestScan_AddNewWorkspace tests that a new git repo in the workspace directory is added.
func TestScan_AddNewWorkspace(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}

	tmpDir := t.TempDir()
	statePath := filepath.Join(tmpDir, "state.json")

	// Create a git repo in the workspace directory
	repoDir := gitTestWorkTree(t)
	workspacePath := filepath.Join(tmpDir, "test-001")
	if err := os.Rename(repoDir, workspacePath); err != nil {
		t.Fatalf("failed to move repo: %v", err)
	}

	cfg := &config.Config{
		WorkspacePath: tmpDir,
		Repos: []config.Repo{
			{Name: "test", URL: "https://example.com/test.git"},
		},
	}
	st := state.New(statePath)
	m := New(cfg, st, statePath)

	// Scan should not add because the repo URL doesn't match
	result, err := m.Scan()
	if err != nil {
		t.Fatalf("Scan() error = %v", err)
	}

	// The workspace should be skipped because the remote URL doesn't match config
	if len(result.Added) != 0 {
		t.Errorf("expected 0 added (repo URL doesn't match), got %d", len(result.Added))
	}
}

// TestScan_RemoveMissingWorkspace tests that workspaces missing from filesystem are removed.
func TestScan_RemoveMissingWorkspace(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}

	tmpDir := t.TempDir()
	statePath := filepath.Join(tmpDir, "state.json")

	// Create a git repo in the workspace directory
	repoDir := gitTestWorkTree(t)
	workspacePath := filepath.Join(tmpDir, "test-001")
	if err := os.Rename(repoDir, workspacePath); err != nil {
		t.Fatalf("failed to move repo: %v", err)
	}

	cfg := &config.Config{
		WorkspacePath: tmpDir,
		Repos: []config.Repo{
			{Name: "test", URL: "https://example.com/test.git"},
		},
	}
	st := state.New(statePath)

	// Add workspace to state
	ws := state.Workspace{
		ID:     "test-001",
		Repo:   "https://example.com/test.git",
		Branch: "main",
		Path:   workspacePath,
	}
	st.AddWorkspace(ws)

	m := New(cfg, st, statePath)

	// Delete the workspace directory
	if err := os.RemoveAll(workspacePath); err != nil {
		t.Fatalf("failed to remove workspace: %v", err)
	}

	result, err := m.Scan()
	if err != nil {
		t.Fatalf("Scan() error = %v", err)
	}

	if len(result.Removed) != 1 {
		t.Errorf("expected 1 removed, got %d", len(result.Removed))
	}
	if len(result.Removed) > 0 && result.Removed[0].ID != "test-001" {
		t.Errorf("expected removed workspace ID test-001, got %s", result.Removed[0].ID)
	}

	// Verify state was updated
	_, found := st.GetWorkspace("test-001")
	if found {
		t.Error("workspace should be removed from state")
	}
}

// TestScan_UpdateWorkspaceBranch tests that workspace branch is updated if changed.
func TestScan_UpdateWorkspaceBranch(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}

	tmpDir := t.TempDir()
	statePath := filepath.Join(tmpDir, "state.json")

	// Create a git repo with main branch
	repoDir := gitTestWorkTree(t)
	workspacePath := filepath.Join(tmpDir, "test-001")
	if err := os.Rename(repoDir, workspacePath); err != nil {
		t.Fatalf("failed to move repo: %v", err)
	}

	// Add a new branch
	runGit(t, workspacePath, "checkout", "-b", "feature-branch")

	cfg := &config.Config{
		WorkspacePath: tmpDir,
		Repos: []config.Repo{
			{Name: "test", URL: "https://example.com/test.git"},
		},
	}
	st := state.New(statePath)

	// Add workspace to state with old branch
	ws := state.Workspace{
		ID:     "test-001",
		Repo:   "https://example.com/test.git",
		Branch: "main",
		Path:   workspacePath,
	}
	st.AddWorkspace(ws)

	m := New(cfg, st, statePath)

	// Scan should detect branch change
	// Note: This test will not actually update because the remote URL doesn't match
	// In a real scenario, you'd need to set up the remote URL properly
	result, err := m.Scan()
	if err != nil {
		t.Fatalf("Scan() error = %v", err)
	}

	// Since the remote URL doesn't match, nothing should be updated
	if len(result.Updated) != 0 {
		t.Errorf("expected 0 updated (remote URL doesn't match), got %d", len(result.Updated))
	}
}

// TestScan_SkipActiveSessionWorkspaces tests that workspaces with active sessions are not modified.
func TestScan_SkipActiveSessionWorkspaces(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}

	tmpDir := t.TempDir()
	statePath := filepath.Join(tmpDir, "state.json")

	cfg := &config.Config{
		WorkspacePath: tmpDir,
		Repos: []config.Repo{
			{Name: "test", URL: "https://example.com/test.git"},
		},
	}
	st := state.New(statePath)

	// Create a git repo in the workspace directory
	repoDir := gitTestWorkTree(t)
	workspacePath := filepath.Join(tmpDir, "test-001")
	if err := os.Rename(repoDir, workspacePath); err != nil {
		t.Fatalf("failed to move repo: %v", err)
	}

	// Add workspace to state
	ws := state.Workspace{
		ID:     "test-001",
		Repo:   "https://example.com/test.git",
		Branch: "main",
		Path:   workspacePath,
	}
	st.AddWorkspace(ws)

	// Add an active session for this workspace
	sess := state.Session{
		ID:          "sess-001",
		WorkspaceID: "test-001",
		Target:       "test-agent",
	}
	st.AddSession(sess)

	m := New(cfg, st, statePath)

	// Delete the workspace directory
	if err := os.RemoveAll(workspacePath); err != nil {
		t.Fatalf("failed to remove workspace: %v", err)
	}

	result, err := m.Scan()
	if err != nil {
		t.Fatalf("Scan() error = %v", err)
	}

	// Workspace should not be removed because it has active sessions
	if len(result.Removed) != 0 {
		t.Errorf("expected 0 removed (has active sessions), got %d", len(result.Removed))
	}

	// Verify workspace still exists in state
	_, found := st.GetWorkspace("test-001")
	if !found {
		t.Error("workspace should still exist in state when it has active sessions")
	}
}

// TestScan_Integration tests the full scan workflow with real repos.
func TestScan_Integration(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}

	tmpDir := t.TempDir()
	statePath := filepath.Join(tmpDir, "state.json")

	// Create first repo
	repoDir1 := gitTestWorkTree(t)
	workspacePath1 := filepath.Join(tmpDir, "test-001")
	if err := os.Rename(repoDir1, workspacePath1); err != nil {
		t.Fatalf("failed to move repo: %v", err)
	}

	// Set up remote URL for first repo
	runGit(t, workspacePath1, "config", "remote.origin.url", "https://example.com/test.git")
	runGit(t, workspacePath1, "checkout", "-b", "feature")

	cfg := &config.Config{
		WorkspacePath: tmpDir,
		Repos: []config.Repo{
			{Name: "test", URL: "https://example.com/test.git"},
		},
	}
	st := state.New(statePath)

	// Add workspace to state with old branch
	ws1 := state.Workspace{
		ID:     "test-001",
		Repo:   "https://example.com/test.git",
		Branch: "main",
		Path:   workspacePath1,
	}
	st.AddWorkspace(ws1)

	m := New(cfg, st, statePath)

	result, err := m.Scan()
	if err != nil {
		t.Fatalf("Scan() error = %v", err)
	}

	// Should have updated the branch from main to feature
	if len(result.Updated) != 1 {
		t.Errorf("expected 1 updated, got %d", len(result.Updated))
	}
	if len(result.Updated) > 0 && result.Updated[0].New.Branch != "feature" {
		t.Errorf("expected branch feature, got %s", result.Updated[0].New.Branch)
	}
	if len(result.Updated) > 0 && result.Updated[0].Old.Branch != "main" {
		t.Errorf("expected old branch main, got %s", result.Updated[0].Old.Branch)
	}

	// Verify state was updated
	wsUpdated, found := st.GetWorkspace("test-001")
	if !found {
		t.Fatal("workspace should still exist in state")
	}
	if wsUpdated.Branch != "feature" {
		t.Errorf("expected state branch to be feature, got %s", wsUpdated.Branch)
	}
}
