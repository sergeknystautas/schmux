package detect

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func codexProbeCtx(t *testing.T, settingsFile string) HookContext {
	t.Helper()
	return HookContext{
		HooksDir: filepath.Join(t.TempDir(), "hooks"),
		Hooks: &HooksDesc{
			Strategy:        "global-json-settings-merge",
			SettingsFile:    settingsFile,
			OwnershipPrefix: "schmux:",
		},
	}
}

func readCodexGroups(t *testing.T, path, event string) []codexHookGroup {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	var root struct {
		Hooks map[string][]codexHookGroup `json:"hooks"`
	}
	if err := json.Unmarshal(data, &root); err != nil {
		t.Fatalf("parse %s: %v", path, err)
	}
	return root.Hooks[event]
}

func schmuxGroupCount(groups []codexHookGroup) int {
	n := 0
	for _, g := range groups {
		if isSchmuxCodexGroup(g) {
			n++
		}
	}
	return n
}

func TestCodexSetupHooks_CreatesFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "hooks.json")
	ctx := codexProbeCtx(t, path)
	if err := codexSetupHooks(ctx); err != nil {
		t.Fatalf("codexSetupHooks: %v", err)
	}
	for _, event := range codexCaptureEvents {
		groups := readCodexGroups(t, path, event)
		if len(groups) != 1 || !isSchmuxCodexGroup(groups[0]) {
			t.Errorf("%s: groups = %+v, want exactly one schmux group", event, groups)
		}
		cmd := groups[0].Hooks[0].Command
		if !strings.Contains(cmd, filepath.Join(ctx.HooksDir, "capture-session.sh")) {
			t.Errorf("%s: command %q does not reference capture-session.sh", event, cmd)
		}
		if !strings.HasPrefix(groups[0].Hooks[0].StatusMessage, "schmux:") {
			t.Errorf("%s: statusMessage %q lacks schmux: prefix", event, groups[0].Hooks[0].StatusMessage)
		}
	}
}

func TestCodexSetupHooks_PreservesUserContent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "hooks.json")
	user := `{
  "description": "my own hooks",
  "hooks": {
    "Stop": [ { "hooks": [ { "type": "command", "command": "/usr/local/bin/stop-thing", "timeout": 30, "statusMessage": "Checking reply" } ] } ],
    "SessionEnd": [ { "hooks": [ { "type": "command", "command": "/usr/local/bin/end-thing" } ] } ],
    "UserPromptSubmit": [ { "hooks": [ { "type": "command", "command": "/usr/local/bin/prompt-thing" } ] } ]
  }
}`
	if err := os.WriteFile(path, []byte(user), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := codexSetupHooks(codexProbeCtx(t, path)); err != nil {
		t.Fatalf("codexSetupHooks: %v", err)
	}
	// User entries survive semantically.
	var root struct {
		Description string                      `json:"description"`
		Hooks       map[string][]codexHookGroup `json:"hooks"`
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(data, &root); err != nil {
		t.Fatalf("parse: %v", err)
	}
	if root.Description != "my own hooks" {
		t.Errorf("description = %q, want preserved", root.Description)
	}
	if g := readCodexGroups(t, path, "Stop"); len(g) != 1 || g[0].Hooks[0].Command != "/usr/local/bin/stop-thing" {
		t.Errorf("Stop groups = %+v, want user group preserved", g)
	}
	if g := readCodexGroups(t, path, "SessionEnd"); len(g) != 1 || g[0].Hooks[0].Command != "/usr/local/bin/end-thing" {
		t.Errorf("SessionEnd groups = %+v, want user group preserved", g)
	}
	// Managed event holds user group + schmux group, user group first so its
	// codex trust key (path:event:index) keeps pointing at the same hook.
	groups := readCodexGroups(t, path, "UserPromptSubmit")
	if len(groups) != 2 || schmuxGroupCount(groups) != 1 {
		t.Fatalf("UserPromptSubmit groups = %+v, want user group + one schmux group", groups)
	}
	if groups[0].Hooks[0].Command != "/usr/local/bin/prompt-thing" {
		t.Errorf("UserPromptSubmit group 0 = %+v, want the user's group to keep index 0", groups[0])
	}
}

func TestCodexSetupHooks_PreservesUserTimeout(t *testing.T) {
	path := filepath.Join(t.TempDir(), "hooks.json")
	user := `{"hooks":{"Stop":[{"hooks":[{"type":"command","command":"/usr/local/bin/stop-thing","timeout":30}]}]}}`
	if err := os.WriteFile(path, []byte(user), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := codexSetupHooks(codexProbeCtx(t, path)); err != nil {
		t.Fatalf("codexSetupHooks: %v", err)
	}
	g := readCodexGroups(t, path, "Stop")
	if len(g) != 1 || g[0].Hooks[0].Timeout == nil || *g[0].Hooks[0].Timeout != 30 {
		t.Errorf("Stop groups = %+v, want timeout 30 preserved", g)
	}
}

func TestCodexSetupHooks_Idempotent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "hooks.json")
	ctx := codexProbeCtx(t, path)
	if err := codexSetupHooks(ctx); err != nil {
		t.Fatal(err)
	}
	first, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := codexSetupHooks(ctx); err != nil {
		t.Fatal(err)
	}
	second, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(first) != string(second) {
		t.Error("second run changed the file; want byte-identical output")
	}
}

