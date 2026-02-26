package detect

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestOpencodeSetupHooks(t *testing.T) {
	dir := t.TempDir()
	adapter := &OpencodeAdapter{}
	err := adapter.SetupHooks(HookContext{WorkspacePath: dir})
	if err != nil {
		t.Fatalf("SetupHooks error: %v", err)
	}

	pluginPath := filepath.Join(dir, ".opencode", "plugins", "schmux.ts")
	content, err := os.ReadFile(pluginPath)
	if err != nil {
		t.Fatalf("failed to read plugin: %v", err)
	}

	contentStr := string(content)

	// Verify key content
	if !strings.Contains(contentStr, "SCHMUX_EVENTS_FILE") {
		t.Error("plugin should reference SCHMUX_EVENTS_FILE")
	}
	if !strings.Contains(contentStr, "appendEvent") {
		t.Error("plugin should contain appendEvent function")
	}
	if !strings.Contains(contentStr, "stop:") {
		t.Error("plugin should contain stop hook")
	}
	if !strings.Contains(contentStr, "classifyError") {
		t.Error("plugin should contain classifyError function")
	}

	// Verify state signals
	for _, state := range []string{"working", "completed", "needs_input"} {
		if !strings.Contains(contentStr, state) {
			t.Errorf("plugin should contain state %q", state)
		}
	}

	// Verify event types
	for _, event := range []string{"session.created", "session.idle", "permission.asked", "message.updated", "tool.execute.after"} {
		if !strings.Contains(contentStr, event) {
			t.Errorf("plugin should handle event %q", event)
		}
	}
}

func TestOpencodeCleanupHooks(t *testing.T) {
	dir := t.TempDir()
	adapter := &OpencodeAdapter{}

	// Setup then cleanup
	if err := adapter.SetupHooks(HookContext{WorkspacePath: dir}); err != nil {
		t.Fatal(err)
	}

	err := adapter.CleanupHooks(dir)
	if err != nil {
		t.Fatalf("CleanupHooks error: %v", err)
	}

	pluginPath := filepath.Join(dir, ".opencode", "plugins", "schmux.ts")
	if _, err := os.Stat(pluginPath); !os.IsNotExist(err) {
		t.Error("plugin file should be removed after cleanup")
	}
}

func TestOpencodeCleanupHooks_NoFile(t *testing.T) {
	dir := t.TempDir()
	adapter := &OpencodeAdapter{}

	// Should not error when there's no plugin file
	if err := adapter.CleanupHooks(dir); err != nil {
		t.Fatalf("CleanupHooks should not error on missing file: %v", err)
	}
}

func TestOpencodeSetupHooksIdempotent(t *testing.T) {
	dir := t.TempDir()
	adapter := &OpencodeAdapter{}

	if err := adapter.SetupHooks(HookContext{WorkspacePath: dir}); err != nil {
		t.Fatal(err)
	}
	pluginPath := filepath.Join(dir, ".opencode", "plugins", "schmux.ts")
	content1, _ := os.ReadFile(pluginPath)

	if err := adapter.SetupHooks(HookContext{WorkspacePath: dir}); err != nil {
		t.Fatal(err)
	}
	content2, _ := os.ReadFile(pluginPath)

	if string(content1) != string(content2) {
		t.Error("SetupHooks should be idempotent")
	}
}

func TestOpencodeWrapRemoteCommand(t *testing.T) {
	adapter := &OpencodeAdapter{}

	wrapped, err := adapter.WrapRemoteCommand(`opencode "hello world"`)
	if err != nil {
		t.Fatalf("WrapRemoteCommand failed: %v", err)
	}

	// Should start with mkdir
	if !strings.HasPrefix(wrapped, "mkdir -p .opencode/plugins") {
		t.Error("Should start with mkdir -p .opencode/plugins")
	}

	// Should contain the original command
	if !strings.Contains(wrapped, `opencode "hello world"`) {
		t.Error("Should contain the original command")
	}

	// Should create schmux.ts
	if !strings.Contains(wrapped, "schmux.ts") {
		t.Error("Should create schmux.ts")
	}

	// Commands should be chained with &&
	if !strings.Contains(wrapped, " && ") {
		t.Error("Commands should be chained with &&")
	}

	// Should include plugin content
	if !strings.Contains(wrapped, "SCHMUX_EVENTS_FILE") {
		t.Error("Wrapped command should include plugin content")
	}
}
