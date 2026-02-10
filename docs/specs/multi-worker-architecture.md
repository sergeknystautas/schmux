# Multi-Worker Architecture

Design notes for supporting multiple tmux workers (local and remote) from a single control plane.

**Status**: Exploratory / Not yet implemented

## Current State

Everything runs on a single machine with tight coupling:

- `internal/tmux` — standalone functions that shell out to `tmux` CLI on the default server (no `-L`/`-S` flags)
- `internal/session` — session manager calls tmux and workspace functions directly
- `internal/workspace` — git operations on local filesystem
- `internal/daemon` — holds session manager, serves dashboard, exposes HTTP API

The dashboard, session manager, tmux, and workspaces are all in the same process. There's no abstraction boundary between "where agents run" and "where the UI is served."

## Proposed Split

### Control Plane

The user-facing orchestration layer:

- Serves the web dashboard
- Exposes HTTP API for spawn/dispose/status
- Knows about N workers
- Routes spawn requests to appropriate workers
- Aggregates session state from all workers for unified UI
- Owns user-facing configuration (repos, presets, etc.)

### Worker

The execution layer — one per machine/environment where agents run:

- Owns tmux server(s) and session lifecycle
- Owns workspace directories and git operations
- Owns agent process lifecycle (PID tracking, cleanup)
- Owns terminal capture and output streaming
- Has local credentials (git SSH keys, API tokens for agents)
- Exposes a defined interface to the control plane

Each worker has its own session manager. The control plane doesn't manage sessions — it asks workers to manage sessions.

## Ownership Table

| Concern                        | Owner                                      |
| ------------------------------ | ------------------------------------------ |
| Git credentials, SSH keys      | Worker (pre-provisioned)                   |
| Workspace creation/cleanup     | Worker (on request from control plane)     |
| Agent binaries (claude, codex) | Worker (pre-provisioned)                   |
| Agent capabilities             | Worker advertises, control plane discovers |
| Prompts / spawn intent         | Control plane                              |
| Session lifecycle              | Worker (on request from control plane)     |
| Terminal output                | Worker (streams to control plane)          |
| Aggregated state / UI          | Control plane                              |
| User configuration             | Control plane                              |

## Worker Interface

Minimum surface area for control plane → worker communication:

### Session Operations

- **Spawn**(workspace spec, command, env, terminal size) → session handle
- **Dispose**(session handle)
- **IsRunning**(session handle) → bool
- **CaptureOutput**(session handle) → terminal content
- **SendKeys**(session handle, keys)
- **StreamOutput**(session handle) → stream

### Workspace Operations

- **GetOrCreateWorkspace**(repo URL, branch) → workspace ID + path
- **ListWorkspaces**() → []workspace

### Discovery

- **Capabilities**() → available agents, resource limits
- **Status**() → health, active session count

## Worker Lifecycle Models

### Managed Workers

Schmux creates and destroys them. Examples:

- Docker containers spun up on demand
- Cloud VMs provisioned via API

Control plane owns full lifecycle: provision → configure → use → tear down.

Use case: "burn tokens quickly" — spin up 10 containers, fan out work, tear down when done.

### External Workers

Someone else runs them; schmux just connects. Examples:

- Persistent dev machine
- Shared build server
- Pre-provisioned cloud instance

Worker registers with control plane or is configured manually. Schmux has no control over when it comes or goes — must handle disconnection gracefully.

## Worker Provider Layer

Above the worker interface, a provider layer that knows how to:

- Start/stop managed workers (docker API, cloud SDK)
- Register/deregister external workers
- Health-check both kinds
- Route spawn requests (by capacity, available agents, repo access)

The worker itself doesn't care whether it's managed or external — same interface either way.

## Communication Transports

### Option 1: gRPC / HTTP API on Worker

Each worker runs a small server. Control plane calls it directly.

**Pros:**

- Clean, well-understood
- Bidirectional streaming for terminal output
- Strong typing with gRPC

**Cons:**

- Every worker needs a listening port
- Network reachability required (firewall, NAT issues)
- Auth and TLS per worker

### Option 2: SSH as Transport

Control plane SSHes into workers and runs commands.

**Pros:**

- No extra server process on worker — just schmux binary + sshd
- Works naturally for external machines
- Auth via existing SSH keys

**Cons:**

- SSH is clunky as an RPC mechanism
- Encoding/decoding over stdin/stdout
- Connection overhead

### Option 3: Reverse Connection (Worker Phones Home)

Workers connect outbound to control plane via WebSocket or gRPC stream.

**Pros:**

- Solves reachability — workers behind NATs/firewalls just need outbound access
- Control plane doesn't need to know worker addresses upfront
- Standard pattern (Buildkite, GitHub Actions runners, etc.)

**Cons:**

- More complex connection management
- Reconnection logic
- Request multiplexing over persistent connections

### Option 4: Docker Exec / Unix Socket (Local Docker Only)

Control plane communicates via `docker exec` or mounted unix sockets.

**Pros:**

- Simple, no network auth
- No extra ports

**Cons:**

- Only works for local docker containers
- Not generalizable to remote workers

### Transport Recommendations by Worker Type

| Worker Type              | Natural Transport                |
| ------------------------ | -------------------------------- |
| Local docker containers  | Unix socket or docker exec       |
| Remote external machines | Reverse connection (phones home) |
| Remote managed VMs       | gRPC or reverse connection       |

The **reverse connection model** is most general — handles both managed and external workers, avoids firewall/NAT issues.

Example: `schmux worker --control-plane wss://host:7337/workers`

## Implementation Phases

### Phase 1: Extract Worker Interface (Local Only)

1. Define `Worker` interface matching current session manager surface
2. Wrap existing session manager behind interface
3. Daemon holds `[]Worker` instead of `*session.Manager`
4. Test with single local worker — no behavior change

### Phase 2: Multi-Worker Routing

1. Config supports multiple workers (even if both local)
2. Dashboard shows which worker owns each session
3. Spawn UI allows worker selection
4. State includes worker qualifier on sessions and workspaces

### Phase 3: Remote Workers

1. Implement reverse-connection transport
2. Worker binary with `schmux worker` subcommand
3. Control plane accepts worker registrations
4. Terminal streaming over the connection

### Phase 4: Managed Worker Providers

1. Docker provider — spin up/down containers
2. Provider interface for future backends (cloud VMs, etc.)
3. Elastic scaling for batch workloads

## Open Questions

- **Workspace identity across workers**: Same repo+branch on two workers — are they the same workspace or different? Probably different (worker-qualified).

- **Session migration**: Can a session move between workers? Probably not — would require workspace sync + tmux session serialization. Simpler to treat sessions as pinned to their worker.

- **Credential distribution**: If control plane knows git credentials, should it push them to managed workers? Or require workers to be pre-provisioned with credentials?

- **Failure handling**: What happens to sessions when a worker disconnects? Mark as unknown? Attempt reconnection? For managed workers, could restart the worker and recover.

- **Log persistence**: Currently logs are local files. With remote workers, logs live on the worker. Does control plane pull/archive them? Or just stream and discard?
