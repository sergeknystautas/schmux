package fence

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
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
		Presets:            []string{"golang", "tmux"},
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
	wantDomains := append([]string{"mcp.posthog.com", "api.z.ai"}, baselineDomains...)
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
	// npm/pip/playwright caches are baseline redirects, on for every session.
	for _, want := range []string{"GIT_TEMPLATE_DIR", "XDG_CACHE_HOME", "NPM_CONFIG_CACHE", "PIP_CACHE_DIR", "PLAYWRIGHT_BROWSERS_PATH"} {
		if !strings.Contains(string(cmd), "export "+want+"=") {
			t.Errorf("baseline export %s missing: %s", want, cmd)
		}
	}
	// Capability presets stay opt-in: no golang/docker exports without them.
	for _, banned := range []string{"GOCACHE", "STATICCHECK_CACHE", "GOFLAGS", "DOCKER_CONFIG"} {
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
	if s.MacOS != nil {
		t.Errorf("macos block should be absent without a mach-granting preset, got %+v", s.MacOS)
	}
	if len(s.Filesystem.DenyWrite) != 0 {
		t.Errorf("denyWrite should be empty without a preset that needs it, got %v", s.Filesystem.DenyWrite)
	}
}

func TestWrapChromiumPresetMachGrants(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "sess")
	if _, err := Wrap(context.Background(), Config{FenceCommand: "fence", WorkspacePath: t.TempDir(), Presets: []string{"chromium"}, DataDir: dir}, "echo hi"); err != nil {
		t.Fatalf("Wrap: %v", err)
	}
	raw, _ := os.ReadFile(filepath.Join(dir, "settings.json"))
	var s settings
	if err := json.Unmarshal(raw, &s); err != nil {
		t.Fatal(err)
	}
	if s.MacOS == nil {
		t.Fatal("chromium preset must emit a macos block")
	}
	wantMach := []string{"org.chromium.*"}
	if !slices.Equal(s.MacOS.Mach.Lookup, wantMach) {
		t.Errorf("macos.mach.lookup = %v, want %v", s.MacOS.Mach.Lookup, wantMach)
	}
	if !slices.Equal(s.MacOS.Mach.Register, wantMach) {
		t.Errorf("macos.mach.register = %v, want %v", s.MacOS.Mach.Register, wantMach)
	}
	// chromium grants only mach permissions — no sockets, no extra domains.
	if s.Network != nil && s.Network.AllowAllUnixSockets {
		t.Errorf("chromium preset must not set allowAllUnixSockets")
	}
	if !IsKnownPreset("chromium") {
		t.Errorf("IsKnownPreset(chromium) = false, want true")
	}
}

func TestWrapMacOSGuiPresetMachWildcard(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "sess")
	if _, err := Wrap(context.Background(), Config{FenceCommand: "fence", WorkspacePath: t.TempDir(), Presets: []string{"macos-gui"}, DataDir: dir}, "echo hi"); err != nil {
		t.Fatalf("Wrap: %v", err)
	}
	raw, _ := os.ReadFile(filepath.Join(dir, "settings.json"))
	var s settings
	if err := json.Unmarshal(raw, &s); err != nil {
		t.Fatal(err)
	}
	if s.MacOS == nil {
		t.Fatal("macos-gui preset must emit a macos block")
	}
	wantMach := []string{"*"}
	if !slices.Equal(s.MacOS.Mach.Lookup, wantMach) || !slices.Equal(s.MacOS.Mach.Register, wantMach) {
		t.Errorf("macos.mach = lookup %v register %v, want %v for both", s.MacOS.Mach.Lookup, s.MacOS.Mach.Register, wantMach)
	}
	if !IsKnownPreset("macos-gui") {
		t.Errorf("IsKnownPreset(macos-gui) = false, want true")
	}
}

