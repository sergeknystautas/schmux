//go:build e2e

package e2e

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestE2EFullLifecycle runs the full E2E test suite as one integrated test.
// This validates the complete flow: daemon → workspace → sessions → cleanup.
func TestE2EFullLifecycle(t *testing.T) {
	env := New(t)

	// Step 1: Create config
	const workspaceRoot = "/tmp/schmux-e2e-workspaces"
	t.Run("01_CreateConfig", func(t *testing.T) {
		env.CreateConfig(workspaceRoot)
	})

	// Step 2: Create local git repo BEFORE starting daemon
	t.Run("02_CreateGitRepo", func(t *testing.T) {
		// Create repo in the configured workspace root
		repoPath := workspaceRoot + "/test-repo"
		if err := os.MkdirAll(repoPath, 0755); err != nil {
			t.Fatalf("Failed to create repo dir: %v", err)
		}

		// Initialize git repo on main to match test branch usage.
		RunCmd(t, repoPath, "git", "init", "-b", "main")
		RunCmd(t, repoPath, "git", "config", "user.email", "e2e@test.local")
		RunCmd(t, repoPath, "git", "config", "user.name", "E2E Test")

		// Create a test file
		testFile := filepath.Join(repoPath, "README.md")
		if err := os.WriteFile(testFile, []byte("# Test Repo\n"), 0644); err != nil {
			t.Fatalf("Failed to create test file: %v", err)
		}

		// Commit
		RunCmd(t, repoPath, "git", "add", ".")
		RunCmd(t, repoPath, "git", "commit", "-m", "Initial commit")

		// Add repo to config BEFORE starting daemon
		env.AddRepoToConfig("test-repo", "file://"+repoPath)
	})

	// Step 3: Start daemon (will load config with the repo)
	t.Run("03_DaemonStart", func(t *testing.T) {
		env.DaemonStart()
	})

	// Ensure we capture artifacts if anything fails
	defer func() {
		if t.Failed() {
			env.CaptureArtifacts()
		}
	}()

	// Step 4: Spawn two sessions with different nicknames
	var session1ID, session2ID string
	t.Run("04_SpawnTwoSessions", func(t *testing.T) {
		// Spawn session 1
		env.SpawnSession("file://"+workspaceRoot+"/test-repo", "main", "echo", "", "agent-one")
		// Spawn session 2
		env.SpawnSession("file://"+workspaceRoot+"/test-repo", "main", "echo", "", "agent-two")

		// Verify sessions via API
		sessions := env.GetAPISessions()
		if len(sessions) < 2 {
			t.Fatalf("Expected at least 2 sessions, got %d", len(sessions))
		}

		// Extract session IDs and verify nicknames
		for _, sess := range sessions {
			if sess.Nickname == "agent-one" {
				session1ID = sess.ID
			} else if sess.Nickname == "agent-two" {
				session2ID = sess.ID
			}
		}

		if session1ID == "" {
			t.Error("Session with nickname 'agent-one' not found in API response")
		}
		if session2ID == "" {
			t.Error("Session with nickname 'agent-two' not found in API response")
		}
	})

	// Step 5: Verify naming consistency across CLI, tmux, and API
	t.Run("05_VerifyNamingConsistency", func(t *testing.T) {
		// Verify CLI list shows the sessions
		cliOutput := env.ListSessions()
		if !strings.Contains(cliOutput, "agent-one") {
			t.Error("CLI list does not contain 'agent-one'")
		}
		if !strings.Contains(cliOutput, "agent-two") {
			t.Error("CLI list does not contain 'agent-two'")
		}

		// Verify tmux ls shows the sessions (names are sanitized)
		tmuxSessions := env.GetTmuxSessions()
		t.Logf("tmux sessions: %v", tmuxSessions)

		// Look for sanitized versions (hyphens become underscores)
		foundOne := false
		foundTwo := false
		for _, name := range tmuxSessions {
			if strings.Contains(name, "agent") && strings.Contains(name, "one") {
				foundOne = true
			}
			if strings.Contains(name, "agent") && strings.Contains(name, "two") {
				foundTwo = true
			}
		}
		if !foundOne {
			t.Error("tmux ls does not show session for agent-one")
		}
		if !foundTwo {
			t.Error("tmux ls does not show session for agent-two")
		}

		// Verify API shows both sessions with correct nicknames
		apiSessions := env.GetAPISessions()
		if len(apiSessions) < 2 {
			t.Errorf("API returned only %d sessions, expected at least 2", len(apiSessions))
		}

		hasOne := false
		hasTwo := false
		for _, sess := range apiSessions {
			if sess.Nickname == "agent-one" {
				hasOne = true
			}
			if sess.Nickname == "agent-two" {
				hasTwo = true
			}
		}
		if !hasOne {
			t.Error("API does not show session with nickname 'agent-one'")
		}
		if !hasTwo {
			t.Error("API does not show session with nickname 'agent-two'")
		}
	})

	// Step 6: Verify workspace was created
	t.Run("06_VerifyWorkspace", func(t *testing.T) {
		sessions := env.GetAPISessions()
		if len(sessions) == 0 {
			t.Fatal("No sessions found")
		}

		// All sessions should be in the same workspace
		workspaceID := sessions[0].ID
		// Session ID format is "workspaceID-uuid", so we can extract workspace
		parts := strings.Split(workspaceID, "-")
		if len(parts) < 2 {
			t.Errorf("Unexpected session ID format: %s", workspaceID)
		}
	})

	// Step 7: Dispose sessions
	t.Run("07_DisposeSessions", func(t *testing.T) {
		if session1ID != "" {
			env.DisposeSession(session1ID)
		}
		if session2ID != "" {
			env.DisposeSession(session2ID)
		}

		// Verify sessions are gone
		sessions := env.GetAPISessions()
		for _, sess := range sessions {
			if sess.ID == session1ID || sess.ID == session2ID {
				t.Error("Session still exists after dispose")
			}
		}

		// Verify tmux sessions are gone
		tmuxSessions := env.GetTmuxSessions()
		for _, name := range tmuxSessions {
			if strings.Contains(name, "agent") && (strings.Contains(name, "one") || strings.Contains(name, "two")) {
				t.Errorf("tmux session still exists after dispose: %s", name)
			}
		}
	})

	// Step 8: Stop daemon
	t.Run("08_DaemonStop", func(t *testing.T) {
		env.DaemonStop()

		// Verify health endpoint is no longer reachable
		if env.HealthCheck() {
			t.Error("Health endpoint still responds after daemon stop")
		}
	})
}

