package config

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
)

// writeRaw writes content as the config file and returns its path.
func writeRaw(t *testing.T, dir, content string) string {
	t.Helper()
	path := filepath.Join(dir, "config.json")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	return path
}

// readTop parses a config file into its top-level keys.
func readTop(t *testing.T, path string) map[string]json.RawMessage {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read config: %v", err)
	}
	var top map[string]json.RawMessage
	if err := json.Unmarshal(data, &top); err != nil {
		t.Fatalf("parse config: %v", err)
	}
	return top
}

// pendingEntries returns the pending file's entries as parsed objects.
func pendingEntries(t *testing.T, configPath string) []map[string]any {
	t.Helper()
	data, err := os.ReadFile(PendingReposPath(configPath))
	if err != nil {
		t.Fatalf("read pending file: %v", err)
	}
	var out []map[string]any
	for _, line := range nonEmptyLines(string(data)) {
		var entry map[string]any
		if err := json.Unmarshal([]byte(line), &entry); err != nil {
			t.Fatalf("pending line is not JSON: %v (%q)", err, line)
		}
		out = append(out, entry)
	}
	return out
}

const reposConfig = `{
  "workspace_path": "/tmp/ws",
  "repos": [
    {"name": "a", "url": "https://host/a.git", "bare_path": "a.git", "overlay_paths": ["x"], "zz_unknown": 1},
    {"name": "b", "url": "https://host/b.git", "vcs": "sapling"}
  ],
  "run_targets": [{"name": "t", "command": "echo hi"}]
}`

func TestLoad_MovesReposToPending(t *testing.T) {
	dir := t.TempDir()
	path := writeRaw(t, dir, reposConfig)

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	// One pending line per entry, as it was.
	entries := pendingEntries(t, path)
	if len(entries) != 2 {
		t.Fatalf("pending entries = %d, want 2", len(entries))
	}
	if entries[0]["name"] != "a" || entries[0]["zz_unknown"] != float64(1) {
		t.Errorf("first pending entry = %v, want it as it was, unknown key kept", entries[0])
	}
	if entries[1]["name"] != "b" || entries[1]["vcs"] != "sapling" {
		t.Errorf("second pending entry = %v", entries[1])
	}

	// One backup, byte for byte.
	backups := backupFiles(t, dir)
	if len(backups) != 1 {
		t.Fatalf("backups = %d, want 1", len(backups))
	}
	backup, err := os.ReadFile(backups[0])
	if err != nil {
		t.Fatalf("read backup: %v", err)
	}
	if string(backup) != reposConfig {
		t.Errorf("backup differs from the original config")
	}

	// The config has no repos key, and the other settings stay.
	top := readTop(t, path)
	if _, ok := top["repos"]; ok {
		t.Error("repos key should be gone from config.json")
	}
	if cfg.GetWorkspacePath() != "/tmp/ws" {
		t.Errorf("workspace_path = %q, want it kept", cfg.GetWorkspacePath())
	}
	if len(cfg.GetRunTargets()) != 1 {
		t.Errorf("run_targets = %d, want 1", len(cfg.GetRunTargets()))
	}
}

func TestLoad_SecondLoadChangesNothing(t *testing.T) {
	dir := t.TempDir()
	path := writeRaw(t, dir, reposConfig)

	if _, err := Load(path); err != nil {
		t.Fatalf("first Load: %v", err)
	}
	pendingBefore, err := os.ReadFile(PendingReposPath(path))
	if err != nil {
		t.Fatalf("read pending: %v", err)
	}
	backupsBefore := backupFiles(t, dir)

	if _, err := Load(path); err != nil {
		t.Fatalf("second Load: %v", err)
	}
	pendingAfter, err := os.ReadFile(PendingReposPath(path))
	if err != nil {
		t.Fatalf("read pending: %v", err)
	}
	if !bytes.Equal(pendingBefore, pendingAfter) {
		t.Error("pending file changed on the second load")
	}
	if got := backupFiles(t, dir); !reflect.DeepEqual(got, backupsBefore) {
		t.Errorf("backups changed on the second load: %v -> %v", backupsBefore, got)
	}
}