// Windowed apps need the GPU, not just the window server: without this class
// MTLCreateSystemDefaultDevice() returns nil inside the fence even with mach "*".
func TestWrapMacOSGuiPresetGrantsGPUUserClient(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "sess")
	if _, err := Wrap(context.Background(), Config{FenceCommand: "fence", WorkspacePath: t.TempDir(), Presets: []string{"macos-gui"}, DataDir: dir}, "echo hi"); err != nil {
		t.Fatalf("Wrap: %v", err)
	}
	raw, _ := os.ReadFile(filepath.Join(dir, "settings.json"))
	var s settings
	if err := json.Unmarshal(raw, &s); err != nil {
		t.Fatal(err)
	}
	if s.MacOS == nil || s.MacOS.IOKit == nil {
		t.Fatal("macos-gui preset must emit a macos.iokit block")
	}
	want := []string{"AGXDeviceUserClient"}
	if !slices.Equal(s.MacOS.IOKit.UserClientClasses, want) {
		t.Errorf("macos.iokit.userClientClasses = %v, want exactly %v", s.MacOS.IOKit.UserClientClasses, want)
	}
	// The grant is per-class by construction. A wildcard would be rejected by
	// fence, but emitting one at all would mean schmux asked for every device.
	for _, c := range s.MacOS.IOKit.UserClientClasses {
		if strings.Contains(c, "*") {
			t.Errorf("macos.iokit.userClientClasses must never contain a wildcard, got %q", c)
		}
	}
}

// GPU access follows from the macos-gui identity claim alone. Every other
// preset must leave the IOKit block off entirely.
func TestOtherPresetsGrantNoIOKitAccess(t *testing.T) {
	for _, name := range []string{"golang", "tmux", "docker", "chromium", "swift", "vercel", "godot-editor"} {
		t.Run(name, func(t *testing.T) {
			dir := filepath.Join(t.TempDir(), "sess")
			if _, err := Wrap(context.Background(), Config{FenceCommand: "fence", WorkspacePath: t.TempDir(), Presets: []string{name}, DataDir: dir}, "echo hi"); err != nil {
				t.Fatalf("Wrap: %v", err)
			}
			raw, _ := os.ReadFile(filepath.Join(dir, "settings.json"))
			if strings.Contains(string(raw), "iokit") {
				t.Errorf("preset %q must not mention iokit, got:\n%s", name, raw)
			}
			var s settings
			if err := json.Unmarshal(raw, &s); err != nil {
				t.Fatal(err)
			}
			if s.MacOS != nil && s.MacOS.IOKit != nil {
				t.Errorf("preset %q emitted an iokit block: %+v", name, s.MacOS.IOKit)
			}
		})
	}
}

// A session with no presets at all must not carry a macos block for IOKit's sake.
func TestNoPresetsEmitsNoIOKitBlock(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "sess")
	if _, err := Wrap(context.Background(), Config{FenceCommand: "fence", WorkspacePath: t.TempDir(), DataDir: dir}, "echo hi"); err != nil {
		t.Fatalf("Wrap: %v", err)
	}
	raw, _ := os.ReadFile(filepath.Join(dir, "settings.json"))
	if strings.Contains(string(raw), "iokit") {
		t.Errorf("preset-free session must not mention iokit, got:\n%s", raw)
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

func TestWrapGodotEditorPresetAllowsWrite(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "sess")
	ws := t.TempDir()
	if _, err := Wrap(context.Background(), Config{FenceCommand: "fence", WorkspacePath: ws, Presets: []string{"godot-editor"}, DataDir: dir}, "echo hi"); err != nil {
		t.Fatalf("Wrap: %v", err)
	}
	raw, _ := os.ReadFile(filepath.Join(dir, "settings.json"))
	var s settings
	if err := json.Unmarshal(raw, &s); err != nil {
		t.Fatal(err)
	}
	configDir, err := os.UserConfigDir()
	if err != nil {
		t.Fatalf("UserConfigDir: %v", err)
	}
	wantGodot := filepath.Join(configDir, "Godot") // macOS: ~/Library/Application Support/Godot
	if !slices.Contains(s.Filesystem.AllowWrite, wantGodot) {
		t.Errorf("allowWrite = %v, want to contain %s", s.Filesystem.AllowWrite, wantGodot)
	}
	if !IsKnownPreset("godot-editor") {
		t.Errorf("IsKnownPreset(godot-editor) = false, want true")
	}
	// godot-editor grants only a filesystem path; it must not add capability-preset
	// cache exports (baseline npm/pip caches are exported for every session).
	cmd, _ := os.ReadFile(filepath.Join(dir, "cmd.sh"))
	for _, banned := range []string{"GOCACHE", "DOCKER_CONFIG"} {
		if strings.Contains(string(cmd), banned) {
			t.Errorf("godot-editor preset must not add %s: %s", banned, cmd)
		}
	}
}

