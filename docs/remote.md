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

| File                                         | Purpose                                                                                                                  |
| -------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------ |
| `internal/remote/manager.go`                 | Multi-host lifecycle: connect, reconnect, disconnect, expiry pruning, session reconciliation, flavor status              |
| `internal/remote/connection.go`              | Single connection lifecycle: connect, reconnect, provision, PTY management, control mode setup, health probe             |
| `internal/remote/controlmode/parser.go`      | Parses tmux control mode protocol (`%begin`/`%end`/`%output`/`%exit`) from stdout stream                                 |
| `internal/remote/controlmode/client.go`      | High-level tmux command interface: create/kill windows, send keys with timing instrumentation, FIFO response correlation |
| `internal/remote/controlmode/keyclassify.go` | Key input classification and `SendKeysTimings` type definition                                                           |
| `internal/session/controlsource.go`          | `ControlSource` interface -- input boundary between SessionRuntime and local/remote implementations                      |
| `internal/session/remotesource.go`           | `RemoteSource`: ControlSource for remote sessions, with health probe goroutine                                           |
| `internal/session/tmux_health.go`            | `TmuxHealthProbe`: ring-buffer RTT measurement for control mode connections                                              |
| `internal/dashboard/latency_collector.go`    | `LatencyCollector`: per-keystroke timing ring buffer with sub-SendKeys breakdown percentiles                             |

### VCS abstraction

Remote sessions support multiple version control systems (Git and Sapling) via the `CommandBuilder` interface in `internal/vcs/`.

| File                                  | Purpose                                      |
| ------------------------------------- | -------------------------------------------- |
| `internal/vcs/vcs.go`                 | CommandBuilder interface (shared by all VCS) |
| `internal/vcs/git.go`                 | Git CommandBuilder implementation            |
| `internal/vcs/sapling.go`             | Sapling CommandBuilder implementation        |
| `internal/workspace/vcs.go`           | `HasVCSSupport` predicate                    |
| `internal/dashboard/handlers_diff.go` | Diff handlers (batch remote file content)    |

**Architecture decisions:**

- **Why batch remote file content**: O(1) RunCommand calls regardless of file count. Individual file fetches would be O(N) SSH round-trips.
- **Why base64 encoding for batch scripts**: `RunCommand` sends the entire command as a single `send-keys -l` call. Literal newlines terminate tmux control mode commands prematurely. Base64 produces a single line safe for transmission.
- **Why `HasVCSSupport` instead of widening `IsGitVCS`**: `IsGitVCS` is retained for call sites that genuinely need git-only behavior (worktree ops, git-specific filesystem watches). `HasVCSSupport` is the broader gate for commit graph/diff features.
- **Why per-file line cap (500) and byte cap (1MB)**: Line cap prevents one file from consuming capture-pane scrollback budget. Byte cap prevents minified JS single-line files from overwhelming the channel.
- **Why file count cap (50)**: Hard ceiling for monorepo edge cases. Numstat summary is always complete; files beyond the cap are listed with stats but content is omitted.

**Gotchas:**

- Sapling `.` is the working copy parent (equivalent to git HEAD); `.^` is the grandparent.
- `parseRangeToRevset` must map `HEAD` to `.` when converting git-style `A..B` range notation to Sapling revsets.
- Shell injection risk in batch scripts: all file paths must be quoted via `shellutil.Quote()` before interpolation.
- `RunCommand` sends the entire command as a single `send-keys -l` — the command MUST be a single line. This is the fundamental constraint driving the base64 approach.

### Architecture decisions

- **Why tmux control mode instead of SSH exec:** Control mode provides a persistent, multiplexed connection. A single SSH session can manage many tmux windows without opening separate SSH channels per agent session.
- **Why PTY for the connection process:** The connection command (SSH, etc.) often requires interactive authentication (Yubikey, MFA). A PTY enables these prompts to flow through to a WebSocket-backed terminal in the dashboard.
- **Why FIFO response correlation:** tmux assigns sequential command IDs starting from 0, not using client-supplied IDs. The client matches responses to commands in FIFO order, which means responses from a previous control mode session (after daemon restart) are stale and must be discarded.
- **Why no auto-reconnect on daemon restart:** Reconnection typically requires interactive authentication (e.g., Yubikey touch). `MarkStaleHostsDisconnected()` marks all previously-connected hosts as disconnected at startup; the user explicitly clicks "Reconnect" in the dashboard.
- **Why session reconciliation uses IDs only:** After reconnection, sessions are matched to remote tmux windows strictly by window ID or pane ID. Name-based matching is deliberately avoided because tmux window names can change and cause wrong matches.

### Multi-instance hosts

A flavor is a template (what kind of host to provision). A host is an instance (a specific running machine). Multiple hosts can share the same flavor, so you can run isolated workspaces on separate machines of the same type.

```
Flavor "www"  --->  Host remote-a1b2c3d4 (devvm1234)  --->  Sessions
              --->  Host remote-e5f6g7h8 (devvm5678)  --->  Sessions
```

**Architecture decisions:**

- **Why separate flavor from host (1:N):** A 1:1 flavor-to-connection mapping forces all sessions on a flavor to share one machine. This defeats workspace isolation. `Manager.Connect()` always creates a new host, never reuses an existing connection for the flavor.
- **Why host:workspace is 1:1:** Each remote host provides a single workspace. The host's filesystem is the workspace.
- **Why UUID identity, not hostname:** The host ID (`remote-{uuid8}`) is generated at provision start, before the hostname is known. Hostname is a display field populated asynchronously.
- **Why RemoteHost and Workspace stay separate:** Different lifecycle state machines. `RemoteHost` tracks infrastructure (hostname, expiry, connection state). `Workspace` tracks code context (repo, branch, path). The 1:1 relationship is maintained via `Workspace.RemoteHostID`.
- **Why expired workspaces persist:** When TTL expires, the workspace card stays with an "expired" badge. Session history is preserved until the user dismisses it.

