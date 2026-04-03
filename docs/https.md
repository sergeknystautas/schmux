# HTTPS

## What it does

Provides trusted HTTPS access to schmux over a private network without browser warnings, CA installation, or internet-routed tunnels. The `dashboard.sx` managed service maps private schmux deployments (192.168.x.x, 10.x.x.x, etc.) to publicly verifiable hostnames of the form `<code>.dashboard.sx`, enabling Let's Encrypt to issue free, automated TLS certificates. This unlocks browser Clipboard API access (copy/paste images) from any device on the LAN.

## Key files

| File                                         | Purpose                                                                                                                 |
| -------------------------------------------- | ----------------------------------------------------------------------------------------------------------------------- |
| `internal/dashboardsx/client.go`             | HTTP client for the dashboard.sx service API (heartbeat, cert provisioning, DNS challenge, callback exchange)           |
| `internal/dashboardsx/acme.go`               | ACME/Let's Encrypt integration via the lego library: DNS-01 challenge provider, cert provisioning, account management   |
| `internal/dashboardsx/heartbeat.go`          | Background heartbeat loop (24h +/- 2h jitter) to keep registrations alive; persists results via `HeartbeatStatusWriter` |
| `internal/dashboardsx/renewal.go`            | Background auto-renewal: checks cert expiry daily, renews when within 30 days of expiration                             |
| `internal/dashboardsx/instance_key.go`       | Generates and persists a 32-byte instance key at `~/.schmux/dashboardsx/instance.key`                                   |
| `internal/dashboardsx/paths.go`              | File paths for certs, keys, and ACME account under `~/.schmux/dashboardsx/`                                             |
| `internal/dashboardsx/ip.go`                 | Detects bindable LAN IP addresses, excludes loopback and link-local                                                     |
| `internal/dashboardsx/status.go`             | Reads filesystem and config to produce current dashboard.sx status (cert presence, expiry, code)                        |
| `internal/dashboard/handlers_dashboardsx.go` | HTTP handlers: OAuth callback exchange, background cert provisioning, provision status polling                          |

## Architecture decisions

- **Why a managed service instead of self-signed certs or mkcert:** Self-signed certs produce browser warnings on every device. mkcert requires installing a CA on every device. Let's Encrypt cannot issue certs for bare IP addresses. dashboard.sx gives each instance a real hostname that resolves to the private IP, so Let's Encrypt can issue a trusted cert with zero per-device setup.
- **Why DNS-01 challenge instead of HTTP-01:** The schmux instance is on a private network unreachable from the internet. DNS-01 challenges verify domain ownership via TXT records, which dashboard.sx manages on Route 53. No inbound internet traffic is needed.
- **Why the private key never leaves the machine:** The lego ACME library runs locally inside schmux. It generates the key pair, sends only the CSR to Let's Encrypt, and stores `cert.pem` and `key.pem` under `~/.schmux/dashboardsx/`. The dashboard.sx service never sees the private key.
- **Why heartbeats have jitter:** The interval is 24h +/- 2h (randomized via `crypto/rand`). This prevents a predictable phone-home cadence that could be used for traffic analysis.
- **Why a one-time callback token:** After GitHub OAuth on dashboard.sx, the browser is redirected to the local schmux instance with a `callback_token`. The daemon exchanges this token server-to-server for registration info (instance key, code, email). This prevents instance key exposure in the browser URL.
- **Why TLS is decoupled from GitHub Auth:** TLS can be enabled independently of GitHub OAuth. The server uses `GetTLSEnabled()` (checks both cert+key paths are non-empty) to decide whether to call `ListenAndServeTLS()`. Auth still requires TLS at the config validation level, but TLS runs independently.

## Setup flow