func TestCodexSetupHooks_MalformedFileUntouched(t *testing.T) {
	path := filepath.Join(t.TempDir(), "hooks.json")
	garbage := []byte("{not json at all")
	if err := os.WriteFile(path, garbage, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := codexSetupHooks(codexProbeCtx(t, path)); err == nil {
		t.Fatal("expected error for malformed file")
	}
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(after) != string(garbage) {
		t.Error("malformed file was rewritten; want untouched")
	}
}

func TestCodexSetupHooks_ReplacesStaleSchmuxGroup(t *testing.T) {
	path := filepath.Join(t.TempDir(), "hooks.json")
	stale := `{"hooks":{"UserPromptSubmit":[{"hooks":[{"type":"command","command":"/old/path/capture-session.sh","statusMessage":"schmux: resume id"}]}]}}`
	if err := os.WriteFile(path, []byte(stale), 0o644); err != nil {
		t.Fatal(err)
	}
	ctx := codexProbeCtx(t, path)
	if err := codexSetupHooks(ctx); err != nil {
		t.Fatal(err)
	}
	groups := readCodexGroups(t, path, "UserPromptSubmit")
	if len(groups) != 1 || schmuxGroupCount(groups) != 1 || groups[0].Hooks[0].Command != codexCaptureCommand(ctx.HooksDir) {
		t.Errorf("UserPromptSubmit groups = %+v, want stale group replaced with current command", groups)
	}
}

// TestCodexSetupHooks_DropsSchmuxGroupFromUnmanagedEvent covers the upgrade
// path: an event schmux used to manage but no longer does loses its schmux
// group entirely, and the now-empty event key disappears.
func TestCodexSetupHooks_DropsSchmuxGroupFromUnmanagedEvent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "hooks.json")
	stale := `{"hooks":{"SessionStart":[{"hooks":[{"type":"command","command":"/old/capture-session.sh","statusMessage":"schmux: resume id"}]}]}}`
	if err := os.WriteFile(path, []byte(stale), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := codexSetupHooks(codexProbeCtx(t, path)); err != nil {
		t.Fatal(err)
	}
	if g := readCodexGroups(t, path, "SessionStart"); len(g) != 0 {
		t.Errorf("SessionStart groups = %+v, want the stale schmux group removed", g)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "SessionStart") {
		t.Errorf("file still mentions SessionStart, want the emptied event key dropped:\n%s", data)
	}
}

func TestCodexSetupHooks_RequiresDescriptorHooks(t *testing.T) {
	ctx := HookContext{HooksDir: t.TempDir()}
	if err := codexSetupHooks(ctx); err == nil {
		t.Error("expected error when ctx.Hooks is nil")
	}
}

func TestCodexSetupHooks_CodeXHomeOverrides(t *testing.T) {
	home := t.TempDir()
	t.Setenv("CODEX_HOME", home)
	// settingsFile points elsewhere; CODEX_HOME must win.
	if err := codexSetupHooks(codexProbeCtx(t, filepath.Join(t.TempDir(), "hooks.json"))); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(home, "hooks.json")); err != nil {
		t.Errorf("expected $CODEX_HOME/hooks.json to be written: %v", err)
	}
}

func TestCodexHooksStrategy_Registered(t *testing.T) {
	s, err := GetHookStrategy("global-json-settings-merge")
	if err != nil {
		t.Fatalf("GetHookStrategy: %v", err)
	}
	if !s.SupportsHooks() {
		t.Error("SupportsHooks = false, want true")
	}
	if err := s.CleanupHooks("/ws"); err != nil {
		t.Errorf("CleanupHooks = %v, want nil (global file is left in place)", err)
	}
	if cmd, err := s.WrapRemoteCommand("codex"); cmd != "codex" || err != nil {
		t.Errorf("WrapRemoteCommand = (%q, %v), want passthrough", cmd, err)
	}
}
