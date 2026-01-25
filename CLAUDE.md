# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

**schmux** is a multi-agent AI orchestration system that runs multiple AI coding agents (Claude Code, Codex, etc.) simultaneously across tmux sessions, each in isolated workspace directories. A web dashboard provides real-time monitoring and management.

## Build, Test, and Run Commands

```bash
# Build the binary (outputs ./schmux)
go build ./cmd/schmux

# Generate TypeScript types from Go contracts**:
go run ./cmd/gen-types

# Build the React dashboard (installs npm deps, runs vite build)
go run ./cmd/build-dashboard

# Run all tests
go test ./...

# Run tests with coverage
go test -cover ./...

# Build E2E Docker image
docker build -f Dockerfile.e2e -t schmux-e2e .

# Run E2E tests
docker run --rm schmux-e2e

# Daemon management (requires config at ~/.schmux/config.json)
./schmux start      # Start daemon in background
./schmux stop       # Stop daemon
./schmux status     # Show status + dashboard URL
./schmux daemon-run # Run daemon in foreground (debug)
```

## Pre-Commit Requirements

Before committing changes, you MUST run:

1. **Run unit tests**: `go test ./...`
2. **Run E2E tests**: `docker build -f Dockerfile.e2e -t schmux-e2e . && docker run --rm schmux-e2e`
3. **Format code**: `go fmt ./...`

This catches issues like Dockerfile/go.mod version mismatches before they reach CI.

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

See `docs/dev/react.md` for React architecture and `docs/web.md` for UX patterns. For API contracts, see `docs/api.md`. Key principles:

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

- `docs/PHILOSOPHY.md` - Product philosophy (source of truth)
- `docs/cli.md` - CLI command reference
- `docs/web.md` - Web dashboard UX
- `docs/api.md` - Daemon HTTP API contract (client-agnostic)
- `docs/dev/react.md` - React architecture
- `docs/dev/architecture.md` - Backend architecture
- `AGENTS.md` - Architecture guidelines (for non-Claude agents)
