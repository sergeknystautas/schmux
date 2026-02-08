# Remote Workspace Architecture for Schmux

## Overview and Motivation

**Problem**: Schmux currently runs AI agents only on the local machine. Many development workflows require remote environments (e.g., GPU instances, specific OS versions, large codebases that need powerful remote machines).

**Solution**: Enable Schmux to orchestrate agents running on remote hosts while keeping the orchestration layer (daemon, web dashboard) local.

**Key Constraint**: Remote hosts are accessed via a remote connection command that requires authentication and provisions on-demand instances.

**Transport Protocol**: tmux Control Mode (`tmux -CC`) - a text-based protocol for programmatic tmux interaction over stdin/stdout.

## Core Architecture

```
┌─────────────────────────────────────────────────────────────┐
│ Developer's Local Machine                                   │
├─────────────────────────────────────────────────────────────┤
│                                                             │
│  Schmux Daemon                                              │
│  ├─ Dashboard Server (:7337)                                │
│  │   ├─ HTTP API (spawn, list, dispose)                     │
│  │   └─ WebSocket (terminal streaming, input)               │
│  │                                                          │
│  ├─ Session Manager                                         │
│  │   ├─ Local Sessions (via exec.Command + tmux)            │
│  │   └─ Remote Sessions (via Remote Manager)                │
│  │                                                          │
│  ├─ Remote Manager                                          │
│  │   └─ Connections (map[hostID]*Connection)                │
│  │                                                          │
│  └─ State/Config                                            │
│      ├─ config.json (remote flavors)                        │
│      └─ state.json (sessions, hosts, workspaces)            │
│                                                             │
└─────────────────────────────────────────────────────────────┘
                       ↓ SSH / Persistent Terminal
┌─────────────────────────────────────────────────────────────┐
│ Remote Host (e.g., remote-host-123.example.com)             │
├─────────────────────────────────────────────────────────────┤
│                                                             │
│  tmux -CC (Control Mode Session)                            │
│  ├─ stdin:  receives commands from local daemon             │
│  ├─ stdout: sends %output, %begin/%end responses            │
│  │                                                          │
│  └─ Windows (each = one Schmux session)                     │
│      ├─ Window @1 → Pane %5  (claude agent)                 │
│      ├─ Window @2 → Pane %10 (codex agent)                  │
│      └─ Window @3 → Pane %15 (cursor agent)                 │
│                                                             │
└─────────────────────────────────────────────────────────────┘
```

## tmux Control Mode Protocol

tmux Control Mode is the foundation of remote workspace communication. It provides a text-based protocol for programmatic interaction with tmux.

### Entering Control Mode

```bash
# Single -C: canonical mode (with echo, for testing)
tmux -C new-session -s mysession

# Double -CC: non-canonical mode (for applications)
tmux -CC new-session -A -s schmux
```

