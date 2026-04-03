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
│                                                          │
│  Session Manager (internal/session/manager.go)           │
│  - Spawn/dispose tmux sessions                           │
│  - ControlSource abstraction (local + remote streaming)  │
│  - SessionTracker: output log, fan-out, event routing    │
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
├── benchutil/           # Shared benchmark helpers
├── branchsuggest/       # AI-powered branch name suggestion
├── commitmessage/       # AI commit message generation
├── compound/            # Bidirectional overlay sync
├── config/              # Config IO (~/.schmux/config.json)
├── conflictresolve/     # AI-powered conflict resolution
├── daemon/              # Background process, main wiring loop
├── dashboard/           # HTTP server + handlers + WebSocket
├── dashboardsx/         # dashboard.sx TLS provisioning + heartbeat
├── detect/              # Tool/agent/model detection + adapters
├── difftool/            # External diff tool support
├── emergence/           # Spawn entry learning (skills)
├── escbuf/              # ANSI escape split-safe framing
├── events/              # Agent event system (JSONL watcher + handlers)
├── fileutil/            # Atomic file write helper
├── floormanager/        # Supervisory agent (Floor Manager)
├── github/              # GitHub PR client, discovery, OAuth
├── logging/             # Structured logging wrappers
├── lore/                # Continual learning (proposals, curation)
├── models/              # Model catalog (registry, user-defined, resolution)
├── nudgenik/            # AI session status assessment
├── oneshot/             # One-shot LLM execution (structured output)
├── persona/             # Agent persona management (YAML)
├── preview/             # Web preview reverse proxy
├── remote/              # Remote workspace via SSH
│   └── controlmode/     # tmux control mode protocol
├── repofeed/            # Cross-developer activity feed
├── schema/              # JSON schema generation from Go structs
├── session/             # Session lifecycle, ControlSource, Tracker
├── state/               # State IO (~/.schmux/state.json)
├── subreddit/           # AI-generated development digest
├── telemetry/           # Anonymous usage tracking (PostHog)
├── timelapse/           # Session recording (asciicast v2)
├── tmux/                # tmux CLI wrapper (free functions)
├── tunnel/              # Cloudflare tunnel for remote access
├── update/              # Self-update mechanism
├── version/             # Build version info
└── workspace/           # Repo clone/checkout, VCS operations
    └── ensure/          # Workspace config setup (hooks, git exclude)

assets/dashboard/        # React frontend (built to dist/)
```

## Key entry point

`cmd/schmux/main.go` parses CLI commands and delegates to `internal/daemon/`. The daemon's `Run()` method wires all subsystems together:

1. Load config and state, write PID file
2. Create managers (session, workspace, remote, models, preview, compound, timelapse, floor manager, tunnel)
3. Wire managers together via setter methods
4. Start background goroutines (git polling, NudgeNik, lore, repofeed, subreddit)
5. Restore trackers for running sessions from persisted state
6. Handle shutdown signals and dev mode restart (exit code 42)

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

`SessionTracker` is source-agnostic: it drains `Events()`, appends to `OutputLog`, and fans out to WebSocket subscribers. Adding a new session type requires only a new `ControlSource` implementation.

## tmux package

Free functions (not an interface). All take `context.Context` as the first argument:

```go
func CreateSession(ctx, name, dir, command) error
func SessionExists(ctx, name) bool
func CaptureOutput(ctx, name) (string, error)
func CaptureLastLines(ctx, name, lines, includeEscapes) (string, error)
func KillSession(ctx, name) error
func ListSessions(ctx) ([]string, error)
func SendKeys(ctx, name, keys) error
func GetWindowSize(ctx, name) (width, height, error)
func ResizeWindow(ctx, name, width, height) error
```

## HTTP router (chi)

The dashboard uses `go-chi/chi` for routing. Routes are grouped by auth requirements:

- **Public routes** -- no auth (remote-auth, OAuth callbacks)
- **WebSocket routes** -- inline auth before upgrade
- **API routes** -- auth middleware applied to group
  - Read-only endpoints: CORS + auth
  - State-changing endpoints: CORS + auth + CSRF

Adding a route: write the handler, add `r.Get(...)` or `r.Post(...)` in the appropriate group. Auth/CSRF are automatic from group membership.

## Data flow: spawning a session

```
POST /api/spawn
  → Workspace manager: create workspace (worktree add, overlay copy)
  → Ensurer: install hooks, git exclude, lore instructions
  → tmux.CreateSession(ctx, name, dir, command)
  → Session manager: start SessionTracker with ControlSource
  → Events watcher: monitor agent event files
  → WebSocket: stream output to dashboard
```

## Configuration

`~/.schmux/config.json` -- user-editable. Key fields: `workspace_path`, `repos`, `run_targets`, `quick_launch`, `nudgenik`, `branch_suggest`, `compound`, `lore`, `sessions`, `network`, `access_control`, `remote_flavors`, `remote_access`, `floor_manager`, `timelapse`, `recycle_workspaces`.

`~/.schmux/state.json` -- auto-managed by daemon. Contains workspaces and sessions. Accessed through the `StateStore` interface with batched saves.

## Design patterns

- **Free functions for tmux** -- no interface, package-level functions with `context.Context`
- **ControlSource for pluggable streaming** -- SessionTracker is source-agnostic
- **Manager pattern** -- subsystems created in `Daemon.Run()`, wired via setters, started with shutdown context
- **Errors returned, not panicked** -- graceful degradation when optional features are unavailable
- **Per-workspace locking** -- coordinates sync operations vs. git status refreshes
- **WebSocket write safety** -- all writes through `wsConn` wrapper with mutex

## See also

- [react.md](react.md) -- Frontend architecture
- [api.md](api.md) -- HTTP API contract
- [terminal-pipeline.md](terminal-pipeline.md) -- Terminal streaming pipeline
- [sessions.md](sessions.md) -- Session lifecycle
- [workspaces.md](workspaces.md) -- Workspace management
