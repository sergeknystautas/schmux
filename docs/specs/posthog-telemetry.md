# PostHog Telemetry Integration

## Overview

Add anonymous usage telemetry to schmux via PostHog to understand how the tool is being used. Telemetry is **enabled by default** with opt-out available.

## Events to Track

| Event               | Trigger Location             | Properties                           |
| ------------------- | ---------------------------- | ------------------------------------ |
| `daemon_started`    | Daemon startup               | version                              |
| `workspace_created` | All workspace creation paths | workspace_id, repo_host, branch      |
| `session_created`   | All session spawn paths      | session_id, workspace_id, target     |
| `push_to_main`      | `LinearSyncToDefault()`      | workspace_id, branch, default_branch |

### Privacy Allowlist

Only these properties are sent. No repo names, URLs, paths, or user data.

| Property         | Source                  | Example                  |
| ---------------- | ----------------------- | ------------------------ |
| `version`        | Binary version          | `1.2.3`                  |
| `workspace_id`   | Workspace.ID            | `myproject-001`          |
| `session_id`     | Session.ID              | `myproject-001-a1b2c3d4` |
| `repo_host`      | Extracted from repo URL | `github.com`             |
| `branch`         | Workspace.Branch        | `feature/xyz`            |
| `target`         | Session target/agent    | `claude`                 |
| `default_branch` | Git default branch      | `main`                   |

## Configuration

### API Key Resolution (priority order)

1. `~/.schmux/secrets.json` → `posthog_api_key` (user override)
2. Embedded in binary at build time (default)

The embedded key is injected via GitHub Actions secrets during build.

### Installation ID

- Stored as `installation_id` in `~/.schmux/config.json`
- Generated on first run if missing (UUID v4)
- Migration: daemon startup checks and sets if absent
- Used as PostHog `distinct_id` for anonymous user tracking

### Opt-Out

Users disable telemetry in `~/.schmux/config.json`:

```json
{
  "telemetry_enabled": false,
  "installation_id": "uuid-here"
}
```

If `telemetry_enabled` is explicitly `false`, skip all tracking. Default is `true`.

## Implementation

### 1. New Package: `internal/telemetry/`

```
internal/telemetry/
├── telemetry.go      # Client, Init, Track, Shutdown
└── telemetry_test.go # Unit tests
```

**Design:**

- PostHog HTTP API directly (no SDK dependency)
- Bounded event queue (100 events) with single worker goroutine
- Non-blocking: `Track()` enqueues and returns immediately
- Graceful shutdown with flush timeout (5s)
- Rate-limited failure logging (max 1 per minute)
- Hardcoded endpoint: `https://us.posthog.com`

### 2. Interface (for dependency injection)

```go
type Telemetry interface {
    Track(event string, properties map[string]any)
    Shutdown()
}

type NoopTelemetry struct{}  // Used when disabled
```

Managers receive `Telemetry` interface in constructor, not package globals.

### 3. Config Changes

**internal/config/config.go:**

```go
type Config struct {
    // ... existing fields
    TelemetryEnabled bool   `json:"telemetry_enabled,omitempty"`
    InstallationID   string `json:"installation_id,omitempty"`
    // ...
}
```

**internal/secrets/secrets.go:**

```go
type Secrets struct {
    // ... existing fields
    PosthogAPIKey string `json:"posthog_api_key,omitempty"`
}
```

### 4. Integration Points

**All workspace creation paths** - call after successful creation:

| Path                    | File                            | Method                  |
| ----------------------- | ------------------------------- | ----------------------- |
| Normal creation         | `internal/workspace/manager.go` | `create()`              |
| From existing workspace | `internal/workspace/manager.go` | `CreateFromWorkspace()` |
| Local repo              | `internal/workspace/manager.go` | `CreateLocalRepo()`     |

**All session spawn paths** - call after successful spawn:

| Path          | File                          | Method           |
| ------------- | ----------------------------- | ---------------- |
| Normal spawn  | `internal/session/manager.go` | `Spawn()`        |
| Remote spawn  | `internal/session/manager.go` | `SpawnRemote()`  |
| Command spawn | `internal/session/manager.go` | `SpawnCommand()` |

**Push to main:**

| Path        | File                                | Method                               |
| ----------- | ----------------------------------- | ------------------------------------ |
| Linear sync | `internal/workspace/linear_sync.go` | `LinearSyncToDefault()` (on success) |

### 5. Build-Time API Key Injection

**GitHub Actions** (`.github/workflows/`):

```yaml
env:
  POSTHOG_API_KEY: ${{ secrets.POSTHOG_API_KEY }}
```

**Build command:**

```bash
go build -ldflags "-X main.posthogAPIKey=$POSTHOG_API_KEY" ./cmd/schmux
```

**internal/telemetry/telemetry.go:**

```go
var embeddedAPIKey string // Set via ldflags

func getAPIKey(secrets *secrets.Secrets) string {
    if secrets != nil && secrets.PosthogAPIKey != "" {
        return secrets.PosthogAPIKey  // User override
    }
    return embeddedAPIKey  // Build-time default
}
```

### 6. Files to Create/Modify

| File                                   | Change                                    |
| -------------------------------------- | ----------------------------------------- |
| `internal/telemetry/telemetry.go`      | **NEW** - PostHog client                  |
| `internal/telemetry/telemetry_test.go` | **NEW** - Unit tests                      |
| `internal/config/config.go`            | Add `TelemetryEnabled`, `InstallationID`  |
| `internal/secrets/secrets.go`          | Add `PosthogAPIKey`                       |
| `internal/daemon/daemon.go`            | Init telemetry, ensure installation ID    |
| `internal/workspace/manager.go`        | Inject telemetry, track workspace_created |
| `internal/session/manager.go`          | Inject telemetry, track session_created   |
| `internal/workspace/linear_sync.go`    | Track push_to_main                        |
| `docs/api.md`                          | Document config changes                   |
| `docs/telemetry.md`                    | **NEW** - User-facing privacy doc         |
| `.github/workflows/*.yml`              | Add POSTHOG_API_KEY secret reference      |

### 7. PostHog API Details

- **Endpoint**: `https://us.posthog.com/capture/`
- **Method**: POST
- **Payload**:

```json
{
  "api_key": "phc_...",
  "event": "event_name",
  "distinct_id": "installation-uuid",
  "properties": { ... },
  "timestamp": "2024-01-15T10:30:00Z"
}
```

### 8. Delivery Guarantees

- **At-most-once** delivery (no retry on failure)
- Events dropped if queue full (oldest dropped, log once)
- Network failures rate-limited to 1 log/minute
- Never blocks caller (enqueue is <1ms)
- Flush on shutdown with 5s timeout

## Verification

1. Fresh install → verify `installation_id` created in config
2. Create workspace → verify `workspace_created` event in PostHog
3. Spawn session → verify `session_created` event in PostHog
4. Push to main → verify `push_to_main` event in PostHog
5. Set `telemetry_enabled: false` → verify no events sent
6. Set `posthog_api_key` in secrets.json → verify override works
7. Kill daemon mid-event → verify graceful shutdown (no goroutine leak)

## Docs to Update

- `docs/api.md` - Add `telemetry_enabled`, `installation_id` to config schema
- `docs/telemetry.md` - **NEW** - Privacy policy, what's collected, how to opt-out
