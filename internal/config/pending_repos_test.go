package config

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

// nonEmptyLines splits a pending file's content into its non-empty lines.
func nonEmptyLines(content string) []string {
	var lines []string
	for _, line := range strings.Split(content, "\n") {
		if strings.TrimSpace(line) != "" {
			lines = append(lines, line)
		}
	}
	return lines
}

// backupFiles lists the timestamped config backups in dir.
func backupFiles(t *testing.T, dir string) []string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read dir: %v", err)
	}
	var backups []string
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), "config.json.") && strings.HasSuffix(e.Name(), ".bak") {
			backups = append(backups, filepath.Join(dir, e.Name()))
		}
	}
	return backups
}

// jsonEqual reports whether two raw JSON values are equal.
func jsonEqual(a, b json.RawMessage) bool {
	var av, bv any
	if err := json.Unmarshal(a, &av); err != nil {
		return false
	}
	if err := json.Unmarshal(b, &bv); err != nil {
		return false
	}
	return reflect.DeepEqual(av, bv)
}

func TestAppendPendingRepos(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.json")
	pending := PendingReposPath(cfgPath)

	entry := json.RawMessage(`{"name":"a","url":"https://host/a.git","unknown_key":42}`)
	if err := appendPendingRepos(cfgPath, []json.RawMessage{entry}); err != nil {
		t.Fatalf("appendPendingRepos: %v", err)
	}

	data, err := os.ReadFile(pending)
	if err != nil {
		t.Fatalf("read pending file: %v", err)
	}
	if !strings.HasPrefix(string(data), "\n") {
		t.Errorf("pending file should start with a newline, got %q", string(data))
	}
	lines := nonEmptyLines(string(data))
	if len(lines) != 1 {
		t.Fatalf("pending file holds %d entries, want 1: %q", len(lines), string(data))
	}
	if strings.Contains(lines[0], "  ") {
		t.Errorf("entry should be compacted, got %q", lines[0])
	}
	var got map[string]any
	if err := json.Unmarshal([]byte(lines[0]), &got); err != nil {
		t.Fatalf("entry is not JSON: %v", err)
	}
	if got["name"] != "a" {
		t.Errorf("entry name = %v, want %q", got["name"], "a")
	}
	if _, ok := got["unknown_key"]; !ok {
		t.Error("entry should keep the unknown key")
	}

	info, err := os.Stat(pending)
	if err != nil {
		t.Fatalf("stat pending file: %v", err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Errorf("pending file mode = %v, want 0600", info.Mode().Perm())
	}

	// A torn last line does not swallow the next append.
	if err := os.WriteFile(pending, []byte("\n{\"name\":\"torn\""), 0o600); err != nil {
		t.Fatalf("write torn pending file: %v", err)
	}
	if err := appendPendingRepos(cfgPath, []json.RawMessage{json.RawMessage(`{"name":"b","url":"https://host/b.git"}`)}); err != nil {
		t.Fatalf("appendPendingRepos: %v", err)
	}
	data, err = os.ReadFile(pending)
	if err != nil {
		t.Fatalf("read pending file: %v", err)
	}
	lines = nonEmptyLines(string(data))
	if len(lines) != 2 {
		t.Fatalf("pending file holds %d entries, want 2: %q", len(lines), string(data))
	}
	var appended map[string]any
	if err := json.Unmarshal([]byte(lines[1]), &appended); err != nil {
		t.Fatalf("the appended entry is not a whole line: %v (%q)", err, lines[1])
	}
	if appended["name"] != "b" {
		t.Errorf("appended name = %v, want %q", appended["name"], "b")
	}
}

func TestBackupConfig(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.json")
	original := []byte(`{"a":1}`)
	if err := os.WriteFile(cfgPath, original, 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	now := time.Date(2026, 11, 22, 10, 30, 0, 123456789, time.UTC)
	if err := backupConfig(cfgPath, now); err != nil {
		t.Fatalf("backupConfig: %v", err)
	}

	backups := backupFiles(t, dir)
	if len(backups) != 1 {
		t.Fatalf("backups = %d, want 1", len(backups))
	}
	// Go's fractional-second layout drops trailing zeros.
	want := cfgPath + ".2026-11-22T10-30-00.123456789.bak"
	if backups[0] != want {
		t.Errorf("backup path = %q, want %q", backups[0], want)
	}
	data, err := os.ReadFile(backups[0])
	if err != nil {
		t.Fatalf("read backup: %v", err)
	}
	if !bytes.Equal(data, original) {
		t.Errorf("backup = %q, want the config bytes %q", data, original)
	}
	info, err := os.Stat(backups[0])
	if err != nil {
		t.Fatalf("stat backup: %v", err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Errorf("backup mode = %v, want 0600", info.Mode().Perm())
	}

	// The same timestamp twice fails, and the first backup is unchanged.
	if err := backupConfig(cfgPath, now); err == nil {
		t.Error("a second backup with the same timestamp: want error, got nil")
	}
	data, err = os.ReadFile(backups[0])
	if err != nil {
		t.Fatalf("read backup: %v", err)
	}
	if !bytes.Equal(data, original) {
		t.Errorf("backup = %q, want it unchanged %q", data, original)
	}
}

func TestMoveReposToPending_Apply(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.json")
	original := `{"repos":[{"name":"a","url":"https://host/a.git","bare_path":"a.git"}]}`
	if err := os.WriteFile(cfgPath, []byte(original), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	cfg := CreateDefault(cfgPath)
	var raw map[string]json.RawMessage
	if err := json.Unmarshal([]byte(original), &raw); err != nil {
		t.Fatalf("parse config: %v", err)
	}

	if err := moveReposToPending.Apply(raw, cfg); err != nil {
		t.Fatalf("Apply: %v", err)
	}

	// The entries are pending as they were.
	data, err := os.ReadFile(PendingReposPath(cfgPath))
	if err != nil {
		t.Fatalf("read pending file: %v", err)
	}
	lines := nonEmptyLines(string(data))
	if len(lines) != 1 {
		t.Fatalf("pending file holds %d entries, want 1: %q", len(lines), string(data))
	}
	var got map[string]any
	if err := json.Unmarshal([]byte(lines[0]), &got); err != nil {
		t.Fatalf("pending entry is not JSON: %v", err)
	}
	want := map[string]any{"name": "a", "url": "https://host/a.git", "bare_path": "a.git"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("pending entry = %v, want %v", got, want)
	}

	// One backup, byte for byte.
	backups := backupFiles(t, dir)
	if len(backups) != 1 {
		t.Fatalf("backups = %d, want 1", len(backups))
	}
	backupData, err := os.ReadFile(backups[0])
	if err != nil {
		t.Fatalf("read backup: %v", err)
	}
	if string(backupData) != original {
		t.Errorf("backup = %q, want the config bytes %q", backupData, original)
	}
}

func TestMoveReposToPending_EmptyList(t *testing.T) {
	for _, content := range []string{`{"repos":[]}`, `{"repos":null}`} {
		t.Run(content, func(t *testing.T) {
			dir := t.TempDir()
			cfgPath := filepath.Join(dir, "config.json")
			if err := os.WriteFile(cfgPath, []byte(content), 0o600); err != nil {
				t.Fatalf("write config: %v", err)
			}
			cfg := CreateDefault(cfgPath)
			var raw map[string]json.RawMessage
			if err := json.Unmarshal([]byte(content), &raw); err != nil {
				t.Fatalf("parse config: %v", err)
			}

			if err := moveReposToPending.Apply(raw, cfg); err != nil {
				t.Fatalf("Apply: %v", err)
			}
			if _, err := os.Stat(PendingReposPath(cfgPath)); !os.IsNotExist(err) {
				t.Error("no pending file should be written")
			}
			if backups := backupFiles(t, dir); len(backups) != 0 {
				t.Errorf("backups = %d, want 0", len(backups))
			}
		})
	}
}

func TestMoveReposToPending_NotAList(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.json")
	if err := os.WriteFile(cfgPath, []byte(`{"repos":{"a":1}}`), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	cfg := CreateDefault(cfgPath)
	raw := map[string]json.RawMessage{"repos": json.RawMessage(`{"a":1}`)}

	if err := moveReposToPending.Apply(raw, cfg); err == nil {
		t.Error("Apply with a repos value that is not a list: want error, got nil")
	}
}

func TestMoveReposToPending_Unwritable(t *testing.T) {
	t.Run("pending path is a directory", func(t *testing.T) {
		dir := t.TempDir()
		cfgPath := filepath.Join(dir, "config.json")
		original := `{"repos":[{"name":"a","url":"https://host/a.git"}]}`
		if err := os.WriteFile(cfgPath, []byte(original), 0o600); err != nil {
			t.Fatalf("write config: %v", err)
		}
		if err := os.MkdirAll(PendingReposPath(cfgPath), 0755); err != nil {
			t.Fatalf("mkdir pending path: %v", err)
		}

		cfg := CreateDefault(cfgPath)
		var raw map[string]json.RawMessage
		if err := json.Unmarshal([]byte(original), &raw); err != nil {
			t.Fatalf("parse config: %v", err)
		}
		if err := moveReposToPending.Apply(raw, cfg); err == nil {
			t.Error("Apply with an unwritable pending path: want error, got nil")
		}

		data, err := os.ReadFile(cfgPath)
		if err != nil {
			t.Fatalf("read config: %v", err)
		}
		if string(data) != original {
			t.Errorf("config = %q, want it unchanged %q", data, original)
		}
	})

	t.Run("config dir refuses a new backup", func(t *testing.T) {
		dir := t.TempDir()
		cfgPath := filepath.Join(dir, "config.json")
		original := `{"repos":[{"name":"a","url":"https://host/a.git"}]}`
		if err := os.WriteFile(cfgPath, []byte(original), 0o600); err != nil {
			t.Fatalf("write config: %v", err)
		}
		if err := os.Chmod(dir, 0500); err != nil {
			t.Fatalf("chmod config dir: %v", err)
		}
		t.Cleanup(func() { os.Chmod(dir, 0700) })

		cfg := CreateDefault(cfgPath)
		var raw map[string]json.RawMessage
		if err := json.Unmarshal([]byte(original), &raw); err != nil {
			t.Fatalf("parse config: %v", err)
		}
		if err := moveReposToPending.Apply(raw, cfg); err == nil {
			t.Error("Apply with an unwritable config dir: want error, got nil")
		}

		data, err := os.ReadFile(cfgPath)
		if err != nil {
			t.Fatalf("read config: %v", err)
		}
		if string(data) != original {
			t.Errorf("config = %q, want it unchanged %q", data, original)
		}
	})
}

func TestDisableAuthRaw_KeepsOtherKeys(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	original := `{"access_control":{"enabled":true,"provider":"github"},"repos":[{"name":"a","url":"https://host/a.git"}],"zz_unknown":{"x":1}}`
	if err := os.WriteFile(path, []byte(original), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	if err := DisableAuthRaw(path); err != nil {
		t.Fatalf("DisableAuthRaw: %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read config: %v", err)
	}
	var got map[string]json.RawMessage
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("parse config: %v", err)
	}
	var access map[string]any
	if err := json.Unmarshal(got["access_control"], &access); err != nil {
		t.Fatalf("parse access_control: %v", err)
	}
	if access["enabled"] != false {
		t.Errorf("access_control.enabled = %v, want false", access["enabled"])
	}

	var want map[string]json.RawMessage
	if err := json.Unmarshal([]byte(original), &want); err != nil {
		t.Fatalf("parse original config: %v", err)
	}
	for _, key := range []string{"repos", "zz_unknown"} {
		if !jsonEqual(got[key], want[key]) {
			t.Errorf("%s changed: got %s, want %s", key, got[key], want[key])
		}
	}
}
