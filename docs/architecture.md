# Architecture

Overall architecture and design patterns for the schmux codebase.

---

## Overview

schmux is a **multi-agent AI orchestration system** that runs multiple AI coding agents simultaneously across tmux sessions, each in isolated workspace directories. A web dashboard provides real-time monitoring and management.

```
┌─────────────────────────────────────────────────────────┐
│ Daemon (internal/daemon/daemon.go)                      │
├─────────────────────────────────────────────────────────┤
│  Dashboard Server (:7337)                               │
│  - HTTP API (internal/dashboard/handlers.go)            │
│  - WebSocket terminal streaming                         │
│  - Serves static assets from assets/dashboard/          │
│                                                         │
│  Session Manager (internal/session/manager.go)          │
│  - Spawn/dispose tmux sessions                          │
│  - Track PIDs, status, terminal output                  │
│                                                         │
│  Workspace Manager (internal/workspace/manager.go)      │
│  - Clone/checkout git repos                             │
│  - Track workspace directories                          │
│  - Overlay file copying                                │
│                                                         │
│  Preview Manager (internal/preview/manager.go)          │
│  - Ephemeral proxy ports for workspace web servers      │
│  - Auto-detect ports from tmux session output           │
│  - Reconcile/cleanup stale previews                     │
│                                                         │
│  tmux Package (internal/tmux/tmux.go)                   │
│  - tmux CLI wrapper (create, capture, list, kill)       │
│                                                         │
│  Config/State (internal/config/, internal/state/)       │
│  - ~/.schmux/config.json  (repos, targets, workspace)   │
│  - ~/.schmux/state.json    (workspaces, sessions)       │
└─────────────────────────────────────────────────────────┘
```

---

## Directory Structure

```
cmd/schmux/              # CLI entry point (main.go)
internal/
├── api/contracts/       # Shared API contract types
├── branchsuggest/       # AI-powered branch name suggestion
├── config/              # Config IO (~/.schmux/config.json)
├── conflictresolve/     # AI-powered conflict resolution
├── daemon/              # Background process, main loop
├── dashboard/           # HTTP server + handlers + websockets
├── detect/              # Tool detection (claude, codex, etc.)
├── difftool/            # External diff tool support
├── github/              # GitHub PR client and discovery
├── nudgenik/            # AI session status assessment
├── oneshot/             # One-shot LLM execution
├── preview/             # Web preview reverse proxy
├── provision/           # Remote host provisioning
├── remote/              # Remote workspace via SSH
├── schema/              # JSON schema generation
├── session/             # Session lifecycle and tracking
├── signal/              # Agent signal parsing
├── state/               # State IO (~/.schmux/state.json)
├── tmux/                # tmux integration
├── update/              # Self-update
├── vcs/                 # VCS abstraction (git, sapling)
└── workspace/           # Repo clone/checkout + overlays
    └── ensure/          # Workspace config setup (hooks, git exclude)

assets/dashboard/        # React frontend (built to dist/)
docs/                    # Documentation
```

---

## Key Packages

### Daemon (`internal/daemon/`)

Long-running background process that coordinates all other packages.

**Responsibilities:**

- Start/stop lifecycle
- Coordinate session and workspace managers
- Host the dashboard server

### Dashboard (`internal/dashboard/`)

HTTP server and WebSocket handler for terminal streaming.

**Key files:**

- `server.go` - HTTP server setup, chi router, route registration
- `handlers.go` - API endpoint handlers
- `websocket.go` - WebSocket terminal streaming

### Session (`internal/session/`)

Session lifecycle management and tracking.

**Responsibilities:**

- Spawn sessions (tmux create + agent execution)
- Track session status (spawning, running, done, disposed)
- Capture terminal output
- Dispose sessions

### Workspace (`internal/workspace/`)

Git repository management and workspace creation.

**Responsibilities:**

- Clone and checkout git repos
- Create sequential workspace directories
- Copy overlay files
- Track workspace state (dirty, ahead, behind)
- Per-workspace locking for sync operations

### Workspace Ensure (`internal/workspace/ensure/`)

