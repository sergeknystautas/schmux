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
	AllowedDomains     []string // model/provider + repo fence.allowed_domains
	Presets            []string // repo fence.presets (golang/node/python/tmux)
	DataDir            string   // where generated launch files go (~/.schmux/fence/<session-id>/)
}

// settings is the generated fence settings file. Field order is fixed so the
// serialized output is deterministic.
type settings struct {
	Extends    string             `json:"extends"`
	Network    *settingsNetwork   `json:"network,omitempty"`
	Filesystem settingsFilesystem `json:"filesystem"`
}

type settingsNetwork struct {
	AllowedDomains      []string `json:"allowedDomains,omitempty"`
	AllowAllUnixSockets bool     `json:"allowAllUnixSockets,omitempty"`
}

type settingsFilesystem struct {
	AllowRead  []string `json:"allowRead"`
	AllowWrite []string `json:"allowWrite"`
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
	var goFlags, goTelemetry, allUnix, dockerConfig bool
	domains := append([]string{}, baselineDomains...)
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
		domains = append(domains, p.domains...)
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

	cmdPath := filepath.Join(c.DataDir, "cmd.sh")
	settingsPath := filepath.Join(c.DataDir, "settings.json")
	monitorLogPath := filepath.Join(c.DataDir, "monitor.log")

	script := exportLines(env)
	if goFlags {
		script += `export GOFLAGS="${GOFLAGS:+$GOFLAGS }-modcacherw"` + "\n"
	}
	script += command
	if err := os.WriteFile(cmdPath, []byte(script), 0o600); err != nil {
		return "", fmt.Errorf("fence: writing cmd.sh: %w", err)
	}

	allowWrite := append([]string{c.WorkspacePath}, c.ExtraWritablePaths...)
	if goTelemetry {
		allowWrite = append(allowWrite, goTelemetryPaths()...)
	}
	allowedDomains := make([]string, 0, len(c.AllowedDomains)+len(domains))
	allowedDomains = append(allowedDomains, c.AllowedDomains...)
	allowedDomains = append(allowedDomains, domains...)
	s := settings{
		Extends: "code",
		Network: &settingsNetwork{
			AllowedDomains:      dedupeDomains(allowedDomains),
			AllowAllUnixSockets: allUnix,
		},
		Filesystem: settingsFilesystem{
			AllowRead:  []string{cmdPath},
			AllowWrite: allowWrite,
		},
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

// dedupeDomains removes empties and duplicates, preserving order.
func dedupeDomains(domains []string) []string {
	out := make([]string, 0, len(domains))
	seen := make(map[string]bool, len(domains))
	for _, d := range domains {
		if d == "" || seen[d] {
			continue
		}
		seen[d] = true
		out = append(out, d)
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
	domains        []string          // append to network.allowedDomains
}

var presets = map[string]preset{
	"golang": {
		cacheEnv:    map[string]string{"GOCACHE": "go-build", "STATICCHECK_CACHE": "staticcheck"},
		goFlags:     true,
		goTelemetry: true,
	},
	"node": {
		cacheEnv: map[string]string{
			"NPM_CONFIG_CACHE":      "npm",
			"npm_config_cache":      "npm",
			"YARN_CACHE_FOLDER":     "yarn",
			"BUN_INSTALL_CACHE_DIR": "bun",
		},
	},
	"python": {
		cacheEnv: map[string]string{"PIP_CACHE_DIR": "pip", "UV_CACHE_DIR": "uv"},
	},
	"tmux": {allUnixSockets: true},
	"docker": {
		cacheEnv:       map[string]string{"DOCKER_CONFIG": "docker"},
		allUnixSockets: true,
		dockerConfig:   true,
		domains:        dockerHubPullDomains,
	},
}

// baselineDomains are network hosts allowed for every fenced session,
// independent of harness, repo, model endpoint, or preset.
var baselineDomains = []string{
	// GitHub Actions results-upload endpoint; gh and Actions workflows inside
	// the fence reach it when running or checking CI.
	"results-receiver.actions.githubusercontent.com",
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

// baselineEnv are cache redirects applied to every fenced session regardless of
// preset, keeping generic tool caches out of the user's home dir.
func baselineEnv(cacheRoot string) map[string]string {
	return map[string]string{
		"GIT_TEMPLATE_DIR": filepath.Join(cacheRoot, "git-template"),
		"XDG_CACHE_HOME":   filepath.Join(cacheRoot, "xdg"),
	}
}

func goTelemetryPaths() []string {
	configDir, err := os.UserConfigDir()
	if err != nil {
		return nil
	}
	return []string{filepath.Join(configDir, "go", "telemetry")}
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
