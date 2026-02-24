# Dashboard.sx Commit Split Plan

**Goal**: Break the 2,231-line dashboard.sx HTTPS feature into 9 atomic, review-friendly commits.

Each commit must compile and pass tests independently.

---

## Commit 1: `feat(config): add DashboardSXConfig schema`

**Files**:
- `internal/config/config.go` — DashboardSXConfig struct, NetworkConfig.DashboardSX field, 5 getters
- `internal/config/config_test.go` — tests for getters and JSON roundtrip

**Changes**:
```go
// New struct
type DashboardSXConfig struct {
    Enabled    bool   `json:"enabled"`
    Code       string `json:"code,omitempty"`
    Email      string `json:"email,omitempty"`
    IP         string `json:"ip,omitempty"`
    ServiceURL string `json:"service_url,omitempty"`
}

// Add to NetworkConfig
DashboardSX *DashboardSXConfig `json:"dashboardsx,omitempty"`

// New getters
GetDashboardSXEnabled()
GetDashboardSXCode()
GetDashboardSXIP()
GetDashboardSXEmail()
GetDashboardSXHostname()
```

**Tests**: Nil checks, enabled/disabled, JSON roundtrip

---

## Commit 2: `feat(dashboardsx): add utility helpers`

**Files**:
- `internal/dashboardsx/paths.go` — certificate/key file paths
- `internal/dashboardsx/instance_key.go` — generate/read instance key
- `internal/dashboardsx/ip.go` — detect public IP
- `internal/dashboardsx/instance_key_test.go`
- `internal/dashboardsx/ip_test.go`

**No external dependencies** — pure utilities.

---

## Commit 3: `feat(dashboardsx): add API client for dashboard.sx service`

**Files**:
- `internal/dashboardsx/client.go` — HTTP client struct, NewClient, API methods
- `internal/dashboardsx/client_test.go` — mock server tests

**Methods**:
- `Heartbeat()`
- `Register()`
- `CertProvisioningStart()`
- `CertProvisioningComplete()`
- `SetTXTRecord()`
- `ClearTXTRecord()`

---

## Commit 4: `feat(dashboardsx): add cert status and background services`

**Files**:
- `internal/dashboardsx/status.go` — GetStatus(), certificate expiry checking
- `internal/dashboardsx/heartbeat.go` — StartHeartbeat() goroutine
- `internal/dashboardsx/renewal.go` — StartAutoRenewal() goroutine
- `internal/dashboardsx/status_test.go`
- `internal/dashboardsx/heartbeat_test.go`

**Dependencies**: Requires client.go from commit 3

---

## Commit 5: `feat(dashboardsx): add ACME certificate provisioning`

**Files**:
- `internal/dashboardsx/acme.go` — Lego integration, ServiceDNSProvider, ProvisionCert()
- `internal/dashboardsx/acme_test.go`

**Key types**:
- `acmeUser` — implements lego registration.User
- `ServiceDNSProvider` — implements challenge.Provider via dashboard.sx API
- `ProvisionCert()` — main entry point

**Dependencies**: Requires all previous dashboardsx files

---

## Commit 6: `feat(dashboard): add dashboard.sx callback handlers`

**Files**:
- `internal/dashboard/server.go` — dsxProvisionStatus field, route registration, CORS fix
- `internal/dashboard/handlers_dashboardsx.go` — callback and provision-status handlers
- `internal/dashboard/server_test.go` — CORS test fix (auth→TLS)

**Changes to server.go**:
- Add `dsxProvision` and `dsxProvisionMu` fields
- Register `/api/dashboardsx/callback` and `/api/dashboardsx/provision-status`
- Fix `isAllowedOrigin`: use `tlsEnabled` instead of `authEnabled` for scheme

---

## Commit 7: `feat(daemon): start dashboard.sx background services`

**Files**:
- `internal/daemon/daemon.go`

**Changes**:
- Add import for `internal/dashboardsx`
- On startup, if `GetDashboardSXEnabled()`:
  - Check cert expiry, warn if < 30 days
  - Read instance key
  - Start heartbeat goroutine
  - Start auto-renewal goroutine (if email configured)
- Fix Status() URL logic: use public_base_url if set (even without auth)

---

## Commit 8: `feat(cli): add dashboardsx command`

**Files**:
- `cmd/schmux/main.go` — wire up "dashboardsx" case, update usage
- `cmd/schmux/dashboardsx.go` — DashboardSXCommand struct and subcommands

**Subcommands**:
- `setup` — open browser to dashboard.sx, poll for callback
- `status` — show current config and cert expiry
- `disable` — set enabled=false, clear config
- `renew-cert` — manually trigger renewal

---

## Commit 9: `docs: update API docs for dashboard.sx`

**Files**:
- `docs/api.md`

**Changes**:
- Update CORS section: TLS determines scheme, not auth
- Add `dashboardsx` to network config response
- Document TLS behavior note
- Add `GET /api/dashboardsx/callback` endpoint
- Add `GET /api/dashboardsx/provision-status` endpoint
- Update `GET /api/auth/secrets` description

---

## Execution Strategy

1. **Stage files incrementally** using `git add -p` or by file
2. **Run tests after each commit**: `./test.sh`
3. **Each commit message follows conventional commits**
4. **Order matters** — later commits depend on earlier ones

## Verification

After all 9 commits:
```bash
git log --oneline HEAD~9..HEAD
./test.sh --all
```

Should show 9 commits and all tests passing.
