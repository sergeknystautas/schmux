// Package fence wraps a session launch command in the fence OS sandbox. It is
// agent-agnostic and VCS-agnostic: it writes a per-spawn settings file plus a
// launch script and returns the tmux-level command string, treating the launch
// command as opaque. The baseline sandbox policy comes from the fence "code"
// template; schmux adds only per-session workspace, endpoint, and opt-in
// per-language allowances.
package fence

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"github.com/sergeknystautas/schmux/pkg/shellutil"
)

// Config is the per-spawn input. Only the schmux-specific bits — everything
// else comes from the fence "code" template.
type Config struct {
	FenceCommand       string   // from the existing dependency report's "fence" status
	WorkspacePath      string   // cwd of the pane; writable
	ExtraWritablePaths []string // out-of-workspace paths the VCS must write (e.g. a git worktree's shared .git). Opaque to fence.
	ExtraReadablePaths []string // out-of-workspace paths the process may read (e.g. the workspace's fence-log dir). Opaque to fence.
	AllowedDomains     []string // model/provider + repo fence.allowed_domains
	Presets            []string // repo fence.presets (golang/tmux/docker/godot-editor)
	DataDir            string   // where generated launch files go (~/.schmux/fence/<workspace-id>/<session-id>/)
}

// settings is the generated fence settings file. Field order is fixed so the
// serialized output is deterministic.
type settings struct {
	Extends    string             `json:"extends"`
	Network    *settingsNetwork   `json:"network,omitempty"`
	MacOS      *settingsMacOS     `json:"macos,omitempty"`
	Filesystem settingsFilesystem `json:"filesystem"`
}

type settingsNetwork struct {
	AllowedDomains      []string `json:"allowedDomains,omitempty"`
	AllowAllUnixSockets bool     `json:"allowAllUnixSockets,omitempty"`
}

// settingsMacOS maps to fence's macOS-specific Seatbelt controls; fence ignores
// it on other platforms. Mach patterns are exact service names, trailing
// wildcards ("org.chromium.*"), or "*" (the macos-gui preset's grant).
type settingsMacOS struct {
	Mach settingsMach `json:"mach"`
}

type settingsMach struct {
	Lookup   []string `json:"lookup,omitempty"`
	Register []string `json:"register,omitempty"`
}

type settingsFilesystem struct {
	AllowRead  []string `json:"allowRead"`
	AllowWrite []string `json:"allowWrite"`
	DenyWrite  []string `json:"denyWrite,omitempty"` // overrides allowWrite; used to keep the swift shim dir tamper-proof while on PATH
}

// fenceCacheRel is the workspace-relative directory where fence redirects
// build-tool caches (npm, go-build, etc.) so they never touch the user's home
// dir while fenced. It is the single source of truth for both the cache layout
// (baselineEnv/presets) and the git-exclude pattern (WorkspaceExcludePatterns).
const fenceCacheRel = ".cache/schmux-fence"

// WorkspaceExcludePatterns returns the gitignore patterns for files fence writes
// inside a workspace. The workspace ensurer adds these to .git/info/exclude so a
// workspace fenced after creation does not leak fence's caches into git status.
func WorkspaceExcludePatterns() []string {
	return []string{fenceCacheRel + "/"}
}

