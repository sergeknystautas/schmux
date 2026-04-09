# Parallel Schmux Instances — Design (v2)

## Changes from previous version

**Critical issues addressed:**

1. **Complete inventory of hardcoded paths.** The v1 design listed ~8 packages. A full codebase audit found 25+ files with `os.UserHomeDir() + ".schmux"` joins. This revision includes every location, organized by subsystem.

2. **Secrets isolation.** `internal/config/secrets.go` has a `secretsPath()` function that hardcodes `~/.schmux/secrets.json`. All secrets operations (load, save, session secret, auth tokens) now go through the schmux dir. Two instances will have independent secrets files.

3. **`stop` command.** `daemon.Stop()` hardcodes the PID file path. Now accepts the resolved config dir, same as `Start` and `Status`.

4. **CLI subcommand plumbing.** v1 said `--config-dir` is "stripped from os.Args so subcommands never see it" but never explained how the resolved dir reaches `cli.ResolveURL()` calls in `spawn`, `list`, `attach`, `dispose`, `tell`, `events`, `capture`, `inspect`, `branches`, `repofeed`, and `end-shift`. This revision resolves it via `cli.ResolveURL(configDir)`.

5. **Dev mode restart propagation.** `dev.sh` delegates to `tools/dev-runner/`, which launches `schmux daemon-run`. Verified that `cleanEnv()` already preserves `SCHMUX_HOME` (it only strips `npm_*` and a few specific vars). No code change needed — just verified the passthrough chain works.

**Suggestions incorporated:**

- **Package-level `SetSchmuxDir()` / `GetSchmuxDir()`.** Instead of threading `configDir` through every function signature across 25+ files, use a single `SetSchmuxDir()` call at startup. This matches the existing `SetLogger()` pattern already used in 10+ packages. Reduces diff size and eliminates the risk of missing a call site.
- **Startup log line.** Log the resolved config dir prominently when non-default.
- **PID file as safety net.** Documented that the existing PID check prevents two instances from sharing a config dir, even when `SCHMUX_HOME` is set to the default path explicitly.
- **`=` syntax for the flag.** `--config-dir=<path>` is handled alongside `--config-dir <path>`.
- **Missing `tmux_socket` warning.** Not implemented as a startup check (it would require cross-instance communication), but documented clearly in the User Responsibility section.

## Goal

Allow two (or more) schmux instances to run side-by-side on the same machine without interfering with each other. The only required change is making the `~/.schmux/` config directory configurable; port and tmux socket isolation are already supported via `config.json`.

## Resolution Logic

Resolve the schmux directory once at startup in `main.go`, before command dispatch:

```
Priority: --config-dir flag > SCHMUX_HOME env var > ~/.schmux default
```

- `--config-dir` is a **global flag** parsed before the subcommand switch. Both `--config-dir <path>` and `--config-dir=<path>` forms are accepted.
- It is stripped from `os.Args` so subcommands never see it.
- The resolved path is converted to an absolute path.
- The resolved path is stored via `schmuxdir.Set(path)` for all packages to read.

## The `schmuxdir` Package

A new internal package, `internal/schmuxdir/`, provides the resolved directory to every package without parameter threading:

```go
package schmuxdir

var dir string  // defaults to "" (unset)

// Set stores the resolved schmux directory. Called once at startup.
func Set(d string) { dir = d }

// Get returns the schmux directory, defaulting to ~/.schmux if unset.
func Get() string {
    if dir != "" {
        return dir
    }
    home, err := os.UserHomeDir()
    if err != nil {
        return ""
    }
    return filepath.Join(home, ".schmux")
}
```

This follows the existing `SetLogger()` pattern used in `internal/config/`, `internal/lore/`, `internal/detect/`, `internal/dashboardsx/`, `internal/compound/`, `internal/update/`, `internal/tunnel/`, and `internal/workspace/ensure/`. It is called once before any other initialization, so there is no concurrency concern.

**Why not parameter threading?** The review identified 25+ files with hardcoded paths across `cmd/`, `internal/config/`, `internal/daemon/`, `internal/dashboard/`, `internal/workspace/`, `internal/lore/`, `internal/logging/`, `internal/floormanager/`, `internal/assets/`, `internal/detect/`, `internal/dashboardsx/`, `internal/oneshot/`, and `pkg/cli/`. Threading a `configDir` parameter through every function would touch 50+ call sites and require signature changes across package boundaries. A single `schmuxdir.Set()` / `schmuxdir.Get()` achieves the same isolation with a much smaller and safer diff.

