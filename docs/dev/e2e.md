# End-to-End Testing Plan (Docker + GitHub Actions)

This document defines the agreed end-to-end (E2E) testing approach for schmux.

---

## Goals

- Full-loop validation: CLI → daemon → tmux → daemon HTTP API.
- Enforced session naming conventions with two concurrent sessions:
  - Nickname uniqueness
  - Consistent naming across CLI output, tmux session name, and API responses
- Safe execution without touching user config/state on the host.
- Reproducible in CI (GitHub Actions) using Docker.

---

## Non-Goals (v1)

- Websocket coverage (deferred to a later phase)
- Browser-based UI automation (not required in v1)
- External remote git repos (use local temp repo fixtures)

---

## High-Level Design

### Execution Environment

- All E2E runs happen in Docker.
- The dashboard UI is built in the container so the daemon serves real assets.

### Test Harness Location

- Preferred: `internal/e2e` Go test package for integration with `go test ./...`.
- Alternative: `cmd/schmux-e2e` runner (acceptable if simpler).

### Isolation Model

**Docker container provides all isolation.** No HOME overrides or env vars needed.

- Container's `~/.schmux/` is isolated from host and other containers
- Container's port 7337 is isolated
- Container's tmux server is isolated
- Tests use standard `~/.schmux/` paths (which resolve to container paths)
- Container is ephemeral - deleted after test completes

---

## How to Run

### Locally (Required before pushing)

E2E tests MUST be run locally via Docker before pushing changes:

```bash
# Build the E2E Docker image
docker build -f Dockerfile.e2e -t schmux-e2e .

# Run the E2E tests
docker run --rm schmux-e2e
```

The Docker image includes:

- Go compiler and test tools
- tmux, git, curl, bash
- Node.js + npm for dashboard build
- Pre-built schmux binary and dashboard assets

Tests run inside the container with full isolation from your host environment.

---

## Required Behaviors to Validate

1. **Daemon lifecycle**
   - `schmux start` succeeds.
   - Health/status endpoint responds.

2. **Workspace creation**
   - CLI can create/init a workspace using a local temp git repo.
   - State reflects workspace entry.

3. **Two sessions with naming guarantees**
   - Start two sessions and confirm names are unique.
   - CLI list output contains exact names.
   - `tmux ls` shows the exact names.
   - API list endpoint includes both names.

4. **Session teardown**
   - CLI stop removes both sessions.
   - `tmux ls` no longer shows them.
   - API list returns empty.

5. **Daemon shutdown**
   - `schmux stop` succeeds.
   - Health endpoint no longer reachable.

---

## Artifacts on Failure

When any E2E test fails, capture and persist:

- Daemon logs
- `config.json` and `state.json` from `~/.schmux/`
- `tmux ls` output
- API response dumps (health, sessions list)

---

## Docker Image Requirements

- Go (per `go.mod`)
- Node + npm (for dashboard build)
- tmux
- git
- curl
- bash

### Dashboard Build

- Run `go run ./cmd/build-dashboard` as part of the E2E run or image build.
- Ensure daemon serves the built assets for realistic API/HTML coverage.

---

## GitHub Actions Plan

- Build E2E Docker image.
- Run E2E tests inside the container:
  - `go test ./internal/e2e -v` (or equivalent runner)
- Collect artifacts on failure (logs/state/tmux output).

---

## Implementation Status

### Completed

- ✓ Docker-based E2E execution
- ✓ Daemon lifecycle test (start/stop/health endpoint)
- ✓ Spawn handler bug fix (repo name → repo URL lookup)

### Current Status

- **Passing**: TestE2EDaemonLifecycle
- **Failing**: TestE2EFullLifecycle and TestE2ETwoSessionsNaming due to pipe-pane issue

### Known Limitation

**Pipe-pane fails in Docker container**: The spawn succeeds (session is created), but pipe-pane fails with "no server running on /tmp/tmUX-1000/default". This appears to be a Docker environment limitation where tmux socket resolution behaves differently when run from the daemon vs directly from bash.

Works locally: Running `tmux new-session -d -s test && tmux pipe-pane -t =test:0.0 'cat >> /tmp/test.log'` works fine in the container.

### Implementation Phases

### Phase 1 (v1) - IN PROGRESS

- ✓ Docker-based E2E execution
- ✓ Daemon lifecycle validation
- ✗ Session spawning with pipe-pane (blocked by Docker/tmux issue)
- ✗ Naming convention enforcement with two sessions (blocked by pipe-pane issue)
- ✓ UI build included (no browser automation)

### Phase 2 (later)

- Websocket coverage (session event stream)
- Optional dashboard UI smoke tests

---

## Open Questions (tracked)

- Which API endpoints are stable for health/session listing?
  - `/api/healthz` - confirmed stable
  - `/api/sessions` (GET) - confirmed stable
