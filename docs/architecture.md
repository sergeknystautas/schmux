# Architecture

Overall architecture and design patterns for the schmux codebase.

## Overview

schmux is a multi-agent AI orchestration system that runs multiple AI coding agents simultaneously across tmux sessions, each in isolated workspace directories. A web dashboard provides real-time monitoring and management.

```
┌──────────────────────────────────────────────────────────┐
│ Daemon (internal/daemon/daemon.go)                       │
├──────────────────────────────────────────────────────────┤
│  Dashboard Server (:7337)                                │
│  - HTTP API + chi router (internal/dashboard/)           │
│  - WebSocket terminal streaming + dashboard broadcast    │
│  - Serves embedded React assets or Vite dev proxy        │
│  - Handler groups with explicit dependency declarations  │
│                                                          │
│  Session Manager (internal/session/manager.go)           │
│  - Spawn/dispose tmux sessions                           │
│  - ControlSource abstraction (local + remote streaming)  │
│  - SessionRuntime: output log, fan-out, event routing    │
│                                                          │
│  Workspace Manager (internal/workspace/manager.go)       │
│  - Clone/checkout git repos (worktrees or full clones)   │
│  - VCS abstraction (git + sapling backends)              │
│  - Git watcher, overlay management, linear sync          │
│                                                          │
│  Remote Manager (internal/remote/manager.go)             │
│  - SSH connections via tmux control mode                  │
│  - Multi-instance hosts per flavor                       │
│                                                          │
│  Models Manager (internal/models/manager.go)             │
│  - Registry-driven model catalog + resolution            │
│                                                          │
│  Preview Manager (internal/preview/manager.go)           │
│  - Reverse proxy for workspace dev servers               │
│                                                          │
│  Timelapse (internal/timelapse/)                         │
│  - Always-on session recording in asciicast v2           │
│                                                          │
│  Floor Manager (internal/floormanager/)                  │
│  - Supervisory agent monitoring session status           │
│                                                          │
│  Tunnel Manager (internal/tunnel/)                       │
│  - Cloudflare tunnel for remote access                   │
│                                                          │
│  Config/State (internal/config/, internal/state/)        │
│  - ~/.schmux/config.json  (repos, targets, models, etc.) │
│  - ~/.schmux/state.json   (workspaces, sessions)         │
└──────────────────────────────────────────────────────────┘
```

## Directory structure

```
cmd/
├── schmux/              # CLI entry point + subcommands
├── build-dashboard/     # Go wrapper for building React dashboard
├── build-website/       # Documentation site builder
└── gen-types/           # TypeScript type generator from Go contracts

pkg/
├── cli/                 # Daemon client interface for CLI commands
└── shellutil/           # Shell quoting and argument splitting

internal/
├── api/contracts/       # Shared API contract types (Go → TypeScript)
├── assets/              # Asset download helpers
├── autolearn/           # Continual learning (batches, curation, merge)
├── benchutil/           # Shared benchmark helpers
├── branchsuggest/       # AI-powered branch name suggestion
├── commitmessage/       # AI commit message generation
├── compound/            # Bidirectional overlay sync
├── config/              # Config IO (~/.schmux/config.json)
├── conflictresolve/     # AI-powered conflict resolution
├── daemon/              # Background process, lifecycle orchestration
├── dashboard/           # HTTP server + handler groups + WebSocket
├── dashboardsx/         # dashboard.sx TLS provisioning + heartbeat
├── detect/              # Tool/agent/model detection + adapters
├── difftool/            # External diff tool support
├── escbuf/              # ANSI escape split-safe framing
├── events/              # Agent event system (JSONL watcher + handlers)
├── fileutil/            # Atomic file write helper
├── floormanager/        # Supervisory agent (Floor Manager)
├── github/              # GitHub PR client, discovery, OAuth
├── logging/             # Structured logging wrappers
├── models/              # Model catalog (registry, user-defined, resolution)
├── nudgenik/            # AI session status assessment
├── oneshot/             # One-shot LLM execution (structured output)
├── persona/             # Agent persona management (YAML)
├── preview/             # Web preview reverse proxy
├── remote/              # Remote workspace via SSH
│   └── controlmode/     # tmux control mode protocol
├── repofeed/            # Cross-developer activity feed
├── schema/              # JSON schema generation from Go structs
├── schmuxdir/           # ~/.schmux/ path management
├── session/             # Session lifecycle, ControlSource, Tracker
├── spawn/               # Spawn entry store + metadata
├── state/               # State IO (~/.schmux/state.json)
├── style/               # UI style/theme management
├── subreddit/           # AI-generated development digest
├── telemetry/           # Anonymous usage tracking (PostHog + external command)
├── timelapse/           # Session recording (asciicast v2)
├── tmux/                # tmux CLI wrapper
├── tunnel/              # Cloudflare tunnel for remote access
├── types/               # Shared leaf types (tool names, model ID migration)
├── update/              # Self-update mechanism
├── version/             # Build version info
└── workspace/           # Repo clone/checkout, VCS operations
    └── ensure/          # Workspace config setup (hooks, git exclude)

assets/dashboard/        # React frontend (built to dist/)
```