Stateful service that ensures workspaces have necessary schmux configuration (agent hooks, git exclude entries). Initialized once at daemon startup with a state reference.

### Preview (`internal/preview/`)

Ephemeral proxy management for workspace web servers.

**Responsibilities:**

- Create proxy listeners on stable per-workspace ports (deterministic from port block allocation)
- Forward requests to workspace-local servers (e.g., Vite on port 5173)
- Auto-detect listening ports from tmux session output (via `ss` on Linux, `lsof` on macOS)
- Reconcile and clean up stale previews when upstream servers die

**Key files:**

- `manager.go` - Preview lifecycle management
- Auto-detection in `internal/dashboard/preview_autodetect.go`

### Schema (`internal/schema/`)

Centralized JSON schema generation from Go structs using `swaggest/jsonschema-go`. Domain packages register their types via `schema.Register()` in `init()` functions; schemas are generated on first access and cached.

**Key files:**

- `schema.go` - `Register()`, `Get()`, `Labels()`, `GenerateJSON()` API

### tmux (`internal/tmux/`)

tmux CLI wrapper.

**Key functions:**

- `Create(name, command, width, height)` - Create new session
- `CapturePane(name)` - Get terminal output
- `ListSessions()` - List all tmux sessions
- `KillSession(name)` - Delete session

---

## HTTP Router (chi)

