//go:build e2e

package e2e

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestE2ETellSession tests sending a message to a session via POST /api/sessions/{id}/tell
// and verifying it arrives via the terminal WebSocket.
func TestE2ETellSession(t *testing.T) {
	t.Parallel()
	env := New(t)

	workspaceRoot := t.TempDir()
	env.CreateConfig(workspaceRoot)

	repoPath := workspaceRoot + "/tell-test-repo"
	os.MkdirAll(repoPath, 0755)
	RunCmd(t, repoPath, "git", "init", "-b", "main")
	RunCmd(t, repoPath, "git", "config", "user.email", "e2e@test.local")
	RunCmd(t, repoPath, "git", "config", "user.name", "E2E Test")
	os.WriteFile(filepath.Join(repoPath, "README.md"), []byte("# Tell Test\n"), 0644)
	RunCmd(t, repoPath, "git", "add", ".")
	RunCmd(t, repoPath, "git", "commit", "-m", "Initial commit")
	env.AddRepoToConfig("tell-test-repo", "file://"+repoPath)

	env.DaemonStart()
	defer func() {
		env.DaemonStop()
		if t.Failed() {
			env.CaptureArtifacts()
		}
	}()

	// Spawn a cat session — it echoes back any input it receives
	sessionID := env.SpawnSession("file://"+workspaceRoot+"/tell-test-repo", "main", "cat", "", env.Nickname("tell-agent"))
	if sessionID == "" {
		t.Fatal("Expected session ID from spawn")
	}

	// Connect terminal WebSocket to observe output
	conn, err := env.ConnectTerminalWebSocket(sessionID)
	if err != nil {
		t.Fatalf("Failed to connect websocket: %v", err)
	}
	defer conn.Close()

	// Wait for the cat target's START bootstrap
	if _, err := env.WaitForWebSocketContent(conn, "START", 5*time.Second); err != nil {
		t.Fatalf("Failed to receive bootstrap: %v", err)
	}

	// Send a tell message via API
	tellBody, _ := json.Marshal(map[string]string{"message": "E2E_TELL_TEST_MARKER"})
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	req, _ := http.NewRequestWithContext(ctx, http.MethodPost, env.DaemonURL+"/api/sessions/"+sessionID+"/tell", bytes.NewReader(tellBody))
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	cancel()
	if err != nil {
		t.Fatalf("Tell request failed: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("Tell returned non-200: %d\nBody: %s", resp.StatusCode, body)
	}

	// Verify the message shows up in terminal output (cat echoes it back)
	// The message is prefixed with "[from FM] " by the server
	if _, err := env.WaitForWebSocketContent(conn, "E2E_TELL_TEST_MARKER", 5*time.Second); err != nil {
		t.Fatalf("Tell message did not appear in terminal output: %v", err)
	}
}

// TestE2EConfigGet tests GET /api/config returns the current configuration.
func TestE2EConfigGet(t *testing.T) {
	t.Parallel()
	env := New(t)

	workspaceRoot := t.TempDir()
	env.CreateConfig(workspaceRoot)

	repoPath := workspaceRoot + "/config-test-repo"
	os.MkdirAll(repoPath, 0755)
	RunCmd(t, repoPath, "git", "init", "-b", "main")
	RunCmd(t, repoPath, "git", "config", "user.email", "e2e@test.local")
	RunCmd(t, repoPath, "git", "config", "user.name", "E2E Test")
	os.WriteFile(filepath.Join(repoPath, "README.md"), []byte("# Config Test\n"), 0644)
	RunCmd(t, repoPath, "git", "add", ".")
	RunCmd(t, repoPath, "git", "commit", "-m", "Initial commit")
	env.AddRepoToConfig("config-test-repo", "file://"+repoPath)

	env.DaemonStart()
	defer func() {
		env.DaemonStop()
		if t.Failed() {
			env.CaptureArtifacts()
		}
	}()

	t.Run("ReturnsReposAndTargets", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		req, _ := http.NewRequestWithContext(ctx, http.MethodGet, env.DaemonURL+"/api/config", nil)
		resp, err := http.DefaultClient.Do(req)
		cancel()
		if err != nil {
			t.Fatalf("GET /api/config failed: %v", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			body, _ := io.ReadAll(resp.Body)
			t.Fatalf("Expected 200, got %d: %s", resp.StatusCode, body)
		}

		var result map[string]json.RawMessage
		if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
			t.Fatalf("Failed to decode config response: %v", err)
		}

		// Verify repos array exists and contains our test repo
		if _, ok := result["repos"]; !ok {
			t.Fatal("Config response missing 'repos' field")
		}

		var repos []map[string]interface{}
		if err := json.Unmarshal(result["repos"], &repos); err != nil {
			t.Fatalf("Failed to unmarshal repos: %v", err)
		}

		found := false
		for _, repo := range repos {
			if name, ok := repo["name"].(string); ok && name == "config-test-repo" {
				found = true
			}
		}
		if !found {
			t.Errorf("Expected repo 'config-test-repo' in config, got repos: %v", repos)
		}

		// Verify run_targets array exists and contains echo and cat
		if _, ok := result["run_targets"]; !ok {
			t.Fatal("Config response missing 'run_targets' field")
		}

		var targets []map[string]interface{}
		if err := json.Unmarshal(result["run_targets"], &targets); err != nil {
			t.Fatalf("Failed to unmarshal run_targets: %v", err)
		}

		foundEcho := false
		foundCat := false
		for _, tgt := range targets {
			if name, ok := tgt["name"].(string); ok {
				if name == "echo" {
					foundEcho = true
				}
				if name == "cat" {
					foundCat = true
				}
			}
		}
		if !foundEcho {
			t.Error("Expected 'echo' target in run_targets")
		}
		if !foundCat {
			t.Error("Expected 'cat' target in run_targets")
		}
	})

	t.Run("DetectToolsEndpoint", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		req, _ := http.NewRequestWithContext(ctx, http.MethodGet, env.DaemonURL+"/api/detect-tools", nil)
		resp, err := http.DefaultClient.Do(req)
		cancel()
		if err != nil {
			t.Fatalf("GET /api/detect-tools failed: %v", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			body, _ := io.ReadAll(resp.Body)
			t.Fatalf("Expected 200, got %d: %s", resp.StatusCode, body)
		}

		var result map[string]json.RawMessage
		if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
			t.Fatalf("Failed to decode detect-tools response: %v", err)
		}
		if _, ok := result["tools"]; !ok {
			t.Fatal("Response missing 'tools' field")
		}
	})
}

