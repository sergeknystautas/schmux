package main

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// errIsExitCode reports whether err carries the given migrate exit code.
// Test helper for asserting on the exit code wired into migrateExitError.
func errIsExitCode(err error, code int) bool {
	var me *migrateExitError
	if errors.As(err, &me) {
		return me.exitCode == code
	}
	return false
}

// TestConfigMigrateConvertsLegacyForm verifies that string-form sapling commands
// are converted to argv arrays and that a backup file is created.
func TestConfigMigrateConvertsLegacyForm(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.json")
	legacy := `{"sapling_commands":{"create_workspace":"vcs-clone {{.RepoIdentifier}} {{.DestPath}}"}}`
	if err := os.WriteFile(cfgPath, []byte(legacy), 0600); err != nil {
		t.Fatal(err)
	}

	if err := runConfigMigrate(cfgPath, false /*dryRun*/); err != nil {
		t.Fatalf("migrate failed: %v", err)
	}

	data, err := os.ReadFile(cfgPath)
	if err != nil {
		t.Fatalf("read migrated config: %v", err)
	}
	var got map[string]interface{}
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("parse migrated config: %v", err)
	}
	cmds, ok := got["sapling_commands"].(map[string]interface{})
	if !ok {
		t.Fatalf("sapling_commands not an object: %T", got["sapling_commands"])
	}
	created, ok := cmds["create_workspace"].([]interface{})
	if !ok {
		t.Fatalf("create_workspace not an array: %T", cmds["create_workspace"])
	}
	if len(created) != 3 || created[0] != "vcs-clone" || created[1] != "{{.RepoIdentifier}}" || created[2] != "{{.DestPath}}" {
		t.Errorf("conversion produced %v, want [vcs-clone, {{.RepoIdentifier}}, {{.DestPath}}]", created)
	}

	// Backup file must exist with original contents.
	bakData, err := os.ReadFile(cfgPath + ".bak")
	if err != nil {
		t.Fatalf("expected .bak backup, got: %v", err)
	}
	if string(bakData) != legacy {
		t.Errorf("backup contents differ from original\n got: %s\nwant: %s", bakData, legacy)
	}

	// Migrated file must be at mode 0600 (per §2.2).
	info, err := os.Stat(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	if mode := info.Mode().Perm(); mode != 0600 {
		t.Errorf("migrated file mode = %#o, want 0600", mode)
	}
}

// TestConfigMigrateConvertsExternalDiffCommands verifies that the array-of-structs
// shape (each element with a `command` field) is migrated correctly.
func TestConfigMigrateConvertsExternalDiffCommands(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.json")
	legacy := `{"external_diff_commands":[
		{"name":"meld","command":"meld LOCAL REMOTE"},
		{"name":"already-migrated","command":["code","--diff"]}
	]}`
	if err := os.WriteFile(cfgPath, []byte(legacy), 0600); err != nil {
		t.Fatal(err)
	}

	if err := runConfigMigrate(cfgPath, false); err != nil {
		t.Fatalf("migrate failed: %v", err)
	}

	data, err := os.ReadFile(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	var got map[string]interface{}
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatal(err)
	}
	arr, ok := got["external_diff_commands"].([]interface{})
	if !ok || len(arr) != 2 {
		t.Fatalf("external_diff_commands not a 2-element array: %v", got["external_diff_commands"])
	}
	first := arr[0].(map[string]interface{})
	cmd, ok := first["command"].([]interface{})
	if !ok {
		t.Fatalf("first.command not an array: %T", first["command"])
	}
	if len(cmd) != 3 || cmd[0] != "meld" || cmd[1] != "LOCAL" || cmd[2] != "REMOTE" {
		t.Errorf("first element converted to %v, want [meld LOCAL REMOTE]", cmd)
	}
	// Second element was already in argv form — must still be argv form, not double-wrapped.
	second := arr[1].(map[string]interface{})
	cmd2, ok := second["command"].([]interface{})
	if !ok {
		t.Fatalf("second.command not an array: %T", second["command"])
	}
	if len(cmd2) != 2 || cmd2[0] != "code" || cmd2[1] != "--diff" {
		t.Errorf("second element corrupted: %v", cmd2)
	}
}

// TestConfigMigrateConvertsTelemetryCommand verifies the single-nested-field shape.
func TestConfigMigrateConvertsTelemetryCommand(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.json")
	legacy := `{"telemetry":{"command":"vendor-logger vendor_log_table"}}`
	if err := os.WriteFile(cfgPath, []byte(legacy), 0600); err != nil {
		t.Fatal(err)
	}
	if err := runConfigMigrate(cfgPath, false); err != nil {
		t.Fatalf("migrate failed: %v", err)
	}
	data, err := os.ReadFile(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	var got map[string]interface{}
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatal(err)
	}
	tel, ok := got["telemetry"].(map[string]interface{})
	if !ok {
		t.Fatalf("telemetry not an object: %T", got["telemetry"])
	}
	cmd, ok := tel["command"].([]interface{})
	if !ok {
		t.Fatalf("telemetry.command not an array: %T", tel["command"])
	}
	if len(cmd) != 2 || cmd[0] != "vendor-logger" || cmd[1] != "vendor_log_table" {
		t.Errorf("telemetry.command converted to %v, want [vendor-logger vendor_log_table]", cmd)
	}
}