// Wrap writes <DataDir>/settings.json and <DataDir>/cmd.sh, then returns the
// tmux-level command string. The generated shell script exports workspace-local
// cache paths before the caller's verbatim command so common build tools do not
// write into the user's home directory while fenced.
func Wrap(_ context.Context, c Config, command string) (string, error) {
	if err := os.MkdirAll(c.DataDir, 0o700); err != nil {
		return "", fmt.Errorf("fence: creating launch dir: %w", err)
	}

	cacheRoot := filepath.Join(c.WorkspacePath, filepath.FromSlash(fenceCacheRel))
	env := baselineEnv(cacheRoot)
	var goFlags, goTelemetry, allUnix, dockerConfig, godotEditor, swiftShim bool
	domains := append([]string{}, baselineDomains...)
	var machLookup, machRegister []string
	for _, name := range c.Presets {
		p, ok := presets[name]
		if !ok {
			continue
		}
		for k, sub := range p.cacheEnv {
			env[k] = filepath.Join(cacheRoot, sub)
		}
		goFlags = goFlags || p.goFlags
		goTelemetry = goTelemetry || p.goTelemetry
		allUnix = allUnix || p.allUnixSockets
		dockerConfig = dockerConfig || p.dockerConfig
		godotEditor = godotEditor || p.godotEditor
		swiftShim = swiftShim || p.swiftShim
		domains = append(domains, p.domains...)
		machLookup = append(machLookup, p.machLookup...)
		machRegister = append(machRegister, p.machRegister...)
	}
	for _, dir := range env {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return "", fmt.Errorf("fence: creating local cache dir: %w", err)
		}
	}
	if dockerConfig {
		if cfgDir := env["DOCKER_CONFIG"]; cfgDir != "" {
			if extra := dockerPluginDirsFn(); len(extra) > 0 {
				data, err := json.Marshal(map[string][]string{"cliPluginsExtraDirs": extra})
				if err != nil {
					return "", fmt.Errorf("fence: marshaling docker config: %w", err)
				}
				if err := os.WriteFile(filepath.Join(cfgDir, "config.json"), data, 0o600); err != nil {
					return "", fmt.Errorf("fence: writing docker config: %w", err)
				}
			}
		}
	}

	// The swift preset writes a `swift` shim and prepends its dir to PATH so
	// SwiftPM builds survive fence (see swiftShimScript). Skipped when swift is not
	// on the host, mirroring docker's no-plugins case.
	var swiftShimDir string
	if swiftShim {
		if realSwift := swiftLookPathFn(); realSwift != "" {
			swiftShimDir = filepath.Join(cacheRoot, "swift-shim")
			if err := os.MkdirAll(swiftShimDir, 0o700); err != nil {
				return "", fmt.Errorf("fence: creating swift shim dir: %w", err)
			}
			if err := os.WriteFile(filepath.Join(swiftShimDir, "swift"), []byte(swiftShimScript(realSwift)), 0o700); err != nil {
				return "", fmt.Errorf("fence: writing swift shim: %w", err)
			}
		}
	}

	cmdPath := filepath.Join(c.DataDir, "cmd.sh")
	settingsPath := filepath.Join(c.DataDir, "settings.json")
	monitorLogPath := filepath.Join(c.DataDir, "monitor.log")

	script := exportLines(env)
	if goFlags {
		script += `export GOFLAGS="${GOFLAGS:+$GOFLAGS }-modcacherw"` + "\n"
	}
	if swiftShimDir != "" {
		script += "export PATH=" + shellutil.Quote(swiftShimDir) + ":$PATH\n"
	}
	script += command
	if err := os.WriteFile(cmdPath, []byte(script), 0o600); err != nil {
		return "", fmt.Errorf("fence: writing cmd.sh: %w", err)
	}

	allowWrite := append([]string{c.WorkspacePath}, c.ExtraWritablePaths...)
	if goTelemetry {
		allowWrite = append(allowWrite, goTelemetryPaths()...)
	}
	if godotEditor {
		allowWrite = append(allowWrite, godotEditorPaths()...)
	}
	allowedDomains := make([]string, 0, len(c.AllowedDomains)+len(domains))
	allowedDomains = append(allowedDomains, c.AllowedDomains...)
	allowedDomains = append(allowedDomains, domains...)
	// The swift shim sits on PATH under the writable workspace; denyWrite it so a
	// fenced agent cannot overwrite it or drop shadow binaries into a PATH-first
	// dir. denyWrite overrides allowWrite in fence's policy.
	var denyWrite []string
	if swiftShimDir != "" {
		denyWrite = append(denyWrite, swiftShimDir)
	}
	s := settings{
		Extends: "code",
		Network: &settingsNetwork{
			AllowedDomains:      dedupeStrings(allowedDomains),
			AllowAllUnixSockets: allUnix,
		},
		Filesystem: settingsFilesystem{
			AllowRead:  append([]string{cmdPath}, c.ExtraReadablePaths...),
			AllowWrite: allowWrite,
			DenyWrite:  denyWrite,
		},
	}
	if len(machLookup)+len(machRegister) > 0 {
		s.MacOS = &settingsMacOS{Mach: settingsMach{
			Lookup:   dedupeStrings(machLookup),
			Register: dedupeStrings(machRegister),
		}}
	}
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return "", fmt.Errorf("fence: marshaling settings: %w", err)
	}
	if err := os.WriteFile(settingsPath, data, 0o600); err != nil {
		return "", fmt.Errorf("fence: writing settings.json: %w", err)
	}

	return fmt.Sprintf("%s -m --fence-log-file %s --settings %s /bin/sh %s",
		c.FenceCommand, shellutil.Quote(monitorLogPath), shellutil.Quote(settingsPath), shellutil.Quote(cmdPath)), nil
}

