package detect

import (
	"os"
	"path/filepath"
	"testing"
)

func TestJsonSettingsStrategy_SupportsHooks(t *testing.T) {
	s, err := GetHookStrategy("json-settings-merge")
	if err != nil {
		t.Fatalf("GetHookStrategy: %v", err)
	}
	if !s.SupportsHooks() {
		t.Error("json-settings-merge should support hooks")
	}
}

func TestJsonSettingsStrategy_SetupAndCleanup(t *testing.T) {
	s, _ := GetHookStrategy("json-settings-merge")
	dir := t.TempDir()

	// Setup hooks — should create .claude/settings.local.json
	err := s.SetupHooks(HookContext{WorkspacePath: dir, HooksDir: ""})
	if err != nil {
		t.Fatalf("SetupHooks: %v", err)
	}
	settingsPath := filepath.Join(dir, ".claude", "settings.local.json")
	if _, err := os.Stat(settingsPath); err != nil {
		t.Fatalf("settings file not created: %v", err)
	}
	content, _ := os.ReadFile(settingsPath)
	if len(content) == 0 {
		t.Error("settings file is empty")
	}

	// Cleanup — should remove schmux hooks
	err = s.CleanupHooks(dir)
	if err != nil {
		t.Fatalf("CleanupHooks: %v", err)
	}
}

func TestJsonSettingsStrategy_WrapRemoteCommand(t *testing.T) {
	s, _ := GetHookStrategy("json-settings-merge")
	wrapped, err := s.WrapRemoteCommand("claude --model opus")
	if err != nil {
		t.Fatalf("WrapRemoteCommand: %v", err)
	}
	if wrapped == "claude --model opus" {
		t.Error("WrapRemoteCommand should prepend hook setup")
	}
}