1. User opens `https://dashboard.sx/register?instance_key=<key>&ip=<ip>&return_url=<callback>` (launched from dashboard UI or CLI)
2. dashboard.sx validates the return URL (must match provided IP, must be private range)
3. User authenticates with GitHub on dashboard.sx
4. dashboard.sx assigns a random 5-digit code, creates a Route 53 A record (`<code>.dashboard.sx` -> user's LAN IP), generates a one-time `callback_token`
5. Browser redirects to `http://<ip>:7337/api/dashboardsx/callback?callback_token=<token>`
6. schmux exchanges the callback token server-to-server with dashboard.sx for registration info
7. schmux saves config (code, email, IP) and kicks off cert provisioning in a background goroutine
8. Cert provisioning: schmux calls `/cert-provisioning/start` to get a challenge token, the lego library runs DNS-01 challenge via dashboard.sx's `/dns-challenge` endpoint, Let's Encrypt issues the cert
9. schmux saves cert/key to `~/.schmux/dashboardsx/`, updates config with TLS paths and `public_base_url`
10. On next daemon restart, the server starts with `ListenAndServeTLS()`

## File layout

```
~/.schmux/dashboardsx/
  instance.key       # 32-byte random hex, identifies this schmux instance
  acme-account.key   # EC P-256 private key for Let's Encrypt account
  cert.pem           # TLS certificate (Let's Encrypt issued)
  key.pem            # TLS private key (never leaves machine)
```

## Config UI

The Access tab in the dashboard settings is organized into a cascading dependency chain:

1. **Network** -- Bind address (localhost vs LAN), port
2. **HTTPS** -- Enable toggle, cert/key paths (read-only, configured via modal), certificate hostname (extracted from cert), auto-composed dashboard URL
3. **GitHub Authentication** -- Enable toggle (greyed out with "Requires HTTPS" badge until HTTPS is configured), session TTL, OAuth credentials
4. **Remote Access** -- Cloudflare tunnel, password, ntfy (independent of the cascade)

### TLS Certificate Modal

- Two path input fields (cert path, key path), pre-filled with current values
- "Validate" button hits `POST /api/tls/validate` for server-side validation
- Inline results: file exists, valid PEM, cert+key match, hostname, expiry
- "Save" only enabled after successful validation

### Edge cases

- **HTTPS enabled, no certs configured:** Warning on save; auth toggle stays disabled
- **Auth enabled, user disables HTTPS:** Confirmation dialog ("Disabling HTTPS will also disable GitHub Authentication. Continue?")
- **Cert expiring within 30 days:** Yellow warning in UI; auto-renewal attempts daily
- **Port changes:** Dashboard URL auto-updates (composed from hostname + port)

## DashboardSX status alerts

The daemon surfaces dashboard.sx heartbeat failures and certificate expiry warnings on the home page. Before this, both conditions were log-only -- nobody noticed until the URL broke.

**What is tracked:** The `DashboardSXStatus` struct in `internal/state/state.go` stores `LastHeartbeatTime`, `LastHeartbeatStatus` (HTTP status code, 0 for network errors), `LastHeartbeatError`, `CertDomain`, and `CertExpiresAt`.

**How it is populated:**

- **At daemon startup:** If dashboard.sx is enabled, reads `CertDomain` and `CertExpiresAt` from the cert on disk.
- **On every heartbeat:** `StartHeartbeat` accepts a `HeartbeatStatusWriter` interface. The daemon provides a `heartbeatStateWriter` adapter that merges heartbeat results into the existing status (preserving cert fields) and saves.

**How it reaches the frontend:** Piggybacked on `ConfigResponse.DashboardSXStatus` (same pattern as `NeedsRestart`). The home page computes alerts when `last_heartbeat_status` is not 200 or `cert_expires_at` is within 30 days.

**Why home page, not config page:** The config page is for changing settings. Heartbeat failures are operational alerts that need passive visibility on the landing page.

**Gotchas:**

- The `heartbeatStateWriter` merges results into existing status. Creating a new struct each time would wipe cert fields on every heartbeat.
- Cert info is populated once at startup from disk. If auto-renewal replaces the cert, `CertExpiresAt` becomes stale until restart.
- The 30-day frontend alert threshold matches the auto-renewal threshold in `renewal.go`. Keep them in sync.

## Gotchas

- The dashboard.sx service is hosted on fly.io. DNS is managed via Route 53. The authoritative nameservers for `dashboard.sx` are hardcoded in `acme.go` for DNS-01 verification to bypass recursive resolver caching of negative responses.
- Cert auto-renewal runs in the background but requires a daemon restart to pick up the new cert (Go's `ListenAndServeTLS` loads certs at startup).
- The instance key is generated once and persists across cert renewals. It identifies the schmux instance to dashboard.sx.
- `DetectBindableIPs()` excludes loopback (127.x.x.x) and link-local (169.254.x.x) addresses but includes all other IPv4 addresses on up interfaces.
- The `callback_token` exchange is server-to-server (daemon calls dashboard.sx), not browser-to-server. This prevents the instance key from appearing in the browser URL.

## Common modification patterns

- **Add a new dashboard.sx API endpoint:** Add request/response types and a method to `internal/dashboardsx/client.go`.
- **Change cert storage location:** Update path functions in `internal/dashboardsx/paths.go`.
- **Modify the provisioning flow:** Edit `internal/dashboard/handlers_dashboardsx.go` for the HTTP handler and `internal/dashboardsx/acme.go` for the ACME flow.
- **Adjust auto-renewal threshold:** Change `renewalThresholdDays` in `internal/dashboardsx/renewal.go` (currently 30 days).
- **Add TLS validation rules:** The `POST /api/tls/validate` endpoint validates file existence, PEM format, cert+key match, hostname extraction, and expiry.
- **Add new status alerts:** Add fields to `state.DashboardSXStatus`, populate in daemon or heartbeat writer, add to `contracts.DashboardSXStatus`, regenerate types, add alert logic in `HomePage.tsx`.

## Configuration

```json
{
  "network": {
    "bind_address": "0.0.0.0",
    "port": 7337,
    "public_base_url": "https://47293.dashboard.sx:7337",
    "tls": {
      "cert_path": "/home/user/.schmux/dashboardsx/cert.pem",
      "key_path": "/home/user/.schmux/dashboardsx/key.pem"
    },
    "dashboard_sx": {
      "enabled": true,
      "code": "47293",
      "email": "user@example.com",
      "ip": "192.168.1.100",
      "service_url": "https://dashboard.sx"
    }
  }
}
```

## Test coverage

| Test file                                   | Scope                                                                                          |
| ------------------------------------------- | ---------------------------------------------------------------------------------------------- |
| `internal/dashboardsx/client_test.go`       | API client: heartbeat, cert provisioning start, DNS challenge create/delete, callback exchange |
| `internal/dashboardsx/acme_test.go`         | ACME account loading/creation, cert saving, DNS provider                                       |
| `internal/dashboardsx/heartbeat_test.go`    | Heartbeat interval jitter                                                                      |
| `internal/dashboardsx/instance_key_test.go` | Instance key generation and persistence                                                        |
| `internal/dashboardsx/ip_test.go`           | IP filtering (loopback, link-local exclusion)                                                  |
| `internal/dashboardsx/status_test.go`       | Status aggregation from filesystem and config                                                  |
| `internal/dashboard/handlers_tls_test.go`   | TLS validation endpoint                                                                        |
| `internal/state/state_dashboardsx_test.go`  | DashboardSXStatus getter/setter and persistence                                                |