// dedupeStrings removes empties and duplicates, preserving order.
func dedupeStrings(in []string) []string {
	out := make([]string, 0, len(in))
	seen := make(map[string]bool, len(in))
	for _, s := range in {
		if s == "" || seen[s] {
			continue
		}
		seen[s] = true
		out = append(out, s)
	}
	return out
}

// preset is a named bundle of fence allowances a repo opts into via
// .schmux/config.json fence.presets. Each is a verbatim extraction of an
// allowance that used to be applied to every fenced session.
type preset struct {
	cacheEnv       map[string]string // env var -> cache subdir under the workspace cache root
	goFlags        bool              // append GOFLAGS=-modcacherw (keep module cache writable)
	goTelemetry    bool              // allowWrite the Go telemetry dir
	allUnixSockets bool              // network.allowAllUnixSockets
	dockerConfig   bool              // stage a DOCKER_CONFIG/config.json with cliPluginsExtraDirs
	godotEditor    bool              // allowWrite the Godot editor config dir (~/Library/Application Support/Godot)
	swiftShim      bool              // put a `swift` shim on PATH that adds --disable-sandbox (SwiftPM's nested sandbox can't run inside fence)
	domains        []string          // append to network.allowedDomains
	machLookup     []string          // append to macos.mach.lookup (macOS Seatbelt; ignored elsewhere)
	machRegister   []string          // append to macos.mach.register
}

var presets = map[string]preset{
	"golang": {
		cacheEnv:    map[string]string{"GOCACHE": "go-build", "STATICCHECK_CACHE": "staticcheck"},
		goFlags:     true,
		goTelemetry: true,
	},
	"tmux":         {allUnixSockets: true},
	"godot-editor": {godotEditor: true},
	// SwiftPM evaluates Package.swift (and runs build-tool/command plugins) inside
	// a nested macOS Seatbelt sandbox via sandbox-exec. That nested sandbox_apply
	// is denied inside fence's own sandbox ("Operation not permitted"), so
	// `swift build/test/run` aborts with "Invalid manifest". No fence setting can
	// grant nested sandboxing and SwiftPM has no env var for it, so the preset
	// shims `swift` on PATH to add --disable-sandbox. That turns off SwiftPM's
	// *own* nested sandbox, NOT fence — fence still confines the build and every
	// subprocess to workspace writes and allowed domains. The residual cost: a
	// package's manifest/plugin code then runs with the fenced session's
	// permissions instead of SwiftPM's stricter read-only/no-network subset.
	// Keeping both is impossible — fence forbids the nested sandbox_apply that
	// SwiftPM's inner sandbox needs.
	"swift": {swiftShim: true},
	"docker": {
		cacheEnv:       map[string]string{"DOCKER_CONFIG": "docker"},
		allUnixSockets: true,
		dockerConfig:   true,
		domains:        dockerHubPullDomains,
	},
	// Chromium's multiprocess model bootstrap-registers a per-process Mach
	// rendezvous port and children look it up (MachPortRendezvousServer); the
	// baseline seatbelt denies both, so even headless Chromium aborts with
	// "bootstrap_check_in ... Permission denied (1100)".
	"chromium": {
		machLookup:   []string{"org.chromium.*"},
		machRegister: []string{"org.chromium.*"},
	},
	// Native windowed apps (AppKit) need Mach IPC to the window server and a
	// wide, macOS-version-dependent set of system services; the baseline denies
	// them, so AppKit init fails before the app's code runs (e.g. windowed Godot
	// dies on com.apple.hiservices-xpcservice). "*" is the grant proven by a
	// live windowed-Godot run; it disables Mach IPC isolation entirely while
	// the filesystem/network fence stays intact. A curated service list is not
	// maintainable: allowed lookups are never logged, so misses can only be
	// found one relaunch at a time.
	"macos-gui": {
		machLookup:   []string{"*"},
		machRegister: []string{"*"},
	},
}

