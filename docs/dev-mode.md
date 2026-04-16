# Dev Mode

## What it does

Runs the Go backend and React frontend together with hot-reload support, workspace switching, and real-time event monitoring. Frontend changes reflect in the browser within ~100ms via Vite HMR. Backend rebuilds are triggered manually from the terminal or dashboard. Failed builds are safe — the old binary keeps running.

## Quick start

```bash
./dev.sh
```

Opens the dashboard at http://localhost:7337. Press `q` to stop.

First run downloads Go modules, builds the React dashboard, and installs dev-runner npm dependencies. Subsequent runs skip these steps unless dependencies change.

### Flags

| Flag      | Effect                                                                              |
| --------- | ----------------------------------------------------------------------------------- |
| `--plain` | Streams interleaved `[FE]`/`[BE]` log lines to stdout instead of the split-pane TUI |

## How it works

Dev mode has three layers: a bash setup script, a TypeScript dev-runner TUI, and the Go daemon running with special flags.

### Startup sequence

```
./dev.sh
  ├─ Download Go modules (if go.sum changed)
  ├─ Build React dashboard (if dist/ missing)
  ├─ Install dev-runner npm deps (if node_modules/ missing)
  ├─ Build schmux CLI (go build ./cmd/schmux)
  ├─ Snapshot pre-npx environment (SCHMUX_PRISTINE_*)
  └─ Delegate to tools/dev-runner/src/main.tsx (via exec npx)
       ├─ Kill existing daemon (if running)
       ├─ Build Go binary → tmp/schmux
       ├─ Kill orphaned Vite processes on port 7338
       ├─ Install dashboard npm deps
       ├─ Write dev-state.json (source workspace)
       ├─ Build cleaned env via cleanEnv() (strips npx pollution)
       ├─ Start Vite dev server (port 7338)
       └─ Start daemon: schmux daemon-run --dev-mode --dev-proxy (with cleaned env)
```

### Daemon flags

The dev-runner starts the daemon with two special flags:

- **`--dev-mode`** — Enables dev-only API endpoints (`/api/dev/*`), event broadcasting to the dashboard, and the ability to exit with code 42 for workspace switching.
- **`--dev-proxy`** — Routes all non-API HTTP requests to the Vite dev server at `http://localhost:7338` instead of serving the embedded static dashboard assets. This enables Vite HMR in the browser.

### Hot-reload behavior

| Component      | Mechanism                                               | Latency                    |
| -------------- | ------------------------------------------------------- | -------------------------- |
| React frontend | Vite HMR — file changes trigger instant browser updates | ~100ms                     |
| Go backend     | Manual rebuild via `r` key or dashboard button          | ~5-10s (compile + restart) |

There is no automatic file watcher for Go. You trigger rebuilds explicitly.

## Dev-runner TUI

The dev-runner (`tools/dev-runner/`) is an Ink (React for CLI) application that manages both processes and provides a split-pane log view.

### Layout

```
┌──────────────────────────────────┐
│ schmux dev                       │
│ Dev root   /path/to/schmux       │
│ Workspace  /path/to/schmux       │
│ Dashboard  http://localhost:7337  │
│ Backend ● running  Frontend ● running │
├──────────────────────────────────┤
│ Frontend                         │
│   (Vite output)                  │
├──────────────────────────────────┤
│ Backend                          │
│   (Go daemon output)             │
│                                  │
├──────────────────────────────────┤
│ r restart backend  p pull  w reset workspace  c clear logs  l split logs  q quit │
└──────────────────────────────────┘
```

### Keyboard shortcuts

| Key | Action                                         | Available when         |
| --- | ---------------------------------------------- | ---------------------- |
| `r` | Stop daemon, rebuild Go binary, restart daemon | Running, not mid-build |
| `p` | Git pull, then rebuild and restart             | Running, not mid-build |
| `w` | Reset workspace back to dev root               | Workspace is switched  |
| `c` | Clear log panels                               | Always (TUI mode only) |
| `l` | Toggle horizontal/vertical log layout          | Always (TUI mode only) |
| `q` | Stop both processes and exit                   | Always                 |

### Process status indicators

| Symbol     | Meaning                        |
| ---------- | ------------------------------ |
| `●` green  | Running                        |
| `●` yellow | Starting, building, or pulling |
| `●` red    | Crashed or stopped             |
| `●` dim    | Idle (not yet started)         |

## Workspace switching

Workspace switching lets you test code from a different worktree without restarting `./dev.sh`. This is the core workflow for testing agent-produced code: click a button in the dashboard, and the daemon rebuilds from that worktree's source.

### From the dashboard

Each workspace card in the sidebar shows a button when dev mode is active:

