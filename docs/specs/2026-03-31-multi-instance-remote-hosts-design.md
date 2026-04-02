# Multi-Instance Remote Hosts

## Problem

Remote host flavors (e.g., "www", "gpu") map 1:1 to connections. You can only have one OD instance per flavor, forcing all sessions on that flavor to share a single host. This defeats schmux's core value proposition — workspace isolation — because agents can't get independent filesystems on separate OD instances of the same flavor.

## Design

Separate **flavor** (template) from **host** (instance). A flavor defines what kind of OD to provision. A host is a specific running OD. Multiple hosts can share the same flavor.

```
Flavor "www"  ───>  Host remote-a1b2c3d4 (devvm1234.od)  ───>  Sessions
              ───>  Host remote-e5f6g7h8 (devvm5678.od)  ───>  Sessions
```

### Core Decisions

| Decision                | Choice                                           | Rationale                                                                                                   |
| ----------------------- | ------------------------------------------------ | ----------------------------------------------------------------------------------------------------------- |
| Flavor:Host             | 1:N                                              | Flavor is a template, host is an instance                                                                   |
| Host:Workspace          | 1:1                                              | One OD = one workspace. ODs can't host multiple workspaces.                                                 |
| Host identity           | Generated UUID (`remote-{uuid8}`)                | Created at provision start, before hostname is known. Hostname is a display field populated asynchronously. |
| Host creation           | Inline during spawn                              | "+ Add workspace" with remote flavor provisions a new OD                                                    |
| Session spawn           | No host picker                                   | New workspace = new host. Add session = existing host's workspace.                                          |
| Home page               | Remote workspaces are peers alongside local ones | Uniform display, no grouping by type                                                                        |
| Reconnection            | Per-host                                         | Separate SSH auth per OD, no batch shortcuts                                                                |
| Expiry                  | Workspace persists as "expired"                  | User can dismiss. Session history preserved until dismissed.                                                |
| Data model              | Keep RemoteHost separate from Workspace          | Different lifecycle state machines (connection vs execution). Unify at the API layer for display.           |
| Concurrent provisioning | Fully concurrent                                 | Each provisioning is an independent SSH process. No artificial serialization.                               |

### One-Way Doors

- **RemoteHost identity model** — UUID as primary key, hostname as display field. All references key off this.
- **1:N flavor-to-host relationship** — `Manager.Connect()` always creates a new host instead of reusing.

### Two-Way Doors

- Spawn wizard UX details (safe to iterate)
- Expired workspace display treatment
- Adding user labels to hosts (additive, anytime)
- Batch reconnection (can add later if friction warrants it)
- Host deprovisioning / `TeardownCommand` (defer until needed)

## Data Model

### RemoteHost Stays Separate

`Workspace` and `RemoteHost` remain distinct entities with distinct lifecycle state machines:

- **Workspace** = code context (repo, branch, path, execution state)
- **RemoteHost** = infrastructure (hostname, expiry, connection state, provisioning)

The 1:1 relationship between them is maintained via `Workspace.RemoteHostID`. The API layer synthesizes both into `WorkspaceResponseItem` for the frontend, so the home page displays remote workspaces as peer cards without changing the underlying model.

### RemoteHost Fields

All existing `RemoteHost` fields are preserved:

| Field         | Purpose                                                                            | Change                                                                                 |
| ------------- | ---------------------------------------------------------------------------------- | -------------------------------------------------------------------------------------- |
| `UUID`        | Primary identity (`remote-{uuid8}`)                                                | No change — already generated, not flavor-derived                                      |
| `Hostname`    | Display name, extracted from provisioning output                                   | No change — populated asynchronously after extraction                                  |
| `ConnectedAt` | Connection timestamp                                                               | No change                                                                              |
| `ExpiresAt`   | `ConnectedAt + 12h`, drives expiry UI                                              | No change                                                                              |
| `Provisioned` | Tracks one-time provision command execution                                        | No change                                                                              |
| `Status`      | 6-state enum (provisioning/connecting/connected/disconnected/expired/reconnecting) | No change                                                                              |
| `FlavorID`    | Reference to the flavor template                                                   | **Already exists** — the key change is that multiple hosts can share the same FlavorID |

### Session Fields

