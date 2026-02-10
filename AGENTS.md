# Repository Guidelines

## Project Structure & Module Organization

- `cmd/schmux/` — CLI entry point (`main.go`) and user-facing commands.
- `internal/` — application packages (not imported externally):
  - `internal/daemon/` — long-running background process.
  - `internal/dashboard/` — HTTP server + handlers + websockets.
  - `internal/session/` — session lifecycle and tracking.
  - `internal/workspace/` — repo clone/checkout management.
  - `internal/tmux/` — tmux integration and process inspection.
  - `internal/config/`, `internal/state/` — config/state IO.
- `assets/dashboard/` — static web UI assets (HTML/CSS/TypeScript) served by the daemon.
- Docs: `README.md`, `docs/cli.md`, `docs/web.md`, `docs/api.md`, `docs/dev/react.md`, `docs/dev/architecture.md`, `docs/dev/README.md`.

## Build, Test, and Development Commands

Prereqs: Go (see `go.mod`), `tmux`, `git`, and Docker (for E2E tests).

## ⚠️ E2E Tests — Use Docker Hook, NOT direct `go test`

**NEVER run E2E tests via direct `go test` invocation.**

E2E tests in this repo are Docker-gated and must be executed through the Docker runner so CI/local behavior stays aligned.

❌ **WRONG**: `go test ./internal/e2e/...`, `go test -tags=e2e ./...`
✅ **RIGHT**: `docker build -f Dockerfile.e2e -t schmux-e2e . && docker run --rm schmux-e2e`

- `go build ./cmd/schmux` — build the runnable binary at `./schmux`.
- `go run ./cmd/gen-types` — generate TypeScript types from Go contracts.
- `go run ./cmd/build-dashboard` — build the React dashboard (installs npm deps, runs vite build).
- `./test.sh --all` — run all tests (unit + E2E) - **recommended before commits**.
- `./test.sh` — run unit tests only (default).
- `./test.sh --race` — run unit tests with race detector.
- `./test.sh --coverage` — run unit tests with coverage report.
- `./test.sh --e2e` — run E2E tests only (requires Docker).
- `./test.sh --help` — see all options.
- `go test ./...` — run unit tests directly (alternative to test.sh).
- `./schmux start` / `./schmux stop` / `./schmux status` — manage the daemon locally.

## Coding Style & Naming Conventions

- Go: keep changes `gofmt`-clean (`gofmt -w .` or `go fmt ./...`).
- Packages: lowercase, short, domain-based (`dashboard`, `workspace`, `session`).
- Identifiers: exported `CamelCase`, unexported `camelCase`; errors as `err`.
- Frontend assets live in `assets/dashboard/`; **build via `go run ./cmd/build-dashboard` only — never npm directly**; keep HTML/CSS/TypeScript minimal and consistent with `docs/dev/react.md`.

## Testing Guidelines

- Framework: standard Go `testing` package (`*_test.go`, `TestXxx` naming).
- Prefer table-driven tests for parsing/state transitions.
- When changing daemon/dashboard behavior, add/adjust tests in the nearest `internal/<pkg>/` package.

## Pre-Commit Requirements

Before committing changes, you MUST run:

1. **Run all tests**: `./test.sh --all`
2. **Format code**: `go fmt ./...`

The test script runs both unit tests and E2E tests. This catches issues like Dockerfile/go.mod version mismatches before they reach CI.

For faster iteration during development:

- Run unit tests only: `./test.sh` (or `go test ./...`)
- Skip E2E tests and let CI handle them on PRs

## Commit & Pull Request Guidelines

- Commits: short, imperative subject lines (e.g., “Implement v0.5 spec”, “Polish README”); keep unrelated changes split.
- PRs: describe **what** changed and **why**, link to relevant docs when applicable, and list manual verification steps.
- UI changes: include screenshots or a short screen recording of the dashboard views you touched.

## Configuration & Safety Notes

- Local config/state are user-scoped: `~/.schmux/config.json` and `~/.schmux/state.json`; never commit secrets.
- Local dev artifacts are ignored via `.gitignore` (notably `.schmux/` and the `schmux` binary).
