# dashboard.sx Status Alerts

Surface dashboard.sx heartbeat failures and certificate expiry warnings in the web dashboard home page, replacing the current log-only behavior that nobody sees.

## Problem

The heartbeat to dashboard.sx runs every ~24h. When it gets a non-200 response (403, 500, network error), the daemon logs it and retries in 24h. No user-visible signal. The cert expiry check at daemon startup is also log-only. The result: when dashboard.sx stops working, the user finds out when the URL breaks.

## State struct

Add to `internal/state/state.go`:

```go
type DashboardSXStatus struct {
    LastHeartbeatTime   time.Time `json:"last_heartbeat_time,omitempty"`
    LastHeartbeatStatus int       `json:"last_heartbeat_status,omitempty"` // HTTP status code, 0 for network error
    LastHeartbeatError  string    `json:"last_heartbeat_error,omitempty"`
    CertDomain          string    `json:"cert_domain,omitempty"`
    CertExpiresAt       time.Time `json:"cert_expires_at,omitempty"`
}
```

Field on `State`: `DashboardSX *DashboardSXStatus json:"dashboard_sx,omitempty"`

Getter/setter following the `NeedsRestart` pattern.

## Backend changes

### `internal/dashboardsx/client.go`

`Heartbeat()` returns the HTTP status code alongside the error. Currently it discards the response body and only returns `error`. Change signature to return `(int, error)` where the int is the HTTP status code (0 for network/transport errors).

### `internal/dashboardsx/heartbeat.go`

`StartHeartbeat` accepts a state writer interface so it can persist heartbeat results. After each heartbeat call, save the status code, timestamp, and error message to state.

### `internal/daemon/daemon.go`

On startup when dashboardsx is enabled:

- Read cert from disk, populate `CertDomain` and `CertExpiresAt` in state (replacing the log-only expiry check).
- Pass state to `StartHeartbeat`.

### API contract

Add `DashboardSXStatus` to the config API response (`contracts.ConfigResponse`), populated from state. Same pattern as `NeedsRestart`.

## Frontend

### Home page

When `DashboardSXStatus` has alerts (last heartbeat status != 200, or cert expires within 30 days), render a section titled "dashboard.sx alerts". Only renders when there are alerts to show.

Format:

```
dashboard.sx alerts
  heartbeat: 2026-04-01 14:30 403 registration not found
  certificate: 12540.dashboard.sx expires in 12 days
```

## Files to change

| File                                          | Change                                                                 |
| --------------------------------------------- | ---------------------------------------------------------------------- |
| `internal/state/state.go`                     | Add `DashboardSXStatus` struct, field, getter/setter, interface method |
| `internal/state/interfaces.go`                | Add getter/setter to interface                                         |
| `internal/dashboardsx/client.go`              | `Heartbeat()` returns `(int, error)`                                   |
| `internal/dashboardsx/heartbeat.go`           | Accept state writer, persist result after each heartbeat               |
| `internal/daemon/daemon.go`                   | Populate cert info in state, pass state to StartHeartbeat              |
| `internal/api/contracts/config.go`            | Add `DashboardSXStatus` to `ConfigResponse`                            |
| `internal/dashboard/handlers_config.go`       | Populate `DashboardSXStatus` from state                                |
| `assets/dashboard/src/lib/types.generated.ts` | Regenerate (via `go run ./cmd/gen-types`)                              |
| `assets/dashboard/src/routes/HomePage.tsx`    | Render alerts section when present                                     |
| Tests for all of the above                    |
