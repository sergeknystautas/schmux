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

Prereqs: Go (see `go.mod`), `tmux`, and `git`.

- `go run ./cmd/build-dashboard` — build the React dashboard (installs npm deps, runs vite build).
- `go build ./cmd/schmux` — build the runnable binary at `./schmux`.
- `go test ./...` — run all unit tests.
- `go test -v ./...` — verbose test output.
- `go test -cover ./...` — quick coverage signal.
- `./schmux start` / `./schmux stop` / `./schmux status` — manage the daemon locally.

## Coding Style & Naming Conventions

- Go: keep changes `gofmt`-clean (`gofmt -w .` or `go fmt ./...`).
- Packages: lowercase, short, domain-based (`dashboard`, `workspace`, `session`).
- Identifiers: exported `CamelCase`, unexported `camelCase`; errors as `err`.
- Frontend assets live in `assets/dashboard/`; keep HTML/CSS/TypeScript minimal and consistent with `docs/dev/react.md`.

## Testing Guidelines

- Framework: standard Go `testing` package (`*_test.go`, `TestXxx` naming).
- Prefer table-driven tests for parsing/state transitions.
- When changing daemon/dashboard behavior, add/adjust tests in the nearest `internal/<pkg>/` package and run `go test ./...`.

## Commit & Pull Request Guidelines

- Commits: short, imperative subject lines (e.g., “Implement v0.5 spec”, “Polish README”); keep unrelated changes split.
- PRs: describe **what** changed and **why**, link to relevant docs when applicable, and list manual verification steps.
- UI changes: include screenshots or a short screen recording of the dashboard views you touched.

## Configuration & Safety Notes

- Local config/state are user-scoped: `~/.schmux/config.json` and `~/.schmux/state.json`; never commit secrets.
- Local dev artifacts are ignored via `.gitignore` (notably `.schmux/` and the `schmux` binary).
