package provision

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestEnsureAgentInstructions_CreatesNewFile(t *testing.T) {
	tmpDir := t.TempDir()

	err := EnsureAgentInstructions(tmpDir, "claude")
	if err != nil {
		t.Fatalf("EnsureAgentInstructions failed: %v", err)
	}

	// Check that the file was created
	instructionPath := filepath.Join(tmpDir, ".claude", "CLAUDE.md")
	content, err := os.ReadFile(instructionPath)
	if err != nil {
		t.Fatalf("Failed to read instruction file: %v", err)
	}

	// Check that it contains the schmux markers
	if !strings.Contains(string(content), schmuxMarkerStart) {
		t.Error("File should contain SCHMUX:BEGIN marker")
	}
	if !strings.Contains(string(content), schmuxMarkerEnd) {
		t.Error("File should contain SCHMUX:END marker")
	}
	if !strings.Contains(string(content), "$SCHMUX_STATUS_FILE") {
		t.Error("File should contain signaling instructions")
	}
}

func TestEnsureAgentInstructions_AppendsToExisting(t *testing.T) {
	tmpDir := t.TempDir()

	// Create existing instruction file
	instructionDir := filepath.Join(tmpDir, ".claude")
	if err := os.MkdirAll(instructionDir, 0755); err != nil {
		t.Fatal(err)
	}
	instructionPath := filepath.Join(instructionDir, "CLAUDE.md")
	existingContent := "# My Project\n\nExisting instructions here.\n"
	if err := os.WriteFile(instructionPath, []byte(existingContent), 0644); err != nil {
		t.Fatal(err)
	}

	err := EnsureAgentInstructions(tmpDir, "claude")
	if err != nil {
		t.Fatalf("EnsureAgentInstructions failed: %v", err)
	}

	content, err := os.ReadFile(instructionPath)
	if err != nil {
		t.Fatal(err)
	}

	// Check that original content is preserved
	if !strings.Contains(string(content), "My Project") {
		t.Error("Original content should be preserved")
	}
	if !strings.Contains(string(content), "Existing instructions here") {
		t.Error("Original content should be preserved")
	}

	// Check that schmux block was appended
	if !strings.Contains(string(content), schmuxMarkerStart) {
		t.Error("File should contain SCHMUX:BEGIN marker")
	}
}

func TestEnsureAgentInstructions_UpdatesExisting(t *testing.T) {
	tmpDir := t.TempDir()

	// Create existing instruction file with old schmux block
	instructionDir := filepath.Join(tmpDir, ".claude")
	if err := os.MkdirAll(instructionDir, 0755); err != nil {
		t.Fatal(err)
	}
	instructionPath := filepath.Join(instructionDir, "CLAUDE.md")
	existingContent := "# My Project\n\n" + schmuxMarkerStart + "\nOld content\n" + schmuxMarkerEnd + "\n"
	if err := os.WriteFile(instructionPath, []byte(existingContent), 0644); err != nil {
		t.Fatal(err)
	}

	err := EnsureAgentInstructions(tmpDir, "claude")
	if err != nil {
		t.Fatalf("EnsureAgentInstructions failed: %v", err)
	}

	content, err := os.ReadFile(instructionPath)
	if err != nil {
		t.Fatal(err)
	}

	// Check that old content was replaced
	if strings.Contains(string(content), "Old content") {
		t.Error("Old schmux content should be replaced")
	}

	// Check that new content is present
	if !strings.Contains(string(content), "$SCHMUX_STATUS_FILE") {
		t.Error("New signaling instructions should be present")
	}

	// Should only have one set of markers
	if strings.Count(string(content), schmuxMarkerStart) != 1 {
		t.Error("Should have exactly one SCHMUX:BEGIN marker")
	}
}

func TestEnsureAgentInstructions_DifferentAgents(t *testing.T) {
	tests := []struct {
		target       string
		expectedDir  string
		expectedFile string
	}{
		{"claude", ".claude", "CLAUDE.md"},
		{"codex", ".codex", "AGENTS.md"},
		{"gemini", ".gemini", "GEMINI.md"},
		{"claude-opus", ".claude", "CLAUDE.md"},   // Model should use base tool
		{"claude-sonnet", ".claude", "CLAUDE.md"}, // Model should use base tool
	}

	for _, tt := range tests {
		t.Run(tt.target, func(t *testing.T) {
			tmpDir := t.TempDir()

			err := EnsureAgentInstructions(tmpDir, tt.target)
			if err != nil {
				t.Fatalf("EnsureAgentInstructions failed: %v", err)
			}

			instructionPath := filepath.Join(tmpDir, tt.expectedDir, tt.expectedFile)
			if _, err := os.Stat(instructionPath); os.IsNotExist(err) {
				t.Errorf("Expected instruction file at %s", instructionPath)
			}
		})
	}
}

