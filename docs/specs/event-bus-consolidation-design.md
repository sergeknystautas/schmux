# Event Bus Consolidation

## Problem

The daemon routes agent signals through ad-hoc callbacks (`SetSignalCallback`, `SetLifecycleCallback`, etc.) wired in `daemon.go`. The floor-manager-v2 branch introduced a second file watcher (`EventWatcher` for JSONL event files) alongside the original `FileWatcher` (for signal files). Both watchers invoke the same signal callback, which increments `NudgeSeq` and broadcasts to the frontend — producing **double audio pings** on every agent status change.

The frontend compounds this with two independent `useEffect` hooks for notifications: one for nudge seq changes and one for escalation. These can stack, producing a third ping when timing overlaps.

The root cause is structural: there is no single event bus, so each new feature (floor manager, escalation, unified events) added another callback path without deduplication.

## Design

### Single source of truth

The JSONL event file (`$SCHMUX_EVENTS_FILE`) becomes the sole agent→daemon communication channel. The signal file (`$SCHMUX_STATUS_FILE`) and its `FileWatcher` are deleted entirely.

- Hooks write only to `$SCHMUX_EVENTS_FILE` (no more dual-write)
- `EventWatcher` is the sole file-based ingestion path for local sessions
- Remote sessions switch from `cat`-ing the signal file to `tail -f`-ing the event file
- `ParseSignalFile()` is removed; remote watcher parses JSON lines via `event.ParseEvent()`

### In-process event bus

A new `internal/bus/` package provides a simple in-process pub/sub mechanism:

```go
type Bus struct { ... }

func New() *Bus
func (b *Bus) Publish(event Event)
func (b *Bus) Subscribe(handler Handler, eventTypes ...string) func()
```

Each published event receives a monotonic `Seq uint64`. Dispatch is synchronous fan-out, one goroutine per handler so slow consumers don't block others. No persistence, no replay, no buffering beyond Go channels.

The bus sits between file-based ingestion and daemon-internal routing:

```
Agent → Event file (JSONL append) → EventWatcher → Bus → Consumers
                                                       → Dashboard broadcaster
                                                       → Floor manager injector
                                                       → Lore collector
```

### Event types on the bus

Only signal/notification-related events flow through the bus. Independent subsystems (git watcher, overlay sync, preview autodetect, tunnel status) keep their existing direct callbacks.

| Event type           | Source                             | Description                                          |
| -------------------- | ---------------------------------- | ---------------------------------------------------- |
| `agent.status`       | EventWatcher / RemoteSignalWatcher | Agent state change (working, completed, error, etc.) |
| `agent.lore`         | EventWatcher                       | Failure, reflection, or friction entry               |
| `session.created`    | Session manager                    | New session spawned                                  |
| `session.disposed`   | Session manager                    | Session disposed                                     |
| `workspace.created`  | Workspace manager                  | New workspace created                                |
| `workspace.deleted`  | Workspace manager                  | Workspace deleted                                    |
| `escalation.set`     | API handler (POST /api/escalate)   | Floor manager requesting operator attention          |
| `escalation.cleared` | API handler (DELETE /api/escalate) | Operator dismissed escalation                        |
| `nudgenik.result`    | NudgeNik poller                    | LLM-classified terminal state                        |

### Producers

**EventWatcher (local agent → bus):** Unchanged file-watching logic. Instead of invoking a callback, publishes `agent.status` or `agent.lore` to the bus based on event type.

**RemoteSignalWatcher (remote agent → bus):** The watcher script changes from watching the signal file to tailing the event file:

```bash
tail -n0 -f "$EVENTS_FILE" | while IFS= read -r line; do
  echo "__SCHMUX_SIGNAL__${line}__END__"
done
```

`RemoteSignalWatcher.ProcessOutput` parses JSON lines via `event.ParseEvent()` then publishes to the bus.

**Session/Workspace managers (lifecycle → bus):** Replace `SetLifecycleCallback()` with direct `bus.Publish()` calls. Managers receive a `*bus.Bus` reference.

**NudgeNik poller (terminal scraping → bus):** Replaces direct state mutation + broadcast with `bus.Publish(nudgenik.result, ...)`.

**Escalation API (operator → bus):** Replaces direct state mutation + broadcast with `bus.Publish(escalation.set, ...)` or `bus.Publish(escalation.cleared, ...)`.

### Consumers

**Dashboard broadcaster:** Subscribes to all bus event types. On any event, updates the relevant state field (nudge, escalation, etc.) and calls `BroadcastSessions()` (debounced 100ms). Replaces `HandleAgentSignal`, the nudgenik state update, and the escalation state update — all three collapse into one subscriber.