func TestWrapSwiftPresetWritesShimAndPath(t *testing.T) {
	// Stub the host swift resolution so the test is deterministic and does not
	// require swift installed (CI is Linux, no swift).
	orig := swiftLookPathFn
	swiftLookPathFn = func() string { return "/opt/toolchain/usr/bin/swift" }
	defer func() { swiftLookPathFn = orig }()

	dir := filepath.Join(t.TempDir(), "sess")
	ws := t.TempDir()
	if _, err := Wrap(context.Background(), Config{FenceCommand: "fence", WorkspacePath: ws, Presets: []string{"swift"}, DataDir: dir}, "echo hi"); err != nil {
		t.Fatalf("Wrap: %v", err)
	}

	shimPath := filepath.Join(ws, ".cache", "schmux-fence", "swift-shim", "swift")
	fi, err := os.Stat(shimPath)
	if err != nil {
		t.Fatalf("swift shim not written: %v", err)
	}
	if fi.Mode().Perm()&0o100 == 0 {
		t.Errorf("swift shim mode = %o, want executable", fi.Mode().Perm())
	}
	shim, _ := os.ReadFile(shimPath)
	for _, want := range []string{"/opt/toolchain/usr/bin/swift", "--disable-sandbox", "build|test|run"} {
		if !strings.Contains(string(shim), want) {
			t.Errorf("shim missing %q\nshim=%s", want, shim)
		}
	}

	// cmd.sh prepends the shim dir to PATH so the shim wins over the real swift.
	cmd, _ := os.ReadFile(filepath.Join(dir, "cmd.sh"))
	wantPath := "export PATH='" + filepath.Join(ws, ".cache", "schmux-fence", "swift-shim") + "':$PATH"
	if !strings.Contains(string(cmd), wantPath) {
		t.Errorf("cmd.sh missing PATH prepend %q\ncmd=%s", wantPath, cmd)
	}

	// The swift preset grants nothing at the fence-settings level.
	raw, _ := os.ReadFile(filepath.Join(dir, "settings.json"))
	var s settings
	if err := json.Unmarshal(raw, &s); err != nil {
		t.Fatal(err)
	}
	if s.MacOS != nil {
		t.Errorf("swift preset must not emit a macos block, got %+v", s.MacOS)
	}
	if s.Network != nil && s.Network.AllowAllUnixSockets {
		t.Errorf("swift preset must not set allowAllUnixSockets")
	}
	// The shim dir sits on PATH and under the writable workspace, so it must be
	// denyWrite'd — otherwise the fenced agent could overwrite the shim or drop
	// shadow binaries into a PATH-first directory.
	shimDir := filepath.Join(ws, ".cache", "schmux-fence", "swift-shim")
	if !slices.Contains(s.Filesystem.DenyWrite, shimDir) {
		t.Errorf("swift preset must denyWrite the shim dir %q, got %v", shimDir, s.Filesystem.DenyWrite)
	}
	if !IsKnownPreset("swift") {
		t.Errorf("IsKnownPreset(swift) = false, want true")
	}
}

