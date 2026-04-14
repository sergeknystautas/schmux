package detect

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sergeknystautas/schmux/internal/schmuxdir"
)

func TestClaudeSetupHooks(t *testing.T) {
	tmpDir := t.TempDir()

	if err := claudeSetupHooks(tmpDir, ""); err != nil {
		t.Fatalf("claudeSetupHooks failed: %v", err)
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
	for _, event := range []string{"SessionStart", "SessionEnd", "UserPromptSubmit", "Stop", "Notification", "PostToolUseFailure"} {
		if _, ok := hooks[event]; !ok {
			t.Errorf("Missing hook event: %s", event)
		}
	}

	// Verify the commands reference SCHMUX_EVENTS_FILE
	contentStr := string(content)
	if !strings.Contains(contentStr, "SCHMUX_EVENTS_FILE") {
		t.Error("Hooks should reference SCHMUX_EVENTS_FILE")
	}

	// Verify state signals are present in the command strings
	for _, state := range []string{"working", "completed", "needs_input", "idle"} {
		if !strings.Contains(contentStr, state) {
			t.Errorf("Hooks should contain state %q", state)
		}
	}
}

func TestClaudeSetupHooksPreservesUserHooks(t *testing.T) {
	tmpDir := t.TempDir()

	// Create existing settings with user-defined hooks and other settings
	settingsDir := filepath.Join(tmpDir, ".claude")
	if err := os.MkdirAll(settingsDir, 0755); err != nil {
		t.Fatal(err)
	}
	settingsPath := filepath.Join(settingsDir, "settings.local.json")
	existing := `{
		"permissions": {"allow": ["Read"]},
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

	if err := claudeSetupHooks(tmpDir, ""); err != nil {
		t.Fatalf("claudeSetupHooks failed: %v", err)
	}

	content, err := os.ReadFile(settingsPath)
	if err != nil {
		t.Fatal(err)
	}

	var settings map[string]json.RawMessage
	if err := json.Unmarshal(content, &settings); err != nil {
		t.Fatalf("Invalid JSON: %v", err)
	}

	// Other settings preserved
	if _, ok := settings["permissions"]; !ok {
		t.Error("permissions should be preserved")
	}

	var hooks map[string][]claudeHookMatcherGroup
	if err := json.Unmarshal(settings["hooks"], &hooks); err != nil {
		t.Fatalf("Invalid hooks JSON: %v", err)
	}

	// User's Stop hook should be preserved alongside schmux's
	stopGroups := hooks["Stop"]
	if len(stopGroups) != 4 {
		t.Fatalf("Stop should have 4 matcher groups (user + 3 schmux), got %d", len(stopGroups))
	}
	// First should be the user's hook (preserved order)
	if stopGroups[0].Hooks[0].Command != "echo user-stop-hook" {
		t.Error("User's Stop hook should be preserved")
	}
	// Remaining should be schmux's
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
	for _, event := range []string{"SessionStart", "SessionEnd", "UserPromptSubmit", "Stop", "Notification", "PostToolUseFailure"} {
		if _, ok := hooks[event]; !ok {
			t.Errorf("Missing schmux hook event: %s", event)
		}
	}
}

func TestClaudeSetupHooksIdempotent(t *testing.T) {
	tmpDir := t.TempDir()

	// Run twice
	if err := claudeSetupHooks(tmpDir, ""); err != nil {
		t.Fatalf("First call failed: %v", err)
	}
	settingsPath := filepath.Join(tmpDir, ".claude", "settings.local.json")
	content1, _ := os.ReadFile(settingsPath)

	if err := claudeSetupHooks(tmpDir, ""); err != nil {
		t.Fatalf("Second call failed: %v", err)
	}
	content2, _ := os.ReadFile(settingsPath)

	if string(content1) != string(content2) {
		t.Error("claudeSetupHooks should be idempotent")
	}
}

func TestClaudeCleanupHooks(t *testing.T) {
	tmpDir := t.TempDir()

	// First set up hooks
	if err := claudeSetupHooks(tmpDir, ""); err != nil {
		t.Fatal(err)
	}

	// Add a user hook alongside schmux hooks
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

	// Add user-only event
	hooks["PreToolUse"] = []claudeHookMatcherGroup{
		{
			Matcher: "Write",
			Hooks: []claudeHookHandler{
				{Type: "command", Command: "echo lint"},
			},
		},
	}

	hooksJSON, _ := json.Marshal(hooks)
	settings["hooks"] = json.RawMessage(hooksJSON)
	data, _ := json.MarshalIndent(settings, "", "  ")
	os.WriteFile(settingsPath, append(data, '\n'), 0644)

	// Now cleanup
	if err := claudeCleanupHooks(tmpDir); err != nil {
		t.Fatalf("claudeCleanupHooks failed: %v", err)
	}

	// Read back
	content, err := os.ReadFile(settingsPath)
	if err != nil {
		t.Fatalf("Settings file should still exist: %v", err)
	}

	var cleanedSettings map[string]json.RawMessage
	json.Unmarshal(content, &cleanedSettings)

	var cleanedHooks map[string][]claudeHookMatcherGroup
	json.Unmarshal(cleanedSettings["hooks"], &cleanedHooks)

	// User's Stop hook should remain
	stopGroups := cleanedHooks["Stop"]
	if len(stopGroups) != 1 {
		t.Fatalf("Stop should have 1 group (user only), got %d", len(stopGroups))
	}
	if stopGroups[0].Hooks[0].Command != "echo user-hook" {
		t.Error("User's Stop hook should be preserved")
	}

	// User's PreToolUse hook should remain
	preToolGroups := cleanedHooks["PreToolUse"]
	if len(preToolGroups) != 1 {
		t.Fatalf("PreToolUse should have 1 group, got %d", len(preToolGroups))
	}

	// Schmux-only events should be gone
	for _, event := range []string{"SessionStart", "SessionEnd", "UserPromptSubmit", "Notification", "PostToolUseFailure"} {
		if _, ok := cleanedHooks[event]; ok {
			t.Errorf("Schmux-only event %s should be removed", event)
		}
	}
}

func TestClaudeCleanupHooks_RemovesFileWhenOnlySchmux(t *testing.T) {
	tmpDir := t.TempDir()

	// Set up hooks only (no other settings)
	if err := claudeSetupHooks(tmpDir, ""); err != nil {
		t.Fatal(err)
	}

	// Cleanup should remove the file entirely
	if err := claudeCleanupHooks(tmpDir); err != nil {
		t.Fatalf("claudeCleanupHooks failed: %v", err)
	}

	settingsPath := filepath.Join(tmpDir, ".claude", "settings.local.json")
	if _, err := os.Stat(settingsPath); !os.IsNotExist(err) {
		t.Error("Settings file should be removed when only schmux hooks existed")
	}
}

func TestClaudeCleanupHooks_PreservesOtherSettings(t *testing.T) {
	tmpDir := t.TempDir()

	// Create settings with hooks and other keys
	settingsDir := filepath.Join(tmpDir, ".claude")
	if err := os.MkdirAll(settingsDir, 0755); err != nil {
		t.Fatal(err)
	}
	settingsPath := filepath.Join(settingsDir, "settings.local.json")

	// First set up schmux hooks
	if err := claudeSetupHooks(tmpDir, ""); err != nil {
		t.Fatal(err)
	}

	// Add another top-level setting
	content, _ := os.ReadFile(settingsPath)
	var settings map[string]json.RawMessage
	json.Unmarshal(content, &settings)
	settings["permissions"] = json.RawMessage(`{"allow": ["Read"]}`)
	data, _ := json.MarshalIndent(settings, "", "  ")
	os.WriteFile(settingsPath, append(data, '\n'), 0644)

	// Cleanup
	if err := claudeCleanupHooks(tmpDir); err != nil {
		t.Fatal(err)
	}

	// File should still exist with permissions
	content, err := os.ReadFile(settingsPath)
	if err != nil {
		t.Fatal("Settings file should still exist")
	}
	var cleanedSettings map[string]json.RawMessage
	json.Unmarshal(content, &cleanedSettings)

	if _, ok := cleanedSettings["permissions"]; !ok {
		t.Error("permissions should be preserved")
	}
	// hooks key should be gone (all were schmux)
	if _, ok := cleanedSettings["hooks"]; ok {
		t.Error("hooks key should be removed when only schmux hooks existed")
	}
}

func TestClaudeCleanupHooks_NoFile(t *testing.T) {
	tmpDir := t.TempDir()

	// Should not error when there's no settings file
	if err := claudeCleanupHooks(tmpDir); err != nil {
		t.Fatalf("claudeCleanupHooks should not error on missing file: %v", err)
	}
}

func TestClaudeWrapRemoteCommand(t *testing.T) {
	wrapped, err := claudeWrapRemoteCommand(`claude "hello world"`)
	if err != nil {
		t.Fatalf("claudeWrapRemoteCommand failed: %v", err)
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

	// Should include SessionEnd in the inline JSON
	if !strings.Contains(wrapped, "SessionEnd") {
		t.Error("Wrapped command should include SessionEnd hook")
	}
}

func TestClaudeAdapterSetupHooks(t *testing.T) {
	loadDescriptorAdapter(t, "claude")
	adapter := GetAdapter("claude")
	if adapter == nil {
		t.Fatal("claude adapter not registered")
	}
	tmpDir := t.TempDir()

	ctx := HookContext{
		WorkspacePath: tmpDir,
		HooksDir:      "",
	}
	if err := adapter.SetupHooks(ctx); err != nil {
		t.Fatalf("SetupHooks failed: %v", err)
	}

	// Verify file was created
	settingsPath := filepath.Join(tmpDir, ".claude", "settings.local.json")
	if _, err := os.Stat(settingsPath); err != nil {
		t.Fatalf("settings.local.json should exist: %v", err)
	}
}

func TestClaudeAdapterCleanupHooks(t *testing.T) {
	loadDescriptorAdapter(t, "claude")
	adapter := GetAdapter("claude")
	if adapter == nil {
		t.Fatal("claude adapter not registered")
	}
	tmpDir := t.TempDir()

	// Setup first
	ctx := HookContext{WorkspacePath: tmpDir}
	adapter.SetupHooks(ctx)

	// Then cleanup
	if err := adapter.CleanupHooks(tmpDir); err != nil {
		t.Fatalf("CleanupHooks failed: %v", err)
	}

	// File should be removed (only schmux hooks)
	settingsPath := filepath.Join(tmpDir, ".claude", "settings.local.json")
	if _, err := os.Stat(settingsPath); !os.IsNotExist(err) {
		t.Error("Settings file should be removed after cleanup")
	}
}

func TestClaudeAdapterWrapRemoteCommand(t *testing.T) {
	loadDescriptorAdapter(t, "claude")
	adapter := GetAdapter("claude")
	if adapter == nil {
		t.Fatal("claude adapter not registered")
	}

	wrapped, err := adapter.WrapRemoteCommand("claude test")
	if err != nil {
		t.Fatalf("WrapRemoteCommand failed: %v", err)
	}

	if !strings.HasPrefix(wrapped, "mkdir -p .claude") {
		t.Error("Should start with mkdir -p .claude")
	}
	if !strings.Contains(wrapped, "claude test") {
		t.Error("Should contain the original command")
	}
}

func TestEnsureGlobalHookScripts(t *testing.T) {
	tmpHome := t.TempDir()
	schmuxdir.Set(filepath.Join(tmpHome, ".schmux"))
	t.Cleanup(func() { schmuxdir.Set("") })

	hooksDir, err := EnsureGlobalHookScripts(tmpHome)
	if err != nil {
		t.Fatalf("EnsureGlobalHookScripts failed: %v", err)
	}

	expectedDir := filepath.Join(tmpHome, ".schmux", "hooks")
	if hooksDir != expectedDir {
		t.Errorf("hooksDir = %q, want %q", hooksDir, expectedDir)
	}

	// Verify all scripts were written
	for _, name := range []string{"capture-failure.sh", "stop-status-check.sh", "stop-autolearn-check.sh"} {
		path := filepath.Join(hooksDir, name)
		info, err := os.Stat(path)
		if err != nil {
			t.Errorf("Script %s should exist: %v", name, err)
			continue
		}
		// Check executable
		if info.Mode()&0111 == 0 {
			t.Errorf("Script %s should be executable", name)
		}
		// Check non-empty
		if info.Size() == 0 {
			t.Errorf("Script %s should not be empty", name)
		}
	}
}

func TestBuildClaudeHooksMap_AllEvents(t *testing.T) {
	hooks := buildClaudeHooksMap("")

	expectedEvents := []string{"SessionStart", "SessionEnd", "UserPromptSubmit", "Stop", "Notification", "PostToolUseFailure"}
	for _, event := range expectedEvents {
		if _, ok := hooks[event]; !ok {
			t.Errorf("Missing hook event: %s", event)
		}
	}
}

func TestBuildClaudeHooksMap_WithHooksDir(t *testing.T) {
	hooks := buildClaudeHooksMap("/home/user/.schmux/hooks")

	// Stop hooks should reference the centralized directory
	stopGroups := hooks["Stop"]
	foundCentralized := false
	for _, g := range stopGroups {
		for _, h := range g.Hooks {
			if strings.Contains(h.Command, "/home/user/.schmux/hooks/") {
				foundCentralized = true
			}
		}
	}
	if !foundCentralized {
		t.Error("Stop hooks should reference centralized hooks directory when provided")
	}

	// PostToolUseFailure should also reference centralized
	ptuf := hooks["PostToolUseFailure"]
	if len(ptuf) > 0 && !strings.Contains(ptuf[0].Hooks[0].Command, "/home/user/.schmux/hooks/capture-failure.sh") {
		t.Error("PostToolUseFailure should reference centralized capture-failure.sh")
	}
}

func TestBuildClaudeHooksMap_WithoutHooksDir(t *testing.T) {
	hooks := buildClaudeHooksMap("")

	// Stop hooks should use $CLAUDE_PROJECT_DIR fallback
	stopGroups := hooks["Stop"]
	foundFallback := false
	for _, g := range stopGroups {
		for _, h := range g.Hooks {
			if strings.Contains(h.Command, "$CLAUDE_PROJECT_DIR") {
				foundFallback = true
			}
		}
	}
	if !foundFallback {
		t.Error("Stop hooks should use CLAUDE_PROJECT_DIR fallback when no hooksDir")
	}
}

func TestClaudeHooksJSON_ValidJSON(t *testing.T) {
	jsonBytes, err := ClaudeHooksJSON("")
	if err != nil {
		t.Fatalf("ClaudeHooksJSON failed: %v", err)
	}

	var parsed map[string]interface{}
	if err := json.Unmarshal(jsonBytes, &parsed); err != nil {
		t.Fatalf("ClaudeHooksJSON returned invalid JSON: %v", err)
	}

	if _, ok := parsed["hooks"]; !ok {
		t.Error("Should contain hooks key")
	}

	// Should not contain single quotes (safe for shell wrapping)
	if strings.Contains(string(jsonBytes), "'") {
		t.Error("JSON should not contain single quotes")
	}
}

func TestIsSchmuxMatcherGroup_Detect(t *testing.T) {
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
			name: "empty hooks",
			group: claudeHookMatcherGroup{
				Hooks: []claudeHookHandler{},
			},
			expected: false,
		},
		{
			name: "no statusMessage",
			group: claudeHookMatcherGroup{
				Hooks: []claudeHookHandler{
					{Type: "command", Command: "echo test"},
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

func TestMergeHooksForEvent_Detect(t *testing.T) {
	schmuxGroup := claudeHookMatcherGroup{
		Hooks: []claudeHookHandler{
			{Type: "command", Command: "echo new-schmux", StatusMessage: "schmux: signaling"},
		},
	}
	userGroup := claudeHookMatcherGroup{
		Hooks: []claudeHookHandler{
			{Type: "command", Command: "echo user-hook", StatusMessage: "user: custom"},
		},
	}
	oldSchmuxGroup := claudeHookMatcherGroup{
		Hooks: []claudeHookHandler{
			{Type: "command", Command: "echo old-schmux", StatusMessage: "schmux: old"},
		},
	}

	t.Run("empty existing adds schmux", func(t *testing.T) {
		result := mergeHooksForEvent(nil, []claudeHookMatcherGroup{schmuxGroup})
		if len(result) != 1 || result[0].Hooks[0].Command != "echo new-schmux" {
			t.Error("should contain only the new schmux group")
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
		if result[0].Hooks[0].Command != "echo user-hook" {
			t.Error("user hook should come first")
		}
		if result[1].Hooks[0].Command != "echo new-schmux" {
			t.Error("schmux hook should come second")
		}
	})

	t.Run("old schmux hooks replaced by new", func(t *testing.T) {
		result := mergeHooksForEvent(
			[]claudeHookMatcherGroup{oldSchmuxGroup},
			[]claudeHookMatcherGroup{schmuxGroup},
		)
		if len(result) != 1 || result[0].Hooks[0].Command != "echo new-schmux" {
			t.Error("old schmux should be replaced by new")
		}
	})

	t.Run("user + old schmux replaced correctly", func(t *testing.T) {
		result := mergeHooksForEvent(
			[]claudeHookMatcherGroup{userGroup, oldSchmuxGroup},
			[]claudeHookMatcherGroup{schmuxGroup},
		)
		if len(result) != 2 {
			t.Fatalf("expected 2 groups, got %d", len(result))
		}
		if result[0].Hooks[0].Command != "echo user-hook" {
			t.Error("user hook should be preserved")
		}
		if result[1].Hooks[0].Command != "echo new-schmux" {
			t.Error("new schmux hook should be appended")
		}
	})
}