## Changes by Package

### `internal/schmuxdir/` (new)

- `Set(dir string)` — called once at startup.
- `Get() string` — returns the resolved dir; defaults to `~/.schmux`.

### `cmd/schmux/main.go`

- Parse `--config-dir <path>` and `--config-dir=<path>` from `os.Args` before the command switch.
- Check `SCHMUX_HOME` env var as fallback.
- Default to `~/.schmux`.
- Call `schmuxdir.Set(resolved)` once.
- When the resolved dir is not the default, log: `schmux: using config dir <path>`.
- Strip `--config-dir` (and its value) from `os.Args` so subcommands do not see it.

### `internal/config/`

All functions that currently call `os.UserHomeDir()` + `".schmux"` are changed to call `schmuxdir.Get()`:

- **`config.go`**: `ConfigExists()`, `EnsureExists()` — use `schmuxdir.Get()` instead of hardcoding `~/.schmux`.
- **`config.go`**: `GetWorktreeBasePath()` — default path changes from `~/.schmux/repos` to `schmuxdir.Get() + "/repos"`.
- **`config.go`**: `GetQueryRepoPath()` — default path changes from `~/.schmux/query` to `schmuxdir.Get() + "/query"`.
- **`secrets.go`**: `secretsPath()` — changes from `~/.schmux/secrets.json` to `schmuxdir.Get() + "/secrets.json"`. This fixes the critical cross-talk issue where two instances would share secrets, auth tokens, and session secrets.
- `Load()` already accepts a config path — no change needed.

### `internal/daemon/`

- `NewDaemon()` — no signature change needed (reads from `schmuxdir.Get()`).
- `Start()` — propagate to the child process by setting `SCHMUX_HOME` in `cmd.Env`, so the forked `schmux daemon-run` inherits the correct directory.
- **`Stop()`** — use `schmuxdir.Get()` instead of hardcoding `~/.schmux` for the PID file path. This was missing from v1.
- `Status()` — use `schmuxdir.Get()` for `daemon.url` and `daemon.pid`.
- `ValidateReadyToRun()` — use `schmuxdir.Get()`.
- Replace all remaining `filepath.Join(homeDir, ".schmux")` with `schmuxdir.Get()`.

### `pkg/cli/`

- `ResolveURL()` — change the fallback path from `~/.schmux/daemon.url` to `schmuxdir.Get() + "/daemon.url"`. The `SCHMUX_URL` env var check remains first priority (unchanged).
- No new `DaemonURLFromDir()` function is needed since `schmuxdir.Get()` provides the dir.

### `internal/dashboard/`

Six handler files independently call `os.UserHomeDir()` and join `".schmux"`. All are changed to use `schmuxdir.Get()`:

- **`handlers_dev.go`**: dev state path (`dev-state.json`), restart manifest (`dev-restart.json`), build status (`dev-build-status.json`).
- **`handlers_subreddit.go`**: `getSubredditDir()` — subreddit data dir.
- **`handlers_timelapse.go`**: `recordingsDir()` — timelapse recordings.
- **`handlers_usermodels.go`**: user models path (`user-models.json`).
- **`handlers_lore.go`**: lore curator run dirs (`lore-curator-runs/`).
- **`websocket.go`**: diagnostic output dirs — uses `os.Getenv("HOME")` not `os.UserHomeDir()`.
- **`server.go`**: log-message path comparison — uses `os.Getenv("HOME")` not `os.UserHomeDir()`.

### `internal/workspace/overlay.go`

- `OverlayDir(repoName)` — use `schmuxdir.Get()` instead of `os.UserHomeDir() + ".schmux"`.
- `EnsureOverlayDir(repoName)` — no direct change needed (delegates to `OverlayDir`).

### `internal/workspace/ensure/manager.go`

- `SignalingInstructionsFilePath()` — use `schmuxdir.Get()` instead of `os.UserHomeDir() + ".schmux"`.

### `internal/lore/scratchpad.go`

- `LoreStateDir(repoName)` — use `schmuxdir.Get()` instead of `os.UserHomeDir() + ".schmux"`.
- `LoreStatePath(repoName)` — no direct change needed (delegates to `LoreStateDir`).

### `internal/logging/logging.go`

- Startup log path — use `schmuxdir.Get()` instead of `os.UserHomeDir() + ".schmux"` for `daemon-startup.log`. Without this, two instances would interleave log output.

### `internal/floormanager/manager.go`

