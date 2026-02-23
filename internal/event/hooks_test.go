package event

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestEnsureGlobalHookScripts(t *testing.T) {
	t.Run("creates hooks directory and writes all scripts", func(t *testing.T) {
		homeDir := t.TempDir()

		hooksDir, err := EnsureGlobalHookScripts(homeDir)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		expectedDir := filepath.Join(homeDir, ".schmux", "hooks")
		if hooksDir != expectedDir {
			t.Errorf("returned path = %q, want %q", hooksDir, expectedDir)
		}

		// Verify directory exists
		info, err := os.Stat(hooksDir)
		if err != nil {
			t.Fatalf("hooks directory does not exist: %v", err)
		}
		if !info.IsDir() {
			t.Fatal("hooks path is not a directory")
		}

		// Verify all three scripts exist and are executable
		scripts := []string{
			"capture-failure.sh",
			"stop-status-check.sh",
			"stop-lore-check.sh",
		}
		for _, name := range scripts {
			path := filepath.Join(hooksDir, name)
			info, err := os.Stat(path)
			if err != nil {
				t.Errorf("script %q does not exist: %v", name, err)
				continue
			}

			// Check file is not empty
			if info.Size() == 0 {
				t.Errorf("script %q is empty", name)
			}

			// Check file is executable (owner execute bit)
			mode := info.Mode()
			if mode&0100 == 0 {
				t.Errorf("script %q is not executable: mode=%v", name, mode)
			}
		}
	})

	t.Run("files are executable with 0755 permissions", func(t *testing.T) {
		homeDir := t.TempDir()

		hooksDir, err := EnsureGlobalHookScripts(homeDir)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		scripts := []string{
			"capture-failure.sh",
			"stop-status-check.sh",
			"stop-lore-check.sh",
		}
		for _, name := range scripts {
			path := filepath.Join(hooksDir, name)
			info, err := os.Stat(path)
			if err != nil {
				t.Fatalf("script %q does not exist: %v", name, err)
			}

			// On Unix, verify the permission bits
			mode := info.Mode().Perm()
			if mode != 0755 {
				t.Errorf("script %q has permissions %o, want 0755", name, mode)
			}
		}
	})

	t.Run("scripts start with shebang", func(t *testing.T) {
		homeDir := t.TempDir()

		hooksDir, err := EnsureGlobalHookScripts(homeDir)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		scripts := []string{
			"capture-failure.sh",
			"stop-status-check.sh",
			"stop-lore-check.sh",
		}
		for _, name := range scripts {
			path := filepath.Join(hooksDir, name)
			content, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("failed to read %q: %v", name, err)
			}
			if len(content) < 2 || string(content[:2]) != "#!" {
				t.Errorf("script %q does not start with shebang (#!), starts with: %q", name, string(content[:min(10, len(content))]))
			}
		}
	})

	t.Run("idempotent - second call succeeds", func(t *testing.T) {
		homeDir := t.TempDir()

		hooksDir1, err := EnsureGlobalHookScripts(homeDir)
		if err != nil {
			t.Fatalf("first call: unexpected error: %v", err)
		}

		hooksDir2, err := EnsureGlobalHookScripts(homeDir)
		if err != nil {
			t.Fatalf("second call: unexpected error: %v", err)
		}

		if hooksDir1 != hooksDir2 {
			t.Errorf("first call returned %q, second call returned %q", hooksDir1, hooksDir2)
		}

		// Verify scripts still exist and are valid after second call
		scripts := []string{
			"capture-failure.sh",
			"stop-status-check.sh",
			"stop-lore-check.sh",
		}
		for _, name := range scripts {
			path := filepath.Join(hooksDir2, name)
			info, err := os.Stat(path)
			if err != nil {
				t.Errorf("script %q does not exist after second call: %v", name, err)
				continue
			}
			if info.Size() == 0 {
				t.Errorf("script %q is empty after second call", name)
			}
		}
	})

	t.Run("returns correct hooks directory path", func(t *testing.T) {
		homeDir := t.TempDir()

		hooksDir, err := EnsureGlobalHookScripts(homeDir)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		// The path should be <homeDir>/.schmux/hooks
		expected := filepath.Join(homeDir, ".schmux", "hooks")
		if hooksDir != expected {
			t.Errorf("hooksDir = %q, want %q", hooksDir, expected)
		}
	})
}

