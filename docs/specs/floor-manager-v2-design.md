# Floor Manager v2 Design

## Problem

The floor manager lacks situational awareness. It only receives agent state transitions (`[SIGNAL]` messages) but has no visibility into session/workspace lifecycle events and no mechanism to get the human operator's attention when intervention is needed.

## What Was Built (Prior to This Design)

### Stop Hook for Agent Status Reporting

Agents are forced to write a status update to `$SCHMUX_STATUS_FILE` before finishing each turn via a Claude Code Stop hook. The hook blocks the first stop attempt with a JSON `{"decision":"block"}` response, prompting the agent to write its current state. On the second attempt (`stop_hook_active: true`), the hook allows through and writes `completed` to the signal file.

Implementation: `.claude/hooks/stop-status-check.sh`, provisioned by `internal/provision/provision.go`.

### Session Lifecycle Events

The floor manager now receives `[LIFECYCLE]` messages when sessions are created or disposed. These fire from all spawn paths (`Spawn`, `SpawnCommand`, `SpawnRemote`) and dispose paths (`Dispose`, `disposeRemoteSession`). Floor manager's own sessions are excluded.

Format: `[LIFECYCLE] Session "name" created (id=..., target=..., workspace=..., branch=...)`

### Workspace Lifecycle Events

Same pattern for workspace create/dispose. Fires from `create`, `CreateLocalRepo`, `CreateFromWorkspace`, and `Dispose`.

Format: `[LIFECYCLE] Workspace created (id=..., branch=...)`

### Implementation Details

- `Injector.InjectLifecycle(msg string)` always queues (no signal filtering)
- `session.Manager.SetLifecycleCallback` and `workspace.Manager.SetLifecycleCallback`
- Wired through `daemon.go` with `fmMu`-protected injector access

## Escalation Mechanism (To Be Built)

### Overview

The floor manager needs a way to actively get the human operator's attention. This is a `schmux escalate` CLI command that triggers sound, visual, and browser notifications on the dashboard.

### Data Flow

1. Floor manager runs `schmux escalate "Agent X is stuck on auth"`
2. CLI sends POST to `/api/escalate` with message body
3. Daemon stores escalation on the floor manager session state
4. Daemon broadcasts updated session state over `/ws/dashboard`
5. Dashboard triggers: attention sound + browser Notification API + visual banner

### Escalation Clearing

The escalation clears when either:

- The operator dismisses the banner manually (sends `DELETE /api/escalate`)
- The floor manager's next signal arrives (any state change from the FM clears it automatically)

### Backend

**New CLI command** (`cmd/schmux/escalate.go`):

- `schmux escalate <message>` sends POST to `/api/escalate`
- Body: `{"message": "..."}`

**New API endpoints** (`internal/dashboard/handlers.go`):

- `POST /api/escalate` — stores escalation message on floor manager session, triggers broadcast
- `DELETE /api/escalate` — clears escalation, triggers broadcast

**State change** (`internal/state/state.go`):

- Add `Escalation` field (string) to `Session`
- Non-empty means active escalation

**Auto-clear** (`internal/daemon/daemon.go`):

- In the signal callback, when the floor manager's signal arrives, clear the `Escalation` field and re-broadcast

### Frontend

**Browser notifications** (`assets/dashboard/src/lib/browserNotification.ts`):

- Wrapper around the browser Notification API
- Request permission on first interaction or via settings
- Fire notification when escalation arrives and tab is not focused

**Home page banner** (`assets/dashboard/src/routes/HomePage.tsx`):

- Dismissible alert bar above the floor manager terminal
- Shows escalation message from the floor manager session
- Dismiss button sends `DELETE /api/escalate`

**Sound** (`assets/dashboard/src/contexts/SessionsContext.tsx`):

- Reuse existing `playAttentionSound()` when escalation appears

### Floor Manager Prompt Update

Add to `internal/floormanager/prompt.go`:

- `schmux escalate "<message>"` in the available commands section
- Guidance on when to escalate: agent blocked on human decision, unrecoverable error, situation the operator should know about

### Out of Scope

- **ntfy for mobile** — can be wired to the same endpoint later
- **Severity levels** — one level is enough; the message conveys urgency
- **Escalation history** — terminal history and memory.md serve as the log
- **Auto-escalation rules** — the floor manager uses its judgment; no automatic triggers

### Files Changed

| Component                    | File                                                |
| ---------------------------- | --------------------------------------------------- |
| CLI command                  | `cmd/schmux/escalate.go`                            |
| API endpoints                | `internal/dashboard/handlers.go`                    |
| Session state                | `internal/state/state.go`                           |
| Auto-clear wiring            | `internal/daemon/daemon.go`                         |
| FM prompt                    | `internal/floormanager/prompt.go`                   |
| Home page banner             | `assets/dashboard/src/routes/HomePage.tsx`          |
| Browser notifications        | `assets/dashboard/src/lib/browserNotification.ts`   |
| Sound + notification trigger | `assets/dashboard/src/contexts/SessionsContext.tsx` |
| API docs                     | `docs/api.md`                                       |
