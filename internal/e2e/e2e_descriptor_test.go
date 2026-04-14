//go:build e2e

package e2e

import (
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// TestE2EDescriptorAdapterSpawn validates the full descriptor pipeline:
// drop a YAML descriptor into ~/.schmux/adapters/ → daemon detects the tool →
// tool is auto-enabled → session can be spawned using the tool as a target,
// all without any manual config.
func TestE2EDescriptorAdapterSpawn(t *testing.T) {
	t.Parallel()
	env := New(t)

	workspaceRoot := t.TempDir()
	env.CreateConfig(workspaceRoot)
	repoPath := env.SetupTestRepo(workspaceRoot, "test-repo")

	// Create a wrapper script that acts as a fake agent binary.
	// It prints a marker string and then sleeps (keeping the session alive).
	binDir := filepath.Join(env.HomeDir, "bin")
	if err := os.MkdirAll(binDir, 0755); err != nil {
		t.Fatalf("Failed to create bin dir: %v", err)
	}
	agentScript := filepath.Join(binDir, "myagent")
	if err := os.WriteFile(agentScript, []byte("#!/bin/sh\necho hello-from-myagent\nsleep 600\n"), 0755); err != nil {
		t.Fatalf("Failed to write agent script: %v", err)
	}

	// Write a runtime descriptor that detects the fake agent via file_exists.
	adaptersDir := filepath.Join(env.HomeDir, ".schmux", "adapters")
	if err := os.MkdirAll(adaptersDir, 0755); err != nil {
		t.Fatalf("Failed to create adapters dir: %v", err)
	}
	descriptor := "name: myagent\ndisplay_name: My Agent\ndetect:\n  - type: file_exists\n    path: " + agentScript + "\ncapabilities: [interactive]\n"
	if err := os.WriteFile(filepath.Join(adaptersDir, "myagent.yaml"), []byte(descriptor), 0644); err != nil {
		t.Fatalf("Failed to write descriptor: %v", err)
	}

	// Start daemon — it should auto-discover myagent from the descriptor
	env.DaemonStart()

	// Step 1: Verify myagent appears in detection summary
	t.Log("Checking detection summary...")
	type DetectionSummary struct {
		Agents []struct {
			Name    string `json:"name"`
			Command string `json:"command"`
			Source  string `json:"source"`
		} `json:"agents"`
	}

	resp, err := http.Get(env.DaemonURL + "/api/detection-summary")
	if err != nil {
		t.Fatalf("Failed to get detection summary: %v", err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()

	var summary DetectionSummary
	if err := json.Unmarshal(body, &summary); err != nil {
		t.Fatalf("Failed to parse detection summary: %v\nBody: %s", err, body)
	}

	found := false
	for _, agent := range summary.Agents {
		if agent.Name == "myagent" {
			found = true
			t.Logf("myagent detected: command=%s source=%s", agent.Command, agent.Source)
			break
		}
	}
	if !found {
		t.Fatalf("myagent not found in detection summary.\nFull response: %s", body)
	}

	// Step 2: Verify myagent is auto-enabled (no manual enablement needed)
	t.Log("Checking auto-enablement...")
	type ConfigResponse struct {
		EnabledModels map[string]string `json:"enabled_models"`
	}

	resp, err = http.Get(env.DaemonURL + "/api/config")
	if err != nil {
		t.Fatalf("Failed to get config: %v", err)
	}
	body, _ = io.ReadAll(resp.Body)
	resp.Body.Close()

	var cfg ConfigResponse
	if err := json.Unmarshal(body, &cfg); err != nil {
		t.Fatalf("Failed to parse config: %v", err)
	}

	if runner, ok := cfg.EnabledModels["myagent"]; !ok {
		t.Fatalf("myagent not in enabled_models.\nFull enabled_models: %v", cfg.EnabledModels)
	} else {
		t.Logf("myagent auto-enabled with runner: %s", runner)
	}

	// Step 3: Spawn a session using myagent as the target — no config needed
	t.Log("Spawning session with myagent target...")
	nickname := env.Nickname("myagent-test")
	sessionID := env.SpawnSession("file://"+repoPath, "main", "myagent", "", nickname)
	if sessionID == "" {
		t.Fatal("Failed to spawn session with myagent target")
	}
	t.Logf("Session spawned: %s", sessionID)

	// Step 4: Verify the session is running and produced output
	t.Log("Waiting for session to be running...")
	sess := env.WaitForSessionRunning(sessionID, 10*time.Second)
	if sess == nil {
		env.CaptureArtifacts()
		t.Fatal("Session never became running")
	}
	t.Logf("Session running: target=%s", sess.Target)

	// Step 5: Verify the session's target is myagent
	if sess.Target != "myagent" {
		t.Errorf("session target = %q, want %q", sess.Target, "myagent")
	}

	// Step 6: Connect to WebSocket and verify the command actually ran
	t.Log("Checking terminal output...")
	wsConn, err := env.ConnectTerminalWebSocket(sessionID)
	if err != nil {
		env.CaptureArtifacts()
		t.Fatalf("Failed to connect terminal WebSocket: %v", err)
	}
	defer wsConn.Close()

	output, err := env.WaitForWebSocketContent(wsConn, "hello-from-myagent", 10*time.Second)
	if err != nil {
		env.CaptureArtifacts()
		t.Fatalf("Expected output 'hello-from-myagent' not found.\nReceived: %q", output)
	}
	t.Log("Terminal output confirmed: hello-from-myagent")

	// Step 7: Clean up — dispose session
	env.DisposeSession(sessionID)
	t.Log("Session disposed successfully")
}
