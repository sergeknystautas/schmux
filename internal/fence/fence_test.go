package fence

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
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
		AllowedDomains:     []string{"mcp.posthog.com", "api.z.ai"},
		Presets:            []string{"golang", "node", "python", "tmux"},
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

func TestWrapNoPresetsBaselineOnly(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "sess")
	ws := t.TempDir()
	if _, err := Wrap(context.Background(), Config{FenceCommand: "fence", WorkspacePath: ws, DataDir: dir}, "echo hi"); err != nil {
		t.Fatalf("Wrap: %v", err)
	}
	cmd, _ := os.ReadFile(filepath.Join(dir, "cmd.sh"))
	if !strings.Contains(string(cmd), "export GIT_TEMPLATE_DIR=") || !strings.Contains(string(cmd), "export XDG_CACHE_HOME=") {
		t.Errorf("baseline caches missing: %s", cmd)
	}
	for _, banned := range []string{"GOCACHE", "STATICCHECK_CACHE", "GOFLAGS", "NPM_CONFIG_CACHE", "PIP_CACHE_DIR", "DOCKER_CONFIG"} {
		if strings.Contains(string(cmd), banned) {
			t.Errorf("cmd.sh has %s without a preset: %s", banned, cmd)
		}
	}
	raw, _ := os.ReadFile(filepath.Join(dir, "settings.json"))
	var s settings
	if err := json.Unmarshal(raw, &s); err != nil {
		t.Fatal(err)
	}
	if s.Network != nil && s.Network.AllowAllUnixSockets {
		t.Errorf("allowAllUnixSockets should be false without tmux preset")
	}
	if len(s.Filesystem.AllowWrite) != 1 || s.Filesystem.AllowWrite[0] != ws {
		t.Errorf("allowWrite = %v, want [%s] (no telemetry without golang)", s.Filesystem.AllowWrite, ws)
	}
}

func TestWrapGolangPresetOnly(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "sess")
	ws := t.TempDir()
	if _, err := Wrap(context.Background(), Config{FenceCommand: "fence", WorkspacePath: ws, Presets: []string{"golang"}, DataDir: dir}, "echo hi"); err != nil {
		t.Fatalf("Wrap: %v", err)
	}
	cmd, _ := os.ReadFile(filepath.Join(dir, "cmd.sh"))
	if !strings.Contains(string(cmd), "export GOCACHE=") || !strings.Contains(string(cmd), `export GOFLAGS="${GOFLAGS:+$GOFLAGS }-modcacherw"`) {
		t.Errorf("golang preset missing GOCACHE/GOFLAGS: %s", cmd)
	}
	if strings.Contains(string(cmd), "NPM_CONFIG_CACHE") {
		t.Errorf("golang preset must not pull in node caches: %s", cmd)
	}
	raw, _ := os.ReadFile(filepath.Join(dir, "settings.json"))
	var s settings
	if err := json.Unmarshal(raw, &s); err != nil {
		t.Fatal(err)
	}
	configDir, _ := os.UserConfigDir()
	wantTel := filepath.Join(configDir, "go", "telemetry")
	if len(s.Filesystem.AllowWrite) != 2 || s.Filesystem.AllowWrite[1] != wantTel {
		t.Errorf("allowWrite = %v, want [%s %s]", s.Filesystem.AllowWrite, ws, wantTel)
	}
}

func TestWrapTmuxPresetSetsUnixSockets(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "sess")
	if _, err := Wrap(context.Background(), Config{FenceCommand: "fence", WorkspacePath: t.TempDir(), Presets: []string{"tmux"}, DataDir: dir}, "echo hi"); err != nil {
		t.Fatalf("Wrap: %v", err)
	}
	raw, _ := os.ReadFile(filepath.Join(dir, "settings.json"))
	var s settings
	if err := json.Unmarshal(raw, &s); err != nil {
		t.Fatal(err)
	}
	if s.Network == nil || !s.Network.AllowAllUnixSockets {
		t.Errorf("tmux preset must set allowAllUnixSockets")
	}
}

