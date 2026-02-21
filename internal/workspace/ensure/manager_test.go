package ensure

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestAgentInstructions_CreatesNewFile(t *testing.T) {
	tmpDir := t.TempDir()

	err := AgentInstructions(tmpDir, "claude")
	if err != nil {
		t.Fatalf("AgentInstructions failed: %v", err)
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
	if !strings.Contains(string(content), "$SCHMUX_EVENTS_FILE") {
		t.Error("File should contain signaling instructions")
	}
}

func TestAgentInstructions_AppendsToExisting(t *testing.T) {
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

	err := AgentInstructions(tmpDir, "claude")
	if err != nil {
		t.Fatalf("AgentInstructions failed: %v", err)
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

func TestAgentInstructions_UpdatesExisting(t *testing.T) {
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

	err := AgentInstructions(tmpDir, "claude")
	if err != nil {
		t.Fatalf("AgentInstructions failed: %v", err)
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
	if !strings.Contains(string(content), "$SCHMUX_EVENTS_FILE") {
		t.Error("New signaling instructions should be present")
	}

	// Should only have one set of markers
	if strings.Count(string(content), schmuxMarkerStart) != 1 {
		t.Error("Should have exactly one SCHMUX:BEGIN marker")
	}
}

func TestAgentInstructions_DifferentAgents(t *testing.T) {
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

			err := AgentInstructions(tmpDir, tt.target)
			if err != nil {
				t.Fatalf("AgentInstructions failed: %v", err)
			}

			instructionPath := filepath.Join(tmpDir, tt.expectedDir, tt.expectedFile)
			if _, err := os.Stat(instructionPath); os.IsNotExist(err) {
				t.Errorf("Expected instruction file at %s", instructionPath)
			}
		})
	}
}

func TestAgentInstructions_UnknownTarget(t *testing.T) {
	tmpDir := t.TempDir()

	// Unknown target should not create any files
	err := AgentInstructions(tmpDir, "unknown-agent")
	if err != nil {
		t.Fatalf("AgentInstructions should not error for unknown target: %v", err)
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
	if err := AgentInstructions(tmpDir, "claude"); err != nil {
		t.Fatal(err)
	}

	// Verify they exist
	if !HasSignalingInstructions(tmpDir, "claude") {
		t.Fatal("Instructions should exist after AgentInstructions")
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
	if err := AgentInstructions(tmpDir, "claude"); err != nil {
		t.Fatal(err)
	}

	// Should be true now
	if !HasSignalingInstructions(tmpDir, "claude") {
		t.Error("Should be true after adding instructions")
	}
}

func TestSignalingInstructionsFile(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("HOME", tmpHome)

	if err := SignalingInstructionsFile(); err != nil {
		t.Fatalf("SignalingInstructionsFile failed: %v", err)
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

func TestClaudeHooks_CreatesNewFile(t *testing.T) {
	tmpDir := t.TempDir()

	if err := ClaudeHooks(tmpDir, t.TempDir(), true); err != nil {
		t.Fatalf("ClaudeHooks failed: %v", err)
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
	for _, event := range []string{"SessionStart", "SessionEnd", "UserPromptSubmit", "Stop", "Notification"} {
		if _, ok := hooks[event]; !ok {
			t.Errorf("Missing hook event: %s", event)
		}
	}

	// Verify the commands reference SCHMUX_STATUS_FILE
	contentStr := string(content)
	if !strings.Contains(contentStr, "SCHMUX_STATUS_FILE") {
		t.Error("Hooks should reference SCHMUX_STATUS_FILE")
	}

	// Verify the commands also reference SCHMUX_EVENTS_FILE (dual-write)
	if !strings.Contains(contentStr, "SCHMUX_EVENTS_FILE") {
		t.Error("Hooks should reference SCHMUX_EVENTS_FILE for dual-write")
	}

	// Verify state signals are present in the command strings
	for _, state := range []string{"working", "completed", "needs_input"} {
		if !strings.Contains(contentStr, state) {
			t.Errorf("Hooks should contain state %q", state)
		}
	}
}

func TestClaudeHooks_PreservesOtherSettings(t *testing.T) {
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

	if err := ClaudeHooks(tmpDir, "/tmp/test-hooks", true); err != nil {
		t.Fatalf("ClaudeHooks failed: %v", err)
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

func TestClaudeHooks_Idempotent(t *testing.T) {
	tmpDir := t.TempDir()

	// Run twice
	if err := ClaudeHooks(tmpDir, "/tmp/test-hooks", true); err != nil {
		t.Fatalf("First call failed: %v", err)
	}
	content1, _ := os.ReadFile(filepath.Join(tmpDir, ".claude", "settings.local.json"))

	if err := ClaudeHooks(tmpDir, "/tmp/test-hooks", true); err != nil {
		t.Fatalf("Second call failed: %v", err)
	}
	content2, _ := os.ReadFile(filepath.Join(tmpDir, ".claude", "settings.local.json"))

	if string(content1) != string(content2) {
		t.Error("ClaudeHooks should be idempotent")
	}
}

func TestClaudeHooksJSON(t *testing.T) {
	jsonBytes, err := ClaudeHooksJSON("/tmp/test-hooks")
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

func TestWrapCommandWithHooks(t *testing.T) {
	wrapped, err := WrapCommandWithHooks(`claude "hello world"`, "/tmp/test-hooks")
	if err != nil {
		t.Fatalf("WrapCommandWithHooks failed: %v", err)
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
	jsonBytes, err := ClaudeHooksJSON("/tmp/test-hooks")
	if err != nil {
		t.Fatal(err)
	}

	content := string(jsonBytes)

	// Verify Notification hook matches the right event types
	for _, notifType := range []string{"permission_prompt", "elicitation_dialog"} {
		if !strings.Contains(content, notifType) {
			t.Errorf("Notification hook should match %q", notifType)
		}
	}

	// idle_prompt should NOT be matched — it fires when the agent is just
	// waiting for input, which is normal idle state, not "needs attention"
	if strings.Contains(content, "idle_prompt") {
		t.Error("Notification hook should NOT match idle_prompt")
	}
}

func TestClaudeHooks_MergesWithExistingHooks(t *testing.T) {
	tmpDir := t.TempDir()

	// Create existing settings with user-defined hooks
	settingsDir := filepath.Join(tmpDir, ".claude")
	if err := os.MkdirAll(settingsDir, 0755); err != nil {
		t.Fatal(err)
	}
	settingsPath := filepath.Join(settingsDir, "settings.local.json")
	existing := `{
		"hooks": {
			"Stop": [
				{
					"hooks": [
						{
							"type": "command",
							"command": "echo user-stop-hook",
							"statusMessage": "user: my stop hook"
						}
					]
				}
			],
			"PostToolUse": [
				{
					"matcher": "Write|Edit",
					"hooks": [
						{
							"type": "command",
							"command": "echo lint-check"
						}
					]
				}
			]
		}
	}`
	if err := os.WriteFile(settingsPath, []byte(existing), 0644); err != nil {
		t.Fatal(err)
	}

	if err := ClaudeHooks(tmpDir, "/tmp/test-hooks", true); err != nil {
		t.Fatalf("ClaudeHooks failed: %v", err)
	}

	content, err := os.ReadFile(settingsPath)
	if err != nil {
		t.Fatal(err)
	}

	var settings map[string]json.RawMessage
	if err := json.Unmarshal(content, &settings); err != nil {
		t.Fatalf("Invalid JSON: %v", err)
	}

	var hooks map[string][]claudeHookMatcherGroup
	if err := json.Unmarshal(settings["hooks"], &hooks); err != nil {
		t.Fatalf("Invalid hooks JSON: %v", err)
	}

	// User's Stop hook should be preserved alongside schmux's
	stopGroups := hooks["Stop"]
	if len(stopGroups) != 3 {
		t.Fatalf("Stop should have 3 matcher groups (user + 2 schmux), got %d", len(stopGroups))
	}
	// First should be the user's hook (preserved order)
	if stopGroups[0].Hooks[0].Command != "echo user-stop-hook" {
		t.Error("User's Stop hook should be preserved")
	}
	// Second and third should be schmux's
	if !strings.HasPrefix(stopGroups[1].Hooks[0].StatusMessage, "schmux:") {
		t.Error("Schmux Stop hook should be appended")
	}

	// User's PostToolUse hook (event not managed by schmux) should be preserved
	postToolGroups := hooks["PostToolUse"]
	if len(postToolGroups) != 1 {
		t.Fatalf("PostToolUse should have 1 matcher group, got %d", len(postToolGroups))
	}
	if postToolGroups[0].Hooks[0].Command != "echo lint-check" {
		t.Error("User's PostToolUse hook should be preserved")
	}

	// Schmux events should all be present
	for _, event := range []string{"SessionStart", "SessionEnd", "UserPromptSubmit", "Stop", "Notification"} {
		if _, ok := hooks[event]; !ok {
			t.Errorf("Missing schmux hook event: %s", event)
		}
	}
}

func TestClaudeHooks_ReplacesOldSchmuxHooks(t *testing.T) {
	tmpDir := t.TempDir()

	// First provisioning
	if err := ClaudeHooks(tmpDir, "/tmp/test-hooks", true); err != nil {
		t.Fatal(err)
	}

	// Manually add a user hook to the Stop event alongside the schmux one
	settingsPath := filepath.Join(tmpDir, ".claude", "settings.local.json")
	content, _ := os.ReadFile(settingsPath)

	var settings map[string]json.RawMessage
	json.Unmarshal(content, &settings)

	var hooks map[string][]claudeHookMatcherGroup
	json.Unmarshal(settings["hooks"], &hooks)

	// Add user hook to Stop
	hooks["Stop"] = append([]claudeHookMatcherGroup{
		{
			Hooks: []claudeHookHandler{
				{Type: "command", Command: "echo user-hook", StatusMessage: "user: custom"},
			},
		},
	}, hooks["Stop"]...)

	hooksJSON, _ := json.Marshal(hooks)
	settings["hooks"] = json.RawMessage(hooksJSON)
	data, _ := json.MarshalIndent(settings, "", "  ")
	os.WriteFile(settingsPath, data, 0644)

	// Second provisioning should replace schmux hooks but keep user hook
	if err := ClaudeHooks(tmpDir, "/tmp/test-hooks", true); err != nil {
		t.Fatal(err)
	}

	content, _ = os.ReadFile(settingsPath)
	json.Unmarshal(content, &settings)
	json.Unmarshal(settings["hooks"], &hooks)

	stopGroups := hooks["Stop"]
	if len(stopGroups) != 3 {
		t.Fatalf("Stop should have 3 groups (user + 2 schmux), got %d", len(stopGroups))
	}

	// User hook preserved
	if stopGroups[0].Hooks[0].Command != "echo user-hook" {
		t.Errorf("User hook should be preserved, got %q", stopGroups[0].Hooks[0].Command)
	}

	// Schmux hooks present (not duplicated)
	schmuxCount := 0
	for _, g := range stopGroups {
		if isSchmuxMatcherGroup(g) {
			schmuxCount++
		}
	}
	if schmuxCount != 2 {
		t.Errorf("Should have exactly 2 schmux Stop groups (status + lore), got %d", schmuxCount)
	}
}

func TestIsSchmuxMatcherGroup(t *testing.T) {
	tests := []struct {
		name     string
		group    claudeHookMatcherGroup
		expected bool
	}{
		{
			name: "schmux hook identified by prefix",
			group: claudeHookMatcherGroup{
				Hooks: []claudeHookHandler{
					{Type: "command", Command: "echo test", StatusMessage: "schmux: signaling"},
				},
			},
			expected: true,
		},
		{
			name: "user hook without schmux prefix",
			group: claudeHookMatcherGroup{
				Hooks: []claudeHookHandler{
					{Type: "command", Command: "echo test", StatusMessage: "user: my hook"},
				},
			},
			expected: false,
		},
		{
			name: "hook with empty statusMessage",
			group: claudeHookMatcherGroup{
				Hooks: []claudeHookHandler{
					{Type: "command", Command: "echo test", StatusMessage: ""},
				},
			},
			expected: false,
		},
		{
			name: "hook with no statusMessage field",
			group: claudeHookMatcherGroup{
				Hooks: []claudeHookHandler{
					{Type: "command", Command: "echo test"},
				},
			},
			expected: false,
		},
		{
			name: "multiple handlers one is schmux",
			group: claudeHookMatcherGroup{
				Hooks: []claudeHookHandler{
					{Type: "command", Command: "echo first", StatusMessage: "user: hook"},
					{Type: "command", Command: "echo second", StatusMessage: "schmux: signaling"},
				},
			},
			expected: true,
		},
		{
			name: "empty hooks array",
			group: claudeHookMatcherGroup{
				Hooks: []claudeHookHandler{},
			},
			expected: false,
		},
		{
			name: "statusMessage contains schmux but not as prefix",
			group: claudeHookMatcherGroup{
				Hooks: []claudeHookHandler{
					{Type: "command", Command: "echo test", StatusMessage: "not schmux: foo"},
				},
			},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isSchmuxMatcherGroup(tt.group); got != tt.expected {
				t.Errorf("isSchmuxMatcherGroup() = %v, want %v", got, tt.expected)
			}
		})
	}
}

func TestMergeHooksForEvent(t *testing.T) {
	schmuxGroup := claudeHookMatcherGroup{
		Hooks: []claudeHookHandler{
			{Type: "command", Command: "echo completed", StatusMessage: "schmux: signaling"},
		},
	}
	userGroup := claudeHookMatcherGroup{
		Hooks: []claudeHookHandler{
			{Type: "command", Command: "echo user-stop", StatusMessage: "user: custom"},
		},
	}
	userGroupNoStatus := claudeHookMatcherGroup{
		Matcher: "Write",
		Hooks: []claudeHookHandler{
			{Type: "command", Command: "echo lint"},
		},
	}
	oldSchmuxGroup := claudeHookMatcherGroup{
		Hooks: []claudeHookHandler{
			{Type: "command", Command: "echo old-schmux", StatusMessage: "schmux: old"},
		},
	}

	t.Run("empty existing adds schmux", func(t *testing.T) {
		result := mergeHooksForEvent(nil, []claudeHookMatcherGroup{schmuxGroup})
		if len(result) != 1 {
			t.Fatalf("expected 1 group, got %d", len(result))
		}
		if result[0].Hooks[0].Command != "echo completed" {
			t.Error("should contain schmux group")
		}
	})

	t.Run("user hooks preserved alongside schmux", func(t *testing.T) {
		result := mergeHooksForEvent(
			[]claudeHookMatcherGroup{userGroup},
			[]claudeHookMatcherGroup{schmuxGroup},
		)
		if len(result) != 2 {
			t.Fatalf("expected 2 groups, got %d", len(result))
		}
		if result[0].Hooks[0].Command != "echo user-stop" {
			t.Error("user hook should come first")
		}
		if result[1].Hooks[0].Command != "echo completed" {
			t.Error("schmux hook should come second")
		}
	})

	t.Run("old schmux hooks replaced by new", func(t *testing.T) {
		result := mergeHooksForEvent(
			[]claudeHookMatcherGroup{oldSchmuxGroup},
			[]claudeHookMatcherGroup{schmuxGroup},
		)
		if len(result) != 1 {
			t.Fatalf("expected 1 group (old removed, new added), got %d", len(result))
		}
		if result[0].Hooks[0].Command != "echo completed" {
			t.Errorf("should have new schmux command, got %q", result[0].Hooks[0].Command)
		}
	})

	t.Run("user + old schmux replaced correctly", func(t *testing.T) {
		result := mergeHooksForEvent(
			[]claudeHookMatcherGroup{userGroup, oldSchmuxGroup, userGroupNoStatus},
			[]claudeHookMatcherGroup{schmuxGroup},
		)
		if len(result) != 3 {
			t.Fatalf("expected 3 groups (2 user + 1 schmux), got %d", len(result))
		}
		// User hooks preserved in order
		if result[0].Hooks[0].Command != "echo user-stop" {
			t.Error("first user hook should be preserved")
		}
		if result[1].Hooks[0].Command != "echo lint" {
			t.Error("second user hook should be preserved")
		}
		// Schmux hook appended
		if result[2].Hooks[0].Command != "echo completed" {
			t.Error("schmux hook should be appended at end")
		}
	})

	t.Run("multiple old schmux groups all removed", func(t *testing.T) {
		anotherOldSchmux := claudeHookMatcherGroup{
			Hooks: []claudeHookHandler{
				{Type: "command", Command: "echo also-old", StatusMessage: "schmux: another"},
			},
		}
		result := mergeHooksForEvent(
			[]claudeHookMatcherGroup{oldSchmuxGroup, userGroup, anotherOldSchmux},
			[]claudeHookMatcherGroup{schmuxGroup},
		)
		if len(result) != 2 {
			t.Fatalf("expected 2 groups (1 user + 1 new schmux), got %d", len(result))
		}
		schmuxCount := 0
		for _, g := range result {
			if isSchmuxMatcherGroup(g) {
				schmuxCount++
			}
		}
		if schmuxCount != 1 {
			t.Errorf("should have exactly 1 schmux group, got %d", schmuxCount)
		}
	})

	t.Run("empty schmux input just filters old schmux", func(t *testing.T) {
		result := mergeHooksForEvent(
			[]claudeHookMatcherGroup{userGroup, oldSchmuxGroup},
			nil,
		)
		if len(result) != 1 {
			t.Fatalf("expected 1 group (user only), got %d", len(result))
		}
		if result[0].Hooks[0].Command != "echo user-stop" {
			t.Error("only user hook should remain")
		}
	})
}

func TestClaudeHooks_CleansStaleSchmuxEvents(t *testing.T) {
	tmpDir := t.TempDir()

	// Create settings with a schmux hook on an event that schmux no longer manages
	settingsDir := filepath.Join(tmpDir, ".claude")
	if err := os.MkdirAll(settingsDir, 0755); err != nil {
		t.Fatal(err)
	}
	settingsPath := filepath.Join(settingsDir, "settings.local.json")
	existing := `{
		"hooks": {
			"PreToolUse": [
				{
					"matcher": "Bash",
					"hooks": [
						{
							"type": "command",
							"command": "echo stale-schmux",
							"statusMessage": "schmux: old stale hook"
						}
					]
				},
				{
					"matcher": "Write",
					"hooks": [
						{
							"type": "command",
							"command": "echo user-pretool"
						}
					]
				}
			]
		}
	}`
	if err := os.WriteFile(settingsPath, []byte(existing), 0644); err != nil {
		t.Fatal(err)
	}

	if err := ClaudeHooks(tmpDir, "/tmp/test-hooks", true); err != nil {
		t.Fatal(err)
	}

	content, _ := os.ReadFile(settingsPath)
	var settings map[string]json.RawMessage
	json.Unmarshal(content, &settings)

	var hooks map[string][]claudeHookMatcherGroup
	json.Unmarshal(settings["hooks"], &hooks)

	// Stale schmux hook on PreToolUse should be removed
	preToolGroups := hooks["PreToolUse"]
	if len(preToolGroups) != 1 {
		t.Fatalf("PreToolUse should have 1 group (user only, stale schmux removed), got %d", len(preToolGroups))
	}
	if preToolGroups[0].Hooks[0].Command != "echo user-pretool" {
		t.Error("User's PreToolUse hook should be preserved")
	}
}

func TestClaudeHooks_RemovesEventWithOnlyStaleSchmux(t *testing.T) {
	tmpDir := t.TempDir()

	// Create settings with a schmux-only hook on an event schmux no longer manages
	settingsDir := filepath.Join(tmpDir, ".claude")
	if err := os.MkdirAll(settingsDir, 0755); err != nil {
		t.Fatal(err)
	}
	settingsPath := filepath.Join(settingsDir, "settings.local.json")
	existing := `{
		"hooks": {
			"PreToolUse": [
				{
					"hooks": [
						{
							"type": "command",
							"command": "echo stale",
							"statusMessage": "schmux: stale"
						}
					]
				}
			]
		}
	}`
	if err := os.WriteFile(settingsPath, []byte(existing), 0644); err != nil {
		t.Fatal(err)
	}

	if err := ClaudeHooks(tmpDir, "/tmp/test-hooks", true); err != nil {
		t.Fatal(err)
	}

	content, _ := os.ReadFile(settingsPath)
	var settings map[string]json.RawMessage
	json.Unmarshal(content, &settings)

	var hooks map[string][]claudeHookMatcherGroup
	json.Unmarshal(settings["hooks"], &hooks)

	// PreToolUse should be gone entirely (only had stale schmux hooks)
	if groups, ok := hooks["PreToolUse"]; ok {
		t.Errorf("PreToolUse should be removed when only stale schmux hooks, got %d groups", len(groups))
	}
}

func TestClaudeHooks_MalformedExistingHooks(t *testing.T) {
	tmpDir := t.TempDir()

	// Create settings with malformed hooks value
	settingsDir := filepath.Join(tmpDir, ".claude")
	if err := os.MkdirAll(settingsDir, 0755); err != nil {
		t.Fatal(err)
	}
	settingsPath := filepath.Join(settingsDir, "settings.local.json")
	existing := `{"hooks": "not-an-object", "other": "preserved"}`
	if err := os.WriteFile(settingsPath, []byte(existing), 0644); err != nil {
		t.Fatal(err)
	}

	// Should not error, just start fresh for hooks
	if err := ClaudeHooks(tmpDir, "/tmp/test-hooks", true); err != nil {
		t.Fatalf("ClaudeHooks should handle malformed hooks: %v", err)
	}

	content, _ := os.ReadFile(settingsPath)
	var settings map[string]json.RawMessage
	json.Unmarshal(content, &settings)

	// Hooks should now be valid
	var hooks map[string][]claudeHookMatcherGroup
	if err := json.Unmarshal(settings["hooks"], &hooks); err != nil {
		t.Fatalf("hooks should be valid JSON after recovery: %v", err)
	}

	// Schmux hooks should be present
	if _, ok := hooks["Stop"]; !ok {
		t.Error("Stop hook should be present after recovery")
	}

	// Other settings preserved
	if _, ok := settings["other"]; !ok {
		t.Error("Other settings should be preserved")
	}
}

func TestDualWriteCommand(t *testing.T) {
	cmd := dualWriteCommand("needs_input")

	// Should contain the state
	if !strings.Contains(cmd, "needs_input") {
		t.Error("Command should contain the signal state")
	}

	// Should reference SCHMUX_STATUS_FILE (signal file)
	if !strings.Contains(cmd, "SCHMUX_STATUS_FILE") {
		t.Error("Command should reference SCHMUX_STATUS_FILE")
	}

	// Should reference SCHMUX_EVENTS_FILE (event file)
	if !strings.Contains(cmd, "SCHMUX_EVENTS_FILE") {
		t.Error("Command should reference SCHMUX_EVENTS_FILE")
	}

	// Should fall back gracefully (|| true)
	if !strings.Contains(cmd, "|| true") {
		t.Error("Command should fall back gracefully")
	}
}

func TestDualWriteCommandWithIntent(t *testing.T) {
	tests := []struct {
		state  string
		field  string
		wantJq string
	}{
		{"working", "prompt", ".prompt"},
		{"needs_input", "message", ".message"},
		{"error", "error", ".error"},
	}
	for _, tt := range tests {
		t.Run(tt.state+"_"+tt.field, func(t *testing.T) {
			cmd := dualWriteCommandWithIntent(tt.state, tt.field)
			if !strings.Contains(cmd, tt.state) {
				t.Errorf("Command should contain state %q", tt.state)
			}
			if !strings.Contains(cmd, tt.wantJq) {
				t.Errorf("Command should contain jq field %q", tt.wantJq)
			}
			if !strings.Contains(cmd, "SCHMUX_STATUS_FILE") {
				t.Error("Command should reference SCHMUX_STATUS_FILE")
			}
			if !strings.Contains(cmd, "SCHMUX_EVENTS_FILE") {
				t.Error("Command should reference SCHMUX_EVENTS_FILE")
			}
			if !strings.Contains(cmd, "jq -Rs") {
				t.Error("event write should escape MSG through jq -Rs for JSON safety")
			}
		})
	}
}

func TestBuildClaudeHooksMap_ContextExtraction(t *testing.T) {
	hooks := buildClaudeHooksMap("/tmp/test-hooks", true)

	// Notification hook should extract .message from stdin
	notifGroups := hooks["Notification"]
	if len(notifGroups) == 0 {
		t.Fatal("Notification hook should exist")
	}
	notifCmd := notifGroups[0].Hooks[0].Command
	if !strings.Contains(notifCmd, "jq") || !strings.Contains(notifCmd, ".message") {
		t.Error("Notification hook should extract .message from stdin JSON")
	}
	if !strings.Contains(notifCmd, "blockers:") {
		t.Error("Notification hook should write blockers: line to signal file")
	}

	// UserPromptSubmit hook should extract .prompt from stdin
	promptGroups := hooks["UserPromptSubmit"]
	if len(promptGroups) == 0 {
		t.Fatal("UserPromptSubmit hook should exist")
	}
	promptCmd := promptGroups[0].Hooks[0].Command
	if !strings.Contains(promptCmd, "jq") || !strings.Contains(promptCmd, ".prompt") {
		t.Error("UserPromptSubmit hook should extract .prompt from stdin JSON")
	}

	// SessionStart and SessionEnd should NOT use context extraction (no useful fields)
	startCmd := hooks["SessionStart"][0].Hooks[0].Command
	if strings.Contains(startCmd, "jq") {
		t.Error("SessionStart hook should not use jq (no useful context field)")
	}
	endCmd := hooks["SessionEnd"][0].Hooks[0].Command
	if strings.Contains(endCmd, "jq") {
		t.Error("SessionEnd hook should not use jq (no useful context field)")
	}

	// Stop hook should reference the external script (not inline the logic)
	stopCmd := hooks["Stop"][0].Hooks[0].Command
	if !strings.Contains(stopCmd, "stop-status-check.sh") {
		t.Error("Stop hook should reference stop-status-check.sh script")
	}
}

func TestBuildClaudeHooksMap_SessionEndHook(t *testing.T) {
	hooks := buildClaudeHooksMap("/tmp/test-hooks", true)

	// SessionEnd hook should exist and signal "completed"
	endGroups, ok := hooks["SessionEnd"]
	if !ok {
		t.Fatal("SessionEnd hook should exist")
	}
	if len(endGroups) != 1 {
		t.Fatalf("SessionEnd should have 1 matcher group, got %d", len(endGroups))
	}
	endCmd := endGroups[0].Hooks[0].Command
	if !strings.Contains(endCmd, "completed") {
		t.Error("SessionEnd hook should signal 'completed'")
	}
	if !strings.Contains(endCmd, "SCHMUX_STATUS_FILE") {
		t.Error("SessionEnd hook should reference SCHMUX_STATUS_FILE")
	}
}

func TestClaudeHooks_SessionEndOnDisk(t *testing.T) {
	tmpDir := t.TempDir()

	if err := ClaudeHooks(tmpDir, "/tmp/test-hooks", true); err != nil {
		t.Fatalf("ClaudeHooks failed: %v", err)
	}

	settingsPath := filepath.Join(tmpDir, ".claude", "settings.local.json")
	content, err := os.ReadFile(settingsPath)
	if err != nil {
		t.Fatalf("Failed to read settings file: %v", err)
	}

	// Parse and verify SessionEnd is present in the written file
	var settings map[string]json.RawMessage
	if err := json.Unmarshal(content, &settings); err != nil {
		t.Fatalf("Invalid JSON: %v", err)
	}

	var hooks map[string][]claudeHookMatcherGroup
	if err := json.Unmarshal(settings["hooks"], &hooks); err != nil {
		t.Fatalf("Invalid hooks JSON: %v", err)
	}

	endGroups, ok := hooks["SessionEnd"]
	if !ok {
		t.Fatal("SessionEnd should be present in the written settings file")
	}
	if len(endGroups) != 1 {
		t.Fatalf("SessionEnd should have 1 matcher group on disk, got %d", len(endGroups))
	}

	// Verify the command signals completed
	cmd := endGroups[0].Hooks[0].Command
	if !strings.Contains(cmd, "completed") {
		t.Error("SessionEnd hook on disk should signal 'completed'")
	}
}

func TestWrapCommandWithHooks_IncludesSessionEnd(t *testing.T) {
	wrapped, err := WrapCommandWithHooks("claude test", "/tmp/test-hooks")
	if err != nil {
		t.Fatalf("WrapCommandWithHooks failed: %v", err)
	}

	if !strings.Contains(wrapped, "SessionEnd") {
		t.Error("Wrapped command should include SessionEnd hook in the inline JSON")
	}
}

func TestDualWriteCommandWithBlockers(t *testing.T) {
	cmd := dualWriteCommandWithBlockers("needs_input", "message")
	if !strings.Contains(cmd, "blockers:") {
		t.Error("expected blockers extraction in signal command")
	}
	if !strings.Contains(cmd, "SCHMUX_STATUS_FILE") {
		t.Error("expected SCHMUX_STATUS_FILE reference")
	}
	if !strings.Contains(cmd, "SCHMUX_EVENTS_FILE") {
		t.Error("expected SCHMUX_EVENTS_FILE reference")
	}
	if !strings.Contains(cmd, "needs_input") {
		t.Error("expected needs_input state in command")
	}
	if !strings.Contains(cmd, ".message") {
		t.Error("expected jq extraction of .message field")
	}
	if !strings.Contains(cmd, "jq -Rs") {
		t.Error("event write should escape MSG through jq -Rs for JSON safety")
	}
}

func TestClaudeHooks_MultipleUserHooksOnSameEvent(t *testing.T) {
	tmpDir := t.TempDir()

	settingsDir := filepath.Join(tmpDir, ".claude")
	if err := os.MkdirAll(settingsDir, 0755); err != nil {
		t.Fatal(err)
	}
	settingsPath := filepath.Join(settingsDir, "settings.local.json")
	existing := `{
		"hooks": {
			"Notification": [
				{
					"matcher": "permission_prompt",
					"hooks": [
						{
							"type": "command",
							"command": "echo perm-alert",
							"statusMessage": "user: permission alert"
						}
					]
				},
				{
					"matcher": "idle_prompt",
					"hooks": [
						{
							"type": "command",
							"command": "echo idle-alert",
							"statusMessage": "user: idle alert"
						}
					]
				}
			]
		}
	}`
	if err := os.WriteFile(settingsPath, []byte(existing), 0644); err != nil {
		t.Fatal(err)
	}

	if err := ClaudeHooks(tmpDir, "/tmp/test-hooks", true); err != nil {
		t.Fatal(err)
	}

	content, _ := os.ReadFile(settingsPath)
	var settings map[string]json.RawMessage
	json.Unmarshal(content, &settings)

	var hooks map[string][]claudeHookMatcherGroup
	json.Unmarshal(settings["hooks"], &hooks)

	notifGroups := hooks["Notification"]
	// Should have 2 user groups + 1 schmux group = 3
	if len(notifGroups) != 3 {
		t.Fatalf("Notification should have 3 groups (2 user + 1 schmux), got %d", len(notifGroups))
	}

	// Verify ordering: user hooks first, then schmux
	if notifGroups[0].Hooks[0].Command != "echo perm-alert" {
		t.Error("First user notification hook should be preserved")
	}
	if notifGroups[1].Hooks[0].Command != "echo idle-alert" {
		t.Error("Second user notification hook should be preserved")
	}
	if !isSchmuxMatcherGroup(notifGroups[2]) {
		t.Error("Schmux notification hook should be appended last")
	}
}

func TestBuildClaudeHooksMap_WithLore(t *testing.T) {
	hooks := buildClaudeHooksMap("/tmp/test-hooks", true)

	// PostToolUseFailure hook should exist when lore is enabled
	ptuf, ok := hooks["PostToolUseFailure"]
	if !ok {
		t.Fatal("PostToolUseFailure hook not found when lore is enabled")
	}
	if len(ptuf) == 0 || len(ptuf[0].Hooks) == 0 {
		t.Fatal("PostToolUseFailure should have at least one handler")
	}
	if !strings.Contains(ptuf[0].Hooks[0].Command, "capture-failure") {
		t.Errorf("PostToolUseFailure command should reference capture-failure script, got: %s", ptuf[0].Hooks[0].Command)
	}

	// Stop hook should have both status and lore check
	stop, ok := hooks["Stop"]
	if !ok {
		t.Fatal("Stop hook not found")
	}
	if len(stop) < 2 {
		t.Fatalf("Stop hook should have at least 2 groups (status + lore), got %d", len(stop))
	}
	foundLoreCheck := false
	for _, group := range stop {
		for _, h := range group.Hooks {
			if strings.Contains(h.Command, "stop-lore-check") {
				foundLoreCheck = true
			}
		}
	}
	if !foundLoreCheck {
		t.Error("Stop hook should include stop-lore-check handler when lore is enabled")
	}
}

func TestEnsureExcludeEntries_CreatesNewFile(t *testing.T) {
	tmpDir := t.TempDir()
	excludePath := filepath.Join(tmpDir, "info", "exclude")

	if err := ensureExcludeEntries(excludePath); err != nil {
		t.Fatalf("ensureExcludeEntries failed: %v", err)
	}

	content, err := os.ReadFile(excludePath)
	if err != nil {
		t.Fatalf("failed to read exclude file: %v", err)
	}

	contentStr := string(content)
	if !strings.Contains(contentStr, excludeMarkerStart) {
		t.Error("should contain SCHMUX:BEGIN marker")
	}
	if !strings.Contains(contentStr, excludeMarkerEnd) {
		t.Error("should contain SCHMUX:END marker")
	}
	for _, pattern := range excludePatterns {
		if !strings.Contains(contentStr, pattern) {
			t.Errorf("should contain pattern %q", pattern)
		}
	}
}

func TestEnsureExcludeEntries_AppendsToExisting(t *testing.T) {
	tmpDir := t.TempDir()
	infoDir := filepath.Join(tmpDir, "info")
	if err := os.MkdirAll(infoDir, 0755); err != nil {
		t.Fatal(err)
	}
	excludePath := filepath.Join(infoDir, "exclude")
	userContent := "# user patterns\n*.log\n"
	if err := os.WriteFile(excludePath, []byte(userContent), 0644); err != nil {
		t.Fatal(err)
	}

	if err := ensureExcludeEntries(excludePath); err != nil {
		t.Fatalf("ensureExcludeEntries failed: %v", err)
	}

	content, err := os.ReadFile(excludePath)
	if err != nil {
		t.Fatal(err)
	}

	contentStr := string(content)
	if !strings.Contains(contentStr, "*.log") {
		t.Error("user content should be preserved")
	}
	if !strings.Contains(contentStr, excludeMarkerStart) {
		t.Error("schmux block should be appended")
	}
}

func TestEnsureExcludeEntries_ReplacesStaleBlock(t *testing.T) {
	tmpDir := t.TempDir()
	infoDir := filepath.Join(tmpDir, "info")
	if err := os.MkdirAll(infoDir, 0755); err != nil {
		t.Fatal(err)
	}
	excludePath := filepath.Join(infoDir, "exclude")
	staleContent := "# user patterns\n*.log\n\n" + excludeMarkerStart + "\nold-stale-pattern\n" + excludeMarkerEnd + "\n"
	if err := os.WriteFile(excludePath, []byte(staleContent), 0644); err != nil {
		t.Fatal(err)
	}

	if err := ensureExcludeEntries(excludePath); err != nil {
		t.Fatalf("ensureExcludeEntries failed: %v", err)
	}

	content, err := os.ReadFile(excludePath)
	if err != nil {
		t.Fatal(err)
	}

	contentStr := string(content)
	if strings.Contains(contentStr, "old-stale-pattern") {
		t.Error("stale content should be replaced")
	}
	if !strings.Contains(contentStr, ".schmux/signal/") {
		t.Error("new patterns should be present")
	}
	if strings.Count(contentStr, excludeMarkerStart) != 1 {
		t.Error("should have exactly one SCHMUX:BEGIN marker")
	}
}

func TestEnsureExcludeEntries_Idempotent(t *testing.T) {
	tmpDir := t.TempDir()
	excludePath := filepath.Join(tmpDir, "info", "exclude")

	if err := ensureExcludeEntries(excludePath); err != nil {
		t.Fatal(err)
	}
	content1, _ := os.ReadFile(excludePath)

	if err := ensureExcludeEntries(excludePath); err != nil {
		t.Fatal(err)
	}
	content2, _ := os.ReadFile(excludePath)

	if string(content1) != string(content2) {
		t.Error("ensureExcludeEntries should be idempotent")
	}
}

func TestEnsureExcludeEntries_PreservesUserEntries(t *testing.T) {
	tmpDir := t.TempDir()
	infoDir := filepath.Join(tmpDir, "info")
	if err := os.MkdirAll(infoDir, 0755); err != nil {
		t.Fatal(err)
	}
	excludePath := filepath.Join(infoDir, "exclude")

	// User entries before and after the schmux block
	before := "# before\n*.tmp\n"
	after := "# after\n*.bak\n"
	existing := before + "\n" + buildExcludeBlock() + after
	if err := os.WriteFile(excludePath, []byte(existing), 0644); err != nil {
		t.Fatal(err)
	}

	if err := ensureExcludeEntries(excludePath); err != nil {
		t.Fatal(err)
	}

	content, _ := os.ReadFile(excludePath)
	contentStr := string(content)

	if !strings.Contains(contentStr, "*.tmp") {
		t.Error("user entries before block should be preserved")
	}
	if !strings.Contains(contentStr, "*.bak") {
		t.Error("user entries after block should be preserved")
	}
}

func TestEnsureExcludeEntries_NoTrailingNewline(t *testing.T) {
	tmpDir := t.TempDir()
	infoDir := filepath.Join(tmpDir, "info")
	if err := os.MkdirAll(infoDir, 0755); err != nil {
		t.Fatal(err)
	}
	excludePath := filepath.Join(infoDir, "exclude")
	// File without trailing newline
	if err := os.WriteFile(excludePath, []byte("*.log"), 0644); err != nil {
		t.Fatal(err)
	}

	if err := ensureExcludeEntries(excludePath); err != nil {
		t.Fatal(err)
	}

	content, _ := os.ReadFile(excludePath)
	contentStr := string(content)

	// Should not mangle the user entry
	if !strings.Contains(contentStr, "*.log") {
		t.Error("user entry should be preserved")
	}
	// The schmux block should be on its own lines (not joined to *.log)
	if strings.Contains(contentStr, "*.log"+excludeMarkerStart) {
		t.Error("block should be separated from existing content")
	}
}

func TestLoreHookScripts(t *testing.T) {
	tmpDir := t.TempDir()
	if err := LoreHookScripts(tmpDir); err != nil {
		t.Fatalf("LoreHookScripts failed: %v", err)
	}
}

func TestBuildClaudeHooksMap_WithoutLore(t *testing.T) {
	hooks := buildClaudeHooksMap("/tmp/test-hooks", false)

	// PostToolUseFailure hook should NOT exist when lore is disabled
	if _, ok := hooks["PostToolUseFailure"]; ok {
		t.Error("PostToolUseFailure hook should not exist when lore is disabled")
	}

	// Stop hook should only have status check, not lore check
	stop, ok := hooks["Stop"]
	if !ok {
		t.Fatal("Stop hook not found")
	}
	for _, group := range stop {
		for _, h := range group.Hooks {
			if strings.Contains(h.Command, "stop-lore-check") {
				t.Error("Stop hook should NOT include stop-lore-check when lore is disabled")
			}
		}
	}
}