// TestConfigMigrateConvertsRemoteVCSCommands verifies that the
// nested-inside-each-remote-profile shape is walked correctly.
func TestConfigMigrateConvertsRemoteVCSCommands(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.json")
	legacy := `{"remote_profiles":[
		{"id":"alpha","display_name":"Alpha","vcs":"sapling","workspace_path":"/tmp/a","flavors":[],
		 "remote_vcs_commands":{"create_worktree":"sl clone {{.RepoBasePath}} {{.DestPath}}"}},
		{"id":"beta","display_name":"Beta","vcs":"git","workspace_path":"/tmp/b","flavors":[],
		 "remote_vcs_commands":{"check_dirty":["sl","status","--cwd","{{.WorkspacePath}}"]}}
	]}`
	if err := os.WriteFile(cfgPath, []byte(legacy), 0600); err != nil {
		t.Fatal(err)
	}
	if err := runConfigMigrate(cfgPath, false); err != nil {
		t.Fatalf("migrate failed: %v", err)
	}
	data, err := os.ReadFile(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	var got map[string]interface{}
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatal(err)
	}
	profiles := got["remote_profiles"].([]interface{})
	if len(profiles) != 2 {
		t.Fatalf("expected 2 profiles, got %d", len(profiles))
	}

	first := profiles[0].(map[string]interface{})
	rvcs1 := first["remote_vcs_commands"].(map[string]interface{})
	cmd1, ok := rvcs1["create_worktree"].([]interface{})
	if !ok {
		t.Fatalf("profile[0].remote_vcs_commands.create_worktree not an array: %T", rvcs1["create_worktree"])
	}
	if len(cmd1) != 4 || cmd1[0] != "sl" || cmd1[1] != "clone" {
		t.Errorf("profile[0].create_worktree converted to %v, want [sl clone {{.RepoBasePath}} {{.DestPath}}]", cmd1)
	}

	// Already-array second profile must remain unchanged.
	second := profiles[1].(map[string]interface{})
	rvcs2 := second["remote_vcs_commands"].(map[string]interface{})
	cmd2, ok := rvcs2["check_dirty"].([]interface{})
	if !ok {
		t.Fatalf("profile[1].remote_vcs_commands.check_dirty not an array: %T", rvcs2["check_dirty"])
	}
	if len(cmd2) != 4 || cmd2[0] != "sl" {
		t.Errorf("profile[1].check_dirty corrupted: %v", cmd2)
	}
}

// TestConfigMigrateMixedFile verifies that a file with both legacy and migrated
// keys touches only the legacy ones.
func TestConfigMigrateMixedFile(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.json")
	mixed := `{"sapling_commands":{"create_workspace":"vcs-clone X Y","remove_workspace":["rm","-rf","{{.WorkspacePath}}"]}}`
	if err := os.WriteFile(cfgPath, []byte(mixed), 0600); err != nil {
		t.Fatal(err)
	}
	if err := runConfigMigrate(cfgPath, false); err != nil {
		t.Fatalf("migrate failed: %v", err)
	}
	data, err := os.ReadFile(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	var got map[string]interface{}
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatal(err)
	}
	cmds := got["sapling_commands"].(map[string]interface{})
	created, ok := cmds["create_workspace"].([]interface{})
	if !ok {
		t.Fatalf("create_workspace not an array after migrate: %T", cmds["create_workspace"])
	}
	if len(created) != 3 || created[0] != "vcs-clone" {
		t.Errorf("legacy key not converted: %v", created)
	}
	removed, ok := cmds["remove_workspace"].([]interface{})
	if !ok || len(removed) != 3 || removed[0] != "rm" {
		t.Errorf("already-migrated key corrupted: %v", removed)
	}
}

// TestConfigMigrateIdempotent verifies that running migrate twice on the same
// already-migrated config produces no change.
func TestConfigMigrateIdempotent(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.json")
	already := `{"sapling_commands":{"create_workspace":["vcs-clone","{{.RepoIdentifier}}","{{.DestPath}}"]}}`
	if err := os.WriteFile(cfgPath, []byte(already), 0600); err != nil {
		t.Fatal(err)
	}

	if err := runConfigMigrate(cfgPath, false); err != nil {
		t.Fatalf("migrate on already-migrated config failed: %v", err)
	}
	got, err := os.ReadFile(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(got), `["vcs-clone"`) {
		t.Errorf("idempotent migrate corrupted config: %s", got)
	}

	// No-op should not have created a backup file.
	if _, err := os.Stat(cfgPath + ".bak"); !os.IsNotExist(err) {
		t.Errorf("idempotent migrate created a backup unexpectedly: %v", err)
	}
}