## Key entry point

`cmd/schmux/main.go` parses CLI commands and delegates to `internal/daemon/`. The daemon's `Run()` method is a ~75-line lifecycle orchestration:

```go
func (d *Daemon) Run(background, devProxy, devMode bool) error {
    di, err := d.initConfigAndState(devMode)     // config, state, telemetry, tmux
    d.initDashboardSX(cfg, st, logger)           // custom domain provisioning
    wm, sm := d.initManagers(...)                // workspace + session managers
    server, mm := d.initDashboard(...)           // detection, models, PR discovery, server
    d.wireCallbacks(...)                         // remote, tunnel, events, floor manager
    d.restoreSessions(...)                       // tmux restore, git watcher
    d.initCompound(...)                          // overlay sync
    d.initAutolearn(...)                         // spawn store + learning system
    d.startBackgroundJobs(...)                   // pollers, schedulers, feeds
    d.startAndWait(...)                          // serve until signal
    d.shutdown(...)                              // orderly teardown
}
```

Each lifecycle method is self-contained with explicit parameters and return values. The `daemonInit` struct carries config/state/telemetry/loggers across phases; `shutdownHandles` carries handles for orderly teardown.

## Interface decomposition

### StateStore

`state.StateStore` is composed from domain-specific sub-interfaces:

- `SessionStore` — session CRUD, nudge tracking, xterm title
- `WorkspaceStore` — workspace CRUD, overlay manifests, tabs, resolve conflicts
- `RemoteHostStore` — remote host CRUD, lookups by flavor/profile/hostname
- `PersistenceStore` — Save, SaveBatched, FlushPending, NeedsRestart

Consumers that need all domains take `StateStore`; consumers with narrow needs can take a sub-interface (e.g., `AutolearnHandlers` takes `WorkspaceStore`).

### WorkspaceManager

`workspace.WorkspaceManager` is composed from:

- `WorkspaceCRUD` — lifecycle (create, dispose, purge, scan)
- `WorkspaceVCS` — git operations (status, graph, sync, push)
- `WorkspaceInfra` — infrastructure (workspace dir, overlays, origin queries)

### Config

`config.Config` embeds `ConfigData` (all JSON-serializable fields) separately from runtime fields (`mu`, `path`, `repoURLCache`). This allows `Reload()` to atomically swap all config values with a single struct assignment, preventing the bug class where new fields are forgotten in a field-by-field copy.

## Dashboard handler groups

HTTP handlers are organized into handler group structs, each declaring only the dependencies it needs:

| Group                | File(s)                                                                                                                                  | Key dependencies                                                         |
| -------------------- | ---------------------------------------------------------------------------------------------------------------------------------------- | ------------------------------------------------------------------------ |
| `SessionHandlers`    | `handlers_sessions.go`                                                                                                                   | config, state, session, workspace, models, remote, preview, persona      |
| `SpawnHandlers`      | `handlers_spawn.go`, `handlers_branches.go`                                                                                              | config, state, session, workspace, models, remote, persona, style, spawn |
| `GitHandlers`        | `handlers_git.go`, `handlers_vcs.go`, `handlers_diff.go`, `handlers_inspect.go`, `handlers_sync.go`, `commit.go`                         | config, state, workspace, remote, tmuxServer                             |
| `ConfigHandlers`     | `handlers_config.go`, `handlers_models.go`, `handlers_usermodels.go`, `handlers_detection.go`, `handlers_features.go`                    | config, state, models, workspace                                         |
| `WorkspaceHandlers`  | `handlers_workspace.go`, `handlers_tabs.go`, `handlers_overlay.go`, `handlers_dispose.go`, `handlers_repos.go`, `handlers_backburner.go` | config, state, workspace, preview, session                               |
| `RemoteHandlers`     | `handlers_remote.go`                                                                                                                     | config, state, remote, preview                                           |
| `AutolearnHandlers`  | `handlers_autolearn.go`                                                                                                                  | state (WorkspaceStore), autolearn stores, executor                       |
| `SpawnEntryHandlers` | `handlers_spawn_entries.go`                                                                                                              | spawn stores                                                             |
| `StyleHandlers`      | `handlers_styles.go`                                                                                                                     | style manager                                                            |
| `PersonaHandlers`    | `handlers_personas.go`                                                                                                                   | persona manager                                                          |

Cross-cutting Server methods (like `BroadcastSessions`) are injected as callback fields. Handlers that don't fit any group (healthz, auth, WebSocket, PRs) remain on `*Server`.

## ControlSource layer

The `ControlSource` interface unifies local and remote terminal streaming:

```go
type ControlSource interface {
    Events() <-chan SourceEvent
    SendKeys(keys string) (controlmode.SendKeysTimings, error)
    CaptureVisible() (string, error)
    CaptureLines(n int) (string, error)
    GetCursorState() (controlmode.CursorState, error)
    Resize(cols, rows int) error
    Close() error
}
```

- `LocalSource` -- attaches to local tmux via control mode
- `RemoteSource` -- wraps a `remote.Connection` for SSH sessions

