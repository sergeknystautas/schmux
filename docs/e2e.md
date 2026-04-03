# End-to-End Testing

This document describes the E2E testing infrastructure for schmux.

---

## Goals

- Full-loop validation: daemon → tmux → HTTP API → WebSocket.
- Session lifecycle: spawn, capture, nudge, rename, dispose.
- Workspace operations: clone, inspect, git amend/uncommit, branches.
- Remote sessions: mock connection, SSH smoke, hooks provisioning.
- Safe execution without touching user config/state on the host.
- Reproducible in CI (GitHub Actions) using Docker.

---

## Test Suites

### Go E2E Tests (`internal/e2e/`)

Integration tests that exercise the daemon HTTP API and WebSocket endpoints inside a Docker container. Each test gets an isolated HOME directory and ephemeral daemon port via the `Env` helper, so tests run concurrently with `t.Parallel()`.

**Coverage areas:**

- Daemon lifecycle (start/stop/health)
- Session spawn, capture, dispose, nickname rename
- Workspace inspect, branches, git amend/uncommit, scan
- File-based signaling (event files → nudge state via dashboard WebSocket)
- Dashboard WebSocket (real-time session/workspace broadcasts)
- Terminal WebSocket (bidirectional I/O, tell messages)
- Remote sessions (mock connection, multi-session, state persistence, hooks provisioning)
- Remote SSH smoke test (real SSH to localhost)
- Overlay compounding

### Playwright Scenario Tests (`test/scenarios/`)

Browser-based tests using Playwright that validate dashboard UI flows end-to-end. These run against a live daemon with the built dashboard.

**Coverage areas:**

- Spawn wizard flows (single session, multiple agents, quick launch)
- Session management (dispose, edit nickname)
- Git operations (diff, stage, discard)
- Terminal streaming and fidelity
- Config pages (lore, remote access, repository)
- Conflict resolution UI
- Remote access onboarding and authentication

---

## Docker Setup

All E2E runs happen in Docker. The container includes:

- Go compiler and test tools
- tmux, git, curl, bash
- Node.js + npm for dashboard build
- Pre-built schmux binary and dashboard assets
- SSH server (for remote SSH smoke test)

Docker provides all isolation — no HOME overrides or env vars needed.

---

## How to Run

### Full test suite (recommended)

```bash
./test.sh --all       # Unit + E2E + scenario tests
./test.sh --e2e       # E2E tests only
./test.sh --scenarios # Scenario tests only (Playwright)
```

### Docker manually

```bash
# Build the E2E Docker image
docker build -f Dockerfile.e2e -t schmux-e2e .

# Run E2E tests
docker run --rm schmux-e2e
```

### Go E2E tests directly (inside Docker)

```bash
go test -tags e2e -v -timeout 300s ./internal/e2e/...
```

---

## Test Helpers

### Go (`internal/e2e/e2e.go`)

The `Env` struct provides isolated test environments:

- `New(t)` — creates an isolated HOME, finds the schmux binary
- `DaemonStart()` / `DaemonStop()` — manages the daemon lifecycle
- `SpawnSession()` / `DisposeSession()` — session management
- `WaitForDashboardSession()` / `WaitForSessionNudgeState()` — polling helpers for session state
- `ConnectDashboardWebSocket()` / `ConnectTerminalWebSocket()` — WebSocket helpers
- `CaptureArtifacts()` — dumps daemon logs, config.json, state.json, tmux ls, and API sessions on failure

### Playwright (`test/scenarios/generated/helpers.ts`)

Shared helpers for scenario tests:

- `seedConfig()` — seeds daemon config via API
- `getConfig()` / `resetConfig()` — save and restore config between specs
- `spawnSession()` / `disposeSession()` / `disposeAllSessions()` — session helpers
- `waitForHealthy()` / `waitForSessionRunning()` — polling helpers
- `waitForTerminalOutput()` — WebSocket-based terminal output assertion

---

## Artifacts on Failure

When any E2E test fails, the `CaptureArtifacts()` method persists:

- Daemon logs (stderr)
- `config.json` and `state.json` from `~/.schmux/`
- `tmux ls` output
- API response dumps (health, sessions list)

---

## GitHub Actions

- Builds the E2E Docker image
- Runs Go E2E tests inside the container
- Runs Playwright scenario tests
- Collects artifacts on failure