// baselineDomains are network hosts allowed for every fenced session,
// independent of harness, repo, model endpoint, or preset.
var baselineDomains = []string{
	// GitHub Actions results-upload endpoint; gh and Actions workflows inside
	// the fence reach it when running or checking CI.
	"results-receiver.actions.githubusercontent.com",
	// GitHub Actions run-log/artifact download: gh streams these from Azure
	// blob storage accounts named productionresultssaNN. fence only accepts
	// *.domain.com wildcards (partial-label globs like productionresultssa*.…
	// are rejected and abort the launch), so this is the tightest expressible
	// scope — it covers all of *.blob.core.windows.net, not just GitHub's.
	"*.blob.core.windows.net",
}

// dockerHubPullDomains are the Docker Hub auth and registry endpoints a fenced
// `docker build` reaches client-side: buildx resolves the base image's token and
// manifest inside the fence ("[internal] load metadata ..."), which fails without
// these. Layer blobs are pulled by the (unfenced) buildkitd in the daemon, so the
// Cloudflare layer CDNs are deliberately omitted. Source: Docker's official
// Desktop allowlist (docs.docker.com/desktop/setup/allow-list), pruned to the two
// anonymous-pull endpoints buildx needs client-side. Overridable in tests.
var dockerHubPullDomains = []string{
	"auth.docker.io",
	"registry-1.docker.io",
}

// IsKnownPreset reports whether name is a defined fence preset.
func IsKnownPreset(name string) bool {
	_, ok := presets[name]
	return ok
}

// swiftLookPathFn resolves the real swift binary on the host. Overridable in
// tests. Runs in the unfenced daemon, so the resolved path is valid inside the
// fence for local sessions.
var swiftLookPathFn = func() string {
	p, err := exec.LookPath("swift")
	if err != nil {
		return ""
	}
	return p
}

// swiftShimScript renders the `swift` PATH shim. SwiftPM evaluates Package.swift
// (and runs build-tool/command plugins) inside a nested macOS Seatbelt sandbox
// via sandbox-exec; that inner sandbox_apply is denied inside fence's own sandbox
// ("Operation not permitted"), aborting the build with "Invalid manifest". No
// fence setting can grant nested sandboxing and SwiftPM exposes no env var for
// it, so the shim adds --disable-sandbox to the subcommands that sandbox
// subprocesses (build/test/run) — never duplicating it, passing everything else
// through — then execs the real swift by absolute path (no PATH recursion).
func swiftShimScript(realSwift string) string {
	return "#!/bin/sh\n" +
		"# Generated by schmux fence (swift preset); do not edit.\n" +
		"real=" + shellutil.Quote(realSwift) + "\n" +
		"case \"$1\" in\n" +
		"build|test|run)\n" +
		"\tfor arg in \"$@\"; do\n" +
		"\t\t[ \"$arg\" = --disable-sandbox ] && exec \"$real\" \"$@\"\n" +
		"\tdone\n" +
		"\tsub=$1; shift\n" +
		"\texec \"$real\" \"$sub\" --disable-sandbox \"$@\" ;;\n" +
		"esac\n" +
		"exec \"$real\" \"$@\"\n"
}

// baselineEnv are cache redirects applied to every fenced session regardless of
// preset, keeping tool caches out of the user's home dir. These are pure env-var
// redirects into the writable workspace with no security tradeoff, so they are
// on for everyone rather than gated behind a preset — unlike golang/tmux/docker,
// which grant real capabilities (telemetry writes, sockets, the Docker daemon).
func baselineEnv(cacheRoot string) map[string]string {
	return map[string]string{
		"GIT_TEMPLATE_DIR": filepath.Join(cacheRoot, "git-template"),
		"XDG_CACHE_HOME":   filepath.Join(cacheRoot, "xdg"),
		// npm/yarn/bun (node), pip/uv (python), and Playwright browsers all
		// default to paths under the user's home dir that the fence blocks.
		"NPM_CONFIG_CACHE":         filepath.Join(cacheRoot, "npm"),
		"npm_config_cache":         filepath.Join(cacheRoot, "npm"),
		"YARN_CACHE_FOLDER":        filepath.Join(cacheRoot, "yarn"),
		"BUN_INSTALL_CACHE_DIR":    filepath.Join(cacheRoot, "bun"),
		"PIP_CACHE_DIR":            filepath.Join(cacheRoot, "pip"),
		"UV_CACHE_DIR":             filepath.Join(cacheRoot, "uv"),
		"PLAYWRIGHT_BROWSERS_PATH": filepath.Join(cacheRoot, "playwright"),
	}
}