// TestSwiftShimInjectsDisableSandbox executes the generated shim against a stub
// "swift" that echoes its args, proving the shim adds --disable-sandbox for
// sandbox-using subcommands, never duplicates it, and passes other invocations
// through untouched.
func TestSwiftShimInjectsDisableSandbox(t *testing.T) {
	stubDir := t.TempDir()
	stub := filepath.Join(stubDir, "swift")
	if err := os.WriteFile(stub, []byte("#!/bin/sh\nfor a in \"$@\"; do echo \"$a\"; done\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	orig := swiftLookPathFn
	swiftLookPathFn = func() string { return stub }
	defer func() { swiftLookPathFn = orig }()

	dir := filepath.Join(t.TempDir(), "sess")
	ws := t.TempDir()
	if _, err := Wrap(context.Background(), Config{FenceCommand: "fence", WorkspacePath: ws, Presets: []string{"swift"}, DataDir: dir}, "echo hi"); err != nil {
		t.Fatalf("Wrap: %v", err)
	}
	shim := filepath.Join(ws, ".cache", "schmux-fence", "swift-shim", "swift")

	run := func(args ...string) []string {
		t.Helper()
		out, err := exec.Command("/bin/sh", append([]string{shim}, args...)...).Output()
		if err != nil {
			t.Fatalf("run shim %v: %v", args, err)
		}
		return strings.Split(strings.TrimSpace(string(out)), "\n")
	}
	count := func(lines []string, want string) int {
		n := 0
		for _, l := range lines {
			if l == want {
				n++
			}
		}
		return n
	}

	// build injects the flag exactly once.
	if got := run("build", "-c", "release"); count(got, "--disable-sandbox") != 1 {
		t.Errorf("swift build args = %v, want one --disable-sandbox", got)
	}
	// test is also sandbox-using.
	if got := run("test"); count(got, "--disable-sandbox") != 1 {
		t.Errorf("swift test args = %v, want one --disable-sandbox", got)
	}
	// Already present: no duplication.
	if got := run("build", "--disable-sandbox"); count(got, "--disable-sandbox") != 1 {
		t.Errorf("swift build --disable-sandbox args = %v, want exactly one (no dup)", got)
	}
	// Non-sandbox invocation passes through untouched.
	if got := run("--version"); count(got, "--disable-sandbox") != 0 {
		t.Errorf("swift --version args = %v, must not inject --disable-sandbox", got)
	}
}

func TestWrapSwiftPresetNoSwiftSkipsShim(t *testing.T) {
	orig := swiftLookPathFn
	swiftLookPathFn = func() string { return "" } // swift not installed on host
	defer func() { swiftLookPathFn = orig }()

	dir := filepath.Join(t.TempDir(), "sess")
	ws := t.TempDir()
	if _, err := Wrap(context.Background(), Config{FenceCommand: "fence", WorkspacePath: ws, Presets: []string{"swift"}, DataDir: dir}, "echo hi"); err != nil {
		t.Fatalf("Wrap: %v", err)
	}
	if _, err := os.Stat(filepath.Join(ws, ".cache", "schmux-fence", "swift-shim", "swift")); !os.IsNotExist(err) {
		t.Errorf("shim should not exist when swift is not on the host; stat err = %v", err)
	}
	cmd, _ := os.ReadFile(filepath.Join(dir, "cmd.sh"))
	if strings.Contains(string(cmd), "swift-shim") {
		t.Errorf("cmd.sh must not prepend a shim dir when swift is absent: %s", cmd)
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

func TestWrapAddsExtraReadablePaths(t *testing.T) {
	dir := t.TempDir()
	out, err := Wrap(context.Background(), Config{
		FenceCommand:       "fence",
		WorkspacePath:      t.TempDir(),
		DataDir:            dir,
		ExtraReadablePaths: []string{"/home/u/.schmux/fence/ws-1"},
	}, "echo hi")
	if err != nil {
		t.Fatalf("Wrap: %v", err)
	}
	if out == "" {
		t.Fatal("empty wrap command")
	}
	data, err := os.ReadFile(filepath.Join(dir, "settings.json"))
	if err != nil {
		t.Fatalf("read settings: %v", err)
	}
	if !strings.Contains(string(data), "/home/u/.schmux/fence/ws-1") {
		t.Errorf("settings.json missing extra readable path: %s", data)
	}
}

func TestWrapVercelPresetAddsDomains(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "sess")
	if _, err := Wrap(context.Background(), Config{FenceCommand: "fence", WorkspacePath: t.TempDir(), Presets: []string{"vercel"}, DataDir: dir}, "echo hi"); err != nil {
		t.Fatalf("Wrap: %v", err)
	}
	raw, _ := os.ReadFile(filepath.Join(dir, "settings.json"))
	var s settings
	if err := json.Unmarshal(raw, &s); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"vercel.com", "api.vercel.com"} {
		if s.Network == nil || !slices.Contains(s.Network.AllowedDomains, want) {
			t.Errorf("vercel preset allowedDomains = %v, want %s", s.Network, want)
		}
	}
	if !IsKnownPreset("vercel") {
		t.Errorf("IsKnownPreset(vercel) = false, want true")
	}
}

func TestWrapVercelPresetWritesShimAndPath(t *testing.T) {
	orig := vercelLookPathFn
	vercelLookPathFn = func() string { return "/usr/local/bin/vercel" }
	defer func() { vercelLookPathFn = orig }()

	dir := filepath.Join(t.TempDir(), "sess")
	ws := t.TempDir()
	if _, err := Wrap(context.Background(), Config{FenceCommand: "fence", WorkspacePath: ws, Presets: []string{"vercel"}, DataDir: dir}, "echo hi"); err != nil {
		t.Fatalf("Wrap: %v", err)
	}

	shimDir := filepath.Join(dir, "vercel-shim")
	shimPath := filepath.Join(shimDir, "vercel")
	fi, err := os.Stat(shimPath)
	if err != nil {
		t.Fatalf("vercel shim not written: %v", err)
	}
	if fi.Mode().Perm()&0o100 == 0 {
		t.Errorf("vercel shim mode = %o, want executable", fi.Mode().Perm())
	}
	shim, _ := os.ReadFile(shimPath)
	for _, want := range []string{"/usr/local/bin/vercel", "NODE_USE_ENV_PROXY=1", "NO_UPDATE_NOTIFIER=1", "--require", "proxy-preload.cjs"} {
		if !strings.Contains(string(shim), want) {
			t.Errorf("shim missing %q\nshim=%s", want, shim)
		}
	}

	preload, err := os.ReadFile(filepath.Join(shimDir, "proxy-preload.cjs"))
	if err != nil {
		t.Fatalf("proxy preload not written: %v", err)
	}
	for _, want := range []string{"globalThis.fetch", "dispatcher"} {
		if !strings.Contains(string(preload), want) {
			t.Errorf("preload missing %q\npreload=%s", want, preload)
		}
	}

	// cmd.sh prepends the shim dir to PATH so the shim wins over the real vercel.
	cmd, _ := os.ReadFile(filepath.Join(dir, "cmd.sh"))
	wantPath := "export PATH='" + shimDir + "':$PATH"
	if !strings.Contains(string(cmd), wantPath) {
		t.Errorf("cmd.sh missing PATH prepend %q\ncmd=%s", wantPath, cmd)
	}

	raw, _ := os.ReadFile(filepath.Join(dir, "settings.json"))
	var s settings
	if err := json.Unmarshal(raw, &s); err != nil {
		t.Fatal(err)
	}
	// The shim lives in the per-session launch dir, outside the writable
	// workspace: it needs an allowRead grant and no denyWrite (the fenced
	// session has no write grant to the launch dir at all).
	if !slices.Contains(s.Filesystem.AllowRead, shimDir) {
		t.Errorf("allowRead = %v, want to contain shim dir %s", s.Filesystem.AllowRead, shimDir)
	}
	if slices.Contains(s.Filesystem.DenyWrite, shimDir) {
		t.Errorf("denyWrite = %v, must not contain the DataDir shim dir %s", s.Filesystem.DenyWrite, shimDir)
	}
}

// TestVercelShimSetsProxyEnv executes the generated shim against a stub
// "vercel" that echoes the proxy env and its args, proving the shim exports
// NODE_USE_ENV_PROXY/NO_UPDATE_NOTIFIER, appends --require to NODE_OPTIONS
// (preserving any existing value), and passes all arguments through.
func TestVercelShimSetsProxyEnv(t *testing.T) {
	stubDir := t.TempDir()
	stub := filepath.Join(stubDir, "vercel")
	stubScript := "#!/bin/sh\n" +
		"echo \"proxy=$NODE_USE_ENV_PROXY\"\n" +
		"echo \"notifier=$NO_UPDATE_NOTIFIER\"\n" +
		"echo \"nodeopts=$NODE_OPTIONS\"\n" +
		"for a in \"$@\"; do echo \"arg=$a\"; done\n"
	if err := os.WriteFile(stub, []byte(stubScript), 0o755); err != nil {
		t.Fatal(err)
	}
	orig := vercelLookPathFn
	vercelLookPathFn = func() string { return stub }
	defer func() { vercelLookPathFn = orig }()

	dir := filepath.Join(t.TempDir(), "sess")
	if _, err := Wrap(context.Background(), Config{FenceCommand: "fence", WorkspacePath: t.TempDir(), Presets: []string{"vercel"}, DataDir: dir}, "echo hi"); err != nil {
		t.Fatalf("Wrap: %v", err)
	}
	shim := filepath.Join(dir, "vercel-shim", "vercel")
	preload := filepath.Join(dir, "vercel-shim", "proxy-preload.cjs")

	cmd := exec.Command("/bin/sh", shim, "whoami", "--token", "x")
	cmd.Env = append(os.Environ(), "NODE_OPTIONS=--max-old-space-size=4096")
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("run shim: %v", err)
	}
	got := string(out)
	for _, want := range []string{
		"proxy=1",
		"notifier=1",
		"nodeopts=--max-old-space-size=4096 --require " + preload,
		"arg=whoami", "arg=--token", "arg=x",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("shim output missing %q\noutput=%s", want, got)
		}
	}
}

