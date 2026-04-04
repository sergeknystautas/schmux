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

## ⚠️ Frontend Tests — Use `./test.sh`, NOT `npx vitest` directly

**NEVER run frontend tests by `cd`-ing into `assets/dashboard/` and invoking `npx vitest run` or similar commands directly.**

Frontend tests are already included in `./test.sh --quick`. Running vitest from the subdirectory bypasses the project test wrapper and produces unreliable results.

❌ **WRONG**: `cd assets/dashboard && npx vitest run`
✅ **RIGHT**: `./test.sh --quick` (from repository root)

## Hot-Reload Development Mode

Run `./dev.sh` for active development with Go backend + Vite HMR. See [`docs/dev-mode.md`](docs/dev-mode.md) for details.

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
./test.sh              # All tests (same as --all)
./test.sh --quick      # Fast tests only (backend + frontend, no Docker)
./test.sh --race       # All tests with race detector
./test.sh --coverage   # All tests with coverage report
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

**ALWAYS use `/commit` to create commits. NEVER run `git commit` directly.**

The `/commit` command enforces the definition of done before every commit:

- Runs `./test.sh` and aborts if tests fail
- Checks that `docs/api.md` is updated when API-related packages change
- Requires a structured self-assessment (tests written, no architecture drift, docs current)

**When to skip tests**: If the commit contains no code changes (only `.md`, `.claude/skills/`, or other non-code files), or if `./test.sh` was already run in this conversation after the last code change, tests do not need to be re-run.

Before the `/commit` command runs, ensure:

1. **Format code**: `./format.sh` (or let the pre-commit hook handle it automatically)

The pre-commit hook automatically formats staged Go, TypeScript, JavaScript, CSS, Markdown, and JSON files. Running `./format.sh` auto-installs the hook if missing.

**`./format.sh` exit code 2 is normal** — it means no staged files required changes. Treat exit codes 0 and 2 as success; only exit code 1 indicates a real formatting error.

For faster iteration during development:

- Run unit tests only: `./test.sh --quick` (or `go test ./...`)
- Skip E2E/scenario tests and let CI handle them on PRs

## ⚠️ `./test.sh --quick` Is NOT a Substitute for `./test.sh`

**`--quick` skips typecheck and other critical validation.** Code that passes `--quick` can still be broken. Do not use `--quick` to declare work complete or to satisfy the definition of done.

The pre-commit requirement says `./test.sh`. That means `./test.sh` — not `./test.sh --quick`, not `go test ./...`, not vitest. Run exactly what you are told to run. If `./test.sh` is specified, run `./test.sh`. Do not substitute a faster alternative and assume it's equivalent. It isn't, and skipping checks is how broken code gets committed.

## Code Architecture

See [`docs/architecture.md`](docs/architecture.md) for the full backend architecture. Key entry point: `cmd/schmux/main.go` → `internal/daemon/`.

**Known large files**: `internal/config/config.go`, `internal/config/config_test.go`, and `assets/dashboard/src/styles/global.css` all exceed the 25,000-token read limit. Do not attempt to read any of them in full — use `Grep` to search for specific symbols, or read targeted sections with `offset`/`limit` parameters.

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

Changes to API-related packages (`internal/dashboard/`, `internal/nudgenik/`, `internal/config/`, `internal/state/`, `internal/workspace/`, `internal/session/`, `internal/tmux/`) **must** include a corresponding update to `docs/api.md`. CI runs `scripts/check-api-docs.sh` to enforce this.

## Code Conventions

- Go: keep changes `gofmt`-clean (`go fmt ./...`)
- Packages: lowercase, domain-based (`dashboard`, `workspace`, `session`)
- Exported identifiers `CamelCase`, unexported `camelCase`
- Errors as `err` variable
- Tests: standard Go `testing` package with `TestXxx` naming; prefer table-driven tests
- Always run all commands (`git`, `./test.sh`, `./format.sh`, `go build`, `go run`, etc.) from the **repository root**, not from subdirectories like `assets/dashboard/`

## Web Dashboard Guidelines

See [`docs/web.md`](docs/web.md) for UX patterns, [`docs/react.md`](docs/react.md) for React architecture, and [`docs/api.md`](docs/api.md) for API contracts. Routes are defined in `assets/dashboard/src/App.tsx`.

Key guardrails:

- **State via WebSocket, not polling**: `SessionsContext` receives real-time updates from `/ws/dashboard`. Do not add polling for session/workspace state.
- **Pending navigation**: After spawning a session, use the pending navigation system (not polling) to navigate once the session appears via WebSocket.
- **WebSocket write safety**: Always use the `wsConn` wrapper (which has a mutex) — gorilla WebSocket is not concurrent-safe for writes.

## Documentation Conventions

Design artifacts live in three directories with a defined lifecycle:

- **`docs/specs/`** — Design specs for features not yet fully implemented
- **`docs/plans/`** — Step-by-step implementation plans (temporary, deleted when done)
- **`docs/reviews/`** — Review artifacts (design reviews, architecture audits)

Lifecycle: spec → plan → implement (delete plan) → finalize spec into `docs/*.md` guide (delete spec). See the README in each directory for details.

## Important Files

- [`docs/PHILOSOPHY.md`](docs/PHILOSOPHY.md) - Product philosophy (source of truth)
- [`docs/cli.md`](docs/cli.md) - CLI command reference
- [`docs/web.md`](docs/web.md) - Web dashboard UX
- [`docs/api.md`](docs/api.md) - Daemon HTTP API contract (client-agnostic)
- [`docs/react.md`](docs/react.md) - React architecture
- [`docs/architecture.md`](docs/architecture.md) - Backend architecture
- [`docs/dev-mode.md`](docs/dev-mode.md) - Dev mode (hot-reload development)
- [`AGENTS.md`](AGENTS.md) - Symlink to CLAUDE.md (for non-Claude agents)