- `New()` — the `workDir` computation changes from `filepath.Join(homeDir, ".schmux", "floor-manager")` to `filepath.Join(schmuxdir.Get(), "floor-manager")`. The `homeDir` parameter to `New()` can be removed (it was only used for the `.schmux` join).

### `internal/assets/download.go`

- Dashboard asset cache path — use `schmuxdir.Get()` instead of `os.UserHomeDir() + ".schmux"` for the `dashboard/` subdirectory.

### `internal/detect/adapter_claude_hooks.go`

- `EnsureGlobalHookScripts(homeDir string)` — change to use `schmuxdir.Get()` directly instead of `filepath.Join(homeDir, ".schmux", "hooks")`. The `homeDir` parameter can be replaced since it was only used for this join.

### `internal/dashboardsx/paths.go`

- `Dir()`, `InstanceKeyPath()`, `CertPath()`, `KeyPath()`, `ACMEAccountPath()` — all use `schmuxdir.Get()` instead of `os.UserHomeDir() + ".schmux"`.

### `internal/oneshot/oneshot.go`

- Schemas dir — use `schmuxdir.Get()` instead of hardcoding `~/.schmux/schemas/`.

### `cmd/schmux/timelapse.go`

- Recordings dir — use `schmuxdir.Get()` instead of hardcoding `~/.schmux/recordings/`.

### `cmd/schmux/auth_github.go`

- TLS cert paths — use `schmuxdir.Get()` instead of hardcoding `~/.schmux/tls/`.

### `cmd/schmux/dashboardsx.go`

- Dashboard.sx paths — use `schmuxdir.Get()` instead of hardcoding `~/.schmux/dashboardsx/`.

## Dev Mode Restart Propagation

Two codepaths launch the daemon:

1. **`schmux start`** forks a background process. `Start()` sets `SCHMUX_HOME` in `cmd.Env` on the child, so the forked `schmux daemon-run` inherits the correct directory.

2. **`dev.sh` / `tools/dev-runner/`** runs `schmux daemon-run --dev-mode --dev-proxy` directly. When the daemon exits with code 42 (dev restart), the dev runner re-launches the binary. The dev runner must forward `SCHMUX_HOME` from its own environment into the spawned process. Since `dev.sh` passes through the shell environment via `exec`, and the TypeScript dev runner spawns the binary with `cleanEnv()`, the fix is: `cleanEnv()` must preserve `SCHMUX_HOME` in the environment it passes to the child process.

## Non-Changes

These are already handled and require no code changes:

- **tmux isolation**: `tmux_socket` config field already exists — each instance uses a different socket via `tmux -L`.
- **Port binding**: `GetPort()` already reads from `config.json` — each instance binds a different port.
- **Workspace directories**: Configured per-instance in `config.json`.
- **WebSocket endpoints**: Bound to the instance's port.
- **Browser cookies**: `schmux_auth`/`schmux_csrf` are scoped to host:port by the browser.
- **Dashboard frontend**: Stateless, talks to whatever backend served it.
- **`state.Load()`**: Already accepts a path parameter.
- **`config.Load()`**: Already accepts a config path parameter.
- **`workspace.New()` and `session.New()`**: Do not hardcode `~/.schmux` — they receive config/state objects from the daemon.
- **`models.New()`**: Already takes a `schmuxDir` field and uses it for `CachePath`, `SaveCache`, `LoadCache`.

## PID File Safety Net

If a user runs `schmux --config-dir ~/.schmux start` while the default instance is already running, both resolve to the same directory. The existing PID check in `ValidateReadyToRun` will detect the running daemon and refuse to start a second one. This is the intended safety net — no additional validation is needed.

## User Responsibility

When running a second instance, the user must configure in the second `config.json`:

- `port` — e.g. `7338` (to avoid binding conflict with the first instance on `7337`)
- `tmux_socket` — e.g. `"schmux-2"` (to isolate tmux sessions)
- Different repos/workspaces as desired

If `tmux_socket` is not changed, both instances will use the default `schmux` socket. Sessions from instance 2 will be visible in instance 1's tmux namespace, and session name collisions may cause one instance to interfere with the other's sessions. The user is responsible for setting a unique socket name. A future enhancement could add a startup warning, but cross-instance detection adds complexity that is not warranted for v1.

## Usage Examples