- **"Test"** — Switch to this workspace (rebuild + restart from its code)
- **"Rebuild"** — Appears when this workspace is already the live source (rebuild + restart)

Only workspaces in the same repository as the dev source are eligible.

### How it works

1. Dashboard sends `POST /api/dev/rebuild` with `{workspace_id, type}` where type is `"frontend"`, `"backend"`, or `"both"`.
2. The daemon writes `~/.schmux/dev-restart.json` with the workspace path and type.
3. The daemon exits with code 42.
4. The dev-runner detects exit code 42, reads the restart manifest.
5. If type includes backend: rebuilds Go binary from the new workspace path.
6. If type includes frontend: restarts Vite pointed at the new workspace's `assets/dashboard/`.
7. npm dependencies are synced (`npm install`) before Vite restarts.
8. The daemon is restarted with the new binary.
9. The dashboard auto-reconnects when the daemon comes back.

If the Go build fails, the dev-runner keeps the previous binary and restarts the daemon with it. The build error appears in the backend log panel.

### Exit code 42

The daemon uses exit code 42 as a signal to the dev-runner that a workspace switch was requested (not a crash). The flow:

```
Dashboard POST /api/dev/rebuild
  → daemon writes dev-restart.json
  → daemon.DevRestart() closes devRestartChan
  → daemon.Run() returns ErrDevRestart
  → main.go: os.Exit(42)
  → dev-runner: handleDaemonExit(42) reads manifest, rebuilds, restarts
```

Any other exit code is treated as a crash. The dev-runner does not automatically restart on non-42 exits.

## Vite integration

### Dev proxy

When `--dev-proxy` is set, the Go HTTP server routes all non-API requests through a reverse proxy to `http://localhost:7338` (Vite). API routes (`/api/*`, `/ws/*`) are handled by Go directly. This means the browser loads the React app from Vite (with HMR) while API calls go to the Go backend.

### Watch pause/resume

During git operations (rebase, merge), source files can temporarily contain conflict markers that cause Vite transform errors. The daemon pauses Vite's file watcher before git operations and resumes it after:

- `POST http://localhost:7338/__dev/pause-watch` — Suppresses HMR updates and blocks Vite server restarts
- `POST http://localhost:7338/__dev/resume-watch` — Resumes watching; HMR updates suppressed while paused are not replayed

This is implemented as a custom Vite plugin in `assets/dashboard/vite.config.js`.

## Dashboard dev-only features

Dev mode features are split into two categories based on how they are enabled:

### Self-build features (require `./dev.sh`)

These features require the daemon to be started with `--dev-mode` and are only available when running via `./dev.sh`. The `/api/healthz` response includes `dev_mode: true` when active.

- **Workspace switching** — Switch which worktree's code is running from the dashboard sidebar
- **Rebuild** — Trigger Go binary rebuild from the dashboard or TUI
- **Vite proxy** — React app served from Vite dev server with HMR

### Debug diagnostic features (available via `debug_ui` config OR dev mode)

These features are available when `debug_mode` is active. Debug mode is enabled automatically in dev mode, but can also be enabled independently by setting `debug_ui: true` in the config. The `/api/healthz` response includes `debug_mode: true` when active.

#### Event Monitor sidebar

A panel in the sidebar showing the last few events from all sessions. Events are streamed in real-time via the `/ws/dashboard` WebSocket.

#### Event Monitor page (`/events`)

A full-page event table with:

- **Type filters** — Toggle visibility of `status`, `failure`, `reflection`, and `friction` events
- **Session filter** — Show events from a specific session or all sessions
- **Auto-scroll** — Follows new events; pauses when you scroll up manually
- **Expandable rows** — Click any row to see the full JSON event payload
- **History merge** — Fetches historical events from `/api/dev/events/history` and merges them with live WebSocket events

#### Diagnostic panels

- **Curation Status** — Lore curation tracking
- **Tmux Diagnostic** — Terminal rendering diagnostics (ring buffers, stats)
- **Typing Performance** — Input latency monitoring

#### Testing helpers

- **Simulate tunnel** buttons — test remote access features locally
- **Lore reset** button — clear lore state for testing

### Enabling debug UI without dev.sh

To enable debug diagnostic features in production (without `./dev.sh`):

1. Set `"debug_ui": true` in `~/.schmux/config.json`
2. Or toggle it from the Settings page in the web dashboard

No restart required — the setting takes effect immediately. This is useful for diagnosing issues in production without the overhead of the full dev mode setup.

### Workspace protection

The workspace currently serving as the dev mode source cannot be disposed. This prevents accidentally destroying the workspace whose code is running.

## State files

Dev mode uses three JSON files in `~/.schmux/` for coordination between the dev-runner and the daemon:

| File                    | Written by | Read by    | Purpose                                                                 |
| ----------------------- | ---------- | ---------- | ----------------------------------------------------------------------- |
| `dev-state.json`        | Dev-runner | Daemon     | Tracks which workspace path is currently serving dev mode               |
| `dev-restart.json`      | Daemon     | Dev-runner | Restart manifest: workspace path and type (`frontend`/`backend`/`both`) |
| `dev-build-status.json` | Dev-runner | Daemon     | Last build result: success/failure, error message, timestamp            |

All three are cleaned up when the dev-runner exits.

### Schemas

**dev-state.json**

```json
{
  "source_workspace": "/Users/you/dev/schmux"
}
```

**dev-restart.json**

```json
{
  "workspace_id": "ws-abc123",
  "workspace_path": "/Users/you/dev/schmux-feature",
  "type": "both",
  "timestamp": "2026-03-29T12:34:56.789Z"
}
```

**dev-build-status.json**

```json
{
  "success": true,
  "workspace_path": "/Users/you/dev/schmux-feature",
  "error": "",
  "at": "2026-03-29T12:35:02.123Z"
}
```

## API endpoints

### Self-build routes (require `--dev-mode`)

These endpoints are only registered when the daemon runs with `--dev-mode` (via `./dev.sh`).

| Method | Path               | Purpose                                                                        |
| ------ | ------------------ | ------------------------------------------------------------------------------ |
| `GET`  | `/api/dev/status`  | Returns dev mode state: active flag, source workspace, last build status       |
| `POST` | `/api/dev/rebuild` | Triggers workspace switch/rebuild (writes manifest, exits daemon with code 42) |

### Debug routes (require `debug_mode`)

These endpoints are registered when the daemon is in dev mode OR when `debug_ui` is set to `true` in the config.

| Method | Path                            | Purpose                                                         |
| ------ | ------------------------------- | --------------------------------------------------------------- |
| `GET`  | `/api/dev/events/history`       | Returns up to 200 historical monitor events                     |
| `POST` | `/api/dev/simulate-tunnel`      | Testing helper: simulates a remote tunnel connection            |
| `POST` | `/api/dev/simulate-tunnel-stop` | Testing helper: clears simulated tunnel                         |
| `POST` | `/api/dev/clear-password`       | Testing helper: clears remote access password                   |
| `POST` | `/api/dev/diagnostic-append`    | Appends scroll/lifecycle diagnostic data to a capture directory |

These endpoints are always registered (not dev-mode-only):

| Method | Path                    | Purpose                                            |
| ------ | ----------------------- | -------------------------------------------------- |
| `GET`  | `/api/environment`      | Returns system-vs-tmux environment comparison      |
| `POST` | `/api/environment/sync` | Syncs one key from system env into tmux server env |

## Key files

| File                                              | Purpose                                                                   |
| ------------------------------------------------- | ------------------------------------------------------------------------- |
| `dev.sh`                                          | Entry point: dependency setup, env snapshot, delegates to dev-runner      |
| `tools/dev-runner/src/main.tsx`                   | Dev-runner entry: alternate screen setup, cleanup handlers                |
| `tools/dev-runner/src/App.tsx`                    | Dev-runner core: startup sequence, workspace switching, keyboard handlers |
| `tools/dev-runner/src/components/StatusBar.tsx`   | TUI status bar: workspace, dashboard URL, process status                  |
| `tools/dev-runner/src/components/KeyBar.tsx`      | TUI keyboard shortcut bar                                                 |
| `tools/dev-runner/src/lib/state.ts`               | State file read/write helpers and path constants                          |
| `internal/dashboard/handlers_dev.go`              | Dev mode HTTP handlers: status, rebuild, simulate-tunnel                  |
| `internal/dashboard/server.go`                    | Dev proxy setup and route registration                                    |
| `internal/daemon/daemon.go`                       | `DevRestart()` method, `ErrDevRestart`, exit code 42 handling             |
| `cmd/schmux/main.go`                              | `parseDaemonRunFlags`, exit code 42 → `os.Exit(42)`                       |
| `assets/dashboard/vite.config.js`                 | Vite pause-watch plugin for safe git operations                           |
| `assets/dashboard/src/hooks/useDevStatus.ts`      | Frontend hook: fetches dev status when dev mode is active                 |
| `assets/dashboard/src/routes/EventsPage.tsx`      | Full-page event monitor (dev mode only)                                   |
| `tools/dev-runner/src/lib/cleanEnv.ts`            | Strips npx env pollution before daemon spawn (see Environment isolation)  |
| `internal/dashboard/handlers_environment.go`      | Environment comparison and sync handlers                                  |
| `assets/dashboard/src/routes/EnvironmentPage.tsx` | System-vs-tmux environment comparison page                                |

## Environment isolation

### The problem