func TestLoad_ReposWithAnotherMigration(t *testing.T) {
	dir := t.TempDir()
	path := writeRaw(t, dir, `{
  "workspace_path": "/tmp/ws",
  "source_code_manager": "git",
  "repos": [{"name": "a", "url": "https://host/a.git"}]
}`)

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.SourceCodeManagement != "git" {
		t.Errorf("source_code_management = %q, want the other migration applied", cfg.SourceCodeManagement)
	}
	if got := pendingEntries(t, path); len(got) != 1 || got[0]["name"] != "a" {
		t.Errorf("pending entries = %v, want [a]", got)
	}
}

func TestLoad_ReposPutBackAreAppended(t *testing.T) {
	dir := t.TempDir()
	path := writeRaw(t, dir, reposConfig)

	if _, err := Load(path); err != nil {
		t.Fatalf("first Load: %v", err)
	}
	firstBackups := backupFiles(t, dir)
	firstBackup, err := os.ReadFile(firstBackups[0])
	if err != nil {
		t.Fatalf("read first backup: %v", err)
	}

	// Put repos back, as a hand edit or an old binary would.
	second := `{"workspace_path": "/tmp/ws", "repos": [{"name": "c", "url": "https://host/c.git"}]}`
	if err := os.WriteFile(path, []byte(second), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	if _, err := Load(path); err != nil {
		t.Fatalf("second Load: %v", err)
	}

	entries := pendingEntries(t, path)
	if len(entries) != 3 || entries[2]["name"] != "c" {
		t.Errorf("pending entries = %v, want the third appended", entries)
	}
	backups := backupFiles(t, dir)
	if len(backups) != 2 {
		t.Fatalf("backups = %d, want 2", len(backups))
	}
	again, err := os.ReadFile(firstBackups[0])
	if err != nil {
		t.Fatalf("read first backup: %v", err)
	}
	if !bytes.Equal(firstBackup, again) {
		t.Error("the first backup changed")
	}
}

func TestLoad_UnwritablePendingKeepsConfig(t *testing.T) {
	dir := t.TempDir()
	path := writeRaw(t, dir, reposConfig)
	if err := os.MkdirAll(PendingReposPath(path), 0o755); err != nil {
		t.Fatalf("mkdir pending path: %v", err)
	}

	if _, err := Load(path); err == nil {
		t.Fatal("Load with an unwritable pending path: want error, got nil")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read config: %v", err)
	}
	if string(data) != reposConfig {
		t.Error("config.json changed although the load failed")
	}
}

func TestLoad_EmptyAndNullReposKeyRemoved(t *testing.T) {
	for _, value := range []string{"[]", "null"} {
		t.Run(value, func(t *testing.T) {
			dir := t.TempDir()
			path := writeRaw(t, dir, `{"workspace_path": "/tmp/ws", "repos": `+value+`}`)

			if _, err := Load(path); err != nil {
				t.Fatalf("Load: %v", err)
			}
			if _, ok := readTop(t, path)["repos"]; ok {
				t.Error("repos key should be removed")
			}
			if _, err := os.Stat(PendingReposPath(path)); !os.IsNotExist(err) {
				t.Errorf("no pending file expected, stat err = %v", err)
			}
			if backups := backupFiles(t, dir); len(backups) != 0 {
				t.Errorf("backups = %d, want 0", len(backups))
			}
		})
	}
}

func TestLoad_InvalidConfigKeepsPending(t *testing.T) {
	dir := t.TempDir()
	// A setting Validate rejects, next to the repos.
	bad := `{
  "workspace_path": "/tmp/ws",
  "run_targets": [{"name": "", "command": "echo hi"}],
  "repos": [{"name": "a", "url": "https://host/a.git"}]
}`
	path := writeRaw(t, dir, bad)

	if _, err := Load(path); err == nil {
		t.Fatal("Load with an invalid setting: want error, got nil")
	}
	if got := pendingEntries(t, path); len(got) != 1 || got[0]["name"] != "a" {
		t.Fatalf("pending entries = %v, want [a] kept", got)
	}

	// The setting fixed: the load succeeds, and the pending file still holds
	// the entries for the consumer.
	fixed := `{"workspace_path": "/tmp/ws", "run_targets": [{"name": "t", "command": "echo hi"}]}`
	if err := os.WriteFile(path, []byte(fixed), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	if _, err := Load(path); err != nil {
		t.Fatalf("Load after the fix: %v", err)
	}
	if got := pendingEntries(t, path); len(got) != 1 {
		t.Errorf("pending entries after the fix = %v, want [a]", got)
	}
}

func TestReload_MovesReposAndRunsConsumerOnce(t *testing.T) {
	dir := t.TempDir()
	path := writeRaw(t, dir, `{"workspace_path": "/tmp/ws"}`)
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	var calls atomic.Int32
	var pendingAtCall []map[string]any
	var workspaceAtCall string
	cfg.SetPendingReposConsumer(func() {
		calls.Add(1)
		pendingAtCall = pendingEntries(t, path)
		workspaceAtCall = cfg.GetWorkspacePath()
	})

	// A hand edit puts repos back and changes a setting.
	edited := `{"workspace_path": "/tmp/ws2", "repos": [{"name": "a", "url": "https://host/a.git"}]}`
	if err := os.WriteFile(path, []byte(edited), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	if err := cfg.Reload(); err != nil {
		t.Fatalf("Reload: %v", err)
	}

	if got := calls.Load(); got != 1 {
		t.Errorf("consumer calls = %d, want 1", got)
	}
	if len(pendingAtCall) != 1 || pendingAtCall[0]["name"] != "a" {
		t.Errorf("pending at the consumer call = %v, want [a]", pendingAtCall)
	}
	if workspaceAtCall != "/tmp/ws2" {
		t.Errorf("workspace path at the consumer call = %q, want the new value visible", workspaceAtCall)
	}
	if _, ok := readTop(t, path)["repos"]; ok {
		t.Error("repos key should be gone after the reload")
	}
}

func TestReload_NoReposNotRewritten(t *testing.T) {
	dir := t.TempDir()
	content := `{
  "config_version": "x",
  "workspace_path": "/tmp/ws",
  "quick_launch": [{"name": "q", "target": "claude", "prompt": "hi"}]
}`
	path := writeRaw(t, dir, content)
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	// Load may rewrite the file; the claim is about Reload.
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read config: %v", err)
	}

	if err := cfg.Reload(); err != nil {
		t.Fatalf("Reload: %v", err)
	}
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read config: %v", err)
	}
	if !bytes.Equal(before, after) {
		t.Errorf("Reload rewrote a config with no repos:\nbefore: %s\nafter:  %s", before, after)
	}
}

func TestConfigSaveHasNoReposKey(t *testing.T) {
	cfg := CreateDefault(filepath.Join(t.TempDir(), "config.json"))
	if err := cfg.Save(); err != nil {
		t.Fatalf("Save: %v", err)
	}
	data, err := os.ReadFile(cfg.FilePath())
	if err != nil {
		t.Fatalf("read config: %v", err)
	}
	if strings.Contains(string(data), `"repos":`) && readTopHas(t, cfg.FilePath(), "repos") {
		t.Error("a saved config should carry no top-level repos key")
	}
}

// readTopHas reports whether the config file has a top-level key.
func readTopHas(t *testing.T, path, key string) bool {
	t.Helper()
	_, ok := readTop(t, path)[key]
	return ok
}
