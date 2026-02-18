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

# Run all tests (unit + E2E + scenarios) - RECOMMENDED
./test.sh --all

# Run tests with various options
./test.sh              # Unit tests only (default)
./test.sh --race       # Unit tests with race detector
./test.sh --coverage   # Unit tests with coverage report
./test.sh --e2e        # E2E tests only (requires Docker)
./test.sh --scenarios  # Scenario tests only (Playwright, requires Docker)
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
│                                                         │
│  Preview Manager (internal/preview/manager.go)          │
│  - Reverse proxy for workspace dev servers              │
│                                                         │
│  Remote Manager (internal/remote/manager.go)            │
│  - SSH connections to remote hosts via tmux control mode │
│                                                         │
│  GitHub Integration (internal/github/)                  │
│  - PR discovery, OAuth, repository info                 │
└─────────────────────────────────────────────────────────┘
```

**Key entry point**: `cmd/schmux/main.go` parses CLI commands and delegates to `internal/daemon/`.

## ⚠️ TypeScript Type Generation — Never Edit `.generated.ts` Files

API types shared between Go and TypeScript are defined in `internal/api/contracts/` and auto-generated into `assets/dashboard/src/lib/types.generated.ts`.

❌ **WRONG**: Edit `types.generated.ts` directly
✅ **RIGHT**: Edit Go structs in `internal/api/contracts/*.go`, then run `go run ./cmd/gen-types`

When to regenerate:

- Adding or modifying structs in `internal/api/contracts/`
- Changing JSON field names or `omitempty` tags
- Adding new API response types

Manual TypeScript types go in `assets/dashboard/src/lib/types.ts` (not the generated file).

## ⚠️ API Documentation — CI-Enforced

Changes to API-related packages (`internal/dashboard/`, `internal/config/`, `internal/state/`, `internal/workspace/`, `internal/session/`, `internal/tmux/`) **must** include a corresponding update to `docs/api.md`. CI runs `scripts/check-api-docs.sh` to enforce this.

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

Key technical patterns:

- **State via WebSocket, not polling**: `SessionsContext` receives real-time updates from `/ws/dashboard`. Do not add polling for session/workspace state.
- **Pending navigation**: After spawning a session, use the pending navigation system (not polling) to navigate once the session appears via WebSocket.
- **Two WebSocket endpoints**: `/ws/dashboard` (server→client session/workspace broadcasts) and `/ws/terminal/{id}` (bidirectional terminal I/O).
- **WebSocket write safety**: Always use the `wsConn` wrapper (which has a mutex) — gorilla WebSocket is not concurrent-safe for writes.
- **Tests**: Vitest + React Testing Library. 130+ tests in `assets/dashboard/src/`. Run via `./test.sh` (included in unit test suite).

Routes:

- `/` - Home (workspace list, session overview)
- `/tips` - Tips (tmux shortcuts, quick reference)
- `/spawn` - Spawn wizard (multi-step form)
- `/sessions/{id}` - Session detail with terminal
- `/preview/{workspaceId}/{previewId}` - Web preview iframe
- `/git/{workspaceId}` - Git commit graph
- `/resolve-conflict/{workspaceId}` - Linear sync conflict resolution
- `/config` - Settings editor
- `/settings/remote` - Remote flavor configuration
- `/ws/terminal/{id}` - WebSocket for live terminal output
- `/ws/dashboard` - WebSocket for real-time session/workspace updates

## Important Files

- [`docs/PHILOSOPHY.md`](docs/PHILOSOPHY.md) - Product philosophy (source of truth)
- [`docs/cli.md`](docs/cli.md) - CLI command reference
- [`docs/web.md`](docs/web.md) - Web dashboard UX
- [`docs/api.md`](docs/api.md) - Daemon HTTP API contract (client-agnostic)
- [`docs/dev/react.md`](docs/dev/react.md) - React architecture
- [`docs/dev/architecture.md`](docs/dev/architecture.md) - Backend architecture
- [`AGENTS.md`](AGENTS.md) - Architecture guidelines (for non-Claude agents)
