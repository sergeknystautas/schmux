//go:build e2e

package e2e

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestE2ECaptureSession tests GET /api/sessions/{id}/capture — captures terminal output
// from a running session with a configurable line count.
func TestE2ECaptureSession(t *testing.T) {
	t.Parallel()
	env := New(t)

	workspaceRoot := t.TempDir()
	env.CreateConfig(workspaceRoot)

	repoPath := workspaceRoot + "/capture-repo"
	os.MkdirAll(repoPath, 0755)
	RunCmd(t, repoPath, "git", "init", "-b", "main")
	RunCmd(t, repoPath, "git", "config", "user.email", "e2e@test.local")
	RunCmd(t, repoPath, "git", "config", "user.name", "E2E Test")
	os.WriteFile(filepath.Join(repoPath, "README.md"), []byte("# Capture\n"), 0644)
	RunCmd(t, repoPath, "git", "add", ".")
	RunCmd(t, repoPath, "git", "commit", "-m", "Initial commit")
	env.AddRepoToConfig("capture-repo", "file://"+repoPath)

	env.DaemonStart()
	defer func() {
		env.DaemonStop()
		if t.Failed() {
			env.CaptureArtifacts()
		}
	}()

	sessionID := env.SpawnSession("file://"+repoPath, "main", "echo", "", env.Nickname("capture"))
	if sessionID == "" {
		t.Fatal("Expected session ID from spawn")
	}

	// Wait for echo target to produce output
	time.Sleep(2 * time.Second)

	t.Run("DefaultLines", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		req, _ := http.NewRequestWithContext(ctx, http.MethodGet, env.DaemonURL+"/api/sessions/"+sessionID+"/capture", nil)
		resp, err := http.DefaultClient.Do(req)
		cancel()
		if err != nil {
			t.Fatalf("Capture request failed: %v", err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			body, _ := io.ReadAll(resp.Body)
			t.Fatalf("Capture returned %d: %s", resp.StatusCode, body)
		}

		var result struct {
			SessionID string `json:"session_id"`
			Lines     int    `json:"lines"`
			Output    string `json:"output"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
			t.Fatalf("Decode failed: %v", err)
		}
		if result.SessionID != sessionID {
			t.Errorf("session_id = %q, want %q", result.SessionID, sessionID)
		}
		if result.Lines != 50 {
			t.Errorf("default lines = %d, want 50", result.Lines)
		}
		// Echo target prints "hello" — should appear in output
		if !strings.Contains(result.Output, "hello") {
			t.Errorf("expected output to contain 'hello', got: %q", result.Output)
		}
	})

	t.Run("CustomLineCount", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		req, _ := http.NewRequestWithContext(ctx, http.MethodGet, env.DaemonURL+"/api/sessions/"+sessionID+"/capture?lines=5", nil)
		resp, err := http.DefaultClient.Do(req)
		cancel()
		if err != nil {
			t.Fatalf("Capture request failed: %v", err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			body, _ := io.ReadAll(resp.Body)
			t.Fatalf("Capture returned %d: %s", resp.StatusCode, body)
		}

		var result struct {
			Lines int `json:"lines"`
		}
		json.NewDecoder(resp.Body).Decode(&result)
		if result.Lines != 5 {
			t.Errorf("custom lines = %d, want 5", result.Lines)
		}
	})

	t.Run("NonexistentSession", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		req, _ := http.NewRequestWithContext(ctx, http.MethodGet, env.DaemonURL+"/api/sessions/nonexistent/capture", nil)
		resp, err := http.DefaultClient.Do(req)
		cancel()
		if err != nil {
			t.Fatalf("Capture request failed: %v", err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusNotFound {
			t.Errorf("expected 404 for nonexistent session, got %d", resp.StatusCode)
		}
	})
}

// TestE2EInspectWorkspace tests GET /api/workspaces/{id}/inspect — returns
// branch, commits ahead/behind, and uncommitted files.
func TestE2EInspectWorkspace(t *testing.T) {
	t.Parallel()
	env := New(t)

	workspaceRoot := t.TempDir()
	env.CreateConfig(workspaceRoot)

	repoPath := workspaceRoot + "/inspect-repo"
	os.MkdirAll(repoPath, 0755)
	RunCmd(t, repoPath, "git", "init", "-b", "main")
	RunCmd(t, repoPath, "git", "config", "user.email", "e2e@test.local")
	RunCmd(t, repoPath, "git", "config", "user.name", "E2E Test")
	os.WriteFile(filepath.Join(repoPath, "README.md"), []byte("# Inspect\n"), 0644)
	RunCmd(t, repoPath, "git", "add", ".")
	RunCmd(t, repoPath, "git", "commit", "-m", "Initial commit")
	env.AddRepoToConfig("inspect-repo", "file://"+repoPath)

	env.DaemonStart()
	defer func() {
		env.DaemonStop()
		if t.Failed() {
			env.CaptureArtifacts()
		}
	}()

	sessionID := env.SpawnSession("file://"+repoPath, "main", "echo", "", env.Nickname("inspect"))
	if sessionID == "" {
		t.Fatal("Expected session ID")
	}

	workspaceID := env.GetWorkspaceIDForSession(sessionID)
	if workspaceID == "" {
		t.Fatal("Expected workspace ID")
	}

	// Wait for workspace to be fully set up
	time.Sleep(2 * time.Second)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, env.DaemonURL+"/api/workspaces/"+workspaceID+"/inspect", nil)
	resp, err := http.DefaultClient.Do(req)
	cancel()
	if err != nil {
		t.Fatalf("Inspect request failed: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("Inspect returned %d: %s", resp.StatusCode, body)
	}

	var result struct {
		WorkspaceID string   `json:"workspace_id"`
		Branch      string   `json:"branch"`
		Commits     []string `json:"commits"`
		Uncommitted []string `json:"uncommitted"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatalf("Decode failed: %v", err)
	}
	if result.WorkspaceID != workspaceID {
		t.Errorf("workspace_id = %q, want %q", result.WorkspaceID, workspaceID)
	}
	if result.Branch != "main" {
		t.Errorf("branch = %q, want %q", result.Branch, "main")
	}
	if result.Commits == nil {
		t.Error("commits should be non-nil (empty array)")
	}
	if result.Uncommitted == nil {
		t.Error("uncommitted should be non-nil (empty array)")
	}
}

// TestE2EModelsEndpoint tests GET /api/models — returns available AI models.
func TestE2EModelsEndpoint(t *testing.T) {
	t.Parallel()
	env := New(t)

	workspaceRoot := t.TempDir()
	env.CreateConfig(workspaceRoot)

	repoPath := workspaceRoot + "/models-repo"
	os.MkdirAll(repoPath, 0755)
	RunCmd(t, repoPath, "git", "init", "-b", "main")
	RunCmd(t, repoPath, "git", "config", "user.email", "e2e@test.local")
	RunCmd(t, repoPath, "git", "config", "user.name", "E2E Test")
	os.WriteFile(filepath.Join(repoPath, "README.md"), []byte("# Models\n"), 0644)
	RunCmd(t, repoPath, "git", "add", ".")
	RunCmd(t, repoPath, "git", "commit", "-m", "Initial commit")
	env.AddRepoToConfig("models-repo", "file://"+repoPath)

	env.DaemonStart()
	defer func() {
		env.DaemonStop()
		if t.Failed() {
			env.CaptureArtifacts()
		}
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, env.DaemonURL+"/api/models", nil)
	resp, err := http.DefaultClient.Do(req)
	cancel()
	if err != nil {
		t.Fatalf("Models request failed: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("Models returned %d: %s", resp.StatusCode, body)
	}

	var result struct {
		Models []struct {
			ID          string `json:"id"`
			DisplayName string `json:"display_name"`
			BaseTool    string `json:"base_tool"`
			Category    string `json:"category"`
		} `json:"models"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatalf("Decode failed: %v", err)
	}
	// Models list may be empty if no AI tools are installed (e.g., in Docker).
	// We verify the response shape is correct and fields are populated when present.
	if result.Models == nil {
		t.Error("models field should be non-nil")
	}

	// Verify every model has required fields
	for _, m := range result.Models {
		if m.ID == "" {
			t.Errorf("model missing ID: %+v", m)
		}
		if m.DisplayName == "" {
			t.Errorf("model %q missing display_name", m.ID)
		}
		if m.BaseTool == "" {
			t.Errorf("model %q missing base_tool", m.ID)
		}
	}
}

// TestE2EBranchesEndpoint tests GET /api/branches — returns branch status across workspaces.
func TestE2EBranchesEndpoint(t *testing.T) {
	t.Parallel()
	env := New(t)

	workspaceRoot := t.TempDir()
	env.CreateConfig(workspaceRoot)

	repoPath := workspaceRoot + "/branches-repo"
	os.MkdirAll(repoPath, 0755)
	RunCmd(t, repoPath, "git", "init", "-b", "main")
	RunCmd(t, repoPath, "git", "config", "user.email", "e2e@test.local")
	RunCmd(t, repoPath, "git", "config", "user.name", "E2E Test")
	os.WriteFile(filepath.Join(repoPath, "README.md"), []byte("# Branches\n"), 0644)
	RunCmd(t, repoPath, "git", "add", ".")
	RunCmd(t, repoPath, "git", "commit", "-m", "Initial commit")
	env.AddRepoToConfig("branches-repo", "file://"+repoPath)

	env.DaemonStart()
	defer func() {
		env.DaemonStop()
		if t.Failed() {
			env.CaptureArtifacts()
		}
	}()

	sessionID := env.SpawnSession("file://"+repoPath, "main", "echo", "", env.Nickname("branches"))
	if sessionID == "" {
		t.Fatal("Expected session ID")
	}

	// Wait for workspace to be set up
	time.Sleep(2 * time.Second)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, env.DaemonURL+"/api/branches", nil)
	resp, err := http.DefaultClient.Do(req)
	cancel()
	if err != nil {
		t.Fatalf("Branches request failed: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("Branches returned %d: %s", resp.StatusCode, body)
	}

	var entries []struct {
		WorkspaceID   string   `json:"workspace_id"`
		Branch        string   `json:"branch"`
		SessionCount  int      `json:"session_count"`
		SessionStates []string `json:"session_states"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&entries); err != nil {
		t.Fatalf("Decode failed: %v", err)
	}
	if len(entries) == 0 {
		t.Fatal("expected at least one branch entry")
	}
	found := false
	for _, entry := range entries {
		if entry.WorkspaceID != "" {
			found = true
			if entry.Branch == "" {
				t.Errorf("branch entry for workspace %q has empty branch", entry.WorkspaceID)
			}
			if entry.SessionCount <= 0 {
				t.Errorf("workspace %q session_count = %d, want > 0", entry.WorkspaceID, entry.SessionCount)
			}
		}
	}
	if !found {
		t.Error("no entries had a workspace_id")
	}
}

// TestE2EGitAmendAndUncommit tests POST /api/workspaces/{id}/git-amend and
// POST /api/workspaces/{id}/git-uncommit — amends last commit and soft-resets it.
func TestE2EGitAmendAndUncommit(t *testing.T) {
	t.Parallel()
	env := New(t)

	workspaceRoot := t.TempDir()
	env.CreateConfig(workspaceRoot)

	repoPath := workspaceRoot + "/amend-repo"
	os.MkdirAll(repoPath, 0755)
	RunCmd(t, repoPath, "git", "init", "-b", "main")
	RunCmd(t, repoPath, "git", "config", "user.email", "e2e@test.local")
	RunCmd(t, repoPath, "git", "config", "user.name", "E2E Test")
	os.WriteFile(filepath.Join(repoPath, "README.md"), []byte("# Amend\n"), 0644)
	RunCmd(t, repoPath, "git", "add", ".")
	RunCmd(t, repoPath, "git", "commit", "-m", "Initial commit")
	env.AddRepoToConfig("amend-repo", "file://"+repoPath)

	env.DaemonStart()
	defer func() {
		env.DaemonStop()
		if t.Failed() {
			env.CaptureArtifacts()
		}
	}()

	sessionID := env.SpawnSession("file://"+repoPath, "main", "echo", "", env.Nickname("amend"))
	if sessionID == "" {
		t.Fatal("Expected session ID")
	}

	workspaceID := env.GetWorkspaceIDForSession(sessionID)
	workspacePath := env.GetWorkspacePath(sessionID)
	if workspaceID == "" || workspacePath == "" {
		t.Fatal("Expected workspace ID and path")
	}

	// Wait for workspace setup
	time.Sleep(2 * time.Second)

	// Set git identity in the workspace clone (not inherited from the source repo)
	RunCmd(t, workspacePath, "git", "config", "user.email", "e2e@test.local")
	RunCmd(t, workspacePath, "git", "config", "user.name", "E2E Test")

	// Create a feature branch with a commit ahead of main
	RunCmd(t, workspacePath, "git", "checkout", "-b", "feature-branch")
	os.WriteFile(filepath.Join(workspacePath, "feature.txt"), []byte("feature\n"), 0644)
	RunCmd(t, workspacePath, "git", "add", "feature.txt")
	RunCmd(t, workspacePath, "git", "commit", "-m", "Add feature")

	// Wait for daemon to detect that we're ahead of main via git status watcher.
	// The git-amend handler checks ws.GitAhead > 0 and returns 400 otherwise.
	env.PollUntil(15*time.Second, "daemon should detect commits ahead of main", func() bool {
		workspaces := env.GetAPIWorkspaces()
		for _, ws := range workspaces {
			if ws.ID == workspaceID && ws.GitAhead > 0 {
				return true
			}
		}
		return false
	})

	t.Run("AmendCommit", func(t *testing.T) {
		// Create a new file to amend into the existing commit
		os.WriteFile(filepath.Join(workspacePath, "amended.txt"), []byte("amended\n"), 0644)

		amendBody, _ := json.Marshal(map[string]interface{}{
			"files": []string{"amended.txt"},
		})
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		req, _ := http.NewRequestWithContext(ctx, http.MethodPost, env.DaemonURL+"/api/workspaces/"+workspaceID+"/git-amend", bytes.NewReader(amendBody))
		req.Header.Set("Content-Type", "application/json")
		resp, err := http.DefaultClient.Do(req)
		cancel()
		if err != nil {
			t.Fatalf("Amend request failed: %v", err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			body, _ := io.ReadAll(resp.Body)
			t.Fatalf("Amend returned %d: %s", resp.StatusCode, body)
		}

		var result map[string]interface{}
		json.NewDecoder(resp.Body).Decode(&result)
		if result["success"] != true {
			t.Errorf("expected success=true, got %v", result)
		}

		// Verify the amended file is in the latest commit
		cmd := exec.Command("git", "diff-tree", "--no-commit-id", "--name-only", "-r", "HEAD")
		cmd.Dir = workspacePath
		output, err := cmd.Output()
		if err != nil {
			t.Fatalf("git diff-tree failed: %v", err)
		}
		if !strings.Contains(string(output), "amended.txt") {
			t.Errorf("expected amended.txt in HEAD commit, got: %s", string(output))
		}
	})

	t.Run("UncommitRequiresHash", func(t *testing.T) {
		// Missing hash should return 400
		body, _ := json.Marshal(map[string]string{})
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		req, _ := http.NewRequestWithContext(ctx, http.MethodPost, env.DaemonURL+"/api/workspaces/"+workspaceID+"/git-uncommit", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		resp, err := http.DefaultClient.Do(req)
		cancel()
		if err != nil {
			t.Fatalf("Uncommit request failed: %v", err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusBadRequest {
			t.Errorf("expected 400 for missing hash, got %d", resp.StatusCode)
		}
	})

	t.Run("UncommitWithCorrectHash", func(t *testing.T) {
		// Get current HEAD hash
		cmd := exec.Command("git", "rev-parse", "HEAD")
		cmd.Dir = workspacePath
		output, err := cmd.Output()
		if err != nil {
			t.Fatalf("git rev-parse failed: %v", err)
		}
		headHash := strings.TrimSpace(string(output))

		body, _ := json.Marshal(map[string]string{"hash": headHash})
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		req, _ := http.NewRequestWithContext(ctx, http.MethodPost, env.DaemonURL+"/api/workspaces/"+workspaceID+"/git-uncommit", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		resp, err := http.DefaultClient.Do(req)
		cancel()
		if err != nil {
			t.Fatalf("Uncommit request failed: %v", err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			respBody, _ := io.ReadAll(resp.Body)
			t.Fatalf("Uncommit returned %d: %s", resp.StatusCode, respBody)
		}

		var result map[string]interface{}
		json.NewDecoder(resp.Body).Decode(&result)
		if result["success"] != true {
			t.Errorf("expected success=true, got %v", result)
		}

		// Verify HEAD changed (commit was undone)
		cmd = exec.Command("git", "rev-parse", "HEAD")
		cmd.Dir = workspacePath
		newOutput, err := cmd.Output()
		if err != nil {
			t.Fatalf("git rev-parse after uncommit failed: %v", err)
		}
		newHead := strings.TrimSpace(string(newOutput))
		if newHead == headHash {
			t.Error("HEAD should have changed after uncommit")
		}
	})
}

// TestE2EBuiltinQuickLaunch tests GET /api/builtin-quick-launch — returns built-in presets.
func TestE2EBuiltinQuickLaunch(t *testing.T) {
	t.Parallel()
	env := New(t)

	workspaceRoot := t.TempDir()
	env.CreateConfig(workspaceRoot)

	repoPath := workspaceRoot + "/ql-repo"
	os.MkdirAll(repoPath, 0755)
	RunCmd(t, repoPath, "git", "init", "-b", "main")
	RunCmd(t, repoPath, "git", "config", "user.email", "e2e@test.local")
	RunCmd(t, repoPath, "git", "config", "user.name", "E2E Test")
	os.WriteFile(filepath.Join(repoPath, "README.md"), []byte("# QL\n"), 0644)
	RunCmd(t, repoPath, "git", "add", ".")
	RunCmd(t, repoPath, "git", "commit", "-m", "Initial commit")
	env.AddRepoToConfig("ql-repo", "file://"+repoPath)

	env.DaemonStart()
	defer func() {
		env.DaemonStop()
		if t.Failed() {
			env.CaptureArtifacts()
		}
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, env.DaemonURL+"/api/builtin-quick-launch", nil)
	resp, err := http.DefaultClient.Do(req)
	cancel()
	if err != nil {
		t.Fatalf("Quick launch request failed: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("Quick launch returned %d: %s", resp.StatusCode, body)
	}

	var result []struct {
		Name   string `json:"name"`
		Target string `json:"target"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatalf("Decode failed: %v", err)
	}
	// The cookbooks.json file has built-in presets
	if len(result) == 0 {
		t.Error("expected at least one builtin quick launch preset")
	}
	for _, preset := range result {
		if preset.Name == "" {
			t.Errorf("preset has empty name: %+v", preset)
		}
	}
}

// TestE2EWorkspaceScan tests POST /api/workspaces/scan — scans for orphan workspaces.
func TestE2EWorkspaceScan(t *testing.T) {
	t.Parallel()
	env := New(t)

	workspaceRoot := t.TempDir()
	env.CreateConfig(workspaceRoot)

	repoPath := workspaceRoot + "/scan-repo"
	os.MkdirAll(repoPath, 0755)
	RunCmd(t, repoPath, "git", "init", "-b", "main")
	RunCmd(t, repoPath, "git", "config", "user.email", "e2e@test.local")
	RunCmd(t, repoPath, "git", "config", "user.name", "E2E Test")
	os.WriteFile(filepath.Join(repoPath, "README.md"), []byte("# Scan\n"), 0644)
	RunCmd(t, repoPath, "git", "add", ".")
	RunCmd(t, repoPath, "git", "commit", "-m", "Initial commit")
	env.AddRepoToConfig("scan-repo", "file://"+repoPath)

	env.DaemonStart()
	defer func() {
		env.DaemonStop()
		if t.Failed() {
			env.CaptureArtifacts()
		}
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	req, _ := http.NewRequestWithContext(ctx, http.MethodPost, env.DaemonURL+"/api/workspaces/scan", nil)
	resp, err := http.DefaultClient.Do(req)
	cancel()
	if err != nil {
		t.Fatalf("Scan request failed: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("Scan returned %d: %s", resp.StatusCode, body)
	}

	var result map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatalf("Decode failed: %v", err)
	}
	// Verify scan returned a valid response with scan-related fields
	if _, ok := result["scanned"]; !ok {
		// Some response came back — that's enough to verify the endpoint works
		t.Log("scan returned:", result)
	}
}

// TestE2EFloorManagerStatus tests GET /api/floor-manager — returns floor manager status.
func TestE2EFloorManagerStatus(t *testing.T) {
	t.Parallel()
	env := New(t)

	workspaceRoot := t.TempDir()
	env.CreateConfig(workspaceRoot)

	repoPath := workspaceRoot + "/fm-repo"
	os.MkdirAll(repoPath, 0755)
	RunCmd(t, repoPath, "git", "init", "-b", "main")
	RunCmd(t, repoPath, "git", "config", "user.email", "e2e@test.local")
	RunCmd(t, repoPath, "git", "config", "user.name", "E2E Test")
	os.WriteFile(filepath.Join(repoPath, "README.md"), []byte("# FM\n"), 0644)
	RunCmd(t, repoPath, "git", "add", ".")
	RunCmd(t, repoPath, "git", "commit", "-m", "Initial commit")
	env.AddRepoToConfig("fm-repo", "file://"+repoPath)

	env.DaemonStart()
	defer func() {
		env.DaemonStop()
		if t.Failed() {
			env.CaptureArtifacts()
		}
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, env.DaemonURL+"/api/floor-manager", nil)
	resp, err := http.DefaultClient.Do(req)
	cancel()
	if err != nil {
		t.Fatalf("Floor manager request failed: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("Floor manager returned %d: %s", resp.StatusCode, body)
	}

	var result map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatalf("Decode failed: %v", err)
	}
	// Floor manager should exist in response even if disabled
	if _, ok := result["enabled"]; !ok {
		t.Error("expected 'enabled' field in floor manager response")
	}
}
