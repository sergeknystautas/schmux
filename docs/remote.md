# Remote Access

## What it does

schmux has two distinct "remote" subsystems: **Remote Access** exposes the local dashboard over the internet via Cloudflare tunnels so you can manage sessions from your phone, and **Remote Sessions** let you orchestrate AI agents running on remote hosts via tmux control mode while keeping the daemon local.

---

## Remote Access (Cloudflare Tunnel)

### What it does

Creates an ephemeral `*.trycloudflare.com` HTTPS URL so you can access your schmux dashboard from any device over the internet, even behind residential NAT. Authentication uses a three-step token-nonce-password flow designed for ephemeral URLs where OAuth callback URLs are not viable.

### Key files

| File                                           | Purpose                                                                                                                      |
| ---------------------------------------------- | ---------------------------------------------------------------------------------------------------------------------------- |
| `internal/tunnel/manager.go`                   | Tunnel lifecycle: spawn/supervise `cloudflared`, parse URL from stderr, state machine (`off`/`starting`/`connected`/`error`) |
| `internal/tunnel/cloudflared.go`               | Binary management: find on PATH, download, extract, verify codesign                                                          |
| `internal/tunnel/notify.go`                    | Notification dispatch via ntfy.sh and/or custom shell command                                                                |
| `internal/dashboard/handlers_remote_auth.go`   | Auth flow: token-nonce exchange, password form, bcrypt validation, session cookie                                            |
| `internal/dashboard/handlers_remote_access.go` | Management endpoints: start/stop tunnel, status, set password, test notification                                             |
| `internal/dashboard/auth.go`                   | Auth middleware: `withAuth`, `withAuthAndCSRF`, `isTrustedRequest`                                                           |
| `assets/dashboard/src/lib/csrf.ts`             | Frontend CSRF cookie reading and header injection                                                                            |

### Architecture decisions

- **Why not GitHub OAuth for remote auth:** Cloudflare quick tunnels generate a new random hostname each session. GitHub OAuth requires a fixed callback URL registered in advance, so it is incompatible with ephemeral tunnel URLs.
- **Why three steps (token-nonce-password) instead of two:** The nonce step exists to remove the token from the browser URL bar via a 302 redirect before the user interacts with the page. This prevents token leakage through browser history sync or server logs.
- **Why per-tunnel session secrets:** The 32-byte HMAC secret is regenerated each time the tunnel starts. This cryptographically invalidates all cookies from previous tunnel sessions without maintaining a revocation list.
- **Why custom commands receive only the base URL:** The `$SCHMUX_REMOTE_URL` env var does not include the auth token, preventing token leakage to arbitrary command environments or shell history.
- **Why non-loopback bind is rejected:** `Manager.Start()` refuses to start when the server binds to `0.0.0.0` to prevent exposing an unauthenticated listener on the LAN.

### Security model

Nine layers, innermost to outermost:

1. **Transport** -- Cloudflare TLS between remote device and edge; encrypted tunnel between Cloudflare and localhost
2. **Authentication** -- One-time token (32B) + 5-min nonce (16B) + bcrypt password
3. **Session cookie** -- HMAC-SHA256 signed, `HttpOnly`/`Secure`/`SameSite=Lax`, 12h TTL, per-tunnel secret
4. **CSRF** -- `X-CSRF-Token` header must match `schmux_csrf` cookie; local requests are exempt
5. **CORS** -- When tunnel is active, only the tunnel URL and localhost origins are allowed
6. **Rate limiting** -- 5 req/min per IP on `/remote-auth` POST; 5 failed passwords per IP locks out all nonces
7. **Trusted request bypass** -- Loopback requests without `Cf-Connecting-IP` skip tunnel-only auth
8. **Binary verification** -- macOS `codesign -v --deep` on downloaded `cloudflared`; download/decompression size limits
9. **Non-loopback bind rejection** -- Tunnel refuses to start on `0.0.0.0`

### Gotchas