Session remote fields are unchanged:

| Field          | Purpose                       | Change                                                        |
| -------------- | ----------------------------- | ------------------------------------------------------------- |
| `RemoteHostID` | Points to the RemoteHost UUID | No change — now one of potentially many hosts for that flavor |
| `RemotePaneID` | Tmux pane ID on remote host   | No change                                                     |
| `RemoteWindow` | Tmux window ID on remote host | No change                                                     |

Resolution path: `Session.RemoteHostID` → `remote.Manager.GetConnection(hostID)` → `*Connection`. Same as today.

### Connection Map

`remote.Manager` connection map is `hostID → *Connection` (already the case). The behavioral change is in `Manager.Connect()`: instead of checking for an existing connection for the flavor and reusing it, always create a new host.

`GetConnectionByFlavorID()` becomes `GetConnectionsByFlavorID()` (returns a slice). Callers that need a specific host use `GetConnection(hostID)` directly.

### Hostname Extraction Fallback

If `hostnameRegex` fails to match provisioning output:

1. Execute `hostname` over the SSH channel as a fallback
2. If that also fails, the workspace shows the UUID as display name
3. Hard timeout on `provisioning` state → transitions to `failed`

## Spawn Flow

### "+ Add workspace" (new workspace)

1. User clicks "+ Add workspace"
2. Spawn wizard shows flavor cards (local repo options + remote flavor options)
3. User picks a remote flavor (e.g., "www")
4. `RemoteHost` entry created immediately with `status: provisioning` and generated UUID
5. Provisioning starts — SSH connect, auth prompts streamed to browser via `/ws/provision/{id}`
6. Hostname extracted from provisioning output (or fallback)
7. Workspace created, linked to the new host via `RemoteHostID`
8. Session spawned on the new host via control mode `new-window`

This is the same flow as today, except it always creates a new host instead of reusing an existing one.

### "Add session" (existing workspace)

1. User is on a workspace/session page
2. Clicks "+" to add a session
3. Session is spawned on that workspace's host — no flavor selection, no provisioning
4. For remote: `conn.CreateSession()` via control mode

No changes needed here — this path doesn't touch host selection.

## Home Page

Remote workspaces appear as peer cards alongside local workspaces:

```
┌──────────────┐  ┌──────────────┐  ┌──────────────┐
│ www           │  │ www           │  │ fbsource      │
│ devvm1234.od  │  │ devvm5678.od  │  │ ~/worktree-1  │
│ 3 sessions    │  │ 1 session     │  │ 2 sessions    │
│ ● connected   │  │ ○ disconnected│  │               │
└──────────────┘  └──────────────┘  └──────────────┘
```

Each remote workspace card shows:

- Flavor name (as title)
- Hostname (as subtitle, falls back to UUID if hostname unknown)
- Session count
- Connection status indicator
- "Reconnect" button when disconnected
- "Expired" badge when TTL exceeded

## Reconnection

Each remote workspace has its own "Reconnect" action. No batch reconnect — each OD requires separate SSH authentication (Yubikey, 2FA).

On daemon restart, all remote workspaces are marked disconnected (SSH processes die with the daemon). Users reconnect individually as needed.

## Expiry

When an OD's TTL expires:

- Workspace card persists on the home page with "expired" status
- Sessions show as stopped/unreachable
- User can **dismiss** the workspace (removes RemoteHost + associated sessions from state.json, cleans up Connection from remote.Manager)
- No "replace" action — user creates a new workspace via "+ Add workspace" if needed (new OD = fresh filesystem, no continuity implied)
- Session history is preserved until the workspace is dismissed

## Host Deprovisioning

Schmux does not manage remote host lifecycle. OD cleanup is handled by external TTLs. A `TeardownCommand` flavor field may be added in the future if needed for non-TTL environments.

## Migration

The migration is **additive**, not structural:

- `RemoteHost` struct is unchanged — no field renames or merges
- The behavioral change is in `Manager.Connect()`: remove the "reuse existing connection for this flavor" check
- Existing state.json entries remain valid — single hosts per flavor are a valid subset of N hosts per flavor
- `Session.RemoteHostID` continues to work as-is
- Consider adding a schema version field to `state.json` opportunistically for future migrations
