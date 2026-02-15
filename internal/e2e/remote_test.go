//go:build e2e

package e2e

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestE2ERemoteBasicLifecycle tests the basic remote session lifecycle using mock connection.
func TestE2ERemoteBasicLifecycle(t *testing.T) {
	env := New(t)

	const workspaceRoot = "/tmp/schmux-e2e-remote-basic"

	t.Run("CreateConfig", func(t *testing.T) {
		env.CreateConfig(workspaceRoot)
	})

	// Get absolute path to mock script
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("Failed to get working directory: %v", err)
	}
	mockScriptPath := filepath.Join(cwd, "..", "..", "test", "mock-remote.sh")
	t.Logf("Mock script path: %s", mockScriptPath)

	// Verify mock script exists
	if _, err := os.Stat(mockScriptPath); err != nil {
		t.Fatalf("Mock script not found at %s: %v", mockScriptPath, err)
	}

	var flavorID string
	t.Run("AddRemoteFlavor", func(t *testing.T) {
		flavorID = env.AddRemoteFlavorToConfig(
			"mock-remote",
			"Mock Remote (E2E Test)",
			"/tmp/test-workspace",
			mockScriptPath,
		)
		if flavorID == "" {
			t.Fatal("Expected flavor ID, got empty")
		}
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
	t.Run("SpawnRemoteSession", func(t *testing.T) {
		sessionID = env.SpawnRemoteSession(flavorID, "echo", "", "remote-test")
		if sessionID == "" {
			t.Fatal("Expected session ID from remote spawn")
		}
		t.Logf("Remote session spawned: %s", sessionID)
	})

	t.Run("WaitForConnection", func(t *testing.T) {
		// Wait for remote host to become connected
		host := env.WaitForRemoteHostStatus(flavorID, "connected", 15*time.Second)
		if host == nil {
			t.Fatal("Remote host did not connect")
		}
		t.Logf("Remote host connected: %s (hostname: %s)", host.ID, host.Hostname)

		// Verify hostname was parsed
		if host.Hostname == "" {
			t.Error("Expected hostname to be populated")
		}
		if !strings.Contains(host.Hostname, "mock-test-host") {
			t.Errorf("Expected hostname to contain 'mock-test-host', got: %s", host.Hostname)
		}
	})

	t.Run("WaitForSessionRunning", func(t *testing.T) {
		// Wait for session to be running
		sess := env.WaitForSessionRunning(sessionID, 10*time.Second)
		if sess == nil {
			t.Fatal("Session did not become running")
		}
		t.Logf("Session running: %s", sess.ID)
	})

	t.Run("VerifySessionInAPI", func(t *testing.T) {
		sessions := env.GetAPISessions()
		found := false
		for _, sess := range sessions {
			if sess.ID == sessionID {
				found = true
				if !sess.Running {
					t.Error("Session should be running")
				}
				if sess.Nickname != "remote-test" {
					t.Errorf("Expected nickname 'remote-test', got: %s", sess.Nickname)
				}
			}
		}
		if !found {
			t.Error("Session not found in API response")
		}
	})

	t.Run("DisposeSession", func(t *testing.T) {
		env.DisposeSession(sessionID)

		// Verify session is gone
		time.Sleep(500 * time.Millisecond)
		sessions := env.GetAPISessions()
		for _, sess := range sessions {
			if sess.ID == sessionID {
				t.Error("Session still exists after dispose")
			}
		}
	})
}

// TestE2ERemoteMultipleSessions tests multiple sessions on the same remote host.
func TestE2ERemoteMultipleSessions(t *testing.T) {
	env := New(t)

	const workspaceRoot = "/tmp/schmux-e2e-remote-multi"

	t.Run("CreateConfig", func(t *testing.T) {
		env.CreateConfig(workspaceRoot)
	})

	cwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("Failed to get working directory: %v", err)
	}
	mockScriptPath := filepath.Join(cwd, "..", "..", "test", "mock-remote.sh")

	var flavorID string
	t.Run("AddRemoteFlavor", func(t *testing.T) {
		flavorID = env.AddRemoteFlavorToConfig(
			"mock-remote-multi",
			"Mock Remote Multi (E2E Test)",
			"/tmp/test-workspace",
			mockScriptPath,
		)
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

	var session1ID, session2ID, session3ID string

	t.Run("SpawnFirstSession", func(t *testing.T) {
		session1ID = env.SpawnRemoteSession(flavorID, "echo", "", "agent-one")
		if session1ID == "" {
			t.Fatal("Expected session ID from first spawn")
		}

		// Wait for connection
		env.WaitForRemoteHostStatus(flavorID, "connected", 15*time.Second)
		env.WaitForSessionRunning(session1ID, 10*time.Second)
	})

	t.Run("SpawnSecondSession", func(t *testing.T) {
		// Second session should reuse existing connection (no provisioning delay)
		session2ID = env.SpawnRemoteSession(flavorID, "echo", "", "agent-two")
		if session2ID == "" {
			t.Fatal("Expected session ID from second spawn")
		}

		// Should be running quickly (no provisioning)
		env.WaitForSessionRunning(session2ID, 5*time.Second)
	})

	t.Run("SpawnThirdSession", func(t *testing.T) {
		session3ID = env.SpawnRemoteSession(flavorID, "echo", "", "agent-three")
		if session3ID == "" {
			t.Fatal("Expected session ID from third spawn")
		}

		env.WaitForSessionRunning(session3ID, 5*time.Second)
	})

	t.Run("VerifyAllSessionsRunning", func(t *testing.T) {
		sessions := env.GetAPISessions()

		foundOne := false
		foundTwo := false
		foundThree := false

		for _, sess := range sessions {
			if sess.ID == session1ID && sess.Running && sess.Nickname == "agent-one" {
				foundOne = true
			}
			if sess.ID == session2ID && sess.Running && sess.Nickname == "agent-two" {
				foundTwo = true
			}
			if sess.ID == session3ID && sess.Running && sess.Nickname == "agent-three" {
				foundThree = true
			}
		}

		if !foundOne {
			t.Error("Session 1 not found or not running")
		}
		if !foundTwo {
			t.Error("Session 2 not found or not running")
		}
		if !foundThree {
			t.Error("Session 3 not found or not running")
		}
	})

	t.Run("VerifySingleRemoteHost", func(t *testing.T) {
		hosts := env.GetRemoteHosts()

		// Should only have one host (all sessions share it)
		connectedHosts := 0
		for _, host := range hosts {
			if host.FlavorID == flavorID && host.Status == "connected" {
				connectedHosts++
			}
		}

		if connectedHosts != 1 {
			t.Errorf("Expected 1 connected host, got %d", connectedHosts)
		}
	})

	t.Run("DisposeSessions", func(t *testing.T) {
		env.DisposeSession(session1ID)
		env.DisposeSession(session2ID)
		env.DisposeSession(session3ID)

		time.Sleep(500 * time.Millisecond)

		// Verify all gone
		sessions := env.GetAPISessions()
		for _, sess := range sessions {
			if sess.ID == session1ID || sess.ID == session2ID || sess.ID == session3ID {
				t.Errorf("Session %s still exists after dispose", sess.ID)
			}
		}
	})
}

// TestE2ERemoteWebSocketOutput tests terminal output streaming for remote sessions.
func TestE2ERemoteWebSocketOutput(t *testing.T) {
	env := New(t)

	const workspaceRoot = "/tmp/schmux-e2e-remote-ws"

	t.Run("CreateConfig", func(t *testing.T) {
		env.CreateConfig(workspaceRoot)
	})

	cwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("Failed to get working directory: %v", err)
	}
	mockScriptPath := filepath.Join(cwd, "..", "..", "test", "mock-remote.sh")

	var flavorID string
	t.Run("AddRemoteFlavor", func(t *testing.T) {
		flavorID = env.AddRemoteFlavorToConfig(
			"mock-remote-ws",
			"Mock Remote WS (E2E Test)",
			"/tmp/test-workspace",
			mockScriptPath,
		)
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
	t.Run("SpawnRemoteSession", func(t *testing.T) {
		// Use 'cat' target which echoes back input
		sessionID = env.SpawnRemoteSession(flavorID, "cat", "", "ws-test")
		if sessionID == "" {
			t.Fatal("Expected session ID from remote spawn")
		}

		env.WaitForRemoteHostStatus(flavorID, "connected", 15*time.Second)
		env.WaitForSessionRunning(sessionID, 10*time.Second)
	})

	t.Run("WebSocketOutput", func(t *testing.T) {
		conn, err := env.ConnectTerminalWebSocket(sessionID)
		if err != nil {
			t.Fatalf("Failed to connect websocket: %v", err)
		}
		defer conn.Close()

		// Send input via WebSocket (remote sessions don't have local tmux sessions,
		// so we must use the WebSocket "input" message type instead of tmux send-keys)
		payload := "remote-ws-e2e-test"
		env.SendWebSocketInput(conn, payload+"\r")

		// Wait for output on websocket
		if _, err := env.WaitForWebSocketContent(conn, payload, 5*time.Second); err != nil {
			t.Fatalf("Did not receive websocket output: %v", err)
		}
	})
}

// TestE2ERemoteStatePersistence tests that remote state persists across daemon restarts.
func TestE2ERemoteStatePersistence(t *testing.T) {
	env := New(t)

	const workspaceRoot = "/tmp/schmux-e2e-remote-state"

	t.Run("CreateConfig", func(t *testing.T) {
		env.CreateConfig(workspaceRoot)
	})

	cwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("Failed to get working directory: %v", err)
	}
	mockScriptPath := filepath.Join(cwd, "..", "..", "test", "mock-remote.sh")

	var flavorID string
	t.Run("AddRemoteFlavor", func(t *testing.T) {
		flavorID = env.AddRemoteFlavorToConfig(
			"mock-remote-state",
			"Mock Remote State (E2E Test)",
			"/tmp/test-workspace",
			mockScriptPath,
		)
	})

	t.Run("DaemonStart", func(t *testing.T) {
		env.DaemonStart()
	})

	var sessionID, hostID, hostname string
	t.Run("SpawnRemoteSession", func(t *testing.T) {
		sessionID = env.SpawnRemoteSession(flavorID, "echo", "", "state-test")
		env.WaitForRemoteHostStatus(flavorID, "connected", 15*time.Second)
		env.WaitForSessionRunning(sessionID, 10*time.Second)

		// Capture host info
		hosts := env.GetRemoteHosts()
		for _, host := range hosts {
			if host.FlavorID == flavorID && host.Status == "connected" {
				hostID = host.ID
				hostname = host.Hostname
				break
			}
		}

		if hostID == "" {
			t.Fatal("Could not find connected remote host")
		}
		t.Logf("Host: %s (hostname: %s)", hostID, hostname)
	})

	defer func() {
		if t.Failed() {
			env.CaptureArtifacts()
		}
	}()

	t.Run("StopDaemon", func(t *testing.T) {
		env.DaemonStop()
	})

	// Wait a bit for daemon to fully stop
	time.Sleep(1 * time.Second)

	t.Run("RestartDaemon", func(t *testing.T) {
		env.DaemonStart()
	})

	t.Run("VerifyHostPersisted", func(t *testing.T) {
		hosts := env.GetRemoteHosts()
		found := false
		for _, host := range hosts {
			if host.ID == hostID {
				found = true
				// After restart, host will be disconnected
				if host.Status != "disconnected" && host.Status != "connected" {
					t.Logf("Warning: Expected host status 'disconnected', got: %s", host.Status)
				}
				if host.Hostname != hostname {
					t.Errorf("Hostname changed after restart: was %s, now %s", hostname, host.Hostname)
				}
			}
		}
		if !found {
			t.Error("Remote host not found after daemon restart")
		}
	})

	t.Run("VerifySessionPersisted", func(t *testing.T) {
		sessions := env.GetAPISessions()
		found := false
		for _, sess := range sessions {
			if sess.ID == sessionID {
				found = true
				// Session will not be running (remote connection lost)
				if sess.Running {
					t.Logf("Note: Session is still running (tmux session survived)")
				}
			}
		}
		if !found {
			t.Error("Session not found after daemon restart")
		}
	})

	t.Run("FinalCleanup", func(t *testing.T) {
		env.DaemonStop()
	})
}

// TestE2ERemoteHooksProvisioning tests that Claude Code hooks are provisioned
// correctly for remote sessions. When spawning a Claude target remotely, the
// command should be wrapped so that .claude/settings.local.json is created in
// the workspace before the agent starts.
func TestE2ERemoteHooksProvisioning(t *testing.T) {
	env := New(t)

	const workspaceRoot = "/tmp/schmux-e2e-remote-hooks"

	// Remote workspace path where the command runs (tmux -c sets cwd to this)
	const remoteWorkspacePath = "/tmp/schmux-e2e-remote-hooks-ws"

	// Ensure workspace directory exists so the command can write files
	if err := os.MkdirAll(remoteWorkspacePath, 0755); err != nil {
		t.Fatalf("Failed to create remote workspace dir: %v", err)
	}
	defer os.RemoveAll(remoteWorkspacePath)

	// Clean up any stale hooks file from previous runs
	os.RemoveAll(filepath.Join(remoteWorkspacePath, ".claude"))

	t.Run("CreateConfig", func(t *testing.T) {
		env.CreateConfig(workspaceRoot)
		// Add "claude" as a detected target with a simple command.
		// The command sleeps so the session stays alive long enough to verify files.
		env.AddDetectedTargetToConfig("claude", "sh -c 'echo hello; sleep 600'")
	})

	cwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("Failed to get working directory: %v", err)
	}
	mockScriptPath := filepath.Join(cwd, "..", "..", "test", "mock-remote.sh")

	var flavorID string
	t.Run("AddRemoteFlavor", func(t *testing.T) {
		flavorID = env.AddRemoteFlavorToConfig(
			"mock-remote-hooks",
			"Mock Remote Hooks (E2E Test)",
			remoteWorkspacePath,
			mockScriptPath,
		)
		if flavorID == "" {
			t.Fatal("Expected flavor ID, got empty")
		}
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
	t.Run("SpawnRemoteClaudeSession", func(t *testing.T) {
		// Target "claude" triggers hooks provisioning (SupportsHooks returns true)
		sessionID = env.SpawnRemoteSession(flavorID, "claude", "do something", "hooks-test")
		if sessionID == "" {
			t.Fatal("Expected session ID from remote spawn")
		}
		t.Logf("Remote Claude session spawned: %s", sessionID)
	})

	t.Run("WaitForConnection", func(t *testing.T) {
		host := env.WaitForRemoteHostStatus(flavorID, "connected", 15*time.Second)
		if host == nil {
			t.Fatal("Remote host did not connect")
		}
	})

	t.Run("WaitForSessionRunning", func(t *testing.T) {
		sess := env.WaitForSessionRunning(sessionID, 10*time.Second)
		if sess == nil {
			t.Fatal("Session did not become running")
		}
	})

	t.Run("VerifyHooksFileCreated", func(t *testing.T) {
		settingsPath := filepath.Join(remoteWorkspacePath, ".claude", "settings.local.json")

		// Wait briefly for the file to be written (the mkdir+printf runs before the command)
		var data []byte
		var readErr error
		deadline := time.Now().Add(5 * time.Second)
		for time.Now().Before(deadline) {
			data, readErr = os.ReadFile(settingsPath)
			if readErr == nil {
				break
			}
			time.Sleep(200 * time.Millisecond)
		}
		if readErr != nil {
			t.Fatalf("Hooks file not created at %s: %v", settingsPath, readErr)
		}

		t.Logf("Hooks file content: %s", string(data))

		// Parse as JSON
		var settings map[string]json.RawMessage
		if err := json.Unmarshal(data, &settings); err != nil {
			t.Fatalf("Hooks file is not valid JSON: %v", err)
		}

		// Verify "hooks" key exists
		hooksRaw, ok := settings["hooks"]
		if !ok {
			t.Fatal("Hooks file missing 'hooks' key")
		}

		// Parse hooks into event map
		var hooks map[string]json.RawMessage
		if err := json.Unmarshal(hooksRaw, &hooks); err != nil {
			t.Fatalf("Failed to parse hooks: %v", err)
		}

		// Verify all expected events are present
		expectedEvents := []string{"SessionStart", "UserPromptSubmit", "Stop", "Notification"}
		for _, event := range expectedEvents {
			if _, ok := hooks[event]; !ok {
				t.Errorf("Missing hook event: %s", event)
			}
		}

		// Verify hooks reference SCHMUX_STATUS_FILE
		hooksStr := string(hooksRaw)
		if !strings.Contains(hooksStr, "SCHMUX_STATUS_FILE") {
			t.Error("Hooks do not reference SCHMUX_STATUS_FILE")
		}

		// Verify state values are present in the commands
		for _, state := range []string{"working", "completed", "needs_input"} {
			if !strings.Contains(hooksStr, state) {
				t.Errorf("Hooks missing state value: %s", state)
			}
		}

		// Verify the Notification hook has the correct matcher for permission prompts
		notificationStr := string(hooks["Notification"])
		if !strings.Contains(notificationStr, "permission_prompt") {
			t.Error("Notification hook missing permission_prompt matcher")
		}
		if !strings.Contains(notificationStr, "idle_prompt") {
			t.Error("Notification hook missing idle_prompt matcher")
		}
	})

	t.Run("DisposeSession", func(t *testing.T) {
		env.DisposeSession(sessionID)
	})
}