// runHookScript executes a hook script with the given stdin, env vars, and returns stdout/stderr.
func runHookScript(t *testing.T, scriptPath, stdinData string, envVars ...string) (stdout, stderr string) {
	t.Helper()
	cmd := exec.Command("/bin/bash", scriptPath)
	cmd.Stdin = strings.NewReader(stdinData)
	var outBuf, errBuf bytes.Buffer
	cmd.Stdout = &outBuf
	cmd.Stderr = &errBuf
	cmd.Env = append(os.Environ(), envVars...)
	err := cmd.Run()
	if err != nil {
		t.Logf("script stderr: %s", errBuf.String())
		t.Fatalf("script %s exited with error: %v", filepath.Base(scriptPath), err)
	}
	return outBuf.String(), errBuf.String()
}

func TestCaptureFailureScript(t *testing.T) {
	if _, err := exec.LookPath("jq"); err != nil {
		t.Skip("jq not found, skipping hook tests")
	}

	homeDir := t.TempDir()
	hooksDir, err := EnsureGlobalHookScripts(homeDir)
	if err != nil {
		t.Fatalf("failed to create hook scripts: %v", err)
	}
	scriptPath := filepath.Join(hooksDir, "capture-failure.sh")

	t.Run("tool failure writes correct event JSON", func(t *testing.T) {
		eventsFile := filepath.Join(t.TempDir(), "events.jsonl")
		stdin := `{"tool_name":"Bash","tool_input":{"command":"foo --bar"},"error":"Missing script: foo","is_interrupt":false}`

		runHookScript(t, scriptPath, stdin,
			"SCHMUX_EVENTS_FILE="+eventsFile,
			"SCHMUX_SESSION_ID=test-session",
		)

		content, err := os.ReadFile(eventsFile)
		if err != nil {
			t.Fatalf("failed to read events file: %v", err)
		}

		var event map[string]interface{}
		if err := json.Unmarshal(content, &event); err != nil {
			t.Fatalf("failed to parse event JSON: %v\ncontent: %s", err, string(content))
		}

		if event["type"] != "failure" {
			t.Errorf("type = %v, want failure", event["type"])
		}
		if event["tool"] != "Bash" {
			t.Errorf("tool = %v, want Bash", event["tool"])
		}
		if event["input"] != "foo --bar" {
			t.Errorf("input = %v, want 'foo --bar'", event["input"])
		}
		if event["category"] != "wrong_command" {
			t.Errorf("category = %v, want wrong_command", event["category"])
		}
		if !strings.Contains(event["error"].(string), "Missing script") {
			t.Errorf("error = %v, want to contain 'Missing script'", event["error"])
		}
		if _, ok := event["ts"]; !ok {
			t.Error("event is missing 'ts' field")
		}
	})

	t.Run("interrupt is skipped", func(t *testing.T) {
		eventsFile := filepath.Join(t.TempDir(), "events.jsonl")
		stdin := `{"tool_name":"Bash","tool_input":{"command":"ls"},"error":"interrupted","is_interrupt":true}`

		runHookScript(t, scriptPath, stdin,
			"SCHMUX_EVENTS_FILE="+eventsFile,
			"SCHMUX_SESSION_ID=test-session",
		)

		if _, err := os.Stat(eventsFile); err == nil {
			content, _ := os.ReadFile(eventsFile)
			if len(content) > 0 {
				t.Errorf("expected no event written for interrupt, but got: %s", string(content))
			}
		}
	})

	t.Run("empty error is skipped", func(t *testing.T) {
		eventsFile := filepath.Join(t.TempDir(), "events.jsonl")
		stdin := `{"tool_name":"Bash","tool_input":{"command":"ls"},"error":"","is_interrupt":false}`

		runHookScript(t, scriptPath, stdin,
			"SCHMUX_EVENTS_FILE="+eventsFile,
			"SCHMUX_SESSION_ID=test-session",
		)

		if _, err := os.Stat(eventsFile); err == nil {
			content, _ := os.ReadFile(eventsFile)
			if len(content) > 0 {
				t.Errorf("expected no event written for empty error, but got: %s", string(content))
			}
		}
	})

	t.Run("error category not_found", func(t *testing.T) {
		eventsFile := filepath.Join(t.TempDir(), "events.jsonl")
		stdin := `{"tool_name":"Read","tool_input":{"file_path":"/tmp/missing.txt"},"error":"No such file or directory","is_interrupt":false}`

		runHookScript(t, scriptPath, stdin,
			"SCHMUX_EVENTS_FILE="+eventsFile,
			"SCHMUX_SESSION_ID=test-session",
		)

		content, err := os.ReadFile(eventsFile)
		if err != nil {
			t.Fatalf("failed to read events file: %v", err)
		}
		var event map[string]interface{}
		if err := json.Unmarshal(content, &event); err != nil {
			t.Fatalf("failed to parse event JSON: %v", err)
		}
		if event["category"] != "not_found" {
			t.Errorf("category = %v, want not_found", event["category"])
		}
	})

	t.Run("error category permission", func(t *testing.T) {
		eventsFile := filepath.Join(t.TempDir(), "events.jsonl")
		stdin := `{"tool_name":"Bash","tool_input":{"command":"cat /etc/shadow"},"error":"permission denied","is_interrupt":false}`

		runHookScript(t, scriptPath, stdin,
			"SCHMUX_EVENTS_FILE="+eventsFile,
			"SCHMUX_SESSION_ID=test-session",
		)

		content, err := os.ReadFile(eventsFile)
		if err != nil {
			t.Fatalf("failed to read events file: %v", err)
		}
		var event map[string]interface{}
		if err := json.Unmarshal(content, &event); err != nil {
			t.Fatalf("failed to parse event JSON: %v", err)
		}
		if event["category"] != "permission" {
			t.Errorf("category = %v, want permission", event["category"])
		}
	})

	t.Run("error category build_failure", func(t *testing.T) {
		eventsFile := filepath.Join(t.TempDir(), "events.jsonl")
		stdin := `{"tool_name":"Bash","tool_input":{"command":"go build ./..."},"error":"build failed: cannot find module","is_interrupt":false}`

		runHookScript(t, scriptPath, stdin,
			"SCHMUX_EVENTS_FILE="+eventsFile,
			"SCHMUX_SESSION_ID=test-session",
		)

		content, err := os.ReadFile(eventsFile)
		if err != nil {
			t.Fatalf("failed to read events file: %v", err)
		}
		var event map[string]interface{}
		if err := json.Unmarshal(content, &event); err != nil {
			t.Fatalf("failed to parse event JSON: %v", err)
		}
		if event["category"] != "build_failure" {
			t.Errorf("category = %v, want build_failure", event["category"])
		}
	})

	t.Run("error category test_failure", func(t *testing.T) {
		eventsFile := filepath.Join(t.TempDir(), "events.jsonl")
		stdin := `{"tool_name":"Bash","tool_input":{"command":"go test ./..."},"error":"--- FAIL: TestSomething","is_interrupt":false}`

		runHookScript(t, scriptPath, stdin,
			"SCHMUX_EVENTS_FILE="+eventsFile,
			"SCHMUX_SESSION_ID=test-session",
		)

		content, err := os.ReadFile(eventsFile)
		if err != nil {
			t.Fatalf("failed to read events file: %v", err)
		}
		var event map[string]interface{}
		if err := json.Unmarshal(content, &event); err != nil {
			t.Fatalf("failed to parse event JSON: %v", err)
		}
		if event["category"] != "test_failure" {
			t.Errorf("category = %v, want test_failure", event["category"])
		}
	})

	t.Run("error category timeout", func(t *testing.T) {
		eventsFile := filepath.Join(t.TempDir(), "events.jsonl")
		stdin := `{"tool_name":"Bash","tool_input":{"command":"curl http://slow"},"error":"connection timed out","is_interrupt":false}`

		runHookScript(t, scriptPath, stdin,
			"SCHMUX_EVENTS_FILE="+eventsFile,
			"SCHMUX_SESSION_ID=test-session",
		)

		content, err := os.ReadFile(eventsFile)
		if err != nil {
			t.Fatalf("failed to read events file: %v", err)
		}
		var event map[string]interface{}
		if err := json.Unmarshal(content, &event); err != nil {
			t.Fatalf("failed to parse event JSON: %v", err)
		}
		if event["category"] != "timeout" {
			t.Errorf("category = %v, want timeout", event["category"])
		}
	})

	t.Run("Bash tool extracts command", func(t *testing.T) {
		eventsFile := filepath.Join(t.TempDir(), "events.jsonl")
		stdin := `{"tool_name":"Bash","tool_input":{"command":"ls -la /nonexistent"},"error":"No such file or directory","is_interrupt":false}`

		runHookScript(t, scriptPath, stdin,
			"SCHMUX_EVENTS_FILE="+eventsFile,
			"SCHMUX_SESSION_ID=test-session",
		)

		content, err := os.ReadFile(eventsFile)
		if err != nil {
			t.Fatalf("failed to read events file: %v", err)
		}
		var event map[string]interface{}
		if err := json.Unmarshal(content, &event); err != nil {
			t.Fatalf("failed to parse event JSON: %v", err)
		}
		if event["input"] != "ls -la /nonexistent" {
			t.Errorf("input = %v, want 'ls -la /nonexistent'", event["input"])
		}
	})

	t.Run("Read tool extracts file_path", func(t *testing.T) {
		eventsFile := filepath.Join(t.TempDir(), "events.jsonl")
		stdin := `{"tool_name":"Read","tool_input":{"file_path":"/tmp/myfile.txt"},"error":"No such file or directory","is_interrupt":false}`

		runHookScript(t, scriptPath, stdin,
			"SCHMUX_EVENTS_FILE="+eventsFile,
			"SCHMUX_SESSION_ID=test-session",
		)

		content, err := os.ReadFile(eventsFile)
		if err != nil {
			t.Fatalf("failed to read events file: %v", err)
		}
		var event map[string]interface{}
		if err := json.Unmarshal(content, &event); err != nil {
			t.Fatalf("failed to parse event JSON: %v", err)
		}
		if event["input"] != "/tmp/myfile.txt" {
			t.Errorf("input = %v, want '/tmp/myfile.txt'", event["input"])
		}
	})
}