`dev.sh` launches the dev-runner via `exec npx --prefix tools/dev-runner tsx ...`. When npm runs a script through `npx`, it injects environment variables into the child process:

- **`npm_config_prefix`**, **`npm_config_global_prefix`**, **`npm_config_local_prefix`** — set to `tools/dev-runner/`, redirecting npm's global install location
- **`npm_config_*`** — npm's full resolved configuration dumped as env vars
- **`npm_package_*`** — fields from dev-runner's `package.json`
- **`npm_lifecycle_*`** — script execution metadata
- **`INIT_CWD`**, **`NODE`** — npm execution context
- **`PATH`** — prepended with `node_modules/.bin` directory chains

If these vars leak into the schmux daemon, every child process inherits them — including tmux sessions where agents run. An agent running `npm install` or `npm link` inside a session would use `tools/dev-runner/` as its prefix instead of the system npm location.

### The fix

The fix has two parts:

**1. Snapshot (dev.sh):** Before `exec npx`, `dev.sh` exports two snapshot variables:

- `SCHMUX_PRISTINE_PATH` — the original PATH before npx modifies it
- `SCHMUX_PRISTINE_NPM_VARS` — base64-encoded, NUL-delimited dump of any `npm_*` vars that already existed in the user's shell (e.g., `npm_config_registry` from `.zshrc`). Empty if none existed.

**2. Restore (dev-runner):** `tools/dev-runner/src/lib/cleanEnv.ts` builds a cleaned environment before spawning the daemon:

1. Strip all `npm_*` vars (removes everything npx injected or overwrote)
2. Decode `SCHMUX_PRISTINE_NPM_VARS` and restore any npm vars that existed before npx (preserving user config)
3. Strip `INIT_CWD` and `NODE` (npx-only vars)
4. Restore `PATH` from the snapshot
5. Remove the `SCHMUX_PRISTINE_*` meta-vars themselves

The daemon process starts with a clean environment. All subsystems — tmux sessions, agent detection, workspace git operations, tunnel management — inherit the restored env without any per-subsystem fixes.

The Vite process is **not** cleaned. It runs inside the npm context and needs the npm vars to function.

### When not running via dev.sh

When the daemon runs directly (`./schmux daemon-run`), the `SCHMUX_PRISTINE_*` vars are absent. `cleanEnv()` detects this and passes the environment through unchanged. The fix has zero effect on production usage.

## Environment page

The Environment page (`/environment`) compares the current system environment against the tmux server's global environment, showing which variables are in sync, which differ, and which exist only on one side. Sync buttons push individual system values into the tmux server so new sessions pick up changes without restarting.

**Why it exists:** The tmux server captures its environment at startup. When you later change `.zshrc` or `.zprofile`, the tmux server retains stale values. Previously the only fix was killing the tmux server, destroying all sessions.

**How it works:** The backend spawns a fresh login shell (`env -i ... $SHELL -l -i -c env`, 10-second timeout) to capture the current system environment, reads the tmux server environment (`tmux show-environment -g`), filters out blocked keys, and compares.

| Status          | Meaning                            | Action |
| --------------- | ---------------------------------- | ------ |
| **in sync**     | Key exists in both, values match   | No     |
| **differs**     | Key exists in both, values differ  | Sync   |
| **system only** | Exists in login shell but not tmux | Sync   |
| **tmux only**   | Exists in tmux but not login shell | No     |

Syncing calls `tmux set-environment -g KEY VALUE`. Only new sessions inherit the change -- existing sessions keep their original values.

**Blocked keys:** A hardcoded blocklist excludes tmux-internal (`TMUX`, `TMUX_PANE`), session-transient (`SHLVL`, `PWD`), terminal-specific (`TERM_SESSION_ID`, `GHOSTTY_*`, `ITERM_*`), macOS system (`LaunchInstanceID`, `XPC_*`), and npm pollution (`npm_*`, `INIT_CWD`).

## Troubleshooting

### Port 7338 already in use

Vite runs with `--strictPort`, so it fails if port 7338 is occupied. The dev-runner tries to kill orphaned Vite processes on startup, but if another application uses this port, you need to free it manually.

### Build fails after workspace switch

The old binary keeps running. Check the backend log panel for the compile error. Fix the code in the target workspace and press `r` to retry, or press `w` to reset back to the dev root workspace.

### Dashboard shows stale data after restart

The dashboard auto-reconnects via WebSocket after the daemon restarts. If state appears stale, hard-refresh the browser (`Cmd+Shift+R`). The dev proxy disables HTTP keep-alives to prevent stale connections after Vite restarts.

### "Test" button missing on workspaces

The button only appears for workspaces in the same repository as the dev source workspace, and only when the daemon is running in dev mode. Workspaces from different repos are not eligible for switching.
