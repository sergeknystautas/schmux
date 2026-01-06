# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

**Schmux** is a multi-agent AI orchestration system that runs multiple AI coding agents (Claude Code, Codex, etc.) simultaneously across tmux sessions, each in isolated workspace directories. A web dashboard provides real-time monitoring and management.

## Build, Test, and Run Commands

```bash
# Build the binary (outputs ./schmux)
go build ./cmd/schmux

# Run all tests
go test ./...

# Run tests with coverage
go test -cover ./...

# Daemon management (requires config at ~/.schmux/config.json)
./schmux start      # Start daemon in background
./schmux stop       # Stop daemon
./schmux status     # Show status + dashboard URL
./schmux daemon-run # Run daemon in foreground (debug)
```

## Code Architecture

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
│  Workspace Manager (internal/workspace/manager.go)       │
│  - Clone/checkout git repos                             │
│  - Track workspace directories                          │
│                                                         │
│  tmux Package (internal/tmux/tmux.go)                   │
│  - tmux CLI wrapper (create, capture, list, kill)       │
│                                                         │
│  Config/State (internal/config/, internal/state/)       │
│  - ~/.schmux/config.json  (repos, agents, workspace)    │
│  - ~/.schmux/state.json    (workspaces, sessions)       │
└─────────────────────────────────────────────────────────┘
```

**Key entry point**: `cmd/schmux/main.go` parses CLI commands and delegates to `internal/daemon/`.

## Code Conventions

- Go: keep changes `gofmt`-clean (`go fmt ./...`)
- Packages: lowercase, domain-based (`dashboard`, `workspace`, `session`)
- Exported identifiers `CamelCase`, unexported `camelCase`
- Errors as `err` variable
- Tests: standard Go `testing` package with `TestXxx` naming; prefer table-driven tests

## Web Dashboard Guidelines

See `WEB-UX.md` for complete UI/UX patterns. Key principles:

- **CLI-first**: web dashboard is for observability/orchestration
- **Status-first**: running/stopped/error visually consistent everywhere
- **Destructive actions slow**: "Dispose" always requires confirmation
- **URLs idempotent**: routes bookmarkable, survive reload
- **Real-time updates**: connection indicator, preserve scroll position

Routes:
- `/` - Sessions list (default landing)
- `/spawn` - Spawn wizard (multi-step form)
- `/sessions/{id}` - Session detail with terminal
- `/ws/terminal/{id}` - WebSocket for live terminal output

## Important Files

- `SPEC.md` - Feature specification
- `WEB-UX.md` - Dashboard UI/UX patterns, component system
- `AGENTS.md` - Architecture guidelines and conventions
