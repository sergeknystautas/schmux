//go:build e2e

package e2e

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

func TestMain(m *testing.M) {
	requireDocker()
	os.Exit(m.Run())
}

func requireDocker() {
	if isRunningInDocker() {
		return
	}
	fmt.Fprintln(os.Stderr, "E2E tests must run in Docker. Please use the Docker runner.")
	os.Exit(1)
}

func isRunningInDocker() bool {
	if _, err := os.Stat("/.dockerenv"); err == nil {
		return true
	}
	if _, err := os.Stat("/run/.containerenv"); err == nil {
		return true
	}
	data, err := os.ReadFile("/proc/1/cgroup")
	if err != nil {
		return false
	}
	cgroup := string(data)
	return strings.Contains(cgroup, "docker") ||
		strings.Contains(cgroup, "containerd") ||
		strings.Contains(cgroup, "kubepods") ||
		strings.Contains(cgroup, "podman")
}

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

	// Step 3: Enable git source code management to allow multiple sessions per branch
	t.Run("03_EnableGitSCM", func(t *testing.T) {
		env.SetSourceCodeManagement("git")
	})

	// Step 4: Start daemon (will load config with the repo)
	t.Run("04_DaemonStart", func(t *testing.T) {
		env.DaemonStart()
	})

	// Ensure we capture artifacts if anything fails
	defer func() {
		if t.Failed() {
			env.CaptureArtifacts()
		}
	}()

	// Step 5: Spawn two sessions with different nicknames
	var session1ID, session2ID string
	t.Run("05_SpawnTwoSessions", func(t *testing.T) {
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

	// Step 6: Verify naming consistency across CLI, tmux, and API
	t.Run("06_VerifyNamingConsistency", func(t *testing.T) {
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

	// Step 7: Verify workspace was created
	t.Run("07_VerifyWorkspace", func(t *testing.T) {
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

	// Step 8: Dispose sessions
	t.Run("08_DisposeSessions", func(t *testing.T) {
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

	// Step 9: Stop daemon
	t.Run("09_DaemonStop", func(t *testing.T) {
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

	t.Run("EnableGitSCM", func(t *testing.T) {
		env.SetSourceCodeManagement("git")
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

// TestE2ESourceCodeManagement tests that the source_code_manager config controls
// whether workspaces are created via worktree or full clone.
func TestE2ESourceCodeManagement(t *testing.T) {
	env := New(t)

	workspaceRoot := t.TempDir()

	t.Run("CreateConfig", func(t *testing.T) {
		env.CreateConfig(workspaceRoot)
	})

	t.Run("CreateGitRepo", func(t *testing.T) {
		repoPath := workspaceRoot + "/scm-test-repo"
		if err := os.MkdirAll(repoPath, 0755); err != nil {
			t.Fatalf("Failed to create repo dir: %v", err)
		}

		RunCmd(t, repoPath, "git", "init", "-b", "main")
		RunCmd(t, repoPath, "git", "config", "user.email", "e2e@test.local")
		RunCmd(t, repoPath, "git", "config", "user.name", "E2E Test")

		testFile := filepath.Join(repoPath, "README.md")
		if err := os.WriteFile(testFile, []byte("# SCM Test\n"), 0644); err != nil {
			t.Fatalf("Failed to create test file: %v", err)
		}

		RunCmd(t, repoPath, "git", "add", ".")
		RunCmd(t, repoPath, "git", "commit", "-m", "Initial commit")

		env.AddRepoToConfig("scm-test-repo", "file://"+repoPath)
	})

	t.Run("SetSourceCodeManagementGit", func(t *testing.T) {
		// Set source_code_manager to "git" (full clone mode)
		env.SetSourceCodeManagement("git")
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

	var session1ID, session2ID string

	t.Run("SpawnFirstSession", func(t *testing.T) {
		// First spawn on "main" - should create full clone
		session1ID = env.SpawnSession("file://"+workspaceRoot+"/scm-test-repo", "main", "echo", "", "first-agent")
		if session1ID == "" {
			t.Fatal("Expected session ID from first spawn")
		}
		t.Logf("First session ID: %s", session1ID)
	})

	t.Run("SpawnSecondSessionSameBranch", func(t *testing.T) {
		// Second spawn on same "main" branch - should succeed with full clone mode
		session2ID = env.SpawnSession("file://"+workspaceRoot+"/scm-test-repo", "main", "echo", "", "second-agent")
		if session2ID == "" {
			t.Fatal("Expected session ID from second spawn")
		}
		t.Logf("Second session ID: %s", session2ID)
	})

	t.Run("VerifyDifferentWorkspaces", func(t *testing.T) {
		workspaces := env.GetAPIWorkspaces()

		if len(workspaces) < 2 {
			t.Fatalf("Expected at least 2 workspaces, got %d", len(workspaces))
		}

		// Find workspaces for our sessions
		var ws1ID, ws2ID string
		for _, ws := range workspaces {
			for _, sess := range ws.Sessions {
				if sess.ID == session1ID {
					ws1ID = ws.ID
				}
				if sess.ID == session2ID {
					ws2ID = ws.ID
				}
			}
		}

		if ws1ID == "" {
			t.Error("Could not find workspace for first session")
		}
		if ws2ID == "" {
			t.Error("Could not find workspace for second session")
		}
		if ws1ID == ws2ID {
			t.Errorf("Both sessions are in the same workspace %s - expected different workspaces", ws1ID)
		}

		t.Logf("Session 1 in workspace: %s", ws1ID)
		t.Logf("Session 2 in workspace: %s", ws2ID)
	})

	t.Run("VerifyBothOnMainBranch", func(t *testing.T) {
		workspaces := env.GetAPIWorkspaces()

		for _, ws := range workspaces {
			hasOurSession := false
			for _, sess := range ws.Sessions {
				if sess.ID == session1ID || sess.ID == session2ID {
					hasOurSession = true
					break
				}
			}
			if hasOurSession && ws.Branch != "main" {
				t.Errorf("Workspace %s has branch %s, expected main", ws.ID, ws.Branch)
			}
		}
	})
}

// TestE2EBranchConflictCheck tests the /api/check-branch-conflict endpoint
// which is used by the UI to validate branches in worktree mode.
func TestE2EBranchConflictCheck(t *testing.T) {
	env := New(t)

	workspaceRoot := t.TempDir()

	t.Run("CreateConfig", func(t *testing.T) {
		env.CreateConfig(workspaceRoot)
	})

	t.Run("CreateGitRepo", func(t *testing.T) {
		repoPath := workspaceRoot + "/conflict-test-repo"
		if err := os.MkdirAll(repoPath, 0755); err != nil {
			t.Fatalf("Failed to create repo dir: %v", err)
		}

		RunCmd(t, repoPath, "git", "init", "-b", "main")
		RunCmd(t, repoPath, "git", "config", "user.email", "e2e@test.local")
		RunCmd(t, repoPath, "git", "config", "user.name", "E2E Test")

		testFile := filepath.Join(repoPath, "README.md")
		if err := os.WriteFile(testFile, []byte("# Conflict Test\n"), 0644); err != nil {
			t.Fatalf("Failed to create test file: %v", err)
		}

		RunCmd(t, repoPath, "git", "add", ".")
		RunCmd(t, repoPath, "git", "commit", "-m", "Initial commit")

		env.AddRepoToConfig("conflict-test-repo", "file://"+repoPath)
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

	repoURL := "file://" + workspaceRoot + "/conflict-test-repo"

	t.Run("CheckNoConflictInitially", func(t *testing.T) {
		result := env.CheckBranchConflict(repoURL, "main")
		if result.Conflict {
			t.Errorf("Expected no conflict initially, got conflict with workspace %s", result.WorkspaceID)
		}
	})

	var sessionID string
	t.Run("SpawnSession", func(t *testing.T) {
		sessionID = env.SpawnSession(repoURL, "main", "echo", "", "test-agent")
		if sessionID == "" {
			t.Fatal("Expected session ID from spawn")
		}
	})

	t.Run("CheckConflictAfterSpawn", func(t *testing.T) {
		result := env.CheckBranchConflict(repoURL, "main")
		if !result.Conflict {
			t.Error("Expected conflict after spawning on same branch")
		}
		if result.WorkspaceID == "" {
			t.Error("Expected workspace ID in conflict response")
		}
		t.Logf("Conflict detected with workspace: %s", result.WorkspaceID)
	})

	t.Run("CheckNoConflictDifferentBranch", func(t *testing.T) {
		result := env.CheckBranchConflict(repoURL, "feature/new-branch")
		if result.Conflict {
			t.Errorf("Expected no conflict for different branch, got conflict with workspace %s", result.WorkspaceID)
		}
	})
}

// TestE2EDashboardWebSocket validates the /ws/dashboard websocket endpoint.
func TestE2EDashboardWebSocket(t *testing.T) {
	env := New(t)

	workspaceRoot := t.TempDir()

	t.Run("CreateConfig", func(t *testing.T) {
		env.CreateConfig(workspaceRoot)
	})

	t.Run("CreateGitRepo", func(t *testing.T) {
		repoPath := workspaceRoot + "/ws-dashboard-repo"
		if err := os.MkdirAll(repoPath, 0755); err != nil {
			t.Fatalf("Failed to create repo dir: %v", err)
		}

		RunCmd(t, repoPath, "git", "init", "-b", "main")
		RunCmd(t, repoPath, "git", "config", "user.email", "e2e@test.local")
		RunCmd(t, repoPath, "git", "config", "user.name", "E2E Test")

		testFile := filepath.Join(repoPath, "README.md")
		if err := os.WriteFile(testFile, []byte("# Dashboard WS Test\n"), 0644); err != nil {
			t.Fatalf("Failed to create test file: %v", err)
		}

		RunCmd(t, repoPath, "git", "add", ".")
		RunCmd(t, repoPath, "git", "commit", "-m", "Initial commit")

		env.AddRepoToConfig("ws-dashboard-repo", "file://"+repoPath)
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

	var conn *websocket.Conn
	t.Run("ConnectWebSocket", func(t *testing.T) {
		var err error
		conn, err = env.ConnectDashboardWebSocket()
		if err != nil {
			t.Fatalf("Failed to connect to dashboard websocket: %v", err)
		}
	})

	defer func() {
		if conn != nil {
			conn.Close()
		}
	}()

	t.Run("ReceiveInitialState", func(t *testing.T) {
		msg, err := env.ReadDashboardMessage(conn, 3*time.Second)
		if err != nil {
			t.Fatalf("Failed to read initial message: %v", err)
		}
		if msg.Type != "sessions" {
			t.Fatalf("Expected message type 'sessions', got %q", msg.Type)
		}
		t.Logf("Initial state: %d workspaces", len(msg.Workspaces))

		// Wait for debounce window to pass (server debounces broadcasts at 500ms)
		// The git status goroutine broadcasts on startup, and we need to wait
		// for that debounce window to close before spawning
		time.Sleep(600 * time.Millisecond)
	})

	var sessionID string
	t.Run("SpawnAndReceiveUpdate", func(t *testing.T) {
		sessionID = env.SpawnSession("file://"+workspaceRoot+"/ws-dashboard-repo", "main", "echo", "", "ws-dash-test")
		if sessionID == "" {
			t.Fatal("Expected session ID from spawn")
		}
		t.Logf("Spawned session: %s", sessionID)

		// Wait for a message that contains our session
		// The spawn handler broadcasts after success, so we should receive it
		msg, err := env.WaitForDashboardSession(conn, sessionID, 5*time.Second)
		if err != nil {
			t.Fatalf("Failed to receive session via websocket: %v", err)
		}
		t.Logf("Received update with session, workspaces: %d", len(msg.Workspaces))
	})

	t.Run("DisposeAndReceiveUpdate", func(t *testing.T) {
		// Wait for debounce window to pass from spawn broadcast
		time.Sleep(600 * time.Millisecond)
		env.DisposeSession(sessionID)

		// Wait for the session to disappear via websocket
		msg, err := env.WaitForDashboardSessionGone(conn, sessionID, 5*time.Second)
		if err != nil {
			t.Fatalf("Failed to receive update after dispose: %v", err)
		}
		t.Logf("Received update without session, workspaces: %d", len(msg.Workspaces))
	})
}
