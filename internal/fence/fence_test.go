package fence

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestWrapWritesArtifactsAndCommand(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "sess-123")
	ws := filepath.Join(t.TempDir(), "repo-001")
	extraWrite := filepath.Join(t.TempDir(), "repo.git", "worktrees", "repo-001")
	cfg := Config{
		FenceCommand:       "fence",
		WorkspacePath:      ws,
		ExtraWritablePaths: []string{extraWrite},
		AllowedDomains:     []string{"api.z.ai"},
		DataDir:            dir,
	}
	const command = `SCHMUX_ENABLED=1 SCHMUX_SESSION_ID=sess-123 claude --continue`

	got, err := Wrap(context.Background(), cfg, command)
	if err != nil {
		t.Fatalf("Wrap: %v", err)
	}

	cmdPath := filepath.Join(dir, "cmd.sh")
	settingsPath := filepath.Join(dir, "settings.json")
	monitorLogPath := filepath.Join(dir, "monitor.log")
	want := "fence -m --fence-log-file '" + monitorLogPath + "' --settings '" + settingsPath + "' /bin/sh '" + cmdPath + "'"
	if got != want {
		t.Errorf("Wrap returned\n  %q\nwant\n  %q", got, want)
	}

	// cmd.sh exports workspace-local caches before the verbatim command.
	gotCmd, err := os.ReadFile(cmdPath)
	if err != nil {
		t.Fatalf("read cmd.sh: %v", err)
	}
	if !strings.Contains(string(gotCmd), "export GOCACHE='"+filepath.Join(ws, ".cache", "schmux-fence", "go-build")+"'") {
		t.Errorf("cmd.sh = %q, want workspace-local GOCACHE export", gotCmd)
	}
	if !strings.Contains(string(gotCmd), "export GIT_TEMPLATE_DIR='"+filepath.Join(ws, ".cache", "schmux-fence", "git-template")+"'") {
		t.Errorf("cmd.sh = %q, want empty GIT_TEMPLATE_DIR export", gotCmd)
	}
	if !strings.Contains(string(gotCmd), "export STATICCHECK_CACHE='"+filepath.Join(ws, ".cache", "schmux-fence", "staticcheck")+"'") {
		t.Errorf("cmd.sh = %q, want workspace-local STATICCHECK_CACHE export", gotCmd)
	}
	if !strings.Contains(string(gotCmd), `export GOFLAGS="${GOFLAGS:+$GOFLAGS }-modcacherw"`) {
		t.Errorf("cmd.sh = %q, want GOFLAGS to keep module cache writable", gotCmd)
	}
	if strings.Contains(string(gotCmd), "GOTELEMETRY") {
		t.Errorf("cmd.sh = %q, should not export non-settable GOTELEMETRY", gotCmd)
	}
	if strings.Contains(string(gotCmd), "GOMODCACHE") {
		t.Errorf("cmd.sh = %q, should not redirect GOMODCACHE", gotCmd)
	}
	for _, tempVar := range []string{"TMPDIR", "TMP", "TEMP"} {
		if strings.Contains(string(gotCmd), "export "+tempVar+"=") {
			t.Errorf("cmd.sh = %q, should not redirect %s", gotCmd, tempVar)
		}
	}
	if !strings.HasSuffix(string(gotCmd), command) {
		t.Errorf("cmd.sh = %q, want verbatim command suffix %q", gotCmd, command)
	}

	// settings.json: extends "code", one allowRead (cmd.sh), allowWrite =
	// workspace + extra paths in that order.
	raw, err := os.ReadFile(settingsPath)
	if err != nil {
		t.Fatalf("read settings.json: %v", err)
	}
	var s settings
	if err := json.Unmarshal(raw, &s); err != nil {
		t.Fatalf("unmarshal settings: %v", err)
	}
	if s.Extends != "code" {
		t.Errorf("extends = %q, want code", s.Extends)
	}
	wantDomains := []string{"mcp.posthog.com", "api.z.ai"}
	if s.Network == nil || len(s.Network.AllowedDomains) != len(wantDomains) {
		t.Errorf("network.allowedDomains = %+v, want %v", s.Network, wantDomains)
	} else {
		for i := range wantDomains {
			if s.Network.AllowedDomains[i] != wantDomains[i] {
				t.Errorf("network.allowedDomains = %v, want %v", s.Network.AllowedDomains, wantDomains)
				break
			}
		}
	}
	if s.Network == nil || !s.Network.AllowAllUnixSockets {
		t.Errorf("network.allowAllUnixSockets = %+v, want true", s.Network)
	}
	if len(s.Filesystem.AllowRead) != 1 || s.Filesystem.AllowRead[0] != cmdPath {
		t.Errorf("allowRead = %v, want [%s]", s.Filesystem.AllowRead, cmdPath)
	}
	configDir, err := os.UserConfigDir()
	if err != nil {
		t.Fatalf("UserConfigDir: %v", err)
	}
	goTelemetryDir := filepath.Join(configDir, "go", "telemetry")
	wantWrite := []string{ws, extraWrite, goTelemetryDir}
	if len(s.Filesystem.AllowWrite) != len(wantWrite) {
		t.Errorf("allowWrite = %v, want %v", s.Filesystem.AllowWrite, wantWrite)
	} else {
		for i := range wantWrite {
			if s.Filesystem.AllowWrite[i] != wantWrite[i] {
				t.Errorf("allowWrite = %v, want %v", s.Filesystem.AllowWrite, wantWrite)
				break
			}
		}
	}
	for _, dir := range []string{
		filepath.Join(ws, ".cache", "schmux-fence", "go-build"),
		filepath.Join(ws, ".cache", "schmux-fence", "git-template"),
		filepath.Join(ws, ".cache", "schmux-fence", "staticcheck"),
		filepath.Join(ws, ".cache", "schmux-fence", "npm"),
	} {
		if _, err := os.Stat(dir); err != nil {
			t.Errorf("expected local cache dir %s: %v", dir, err)
		}
	}
}

func TestWrapFileModes(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "sess-modes")
	cfg := Config{FenceCommand: "fence", WorkspacePath: t.TempDir(), DataDir: dir}
	if _, err := Wrap(context.Background(), cfg, "echo hi"); err != nil {
		t.Fatalf("Wrap: %v", err)
	}
	assertMode := func(path string, want os.FileMode) {
		t.Helper()
		fi, err := os.Stat(path)
		if err != nil {
			t.Fatalf("stat %s: %v", path, err)
		}
		if fi.Mode().Perm() != want {
			t.Errorf("%s mode = %o, want %o", path, fi.Mode().Perm(), want)
		}
	}
	assertMode(dir, 0o700)
	assertMode(filepath.Join(dir, "cmd.sh"), 0o600)
	assertMode(filepath.Join(dir, "settings.json"), 0o600)
}

func TestWorkspaceExcludePatterns(t *testing.T) {
	got := WorkspaceExcludePatterns()
	want := []string{".cache/schmux-fence/"}
	if len(got) != len(want) {
		t.Fatalf("WorkspaceExcludePatterns() = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("WorkspaceExcludePatterns()[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}
