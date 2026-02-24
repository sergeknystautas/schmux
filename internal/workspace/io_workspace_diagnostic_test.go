package workspace

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestIOWorkspaceDiagnosticCapture_WriteToDir(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "test-io-workspace")

	tel := NewIOWorkspaceTelemetry()
	tel.RecordCommand("git", []string{"fetch"}, "ws-1", "/tmp/ws1", RefreshTriggerPoller, 200*time.Millisecond, 0, 1000, 50)
	tel.RecordCommand("git", []string{"status", "--porcelain"}, "ws-1", "/tmp/ws1", RefreshTriggerPoller, 50*time.Millisecond, 0, 200, 0)

	snap := tel.Snapshot(false)
	diag := NewIOWorkspaceDiagnosticCapture(snap, time.Now())
	if err := diag.WriteToDir(dir); err != nil {
		t.Fatalf("WriteToDir failed: %v", err)
	}

	// meta.json must exist and be valid JSON with required fields
	metaData, err := os.ReadFile(filepath.Join(dir, "meta.json"))
	if err != nil {
		t.Fatalf("failed to read meta.json: %v", err)
	}
	var meta map[string]interface{}
	if err := json.Unmarshal(metaData, &meta); err != nil {
		t.Fatalf("meta.json is not valid JSON: %v", err)
	}
	// Check required fields
	requiredFields := []string{"timestamp", "totalCommands", "totalDurationMs", "counters", "triggerCounts", "spanDurations", "byTriggerSpans", "byWorkspaceSpans", "findings", "verdict"}
	for _, field := range requiredFields {
		if _, ok := meta[field]; !ok {
			t.Errorf("meta.json missing required field: %s", field)
		}
	}
	// totalCommands should be 2
	if tc, ok := meta["totalCommands"].(float64); !ok || int64(tc) != 2 {
		t.Errorf("expected totalCommands=2, got %v", meta["totalCommands"])
	}
	// findings must be present and non-empty
	findings, ok := meta["findings"].([]interface{})
	if !ok || len(findings) == 0 {
		t.Errorf("expected non-empty findings, got %v", meta["findings"])
	}
	// verdict must be present and non-empty
	verdict, ok := meta["verdict"].(string)
	if !ok || verdict == "" {
		t.Errorf("expected non-empty verdict, got %v", meta["verdict"])
	}

	// commands-ringbuffer.txt must exist
	cmdData, err := os.ReadFile(filepath.Join(dir, "commands-ringbuffer.txt"))
	if err != nil {
		t.Fatalf("failed to read commands-ringbuffer.txt: %v", err)
	}
	if len(cmdData) == 0 {
		t.Error("commands-ringbuffer.txt is empty")
	}
	// Should contain workspace ID and command name
	if !strings.Contains(string(cmdData), "workspace=ws-1") {
		t.Error("commands-ringbuffer.txt should contain workspace=ws-1")
	}
	if !strings.Contains(string(cmdData), "git fetch") {
		t.Error("commands-ringbuffer.txt should contain 'git fetch'")
	}

	// slow-commands.txt must exist
	slowData, err := os.ReadFile(filepath.Join(dir, "slow-commands.txt"))
	if err != nil {
		t.Fatalf("failed to read slow-commands.txt: %v", err)
	}
	// The 200ms fetch should be in slow commands (threshold is 100ms)
	if !strings.Contains(string(slowData), "git fetch") {
		t.Error("slow-commands.txt should contain 'git fetch' (200ms > 100ms threshold)")
	}

	// by-workspace.txt must exist
	wsData, err := os.ReadFile(filepath.Join(dir, "by-workspace.txt"))
	if err != nil {
		t.Fatalf("failed to read by-workspace.txt: %v", err)
	}
	if !strings.Contains(string(wsData), "workspace=ws-1") {
		t.Error("by-workspace.txt should contain workspace=ws-1")
	}
}

