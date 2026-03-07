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
  - `internal/detect/` — tool/agent detection adapters; files follow the `adapter_<toolname>.go` naming convention (e.g., `adapter_claude.go`, `adapter_opencode.go`). Use `Glob` to list files before reading if the target filename is uncertain.
- `assets/dashboard/` — static web UI assets (HTML/CSS/TypeScript) served by the daemon.
- Docs: `README.md`, `docs/cli.md`, `docs/web.md`, `docs/api.md`, `docs/react.md`, `docs/architecture.md`, `docs/contributing.md`.

**Known large files**: `internal/config/config.go`, `internal/config/config_test.go`, and `assets/dashboard/src/styles/global.css` all exceed the 25,000-token read limit. Do not attempt to read any of them in full — use search/grep to find specific symbols, or read targeted sections using offset/limit parameters.

## Build, Test, and Development Commands

Prereqs: Go (see `go.mod`), `tmux`, `git`, and Docker (for E2E tests).

## ⚠️ E2E Tests — Use Docker Hook, NOT direct `go test`

**NEVER run E2E tests via direct `go test` invocation.**

E2E tests in this repo are Docker-gated and must be executed through the Docker runner so CI/local behavior stays aligned.

❌ **WRONG**: `go test ./internal/e2e/...`, `go test -tags=e2e ./...`
✅ **RIGHT**: `docker build -f Dockerfile.e2e -t schmux-e2e . && docker run --rm schmux-e2e`

## ⚠️ Frontend Tests — Use `./test.sh`, NOT `npx vitest` directly

**NEVER run frontend tests by `cd`-ing into `assets/dashboard/` and invoking `npx vitest run` or similar commands directly.**

Always run frontend tests from the repository root using the test wrapper:

❌ **WRONG**: `cd assets/dashboard && npx vitest run`
✅ **RIGHT**: `./test.sh --quick` (from repo root)

- `go build ./cmd/schmux` — build the runnable binary at `./schmux`.
- `go run ./cmd/gen-types` — generate TypeScript types from Go contracts.
- `go run ./cmd/build-dashboard` — build the React dashboard (installs npm deps, runs vite build).
- `./test.sh` — run all tests (unit + E2E + scenarios) - **recommended before commits**.
- `./test.sh --quick` — run fast tests only (backend + frontend, no Docker).
- `./test.sh --race` — run all tests with race detector.
- `./test.sh --coverage` — run all tests with coverage report.
- `./test.sh --e2e` — run E2E tests only (requires Docker).
- `./test.sh --scenarios` — run scenario tests only (Playwright, requires Docker).
- `./test.sh --help` — see all options.
- `go test ./...` — run Go unit tests directly (alternative to `./test.sh --quick`).
- `./schmux start` / `./schmux stop` / `./schmux status` — manage the daemon locally.

## Coding Style & Naming Conventions

- Go: keep changes `gofmt`-clean (`gofmt -w .` or `go fmt ./...`).
- Packages: lowercase, short, domain-based (`dashboard`, `workspace`, `session`).
- Identifiers: exported `CamelCase`, unexported `camelCase`; errors as `err`.
- Frontend assets live in `assets/dashboard/`; **build via `go run ./cmd/build-dashboard` only — never npm directly**; keep HTML/CSS/TypeScript minimal and consistent with `docs/react.md`.
- Always run all commands (`git`, `./test.sh`, `./format.sh`, `go build`, `go run`, etc.) from the **repository root**, not from subdirectories.

## Testing Guidelines

- Framework: standard Go `testing` package (`*_test.go`, `TestXxx` naming).
- Prefer table-driven tests for parsing/state transitions.
- When changing daemon/dashboard behavior, add/adjust tests in the nearest `internal/<pkg>/` package.

## Pre-Commit Requirements

**ALWAYS use `/commit` to create commits. NEVER run `git commit` directly.**

The `/commit` command enforces the definition of done:

- Runs `./test.sh` and aborts if tests fail
- Checks that `docs/api.md` is updated when API-related packages change
- Requires a structured self-assessment (tests written, no architecture drift, docs current)

Before invoking `/commit`, run `./format.sh` to format all staged files.

**`./format.sh` exit code 2 is normal** — it means no staged files required changes. Treat exit codes 0 and 2 as success; only exit code 1 indicates a real formatting error.

❌ **WRONG**: `git commit -m "message"`
✅ **RIGHT**: `/commit`

## Commit & Pull Request Guidelines

- Commits: short, imperative subject lines (e.g., “Implement v0.5 spec”, “Polish README”); keep unrelated changes split.
- PRs: describe **what** changed and **why**, link to relevant docs when applicable, and list manual verification steps.
- UI changes: include screenshots or a short screen recording of the dashboard views you touched.

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

## React Dashboard Architecture

For React changes, consult `docs/react.md` first — it documents architectural decisions and anti-patterns.

Key patterns:

- **State via WebSocket, not polling**: `SessionsContext` receives real-time updates from `/ws/dashboard`. Do not add polling for session/workspace state.
- **Pending navigation**: After spawning a session, use the pending navigation system (not polling) to navigate once the session appears via WebSocket.
- **Two WebSocket endpoints**: `/ws/dashboard` (server→client session/workspace broadcasts) and `/ws/terminal/{id}` (bidirectional terminal I/O).
- **WebSocket write safety**: Always use the `wsConn` wrapper (which has a mutex) — gorilla WebSocket is not concurrent-safe for writes.
- **Tests**: Vitest + React Testing Library. 130+ tests in `assets/dashboard/src/`. Run via `./test.sh --quick` (included in unit test suite).
- **Vite chunks**: vendor (react, react-dom, react-router-dom) and xterm (@xterm/\*) are split into separate chunks in `vite.config.js`.

## Dev Mode (`./dev.sh`)

Hot-reload development mode:

- Starts Vite dev server (port 5173) + Go daemon with `--dev-mode` flag
- Backend proxies non-API routes to Vite for HMR
- Exit code 42 means "restart requested" (from dashboard UI) — not a crash
- State files: `~/.schmux/dev-state.json`, `~/.schmux/dev-restart.json`, `~/.schmux/dev-build-status.json`
- Auto-installs Go, Node, tmux via Homebrew if missing

## Configuration & Safety Notes

- Local config/state are user-scoped: `~/.schmux/config.json` and `~/.schmux/state.json`; never commit secrets.
- Local dev artifacts are ignored via `.gitignore` (notably `.schmux/` and the `schmux` binary).

## Operational Notes

- **tmux capture-pane timing**: When probing tmux pane output after running a short-lived command, keep the pane alive (e.g., with `; read` or a long-running tail command) before the process exits — `capture-pane` cannot read output from a pane whose window has already closed.

## Lore Capture

As you work, append discoveries to `.schmux/lore.jsonl` — things you learned
that aren't already documented in this file. One JSON line per entry:

```
{"ts":"<ISO8601>","ws":"<workspace-id>","agent":"<agent-name>","type":"operational|codebase","text":"<what you learned>"}
```

Don't evaluate importance. Don't read the file first. Just append.

In zsh, use `printf` (not `echo`) when appending JSONL entries to avoid quote-mismatch errors with special characters in JSON:

```bash
printf '%s\n' '{"ts":"...","ws":"...","agent":"...","type":"operational","text":"..."}' >> .schmux/lore.jsonl
```