func goTelemetryPaths() []string {
	configDir, err := os.UserConfigDir()
	if err != nil {
		return nil
	}
	return []string{filepath.Join(configDir, "go", "telemetry")}
}

// godotEditorPaths returns the Godot editor's per-user config directory, added to
// allowWrite by the godot-editor preset so a fenced agent can read and modify
// Godot editor settings, project lists, and export presets. On macOS
// os.UserConfigDir() is ~/Library/Application Support, so this resolves to
// ~/Library/Application Support/Godot. Reads there already pass the code template
// (non-credential); the preset adds the otherwise-restricted write.
func godotEditorPaths() []string {
	configDir, err := os.UserConfigDir()
	if err != nil {
		return nil
	}
	return []string{filepath.Join(configDir, "Godot")}
}

// dockerSystemPluginDirs are well-known locations of docker CLI plugins outside
// ~/.docker (which the fence "code" template denies reading). Overridable in tests.
var dockerSystemPluginDirs = []string{
	"/Applications/Docker.app/Contents/Resources/cli-plugins",
	"/usr/local/lib/docker/cli-plugins",
	"/usr/libexec/docker/cli-plugins",
	"/usr/lib/docker/cli-plugins",
	"/opt/homebrew/lib/docker/cli-plugins",
}

// dockerPluginDirsFn returns fence-readable directories containing docker CLI
// plugins. Overridable in tests. Runs in the unfenced daemon.
var dockerPluginDirsFn = discoverDockerPluginDirs

// discoverDockerPluginDirs returns fence-readable directories that hold docker
// CLI plugins (buildx/compose). The fence "code" template denies ~/.docker, so
// the plugin dirs named in DOCKER_CONFIG's cliPluginsExtraDirs must be readable
// from outside ~/.docker. It resolves each ~/.docker/cli-plugins entry's real
// target (Docker Desktop and brew symlink plugins to app/system dirs), drops any
// target still under ~/.docker (unreadable while fenced), and unions with known
// system locations. Runs in the unfenced daemon, so ~/.docker is readable here.
func discoverDockerPluginDirs() []string {
	var dirs []string
	if home, err := os.UserHomeDir(); err == nil {
		dockerDir := filepath.Join(home, ".docker")
		cliPlugins := filepath.Join(dockerDir, "cli-plugins")
		// Resolve dockerDir so the under-~/.docker check compares like-for-like
		// with EvalSymlinks'd targets ($HOME / TempDir is often a symlink, e.g.
		// macOS /var -> /private/var).
		dockerDirReal := dockerDir
		if r, err := filepath.EvalSymlinks(dockerDir); err == nil {
			dockerDirReal = r
		}
		if entries, err := os.ReadDir(cliPlugins); err == nil {
			for _, e := range entries {
				target, err := filepath.EvalSymlinks(filepath.Join(cliPlugins, e.Name()))
				if err != nil {
					continue
				}
				d := filepath.Dir(target)
				// Skip targets still under ~/.docker — unreadable while fenced.
				if !strings.HasPrefix(d+string(filepath.Separator), dockerDirReal+string(filepath.Separator)) {
					dirs = append(dirs, d)
				}
			}
		}
	}
	dirs = append(dirs, dockerSystemPluginDirs...)
	return existingUniqueDirs(dirs)
}

// existingUniqueDirs returns the input dirs that exist, deduped, order-preserving.
func existingUniqueDirs(in []string) []string {
	seen := make(map[string]bool, len(in))
	out := make([]string, 0, len(in))
	for _, d := range in {
		if d == "" || seen[d] {
			continue
		}
		if fi, err := os.Stat(d); err != nil || !fi.IsDir() {
			continue
		}
		seen[d] = true
		out = append(out, d)
	}
	return out
}

func exportLines(env map[string]string) string {
	keys := make([]string, 0, len(env))
	for k := range env {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	var b strings.Builder
	for _, k := range keys {
		b.WriteString("export ")
		b.WriteString(k)
		b.WriteString("=")
		b.WriteString(shellutil.Quote(env[k]))
		b.WriteString("\n")
	}
	return b.String()
}
