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
├── daemon/              # Background process, main loop
├── dashboard/           # HTTP server + handlers + websockets
├── session/             # Session lifecycle and tracking
├── workspace/           # Repo clone/checkout + overlays
├── tmux/                # tmux integration
├── config/              # Config IO (~/.schmux/config.json)
├── state/               # State IO (~/.schmux/state.json)
└── detect/              # Tool detection (claude, codex, etc.)

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

- `server.go` - HTTP server setup, route registration
- `handlers.go` - API endpoint handlers
- `terminal.go` - WebSocket terminal streaming

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

### tmux (`internal/tmux/`)

tmux CLI wrapper.

**Key functions:**

- `Create(name, command, width, height)` - Create new session
- `CapturePane(name)` - Get terminal output
- `ListSessions()` - List all tmux sessions
- `KillSession(name)` - Delete session

---

## Data Flow

### Spawning a Session

```
1. CLI request or Dashboard spawn
   ↓
2. Session manager validates workspace exists
   ↓
3. Workspace manager creates workspace (if new)
   ↓
4. tmux package creates session with agent command
   ↓
5. Session manager tracks PID and status
   ↓
6. WebSocket streams terminal output to dashboard
```

### Terminal Streaming

```
1. Dashboard starts WebSocket connection
   ↓
2. tmux.CapturePane() gets terminal output
   ↓
3. Output sent via WebSocket to frontend
   ↓
4. xterm.js renders output in browser
```

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
- Terminal capture polls at fixed interval
- Dashboard handlers are goroutine-safe

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
- [API Reference](api.md) — HTTP API and WebSocket protocol
- [Testing Guide](testing.md) — Test conventions and running tests
