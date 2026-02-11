# Agent Signal Reliability Design

## Problem

Agents communicate status back to schmux by emitting text markers (`--<[schmux:state:message]>--`) in their terminal output. This mechanism has several reliability problems:

1. **Signal detection is coupled to browser tabs** — parsing happens in the WebSocket handler, so signals emitted when no browser tab is viewing the session are silently lost.
2. **Chunk-splitting** — the tracker reads PTY output in 8KB chunks. A signal marker straddling two reads is never matched.
3. **Fragile ANSI stripping** — four regex patterns strip escape sequences before signal matching. Unknown sequence types (DCS, APC, etc.) leave raw bytes that break the regex. Fails silently.
4. **No deduplication** — signals have no unique ID. The frontend replays notification sounds on page reload because its state resets.
5. **`LastSignalAt` not persisted** — tagged `json:"-"`, so daemon restarts cause NudgeNik to immediately override agent-sourced nudges with LLM inference.
6. **No diagnostics** — missed signals produce no log output. Failures are invisible.
7. **Remote sessions have no signal detection** — `handleRemoteTerminalWebSocket()` doesn't parse signals at all.

## Constraints

- The terminal stream is the only universal transport. Remote hosts may only expose a single xterm stream over SSH — no filesystem access, no HTTP sideband.
- Agents emit signals as printable text because every layer in the chain (LLM → harness → shell → tmux) passes printable text through without interpretation. Escape-sequence-based approaches (OSC) risk interception or mangling by intermediate layers.
- The local daemon has full system access (filesystem, network, etc.). The constraint is specifically on the agent-to-daemon leg.

## Design

### 1. Move signal detection into the tracker

Signal parsing moves from the WebSocket handler into the `SessionTracker` read loop. The tracker parses every chunk unconditionally, whether or not a browser tab is connected.

**Current flow:**

```
PTY → tracker.attachAndRead() → clientCh → WebSocket handler → ParseSignals()
                                                 ↓
                                          handleAgentSignal()
```

**New flow:**

```
PTY → tracker.attachAndRead() → SignalDetector.Feed() → signalCallback()
                               ↓                              ↓
                           clientCh                   handleAgentSignal()
                               ↓
                    WebSocket handler (display only)
```

The tracker receives a `signalCallback func(Signal)` at construction time. The WebSocket handler becomes a pure terminal display pipe — it forwards bytes to the browser and handles input/resize, but no longer does signal parsing.

### 2. Line accumulator to solve chunk-splitting

Instead of parsing raw chunks directly, a line accumulator buffers incomplete lines and only parses complete lines for signals.

The `SignalDetector` struct (see section 5) maintains a `buf []byte`:

1. Prepend any leftover `buf` to the new chunk
2. Find the last newline in the combined data
3. Everything up to and including the last newline → **complete lines** → parse for signals
4. Everything after the last newline → store in `buf` for next iteration
5. Forward the original unmodified chunk to `clientCh` — the accumulator only affects signal parsing, not display

**Flush timeout:** If `buf` has content and no new data arrives for 500ms, parse whatever is buffered. This handles the case where a signal is the last thing an agent outputs before going idle (no trailing newline arrives).

**Bounded buffer:** If `buf` exceeds 4KB without seeing a newline, truncate from the front. Terminal lines are never that long in practice.

**Key property:** The display path (`clientCh`) is completely unaffected. Raw chunks go through with zero latency impact. The accumulator is a parallel path for signal parsing only.

### 3. State-machine ANSI stripping

Replace the four-regex `stripANSIBytes()` in `signal.go` with a single-pass state machine modeled on the existing `stripTerminalControl()` in `tracker.go`.

States: `Normal`, `Escape`, `CSI`, `OSC`, `DCS`, `APC`.

Transitions:

- `Normal`: output the byte. On `\x1b` → `Escape`.
- `Escape`: `[` → `CSI`, `]` → `OSC`, `P` → `DCS`, `_` → `APC`, anything else → `Normal`.
- `CSI`: accumulate parameter bytes (`0x30-0x3F`) and intermediate bytes (`0x20-0x2F`) until final byte (`0x40-0x7E`):
  - Final byte `C` → emit N spaces (cursor forward, preserves word boundaries)
  - Final byte `B` → emit N newlines (cursor down, preserves line boundaries)
  - Any other final byte → emit nothing
  - → `Normal`
- `OSC`: consume until `BEL` (`0x07`) or `ST` (`\x1b\\`) → `Normal`.
- `DCS`, `APC`: consume until `ST` (`\x1b\\`) → `Normal`.

**Advantages over regex approach:**

- Complete coverage — unknown escape sequences are consumed, not left as raw bytes
- Single pass, O(n), no backtracking risk
- Follows ECMA-48 terminal protocol structure
- Already proven in the codebase (`stripTerminalControl`)

### 4. Signal deduplication and state persistence

**Persisted fields on Session:**

```go
type Session struct {
    // ...existing fields...
    NudgeSeq     uint64    `json:"nudge_seq"`
    LastSignalAt time.Time `json:"last_signal_at"`
}
```

`LastSignalAt` loses its `json:"-"` tag — it gets persisted to `state.json`. After daemon restart, NudgeNik can check whether a session recently signaled and skip unnecessary LLM inference.

`NudgeSeq` is a monotonic counter per session. Every time the tracker detects a signal, it increments the counter. The sequence number is included in the dashboard WebSocket broadcast so the frontend can deduplicate (see section 6).

**Scrollback parsing on tracker startup:**

