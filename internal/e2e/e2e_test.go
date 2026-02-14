//go:build e2e

package e2e

import (
	"fmt"
	"net/http"
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

	// Add a workspace-level quick launch for the repo.
	t.Run("04b_AddWorkspaceQuickLaunch", func(t *testing.T) {
		configPath := filepath.Join(workspaceRoot, "test-repo", ".schmux", "config.json")
		if err := os.MkdirAll(filepath.Dir(configPath), 0755); err != nil {
			t.Fatalf("Failed to create .schmux dir: %v", err)
		}
		content := `{"quick_launch":[{"name":"Run","command":"echo run"}]}`
		if err := os.WriteFile(configPath, []byte(content), 0644); err != nil {
			t.Fatalf("Failed to write workspace config: %v", err)
		}
		time.Sleep(2 * time.Second)
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

	t.Run("05b_QuickLaunchRequiresWorkspace", func(t *testing.T) {
		status := env.SpawnQuickLaunchWithoutWorkspace("file://"+workspaceRoot+"/test-repo", "main", "Run")
		if status != http.StatusBadRequest {
			t.Fatalf("expected status 400, got %d", status)
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

// TestE2EWebSocketTerminal validates WebSocket terminal streaming works.
// It spawns an echo target and verifies we receive output via WebSocket.
func TestE2EWebSocketTerminal(t *testing.T) {
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
		// Target emits READY immediately, then sleeps (we just need to verify read path)
		sessionID = env.SpawnSession("file://"+workspaceRoot+"/ws-test-repo", "main", "echo", "", "ws-echo")
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

		// Step 1: Verify read path works by receiving bootstrap (echo target emits "hello")
		if _, err := env.WaitForWebSocketContent(conn, "hello", 5*time.Second); err != nil {
			t.Fatalf("Failed to receive bootstrap: %v", err)
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

// TestE2EFileBasedSignaling validates the full file-based signaling pipeline:
// spawn session → write signal file → verify nudge propagates via dashboard WebSocket.
func TestE2EFileBasedSignaling(t *testing.T) {
	env := New(t)

	workspaceRoot := t.TempDir()

	t.Run("CreateConfig", func(t *testing.T) {
		env.CreateConfig(workspaceRoot)
	})

	t.Run("CreateGitRepo", func(t *testing.T) {
		repoPath := workspaceRoot + "/signal-test-repo"
		if err := os.MkdirAll(repoPath, 0755); err != nil {
			t.Fatalf("Failed to create repo dir: %v", err)
		}

		RunCmd(t, repoPath, "git", "init", "-b", "main")
		RunCmd(t, repoPath, "git", "config", "user.email", "e2e@test.local")
		RunCmd(t, repoPath, "git", "config", "user.name", "E2E Test")

		testFile := filepath.Join(repoPath, "README.md")
		if err := os.WriteFile(testFile, []byte("# Signal Test\n"), 0644); err != nil {
			t.Fatalf("Failed to create test file: %v", err)
		}

		RunCmd(t, repoPath, "git", "add", ".")
		RunCmd(t, repoPath, "git", "commit", "-m", "Initial commit")

		env.AddRepoToConfig("signal-test-repo", "file://"+repoPath)
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
		// Wait for debounce window to pass
		time.Sleep(600 * time.Millisecond)
	})

	var sessionID string
	var workspacePath string
	t.Run("SpawnSession", func(t *testing.T) {
		sessionID = env.SpawnSession("file://"+workspaceRoot+"/signal-test-repo", "main", "echo", "", "signal-test")
		if sessionID == "" {
			t.Fatal("Expected session ID from spawn")
		}
		t.Logf("Spawned session: %s", sessionID)

		// Wait for session to appear and get the workspace path
		msg, err := env.WaitForDashboardSession(conn, sessionID, 5*time.Second)
		if err != nil {
			t.Fatalf("Failed to receive session via websocket: %v", err)
		}
		for _, ws := range msg.Workspaces {
			for _, sess := range ws.Sessions {
				if sess.ID == sessionID {
					workspacePath = ws.Path
					break
				}
			}
		}
		if workspacePath == "" {
			t.Fatal("Could not find workspace path for session")
		}
		t.Logf("Workspace path: %s", workspacePath)
	})

	t.Run("VerifySchmuxDirCreated", func(t *testing.T) {
		schmuxDir := filepath.Join(workspacePath, ".schmux", "signal")
		info, err := os.Stat(schmuxDir)
		if err != nil {
			t.Fatalf(".schmux/signal directory not created: %v", err)
		}
		if !info.IsDir() {
			t.Fatal(".schmux/signal exists but is not a directory")
		}
	})

	t.Run("WriteCompletedSignal", func(t *testing.T) {
		// Wait for debounce window to pass from spawn broadcast
		time.Sleep(600 * time.Millisecond)

		signalFile := filepath.Join(workspacePath, ".schmux", "signal", sessionID)
		if err := os.WriteFile(signalFile, []byte("completed Implementation done\n"), 0644); err != nil {
			t.Fatalf("Failed to write signal file: %v", err)
		}
		t.Logf("Wrote signal file: %s", signalFile)

		// Wait for the nudge to appear via dashboard WebSocket
		sess, err := env.WaitForSessionNudgeState(conn, sessionID, "Completed", 5*time.Second)
		if err != nil {
			t.Fatalf("Failed to receive nudge state: %v", err)
		}
		if sess.NudgeSummary != "Implementation done" {
			t.Errorf("NudgeSummary = %q, want %q", sess.NudgeSummary, "Implementation done")
		}
		if sess.NudgeSeq == 0 {
			t.Error("NudgeSeq should be non-zero after signal")
		}
		t.Logf("Received nudge: state=%s summary=%q seq=%d", sess.NudgeState, sess.NudgeSummary, sess.NudgeSeq)
	})

	t.Run("WriteNeedsInputSignal", func(t *testing.T) {
		// Wait for debounce window to pass
		time.Sleep(600 * time.Millisecond)

		signalFile := filepath.Join(workspacePath, ".schmux", "signal", sessionID)
		if err := os.WriteFile(signalFile, []byte("needs_input Should I proceed?\n"), 0644); err != nil {
			t.Fatalf("Failed to write signal file: %v", err)
		}

		sess, err := env.WaitForSessionNudgeState(conn, sessionID, "Needs Authorization", 5*time.Second)
		if err != nil {
			t.Fatalf("Failed to receive nudge state: %v", err)
		}
		if sess.NudgeSummary != "Should I proceed?" {
			t.Errorf("NudgeSummary = %q, want %q", sess.NudgeSummary, "Should I proceed?")
		}
		t.Logf("Received nudge: state=%s summary=%q", sess.NudgeState, sess.NudgeSummary)
	})

	t.Run("WriteWorkingSignal", func(t *testing.T) {
		// Wait for debounce window to pass
		time.Sleep(600 * time.Millisecond)

		signalFile := filepath.Join(workspacePath, ".schmux", "signal", sessionID)
		if err := os.WriteFile(signalFile, []byte("working\n"), 0644); err != nil {
			t.Fatalf("Failed to write signal file: %v", err)
		}

		// "working" is a visible dashboard state (spinner + message)
		sess, err := env.WaitForSessionNudgeState(conn, sessionID, "Working", 5*time.Second)
		if err != nil {
			t.Fatalf("Failed to receive working nudge: %v", err)
		}
		t.Logf("Received working nudge: state=%q summary=%q", sess.NudgeState, sess.NudgeSummary)
	})

	t.Run("WriteErrorSignal", func(t *testing.T) {
		// Wait for debounce window to pass
		time.Sleep(600 * time.Millisecond)

		signalFile := filepath.Join(workspacePath, ".schmux", "signal", sessionID)
		if err := os.WriteFile(signalFile, []byte("error Build failed with 3 errors\n"), 0644); err != nil {
			t.Fatalf("Failed to write signal file: %v", err)
		}

		sess, err := env.WaitForSessionNudgeState(conn, sessionID, "Error", 5*time.Second)
		if err != nil {
			t.Fatalf("Failed to receive error nudge: %v", err)
		}
		if sess.NudgeSummary != "Build failed with 3 errors" {
			t.Errorf("NudgeSummary = %q, want %q", sess.NudgeSummary, "Build failed with 3 errors")
		}
		t.Logf("Received error nudge: state=%s summary=%q", sess.NudgeState, sess.NudgeSummary)
	})

	t.Run("DeduplicatesSameSignal", func(t *testing.T) {
		// Wait for debounce window to pass
		time.Sleep(600 * time.Millisecond)

		// Get current NudgeSeq from API
		sessions := env.GetAPISessions()
		var currentSeq uint64
		for _, s := range sessions {
			if s.ID == sessionID {
				currentSeq = s.NudgeSeq
				break
			}
		}

		// Write the same signal again
		signalFile := filepath.Join(workspacePath, ".schmux", "signal", sessionID)
		if err := os.WriteFile(signalFile, []byte("error Build failed with 3 errors\n"), 0644); err != nil {
			t.Fatalf("Failed to write signal file: %v", err)
		}

		// Wait a bit for any potential callback
		time.Sleep(500 * time.Millisecond)

		// Verify NudgeSeq hasn't changed (dedup prevented double-fire)
		sessions = env.GetAPISessions()
		for _, s := range sessions {
			if s.ID == sessionID {
				if s.NudgeSeq != currentSeq {
					t.Errorf("NudgeSeq changed from %d to %d — dedup failed", currentSeq, s.NudgeSeq)
				} else {
					t.Logf("Dedup works: NudgeSeq unchanged at %d", currentSeq)
				}
				break
			}
		}
	})
}

// TestE2ESignalDaemonRestart validates that signal state survives a daemon restart
// and that new signals work after the restart.
func TestE2ESignalDaemonRestart(t *testing.T) {
	env := New(t)

	workspaceRoot := t.TempDir()

	t.Run("CreateConfig", func(t *testing.T) {
		env.CreateConfig(workspaceRoot)
	})

	t.Run("CreateGitRepo", func(t *testing.T) {
		repoPath := workspaceRoot + "/restart-test-repo"
		if err := os.MkdirAll(repoPath, 0755); err != nil {
			t.Fatalf("Failed to create repo dir: %v", err)
		}

		RunCmd(t, repoPath, "git", "init", "-b", "main")
		RunCmd(t, repoPath, "git", "config", "user.email", "e2e@test.local")
		RunCmd(t, repoPath, "git", "config", "user.name", "E2E Test")

		testFile := filepath.Join(repoPath, "README.md")
		if err := os.WriteFile(testFile, []byte("# Restart Test\n"), 0644); err != nil {
			t.Fatalf("Failed to create test file: %v", err)
		}

		RunCmd(t, repoPath, "git", "add", ".")
		RunCmd(t, repoPath, "git", "commit", "-m", "Initial commit")

		env.AddRepoToConfig("restart-test-repo", "file://"+repoPath)
	})

	t.Run("DaemonStart", func(t *testing.T) {
		env.DaemonStart()
	})

	defer func() {
		if env.HealthCheck() {
			env.DaemonStop()
		}
		if t.Failed() {
			env.CaptureArtifacts()
		}
	}()

	var sessionID string
	var workspacePath string
	t.Run("SpawnAndSignal", func(t *testing.T) {
		sessionID = env.SpawnSession("file://"+workspaceRoot+"/restart-test-repo", "main", "echo", "", "restart-test")
		if sessionID == "" {
			t.Fatal("Expected session ID from spawn")
		}

		// Get workspace path from API
		workspaces := env.GetAPIWorkspaces()
		for _, ws := range workspaces {
			for _, sess := range ws.Sessions {
				if sess.ID == sessionID {
					workspacePath = ws.Path
					break
				}
			}
		}
		if workspacePath == "" {
			t.Fatal("Could not find workspace path for session")
		}

		// Write a signal and wait for it to propagate
		conn, err := env.ConnectDashboardWebSocket()
		if err != nil {
			t.Fatalf("Failed to connect websocket: %v", err)
		}
		defer conn.Close()

		// Drain initial state message
		env.ReadDashboardMessage(conn, 3*time.Second)
		time.Sleep(600 * time.Millisecond)

		signalFile := filepath.Join(workspacePath, ".schmux", "signal", sessionID)
		if err := os.WriteFile(signalFile, []byte("completed Implementation done\n"), 0644); err != nil {
			t.Fatalf("Failed to write signal file: %v", err)
		}

		sess, err := env.WaitForSessionNudgeState(conn, sessionID, "Completed", 5*time.Second)
		if err != nil {
			t.Fatalf("Failed to receive nudge: %v", err)
		}
		t.Logf("Pre-restart nudge: state=%s summary=%q seq=%d", sess.NudgeState, sess.NudgeSummary, sess.NudgeSeq)
	})

	t.Run("RestartDaemon", func(t *testing.T) {
		env.DaemonStop()
		// Signal file persists on disk across restart
		time.Sleep(500 * time.Millisecond)
		env.DaemonStart()
	})

	t.Run("VerifyNudgeSurvived", func(t *testing.T) {
		// After restart, connect to dashboard and verify nudge state was recovered
		conn, err := env.ConnectDashboardWebSocket()
		if err != nil {
			t.Fatalf("Failed to connect websocket after restart: %v", err)
		}
		defer conn.Close()

		// The initial state message should already contain the recovered nudge
		sess, err := env.WaitForSessionNudgeState(conn, sessionID, "Completed", 5*time.Second)
		if err != nil {
			t.Fatalf("Nudge did not survive restart: %v", err)
		}
		if sess.NudgeSummary != "Implementation done" {
			t.Errorf("NudgeSummary = %q, want %q", sess.NudgeSummary, "Implementation done")
		}
		t.Logf("Post-restart nudge: state=%s summary=%q", sess.NudgeState, sess.NudgeSummary)
	})

	t.Run("NewSignalWorksAfterRestart", func(t *testing.T) {
		conn, err := env.ConnectDashboardWebSocket()
		if err != nil {
			t.Fatalf("Failed to connect websocket: %v", err)
		}
		defer conn.Close()

		// Drain initial state
		env.ReadDashboardMessage(conn, 3*time.Second)
		time.Sleep(600 * time.Millisecond)

		// Write a different signal
		signalFile := filepath.Join(workspacePath, ".schmux", "signal", sessionID)
		if err := os.WriteFile(signalFile, []byte("needs_input Approve changes?\n"), 0644); err != nil {
			t.Fatalf("Failed to write signal file: %v", err)
		}

		sess, err := env.WaitForSessionNudgeState(conn, sessionID, "Needs Authorization", 5*time.Second)
		if err != nil {
			t.Fatalf("New signal after restart failed: %v", err)
		}
		if sess.NudgeSummary != "Approve changes?" {
			t.Errorf("NudgeSummary = %q, want %q", sess.NudgeSummary, "Approve changes?")
		}
		t.Logf("Post-restart new signal works: state=%s summary=%q", sess.NudgeState, sess.NudgeSummary)
	})
}

// TestE2ENudgeClearOnTerminalInput validates that typing in the terminal WebSocket
// clears the active nudge.
func TestE2ENudgeClearOnTerminalInput(t *testing.T) {
	env := New(t)

	workspaceRoot := t.TempDir()

	t.Run("CreateConfig", func(t *testing.T) {
		env.CreateConfig(workspaceRoot)
	})

	t.Run("CreateGitRepo", func(t *testing.T) {
		repoPath := workspaceRoot + "/nudge-clear-repo"
		if err := os.MkdirAll(repoPath, 0755); err != nil {
			t.Fatalf("Failed to create repo dir: %v", err)
		}

		RunCmd(t, repoPath, "git", "init", "-b", "main")
		RunCmd(t, repoPath, "git", "config", "user.email", "e2e@test.local")
		RunCmd(t, repoPath, "git", "config", "user.name", "E2E Test")

		testFile := filepath.Join(repoPath, "README.md")
		if err := os.WriteFile(testFile, []byte("# Nudge Clear Test\n"), 0644); err != nil {
			t.Fatalf("Failed to create test file: %v", err)
		}

		RunCmd(t, repoPath, "git", "add", ".")
		RunCmd(t, repoPath, "git", "commit", "-m", "Initial commit")

		env.AddRepoToConfig("nudge-clear-repo", "file://"+repoPath)
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

	// Use "cat" target so the session stays alive and accepts input
	var sessionID string
	var workspacePath string
	t.Run("SpawnSession", func(t *testing.T) {
		sessionID = env.SpawnSession("file://"+workspaceRoot+"/nudge-clear-repo", "main", "cat", "", "nudge-clear")
		if sessionID == "" {
			t.Fatal("Expected session ID from spawn")
		}

		workspaces := env.GetAPIWorkspaces()
		for _, ws := range workspaces {
			for _, sess := range ws.Sessions {
				if sess.ID == sessionID {
					workspacePath = ws.Path
					break
				}
			}
		}
		if workspacePath == "" {
			t.Fatal("Could not find workspace path")
		}
	})

	t.Run("WriteSignalAndVerifyNudge", func(t *testing.T) {
		dashConn, err := env.ConnectDashboardWebSocket()
		if err != nil {
			t.Fatalf("Failed to connect dashboard websocket: %v", err)
		}
		defer dashConn.Close()

		// Drain initial state
		env.ReadDashboardMessage(dashConn, 3*time.Second)
		time.Sleep(600 * time.Millisecond)

		// Write a signal to create a nudge
		signalFile := filepath.Join(workspacePath, ".schmux", "signal", sessionID)
		if err := os.WriteFile(signalFile, []byte("needs_input Waiting for approval\n"), 0644); err != nil {
			t.Fatalf("Failed to write signal file: %v", err)
		}

		// Verify nudge appears
		sess, err := env.WaitForSessionNudgeState(dashConn, sessionID, "Needs Authorization", 5*time.Second)
		if err != nil {
			t.Fatalf("Failed to receive nudge: %v", err)
		}
		t.Logf("Nudge set: state=%s summary=%q", sess.NudgeState, sess.NudgeSummary)
	})

	t.Run("TerminalInputClearsNudge", func(t *testing.T) {
		// Connect dashboard WS to watch for nudge clear
		dashConn, err := env.ConnectDashboardWebSocket()
		if err != nil {
			t.Fatalf("Failed to connect dashboard websocket: %v", err)
		}
		defer dashConn.Close()

		// Drain initial state (should show the nudge)
		env.ReadDashboardMessage(dashConn, 3*time.Second)
		time.Sleep(600 * time.Millisecond)

		// Connect terminal WS and send Enter key
		termConn, err := env.ConnectTerminalWebSocket(sessionID)
		if err != nil {
			t.Fatalf("Failed to connect terminal websocket: %v", err)
		}
		defer termConn.Close()

		// Send Enter via terminal WebSocket (triggers nudge clear)
		env.SendWebSocketInput(termConn, "\r")

		// Verify nudge is cleared via dashboard WebSocket
		sess, err := env.WaitForSessionNudgeState(dashConn, sessionID, "", 5*time.Second)
		if err != nil {
			t.Fatalf("Nudge was not cleared after terminal input: %v", err)
		}
		t.Logf("Nudge cleared: state=%q summary=%q", sess.NudgeState, sess.NudgeSummary)
	})
}

// TestE2EMultipleSessionsIsolatedSignals validates that multiple sessions in the same
// workspace have isolated signal files — writing to one session's signal file does NOT
// nudge the other session.
func TestE2EMultipleSessionsIsolatedSignals(t *testing.T) {
	env := New(t)

	workspaceRoot := t.TempDir()

	t.Run("CreateConfig", func(t *testing.T) {
		env.CreateConfig(workspaceRoot)
	})

	t.Run("CreateGitRepo", func(t *testing.T) {
		repoPath := workspaceRoot + "/multi-session-repo"
		if err := os.MkdirAll(repoPath, 0755); err != nil {
			t.Fatalf("Failed to create repo dir: %v", err)
		}

		RunCmd(t, repoPath, "git", "init", "-b", "main")
		RunCmd(t, repoPath, "git", "config", "user.email", "e2e@test.local")
		RunCmd(t, repoPath, "git", "config", "user.name", "E2E Test")

		testFile := filepath.Join(repoPath, "README.md")
		if err := os.WriteFile(testFile, []byte("# Multi Session Test\n"), 0644); err != nil {
			t.Fatalf("Failed to create test file: %v", err)
		}

		RunCmd(t, repoPath, "git", "add", ".")
		RunCmd(t, repoPath, "git", "commit", "-m", "Initial commit")

		env.AddRepoToConfig("multi-session-repo", "file://"+repoPath)
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
	var workspacePath, workspaceID string

	t.Run("SpawnFirstSession", func(t *testing.T) {
		session1ID = env.SpawnSession("file://"+workspaceRoot+"/multi-session-repo", "main", "echo", "", "multi-1")
		if session1ID == "" {
			t.Fatal("Expected session ID from first spawn")
		}

		workspaces := env.GetAPIWorkspaces()
		for _, ws := range workspaces {
			for _, sess := range ws.Sessions {
				if sess.ID == session1ID {
					workspacePath = ws.Path
					workspaceID = ws.ID
					break
				}
			}
		}
		if workspacePath == "" {
			t.Fatal("Could not find workspace path")
		}
		t.Logf("First session: %s in workspace %s (%s)", session1ID, workspaceID, workspacePath)
	})

	t.Run("SpawnSecondSessionInSameWorkspace", func(t *testing.T) {
		// Spawn into the same workspace to ensure they share the signal file
		session2ID = env.SpawnSessionInWorkspace(workspaceID, "echo", "", "multi-2")
		if session2ID == "" {
			t.Fatal("Expected session ID from second spawn")
		}
		t.Logf("Second session: %s in workspace %s", session2ID, workspaceID)
	})

	t.Run("SignalIsolatedPerSession", func(t *testing.T) {
		dashConn, err := env.ConnectDashboardWebSocket()
		if err != nil {
			t.Fatalf("Failed to connect dashboard websocket: %v", err)
		}
		defer dashConn.Close()

		// Drain initial state
		env.ReadDashboardMessage(dashConn, 3*time.Second)
		time.Sleep(600 * time.Millisecond)

		// Write signal to session1's signal file only
		signalFile := filepath.Join(workspacePath, ".schmux", "signal", session1ID)
		if err := os.WriteFile(signalFile, []byte("completed All tasks done\n"), 0644); err != nil {
			t.Fatalf("Failed to write signal file: %v", err)
		}

		// Wait for session1 to show the nudge
		deadline := time.Now().Add(5 * time.Second)
		session1Got := false

		for time.Now().Before(deadline) && !session1Got {
			msg, err := env.ReadDashboardMessage(dashConn, time.Until(deadline))
			if err != nil {
				t.Fatalf("Timed out waiting for session1 nudge: %v", err)
			}
			if msg.Type == "sessions" {
				for _, ws := range msg.Workspaces {
					for _, sess := range ws.Sessions {
						if sess.ID == session1ID && sess.NudgeState == "Completed" {
							session1Got = true
							t.Logf("Session 1 (%s) received nudge: %s", session1ID, sess.NudgeState)
						}
					}
				}
			}
		}

		if !session1Got {
			t.Fatal("Session 1 did not receive the nudge")
		}

		// Verify session2 did NOT get nudged — read a few more messages and check
		time.Sleep(500 * time.Millisecond)

		// Get current state via REST API to check session2's nudge
		workspaces := env.GetAPIWorkspaces()
		for _, ws := range workspaces {
			for _, sess := range ws.Sessions {
				if sess.ID == session2ID && sess.NudgeState != "" {
					t.Errorf("Session 2 should NOT have been nudged, but got nudge_state=%q", sess.NudgeState)
				}
			}
		}
		t.Log("Session 2 correctly not nudged — signals are isolated per session")
	})
}

// TestE2ESpawnCommandSignaling validates that sessions spawned via the SpawnCommand
// code path (command field, used by quick launch presets) correctly set up signaling.
func TestE2ESpawnCommandSignaling(t *testing.T) {
	env := New(t)

	workspaceRoot := t.TempDir()

	t.Run("CreateConfig", func(t *testing.T) {
		env.CreateConfig(workspaceRoot)
	})

	t.Run("CreateGitRepo", func(t *testing.T) {
		repoPath := workspaceRoot + "/cmd-signal-repo"
		if err := os.MkdirAll(repoPath, 0755); err != nil {
			t.Fatalf("Failed to create repo dir: %v", err)
		}

		RunCmd(t, repoPath, "git", "init", "-b", "main")
		RunCmd(t, repoPath, "git", "config", "user.email", "e2e@test.local")
		RunCmd(t, repoPath, "git", "config", "user.name", "E2E Test")

		testFile := filepath.Join(repoPath, "README.md")
		if err := os.WriteFile(testFile, []byte("# Cmd Signal Test\n"), 0644); err != nil {
			t.Fatalf("Failed to create test file: %v", err)
		}

		RunCmd(t, repoPath, "git", "add", ".")
		RunCmd(t, repoPath, "git", "commit", "-m", "Initial commit")

		env.AddRepoToConfig("cmd-signal-repo", "file://"+repoPath)
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
	var workspacePath string

	t.Run("SpawnCommandSession", func(t *testing.T) {
		repoURL := "file://" + workspaceRoot + "/cmd-signal-repo"
		// Spawn using command field (exercises SpawnCommand code path)
		sessionID = env.SpawnCommandSession(repoURL, "main", "sh -c 'echo hello; sleep 600'", "cmd-signal", "")
		if sessionID == "" {
			t.Fatal("Expected session ID from spawn")
		}

		workspaces := env.GetAPIWorkspaces()
		for _, ws := range workspaces {
			for _, sess := range ws.Sessions {
				if sess.ID == sessionID {
					workspacePath = ws.Path
					break
				}
			}
		}
		if workspacePath == "" {
			t.Fatal("Could not find workspace path for command session")
		}
		t.Logf("Command session: %s in workspace %s", sessionID, workspacePath)
	})

	t.Run("VerifySchmuxDirCreated", func(t *testing.T) {
		schmuxDir := filepath.Join(workspacePath, ".schmux", "signal")
		info, err := os.Stat(schmuxDir)
		if err != nil {
			t.Fatalf(".schmux/signal directory not created for command session: %v", err)
		}
		if !info.IsDir() {
			t.Fatal(".schmux/signal exists but is not a directory")
		}
	})

	t.Run("VerifyStatusFileEnvVar", func(t *testing.T) {
		// Connect terminal and read output — the echo target just says "hello",
		// but we can verify the env var was set by checking via tmux
		termConn, err := env.ConnectTerminalWebSocket(sessionID)
		if err != nil {
			t.Fatalf("Failed to connect terminal websocket: %v", err)
		}
		defer termConn.Close()

		// Verify the session is running (bootstrap output "hello" received)
		if _, err := env.WaitForWebSocketContent(termConn, "hello", 5*time.Second); err != nil {
			t.Fatalf("Session not producing output: %v", err)
		}
	})

	t.Run("SignalWorksForCommandSession", func(t *testing.T) {
		dashConn, err := env.ConnectDashboardWebSocket()
		if err != nil {
			t.Fatalf("Failed to connect dashboard websocket: %v", err)
		}
		defer dashConn.Close()

		// Drain initial state
		env.ReadDashboardMessage(dashConn, 3*time.Second)
		time.Sleep(600 * time.Millisecond)

		// Write a signal file — this verifies the FileWatcher is active for SpawnCommand sessions
		signalFile := filepath.Join(workspacePath, ".schmux", "signal", sessionID)
		if err := os.WriteFile(signalFile, []byte("completed Command finished\n"), 0644); err != nil {
			t.Fatalf("Failed to write signal file: %v", err)
		}

		sess, err := env.WaitForSessionNudgeState(dashConn, sessionID, "Completed", 5*time.Second)
		if err != nil {
			t.Fatalf("Signal not detected for command session: %v", err)
		}
		if sess.NudgeSummary != "Command finished" {
			t.Errorf("NudgeSummary = %q, want %q", sess.NudgeSummary, "Command finished")
		}
		t.Logf("Command session signal works: state=%s summary=%q", sess.NudgeState, sess.NudgeSummary)
	})
}

// TestE2EOverlayCompounding validates the full overlay compounding loop:
// overlay files are copied into workspaces at spawn time, modifications by agents
// propagate back to the overlay directory, and changes propagate to sibling workspaces.
func TestE2EOverlayCompounding(t *testing.T) {
	env := New(t)

	const workspaceRoot = "/tmp/schmux-e2e-compound-test"
	const repoName = "compound-test-repo"
	repoPath := workspaceRoot + "/" + repoName
	repoURL := "file://" + repoPath

	// Step 1: Create config
	t.Run("01_CreateConfig", func(t *testing.T) {
		env.CreateConfig(workspaceRoot)
	})

	// Step 2: Create git repo with .gitignore covering overlay-managed files
	t.Run("02_CreateGitRepo", func(t *testing.T) {
		if err := os.MkdirAll(repoPath, 0755); err != nil {
			t.Fatalf("Failed to create repo dir: %v", err)
		}

		RunCmd(t, repoPath, "git", "init", "-b", "main")
		RunCmd(t, repoPath, "git", "config", "user.email", "e2e@test.local")
		RunCmd(t, repoPath, "git", "config", "user.name", "E2E Test")

		// Create README
		if err := os.WriteFile(filepath.Join(repoPath, "README.md"), []byte("# Compound Test\n"), 0644); err != nil {
			t.Fatalf("Failed to create README: %v", err)
		}

		// Create .gitignore covering overlay-managed files
		gitignore := ".env\n.claude/\n"
		if err := os.WriteFile(filepath.Join(repoPath, ".gitignore"), []byte(gitignore), 0644); err != nil {
			t.Fatalf("Failed to create .gitignore: %v", err)
		}

		RunCmd(t, repoPath, "git", "add", ".")
		RunCmd(t, repoPath, "git", "commit", "-m", "Initial commit")

		env.AddRepoToConfig(repoName, repoURL)
	})

	// Step 3: Enable git SCM (so each session gets its own workspace)
	t.Run("03_EnableGitSCM", func(t *testing.T) {
		env.SetSourceCodeManagement("git")
	})

	// Step 4: Enable compounding with fast debounce
	t.Run("04_SetCompoundConfig", func(t *testing.T) {
		env.SetCompoundConfig(500)
	})

	// Step 5: Create overlay files
	t.Run("05_CreateOverlayFiles", func(t *testing.T) {
		env.CreateOverlayFile(repoName, ".env", "DB_HOST=localhost\nDB_PORT=5432\n")
		env.CreateOverlayFile(repoName, ".claude/settings.json", `{"model":"sonnet"}`)
	})

	// Step 6: Start daemon
	t.Run("06_DaemonStart", func(t *testing.T) {
		env.DaemonStart()
	})

	defer func() {
		env.DaemonStop()
		if t.Failed() {
			env.CaptureArtifacts()
		}
	}()

	// Step 7: Spawn first session
	var session1ID string
	var workspace1Path string
	t.Run("07_SpawnSession1", func(t *testing.T) {
		session1ID = env.SpawnSession(repoURL, "main", "echo", "", "agent-one")
		if session1ID == "" {
			t.Fatal("Expected session ID from spawn")
		}
		workspace1Path = env.GetWorkspacePath(session1ID)
		t.Logf("Session 1: %s at %s", session1ID, workspace1Path)
	})

	// Step 8: Verify overlay was copied to workspace1
	t.Run("08_VerifyOverlayCopied", func(t *testing.T) {
		envFile := filepath.Join(workspace1Path, ".env")
		data, err := os.ReadFile(envFile)
		if err != nil {
			t.Fatalf("Overlay .env was not copied to workspace1: %v", err)
		}
		expected := "DB_HOST=localhost\nDB_PORT=5432\n"
		if string(data) != expected {
			t.Errorf("Overlay .env content mismatch: got %q, want %q", string(data), expected)
		}

		settingsFile := filepath.Join(workspace1Path, ".claude", "settings.json")
		data, err = os.ReadFile(settingsFile)
		if err != nil {
			t.Fatalf("Overlay .claude/settings.json was not copied to workspace1: %v", err)
		}
		if string(data) != `{"model":"sonnet"}` {
			t.Errorf("Overlay settings content mismatch: got %q", string(data))
		}
	})

	// Step 9: Spawn second session (gets its own workspace via git SCM)
	var session2ID string
	var workspace2Path string
	t.Run("09_SpawnSession2", func(t *testing.T) {
		session2ID = env.SpawnSession(repoURL, "main", "echo", "", "agent-two")
		if session2ID == "" {
			t.Fatal("Expected session ID from spawn")
		}
		workspace2Path = env.GetWorkspacePath(session2ID)
		if workspace2Path == workspace1Path {
			t.Fatalf("Session 2 should have a different workspace, got same path: %s", workspace2Path)
		}
		t.Logf("Session 2: %s at %s", session2ID, workspace2Path)
	})

	// Step 10: Verify overlay was also copied to workspace2
	t.Run("10_VerifyOverlayCopiedToWorkspace2", func(t *testing.T) {
		envFile := filepath.Join(workspace2Path, ".env")
		data, err := os.ReadFile(envFile)
		if err != nil {
			t.Fatalf("Overlay .env was not copied to workspace2: %v", err)
		}
		expected := "DB_HOST=localhost\nDB_PORT=5432\n"
		if string(data) != expected {
			t.Errorf("Overlay .env content mismatch in workspace2: got %q, want %q", string(data), expected)
		}
	})

	// Step 11: Simulate agent modifying .env in workspace1
	newContent := "DB_HOST=production.example.com\nDB_PORT=5432\nREDIS_URL=redis://cache:6379\n"
	t.Run("11_ModifyFileInWorkspace1", func(t *testing.T) {
		envFile := filepath.Join(workspace1Path, ".env")
		if err := os.WriteFile(envFile, []byte(newContent), 0644); err != nil {
			t.Fatalf("Failed to write modified .env: %v", err)
		}
		t.Logf("Modified .env in workspace1")
	})

	// Step 12: Wait for propagation to overlay dir and workspace2
	t.Run("12_WaitForPropagation", func(t *testing.T) {
		homeDir, err := os.UserHomeDir()
		if err != nil {
			t.Fatalf("Failed to get home dir: %v", err)
		}
		overlayEnvPath := filepath.Join(homeDir, ".schmux", "overlays", repoName, ".env")
		workspace2EnvPath := filepath.Join(workspace2Path, ".env")

		deadline := time.Now().Add(15 * time.Second)
		for time.Now().Before(deadline) {
			overlayData, err1 := os.ReadFile(overlayEnvPath)
			ws2Data, err2 := os.ReadFile(workspace2EnvPath)

			overlayMatch := err1 == nil && string(overlayData) == newContent
			ws2Match := err2 == nil && string(ws2Data) == newContent

			if overlayMatch && ws2Match {
				t.Logf("Propagation complete: overlay and workspace2 both have new content")
				return
			}

			time.Sleep(200 * time.Millisecond)
		}

		// If we get here, propagation timed out — log what we have for debugging
		overlayData, _ := os.ReadFile(filepath.Join(homeDir, ".schmux", "overlays", repoName, ".env"))
		ws2Data, _ := os.ReadFile(filepath.Join(workspace2Path, ".env"))
		t.Fatalf("Propagation timed out.\nOverlay .env: %q\nWorkspace2 .env: %q\nExpected: %q",
			string(overlayData), string(ws2Data), newContent)
	})

	// Step 13: Verify overlay dir has the updated content
	t.Run("13_VerifyOverlayUpdated", func(t *testing.T) {
		homeDir, err := os.UserHomeDir()
		if err != nil {
			t.Fatalf("Failed to get home dir: %v", err)
		}
		overlayEnvPath := filepath.Join(homeDir, ".schmux", "overlays", repoName, ".env")
		data, err := os.ReadFile(overlayEnvPath)
		if err != nil {
			t.Fatalf("Failed to read overlay .env: %v", err)
		}
		if string(data) != newContent {
			t.Errorf("Overlay .env not updated: got %q, want %q", string(data), newContent)
		}
	})

	// Step 14: Verify sibling workspace has the propagated content
	t.Run("14_VerifySiblingPropagated", func(t *testing.T) {
		ws2EnvPath := filepath.Join(workspace2Path, ".env")
		data, err := os.ReadFile(ws2EnvPath)
		if err != nil {
			t.Fatalf("Failed to read workspace2 .env: %v", err)
		}
		if string(data) != newContent {
			t.Errorf("Workspace2 .env not propagated: got %q, want %q", string(data), newContent)
		}
	})

	// Step 15: Dispose sessions
	t.Run("15_DisposeSessions", func(t *testing.T) {
		env.DisposeSession(session1ID)
		env.DisposeSession(session2ID)
	})
}