- Password change while tunnel is active regenerates the session secret, which invalidates all existing remote cookies. Users must re-authenticate.
- The lockout counter resets only when the tunnel restarts, not after a timeout.
- `AllowAutoDownload` defaults to `false`. Users must explicitly opt in before schmux downloads `cloudflared`.
- The password form page is self-contained HTML served by the Go backend (no React dependency), because the user is not yet authenticated to load the SPA.
- Local requests during an active tunnel are still allowed without authentication (trusted request bypass).

### Common modification patterns

- **Add a new notification channel:** Add delivery logic in `internal/tunnel/notify.go`, extend `NotifyConfig` in `internal/config/config.go`, update the dashboard UI in `assets/dashboard/src/`.
- **Change auth flow behavior:** Modify `internal/dashboard/handlers_remote_auth.go`. State fields live on the `Server` struct in `internal/dashboard/server.go`.
- **Adjust rate limits or lockout thresholds:** Constants are at the top of `internal/dashboard/handlers_remote_auth.go`.

### Configuration

```json
{
  "remote_access": {
    "enabled": true,
    "timeout_minutes": 0,
    "password_hash": "$2a$10$...",
    "allow_auto_download": false,
    "notify": {
      "ntfy_topic": "my-secret-topic",
      "command": ""
    }
  }
}
```

| Field                 | Default | Description                                          |
| --------------------- | ------- | ---------------------------------------------------- |
| `enabled`             | `true`  | Kill switch. When false, tunnel start is rejected.   |
| `timeout_minutes`     | `0`     | Auto-kill tunnel after N minutes. 0 = no timeout.    |
| `password_hash`       | `""`    | bcrypt hash. Plaintext never touches disk.           |
| `allow_auto_download` | `false` | Whether to auto-download `cloudflared` if not found. |
| `notify.ntfy_topic`   | `""`    | ntfy.sh topic for push notifications.                |
| `notify.command`      | `""`    | Shell command run with `$SCHMUX_REMOTE_URL` env var. |

### CLI

```
schmux remote on             Start tunnel, send notification
schmux remote off            Stop tunnel, clear auth state
schmux remote status         Show tunnel state, URL
schmux remote set-password   Set password (interactive prompt)
```

### Test coverage

| Test file                                           | Scope                                                                               |
| --------------------------------------------------- | ----------------------------------------------------------------------------------- |
| `internal/dashboard/handlers_remote_auth_test.go`   | Cookie validation, nonce lifecycle, token consumption, rate limiting, XSS escaping  |
| `internal/dashboard/tunnel_e2e_test.go`             | Full auth flow, CSRF attacks, CORS, nonce reuse, cookie replay, brute force lockout |
| `internal/dashboard/handlers_remote_access_test.go` | On/off/status endpoints                                                             |
| `internal/tunnel/cloudflared_test.go`               | Binary verification, download URLs, decompression bomb protection                   |
| `internal/tunnel/manager_test.go`                   | Tunnel state machine, non-loopback rejection                                        |
| `internal/tunnel/notify_test.go`                    | ntfy.sh notification                                                                |
| `assets/dashboard/src/lib/csrf.test.ts`             | Frontend CSRF cookie reading                                                        |
| `assets/dashboard/src/lib/api-csrf.test.ts`         | CSRF header on all remote access API calls                                          |

---

## Remote Sessions (tmux Control Mode)

### What it does

Orchestrates AI agents running on remote hosts while keeping the schmux daemon and web dashboard on the local machine. Uses tmux control mode (`tmux -CC`) as the transport protocol -- a text-based protocol for programmatic tmux interaction over stdin/stdout of an SSH (or similar) connection.

### Key files

| File                                         | Purpose                                                                                                    |
| -------------------------------------------- | ---------------------------------------------------------------------------------------------------------- |
| `internal/remote/manager.go`                 | Manages multiple remote host connections, session reconciliation, expiry pruning                           |
| `internal/remote/connection.go`              | Single connection lifecycle: connect, reconnect, provision, PTY management, control mode setup             |
| `internal/remote/controlmode/parser.go`      | Parses tmux control mode protocol (`%begin`/`%end`/`%output`/`%exit`) from stdout stream                   |
| `internal/remote/controlmode/client.go`      | High-level tmux command interface: create/kill windows, send keys, capture pane, FIFO response correlation |
| `internal/remote/controlmode/keyclassify.go` | Classifies keyboard input into literal text vs. special keys for correct `send-keys` handling              |