func TestWrapDockerPresetEnvAndSocket(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "sess")
	ws := t.TempDir()
	if _, err := Wrap(context.Background(), Config{FenceCommand: "fence", WorkspacePath: ws, Presets: []string{"docker"}, DataDir: dir}, "echo hi"); err != nil {
		t.Fatalf("Wrap: %v", err)
	}
	cmd, _ := os.ReadFile(filepath.Join(dir, "cmd.sh"))
	wantDocker := "export DOCKER_CONFIG='" + filepath.Join(ws, ".cache", "schmux-fence", "docker") + "'"
	if !strings.Contains(string(cmd), wantDocker) {
		t.Errorf("cmd.sh = %q, want %s", cmd, wantDocker)
	}
	raw, _ := os.ReadFile(filepath.Join(dir, "settings.json"))
	var s settings
	if err := json.Unmarshal(raw, &s); err != nil {
		t.Fatal(err)
	}
	if s.Network == nil || !s.Network.AllowAllUnixSockets {
		t.Errorf("docker preset must set allowAllUnixSockets")
	}
	for _, want := range []string{"auth.docker.io", "registry-1.docker.io"} {
		if s.Network == nil || !slices.Contains(s.Network.AllowedDomains, want) {
			t.Errorf("docker preset allowedDomains = %v, want %s", s.Network, want)
		}
	}
	if _, err := os.Stat(filepath.Join(ws, ".cache", "schmux-fence", "docker")); err != nil {
		t.Errorf("expected DOCKER_CONFIG dir: %v", err)
	}
	if !IsKnownPreset("docker") {
		t.Errorf("IsKnownPreset(docker) = false, want true")
	}
}

func TestDiscoverDockerPluginDirs(t *testing.T) {
	eval := func(p string) string {
		r, err := filepath.EvalSymlinks(p)
		if err != nil {
			t.Fatal(err)
		}
		return r
	}

	home := t.TempDir()
	t.Setenv("HOME", home)

	// A plugin symlinked to a readable dir OUTSIDE ~/.docker → its dir is included.
	realPluginDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(realPluginDir, "docker-buildx"), []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	cliPlugins := filepath.Join(home, ".docker", "cli-plugins")
	if err := os.MkdirAll(cliPlugins, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(realPluginDir, "docker-buildx"), filepath.Join(cliPlugins, "docker-buildx")); err != nil {
		t.Fatal(err)
	}
	// A plugin whose target stays under ~/.docker → must be excluded.
	if err := os.WriteFile(filepath.Join(cliPlugins, "docker-compose"), []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}

	existingSys := t.TempDir()
	orig := dockerSystemPluginDirs
	dockerSystemPluginDirs = []string{existingSys, "/no/such/dir/cli-plugins"}
	defer func() { dockerSystemPluginDirs = orig }()

	got := discoverDockerPluginDirs()

	gotSet := make(map[string]bool, len(got))
	for _, d := range got {
		gotSet[d] = true
	}
	if !gotSet[eval(realPluginDir)] {
		t.Errorf("want symlink-target dir %s in %v", eval(realPluginDir), got)
	}
	if !gotSet[existingSys] {
		t.Errorf("want existing system dir %s in %v", existingSys, got)
	}
	if gotSet["/no/such/dir/cli-plugins"] {
		t.Errorf("nonexistent dir leaked into %v", got)
	}
	if gotSet[eval(cliPlugins)] || gotSet[cliPlugins] {
		t.Errorf("a dir under ~/.docker leaked into %v", got)
	}
}

func TestWrapDockerWritesPluginConfig(t *testing.T) {
	pluginDir := t.TempDir()
	orig := dockerPluginDirsFn
	dockerPluginDirsFn = func() []string { return []string{pluginDir} }
	defer func() { dockerPluginDirsFn = orig }()

	dir := filepath.Join(t.TempDir(), "sess")
	ws := t.TempDir()
	if _, err := Wrap(context.Background(), Config{FenceCommand: "fence", WorkspacePath: ws, Presets: []string{"docker"}, DataDir: dir}, "echo hi"); err != nil {
		t.Fatalf("Wrap: %v", err)
	}
	cfgPath := filepath.Join(ws, ".cache", "schmux-fence", "docker", "config.json")
	raw, err := os.ReadFile(cfgPath)
	if err != nil {
		t.Fatalf("read docker config.json: %v", err)
	}
	var dc struct {
		CliPluginsExtraDirs []string `json:"cliPluginsExtraDirs"`
	}
	if err := json.Unmarshal(raw, &dc); err != nil {
		t.Fatal(err)
	}
	if len(dc.CliPluginsExtraDirs) != 1 || dc.CliPluginsExtraDirs[0] != pluginDir {
		t.Errorf("cliPluginsExtraDirs = %v, want [%s]", dc.CliPluginsExtraDirs, pluginDir)
	}
}

func TestWrapDockerNoPluginsSkipsConfig(t *testing.T) {
	orig := dockerPluginDirsFn
	dockerPluginDirsFn = func() []string { return nil }
	defer func() { dockerPluginDirsFn = orig }()

	dir := filepath.Join(t.TempDir(), "sess")
	ws := t.TempDir()
	if _, err := Wrap(context.Background(), Config{FenceCommand: "fence", WorkspacePath: ws, Presets: []string{"docker"}, DataDir: dir}, "echo hi"); err != nil {
		t.Fatalf("Wrap: %v", err)
	}
	if _, err := os.Stat(filepath.Join(ws, ".cache", "schmux-fence", "docker", "config.json")); !os.IsNotExist(err) {
		t.Errorf("config.json should not exist when no plugin dirs found; stat err = %v", err)
	}
}