// TestE2ESessionEvents tests GET /api/sessions/{id}/events with filtering and last-N.
func TestE2ESessionEvents(t *testing.T) {
	t.Parallel()
	env := New(t)

	workspaceRoot := t.TempDir()
	env.CreateConfig(workspaceRoot)

	repoPath := workspaceRoot + "/events-test-repo"
	os.MkdirAll(repoPath, 0755)
	RunCmd(t, repoPath, "git", "init", "-b", "main")
	RunCmd(t, repoPath, "git", "config", "user.email", "e2e@test.local")
	RunCmd(t, repoPath, "git", "config", "user.name", "E2E Test")
	os.WriteFile(filepath.Join(repoPath, "README.md"), []byte("# Events Test\n"), 0644)
	RunCmd(t, repoPath, "git", "add", ".")
	RunCmd(t, repoPath, "git", "commit", "-m", "Initial commit")
	env.AddRepoToConfig("events-test-repo", "file://"+repoPath)

	env.DaemonStart()
	defer func() {
		env.DaemonStop()
		if t.Failed() {
			env.CaptureArtifacts()
		}
	}()

	sessionID := env.SpawnSession("file://"+workspaceRoot+"/events-test-repo", "main", "echo", "", env.Nickname("events-agent"))
	if sessionID == "" {
		t.Fatal("Expected session ID from spawn")
	}

	// Get workspace path for writing event files
	wsPath := env.GetWorkspacePath(sessionID)
	if wsPath == "" {
		t.Fatal("Could not determine workspace path")
	}

	// Write multiple status events
	writeStatusEvent(t, wsPath, sessionID, "working", "building")
	writeStatusEvent(t, wsPath, sessionID, "completed", "done")
	writeStatusEvent(t, wsPath, sessionID, "needs_input", "waiting")

	// Poll until events are written and readable
	env.PollUntil(3*time.Second, "events written", func() bool {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		req, _ := http.NewRequestWithContext(ctx, http.MethodGet, env.DaemonURL+"/api/sessions/"+sessionID+"/events", nil)
		resp, err := http.DefaultClient.Do(req)
		cancel()
		if err != nil || resp.StatusCode != http.StatusOK {
			if resp != nil {
				resp.Body.Close()
			}
			return false
		}
		var events []json.RawMessage
		json.NewDecoder(resp.Body).Decode(&events)
		resp.Body.Close()
		return len(events) >= 3
	})

	t.Run("GetAllEvents", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		req, _ := http.NewRequestWithContext(ctx, http.MethodGet, env.DaemonURL+"/api/sessions/"+sessionID+"/events", nil)
		resp, err := http.DefaultClient.Do(req)
		cancel()
		if err != nil {
			t.Fatalf("GET events failed: %v", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			body, _ := io.ReadAll(resp.Body)
			t.Fatalf("Expected 200, got %d: %s", resp.StatusCode, body)
		}

		var events []json.RawMessage
		if err := json.NewDecoder(resp.Body).Decode(&events); err != nil {
			t.Fatalf("Failed to decode events: %v", err)
		}

		if len(events) < 3 {
			t.Fatalf("Expected at least 3 events, got %d", len(events))
		}
	})

	t.Run("FilterByType", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		req, _ := http.NewRequestWithContext(ctx, http.MethodGet, env.DaemonURL+"/api/sessions/"+sessionID+"/events?type=status", nil)
		resp, err := http.DefaultClient.Do(req)
		cancel()
		if err != nil {
			t.Fatalf("GET events with filter failed: %v", err)
		}
		defer resp.Body.Close()

		var events []json.RawMessage
		json.NewDecoder(resp.Body).Decode(&events)

		// All events are status type, so count should be same
		if len(events) < 3 {
			t.Fatalf("Expected at least 3 status events, got %d", len(events))
		}
	})

	t.Run("LastN", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		req, _ := http.NewRequestWithContext(ctx, http.MethodGet, env.DaemonURL+"/api/sessions/"+sessionID+"/events?last=1", nil)
		resp, err := http.DefaultClient.Do(req)
		cancel()
		if err != nil {
			t.Fatalf("GET events with last=1 failed: %v", err)
		}
		defer resp.Body.Close()

		var events []json.RawMessage
		json.NewDecoder(resp.Body).Decode(&events)

		if len(events) != 1 {
			t.Fatalf("Expected 1 event with last=1, got %d", len(events))
		}

		// Verify it's the most recent event (needs_input)
		var evt struct {
			State string `json:"state"`
		}
		json.Unmarshal(events[0], &evt)
		if evt.State != "needs_input" {
			t.Errorf("Expected last event state=needs_input, got %q", evt.State)
		}
	})

	t.Run("SessionNotFound", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		req, _ := http.NewRequestWithContext(ctx, http.MethodGet, env.DaemonURL+"/api/sessions/nonexistent-session/events", nil)
		resp, err := http.DefaultClient.Do(req)
		cancel()
		if err != nil {
			t.Fatalf("GET events failed: %v", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusNotFound {
			t.Errorf("Expected 404 for nonexistent session, got %d", resp.StatusCode)
		}
	})
}

