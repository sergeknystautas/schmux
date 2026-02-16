package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestLoreSaveWithUserConfigStructure(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.json")

	// Config matching the user's real file (no lore section)
	userConfig := `{
  "config_version": "dev",
  "workspace_path": "/tmp/test-workspaces",
  "source_code_management": "git-worktree",
  "repos": [
    {"name": "test-repo", "url": "https://example.com/test.git", "bare_path": "test.git"}
  ],
  "run_targets": [
    {"name": "Shell", "type": "command", "command": "bash", "source": "user"},
    {"name": "claude", "type": "promptable", "command": "claude", "source": "detected"}
  ],
  "quick_launch": [],
  "external_diff_cleanup_after_ms": 3600000,
  "terminal": {"width": 120, "height": 40, "seed_lines": 100, "bootstrap_lines": 20000},
  "nudgenik": {"viewed_buffer_ms": 5000, "seen_interval_ms": 2000},
  "conflict_resolve": {"target": "claude-opus"},
  "sessions": {"dashboard_poll_interval_ms": 5000, "git_status_poll_interval_ms": 10000},
  "xterm": {"mtime_poll_interval_ms": 5000, "query_timeout_ms": 5000, "operation_timeout_ms": 10000},
  "network": {"bind_address": "127.0.0.1", "tls": {}},
  "access_control": {"enabled": false, "provider": "github", "session_ttl_minutes": 1440},
  "pr_review": {"target": "claude-opus"},
  "commit_message": {},
  "notifications": {},
  "models": {}
}`
	if err := os.WriteFile(cfgPath, []byte(userConfig), 0644); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(cfgPath)
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}

	// Verify lore starts nil
	if cfg.Lore != nil {
		t.Fatalf("expected Lore to be nil after load, got %+v", cfg.Lore)
	}

	// Simulate handleConfigUpdate: Reload → set lore → ValidateForSave → Save
	if err := cfg.Reload(); err != nil {
		t.Fatalf("Reload failed: %v", err)
	}

	if cfg.Lore != nil {
		t.Fatalf("expected Lore to be nil after reload, got %+v", cfg.Lore)
	}

	cfg.Lore = &LoreConfig{}
	enabled := true
	cfg.Lore.Enabled = &enabled
	cfg.Lore.Target = "claude-opus"
	cfg.Lore.CurateOnDispose = "session"
	autoPR := false
	cfg.Lore.AutoPR = &autoPR

	warnings, err := cfg.ValidateForSave()
	if err != nil {
		t.Fatalf("ValidateForSave failed: %v", err)
	}
	if len(warnings) > 0 {
		t.Logf("Warnings: %v", warnings)
	}

	if err := cfg.Save(); err != nil {
		t.Fatalf("Save failed: %v", err)
	}

	// Read saved file and verify lore section exists
	data, err := os.ReadFile(cfgPath)
	if err != nil {
		t.Fatal(err)
	}

	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("Failed to parse saved config: %v", err)
	}

	loreRaw, ok := raw["lore"]
	if !ok {
		t.Fatalf("lore section NOT found in saved config. Full config:\n%s", string(data))
	}

	var lore map[string]interface{}
	if err := json.Unmarshal(loreRaw, &lore); err != nil {
		t.Fatalf("Failed to parse lore section: %v", err)
	}

	if lore["llm_target"] != "claude-opus" {
		t.Errorf("expected llm_target to be 'claude-opus', got %v", lore["llm_target"])
	}
	if lore["curate_on_dispose"] != "session" {
		t.Errorf("expected curate_on_dispose to be 'session', got %v", lore["curate_on_dispose"])
	}
	if lore["enabled"] != true {
		t.Errorf("expected enabled to be true, got %v", lore["enabled"])
	}

	// Verify lore survives a round-trip: reload and check
	if err := cfg.Reload(); err != nil {
		t.Fatalf("Reload after save failed: %v", err)
	}
	if cfg.Lore == nil {
		t.Fatal("Lore is nil after reload")
	}
	if cfg.Lore.Target != "claude-opus" {
		t.Errorf("expected Target to be 'claude-opus' after reload, got %q", cfg.Lore.Target)
	}
	if cfg.GetLoreCurateOnDispose() != "session" {
		t.Errorf("expected CurateOnDispose to be 'session' after reload, got %q", cfg.GetLoreCurateOnDispose())
	}
}