func TestEnsureAgentInstructions_UnknownTarget(t *testing.T) {
	tmpDir := t.TempDir()

	// Unknown target should not create any files
	err := EnsureAgentInstructions(tmpDir, "unknown-agent")
	if err != nil {
		t.Fatalf("EnsureAgentInstructions should not error for unknown target: %v", err)
	}

	// No files should be created
	entries, _ := os.ReadDir(tmpDir)
	if len(entries) != 0 {
		t.Error("No files should be created for unknown target")
	}
}

func TestRemoveAgentInstructions(t *testing.T) {
	tmpDir := t.TempDir()

	// First ensure instructions exist
	if err := EnsureAgentInstructions(tmpDir, "claude"); err != nil {
		t.Fatal(err)
	}

	// Verify they exist
	if !HasSignalingInstructions(tmpDir, "claude") {
		t.Fatal("Instructions should exist after EnsureAgentInstructions")
	}

	// Remove them
	if err := RemoveAgentInstructions(tmpDir, "claude"); err != nil {
		t.Fatalf("RemoveAgentInstructions failed: %v", err)
	}

	// Verify they're gone (file should be removed since it was only the schmux block)
	instructionPath := filepath.Join(tmpDir, ".claude", "CLAUDE.md")
	if _, err := os.Stat(instructionPath); !os.IsNotExist(err) {
		t.Error("Instruction file should be removed when it only contained schmux block")
	}
}

func TestRemoveAgentInstructions_PreservesOtherContent(t *testing.T) {
	tmpDir := t.TempDir()

	// Create file with both user content and schmux block
	instructionDir := filepath.Join(tmpDir, ".claude")
	if err := os.MkdirAll(instructionDir, 0755); err != nil {
		t.Fatal(err)
	}
	instructionPath := filepath.Join(instructionDir, "CLAUDE.md")
	content := "# My Project\n\nUser content here.\n\n" + buildSchmuxBlock()
	if err := os.WriteFile(instructionPath, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	// Remove schmux block
	if err := RemoveAgentInstructions(tmpDir, "claude"); err != nil {
		t.Fatal(err)
	}

	// File should still exist with user content
	newContent, err := os.ReadFile(instructionPath)
	if err != nil {
		t.Fatal("File should still exist after removing schmux block")
	}

	if !strings.Contains(string(newContent), "User content here") {
		t.Error("User content should be preserved")
	}
	if strings.Contains(string(newContent), schmuxMarkerStart) {
		t.Error("Schmux block should be removed")
	}
}

func TestHasSignalingInstructions(t *testing.T) {
	tmpDir := t.TempDir()

	// Should be false initially
	if HasSignalingInstructions(tmpDir, "claude") {
		t.Error("Should be false before adding instructions")
	}

	// Add instructions
	if err := EnsureAgentInstructions(tmpDir, "claude"); err != nil {
		t.Fatal(err)
	}

	// Should be true now
	if !HasSignalingInstructions(tmpDir, "claude") {
		t.Error("Should be true after adding instructions")
	}
}

func TestEnsureSignalingInstructionsFile(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("HOME", tmpHome)

	if err := EnsureSignalingInstructionsFile(); err != nil {
		t.Fatalf("EnsureSignalingInstructionsFile failed: %v", err)
	}

	path := filepath.Join(tmpHome, ".schmux", "signaling.md")
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("failed to read signaling file: %v", err)
	}

	if string(content) != SignalingInstructions {
		t.Fatal("signaling file content mismatch")
	}
}

func TestSupportsHooks(t *testing.T) {
	tests := []struct {
		tool     string
		expected bool
	}{
		{"claude", true},
		{"codex", false},
		{"gemini", false},
		{"unknown", false},
		{"", false},
	}
	for _, tt := range tests {
		t.Run(tt.tool, func(t *testing.T) {
			if got := SupportsHooks(tt.tool); got != tt.expected {
				t.Errorf("SupportsHooks(%q) = %v, want %v", tt.tool, got, tt.expected)
			}
		})
	}
}

func TestEnsureClaudeHooks_CreatesNewFile(t *testing.T) {
	tmpDir := t.TempDir()

	if err := EnsureClaudeHooks(tmpDir); err != nil {
		t.Fatalf("EnsureClaudeHooks failed: %v", err)
	}

	settingsPath := filepath.Join(tmpDir, ".claude", "settings.local.json")
	content, err := os.ReadFile(settingsPath)
	if err != nil {
		t.Fatalf("Failed to read settings file: %v", err)
	}

	// Parse as JSON to verify structure
	var settings map[string]json.RawMessage
	if err := json.Unmarshal(content, &settings); err != nil {
		t.Fatalf("Invalid JSON: %v", err)
	}

	// Check hooks key exists
	hooksRaw, ok := settings["hooks"]
	if !ok {
		t.Fatal("Settings should contain hooks key")
	}

	// Parse hooks
	var hooks map[string]json.RawMessage
	if err := json.Unmarshal(hooksRaw, &hooks); err != nil {
		t.Fatalf("Invalid hooks JSON: %v", err)
	}

	// Verify all expected events are present
	for _, event := range []string{"SessionStart", "UserPromptSubmit", "Stop", "Notification"} {
		if _, ok := hooks[event]; !ok {
			t.Errorf("Missing hook event: %s", event)
		}
	}

	// Verify the commands reference SCHMUX_STATUS_FILE
	contentStr := string(content)
	if !strings.Contains(contentStr, "SCHMUX_STATUS_FILE") {
		t.Error("Hooks should reference SCHMUX_STATUS_FILE")
	}

	// Verify state signals are present in the command strings
	for _, state := range []string{"working", "completed", "needs_input"} {
		if !strings.Contains(contentStr, state) {
			t.Errorf("Hooks should contain state %q", state)
		}
	}
}