```bash
# Instance 1 (default)
schmux start

# Instance 2 (flag form)
schmux --config-dir ~/.schmux-work start

# Instance 2 (= form)
schmux --config-dir=~/.schmux-work start

# Or via env var
SCHMUX_HOME=~/.schmux-work schmux start

# Status of instance 2
schmux --config-dir ~/.schmux-work status

# Stop instance 2
schmux --config-dir ~/.schmux-work stop

# Attach to a session in instance 2
schmux --config-dir ~/.schmux-work attach my-session

# Spawn in instance 2 via env var
SCHMUX_HOME=~/.schmux-work schmux spawn -a claude -p "fix the bug"
```

## Daemon Start (Background) Propagation

When `schmux start` forks a background daemon process, the resolved config dir must be propagated to the child. This is done by setting `SCHMUX_HOME` in the child process's environment, so the forked `schmux daemon-run` inherits the correct directory without needing to re-parse CLI flags.

## Implementation Order

1. Create `internal/schmuxdir/` package (tiny, no dependencies).
2. Add flag parsing and `schmuxdir.Set()` call in `main.go`.
3. Update `internal/config/` (config.go, secrets.go) — highest cross-talk risk.
4. Update `internal/daemon/` (Start, Stop, Status, ValidateReadyToRun, NewDaemon).
5. Update `pkg/cli/` (ResolveURL).
6. Update all remaining packages in any order (dashboard handlers, workspace/overlay, lore, logging, floormanager, assets, detect, dashboardsx, oneshot, cmd/\* files).
7. Update `tools/dev-runner/` to preserve `SCHMUX_HOME` in `cleanEnv()`.
8. Add test: spin up a second daemon with a temp config dir, verify no files are written to `~/.schmux/`.

## Complete Inventory of `.schmux` References

For verification during implementation. Every file below contains a hardcoded `~/.schmux` path that must be changed to `schmuxdir.Get()`. **Important:** search for both `os.UserHomeDir()` and `os.Getenv("HOME")` when verifying completeness — two files use the latter pattern:

| Package                     | File                      | What it references                                                                |
| --------------------------- | ------------------------- | --------------------------------------------------------------------------------- |
| `internal/config`           | `config.go`               | `ConfigExists()`, `EnsureExists()`, `GetWorktreeBasePath()`, `GetQueryRepoPath()` |
| `internal/config`           | `secrets.go`              | `secretsPath()`                                                                   |
| `internal/daemon`           | `daemon.go`               | `Start()`, `Stop()`, `Status()`, `ValidateReadyToRun()`, Run-time paths           |
| `internal/dashboard`        | `handlers_dev.go`         | dev state, restart manifest, build status                                         |
| `internal/dashboard`        | `handlers_subreddit.go`   | subreddit data dir                                                                |
| `internal/dashboard`        | `handlers_timelapse.go`   | recordings dir                                                                    |
| `internal/dashboard`        | `handlers_usermodels.go`  | user-models.json                                                                  |
| `internal/dashboard`        | `handlers_lore.go`        | lore-curator-runs dir                                                             |
| `internal/dashboard`        | `websocket.go`            | diagnostics dirs (uses `os.Getenv("HOME")`)                                       |
| `internal/dashboard`        | `server.go`               | log-message path comparison (uses `os.Getenv("HOME")`)                            |
| `internal/workspace`        | `overlay.go`              | `OverlayDir()`                                                                    |
| `internal/workspace/ensure` | `manager.go`              | `SignalingInstructionsFilePath()`                                                 |
| `internal/lore`             | `scratchpad.go`           | `LoreStateDir()`, `LoreStatePath()`                                               |
| `internal/logging`          | `logging.go`              | daemon-startup.log                                                                |
| `internal/floormanager`     | `manager.go`              | floor-manager work dir                                                            |
| `internal/assets`           | `download.go`             | dashboard asset cache                                                             |
| `internal/detect`           | `adapter_claude_hooks.go` | hooks dir                                                                         |
| `internal/dashboardsx`      | `paths.go`                | `Dir()`, cert/key/ACME paths                                                      |
| `internal/oneshot`          | `oneshot.go`              | schemas dir                                                                       |
| `cmd/schmux`                | `timelapse.go`            | recordings dir                                                                    |
| `cmd/schmux`                | `auth_github.go`          | TLS certs                                                                         |
| `cmd/schmux`                | `dashboardsx.go`          | dashboardsx paths                                                                 |
| `pkg/cli`                   | `daemon_client.go`        | `ResolveURL()` daemon.url path                                                    |
| `tools/dev-runner`          | `src/App.tsx`             | `cleanEnv()` must preserve `SCHMUX_HOME`                                          |