func TestStopStatusCheckScript(t *testing.T) {
	if _, err := exec.LookPath("jq"); err != nil {
		t.Skip("jq not found, skipping hook tests")
	}

	homeDir := t.TempDir()
	hooksDir, err := EnsureGlobalHookScripts(homeDir)
	if err != nil {
		t.Fatalf("failed to create hook scripts: %v", err)
	}
	scriptPath := filepath.Join(hooksDir, "stop-status-check.sh")

	t.Run("blocks when no status event", func(t *testing.T) {
		eventsFile := filepath.Join(t.TempDir(), "events.jsonl")
		// Create an empty events file
		if err := os.WriteFile(eventsFile, []byte(""), 0644); err != nil {
			t.Fatalf("failed to create events file: %v", err)
		}

		stdout, _ := runHookScript(t, scriptPath, "{}",
			"SCHMUX_EVENTS_FILE="+eventsFile,
			"SCHMUX_SESSION_ID=test-session",
		)

		if !strings.Contains(stdout, `"decision":"block"`) {
			t.Errorf("expected blocking decision, got stdout: %q", stdout)
		}
	})

	t.Run("allows completed status", func(t *testing.T) {
		eventsFile := filepath.Join(t.TempDir(), "events.jsonl")
		event := `{"ts":"2025-01-01T00:00:00Z","type":"status","state":"completed","message":"done"}` + "\n"
		if err := os.WriteFile(eventsFile, []byte(event), 0644); err != nil {
			t.Fatalf("failed to write events file: %v", err)
		}

		stdout, _ := runHookScript(t, scriptPath, "{}",
			"SCHMUX_EVENTS_FILE="+eventsFile,
			"SCHMUX_SESSION_ID=test-session",
		)

		if strings.Contains(stdout, `"decision":"block"`) {
			t.Errorf("expected allow (no block), got stdout: %q", stdout)
		}
	})

	t.Run("allows needs_input status", func(t *testing.T) {
		eventsFile := filepath.Join(t.TempDir(), "events.jsonl")
		event := `{"ts":"2025-01-01T00:00:00Z","type":"status","state":"needs_input","message":"waiting for user"}` + "\n"
		if err := os.WriteFile(eventsFile, []byte(event), 0644); err != nil {
			t.Fatalf("failed to write events file: %v", err)
		}

		stdout, _ := runHookScript(t, scriptPath, "{}",
			"SCHMUX_EVENTS_FILE="+eventsFile,
			"SCHMUX_SESSION_ID=test-session",
		)

		if strings.Contains(stdout, `"decision":"block"`) {
			t.Errorf("expected allow (no block), got stdout: %q", stdout)
		}
	})

	t.Run("allows error status", func(t *testing.T) {
		eventsFile := filepath.Join(t.TempDir(), "events.jsonl")
		event := `{"ts":"2025-01-01T00:00:00Z","type":"status","state":"error","message":"something broke"}` + "\n"
		if err := os.WriteFile(eventsFile, []byte(event), 0644); err != nil {
			t.Fatalf("failed to write events file: %v", err)
		}

		stdout, _ := runHookScript(t, scriptPath, "{}",
			"SCHMUX_EVENTS_FILE="+eventsFile,
			"SCHMUX_SESSION_ID=test-session",
		)

		if strings.Contains(stdout, `"decision":"block"`) {
			t.Errorf("expected allow (no block), got stdout: %q", stdout)
		}
	})

	t.Run("blocks working with empty message", func(t *testing.T) {
		eventsFile := filepath.Join(t.TempDir(), "events.jsonl")
		event := `{"ts":"2025-01-01T00:00:00Z","type":"status","state":"working","message":""}` + "\n"
		if err := os.WriteFile(eventsFile, []byte(event), 0644); err != nil {
			t.Fatalf("failed to write events file: %v", err)
		}

		stdout, _ := runHookScript(t, scriptPath, "{}",
			"SCHMUX_EVENTS_FILE="+eventsFile,
			"SCHMUX_SESSION_ID=test-session",
		)

		if !strings.Contains(stdout, `"decision":"block"`) {
			t.Errorf("expected blocking decision for working with empty message, got stdout: %q", stdout)
		}
	})

	t.Run("allows working with message", func(t *testing.T) {
		eventsFile := filepath.Join(t.TempDir(), "events.jsonl")
		event := `{"ts":"2025-01-01T00:00:00Z","type":"status","state":"working","message":"implementing feature X"}` + "\n"
		if err := os.WriteFile(eventsFile, []byte(event), 0644); err != nil {
			t.Fatalf("failed to write events file: %v", err)
		}

		stdout, _ := runHookScript(t, scriptPath, "{}",
			"SCHMUX_EVENTS_FILE="+eventsFile,
			"SCHMUX_SESSION_ID=test-session",
		)

		if strings.Contains(stdout, `"decision":"block"`) {
			t.Errorf("expected allow for working with message, got stdout: %q", stdout)
		}
	})

	t.Run("stop_hook_active bypasses and writes completed", func(t *testing.T) {
		eventsFile := filepath.Join(t.TempDir(), "events.jsonl")

		stdout, _ := runHookScript(t, scriptPath, `{"stop_hook_active":true}`,
			"SCHMUX_EVENTS_FILE="+eventsFile,
			"SCHMUX_SESSION_ID=test-session",
		)

		// Should not block
		if strings.Contains(stdout, `"decision":"block"`) {
			t.Errorf("expected no block with stop_hook_active, got stdout: %q", stdout)
		}

		// Should write a completed status event to events file
		content, err := os.ReadFile(eventsFile)
		if err != nil {
			t.Fatalf("failed to read events file: %v", err)
		}
		if !strings.Contains(string(content), `"type":"status"`) {
			t.Errorf("expected completed status event in file, got: %s", string(content))
		}
		if !strings.Contains(string(content), `"state":"completed"`) {
			t.Errorf("expected state=completed in event, got: %s", string(content))
		}
	})
}