func TestWrapVercelPresetNoVercelSkipsShim(t *testing.T) {
	orig := vercelLookPathFn
	vercelLookPathFn = func() string { return "" } // vercel not installed on host
	defer func() { vercelLookPathFn = orig }()

	dir := filepath.Join(t.TempDir(), "sess")
	if _, err := Wrap(context.Background(), Config{FenceCommand: "fence", WorkspacePath: t.TempDir(), Presets: []string{"vercel"}, DataDir: dir}, "echo hi"); err != nil {
		t.Fatalf("Wrap: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "vercel-shim", "vercel")); !os.IsNotExist(err) {
		t.Errorf("shim should not exist when vercel is not on the host; stat err = %v", err)
	}
	cmd, _ := os.ReadFile(filepath.Join(dir, "cmd.sh"))
	if strings.Contains(string(cmd), "vercel-shim") {
		t.Errorf("cmd.sh must not prepend a shim dir when vercel is absent: %s", cmd)
	}
	// Domains are an identity grant: present even when the CLI is not installed.
	raw, _ := os.ReadFile(filepath.Join(dir, "settings.json"))
	var s settings
	if err := json.Unmarshal(raw, &s); err != nil {
		t.Fatal(err)
	}
	if s.Network == nil || !slices.Contains(s.Network.AllowedDomains, "api.vercel.com") {
		t.Errorf("allowedDomains = %v, want api.vercel.com even with no CLI on host", s.Network)
	}
}

func TestWrapVercelPresetWhitespaceDataDirFails(t *testing.T) {
	orig := vercelLookPathFn
	vercelLookPathFn = func() string { return "/usr/local/bin/vercel" }
	defer func() { vercelLookPathFn = orig }()

	// NODE_OPTIONS is space-split by Node with no quoting mechanism: a shim
	// path containing whitespace would silently never load the preload, so
	// Wrap fails the launch instead.
	dir := filepath.Join(t.TempDir(), "sess with space")
	_, err := Wrap(context.Background(), Config{FenceCommand: "fence", WorkspacePath: t.TempDir(), Presets: []string{"vercel"}, DataDir: dir}, "echo hi")
	if err == nil || !strings.Contains(err.Error(), "whitespace") {
		t.Errorf("Wrap err = %v, want a whitespace guard error", err)
	}
}
