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
│  - ControlSource abstraction (tmux control mode)         │
│  - SessionRuntime: output log, fan-out, event routing    │
│                                                          │
│  Workspace Manager (internal/workspace/manager.go)       │
│  - Clone/checkout git repos (worktrees or full clones)   │
│  - VCS abstraction (git + sapling backends)              │
│  - Git watcher, overlay management, linear sync          │
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
│   └── controlmode/     # tmux control mode protocol
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
    d.wireCallbacks(...)                         // tunnel, events, floor manager
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

| Group                | File(s)                                                                                                                                  | Key dependencies                                                 |
| -------------------- | ---------------------------------------------------------------------------------------------------------------------------------------- | ---------------------------------------------------------------- |
| `SessionHandlers`    | `handlers_sessions.go`                                                                                                                   | config, state, session, workspace, models, preview, persona      |
| `SpawnHandlers`      | `handlers_spawn.go`, `handlers_branches.go`                                                                                              | config, state, session, workspace, models, persona, style, spawn |
| `GitHandlers`        | `handlers_git.go`, `handlers_vcs.go`, `handlers_diff.go`, `handlers_inspect.go`, `handlers_sync.go`, `commit.go`                         | config, state, workspace, tmuxServer                             |
| `ConfigHandlers`     | `handlers_config.go`, `handlers_models.go`, `handlers_usermodels.go`, `handlers_detection.go`, `handlers_features.go`                    | config, state, models, workspace                                 |
| `WorkspaceHandlers`  | `handlers_workspace.go`, `handlers_tabs.go`, `handlers_overlay.go`, `handlers_dispose.go`, `handlers_repos.go`, `handlers_backburner.go` | config, state, workspace, preview, session                       |
| `AutolearnHandlers`  | `handlers_autolearn.go`                                                                                                                  | state (WorkspaceStore), autolearn stores, executor               |
| `SpawnEntryHandlers` | `handlers_spawn_entries.go`                                                                                                              | spawn stores                                                     |
| `StyleHandlers`      | `handlers_styles.go`                                                                                                                     | style manager                                                    |
| `PersonaHandlers`    | `handlers_personas.go`                                                                                                                   | persona manager                                                  |

Cross-cutting Server methods (like `BroadcastSessions`) are injected as callback fields. Handlers that don't fit any group (healthz, auth, WebSocket, PRs) remain on `*Server`.

## ControlSource layer

The `ControlSource` interface is the input boundary for `SessionRuntime`:

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

`nogithub`, `notunnel`, `nodashboardsx`, `norepofeed`, `nosubreddit`, `noautolearn`, `nofloormanager`, `notimelapse`, `noposthog`, `noupdate`, `nomodelregistry`, `nopersonas`, `nocommstyles`, `nobuildmonitor`

Features are included by default; tags exclude them. Each disabled stub exposes `IsAvailable() bool` returning `false`.

## Build monitor and CI status

`internal/buildmonitor` is the **single owner of CI status**. It owns:

- A branch-head status store keyed by `(repo, branch, SHA)` — GitHub may run the same commit on multiple branches with different results, so branch is part of CI identity
- A `Monitor.Status(repo, branch, SHA)` derivation that maps a workspace's current remote branch head to a chip value (`queued | in_progress | failure | success`, absent when the head is unknown or the repo has no workflows)
- A `Monitor.CheckPass(...)` that runs one CI status pass per unit, persists unit state, and records transitions for remediation orchestration. Unit rows hold each active workflow's newest run on the branch, whatever commit it built, fetched with one request per workflow (`GET /actions/workflows/{id}/runs?branch=&per_page=1`): workflows are independent, and a scheduled or dispatched workflow's newest run is rarely on the head. A row never moves to a run created before the one it records, nor blanks when a response has no run; such responses come back in `PassResult.StaleRuns` and the dashboard logs them. A row keeps its remediation session while the previous row exists — the agent works in the workflow's dedicated workspace, which outlives the failure episode — and the dashboard decides whether that agent counts (the session is in state and lives in the workspace carrying the workflow's name). The commit store entry for the unit head comes from the same runs, scoped to the head SHA. Runs are reconciled against job status (GitHub may start jobs before advancing the run). Watched heads on other branches still use the repo-wide branch listing
- `AggregateWorkflows(rows)`, the unit roll-up behind the build monitor page and toolbar failure count: failure first (`failure > in_progress > queued > success`), so one workflow mid-run never hides another's failure. It differs on purpose from `Monitor.Status`, which answers whether one commit's CI is finished and green
- A pass-owned watch set of branch heads supplied by the dashboard; the monitor stores no workspace identity