`NudgeSeq` increments only for attention-worthy `agent.status` events (`completed`, `needs_input`, `needs_testing`, `error`). Status updates to `working` update the display but don't increment the seq — no sound for routine work.

**Floor manager injector:** Subscribes to `agent.status`, `session.created`, `session.disposed`. Formats `[SIGNAL]` and `[LIFECYCLE]` messages, debounces, sends to tmux. Filters out the floor manager's own session. Handles `rotate` state by triggering rotation.

**Escalation consumer:** Subscribes to `agent.status` from the floor manager session. Clears the escalation field when the floor manager sends its next status signal. Replaces the inline check in the current signal callback.

**Lore collector:** Subscribes to `agent.lore`. Accumulates entries in memory per workspace. On `session.disposed`, flushes to the lore curator. Replaces the current approach of re-reading all event files from disk on dispose.

### Frontend changes

The WebSocket payload format is unchanged. The bus is a backend-internal change.

**Single notification effect:** The two `useEffect` hooks in `SessionsContext.tsx` (nudge seq tracking + escalation tracking) merge into one. A single pass checks all sessions for new `nudge_seq` values and new escalation strings. At most one `playAttentionSound()` call per update cycle. Escalation additionally triggers `showBrowserNotification()`.

If an agent hits `error` and the floor manager escalates in the same broadcast batch, one ping fires — not two or three.

**Escalation uses NudgeSeq:** When the bus publishes `escalation.set`, the dashboard consumer increments the floor manager session's `NudgeSeq`. The frontend's existing seq-based dedup handles it — no separate tracking ref needed.

**localStorage for ack:** `schmux:ack:{sessionId}` continues tracking the last-seen seq per session. On page load, initialize to the current seq from the first WebSocket message so fresh tabs don't replay old sounds.

## What gets deleted

**Files removed:**

- `internal/signal/filewatcher.go` and its test
- `dualWriteCommand()`, `dualWriteCommandWithIntent()`, `dualWriteCommandWithBlockers()` in `ensure/manager.go`

**Code removed:**

- `$SCHMUX_STATUS_FILE` env var from session spawn
- `.schmux/signal/` directory creation
- `signalFilePath` parameter threading through tracker creation
- `ParseSignalFile()` in `signal/signal.go`
- All `SetXxxCallback()` methods: `SetSignalCallback`, `SetLifecycleCallback`
- The ~80 lines of callback wiring in `daemon.go` (lines 458-580)
- Second `useEffect` for escalation tracking in `SessionsContext.tsx`
- `.schmux/signal/` directory creation on remote hosts

**Kept but modified:**

- `signal/remotewatcher.go` — parses JSONL instead of signal file format
- `signal/signal.go` — keeps `Signal` struct, `IsValidState()`, `MapStateToNudge()`; removes parsing functions
- `event/watcher.go` — unchanged, becomes the sole file watcher
- `ensure/manager.go` — hook commands simplified to event-file-only writes

## Implementation order

1. **Create `internal/bus/`** — Bus struct, Event type, Publish, Subscribe. Pure library, unit-testable in isolation.
2. **Wire consumers** — Dashboard broadcaster, floor manager injector, escalation consumer, lore collector as bus subscribers. Temporarily coexist with old callbacks during development.
3. **Switch producers** — EventWatcher, RemoteSignalWatcher, session/workspace managers, nudgenik, escalation API publish to the bus. Remove old `SetXxxCallback()` wiring.
4. **Delete signal file system** — Remove FileWatcher, dual-write commands, `$SCHMUX_STATUS_FILE`, `.schmux/signal/` creation, `ParseSignalFile`. Update hooks to event-file-only writes. Update remote watcher script.
5. **Frontend consolidation** — Merge notification effects, verify single-ping behavior.
6. **Cleanup** — Remove dead code, update tests, update `docs/api.md`.

## Risks

**Remote watcher script:** `tail -f` behavior varies across Linux distributions (buffering, inotify support). Test against actual remote hosts before committing. The existing `inotifywait` fallback pattern should be preserved for the event file variant.

## Out of scope

- Routing git/overlay/preview/tunnel events through the bus — these are independent subsystems with single producers and consumers that work correctly today
- Event replay or persistence in the bus — the JSONL files serve as the durable log if needed
- Server-side ack tracking — localStorage is sufficient for a single-user local tool
- New notification sounds or priority levels — one sound, one browser notification type