// TestConfigMigrateDryRun verifies that --dry-run does not write the file or
// the backup.
func TestConfigMigrateDryRun(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.json")
	legacy := `{"sapling_commands":{"create_workspace":"vcs-clone X Y"}}`
	if err := os.WriteFile(cfgPath, []byte(legacy), 0600); err != nil {
		t.Fatal(err)
	}

	if err := runConfigMigrate(cfgPath, true /*dryRun*/); err != nil {
		t.Fatalf("dry-run failed: %v", err)
	}
	got, err := os.ReadFile(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != legacy {
		t.Errorf("dry-run modified file: %s", got)
	}
	if _, err := os.Stat(cfgPath + ".bak"); !os.IsNotExist(err) {
		t.Errorf("dry-run created backup, want no backup")
	}
}

// TestConfigMigrateRejectsShellFeatures verifies spec §4.1: strings with shell
// features (pipes, redirection, subshells, etc.) are refused with an error
// pointing to manual conversion.
func TestConfigMigrateRejectsShellFeatures(t *testing.T) {
	cases := []struct {
		name  string
		input string
	}{
		{"pipe", `{"sapling_commands":{"check_repo_base":"eden list --json | jq -r .foo"}}`},
		{"redirect", `{"sapling_commands":{"check_repo_base":"sl status > /tmp/x"}}`},
		{"input_redirect", `{"sapling_commands":{"check_repo_base":"cat < /tmp/x"}}`},
		{"and_and", `{"sapling_commands":{"check_repo_base":"sl pull && sl status"}}`},
		{"or_or", `{"sapling_commands":{"check_repo_base":"sl pull || true"}}`},
		{"semicolon", `{"sapling_commands":{"check_repo_base":"sl pull; sl status"}}`},
		{"backtick", "{\"sapling_commands\":{\"check_repo_base\":\"echo `whoami`\"}}"},
		{"dollar_paren", `{"sapling_commands":{"check_repo_base":"echo $(whoami)"}}`},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			cfgPath := filepath.Join(dir, "config.json")
			if err := os.WriteFile(cfgPath, []byte(tc.input), 0600); err != nil {
				t.Fatal(err)
			}
			err := runConfigMigrate(cfgPath, false)
			if err == nil {
				t.Fatal("expected migrate to refuse a string with shell features")
			}
			if !strings.Contains(err.Error(), "shell features") {
				t.Errorf("error doesn't mention shell features: %v", err)
			}
			if !errIsExitCode(err, 2) {
				t.Errorf("shell-feature error should carry exit code 2, got: %v", err)
			}
			// File must not have been modified or backed up.
			got, _ := os.ReadFile(cfgPath)
			if string(got) != tc.input {
				t.Errorf("rejected migrate modified file: %s", got)
			}
			if _, err := os.Stat(cfgPath + ".bak"); !os.IsNotExist(err) {
				t.Errorf("rejected migrate created backup, want no backup")
			}
		})
	}
}

// TestConfigMigrateMissingFile verifies that a missing config gives a clear error.
func TestConfigMigrateMissingFile(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "does-not-exist.json")
	err := runConfigMigrate(cfgPath, false)
	if err == nil {
		t.Fatal("expected error for missing config file")
	}
	if !strings.Contains(err.Error(), cfgPath) {
		t.Errorf("error does not mention path: %v", err)
	}
}

// TestConfigMigrateInvalidJSON verifies that a malformed config gives a clear error.
func TestConfigMigrateInvalidJSON(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.json")
	if err := os.WriteFile(cfgPath, []byte(`{not json`), 0600); err != nil {
		t.Fatal(err)
	}
	err := runConfigMigrate(cfgPath, false)
	if err == nil {
		t.Fatal("expected parse error")
	}
	if !strings.Contains(err.Error(), "parse") {
		t.Errorf("error doesn't mention parse: %v", err)
	}
}

// TestConfigMigrateNoLegacyKeys verifies that a config without any of the four
// schema sites is a no-op.
func TestConfigMigrateNoLegacyKeys(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.json")
	clean := `{"daemon_port":7337,"workspace_dir":"/tmp/ws"}`
	if err := os.WriteFile(cfgPath, []byte(clean), 0600); err != nil {
		t.Fatal(err)
	}
	if err := runConfigMigrate(cfgPath, false); err != nil {
		t.Fatalf("migrate failed: %v", err)
	}
	got, err := os.ReadFile(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != clean {
		t.Errorf("no-op migrate altered file: %s", got)
	}
	if _, err := os.Stat(cfgPath + ".bak"); !os.IsNotExist(err) {
		t.Errorf("no-op migrate created backup, want no backup")
	}
}