The dashboard server uses [`go-chi/chi`](https://github.com/go-chi/chi) for route registration. Chi provides middleware groups and method routing that the stdlib `http.ServeMux` lacks.

### Why chi instead of stdlib ServeMux

- **Auth-by-default** — middleware applied to a group automatically covers every route in it. With `ServeMux`, every new route required manually wrapping with `withCORS(withAuthAndCSRF(...))`. Forgetting the wrapper silently left the route unprotected. With chi groups, forgetting is impossible — the group's middleware applies automatically.
- **Method routing** — `r.Get(...)`, `r.Post(...)`, `r.Delete(...)` eliminate manual `if r.Method != http.MethodPost` guards in handlers. Chi returns 405 Method Not Allowed automatically.
- **URL parameters** — `chi.URLParam(r, "id")` replaces the custom `extractPathSegment()` function.

### Route structure

```go
r := chi.NewRouter()

// Public routes (no auth)
r.HandleFunc("/auth/login", ...)
r.HandleFunc("/auth/callback", ...)

// WebSocket routes (inline auth + origin check)
r.HandleFunc("/ws/terminal/{id}", ...)
r.HandleFunc("/ws/dashboard", ...)

// API routes (auth-by-default)
r.Route("/api", func(r chi.Router) {
    r.Use(s.corsMiddleware)
    r.Use(s.authMiddleware)

    // Read-only endpoints (CORS + Auth only)
    r.Get("/healthz", ...)
    r.Get("/sessions", ...)

    // State-changing endpoints (CORS + Auth + CSRF)
    r.Group(func(r chi.Router) {
        r.Use(s.csrfMiddleware)
        r.Post("/spawn", ...)
        r.Route("/workspaces/{workspaceID}", func(r chi.Router) {
            r.Use(validateWorkspaceID)
            // ... workspace sub-routes
        })
    })
})
```

### Middleware

Three chi-compatible middleware functions (signature: `func(http.Handler) http.Handler`):

- `corsMiddleware` — CORS headers for API routes
- `authMiddleware` — cookie/session authentication
- `csrfMiddleware` — CSRF token validation for state-changing operations

WebSocket endpoints do inline auth (auth check must happen before the WebSocket upgrade).

### Gotchas

- **Route ordering**: chi uses first-match routing (not longest-prefix-match like `ServeMux`). More specific patterns must be registered before wildcards.
- **Trailing slashes**: chi does not auto-redirect `/api/sessions` to `/api/sessions/` like `ServeMux` does.
- **Adding a new API route**: write the handler, add `r.Post(...)` or `r.Get(...)` inside the appropriate group. Auth and CSRF are automatic from group membership.

---

## JSON Schema Generation

The oneshot system (one-shot LLM calls for branch suggestion, conflict resolution, nudgenik assessment) uses JSON schemas to constrain structured output from OpenAI-compatible APIs.

### Architecture decisions

- **Schemas generated from Go structs, not inline strings.** Previously, schemas were maintained as long inline JSON strings in `internal/oneshot/oneshot.go` that could drift from the corresponding Go struct definitions. Now schemas are generated at runtime from the structs using `swaggest/jsonschema-go`.
- **Why `swaggest/jsonschema-go` over alternatives:** OpenAI's `strict: true` structured outputs require `additionalProperties: false` on all objects and all fields in the `required` array. The more popular `invopop/jsonschema` treats `json:",omitempty"` as "optional" (excludes from required), which causes 400 errors with OpenAI. `swaggest/jsonschema-go` provides explicit `required:"true"` struct tags (opt-in).
- **Domain packages own their types.** Each package (nudgenik, branchsuggest, conflictresolve) registers its result struct in `init()` via `schema.Register()`. Schemas are generated on first access via `schema.Get()` and cached.

### Adding a new schema

1. Define the result struct in its domain package with `required:"true"` and `additionalProperties:"false"` tags.
2. Call `schema.Register("label", YourStruct{})` in an `init()` function.
3. Use `schema.Get("label")` in the oneshot call.

---

## Data Flow

### Spawning a Session

```
1. CLI request or Dashboard spawn
   |
2. Session manager validates workspace exists
   |
3. Workspace manager creates workspace (if new)
   |
4. Ensurer runs ForSpawn (hooks, git exclude)
   |
5. tmux package creates session with agent command
   |
6. Session manager tracks PID and status
   |
7. WebSocket streams terminal output to dashboard
```

### Terminal Streaming

```
1. tmux control mode streams %output events to SessionTracker
   |
2. Tracker fans out to subscriber channels + OutputLog (sequenced)
   |
3. WebSocket handler sends binary frames with sequence headers
   |
4. Browser decodes frames, detects gaps, writes to xterm.js
```

See `docs/terminal-pipeline.md` for the full pipeline reference.

---

## Configuration & State

### Config (`~/.schmux/config.json`)

User-editable configuration:

```json
{
  "workspace_path": "~/schmux-workspaces",
  "repos": [...],
  "run_targets": [...],
  "quick_launch": [...],
  "terminal": {
    "width": 120,
    "height": 40,
    "seed_lines": 100
  }
}
```

### State (`~/.schmux/state.json`)

Managed automatically by the daemon:

```json
{
  "workspaces": [...],
  "sessions": [...]
}
```

**Note:** Previews are ephemeral and not persisted. They are auto-detected from running tmux sessions on daemon startup and reconciled during runtime.

---

## Design Patterns

### Interfaces for Testability

Key packages define interfaces for testing:

```go
type Tmux interface {
    Create(name, command string, w, h uint) error
    CapturePane(name string) (string, error)
    // ...
}
```

### Error Handling

- Errors are returned, not panic'd
- Context-based cancellation for long operations
- Graceful degradation (e.g., if tmux isn't installed)

### Concurrency

- Sessions run independently (no coordination between agents)
- Per-workspace locking coordinates sync operations vs. git status refreshes (see `docs/workspaces.md`)
- Dashboard handlers are goroutine-safe
- All WebSocket writes go through `wsConn` wrapper with mutex

---

## Build & Deployment

### Development

```bash
go run ./cmd/schmux daemon-run
```

### Production

```bash
go build ./cmd/schmux
./schmux start
```

### Dashboard

```bash
# Build React assets
go run ./cmd/build-dashboard

# Dashboard served from embedded assets
# or assets/dashboard/dist/ in development
```

---

## See Also

- [React Architecture](react.md) — Frontend architecture and patterns
- [API Reference](../api.md) — HTTP API and WebSocket protocol
- [Testing Guide](testing.md) — Test conventions and running tests
- [Terminal Pipeline](terminal-pipeline.md) — Terminal streaming architecture
- [Workspaces](../workspaces.md) — Workspace management, locking, ensure system
- [Sessions](../sessions.md) — Session lifecycle and spawn modes