// TestE2EDaemonLifecycle tests daemon start/stop and health endpoint.
func TestE2EDaemonLifecycle(t *testing.T) {
	env := New(t)

	t.Run("CreateConfig", func(t *testing.T) {
		env.CreateConfig("/tmp/schmux-e2e-daemon-test")
	})

	t.Run("DaemonStart", func(t *testing.T) {
		env.DaemonStart()
		if !env.HealthCheck() {
			t.Error("Health check failed after daemon start")
		}
	})

	defer func() {
		if t.Failed() {
			env.CaptureArtifacts()
		}
	}()

	t.Run("DaemonStop", func(t *testing.T) {
		env.DaemonStop()
		if env.HealthCheck() {
			t.Error("Health check still succeeds after daemon stop")
		}
	})
}

// TestE2ETwoSessionsNaming tests session nickname uniqueness and consistency.
func TestE2ETwoSessionsNaming(t *testing.T) {
	env := New(t)

	const workspaceRoot = "/tmp/schmux-e2e-naming-test"

	t.Run("CreateConfig", func(t *testing.T) {
		env.CreateConfig(workspaceRoot)
	})

	t.Run("CreateGitRepo", func(t *testing.T) {
		// Create repo in the configured workspace root
		repoPath := workspaceRoot + "/naming-test-repo"
		if err := os.MkdirAll(repoPath, 0755); err != nil {
			t.Fatalf("Failed to create repo dir: %v", err)
		}

		// Initialize git repo on main to match test branch usage.
		RunCmd(t, repoPath, "git", "init", "-b", "main")
		RunCmd(t, repoPath, "git", "config", "user.email", "e2e@test.local")
		RunCmd(t, repoPath, "git", "config", "user.name", "E2E Test")

		// Create a test file
		testFile := filepath.Join(repoPath, "README.md")
		if err := os.WriteFile(testFile, []byte("# Naming Test\n"), 0644); err != nil {
			t.Fatalf("Failed to create test file: %v", err)
		}

		// Commit
		RunCmd(t, repoPath, "git", "add", ".")
		RunCmd(t, repoPath, "git", "commit", "-m", "Initial commit")

		// Add repo to config BEFORE starting daemon
		env.AddRepoToConfig("naming-test-repo", "file://"+repoPath)
	})

	t.Run("DaemonStart", func(t *testing.T) {
		env.DaemonStart()
	})

	defer func() {
		env.DaemonStop()
		if t.Failed() {
			env.CaptureArtifacts()
		}
	}()

	t.Run("SpawnSessions", func(t *testing.T) {
		// Spawn two sessions with distinct nicknames
		env.SpawnSession("file://"+workspaceRoot+"/naming-test-repo", "main", "echo", "", "alpha")
		env.SpawnSession("file://"+workspaceRoot+"/naming-test-repo", "main", "echo", "", "beta")
	})

	t.Run("VerifyCLI", func(t *testing.T) {
		output := env.ListSessions()
		if !strings.Contains(output, "alpha") {
			t.Error("CLI list does not contain 'alpha'")
		}
		if !strings.Contains(output, "beta") {
			t.Error("CLI list does not contain 'beta'")
		}
	})

	t.Run("VerifyAPI", func(t *testing.T) {
		sessions := env.GetAPISessions()
		if len(sessions) < 2 {
			t.Fatalf("Expected at least 2 sessions, got %d", len(sessions))
		}

		hasAlpha := false
		hasBeta := false
		for _, sess := range sessions {
			if sess.Nickname == "alpha" {
				hasAlpha = true
			}
			if sess.Nickname == "beta" {
				hasBeta = true
			}
		}

		if !hasAlpha {
			t.Error("API does not show session with nickname 'alpha'")
		}
		if !hasBeta {
			t.Error("API does not show session with nickname 'beta'")
		}
	})

	t.Run("VerifyTmux", func(t *testing.T) {
		tmuxSessions := env.GetTmuxSessions()
		if len(tmuxSessions) < 2 {
			t.Errorf("Expected at least 2 tmux sessions, got %d", len(tmuxSessions))
		}

		// Check that we have sessions with our nicknames (sanitized)
		hasAlpha := false
		hasBeta := false
		for _, name := range tmuxSessions {
			if strings.Contains(name, "alpha") {
				hasAlpha = true
			}
			if strings.Contains(name, "beta") {
				hasBeta = true
			}
		}

		if !hasAlpha {
			t.Error("tmux does not show session with 'alpha'")
		}
		if !hasBeta {
			t.Error("tmux does not show session with 'beta'")
		}
	})
}

