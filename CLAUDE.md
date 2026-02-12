# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

**schmux** is a multi-agent AI orchestration system that runs multiple AI coding agents (Claude Code, Codex, etc.) simultaneously across tmux sessions, each in isolated workspace directories. A web dashboard provides real-time monitoring and management.

## ⚠️ React Dashboard Builds — Use Go Wrapper, NOT npm

**NEVER run `npm install`, `npm run build`, or `vite build` directly.**

The React dashboard MUST be built via `go run ./cmd/build-dashboard`. This Go wrapper:

- Installs npm deps correctly
- Runs vite build with proper environment
- Outputs to `assets/dashboard/dist/` which gets embedded in the Go binary

❌ **WRONG**: `cd assets/dashboard && npm install && npm run build`
✅ **RIGHT**: `go run ./cmd/build-dashboard`

## Hot-Reload Development Mode

For active development with automatic rebuilding:

```bash
./dev.sh
```

This runs the Go backend and React frontend (via Vite) with workspace switching support:

- **Go changes**: Trigger rebuild from the dashboard Dev Mode panel
- **React changes**: Instant browser update via HMR (<100ms)
- **Workspace switching**: Switch which worktree's code is running from the dashboard UI
- **Access**: http://localhost:7337 (same URL as production)
- **Stop**: Ctrl+C

The dev mode panel in the sidebar lets you switch between workspaces:

- **FE**: Restart Vite pointed at a different worktree's `assets/dashboard/`
- **BE**: Rebuild Go binary from a different worktree, restart daemon
- **Both**: Both frontend and backend switch

Build failures are safe — the old binary keeps running if the new build fails.

First run installs npm dependencies if missing.

## Build, Test, and Run Commands

```bash
# Hot-reload development (recommended for active development)
./dev.sh

# Build the binary (outputs ./schmux)
go build ./cmd/schmux

# Generate TypeScript types from Go contracts:
go run ./cmd/gen-types

# Build the React dashboard (see warning above)
go run ./cmd/build-dashboard

# Run all tests (unit + E2E) - RECOMMENDED
./test.sh --all

# Run tests with various options
./test.sh              # Unit tests only (default)
./test.sh --race       # Unit tests with race detector
./test.sh --coverage   # Unit tests with coverage report
./test.sh --e2e        # E2E tests only (requires Docker)
./test.sh --help       # See all options

# Or run tests directly with go
go test ./...          # Unit tests only
go test -race ./...    # Unit tests with race detector
go test -cover ./...   # Unit tests with coverage

# Build and run E2E tests manually
docker build -f Dockerfile.e2e -t schmux-e2e .
docker run --rm schmux-e2e

# Daemon management (requires config at ~/.schmux/config.json)
./schmux start      # Start daemon in background
./schmux stop       # Stop daemon
./schmux status     # Show status + dashboard URL
./schmux daemon-run # Run daemon in foreground (debug)
```

## Pre-Commit Requirements

Before committing changes, you MUST run:

1. **Run all tests**: `./test.sh --all`
2. **Format code**: `./format.sh` (or let the pre-commit hook handle it automatically)

The pre-commit hook automatically formats staged Go, TypeScript, JavaScript, CSS, Markdown, and JSON files. Running `./format.sh` auto-installs the hook if missing.

The test script runs both unit tests and E2E tests. This catches issues like Dockerfile/go.mod version mismatches before they reach CI.

For faster iteration during development:

- Run unit tests only: `./test.sh` (or `go test ./...`)
- Skip E2E tests and let CI handle them on PRs

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
│  Workspace Manager (internal/workspace/manager.go)      │
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

- `/` - Tips (tmux shortcuts, quick reference)
- `/spawn` - Spawn wizard (multi-step form)
- `/sessions/{id}` - Session detail with terminal
- `/ws/terminal/{id}` - WebSocket for live terminal output

## Important Files

- [`docs/PHILOSOPHY.md`](docs/PHILOSOPHY.md) - Product philosophy (source of truth)
- [`docs/cli.md`](docs/cli.md) - CLI command reference
- [`docs/web.md`](docs/web.md) - Web dashboard UX
- [`docs/api.md`](docs/api.md) - Daemon HTTP API contract (client-agnostic)
- [`docs/dev/react.md`](docs/dev/react.md) - React architecture
- [`docs/dev/architecture.md`](docs/dev/architecture.md) - Backend architecture
- [`AGENTS.md`](AGENTS.md) - Architecture guidelines (for non-Claude agents)