Schmux uses a remote connection command with tmux control mode to:
1. Provision/connect to remote host via persistent terminal
2. Launch tmux in control mode
3. Attach to session named "schmux" (create if doesn't exist)

The connection command varies by infrastructure but typically follows this pattern:
```bash
<remote-connect-cmd> <flavor-or-host> -- tmux -CC new-session -A -s schmux
```

### Command/Response Protocol

Every command sent to tmux's stdin produces a response framed with guard lines:

**Success response:**
```
list-windows
%begin 1578922740 269 1
0:0.0: [80x24] [history 0/2000, 0 bytes] %0 (active)
1:0.0: [80x24] [history 0/2000, 0 bytes] %1 (active)
%end 1578922740 269 1
```

**Error response:**
```
invalid-command
%begin 1578923149 270 1
parse error: unknown command: invalid-command
%error 1578923149 270 1
```

**Guard line format:**
- `%begin <timestamp> <cmd_id> <flags>`
- `%end <timestamp> <cmd_id> <flags>` (success)
- `%error <timestamp> <cmd_id> <flags>` (failure)

**Command ID**: Sequential integer for correlating responses to requests. Critical for concurrent command execution.

### Output Streaming (`%output`)

When panes produce output, tmux sends async notifications:

```
%output %5 Hello\040world\015\012
```

**Format**: `%output <pane_id> <escaped_data>`

**Escaping rules**: Characters < ASCII 32 and `\` are octal-escaped:
- `\` → `\134`
- Space → `\040`
- CR (13) → `\015`
- LF (10) → `\012`
- ESC (27) → `\033`

**Critical detail**: Output from panes in attached session is automatically sent. No polling needed.

### Async Notifications

tmux sends notifications when state changes:

| Notification | Meaning |
|-------------|---------|
| `%window-add @3` | Window created |
| `%window-close @3` | Window closed |
| `%window-renamed @3 new-name` | Window renamed |
| `%session-changed $1 foo` | Attached session changed |
| `%pane-mode-changed %5` | Pane mode changed (e.g., copy mode) |

### Key Commands for Schmux

**Create window (session) with command:**
```
new-window -n sessionname -c /path/to/workdir -P -F '#{window_id} #{pane_id}' command
```
Returns: `@3 %5` (window ID, pane ID)

**Send input to pane:**
```
send-keys -t %5 -l 'text to send'
```
`-l` = literal mode (preserves special characters)

**Kill window:**
```
kill-window -t @3
```

**Capture scrollback:**
```
capture-pane -t %5 -p -S -2000
```
Returns last 2000 lines of pane history.

**List windows:**
```
list-windows -F '#{window_id} #{window_name} #{pane_id}'
```

### ID Prefixes

tmux uses prefixes to distinguish entity types:
- `$0`, `$1` = Session IDs
- `@0`, `@3` = Window IDs
- `%0`, `%5` = Pane IDs

**Important**: Always use IDs, not names. IDs are stable; names can change.

## Configuration and State Management

### Configuration (`~/.schmux/config.json`)

**New type: Remote Flavors**

```json
{
  "remote_flavors": [
    {
      "id": "cloud_gpu",
      "flavor": "gpu-instance-large",
      "display_name": "Cloud GPU Large",
      "vcs": "git",
      "workspace_path": "~/workspace",
      "connect_command": "cloud-ssh connect {{.Flavor}}",
      "reconnect_command": "cloud-ssh reconnect {{.Hostname}}"
    },
    {
      "id": "ssh_remote",
      "flavor": "dev.example.com",
      "display_name": "SSH Remote Server",
      "vcs": "git",
      "workspace_path": "~/workspace"
    }
  ],
  "remote_workspace": {
    "vscode_command_template": "{{.VSCodePath}} --remote ssh-remote+{{.Hostname}} {{.Path}}"
  }
}
```

**Fields:**
- `id`: Auto-generated from flavor string, used for referencing
- `flavor`: The exact value passed to the remote connection command (or the hostname for SSH)
- `display_name`: Human-friendly name shown in UI
- `workspace_path`: Path where code lives on remote host (varies by flavor)
- `vcs`: "git" or "sapling" (affects UI status display)
- `connect_command` (optional): Go template for connecting to the remote host
  - Template variables: `{{.Flavor}}` - the flavor identifier
  - Default: `ssh {{.Flavor}}`
  - **Important**: You only specify HOW to reach the host. Schmux automatically appends `-- tmux -CC new-session -A -s schmux` internally.
  - Examples:
    - SSH: `ssh {{.Flavor}}`
    - Cloud provider: `cloud-ssh connect {{.Flavor}}`
    - AWS SSM: `aws ssm start-session --target {{.Flavor}}`
- `reconnect_command` (optional): Go template for reconnecting to an existing remote host
  - Template variables: `{{.Hostname}}` - remote hostname, `{{.Flavor}}` - flavor identifier
  - Default: `ssh {{.Hostname}}`
  - **Important**: You only specify HOW to reach the host. Schmux automatically appends `-- tmux -CC new-session -A -s schmux` internally.
  - Falls back to `connect_command` if not specified

**Remote Workspace Configuration:**
- `vscode_command_template` (optional): Go template for opening VS Code on remote workspaces
  - Template variables: `{{.VSCodePath}}` - local VSCode path, `{{.Hostname}}` - remote hostname, `{{.Path}}` - remote workspace path
  - Default: `{{.VSCodePath}} --remote ssh-remote+{{.Hostname}} {{.Path}}`
  - Example custom: `{{.VSCodePath}} --folder-uri vscode-remote://custom+{{.Hostname}}{{.Path}}`

**Connection Method Examples:**

1. **Standard SSH** (default - no config needed):
   ```json
   {
     "id": "ssh_dev",
     "flavor": "dev.example.com",
     "display_name": "Dev Server via SSH",
     "vcs": "git",
     "workspace_path": "~/workspace"
   }
   ```
   Internally executes: `ssh dev.example.com -- tmux -CC new-session -A -s schmux`

2. **Custom Connection Tool** (e.g., cloud provider CLI):
   ```json
   {
     "id": "cloud_gpu",
     "flavor": "gpu-large",
     "display_name": "Cloud GPU Instance",
     "vcs": "git",
     "workspace_path": "~/workspace",
     "connect_command": "cloud-ssh connect {{.Flavor}}",
     "reconnect_command": "cloud-ssh reconnect {{.Hostname}}"
   }
   ```
   Internally executes:
   - Connect: `cloud-ssh connect gpu-large -- tmux -CC new-session -A -s schmux`
   - Reconnect: `cloud-ssh reconnect host123.example.com -- tmux -CC new-session -A -s schmux`

3. **SSH with Custom Options**:
   ```json
   {
     "id": "ssh_custom",
     "flavor": "jumphost.example.com",
     "display_name": "Via Jump Host",
     "vcs": "git",
     "workspace_path": "~/code",
     "connect_command": "ssh -J bastion.example.com {{.Flavor}}"
   }
   ```
   Internally executes: `ssh -J bastion.example.com jumphost.example.com -- tmux -CC new-session -A -s schmux`

**Key Design Principle:**

User configuration focuses on **host connectivity only**. You never need to know about tmux, control mode, or schmux's internal protocols. The system automatically handles:
- tmux control mode invocation (`-CC`)
- Session creation/attachment (`new-session -A`)
- Session naming (`-s schmux`)

This separation of concerns makes configuration simpler and more robust.

3. **SSH with Custom Options**:
   ```json
   {
     "id": "ssh_custom",
     "flavor": "jumphost.example.com",
     "display_name": "Via Jump Host",
     "vcs": "git",
     "workspace_path": "~/code",
     "connect_command_template": "ssh -J bastion.example.com {{.Flavor}} -- tmux -CC new-session -A -s schmux"
   }
   ```

**User Management**: Flavors are managed via Settings page in web UI (`/settings/remote`) with full CRUD operations.

### State (`~/.schmux/state.json`)

**New type: Remote Hosts**

```json
{
  "remote_hosts": [
    {
      "id": "remote-abc123",
      "flavor_id": "gpu_ml_large",
      "hostname": "remote-host-456.example.com",
      "uuid": "def456",
      "connected_at": "2026-02-06T10:30:00Z",
      "expires_at": "2026-02-06T22:30:00Z",
      "status": "connected"
    }
  ]
}
```

**Fields:**
- `id`: Unique identifier (e.g., "remote-abc123")
- `flavor_id`: References config.remote_flavors[].id
- `hostname`: Parsed from connection output (e.g., "remote-host-456.example.com")
- `uuid`: Remote session UUID (parsed from stderr)
- `connected_at`: When connection was established
- `expires_at`: connected_at + 12 hours (host lifetime)
- `status`: "provisioning" | "authenticating" | "connected" | "disconnected"

**Session Extensions**:

```json
{
  "sessions": [
    {
      "id": "claude-xyz789",
      "remote_host_id": "remote-abc123",
      "remote_pane_id": "%5",
      // ... other fields
    }
  ]
}
```

- `remote_host_id`: Empty for local sessions, host ID for remote
- `remote_pane_id`: tmux pane ID on remote (e.g., "%5")

**Workspace Extensions**:

```json
{
  "workspaces": [
    {
      "id": "workspace-123",
      "remote_host_id": "remote-abc123",
      "remote_path": "~/workspace",
      // ... other fields
    }
  ]
}
```

- `remote_host_id`: Empty for local workspaces
- `remote_path`: Path on remote host

## Connection Lifecycle

### 1. Provisioning (New Host)

**Trigger**: User selects unconnected remote flavor in spawn wizard.

**Steps**:

1. **Spawn process**:
   ```bash
   remote-connect gpu:ml-large -- tmux -CC new-session -A -s schmux
   ```

2. **Parse stderr** for provisioning info:
   - Match connection establishment patterns to extract hostname
   - Match session UUID patterns to extract identifier

3. **Update state**:
   - Create RemoteHost with status="provisioning"
   - Update to status="authenticating" when hostname found
   - Notify UI via status callback

4. **Authentication flow**:
   - User interaction required (authentication device, password, etc.)
   - No programmatic detection - user observes prompts

5. **Wait for control mode**:
   - Parse stdout for `%` lines or tmux ready indicators
   - Send test command: `display-message -p 'ready'`
   - Timeout: 30 seconds

6. **Connected**:
   - Update state to status="connected"
   - Set expires_at = now + 12h
   - Drain pending session queue (create sessions that were waiting)

### 2. Reconnection (Existing Host)

**Trigger**: User clicks "Reconnect" on disconnected host, or selects flavor with existing hostname.

**Steps**:

1. **Spawn with hostname**:
   ```bash
   remote-connect remote-host-456.example.com -- tmux -CC new-session -A -s schmux
   ```

2. **Skip provisioning parsing** (hostname already known).

3. **Authentication** (still required for new connection).

   **Important**: Interactive authentication (e.g., 2FA prompts, Yubikey touch, SSH password prompts) requires stdin/stdout to be connected to a terminal. This has implications for daemon mode:

   - **Foreground mode** (`./schmux daemon-run`): Authentication prompts appear in the terminal and work correctly. Use this during development or when interactive auth is needed.
   - **Background mode** (`./schmux start`): The daemon runs detached with stdin/stdout redirected to log files. Interactive authentication prompts will hang or fail invisibly. For production use with background mode:
     - Configure non-interactive authentication (SSH keys, certificates)
     - Use authentication methods that don't require terminal interaction (OAuth flow via browser, pre-configured credentials)
     - Or accept running the daemon in foreground mode when remote workspaces are needed

4. **Attach to existing session**:
   - `new-session -A -s schmux` attaches if exists
   - All previous windows (sessions) still running

5. **Rediscover state**:
   - Run `list-windows -F '#{window_id} #{window_name} #{pane_id}'`
   - Match window names to session IDs in state
   - Update session status to "running" if found

6. **Resume output streaming**:
   - Resubscribe to `%output` for rediscovered panes
   - Capture scrollback for history

### 3. Disconnection

**Triggers**:
- User closes laptop (network interruption)
- User clicks "Disconnect" in UI
- Process crashes
- SSH connection drops

**Behavior**:
- Local: Update state to status="disconnected"
- Remote: Sessions keep running (tmux persists)
- UI: Show "Disconnected" badge, disable input

**Recovery**: Reconnection flow restores state.

### 4. Expiry

**Trigger**: Time reaches expires_at (12h from connection).

**Behavior**:
- Host is terminated by infrastructure
- State updated to status="expired"
- Sessions lost (cannot reconnect)

**User action**: Provision new host (full flow).

## Session Management

### Local Sessions (Unchanged)

```
Spawn() → exec.Command("tmux", "new-session", ...) → Process PID
```

### Remote Sessions (New)

```
SpawnRemote(flavorID, target, prompt, nickname) →
  1. Get/create remote host connection
  2. If provisioning: queue session, return pending
  3. If connected: CreateWindow(name, workdir, command)
  4. Store session with remote_host_id + remote_pane_id
```

**CreateWindow flow**:
1. Build command: `new-window -n name -c workdir -P -F '#{window_id} #{pane_id}' command`
2. Send to tmux stdin
3. Parse response: `@3 %5`
4. Store windowID and paneID
5. Subscribe to `%output %5` for streaming

### Session Queuing

**Problem**: Provisioning takes ~15s. User shouldn't wait.

**Solution**:
- Mark session as status="provisioning"
- Store in pending queue on connection
- When connection ready: create all pending sessions
- Update session status to "running"

**UI**: Shows "Provisioning..." status during wait.

## WebSocket Streaming

### Local Terminal Streaming (Existing)

```
WebSocket /ws/terminal/{id} →
  1. Tail /tmp/tmux-{pid}.log
  2. Send "full" message (initial content)
  3. Stream "append" messages as file grows
  4. Handle input: write to stdin
```

### Remote Terminal Streaming (New)

```
WebSocket /ws/terminal/{id} →
  1. Get session → lookup remote_pane_id
  2. Subscribe to connection.SubscribeOutput(paneID)
  3. Capture initial scrollback: CapturePaneLines(paneID, 2000)
  4. Send "full" message with scrollback
  5. Stream "append" messages from %output channel
  6. Handle input: conn.SendKeys(paneID, data)
  7. Defer: UnsubscribeOutput(paneID, chan)
```

**Critical difference**: No file tailing - output comes from control mode parser channel.

### Input Handling

**Flow**:
1. Browser: `terminal.onData(data)` → `sendInput(data)` → WebSocket
2. Backend: Receive `{"type":"input","data":"ls\n"}` message
3. Remote: `conn.SendKeys(ctx, paneID, data)`
4. Control Mode: `send-keys -t %5 -l 'ls\n'`
5. tmux: Sends literal keys to pane
6. Agent: Receives input as if user typed it

**Literal mode (`-l`)**: Preserves special characters (no interpretation).

**Shell escaping**: `shellQuote()` wraps in single quotes, escapes embedded quotes.

## Developer Experience

### Spawn Flow (Remote, First Time)

**UI Flow**:

1. **Click [+ New Session]**

2. **Environment Selection**:
   ```
   Where do you want to run?

   [🖥️ Local]      [☁️ GPU ML]         [☁️ Docker Dev]
   Your machine    Large               Environment
   ● Ready         ○ Connect           ○ Connect
   ```

3. **Click remote flavor** → Connection flow starts:
   ```
   Connecting to GPU ML Large

   ◐ Provisioning remote host...

   Authentication will be required shortly.

   Status: Reserving instance from pool

   [Cancel]
   ```

4. **Authentication prompt** (infrastructure-triggered):
   ```
   🔐 Authentication required

   Please complete authentication...

   [Cancel]
   ```

5. **Connected** → Agent selection:
   ```
   New Session on GPU ML Large

   Host: remote-host-456.example.com
   Workspace: ~/workspace

   Which agent?

   [Claude]  [Codex]  [Cursor]

   [Cancel]  [Start Session]
   ```

6. **Terminal view** (identical to local):
   ```
   Session: claude-abc123
   Host: GPU ML Large - remote-host-456.example.com

   $ claude

   ╭─────────────────────────────────────╮
   │ Claude Code                         │
   │                                     │
   │ I'm ready to help with your code.   │
   │ What would you like to work on?     │
   ╰─────────────────────────────────────╯

   [Nudge]  [Dispose]
   ```

**Time estimates**:
- Provisioning: ~15s
- Authentication: ~2s (user action)
- Total: ~17s first connection

### Spawn Flow (Remote, Existing Connection)

**UI Flow**:

1. **Click [+ New Session]**

2. **Environment Selection**:
   ```
   Where do you want to run?

   [🖥️ Local]      [☁️ GPU ML]         [☁️ Docker Dev]
   Your machine    Large               Environment
   ● Ready         ● Connected         ○ Connect
                   remote-host-456...
   ```

3. **Click connected flavor** → Skip to agent selection (no auth!)

4. **Session starts immediately** (~1s)

**Key UX benefit**: One auth unlocks multiple sessions on same host.

### Monitoring Multiple Sessions

```
Dashboard

Sessions
────────

GPU ML Large - remote-host-456.example.com
├─ claude-abc123  ● Running   Last output: 5s ago   [View]
└─ codex-def456   ● Running   Last output: 12s ago  [View]

Local
└─ claude-ghi789  ● Running   Last output: 2m ago   [View]

[+ New Session]
```

**Grouping**: Sessions grouped by host, shows connection status.

### Disconnection/Reconnection

**Disconnected state**:
```
GPU ML Large - ⚠️ Disconnected
├─ claude-abc123  ? Unknown   (host disconnected)   [Reconnect]
```

**Reconnect**:
1. Click [Reconnect]
2. Authentication required (new connection)
3. Sessions rediscovered via `list-windows`
4. Terminal history restored via `capture-pane`
5. Output streaming resumes

**Persistence**: Agents keep running on remote. Reconnection restores full state.

### Expiry

```
GPU ML Large - ⏰ Expired
├─ claude-abc123  ✕ Lost   (host expired after 12h)

[Provision New Host]
```

**Behavior**: Sessions lost, must reprovision (new host, fresh state).

## API Contracts

### Spawn Remote Session

```
POST /api/spawn
{
  "remote_flavor_id": "gpu_ml_large",  // NEW FIELD
  "target": "claude",
  "prompt": "Help me debug auth",
  "nickname": "auth-fix",
  // repo/branch NOT required for remote spawns
}

Response 200:
{
  "session_id": "claude-abc123",
  "status": "provisioning"  // or "running" if host already connected
}
```

### List Sessions (Extended)

```
GET /api/sessions

Response 200:
{
  "sessions": [
    {
      "id": "claude-abc123",
      "status": "running",
      "remote_host_id": "remote-abc123",              // NEW
      "remote_pane_id": "%5",                         // NEW
      "remote_hostname": "remote-host-456.example.com", // NEW
      "remote_flavor_name": "GPU ML Large",           // NEW
      // ... other fields
    }
  ]
}
```

### Remote Flavor Management

```
GET /api/config/remote-flavors
Response: [{ id, flavor, display_name, vcs, workspace_path }, ...]

POST /api/config/remote-flavors
Body: { flavor, display_name, workspace_path, vcs }
Response: { id, ... } // ID auto-generated

PUT /api/config/remote-flavors/{id}
Body: { display_name, workspace_path, vcs } // flavor immutable

DELETE /api/config/remote-flavors/{id}
Response: 204
```

### Remote Host Status

```
GET /api/remote/flavors/status
Response: [
  {
    "flavor": { id, flavor, display_name, vcs, workspace_path },
    "connected": true,
    "status": "connected",
    "hostname": "remote-host-456.example.com",
    "host_id": "remote-abc123"
  },
  ...
]
```

### Connect/Disconnect

```
POST /api/remote/hosts/connect
Body: { "flavor_id": "gpu_ml_large" }
Response: { "host_id": "remote-abc123", "status": "provisioning" }

POST /api/remote/hosts/{id}/reconnect
Response: { "status": "authenticating" }

DELETE /api/remote/hosts/{id}
Response: 204
```

## Implementation Components

### Control Mode Parser (`internal/remote/controlmode/parser.go`)

**Responsibility**: Parse stdin stream into structured events.

**Output channels**:
- `Responses()`: `%begin`/`%end`/`%error` blocks
- `Output()`: `%output` notifications
- `Events()`: `%window-add`, `%session-changed`, etc.

**Key function**: `unescapeOutput(s string)` - converts octal to bytes.

### Control Mode Client (`internal/remote/controlmode/client.go`)

**Responsibility**: Send commands, correlate responses, manage subscriptions.

**Key methods**:
- `Execute(ctx, cmd string) (string, error)` - Send command, wait for response
- `CreateWindow(ctx, name, workdir, command) (windowID, paneID, error)`
- `SendKeys(ctx, paneID, keys) error`
- `SubscribeOutput(paneID) <-chan OutputEvent`
- `UnsubscribeOutput(paneID, chan)`
- `CapturePaneLines(ctx, paneID, lines) (string, error)`

**Concurrency safety**: `stdinMu sync.Mutex` protects stdin writes.

### Connection Manager (`internal/remote/connection.go`)

**Responsibility**: Manage single remote host connection.

**Lifecycle**:
1. `NewConnection(cfg)` - Create struct
2. `Connect(ctx)` - Spawn remote connection command, parse output, initialize client
3. `Reconnect(ctx, hostname)` - Reuse existing hostname
4. `Close()` - Kill process, close pipes, unsubscribe all

**Key fields**:
- `cmd *exec.Cmd` - The remote connection process
- `client *controlmode.Client` - Control mode interface
- `parser *controlmode.Parser` - Protocol parser
- `host *state.RemoteHost` - Current state

**Status tracking**: `onStatusChange` callback notifies manager of state transitions.

### Remote Manager (`internal/remote/manager.go`)

**Responsibility**: Manage multiple remote hosts.

**Key methods**:
- `Connect(ctx, flavorID) (*Connection, error)` - Get/create connection
- `Reconnect(ctx, hostID) (*Connection, error)` - Reconnect by ID
- `GetConnection(hostID) *Connection` - Lookup connection
- `FlavorStatus() []FlavorStatus` - Get status of all flavors

**State persistence**: Saves/loads RemoteHost state via StateStore.

### Session Manager Updates (`internal/session/manager.go`)

**New method**: `SpawnRemote(ctx, flavorID, target, prompt, nickname) (*state.Session, error)`

**Flow**:
1. Get/create remote connection
2. If provisioning: queue session, return with status="provisioning"
3. If connected: create window via control mode
4. Create workspace (remote)
5. Create session state with `remote_host_id` + `remote_pane_id`
6. Save state

**Modified method**: `IsRunning(sessionID)` - checks via remote connection if remote.

### Dashboard API Updates (`internal/dashboard/handlers.go`)

**Modified**: `handleSpawnPost()` - Route to `SpawnRemote()` if `req.RemoteFlavorID != ""`.

**Modified**: `handleSessionsGet()` - Include remote metadata in response.

**Validation**: Skip repo/branch requirement when `RemoteFlavorID != ""`.

### WebSocket Updates (`internal/dashboard/websocket.go`)

**Modified**: `handleTerminalWebSocket()` - Detect remote session, route to remote streaming.

**Remote streaming**:
1. Subscribe to `conn.SubscribeOutput(paneID)`
2. Capture initial scrollback
3. Send "full" message
4. Stream "append" from output channel
5. Handle "input" → `conn.SendKeys()`
6. Cleanup: `defer conn.UnsubscribeOutput()`

### Workspace Manager Updates (`internal/workspace/manager.go`)

**Modified**: `UpdateGitStatus()` - Early return if `w.RemoteHostID != ""`.

**Modified**: `UpdateAllGitStatus()` - Skip remote workspaces in polling.

**Modified**: `Create()` - Don't add remote workspaces to git watcher.

**Rationale**: Remote workspaces have no local git repo. Attempting git operations causes errors.

## Key Technical Decisions

### 1. tmux Control Mode over Custom Agent

**Rationale**:
- tmux provides robust session persistence (agents survive disconnection)
- Protocol is well-documented and stable
- No custom agent to deploy/maintain on remote hosts
- Leverages existing remote infrastructure

**Trade-off**: Slightly more complex parsing, but avoids deployment complexity.

### 2. Sessions as tmux Windows

**Rationale**:
- One tmux session per host (all Schmux sessions share it)
- Each Schmux session = one tmux window
- Simplifies reconnection (one `tmux -CC` attachment)
- Allows multiple agents on same host without multiple SSH connections

**Trade-off**: Window/pane management more complex than process management.

### 3. Pane ID Targeting (not names)

**Rationale**:
- Pane IDs (`%5`) are stable across reconnections
- Window names can be changed by agent or user
- IDs unambiguous, names can collide

**Trade-off**: Must store pane ID in session state, can't rely on name matching.

### 4. Subscriptions over Polling

**Rationale**:
- Control mode pushes `%output` automatically
- No need to poll `capture-pane` for updates
- Lower latency, less tmux load

**Trade-off**: Must manage subscription lifecycle (prevent leaks).

### 5. Scrollback Capture on Connect

**Rationale**:
- User expects to see history when opening terminal
- Subscriptions only capture live output (post-subscribe)
- `capture-pane -S -2000` provides bootstrap history

**Trade-off**: One-time overhead on WebSocket connection.

### 6. Literal Mode for Input (`send-keys -l`)

**Rationale**:
- Preserves special characters (Ctrl-C, arrows, etc.)
- Prevents tmux from interpreting keys as commands
- User input sent exactly as typed

**Trade-off**: Cannot send tmux key names (but user doesn't need to).

### 7. 12-Hour Host Expiry

**Rationale**:
- Matches infrastructure policy for on-demand instances
- Forces cleanup of idle hosts
- Prevents unlimited cost accumulation

**Trade-off**: User must reprovision after 12h (acceptable for dev workflow).

### 8. Concurrent Command Safety via Mutex

**Rationale**:
- Multiple goroutines can spawn sessions simultaneously
- Interleaved stdin writes corrupt command stream
- Mutex serializes writes, preserves command boundaries

**Trade-off**: Small latency increase (negligible for spawn operations).

## Conclusion

This architecture enables Schmux to orchestrate agents on remote hosts with minimal complexity. By leveraging tmux Control Mode as the transport, the system gains session persistence, output streaming, and input handling without deploying custom agents. The developer experience mirrors local sessions while transparently handling authentication, provisioning, and reconnection.