func TestIOWorkspaceDiagnosticCapture_WriteToDirCreatesDir(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "nested", "deep", "diagnostic")

	tel := NewIOWorkspaceTelemetry()
	snap := tel.Snapshot(false)
	diag := NewIOWorkspaceDiagnosticCapture(snap, time.Now())
	if err := diag.WriteToDir(dir); err != nil {
		t.Fatalf("WriteToDir failed to create nested directory: %v", err)
	}

	// Verify the directory was created
	if _, err := os.Stat(dir); os.IsNotExist(err) {
		t.Error("WriteToDir should create the directory")
	}
}

func TestIOWorkspaceDiagnosticCapture_Findings(t *testing.T) {
	t.Run("dominant command type", func(t *testing.T) {
		tel := NewIOWorkspaceTelemetry()
		// Record one very slow fetch and several fast status commands
		tel.RecordCommand("git", []string{"fetch"}, "ws-1", "/tmp/ws1", RefreshTriggerPoller, 500*time.Millisecond, 0, 1000, 0)
		tel.RecordCommand("git", []string{"status"}, "ws-1", "/tmp/ws1", RefreshTriggerPoller, 10*time.Millisecond, 0, 100, 0)
		tel.RecordCommand("git", []string{"status"}, "ws-1", "/tmp/ws1", RefreshTriggerPoller, 10*time.Millisecond, 0, 100, 0)

		snap := tel.Snapshot(false)
		diag := NewIOWorkspaceDiagnosticCapture(snap, time.Now())

		found := false
		for _, f := range diag.Findings {
			if strings.Contains(f, "git_fetch") && strings.Contains(f, "% of total time") {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("expected finding about dominant git_fetch, got: %v", diag.Findings)
		}
		if !strings.Contains(diag.Verdict, "dominated by git_fetch") {
			t.Errorf("expected verdict about dominant git_fetch, got: %s", diag.Verdict)
		}
	})

	t.Run("watcher poller overlap", func(t *testing.T) {
		tel := NewIOWorkspaceTelemetry()
		// Both poller and watcher run the same command type
		tel.RecordCommand("git", []string{"status"}, "ws-1", "/tmp/ws1", RefreshTriggerPoller, 50*time.Millisecond, 0, 100, 0)
		tel.RecordCommand("git", []string{"status"}, "ws-1", "/tmp/ws1", RefreshTriggerWatcher, 50*time.Millisecond, 0, 100, 0)

		snap := tel.Snapshot(false)
		diag := NewIOWorkspaceDiagnosticCapture(snap, time.Now())

		found := false
		for _, f := range diag.Findings {
			if strings.Contains(f, "Watcher and poller") && strings.Contains(f, "git_status") {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("expected finding about watcher/poller overlap, got: %v", diag.Findings)
		}
	})

	t.Run("dominant workspace", func(t *testing.T) {
		tel := NewIOWorkspaceTelemetry()
		// ws-1 has much more time than ws-2
		tel.RecordCommand("git", []string{"fetch"}, "ws-1", "/tmp/ws1", RefreshTriggerPoller, 400*time.Millisecond, 0, 1000, 0)
		tel.RecordCommand("git", []string{"status"}, "ws-1", "/tmp/ws1", RefreshTriggerPoller, 100*time.Millisecond, 0, 100, 0)
		tel.RecordCommand("git", []string{"status"}, "ws-2", "/tmp/ws2", RefreshTriggerPoller, 10*time.Millisecond, 0, 100, 0)

		snap := tel.Snapshot(false)
		diag := NewIOWorkspaceDiagnosticCapture(snap, time.Now())

		found := false
		for _, f := range diag.Findings {
			if strings.Contains(f, "ws-1") && strings.Contains(f, "% of total time") {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("expected finding about dominant workspace ws-1, got: %v", diag.Findings)
		}
	})

	t.Run("command rate", func(t *testing.T) {
		tel := NewIOWorkspaceTelemetry()
		tel.RecordCommand("git", []string{"status"}, "ws-1", "/tmp/ws1", RefreshTriggerPoller, 50*time.Millisecond, 0, 100, 0)
		tel.RecordCommand("git", []string{"fetch"}, "ws-1", "/tmp/ws1", RefreshTriggerPoller, 100*time.Millisecond, 0, 500, 0)

		snap := tel.Snapshot(false)
		diag := NewIOWorkspaceDiagnosticCapture(snap, time.Now())

		found := false
		for _, f := range diag.Findings {
			if strings.Contains(f, "Command rate") && strings.Contains(f, "commands/sec") {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("expected finding about command rate, got: %v", diag.Findings)
		}
	})

	t.Run("no issues with empty telemetry", func(t *testing.T) {
		tel := NewIOWorkspaceTelemetry()
		snap := tel.Snapshot(false)
		diag := NewIOWorkspaceDiagnosticCapture(snap, time.Now())

		if len(diag.Findings) != 1 || diag.Findings[0] != "No issues detected." {
			t.Errorf("expected 'No issues detected.' finding, got: %v", diag.Findings)
		}
		if diag.Verdict != "No obvious issues detected." {
			t.Errorf("expected 'No obvious issues detected.' verdict, got: %s", diag.Verdict)
		}
	})
}

func TestIOWorkspaceDiagnosticCapture_ByWorkspaceFormat(t *testing.T) {
	tel := NewIOWorkspaceTelemetry()
	tel.RecordCommand("git", []string{"fetch"}, "ws-1", "/tmp/ws1", RefreshTriggerPoller, 300*time.Millisecond, 0, 1000, 0)
	tel.RecordCommand("git", []string{"status"}, "ws-1", "/tmp/ws1", RefreshTriggerPoller, 50*time.Millisecond, 0, 100, 0)
	tel.RecordCommand("git", []string{"show-ref"}, "ws-1", "/tmp/ws1", RefreshTriggerPoller, 20*time.Millisecond, 0, 50, 0)
	tel.RecordCommand("git", []string{"fetch"}, "ws-2", "/tmp/ws2", RefreshTriggerPoller, 150*time.Millisecond, 0, 500, 0)

	snap := tel.Snapshot(false)
	output := formatByWorkspace(snap)

	// Should have both workspaces
	if !strings.Contains(output, "workspace=ws-1") {
		t.Error("expected ws-1 in output")
	}
	if !strings.Contains(output, "workspace=ws-2") {
		t.Error("expected ws-2 in output")
	}
	// ws-1 should show 3 commands
	if !strings.Contains(output, "commands=3") {
		t.Error("expected commands=3 for ws-1")
	}
	// ws-2 should show 1 command
	if !strings.Contains(output, "commands=1") {
		t.Error("expected commands=1 for ws-2")
	}
}

func TestIOWorkspaceDiagnosticCapture_CommandRingbufferFormat(t *testing.T) {
	tel := NewIOWorkspaceTelemetry()
	tel.RecordCommand("git", []string{"fetch"}, "ws-1", "/tmp/ws1", RefreshTriggerPoller, 200*time.Millisecond, 0, 1000, 0)

	snap := tel.Snapshot(false)
	output := formatCommandEntries(snap.AllCommands)

	// Each line should have: timestamp, duration, command, workspace
	lines := strings.Split(strings.TrimSpace(output), "\n")
	if len(lines) != 1 {
		t.Fatalf("expected 1 line, got %d", len(lines))
	}
	line := lines[0]
	if !strings.Contains(line, "ms") {
		t.Error("expected duration in ms")
	}
	if !strings.Contains(line, "git fetch") {
		t.Error("expected 'git fetch' in line")
	}
	if !strings.Contains(line, "workspace=ws-1") {
		t.Error("expected workspace=ws-1 in line")
	}
}