Remediation orchestration (launching a workspace+session for a failing run) is a separate concern owned by the dashboard. The monitor emits transition events only for monitored units, which always represent the current default-branch head; watched non-default branches update CI chips but never emit remediation events. The dashboard decides what to launch. The buildmonitor package does not import `workspace`, `session`, `spawn`, or `dashboard`; data flows in as inputs and out as a `PassResult`.

The dashboard translates chip derivation into the sessions response: per-workspace `ci_status` / `ci_url` are read off the monitor at broadcast time. PR tracking lives in `internal/dashboard/workspace_prs.go` (a dashboard-owned tracker — legacy seam that pre-dates the monitor's branch-head model).

Frontend wiring: `assets/dashboard/src/contexts/BuildMonitorContext.tsx` owns the `/api/build-monitor` fetch+refetch (driven by `buildMonitorUpdateCount` from `SessionsContext`). The page (`routes/BuildMonitorPage.tsx`) and the toolbar (`components/ToolsSection.tsx`) consume that context. The chip in the workspace header (`components/CIStatusChip.tsx`) is a thin component over the sessions payload's `ci_status` / `ci_url` fields.

## Dedicated workspaces

Some subsystems need a workspace of their own. Client Performance needs one workspace where its agent receives browser recordings; Build Monitor needs one workspace per monitored repo and workflow, where an agent fixes CI failures.

Both used to hold a workspace ID instead. A stored workspace ID can outlive its workspace, and a later workspace can reuse the number, so the pointer ends up naming an unrelated workspace -- or nothing at all. That happened to Client Performance: its record outlived its workspace, the number was reused, and the Launch button failed permanently with "performance workspace is unavailable". A session ID has no such failure mode: it is the workspace ID plus a random suffix, so it can go missing but never matches a different session. Hence the rule that a dependent system never stores a workspace ID -- it asks for its workspace by name and tracks only its agent's session ID.

A **dedicated workspace** is an ordinary workspace carrying a name in `state.Workspace.Dedicated`. The name lives on the workspace record, so disposing the workspace removes the name with it and nothing can point at a reused workspace number. The workspace manager owns the mechanism (`internal/workspace/dedicated.go`); callers own their agents.

Four calls make up the interface:

- `RegisterDedicated(name, DedicatedRegistration)` -- declares the workspace and what it needs: a configured repo URL, a branch, and a label (defaults to the name). Registrations live in memory and come from the caller's config, so a caller registers at daemon start and again whenever its config changes. A workspace that carries the name but no longer matches the registration's repo loses the name: the old workspace must not keep serving a name that now points at another repo. A changed registration takes the name's lock and waits for an in-flight `Ensure`; an unchanged one returns at once, because callers register again on every config save.
- `RegisteredDedicated(prefix)` -- lists the names carried by workspaces in state that start with the prefix. At daemon start each caller reconciles: it unregisters the names under its prefix that its current config no longer wants.
- `UnregisterDedicated(name)` -- drops the registration and removes the name from the workspace. The workspace stays where it is as an ordinary workspace; unregistering never destroys the user's work. It takes the name's lock, so an unregister that races an in-flight `Ensure` waits for it: the workspace the `Ensure` creates never keeps a name that has no registration.
- `EnsureDedicated(ctx, name)` -- returns the workspace for a name, creating it when there is none. It returns the workspace carrying the name when it is in the registered repo, is not disposing, and its directory exists. Otherwise the name is removed from it (leaving an ordinary workspace) and a brand-new one is created. Ensure never adopts an existing workspace through `GetOrCreate`'s reuse tiers, and it is serialized per name, so two concurrent calls never create two workspaces.

**Reuse-tier exclusion.** `GetOrCreate`'s tiers (same-branch, any-branch) skip any workspace carrying a `Dedicated` name. Without this, a user spawning on the dedicated branch while the agent is gone would take the workspace over and reset it, destroying the agent's work.

**Agents are the caller's job.** Schmux does not start, find, name, or message agents for a dedicated workspace, and sessions carry no name. Each caller starts its own agents in its dedicated workspace and tracks them by session ID. A session ID is safe to hold: it is the workspace ID plus a random suffix, so it can go missing but never matches a different session. A forced dispose can leave a session in state after its workspace is gone, so a caller counts a tracked session only while its workspace carries the caller's name.

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

`~/.schmux/config.json` -- user-editable. The `Config` struct embeds `ConfigData` for all JSON fields. Key areas: `repos`, `run_targets`, `quick_launch`, `nudgenik`, `branch_suggest`, `compound`, `autolearn`, `sessions`, `network`, `access_control`, `remote_access`, `floor_manager`, `timelapse`, `models`.

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
