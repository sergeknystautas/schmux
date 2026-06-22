// Package fence wraps a session launch command in the fence OS sandbox. It is
// agent-agnostic and VCS-agnostic: it writes a per-spawn settings file plus a
// launch script and returns the tmux-level command string, treating the launch
// command as opaque. The baseline sandbox policy comes from the fence "code"
// template; schmux adds only per-session workspace, endpoint, and local-tooling
// allowances.
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
	AllowedDomains     []string // model/provider domains known at spawn time; appended to the code template allowlist
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

var knownAllowedDomains = []string{
	"mcp.posthog.com",
}

// fenceCacheRel is the workspace-relative directory where fence redirects
// build-tool caches (npm, go-build, etc.) so they never touch the user's home
// dir while fenced. It is the single source of truth for both the cache layout
// (workspaceLocalEnv) and the git-exclude pattern (WorkspaceExcludePatterns).
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
	localEnv, err := workspaceLocalEnv(c.WorkspacePath)
	if err != nil {
		return "", err
	}

	cmdPath := filepath.Join(c.DataDir, "cmd.sh")
	settingsPath := filepath.Join(c.DataDir, "settings.json")
	monitorLogPath := filepath.Join(c.DataDir, "monitor.log")

	script := exportLines(localEnv) + `export GOFLAGS="${GOFLAGS:+$GOFLAGS }-modcacherw"` + "\n" + command
	if err := os.WriteFile(cmdPath, []byte(script), 0o600); err != nil {
		return "", fmt.Errorf("fence: writing cmd.sh: %w", err)
	}

	allowWrite := append([]string{c.WorkspacePath}, c.ExtraWritablePaths...)
	allowWrite = append(allowWrite, knownWritablePaths()...)
	s := settings{
		Extends: "code",
		Network: &settingsNetwork{
			AllowedDomains:      mergedAllowedDomains(c.AllowedDomains),
			AllowAllUnixSockets: true,
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

func mergedAllowedDomains(spawnDomains []string) []string {
	out := make([]string, 0, len(knownAllowedDomains)+len(spawnDomains))
	seen := make(map[string]bool, len(knownAllowedDomains)+len(spawnDomains))
	add := func(domains []string) {
		for _, domain := range domains {
			if domain == "" || seen[domain] {
				continue
			}
			seen[domain] = true
			out = append(out, domain)
		}
	}
	add(knownAllowedDomains)
	add(spawnDomains)
	return out
}

func workspaceLocalEnv(workspacePath string) (map[string]string, error) {
	cacheRoot := filepath.Join(workspacePath, filepath.FromSlash(fenceCacheRel))
	dirs := map[string]string{
		"BUN_INSTALL_CACHE_DIR": filepath.Join(cacheRoot, "bun"),
		"GIT_TEMPLATE_DIR":      filepath.Join(cacheRoot, "git-template"),
		"GOCACHE":               filepath.Join(cacheRoot, "go-build"),
		"NPM_CONFIG_CACHE":      filepath.Join(cacheRoot, "npm"),
		"PIP_CACHE_DIR":         filepath.Join(cacheRoot, "pip"),
		"STATICCHECK_CACHE":     filepath.Join(cacheRoot, "staticcheck"),
		"UV_CACHE_DIR":          filepath.Join(cacheRoot, "uv"),
		"XDG_CACHE_HOME":        filepath.Join(cacheRoot, "xdg"),
		"YARN_CACHE_FOLDER":     filepath.Join(cacheRoot, "yarn"),
		"npm_config_cache":      filepath.Join(cacheRoot, "npm"),
	}
	for _, dir := range dirs {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return nil, fmt.Errorf("fence: creating local cache dir: %w", err)
		}
	}
	return dirs, nil
}

func knownWritablePaths() []string {
	configDir, err := os.UserConfigDir()
	if err != nil {
		return nil
	}
	return []string{filepath.Join(configDir, "go", "telemetry")}
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
