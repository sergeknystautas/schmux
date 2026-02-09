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
<remote-connect-cmd> <flavor-or-host> tmux -CC new-session -A -s schmux
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

**Create hidden window for command execution:**
```
new-window -d -n schmux-cmd -P -F '#{window_id} #{pane_id}' sh -c 'cd /path && command; echo __SCHMUX_DONE_uuid__'
```
The `-d` flag prevents the window from stealing focus. Used by `RunCommand` to execute VCS commands.

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
      "reconnect_command": "cloud-ssh reconnect {{.Hostname}}",
      "provision_command": "git clone {{.Repo}} {{.WorkspacePath}} && cd {{.WorkspacePath}} && git checkout {{.Branch}}",
      "hostname_regex": "Connected to host: (\\S+)"
    },
    {
      "id": "ssh_remote",
      "flavor": "dev.example.com",
      "display_name": "SSH Remote Server",
      "vcs": "git",
      "workspace_path": "~/workspace",
      "vscode_command_template": "{{.VSCodePath}} --remote ssh-remote+{{.Hostname}} {{.Path}}"
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
  - Default: `ssh -tt {{.Flavor}} --`
  - Schmux appends `tmux -CC new-session -A -s schmux` automatically. Include any separator your transport needs (e.g., `--` for SSH) in your command.
  - Examples:
    - SSH: `ssh -tt {{.Flavor}} --`
    - Cloud provider: `cloud-ssh connect {{.Flavor}}`
    - Docker: `docker exec -it {{.Flavor}}`
    - AWS SSM: `aws ssm start-session --target {{.Flavor}}`
- `reconnect_command` (optional): Go template for reconnecting to an existing remote host
  - Template variables: `{{.Hostname}}` - remote hostname, `{{.Flavor}}` - flavor identifier
  - Default: `ssh -tt {{.Hostname}} --`
  - Schmux appends `tmux -CC new-session -A -s schmux` automatically. Include any separator your transport needs (e.g., `--` for SSH) in your command.
  - Falls back to `connect_command` if not specified
- `provision_command` (optional): Go template for one-time workspace provisioning on first connection
  - Template variables: `{{.WorkspacePath}}`, `{{.Repo}}`, `{{.Branch}}`, `{{.VCS}}`
  - Runs once after initial connection, before creating any sessions. Reconnecting skips this step.
  - If empty, assumes workspace is pre-provisioned (e.g., cloud development environments)
  - Example: `git clone {{.Repo}} {{.WorkspacePath}} && cd {{.WorkspacePath}} && git checkout {{.Branch}}`
- `vscode_command_template` (optional): Per-flavor Go template for launching VS Code on remote workspaces
  - Template variables: `{{.VSCodePath}}` - local VSCode path, `{{.Hostname}}` - remote hostname, `{{.Path}}` - remote workspace path
  - Overrides the global `remote_workspace.vscode_command_template` for this flavor
  - If empty, falls back to the global setting, then to the default
  - Example: `{{.VSCodePath}} --remote ssh-remote+jump-{{.Hostname}} {{.Path}}`
- `hostname_regex` (optional): Regular expression for extracting the hostname from provisioning output
  - The first capture group is used as the hostname
  - Default: `Establish ControlMaster connection to (\\S+)`
  - Examples:
    - Custom banner: `Connected to host: (\\S+)`
    - IP address: `allocated (\\d+\\.\\d+\\.\\d+\\.\\d+)`

**Remote Workspace Configuration (global):**
- `vscode_command_template` (optional): Global fallback Go template for opening VS Code on remote workspaces. Per-flavor `vscode_command_template` overrides this.
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
   Internally executes: `ssh -tt dev.example.com -- tmux -CC new-session -A -s schmux`

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
   - Connect: `cloud-ssh connect gpu-large tmux -CC new-session -A -s schmux`
   - Reconnect: `cloud-ssh reconnect host123.example.com tmux -CC new-session -A -s schmux`

3. **SSH with Custom Options**:
   ```json
   {
     "id": "ssh_custom",
     "flavor": "jumphost.example.com",
     "display_name": "Via Jump Host",
     "vcs": "git",
     "workspace_path": "~/code",
     "connect_command": "ssh -J bastion.example.com {{.Flavor}} --"
   }
   ```
   Internally executes: `ssh -J bastion.example.com jumphost.example.com -- tmux -CC new-session -A -s schmux`

**Key Design Principle:**

User configuration focuses on **host connectivity**. Schmux automatically appends `tmux -CC new-session -A -s schmux` to your command. You include any transport-specific separators (like `--` for SSH) in your command template.

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
      "status": "connected",
      "provisioned": true
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
- `status`: "provisioning" | "authenticating" | "connected" | "disconnected" | "expired" | "reconnecting"
- `provisioned`: Whether the workspace has been provisioned (via `provision_command`)

**Session Extensions**:

```json
{
  "sessions": [
    {
      "id": "claude-xyz789",
      "remote_host_id": "remote-abc123",
      "remote_pane_id": "%5",
      "remote_window": "@3",
      "status": "running",
      // ... other fields
    }
  ]
}
```

- `remote_host_id`: Empty for local sessions, host ID for remote
- `remote_pane_id`: tmux pane ID on remote (e.g., "%5")
- `remote_window`: tmux window ID on remote (e.g., "@3")
- `status`: Remote session status: "provisioning" | "running" | "failed"

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
   remote-connect gpu:ml-large tmux -CC new-session -A -s schmux
   ```

2. **Parse PTY output** for provisioning info:
   - Match connection establishment patterns to extract hostname (configurable via `hostname_regex`)
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

**Trigger**: User clicks "Reconnect" on a disconnected host in the dashboard.

Reconnection is **never automatic**. When the daemon restarts, stale hosts (those with status "connected" in persisted state but no live SSH/ET process) are marked as "disconnected" immediately. This preserves the workspaces and sessions in the sidebar so the user can reconnect when ready. Automatic reconnection is not viable because it typically requires interactive authentication (e.g., Yubikey touch, 2FA) that can only happen through the dashboard's provisioning terminal.

**Steps**:

1. **User clicks "Reconnect"**:
   - Dashboard POSTs to `/api/remote/hosts/{id}/reconnect`
   - `StartReconnect` creates a new `Connection` with the stored hostname
   - Returns a provisioning session ID for WebSocket terminal streaming

2. **Spawn with hostname**:
   ```bash
   dev connect -n host-456.example.com -- tmux -CC new-session -A -s schmux
   ```
   Uses the `reconnect_command` template with `{{.Hostname}}` resolved.

3. **Authentication** (interactive, user-driven):
   The provisioning terminal is streamed to the dashboard via WebSocket. The user
   interacts directly (e.g., touches Yubikey, enters 2FA code). There is no timeout
   pressure — the connection waits for the user to complete authentication.

4. **Attach to existing tmux session**:
   - `new-session -A -s schmux` attaches to the existing session if it exists
   - All previous windows (agent sessions) are still running on the remote host
   - tmux sessions persist independently of SSH/ET connections

5. **Rediscover and reconcile sessions**:
   - Run `list-windows -F '#{window_id} #{window_name} #{pane_id}'`
   - Match discovered windows to sessions in state by window ID or pane ID
   - Update matched sessions to status "running"
   - Mark unmatched sessions as "disconnected"

6. **Resume output streaming**:
   - Resubscribe to `%output` for rediscovered panes
   - Capture scrollback for history

7. **Process monitoring**:
   - A `monitorProcess` goroutine watches the SSH/ET process
   - If the process exits unexpectedly, the connection status is immediately
     updated to "disconnected" and the dashboard reflects this in real time

### 3. Disconnection

**Triggers**:
- User closes laptop (network interruption)
- User clicks "Disconnect" in UI
- SSH/ET process crashes or exits
- Daemon restart (all connections are lost)

**Behavior**:
- Local: Update state to status="disconnected"
- Remote: Sessions keep running (tmux persists independently)
- UI: Show "Disconnected" badge with "Reconnect" button
- A `monitorProcess` goroutine detects unexpected process exits and updates
  the status immediately, so the dashboard always reflects reality

**Daemon restart**: When the daemon starts, all hosts that were "connected"
in persisted state are immediately marked as "disconnected" (the SSH/ET
processes from the previous daemon are gone). Workspaces and sessions are
preserved in the sidebar. The user clicks "Reconnect" to re-establish the
connection with interactive authentication.

**Recovery**: User-initiated reconnection flow restores state (see above).

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
  4. Store session with remote_host_id + remote_pane_id + remote_window
```

**CreateWindow flow**:
1. Build command: `new-window -n name -c workdir -P -F '#{window_id} #{pane_id}' command`
2. Send to tmux stdin
3. Parse response: `@3 %5`
4. Store windowID (`remote_window`) and paneID (`remote_pane_id`)
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

### Remote Diff and Commit Graph

These existing endpoints transparently support remote workspaces. When the workspace has a `remote_host_id`, the backend delegates to remote handlers that execute VCS commands via `RunCommand`.

```
GET /api/diff/{workspace-id}

Response 200 (same format for local and remote):
{
  "workspace_id": "workspace-123",
  "repo": "my-app",
  "branch": "feature-x",
  "files": [
    {
      "new_path": "src/main.go",
      "old_content": "...",
      "new_content": "...",
      "status": "modified",
      "lines_added": 10,
      "lines_removed": 3,
      "is_binary": false
    }
  ]
}
```

```
GET /api/workspaces/{id}/git-graph?max_commits=200&context=5

Response 200 (same format for local and remote):
{
  "repo": "my-app",
  "nodes": [...],
  "branches": {...}
}
```

The VCS type is determined from the remote flavor's `vcs` field. Git and sapling workspaces produce identical response formats.

### Remote Flavor Management

```
GET /api/config/remote-flavors
Response: [{ id, flavor, display_name, vcs, workspace_path, connect_command, reconnect_command, provision_command, hostname_regex, vscode_command_template }, ...]

POST /api/config/remote-flavors
Body: { flavor, display_name, workspace_path, vcs, connect_command, reconnect_command, provision_command, hostname_regex, vscode_command_template }
Response: { id, ... } // ID auto-generated

PUT /api/config/remote-flavors/{id}
Body: { display_name, workspace_path, vcs, connect_command, reconnect_command, provision_command, hostname_regex, vscode_command_template } // flavor immutable

DELETE /api/config/remote-flavors/{id}
Response: 204
```

### Remote Host Status

```
GET /api/remote/flavor-statuses
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
GET /api/remote/hosts
Response: [
  {
    "id": "remote-abc123",
    "flavor_id": "gpu_ml_large",
    "display_name": "GPU ML Large",
    "hostname": "remote-host-456.example.com",
    "status": "connected",
    "connected_at": "2026-02-06T10:30:00Z",
    "expires_at": "2026-02-06T22:30:00Z",
    "provisioned": true,
    "provisioning_session_id": "provision-remote-abc123"
  },
  ...
]

POST /api/remote/hosts/connect
Body: { "flavor_id": "gpu_ml_large" }
Response 202: { "flavor_id": "gpu_ml_large", "status": "provisioning", "provisioning_session_id": "provision-remote-abc123" }

POST /api/remote/hosts/{id}/reconnect
Response 202: { "status": "reconnecting", "provisioning_session_id": "provision-remote-abc123" }

DELETE /api/remote/hosts/{id}
Response: 204
```

### Connection Progress Streaming

```
GET /api/remote/hosts/connect/stream?flavor_id=gpu_ml_large
Content-Type: text/event-stream

Server-Sent Events stream for provisioning progress.
Sends real-time status updates during connection setup.
125-second timeout with graceful degradation.

WebSocket /ws/provision/{provisioning_session_id}
WebSocket endpoint for raw PTY output during provisioning.
Shows authentication prompts, connection progress, etc.
```

## Implementation Components

### Control Mode Parser (`internal/remote/controlmode/parser.go`)

**Responsibility**: Parse stdin stream into structured events.

**Output channels**:
- `Responses()`: `%begin`/`%end`/`%error` blocks
- `Output()`: `%output` notifications
- `Events()`: `%window-add`, `%session-changed`, etc.
- `ControlModeReady()`: Closed when first `%` protocol line is detected

**Key function**: `UnescapeOutput(s string)` - converts octal to bytes.

### Control Mode Client (`internal/remote/controlmode/client.go`)

**Responsibility**: Send commands, correlate responses, manage subscriptions.

**Key methods**:
- `Execute(ctx, cmd string) (string, error)` - Send command, wait for response
- `CreateWindow(ctx, name, workdir, command) (windowID, paneID, error)`
- `SendKeys(ctx, paneID, keys) error`
- `SubscribeOutput(paneID) <-chan OutputEvent`
- `UnsubscribeOutput(paneID, chan)`
- `CapturePaneLines(ctx, paneID, lines) (string, error)`
- `RunCommand(ctx, workdir, command) (string, error)` - Execute a command in a hidden window and return output

**Concurrency safety**: `stdinMu sync.Mutex` protects stdin writes. FIFO queue correlates responses to requests.

**RunCommand flow**:

`RunCommand` executes arbitrary commands on the remote host by creating a hidden tmux window with a shell, typing the command via `send-keys`, and capturing the output:

1. Generate unique begin/end sentinels: `__SCHMUX_BEGIN_<uuid>__` / `__SCHMUX_END_<uuid>__`
2. Create a hidden window (`new-window -d`) with the default shell (no command embedded — avoids tmux quoting issues)
3. Wait briefly for the shell to initialize
4. Type the command via `send-keys -l` using `tmuxQuote` (double-quote escaping for tmux protocol): `echo <begin>; cd <workdir> && <command>; echo <end>`
5. Press Enter via `send-keys Enter`
6. Poll `capture-pane -p -S -50000` every 200ms until the end sentinel appears on its own line
7. Extract output between begin and end sentinels (skipping the shell's command echo)
8. Kill the window (via `defer` to guarantee cleanup)

**Why send-keys instead of embedding the command in new-window**: tmux's command parser uses single-quote semantics that differ from shell quoting. The `'\''` trick for embedded single quotes works in bash but not in tmux. VCS commands (especially sapling's `-T '{node}|...'` templates) contain single quotes, causing the tmux parser to misinterpret the command. The send-keys approach bypasses tmux's command parser entirely — keystrokes go directly to the shell.

**Quoting layers**: `tmuxQuote` (double quotes, escaping `\`, `"`, `$`) handles the tmux protocol layer. `shellQuote` (single quotes with `'\''`) handles the shell layer for arguments like `workdir`. The VCS command string is sent as-is since it's already properly formatted by the `CommandBuilder`.

The `-d` flag ensures the window doesn't steal focus from the user's active session. This mechanism is used by the remote diff and git graph handlers to run VCS commands on the remote host.

### VCS Command Builder (`internal/vcs/`)

**Responsibility**: Abstract VCS command syntax differences between git and sapling.

The `CommandBuilder` interface generates shell command strings for VCS operations. Each method returns a complete command string ready to be executed via `RunCommand`.

**Interface methods**:
- `DiffNumstat() string` - Numstat diff against HEAD
- `ShowFile(path, revision) string` - Show file at a revision
- `FileContent(path) string` - Read file from working directory
- `UntrackedFiles() string` - List untracked files
- `Log(refs, maxCount) string` - Commit log in parseable format (`hash|short_hash|message|author|timestamp|parents`)
- `LogRange(refs, forkPoint) string` - Log between fork point and refs
- `ResolveRef(ref) string` - Resolve ref to commit hash
- `MergeBase(ref1, ref2) string` - Find merge base
- `DefaultBranchRef(branch) string` - Upstream branch ref (e.g., `origin/main`)

**Implementations**:
- `GitCommandBuilder` - Generates git commands (e.g., `git diff HEAD --numstat`, `git show HEAD:path`)
- `SaplingCommandBuilder` - Generates sapling commands (e.g., `sl diff --numstat`, `sl cat -r .^ path`)

**Factory**: `NewCommandBuilder(vcsType string) CommandBuilder` - Returns `GitCommandBuilder` for "git"/empty, `SaplingCommandBuilder` for "sapling".

**Key sapling equivalences**:

| Operation | Git | Sapling |
|-----------|-----|---------|
| Diff numstat | `git diff HEAD --numstat` | `sl diff --numstat` |
| Show file at HEAD | `git show HEAD:file` | `sl cat -r .^ file` |
| Untracked files | `git ls-files --others --exclude-standard` | `sl status --unknown --no-status` |
| Resolve ref | `git rev-parse --verify HEAD` | `sl log -T '{node}' -r '.' --limit 1` |
| Merge base | `git merge-base ref1 ref2` | `sl log -T '{node}' -r 'ancestor(ref1, ref2)'` |
| Default branch ref | `origin/main` | `remote/main` |

### Connection Manager (`internal/remote/connection.go`)

**Responsibility**: Manage single remote host connection.

**Lifecycle**:
1. `NewConnection(cfg)` - Create struct
2. `Connect(ctx)` - Spawn remote connection command via PTY, parse output, initialize client, start process monitor
3. `Reconnect(ctx, hostname)` - Reuse existing hostname, same flow as Connect
4. `Close()` - Kill process, close pipes, update status to disconnected, unsubscribe all

**Process monitoring**: A `monitorProcess` goroutine (started in both `Connect` and `Reconnect`) calls `cmd.Wait()` to detect when the SSH/ET process exits. On unexpected exit, it calls `Close()` to update the status to "disconnected" and notify the dashboard. This is the sole caller of `cmd.Wait()` to avoid double-wait races.

**Key methods**:
- `RunCommand(ctx, workdir, command) (string, error)` - Execute a command on the remote host via hidden tmux window

**Key fields**:
- `cmd *exec.Cmd` - The remote connection process
- `pty *os.File` - PTY for interactive authentication
- `client *controlmode.Client` - Control mode interface
- `parser *controlmode.Parser` - Protocol parser
- `host *state.RemoteHost` - Current state
- `pendingSessions []PendingSession` - Queued sessions during provisioning

**Status tracking**: `onStatusChange` and `onProgress` callbacks notify manager of state transitions.

**Session queuing methods**:
- `QueueSession(ctx, sessionID, name, workdir, command) <-chan PendingSessionResult` - Queue a session for creation when connected
- `drainPendingQueue(ctx)` - Process all pending sessions after connection is ready

**PTY streaming methods**:
- `SubscribePTYOutput() chan []byte` - Subscribe to raw PTY output (for provisioning WebSocket)
- `UnsubscribePTYOutput(ch)` - Remove subscriber

### Remote Manager (`internal/remote/manager.go`)

**Responsibility**: Manage multiple remote hosts.

**Key methods**:
- `Connect(ctx, flavorID) (*Connection, error)` - Get/create connection
- `Reconnect(ctx, hostID) (*Connection, error)` - Reconnect by ID
- `StartConnect(flavorID) (provisioningSessionID, error)` - Non-blocking background connection
- `StartReconnect(hostID, onFail) (provisioningSessionID, error)` - Non-blocking background reconnection
- `MarkStaleHostsDisconnected() int` - Mark stale hosts as disconnected at daemon startup
- `GetConnection(hostID) *Connection` - Lookup connection
- `GetFlavorStatuses() []FlavorStatus` - Get status of all flavors
- `RunCommand(ctx, hostID, workdir, command) (string, error)` - Execute a command on a remote host

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

**Modified method**: `IsRunning(sessionID)` - checks via remote connection if remote. Returns true only if `RemotePaneID` is set (i.e., session has been created on the remote host, not just queued).

**New method**: `disposeRemoteSession(ctx, session)` - Kills remote window via control mode, removes session from state. Does not remove the workspace (shared across all sessions on the same host).

### Dashboard API Updates (`internal/dashboard/handlers.go`, `internal/dashboard/handlers_remote.go`)

**Modified**: `handleSpawnPost()` - Route to `SpawnRemote()` if `req.RemoteFlavorID != ""`. Auto-detects remote flavor when spawning into a remote workspace.

**Modified**: `handleSessionsGet()` - Include remote metadata in response.

**Modified**: `handleDiff()` - Detects remote workspaces (`ws.RemoteHostID != ""`) and delegates to `handleRemoteDiff()`.

**Modified**: `handleWorkspaceGitGraph()` - Detects remote workspaces and delegates to `handleRemoteGitGraph()`.

**New**: `handleRemoteDiff(w, r, ws)` - Executes VCS diff commands on the remote host via `RunCommand`:
1. Gets connection and VCS command builder from flavor config
2. Runs `DiffNumstat()` to get changed files with line counts
3. For each file: runs `ShowFile(path, "HEAD")` and `FileContent(path)` for old/new content
4. Runs `UntrackedFiles()` and fetches content for each
5. Returns same `DiffResponse` JSON format as local handler

**New**: `handleRemoteGitGraph(w, r, ws, maxCommits, contextSize)` - Builds commit graph from remote VCS:
1. Gets connection and VCS command builder
2. Detects default branch via `git symbolic-ref` on remote
3. Resolves HEAD and default branch ref via `ResolveRef()`
4. Finds fork point via `MergeBase()`
5. Runs `Log()` or `LogRange()` to get commits
6. Parses output with `workspace.ParseGitLogOutput()` (shared with local handler)
7. Builds graph with `workspace.BuildGraphResponse()` (shared with local handler)
8. Returns same `GitGraphResponse` JSON format

**Validation**: Skip repo/branch requirement when `RemoteFlavorID != ""`.

**Remote-specific handlers** (in `handlers_remote.go`):
- Remote flavor CRUD (GET/POST/PUT/DELETE)
- Remote host listing, connect, reconnect, disconnect
- Flavor status endpoint
- SSE connection progress streaming

### WebSocket Updates (`internal/dashboard/websocket.go`)

**Modified**: `handleTerminalWebSocket()` - Detect remote session, route to `handleRemoteTerminalWebSocket()`.

**New**: `handleProvisionWebSocket()` - Streams raw PTY output during provisioning for live terminal display.

**Remote streaming** (`handleRemoteTerminalWebSocket`):
1. Validate session has `RemotePaneID` (return 503 if still provisioning)
2. Subscribe to `conn.SubscribeOutput(paneID)`
3. Capture initial scrollback
4. Send "full" message
5. Stream "append" from output channel
6. Handle "input" → `conn.SendKeys()`
7. Periodic health check of remote connection
8. Cleanup: `defer conn.UnsubscribeOutput()`

### Workspace Manager Updates (`internal/workspace/manager.go`, `internal/workspace/git_graph.go`)

**Modified**: `UpdateGitStatus()` - Early return if `w.RemoteHostID != ""`.

**Modified**: `UpdateAllGitStatus()` - Skip remote workspaces in polling.

**Modified**: `Create()` - Don't add remote workspaces to git watcher.

**Exported graph functions** (`git_graph.go`): The following functions are exported for reuse by the remote git graph handler:
- `ParseGitLogOutput(output string) []RawNode` - Parses pipe-delimited log output into structured nodes
- `BuildGraphResponse(nodes, localBranch, defaultBranch, ...) *GitGraphResponse` - Builds the full graph response from raw nodes (topological sort, branch annotation, etc.)
- `RawNode` - Exported struct for parsed commit data
- `WalkBranchMembership(...)` - Marks nodes reachable from a head as belonging to a branch
- `NonNilSlice(s []string) []string` - Utility for JSON serialization

**Rationale**: Remote workspaces have no local git repo. Attempting git operations causes errors. The exported graph functions allow the remote handler to reuse the same graph building logic with data fetched via `RunCommand`.

### Dashboard Frontend Updates (`assets/dashboard/src/components/SessionTabs.tsx`)

**Modified**: Diff and commit graph tabs visibility logic.

Previously, the diff tab and commit graph tab were gated on `isGit` (`!workspace?.vcs || workspace.vcs === 'git'`), hiding them for non-git workspaces including remote workspaces with sapling.

Now uses `isVCS` which is `true` when:
- The workspace is remote (`remote_host_id` is set) — backend handles VCS abstraction
- VCS is "git" (or omitted, which defaults to git)
- VCS is "sapling"

This ensures diff and commit graph tabs appear for all remote workspaces regardless of VCS type. The tab labels remain "Diff" and "commit graph" for all VCS types since the backend normalizes the output format.

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

### 9. Hidden Window Command Execution (`RunCommand`)

**Rationale**:
- Remote diff/graph features require running VCS commands on the remote host
- No SSH or separate transport available — only tmux control mode
- Hidden windows (`new-window -d`) don't steal focus from user sessions
- Begin/end sentinel-based extraction cleanly separates command output from shell echo

**Approach**: Create hidden window with default shell → type command via `send-keys -l` → poll `capture-pane` for end sentinel → extract output between begin/end sentinels → kill window via `defer`.

**Why send-keys instead of embedding commands in new-window**: tmux's single-quote parser has no escape mechanism (unlike shell's `'\''` trick). VCS commands containing single quotes (especially sapling's `-T '{node}|...'` templates) caused tmux to misparse the command, the shell process exited immediately, and the window was destroyed before `capture-pane` could run. The send-keys approach bypasses tmux's command parser entirely.

**Trade-off**: Polling every 200ms adds latency vs event-driven approach. A brief 200ms delay for shell initialization is needed. Both are acceptable for diff/graph operations which are user-initiated and not latency-critical.

### 10. VCS Command Builder Abstraction

**Rationale**:
- Remote workspaces may use git or sapling
- Same diff/graph UI should work for both VCS types
- Command syntax differs significantly (e.g., `git show HEAD:file` vs `sl cat -r .^ file`)
- Interface pattern allows adding new VCS types without modifying handlers

**Trade-off**: Extra abstraction layer, but keeps handler code VCS-agnostic.

### 11. Shared Graph Building Logic

**Rationale**:
- Local and remote git graph handlers produce identical response formats
- Topological sort, branch annotation, and ISL-style ordering are complex algorithms
- Exporting `ParseGitLogOutput` and `BuildGraphResponse` prevents duplication
- Both handlers use the same pipe-delimited log format (`hash|short_hash|message|author|timestamp|parents`)

**Trade-off**: Exported functions increase the public API surface of the workspace package.

## Conclusion

This architecture enables Schmux to orchestrate agents on remote hosts with minimal complexity. By leveraging tmux Control Mode as the transport, the system gains session persistence, output streaming, and input handling without deploying custom agents. The `RunCommand` mechanism extends this to support VCS operations (diff, commit graph) on remote workspaces, with a command builder abstraction supporting both git and sapling. Reconnection is user-initiated (never automatic) to support interactive authentication flows like Yubikey/2FA, with process monitoring to detect and surface disconnections in real time. The developer experience mirrors local sessions while transparently handling authentication, provisioning, and reconnection.