**Key data model:**

- `Manager.connections` is `map[string]*Connection` keyed by host ID. `GetConnectionsByFlavorID()` returns all connections for a flavor.
- `Session.RemoteHostID` -> `remote.Manager.GetConnection(hostID)` -> `*Connection`.
- `ensureWorkspaceForHost()` creates the workspace immediately when a host is created.

### Typing profiling

The `sendKeys` segment in the typing performance breakdown is instrumented to expose where latency accumulates. Three non-overlapping sub-timings partition every `SendKeys` call:

```
sendKeys:  |---mutexWait---|---executeNet (stdin + FIFO)---|---classify overhead---|
```

**Architecture decisions:**

- **Why instrument at the `Execute()` level:** The three latency sources (mutex contention on `stdinMu`, SSH round-trip, FIFO head-of-line blocking) are only distinguishable inside `Client.Execute()`.
- **Why `Execute()` returns `(string, time.Duration, error)`:** Returning mutex wait as a stack-local value eliminates shared-mutable-state problems. The ~20 call sites that do not need timings use `_, _, err`.
- **Why `SendKeysTimings` lives in `controlmode`:** Follows the existing precedent where `ControlSource.GetCursorState()` returns `controlmode.CursorState`. The type flows through: `controlmode.Client.SendKeys` -> `remote.Connection.SendKeys` -> `ControlSource.SendKeys` -> `SessionRuntime.SendInput` -> WebSocket handler.
- **Why health probes for remote sessions:** `RemoteSource` runs a health probe goroutine (`Connection.ExecuteHealthProbe()`, a lightweight `display-message -p ok`). The probe result lets the dashboard distinguish network latency from FIFO head-of-line blocking.

**Decision framework** (for interpreting collected data):

| Dominant cost                               | Indicates                        | Next action                                               |
| ------------------------------------------- | -------------------------------- | --------------------------------------------------------- |
| `mutexWait` > 50% of `sendKeys`             | Shared-mutex bottleneck          | Per-session stdin channels or dedicated input SSH channel |
| `executeNet` dominates, single session      | SSH round-trip cost              | Fire-and-forget `send-keys` or command pipelining         |
| Health probe RTT diverges from `executeNet` | Contention vs. network separable | Use probe RTT as network baseline                         |

### Gotchas

- The `parseProvisioningOutput` goroutine is the sole PTY reader. Two goroutines must never read from the same PTY fd.
- After `controlModeEstablished` is set to true, hostname extraction from PTY output stops.
- `Connection.Close()` uses `sync.Once` so it is safe to call from both `monitorProcess` and explicit disconnect.
- Pending sessions are queued during connection setup and drained once control mode is ready.
- Host expiry defaults to 12 hours (`DefaultHostExpiry`). `PruneExpiredHosts()` runs periodically.
- `Manager.Connect()` always creates a new host. There is no "reuse existing connection for this flavor" path.
- `SetConnectCancel` must be called BEFORE the connect goroutine starts. If `Close()` races, the cancel never fires and the goroutine blocks for the full 5-minute timeout.
- The `max(0, execDur - mutexWait)` guard in `Client.SendKeys` prevents negative `ExecuteNet` values from macOS clock granularity edge cases.
- The health probe goroutine in `RemoteSource` subscribes to output BEFORE launching the probe. Reversing this order drops terminal output during the jitter window.
- `Execute()` returns mutex wait even on error paths.

### Common modification patterns

- **Add a new remote connection method:** Implement a new `ConnectCommand` template in the flavor config.
- **Change provisioning behavior:** Edit `Connection.Provision()` in `internal/remote/connection.go`.
- **Add a new tmux command:** Add a method to `controlmode.Client`. Use `c.Execute(ctx, cmd)` which handles FIFO queuing and response correlation.
- **Customize hostname extraction:** Set `hostname_regex` in the remote flavor config.
- **Add a new timing sub-segment:** Add a field to `controlmode.SendKeysTimings`, propagate through `Connection.SendKeys` -> `ControlSource.SendKeys` -> `SessionRuntime.SendInput`. Add to `LatencySample` and `LatencyPercentiles` in `latency_collector.go`.
- **Change health probe interval:** Constants at the top of `internal/session/tmux_health.go`.
- **Add host deprovisioning:** Add a `TeardownCommand` field to `config.RemoteFlavor`, execute in `Manager.Disconnect()`.

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

| Test file                                    | Scope                                                                          |
| -------------------------------------------- | ------------------------------------------------------------------------------ |
| `internal/remote/manager_test.go`            | Multi-host connection lifecycle, flavor status, reconnection, expiry           |
| `internal/remote/connection_test.go`         | Connect/reconnect, PTY management, provisioning, health probe                  |
| `internal/remote/controlmode/parser_test.go` | Protocol parsing, edge cases                                                   |
| `internal/remote/controlmode/client_test.go` | Command execution, FIFO correlation, stale response handling, SendKeys timings |
| `internal/session/remotesource_test.go`      | RemoteSource event forwarding, health probe lifecycle                          |
| `internal/session/controlsource_test.go`     | ControlSource interface compliance                                             |