func TestEnsureClaudeHooks_PreservesOtherSettings(t *testing.T) {
	tmpDir := t.TempDir()

	// Create existing settings file with other settings
	settingsDir := filepath.Join(tmpDir, ".claude")
	if err := os.MkdirAll(settingsDir, 0755); err != nil {
		t.Fatal(err)
	}
	settingsPath := filepath.Join(settingsDir, "settings.local.json")
	existing := `{"permissions": {"allow": ["Read"]}, "other_key": "value"}`
	if err := os.WriteFile(settingsPath, []byte(existing), 0644); err != nil {
		t.Fatal(err)
	}

	if err := EnsureClaudeHooks(tmpDir); err != nil {
		t.Fatalf("EnsureClaudeHooks failed: %v", err)
	}

	content, err := os.ReadFile(settingsPath)
	if err != nil {
		t.Fatal(err)
	}

	var settings map[string]json.RawMessage
	if err := json.Unmarshal(content, &settings); err != nil {
		t.Fatalf("Invalid JSON: %v", err)
	}

	// Check hooks were added
	if _, ok := settings["hooks"]; !ok {
		t.Error("hooks should be present")
	}

	// Check other settings preserved
	if _, ok := settings["permissions"]; !ok {
		t.Error("permissions should be preserved")
	}
	if _, ok := settings["other_key"]; !ok {
		t.Error("other_key should be preserved")
	}
}

func TestEnsureClaudeHooks_Idempotent(t *testing.T) {
	tmpDir := t.TempDir()

	// Run twice
	if err := EnsureClaudeHooks(tmpDir); err != nil {
		t.Fatalf("First call failed: %v", err)
	}
	content1, _ := os.ReadFile(filepath.Join(tmpDir, ".claude", "settings.local.json"))

	if err := EnsureClaudeHooks(tmpDir); err != nil {
		t.Fatalf("Second call failed: %v", err)
	}
	content2, _ := os.ReadFile(filepath.Join(tmpDir, ".claude", "settings.local.json"))

	if string(content1) != string(content2) {
		t.Error("EnsureClaudeHooks should be idempotent")
	}
}

func TestClaudeHooksJSON(t *testing.T) {
	jsonBytes, err := ClaudeHooksJSON()
	if err != nil {
		t.Fatalf("ClaudeHooksJSON failed: %v", err)
	}

	// Should be valid JSON
	var parsed map[string]interface{}
	if err := json.Unmarshal(jsonBytes, &parsed); err != nil {
		t.Fatalf("ClaudeHooksJSON returned invalid JSON: %v", err)
	}

	// Should have hooks key
	if _, ok := parsed["hooks"]; !ok {
		t.Error("Should contain hooks key")
	}

	// Should not contain single quotes (safe for shell wrapping)
	if strings.Contains(string(jsonBytes), "'") {
		t.Error("JSON should not contain single quotes")
	}
}

func TestWrapCommandWithHooksProvisioning(t *testing.T) {
	wrapped, err := WrapCommandWithHooksProvisioning(`claude "hello world"`)
	if err != nil {
		t.Fatalf("WrapCommandWithHooksProvisioning failed: %v", err)
	}

	// Should start with mkdir
	if !strings.HasPrefix(wrapped, "mkdir -p .claude") {
		t.Error("Should start with mkdir -p .claude")
	}

	// Should contain the original command
	if !strings.Contains(wrapped, `claude "hello world"`) {
		t.Error("Should contain the original command")
	}

	// Should contain settings.local.json creation
	if !strings.Contains(wrapped, "settings.local.json") {
		t.Error("Should create settings.local.json")
	}

	// Should contain hooks config
	if !strings.Contains(wrapped, "hooks") {
		t.Error("Should contain hooks configuration")
	}

	// Commands should be chained with &&
	if !strings.Contains(wrapped, " && ") {
		t.Error("Commands should be chained with &&")
	}
}

func TestClaudeHooksNotificationMatcher(t *testing.T) {
	jsonBytes, err := ClaudeHooksJSON()
	if err != nil {
		t.Fatal(err)
	}

	content := string(jsonBytes)

	// Verify Notification hook matches the right event types
	for _, notifType := range []string{"permission_prompt", "idle_prompt", "elicitation_dialog"} {
		if !strings.Contains(content, notifType) {
			t.Errorf("Notification hook should match %q", notifType)
		}
	}
}
