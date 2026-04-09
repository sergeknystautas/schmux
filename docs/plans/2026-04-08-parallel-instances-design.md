# Parallel Schmux Instances — Design

## Goal

Allow two (or more) schmux instances to run side-by-side on the same machine without interfering with each other. The only required change is making the `~/.schmux/` config directory configurable; port and tmux socket isolation are already supported via `config.json`.

## Resolution Logic

Resolve the schmux directory once at startup in `main.go`, before command dispatch:

```
Priority: --config-dir flag > SCHMUX_HOME env var > ~/.schmux default
```

- `--config-dir` is a **global flag** parsed before the subcommand switch.
- It is stripped from `os.Args` so subcommands never see it.
- The resolved path is converted to an absolute path.
- The resolved path is passed into daemon entry points and threaded through constructors.

## Changes by Package

### `cmd/schmux/main.go`

- Parse `--config-dir <path>` from `os.Args` before the command switch.
- Check `SCHMUX_HOME` env var as fallback.
- Default to `~/.schmux`.
- Pass resolved dir into:
  - `config.EnsureExists(configDir)`
  - `daemon.ValidateReadyToRun(configDir)`
  - `daemon.Start(configDir)`
  - `daemon.Status(configDir)`
  - `daemon.NewDaemon(configDir)`
  - All other command handlers that reference `~/.schmux`

### `internal/daemon/`

- `NewDaemon(configDir string)` — store on the `Daemon` struct.
- `Start(configDir string)` — pass to the background process (via env var `SCHMUX_HOME` on the child process, so the forked daemon inherits the dir).
- `Status(configDir string)` — read `daemon.url` and `daemon.pid` from the given dir.
- `ValidateReadyToRun(configDir string)` — check config in the given dir.
- Replace all `filepath.Join(homeDir, ".schmux")` with the stored `configDir`.
- Thread `configDir` into subsystem constructors (session manager, workspace manager, preview manager, models manager, dashboardsx, etc.).

### `internal/config/`

- `EnsureExists(configDir string)` — use the given dir instead of hardcoding `~/.schmux`.
- `Load()` already accepts a config path — no change needed.

### `pkg/cli/`

- Add `DaemonURLFromDir(configDir string) string` — checks `SCHMUX_URL` env var, then `configDir/daemon.url`, then falls back to `http://localhost:7337`.
- Update `DaemonURL()` to resolve the dir (checking `SCHMUX_HOME` env var, defaulting to `~/.schmux`) and delegate to `DaemonURLFromDir()`.

### Subsystem Constructors

Most subsystem constructors already accept paths (evidenced by test code using temp dirs). The change is wiring the threaded `configDir` value instead of the hardcoded default in production initialization code:

- `internal/models/` — `schmuxDir` field already exists
- `internal/dashboardsx/` — `Dir()` function resolves `~/.schmux/dashboardsx/`
- `internal/oneshot/` — schemas dir at `~/.schmux/schemas/`
- `cmd/schmux/timelapse.go` — recordings dir at `~/.schmux/recordings/`
- `cmd/schmux/auth_github.go` — TLS certs at `~/.schmux/tls/`

## Non-Changes

These are already handled and require no code changes:

- **tmux isolation**: `tmux_socket` config field already exists — each instance uses a different socket via `tmux -L`.
- **Port binding**: `GetPort()` already reads from `config.json` — each instance binds a different port.
- **Workspace directories**: Configured per-instance in `config.json`.
- **WebSocket endpoints**: Bound to the instance's port.
- **Browser cookies**: `schmux_auth`/`schmux_csrf` are scoped to host:port by the browser.
- **Dashboard frontend**: Stateless, talks to whatever backend served it.

## User Responsibility

When running a second instance, the user must configure in the second `config.json`:

- `port` — e.g. `7338` (to avoid binding conflict with the first instance on `7337`)
- `tmux_socket` — e.g. `"schmux-2"` (to isolate tmux sessions)
- Different repos/workspaces as desired

## Usage Examples

```bash
# Instance 1 (default)
schmux start

# Instance 2
schmux --config-dir ~/.schmux-work start

# Or via env var
SCHMUX_HOME=~/.schmux-work schmux start

# Status of instance 2
schmux --config-dir ~/.schmux-work status

# Attach to a session in instance 2
schmux --config-dir ~/.schmux-work attach my-session
```

## Daemon Start (Background) Propagation

When `schmux start` forks a background daemon process, the resolved config dir must be propagated to the child. This is done by setting `SCHMUX_HOME` in the child process's environment, so the forked `schmux daemon-run` inherits the correct directory without needing to re-parse CLI flags.