// TestE2ETwitterStream validates websocket output after tmux input.
func TestE2ETwitterStream(t *testing.T) {
	env := New(t)

	const workspaceRoot = "/tmp/schmux-e2e-ws-test"

	t.Run("CreateConfig", func(t *testing.T) {
		env.CreateConfig(workspaceRoot)
	})

	t.Run("CreateGitRepo", func(t *testing.T) {
		repoPath := workspaceRoot + "/ws-test-repo"
		if err := os.MkdirAll(repoPath, 0755); err != nil {
			t.Fatalf("Failed to create repo dir: %v", err)
		}

		RunCmd(t, repoPath, "git", "init", "-b", "main")
		RunCmd(t, repoPath, "git", "config", "user.email", "e2e@test.local")
		RunCmd(t, repoPath, "git", "config", "user.name", "E2E Test")

		testFile := filepath.Join(repoPath, "README.md")
		if err := os.WriteFile(testFile, []byte("# WS Test\n"), 0644); err != nil {
			t.Fatalf("Failed to create test file: %v", err)
		}

		RunCmd(t, repoPath, "git", "add", ".")
		RunCmd(t, repoPath, "git", "commit", "-m", "Initial commit")

		env.AddRepoToConfig("ws-test-repo", "file://"+repoPath)
	})

	t.Run("DaemonStart", func(t *testing.T) {
		env.DaemonStart()
	})

	defer func() {
		env.DaemonStop()
		if t.Failed() {
			env.CaptureArtifacts()
		}
	}()

	var sessionID string
	t.Run("SpawnSession", func(t *testing.T) {
		sessionID = env.SpawnSession("file://"+workspaceRoot+"/ws-test-repo", "main", "cat", "", "ws-echo")
		if sessionID == "" {
			t.Fatal("Expected session ID from spawn")
		}
	})

	t.Run("WebSocketOutput", func(t *testing.T) {
		conn, err := env.ConnectTerminalWebSocket(sessionID)
		if err != nil {
			t.Fatalf("Failed to connect websocket: %v", err)
		}
		defer conn.Close()

		payload := "ws-e2e-hello"
		env.SendKeysToTmux("ws-echo", payload)

		if _, err := env.WaitForWebSocketContent(conn, payload, 3*time.Second); err != nil {
			t.Fatalf("Did not receive websocket output: %v", err)
		}
	})
}