When a tracker first attaches to a tmux session (including after daemon restart), it captures the last N lines of scrollback and runs signal parsing on them. Signals that would produce a `NudgeSeq` ≤ the persisted value are ignored. Only signals with a higher sequence are processed. This covers signals emitted during the brief window while the daemon was down.

### 5. SignalDetector — reusable component

Extract the line accumulator + state-machine ANSI stripping + signal regex matching into a single reusable struct:

```go
// Package signal

type SignalDetector struct {
    buf       []byte        // line accumulator for incomplete lines
    callback  func(Signal)  // fires on each detected signal
    lastData  time.Time     // timestamp of last Feed() call, for flush timeout
}

// Feed processes raw terminal bytes. Complete lines are parsed for signals
// immediately. Incomplete trailing lines are buffered until the next Feed()
// or Flush() call.
func (d *SignalDetector) Feed(data []byte)

// Flush force-parses any buffered incomplete line. Called on a timer
// when no new data has arrived for the flush timeout period.
func (d *SignalDetector) Flush()
```

Both local `SessionTracker` and remote session monitors use the same `SignalDetector`. The parsing logic exists in exactly one place.

### 6. Frontend notification deduplication

The frontend uses `NudgeSeq` (from the dashboard WebSocket broadcast) and `localStorage` to deduplicate notifications across page reloads, tab switches, and reconnections.

```typescript
// On receiving session state update:
const lastAckedSeq = parseInt(localStorage.getItem(`schmux:ack:${sessionId}`) || '0', 10);

if (session.nudge_seq > lastAckedSeq && isAttentionState(session.nudge_state)) {
  shouldPlaySound = true;
  localStorage.setItem(`schmux:ack:${sessionId}`, String(session.nudge_seq));
}
```

**Properties:**

- Page reload — `localStorage` survives, stale nudges don't re-trigger sounds
- Multiple tabs — shared `localStorage`, so a nudge acknowledged in one tab won't re-fire in another
- New nudge after reload — agent emits new signal, `NudgeSeq` increments past acked value, sound plays correctly
- Daemon restart — `NudgeSeq` is persisted in state.json, stays monotonic, frontend acked values remain valid

**Cleanup:** When sessions are disposed, their `localStorage` entries are removed. Entries for session IDs not in the current session list are pruned periodically.

`prevNudgeStatesRef` is replaced entirely by the `localStorage`-based approach.

### 7. Remote session signal detection

Remote sessions currently have no signal parsing. The fix mirrors the local `SessionTracker` model.

**Lightweight remote signal monitor:** A goroutine per remote session that subscribes to the remote output channel and feeds each event to a `SignalDetector`. It lives in the session manager, independent of WebSocket connections.

It does NOT need the full `SessionTracker` machinery (PTY management, resize, input forwarding). It only subscribes to the existing `RemoteConnection.SubscribeOutput()` channel and runs `SignalDetector.Feed()` on each event.

**Scrollback on startup:** Same as local. The remote connection supports `CapturePaneLines()`. On startup, capture scrollback, parse for signals, ignore any with sequence ≤ persisted `NudgeSeq`.

### 8. Diagnostic logging

When the state machine strips ANSI from a complete line and the cleaned result contains the substring `schmux` but does NOT match the bracket pattern, log a warning:

```
[signal] abc12345 - potential missed signal: "--<[schmux:completed:Taskdone]>--"
```

This provides a diagnostic trail for previously invisible failures. The log tells you exactly what the cleaned line looked like after ANSI stripping, so you can see why the regex didn't match (concatenated words, malformed marker, etc.).

## Files Changed

| File                                                | Change                                                                                                                                            |
| --------------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------- |
| `internal/signal/signal.go`                         | Replace regex-based `stripANSIBytes()` with state machine. Add `SignalDetector` struct with `Feed()`/`Flush()`. Add near-miss diagnostic logging. |
| `internal/signal/signal_test.go`                    | Update tests for state machine. Add chunk-split tests. Add flush-timeout tests. Add near-miss logging tests.                                      |
| `internal/session/tracker.go`                       | Add `signalCallback` field. Instantiate `SignalDetector` in read loop. Feed chunks to detector. Add scrollback parsing on startup.                |
| `internal/state/state.go`                           | Remove `json:"-"` from `LastSignalAt`. Add `NudgeSeq uint64` field to `Session`.                                                                  |
| `internal/dashboard/websocket.go`                   | Remove `signal.ParseSignals()` call from WebSocket handler. Remove signal-related imports if unused. Handler becomes display-only.                |
| `internal/dashboard/handlers.go`                    | Include `nudge_seq` in session response JSON.                                                                                                     |
| `internal/session/manager.go`                       | Wire `signalCallback` when creating trackers. Create remote signal monitors for remote sessions.                                                  |
| `internal/daemon/daemon.go`                         | NudgeNik check uses persisted `LastSignalAt` (no behavior change, just works across restarts now).                                                |
| `assets/dashboard/src/contexts/SessionsContext.tsx` | Replace `prevNudgeStatesRef` with `localStorage`-based deduplication using `nudge_seq`.                                                           |
| `assets/dashboard/src/lib/types.ts`                 | Add `nudge_seq` to `SessionResponse`.                                                                                                             |

## Backwards Compatibility

- The bracket marker format (`--<[schmux:state:message]>--`) is unchanged. No agent re-provisioning required.
- The dashboard WebSocket protocol gains a `nudge_seq` field but is otherwise unchanged. Old frontends ignore the new field.
- State.json gains `nudge_seq` and `last_signal_at` fields per session. Existing state files load correctly — Go zero-values the new fields.