### Architecture decisions

- **Why tmux control mode instead of SSH exec:** Control mode provides a persistent, multiplexed connection. A single SSH session can manage many tmux windows without opening separate SSH channels per agent session.
- **Why PTY for the connection process:** The connection command (SSH, etc.) often requires interactive authentication (Yubikey, MFA). A PTY enables these prompts to flow through to a WebSocket-backed terminal in the dashboard.
- **Why FIFO response correlation:** tmux assigns sequential command IDs starting from 0, not using client-supplied IDs. The client matches responses to commands in FIFO order, which means responses from a previous control mode session (after daemon restart) are stale and must be discarded.
- **Why no auto-reconnect on daemon restart:** Reconnection typically requires interactive authentication (e.g., Yubikey touch). `MarkStaleHostsDisconnected()` marks all previously-connected hosts as disconnected at startup; the user explicitly clicks "Reconnect" in the dashboard.
- **Why session reconciliation uses IDs only:** After reconnection, sessions are matched to remote tmux windows strictly by window ID or pane ID. Name-based matching is deliberately avoided because tmux window names can change and cause wrong matches.

### Gotchas

- The `parseProvisioningOutput` goroutine is the sole PTY reader. It broadcasts raw bytes to WebSocket subscribers and tees data to a pipe for the control mode parser. Two goroutines must never read from the same PTY fd.
- After `controlModeEstablished` is set to true, hostname extraction from PTY output stops. Without this guard, tmux `%output` events containing hostnames would cause false-positive status updates.
- `Connection.Close()` uses `sync.Once` so it is safe to call from both `monitorProcess` (when SSH dies) and explicit disconnect.
- Pending sessions are queued during connection setup and drained once control mode is ready. This prevents commands from being sent before tmux enters control mode.
- Host expiry defaults to 12 hours (`DefaultHostExpiry`). `PruneExpiredHosts()` runs periodically to clean up.

### Common modification patterns

- **Add a new remote connection method:** Implement a new `ConnectCommand` template in the flavor config. The template receives `{{.Flavor}}` for connect and `{{.Hostname}}` + `{{.Flavor}}` for reconnect.
- **Change provisioning behavior:** Edit `Connection.Provision()` in `internal/remote/connection.go`. The provision command is a Go template receiving `{{.WorkspacePath}}` and `{{.VCS}}`.
- **Add a new tmux command:** Add a method to `controlmode.Client` in `internal/remote/controlmode/client.go`. Use `c.Execute(ctx, cmd)` which handles FIFO queuing and response correlation.
- **Customize hostname extraction:** Set `hostname_regex` in the remote flavor config. The first capture group is used as the hostname.

### Configuration

Remote flavors are configured in `~/.schmux/config.json` under `remote_flavors`:

```json
{
  "remote_flavors": [
    {
      "id": "my-remote",
      "flavor": "gpu-large",
      "display_name": "GPU Instance",
      "connect_command": "ssh -t {{.Flavor}} tmux -CC new-session",
      "reconnect_command": "ssh -t {{.Hostname}} tmux -CC attach",
      "provision_command": "cd {{.WorkspacePath}} && git pull",
      "workspace_path": "/home/user/project",
      "vcs": "git",
      "hostname_regex": "Connecting to (\\S+)"
    }
  ]
}
```

### Test coverage

| Test file                                    | Scope                                                        |
| -------------------------------------------- | ------------------------------------------------------------ |
| `internal/remote/manager_test.go`            | Connection lifecycle, flavor status, reconnection            |
| `internal/remote/connection_test.go`         | Connect/reconnect, PTY management, provisioning              |
| `internal/remote/controlmode/parser_test.go` | Protocol parsing, edge cases                                 |
| `internal/remote/controlmode/client_test.go` | Command execution, FIFO correlation, stale response handling |