func TestStopLoreCheckScript(t *testing.T) {
	if _, err := exec.LookPath("jq"); err != nil {
		t.Skip("jq not found, skipping hook tests")
	}

	homeDir := t.TempDir()
	hooksDir, err := EnsureGlobalHookScripts(homeDir)
	if err != nil {
		t.Fatalf("failed to create hook scripts: %v", err)
	}
	scriptPath := filepath.Join(hooksDir, "stop-lore-check.sh")

	t.Run("blocks when no reflection", func(t *testing.T) {
		eventsFile := filepath.Join(t.TempDir(), "events.jsonl")
		// Create an empty events file
		if err := os.WriteFile(eventsFile, []byte(""), 0644); err != nil {
			t.Fatalf("failed to create events file: %v", err)
		}

		stdout, _ := runHookScript(t, scriptPath, "{}",
			"SCHMUX_EVENTS_FILE="+eventsFile,
			"SCHMUX_SESSION_ID=test-session",
		)

		if !strings.Contains(stdout, `"decision":"block"`) {
			t.Errorf("expected blocking decision, got stdout: %q", stdout)
		}
	})

	t.Run("allows when reflection exists", func(t *testing.T) {
		eventsFile := filepath.Join(t.TempDir(), "events.jsonl")
		event := `{"ts":"2025-01-01T00:00:00Z","type":"reflection","text":"When doing X, do Y instead"}` + "\n"
		if err := os.WriteFile(eventsFile, []byte(event), 0644); err != nil {
			t.Fatalf("failed to write events file: %v", err)
		}

		stdout, _ := runHookScript(t, scriptPath, "{}",
			"SCHMUX_EVENTS_FILE="+eventsFile,
			"SCHMUX_SESSION_ID=test-session",
		)

		if strings.Contains(stdout, `"decision":"block"`) {
			t.Errorf("expected allow (no block), got stdout: %q", stdout)
		}
	})

	t.Run("stop_hook_active bypasses", func(t *testing.T) {
		eventsFile := filepath.Join(t.TempDir(), "events.jsonl")
		// Empty events file — would normally block
		if err := os.WriteFile(eventsFile, []byte(""), 0644); err != nil {
			t.Fatalf("failed to create events file: %v", err)
		}

		stdout, _ := runHookScript(t, scriptPath, `{"stop_hook_active":true}`,
			"SCHMUX_EVENTS_FILE="+eventsFile,
			"SCHMUX_SESSION_ID=test-session",
		)

		if strings.Contains(stdout, `"decision":"block"`) {
			t.Errorf("expected no block with stop_hook_active, got stdout: %q", stdout)
		}
	})
}