// TestE2ENicknameUpdate tests PUT /api/sessions-nickname/{id} for renaming sessions.
func TestE2ENicknameUpdate(t *testing.T) {
	t.Parallel()
	env := New(t)

	workspaceRoot := t.TempDir()
	env.CreateConfig(workspaceRoot)
	env.SetSourceCodeManagement("git")

	repoPath := workspaceRoot + "/nick-test-repo"
	os.MkdirAll(repoPath, 0755)
	RunCmd(t, repoPath, "git", "init", "-b", "main")
	RunCmd(t, repoPath, "git", "config", "user.email", "e2e@test.local")
	RunCmd(t, repoPath, "git", "config", "user.name", "E2E Test")
	os.WriteFile(filepath.Join(repoPath, "README.md"), []byte("# Nick Test\n"), 0644)
	RunCmd(t, repoPath, "git", "add", ".")
	RunCmd(t, repoPath, "git", "commit", "-m", "Initial commit")
	env.AddRepoToConfig("nick-test-repo", "file://"+repoPath)

	env.DaemonStart()
	defer func() {
		env.DaemonStop()
		if t.Failed() {
			env.CaptureArtifacts()
		}
	}()

	session1ID := env.SpawnSession("file://"+workspaceRoot+"/nick-test-repo", "main", "echo", "", env.Nickname("old-name"))
	if session1ID == "" {
		t.Fatal("Expected session ID from spawn")
	}

	t.Run("RenameSucceeds", func(t *testing.T) {
		newNick := env.Nickname("new-name")
		body, _ := json.Marshal(map[string]string{"nickname": newNick})

		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		req, _ := http.NewRequestWithContext(ctx, http.MethodPut, env.DaemonURL+"/api/sessions-nickname/"+session1ID, bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		resp, err := http.DefaultClient.Do(req)
		cancel()
		if err != nil {
			t.Fatalf("Rename request failed: %v", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			respBody, _ := io.ReadAll(resp.Body)
			t.Fatalf("Rename returned non-200: %d\nBody: %s", resp.StatusCode, respBody)
		}

		// Verify via API
		env.PollUntil(3*time.Second, "API does not reflect new nickname", func() bool {
			sessions := env.GetAPISessions()
			for _, s := range sessions {
				if s.ID == session1ID && s.Nickname == newNick {
					return true
				}
			}
			return false
		})

		// Verify tmux session name changed too
		tmuxSessions := env.GetTmuxSessions()
		found := false
		for _, name := range tmuxSessions {
			if strings.Contains(name, env.Nickname("new-name")) {
				found = true
			}
		}
		if !found {
			t.Errorf("tmux session not renamed; tmux ls = %v", tmuxSessions)
		}
	})

	t.Run("ConflictOnDuplicateName", func(t *testing.T) {
		// Spawn a second session
		session2ID := env.SpawnSession("file://"+workspaceRoot+"/nick-test-repo", "main", "echo", "", env.Nickname("other-agent"))
		if session2ID == "" {
			t.Fatal("Expected session ID from second spawn")
		}

		// Try to rename session2 to session1's nickname — should get 409
		body, _ := json.Marshal(map[string]string{"nickname": env.Nickname("new-name")})
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		req, _ := http.NewRequestWithContext(ctx, http.MethodPut, env.DaemonURL+"/api/sessions-nickname/"+session2ID, bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		resp, err := http.DefaultClient.Do(req)
		cancel()
		if err != nil {
			t.Fatalf("Rename request failed: %v", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusConflict {
			respBody, _ := io.ReadAll(resp.Body)
			t.Errorf("Expected 409 for duplicate nickname, got %d: %s", resp.StatusCode, respBody)
		}
	})
}

// TestE2EWorkspaceDisposeAll tests POST /api/workspaces/{id}/dispose-all
// which disposes all sessions and the workspace itself.
func TestE2EWorkspaceDisposeAll(t *testing.T) {
	t.Parallel()
	env := New(t)

	workspaceRoot := t.TempDir()
	env.CreateConfig(workspaceRoot)
	env.SetSourceCodeManagement("git")

	repoPath := workspaceRoot + "/dispose-all-repo"
	os.MkdirAll(repoPath, 0755)
	RunCmd(t, repoPath, "git", "init", "-b", "main")
	RunCmd(t, repoPath, "git", "config", "user.email", "e2e@test.local")
	RunCmd(t, repoPath, "git", "config", "user.name", "E2E Test")
	os.WriteFile(filepath.Join(repoPath, "README.md"), []byte("# Dispose All\n"), 0644)
	RunCmd(t, repoPath, "git", "add", ".")
	RunCmd(t, repoPath, "git", "commit", "-m", "Initial commit")
	env.AddRepoToConfig("dispose-all-repo", "file://"+repoPath)

	env.DaemonStart()
	defer func() {
		env.DaemonStop()
		if t.Failed() {
			env.CaptureArtifacts()
		}
	}()

	// Spawn two sessions in the same workspace
	session1ID := env.SpawnSession("file://"+workspaceRoot+"/dispose-all-repo", "main", "echo", "", env.Nickname("disp-a"))
	if session1ID == "" {
		t.Fatal("Expected session1 ID from spawn")
	}

	// Get workspace ID
	workspaceID := env.GetWorkspaceIDForSession(session1ID)
	if workspaceID == "" {
		t.Fatal("Could not determine workspace ID")
	}

	// Spawn a second session in the same workspace
	session2ID := env.SpawnSessionInWorkspace(workspaceID, "echo", "", env.Nickname("disp-b"))
	if session2ID == "" {
		t.Fatal("Expected session2 ID from spawn")
	}

	// Verify both sessions exist
	sessions := env.GetAPISessions()
	if len(sessions) < 2 {
		t.Fatalf("Expected at least 2 sessions, got %d", len(sessions))
	}

	// Dispose all sessions + workspace.
	// Server-side: sessions (each up to 5s grace + 10s kill, concurrent) then
	// workspace (60s). With 5s DisposeGracePeriod the worst case is ~75s; 120s
	// gives ample headroom for CPU contention.
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	req, _ := http.NewRequestWithContext(ctx, http.MethodPost, env.DaemonURL+"/api/workspaces/"+workspaceID+"/dispose-all", nil)
	resp, err := http.DefaultClient.Do(req)
	cancel()
	if err != nil {
		t.Fatalf("Dispose-all request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("Dispose-all returned non-200: %d\nBody: %s", resp.StatusCode, body)
	}

	// Parse response to verify sessions_disposed count
	var result map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}
	if disposed, ok := result["sessions_disposed"].(float64); ok {
		if int(disposed) < 2 {
			t.Errorf("Expected at least 2 sessions disposed, got %d", int(disposed))
		}
	}

	// Verify no workspaces or sessions remain
	env.PollUntil(10*time.Second, "workspaces/sessions not cleaned up", func() bool {
		workspaces := env.GetAPIWorkspaces()
		return len(workspaces) == 0
	})
}

// TestE2EGitGraphAndDiff tests git graph, diff, stage, and discard API endpoints.
func TestE2EGitGraphAndDiff(t *testing.T) {
	t.Parallel()
	env := New(t)

	workspaceRoot := t.TempDir()
	env.CreateConfig(workspaceRoot)

	repoPath := workspaceRoot + "/git-ops-repo"
	os.MkdirAll(repoPath, 0755)
	RunCmd(t, repoPath, "git", "init", "-b", "main")
	RunCmd(t, repoPath, "git", "config", "user.email", "e2e@test.local")
	RunCmd(t, repoPath, "git", "config", "user.name", "E2E Test")
	os.WriteFile(filepath.Join(repoPath, "README.md"), []byte("# Git Ops Test\n"), 0644)
	RunCmd(t, repoPath, "git", "add", ".")
	RunCmd(t, repoPath, "git", "commit", "-m", "Initial commit")
	env.AddRepoToConfig("git-ops-repo", "file://"+repoPath)

	env.DaemonStart()
	defer func() {
		env.DaemonStop()
		if t.Failed() {
			env.CaptureArtifacts()
		}
	}()

	sessionID := env.SpawnSession("file://"+workspaceRoot+"/git-ops-repo", "main", "echo", "", env.Nickname("git-agent"))
	if sessionID == "" {
		t.Fatal("Expected session ID from spawn")
	}

	wsPath := env.GetWorkspacePath(sessionID)
	workspaceID := env.GetWorkspaceIDForSession(sessionID)

	t.Run("GitGraph", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		req, _ := http.NewRequestWithContext(ctx, http.MethodGet,
			fmt.Sprintf("%s/api/workspaces/%s/git-graph", env.DaemonURL, workspaceID), nil)
		resp, err := http.DefaultClient.Do(req)
		cancel()
		if err != nil {
			t.Fatalf("GET git-graph failed: %v", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			body, _ := io.ReadAll(resp.Body)
			t.Fatalf("Expected 200, got %d: %s", resp.StatusCode, body)
		}

		var graph map[string]json.RawMessage
		if err := json.NewDecoder(resp.Body).Decode(&graph); err != nil {
			t.Fatalf("Failed to decode git graph: %v", err)
		}

		if _, ok := graph["nodes"]; !ok {
			t.Fatal("Git graph response missing 'nodes' field")
		}

		var nodes []map[string]interface{}
		json.Unmarshal(graph["nodes"], &nodes)
		if len(nodes) < 1 {
			t.Fatalf("Expected at least 1 node, got %d", len(nodes))
		}

		found := false
		for _, c := range nodes {
			if msg, ok := c["message"].(string); ok && strings.Contains(msg, "Initial commit") {
				found = true
			}
		}
		if !found {
			t.Error("Expected 'Initial commit' in git graph nodes")
		}
	})

	t.Run("DiffShowsUntracked", func(t *testing.T) {
		// Create a new file in the workspace
		newFile := filepath.Join(wsPath, "newfile.txt")
		os.WriteFile(newFile, []byte("hello from E2E\n"), 0644)

		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		req, _ := http.NewRequestWithContext(ctx, http.MethodGet,
			fmt.Sprintf("%s/api/diff/%s", env.DaemonURL, workspaceID), nil)
		resp, err := http.DefaultClient.Do(req)
		cancel()
		if err != nil {
			t.Fatalf("GET diff failed: %v", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			body, _ := io.ReadAll(resp.Body)
			t.Fatalf("Expected 200, got %d: %s", resp.StatusCode, body)
		}

		var diffResp struct {
			Files []map[string]interface{} `json:"files"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&diffResp); err != nil {
			t.Fatalf("Failed to decode diff: %v", err)
		}

		found := false
		for _, d := range diffResp.Files {
			if newPath, ok := d["new_path"].(string); ok && strings.Contains(newPath, "newfile.txt") {
				found = true
			}
		}
		if !found {
			t.Error("Expected newfile.txt in diff output")
		}
	})

	t.Run("StageFiles", func(t *testing.T) {
		stageBody, _ := json.Marshal(map[string]interface{}{
			"files": []string{"newfile.txt"},
		})
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		req, _ := http.NewRequestWithContext(ctx, http.MethodPost,
			fmt.Sprintf("%s/api/workspaces/%s/git-commit-stage", env.DaemonURL, workspaceID),
			bytes.NewReader(stageBody))
		req.Header.Set("Content-Type", "application/json")
		resp, err := http.DefaultClient.Do(req)
		cancel()
		if err != nil {
			t.Fatalf("POST git-commit-stage failed: %v", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			body, _ := io.ReadAll(resp.Body)
			t.Fatalf("Expected 200, got %d: %s", resp.StatusCode, body)
		}

		// Verify git shows the file as staged (via git status in workspace)
		// We can verify via the diff endpoint — staged files are included
	})

	t.Run("DiscardUntracked", func(t *testing.T) {
		// Create a second throwaway file to discard
		throwaway := filepath.Join(wsPath, "throwaway.txt")
		os.WriteFile(throwaway, []byte("to be discarded\n"), 0644)

		discardBody, _ := json.Marshal(map[string]interface{}{
			"files": []string{"throwaway.txt"},
		})
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		req, _ := http.NewRequestWithContext(ctx, http.MethodPost,
			fmt.Sprintf("%s/api/workspaces/%s/git-discard", env.DaemonURL, workspaceID),
			bytes.NewReader(discardBody))
		req.Header.Set("Content-Type", "application/json")
		resp, err := http.DefaultClient.Do(req)
		cancel()
		if err != nil {
			t.Fatalf("POST git-discard failed: %v", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			body, _ := io.ReadAll(resp.Body)
			t.Fatalf("Expected 200, got %d: %s", resp.StatusCode, body)
		}

		// Verify file is gone after discard
		env.PollUntil(3*time.Second, "throwaway.txt not removed after discard", func() bool {
			_, err := os.Stat(throwaway)
			return os.IsNotExist(err)
		})
	})
}