`SessionRuntime` is source-agnostic: it drains `Events()`, appends to `OutputLog`, and fans out to WebSocket subscribers. The `session.Manager` provides `CaptureLastLines` and `GetCursorState` methods that try the tracker first and fall back to direct tmux CLI — callers don't need to know about tmux.

## HTTP router (chi)

The dashboard uses `go-chi/chi` for routing. Routes are grouped by auth requirements:

- **Public routes** -- no auth (remote-auth, OAuth callbacks)
- **WebSocket routes** -- inline auth before upgrade
- **API routes** -- auth middleware applied to group
  - Read-only endpoints: CORS + auth
  - State-changing endpoints: CORS + auth + CSRF

Adding a route: write the handler method on the appropriate handler group, add `r.Get(...)` or `r.Post(...)` in `Start()`, construct the handler group with needed dependencies. Auth/CSRF are automatic from group membership.

All API error responses use `writeJSONError(w, msg, code)` which returns `{"error": "..."}` with `Content-Type: application/json`.

## Error handling

Domain packages define sentinel errors for structured error matching:

- `spawn.ErrNotFound` — spawn entry not found
- `workspace.ErrNotFound`, `ErrInvalidCommit`, `ErrCommitNotFound`, `ErrWorkspaceLocked`
- `preview.ErrLimitReached` — preview cap exceeded
- `session.ErrNicknameInUse` — nickname collision

Handlers use `errors.Is(err, pkg.ErrXxx)` instead of string matching.

## Build tag system

Experimental features can be compiled out via build tags. Each feature has a `*_disabled.go` stub that provides no-op implementations:

`nogithub`, `notunnel`, `nodashboardsx`, `norepofeed`, `nosubreddit`, `noautolearn`, `nofloormanager`, `notimelapse`, `noposthog`, `noupdate`, `nomodelregistry`, `nopersonas`, `nocommstyles`

Features are included by default; tags exclude them. Each disabled stub exposes `IsAvailable() bool` returning `false`.

## Data flow: spawning a session

```
POST /api/spawn
  → Workspace manager: create workspace (worktree add, overlay copy)
  → Ensurer: install hooks, git exclude, autolearn instructions
  → tmux.CreateSession(ctx, name, dir, command)
  → Session manager: start SessionRuntime with ControlSource
  → Events watcher: monitor agent event files
  → WebSocket: stream output to dashboard
```

## Configuration

`~/.schmux/config.json` -- user-editable. The `Config` struct embeds `ConfigData` for all JSON fields. Key areas: `repos`, `run_targets`, `quick_launch`, `nudgenik`, `branch_suggest`, `compound`, `autolearn`, `sessions`, `network`, `access_control`, `remote_profiles`, `remote_access`, `floor_manager`, `timelapse`, `models`, `recycle_workspaces`.

`~/.schmux/state.json` -- auto-managed by daemon. Contains workspaces and sessions. Accessed through the `StateStore` interface with batched saves. File locking protects concurrent secrets writes.

Path helpers in `internal/schmuxdir/` centralize `~/.schmux/` path construction (`schmuxdir.ConfigPath()`, `schmuxdir.StatePath()`, etc.).

**Build defaults and env var expansion.** `internal/config/defaults.go` supports embedding a `build_defaults.json` file that seeds new configs via `CreateDefault()`. The `resolveConfigTemplates()` function runs `os.ExpandEnv()` on the serialized JSON, expanding `${USER}`, `$HOME`, etc. from the user's environment. This only runs during `CreateDefault()` — not during `Load()` or `Reload()` — because user configs may contain intentional `$VAR` references in shell command fields (e.g., `$SCHMUX_REMOTE_URL` in notify commands) that must not be expanded at config parse time.

## Design patterns

- **Lifecycle methods on Daemon** -- `Run()` delegates to named methods (`initConfigAndState`, `initManagers`, etc.) rather than inline logic
- **Handler groups with explicit deps** -- each handler group struct declares only the dependencies it uses
- **Composed interfaces** -- `StateStore` and `WorkspaceManager` are composed from domain-specific sub-interfaces
- **Sentinel errors** -- domain packages define typed errors; handlers use `errors.Is()` for matching
- **ControlSource for pluggable streaming** -- SessionRuntime is source-agnostic
- **ConfigData embedding** -- `Reload()` swaps all config fields atomically via struct assignment
- **Manager pattern** -- subsystems created in lifecycle methods, wired via setters, started with shutdown context
- **Build tag gating** -- experimental features compile out cleanly via `_disabled.go` stubs
- **Errors returned, not panicked** -- graceful degradation when optional features are unavailable
- **Per-workspace locking** -- coordinates sync operations vs. git status refreshes
- **WebSocket write safety** -- all writes through `wsConn` wrapper with mutex

## See also

- [react.md](react.md) -- Frontend architecture
- [api.md](api.md) -- HTTP API contract
- [terminal-pipeline.md](terminal-pipeline.md) -- Terminal streaming pipeline
- [sessions.md](sessions.md) -- Session lifecycle
- [workspaces.md](workspaces.md) -- Workspace management
- [autolearn.md](autolearn.md) -- Autolearn system
