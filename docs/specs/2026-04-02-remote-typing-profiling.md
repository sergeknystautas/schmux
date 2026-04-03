# Remote Typing Performance Profiling

**Date**: 2026-04-02
**Branch**: fix/remote-host-typing
**Goal**: Instrument the remote typing path to identify where latency accumulates, before making architectural changes.

## Problem

The `sendKeys` segment in the typing performance breakdown is a black box — it measures the entire `tracker.SendInput()` call as one number. For remote sessions, that single number hides three independent latency sources:

1. **Mutex contention** — waiting for `stdinMu` (shared across all sessions on a host)
2. **Execute round-trips** — each key run requires a full SSH round-trip to tmux
3. **Head-of-line blocking** — FIFO response queue means slow commands starve fast ones

Additionally, remote sessions have no baseline RTT measurement (health probes disabled) and no session type tagging in diagnostic messages.

## Changes — Three Levels

### Level 1: Break the SendKeys Black Box

#### New type: `controlmode.SendKeysTimings`

Defined once in `controlmode`, used throughout the chain. This follows the existing
precedent where `ControlSource.GetCursorState()` already returns `controlmode.CursorState`.

```go
// controlmode/keyclassify.go (co-located with ClassifyKeyRuns)

type SendKeysTimings struct {
    MutexWait    time.Duration // time blocked on stdinMu across all Execute() calls
    ExecuteNet   time.Duration // sum of Execute() round-trips, EXCLUDING mutex wait
    ExecuteCount int           // number of Execute() calls (= number of key runs)
}
```

**`MutexWait` and `ExecuteNet` are non-overlapping.** They partition the `SendKeys`
duration into contention time vs. actual work (stdin write + FIFO response wait).
The UI renders them as additive sub-segments that sum correctly:

```
sendKeys:  |---mutexWait---|---executeNet (stdin + FIFO)---|---classify overhead---|
```

#### Modified: `controlmode.Client.Execute` — Return Mutex Wait Directly

Change `Execute` to return the mutex wait duration as a stack-local value. This
eliminates the shared-mutable-state problem entirely — each caller gets its own
copy, safe under any concurrency pattern.

Current signature:

```go
func (c *Client) Execute(ctx context.Context, cmd string) (string, error)
```

New signature:

```go
func (c *Client) Execute(ctx context.Context, cmd string) (string, time.Duration, error)
```

Inside `Execute`, wrap the existing `stdinMu.Lock()` and return the timing:

```go
mutexStart := time.Now()
c.stdinMu.Lock()
mutexWait := time.Since(mutexStart)
_, err := fmt.Fprintf(c.stdin, "%s\n", cmd)
c.stdinMu.Unlock()
if err != nil {
    return "", mutexWait, fmt.Errorf("failed to send command: %w", err)
}
// ... wait for response ...
return resp.Content, mutexWait, nil
```

All existing callers of `Execute` update to `output, _, err := c.Execute(ctx, cmd)`.
This is a mechanical change (~20 call sites) fully enforced by the compiler.

#### Modified: `controlmode.Client.SendKeys`

Current signature:

```go
func (c *Client) SendKeys(ctx context.Context, paneID, keys string) error
```

New signature:

```go
func (c *Client) SendKeys(ctx context.Context, paneID, keys string) (SendKeysTimings, error)
```

Implementation:

```go
func (c *Client) SendKeys(ctx context.Context, paneID, keys string) (SendKeysTimings, error) {
    var timings SendKeysTimings
    runs := ClassifyKeyRuns(nil, keys)
    timings.ExecuteCount = len(runs)
    for _, run := range runs {
        // ... build cmd as before ...
        execStart := time.Now()
        _, mutexWait, err := c.Execute(ctx, cmd)
        if err != nil {
            return timings, err
        }
        execDur := time.Since(execStart)
        timings.MutexWait += mutexWait
        timings.ExecuteNet += max(0, execDur - mutexWait)
    }
    return timings, nil
}
```

The `max(0, ...)` guard on `ExecuteNet` prevents negative values from clock
granularity edge cases (~100ns resolution boundaries on macOS). This is
defense-in-depth — structurally `execDur >= mutexWait` since mutex acquisition
is a subset of the total execute duration.

#### Modified: `remote.Connection.SendKeys`

Current:

```go
func (c *Connection) SendKeys(ctx context.Context, paneID, keys string) error
```

New:

```go
func (c *Connection) SendKeys(ctx context.Context, paneID, keys string) (controlmode.SendKeysTimings, error)
```

Passthrough — propagates timings from `c.client.SendKeys()`.

**Other `Connection.SendKeys` callers** (`handlers_tell.go:51`, `clipboard.go:202,214`,
`manager.go:278`) update to `_, err := conn.SendKeys(...)` — they don't need timings.

#### Modified: `ControlSource.SendKeys` interface

Current:

```go
SendKeys(keys string) error
```

New:

```go
SendKeys(keys string) (controlmode.SendKeysTimings, error)
```

Both `LocalSource` and `RemoteSource` propagate real timings from `client.SendKeys()`.
`LocalSource` uses the same `controlmode.Client.SendKeys` as remote, so it gets
instrumented sub-timings for free. This is useful: mutex contention exists locally
too when multiple sessions share a tmux server.

`MockControlSource` in `controlsource_test.go` updates to return
`(controlmode.SendKeysTimings{}, nil)`.

#### Modified: `SessionTracker.SendInput`

Current:

```go
func (t *SessionTracker) SendInput(data string) error {
    return t.source.SendKeys(data)
}
```

New:

```go
func (t *SessionTracker) SendInput(data string) (controlmode.SendKeysTimings, error) {
    return t.source.SendKeys(data)
}
```

#### Modified: WebSocket handler async input sender

In `websocket.go`, the `inputResult` struct gains new fields:

```go
type inputResult struct {
    sendKeysDur   time.Duration
    t3            time.Time
    dispatch      time.Duration
    outputChDepth int
    // New: sub-timings from SendKeys
    mutexWait    time.Duration
    executeNet   time.Duration
    executeCount int
}
```

The goroutine calls `tracker.SendInput(batch.data)` and captures the returned timings.

#### Modified: `LatencySample`

New context fields:

```go
type LatencySample struct {
    // ... existing fields ...
    MutexWait    time.Duration // time waiting for stdinMu
    ExecuteNet   time.Duration // sum of Execute() round-trips (excluding mutex wait)
    ExecuteCount int           // number of Execute() calls per keystroke
}
```

#### Modified: `LatencyPercentiles`

New fields:

```go
type LatencyPercentiles struct {
    // ... existing fields ...
    MutexWaitP50    float64 `json:"mutexWaitP50"`
    MutexWaitP99    float64 `json:"mutexWaitP99"`
    ExecuteNetP50   float64 `json:"executeNetP50"`
    ExecuteNetP99   float64 `json:"executeNetP99"`
    ExecuteCountP50 float64 `json:"executeCountP50"`
    ExecuteCountP99 float64 `json:"executeCountP99"`
}
```

#### Modified: `inputEcho` sideband message

New fields in the JSON (dev mode only — `LatencyPercentiles` in `WSStatsMessage`
carries the P50/P99 aggregates in all modes):

```json
{
  "type": "inputEcho",
  "serverMs": 77.2,
  "dispatchMs": 0.1,
  "sendKeysMs": 75.0,
  "echoMs": 1.5,
  "frameSendMs": 0.6,
  "mutexWaitMs": 12.3,
  "executeNetMs": 62.5,
  "executeCount": 2
}
```

#### Frontend: `ServerSegmentTuple`

```typescript
// inputLatency.ts — manually defined (NOT in gen-types pipeline)
export type ServerSegmentTuple = {
  dispatch: number;
  sendKeys: number;
  echo: number;
  frameSend: number;
  total: number;
  // New: sendKeys sub-breakdown (present when instrumented)
  mutexWait?: number;
  executeNet?: number;
  executeCount?: number;
};
```

Optional fields so existing data without these fields still works.

Also update `ServerLatencySegments` in the same file to add the new P50/P99 fields.

#### Frontend: `LatencyBreakdown`

```typescript
export type LatencyBreakdown = {
  // ... existing fields ...
  // New: sendKeys sub-segments (only present for instrumented sessions)
  mutexWait?: number;
  executeNet?: number;
};
```

When `mutexWait` and `executeNet` are present, the `sendKeys` bar in `TypingPerformance`
splits into sub-segments visually:

- `mutexWait` — red-tinted (contention indicator)
- `executeNet` — blue (SSH round-trip)
- implicit remainder (`sendKeys - mutexWait - executeNet`) — classify/overhead

When absent (pre-upgrade sessions), `sendKeys` displays as before.

#### Frontend: `TypingPerformance.tsx`

Update `SEGMENTS`, `SEGMENT_COLORS`, `SEGMENT_LABELS` to include the new sub-segments.
The sub-segments only render when present (backward compatible).

New segments inserted after `sendKeys`:

```typescript
const SEGMENT_COLORS = {
  // ... existing ...
  mutexWait: 'rgba(220, 80, 80, 0.7)', // red — contention
  executeNet: 'rgba(80, 130, 200, 0.7)', // blue — network
};

const SEGMENT_LABELS = {
  // ... existing ...
  mutexWait: 'mutex',
  executeNet: 'execNet',
};
```

When sub-timings are present, `sendKeys` is hidden and replaced by `mutexWait` +
`executeNet`. When absent, `sendKeys` displays as before.

---

### Level 2: Enable Health Probes for Remote Sessions

#### Modified: `NewSessionTracker`

Current logic (tracker.go:131-137):

```go
if ls, ok := source.(*LocalSource); ok {
    healthProbe = ls.HealthProbe
} else {
    healthProbe = NewTmuxHealthProbe()
}
```

This already creates a `TmuxHealthProbe` for remote sessions. The missing piece is
that **nobody runs the probe goroutine** for remote sessions — `LocalSource.run()`
starts it at line 284, but `RemoteSource` has no equivalent.

#### New method: `Connection.ExecuteHealthProbe`

The health probe needs to call `Execute` on the control mode client. Add a dedicated
method to `Connection`:

```go
// ExecuteHealthProbe runs a lightweight no-op command for RTT measurement.
// Returns (output, mutexWait, error) matching Execute's new signature.
func (c *Connection) ExecuteHealthProbe(ctx context.Context) (string, time.Duration, error) {
    if !c.IsConnected() {
        return "", 0, fmt.Errorf("not connected")
    }
    return c.client.Execute(ctx, "display-message -p ok")
}
```

#### Modified: `RemoteSource` struct and `run()`

Add `healthProbe *TmuxHealthProbe` field. Initialize in `NewRemoteSource`.

In `run()`, subscribe to output **first**, then launch the health probe goroutine.
The jitter lives inside the probe goroutine, not before the subscription — this
prevents dropping terminal output during the jitter window:

```go
func (s *RemoteSource) run() {
    defer close(s.doneCh)
    defer close(s.events)

    // Subscribe to output FIRST — before any probe delay
    outputCh := s.conn.SubscribeOutput(s.paneID)
    defer s.conn.UnsubscribeOutput(s.paneID, outputCh)

    // Health probe goroutine (jitter is internal, does not block output)
    probeStop := make(chan struct{})
    go func() {
        // Stagger first probe to avoid correlated contention after reconnect
        jitter := time.Duration(rand.Int63n(int64(healthProbeInterval)))
        select {
        case <-time.After(jitter):
        case <-probeStop:
            return
        case <-s.stopCh:
            return
        }

        ticker := time.NewTicker(healthProbeInterval)
        defer ticker.Stop()
        for {
            select {
            case <-ticker.C:
                ctx, cancel := context.WithTimeout(context.Background(), healthProbeTimeout)
                start := time.Now()
                _, _, err := s.conn.ExecuteHealthProbe(ctx)
                rttUs := float64(time.Since(start).Microseconds())
                cancel()
                s.healthProbe.Record(rttUs, err != nil)
            case <-probeStop:
                return
            case <-s.stopCh:
                return
            }
        }
    }()
    defer close(probeStop)

    // Output event loop (unchanged)
    for {
        select {
        case event, ok := <-outputCh:
            if !ok {
                s.emit(SourceEvent{Type: SourceClosed})
                return
            }
            s.emit(SourceEvent{Type: SourceOutput, Data: event.Data})
        case <-s.stopCh:
            s.emit(SourceEvent{Type: SourceClosed})
            return
        }
    }
}
```

#### Modified: `NewSessionTracker`

Update to extract the probe from `RemoteSource`:

```go
var healthProbe *TmuxHealthProbe
switch s := source.(type) {
case *LocalSource:
    healthProbe = s.HealthProbe
case *RemoteSource:
    healthProbe = s.healthProbe
default:
    healthProbe = NewTmuxHealthProbe()
}
```

**Contention budget**: 10 sessions x 1 probe/5s = 2 `Execute()`/sec. Negligible
in steady state. The jitter prevents post-reconnect spikes. If probes cause
measurable `mutexWait` spikes, the instrumentation will self-report it.

---

### Level 3: Tag Metrics by Session Type

#### Modified: `WSStatsMessage`

New field:

```go
type WSStatsMessage struct {
    // ... existing fields ...
    SessionType string `json:"sessionType"` // "local" or "remote"
}
```

Set from `sess.IsRemoteSession()` in the WebSocket handler — `sess` is already
available at line 208.

#### Modified: `inputEcho` sideband

Add `"sessionType"` field to the JSON map:

```json
{
  "type": "inputEcho",
  "sessionType": "remote",
  ...
}
```

#### Frontend: `ServerSegmentTuple`

```typescript
export type ServerSegmentTuple = {
  // ... existing fields ...
  sessionType?: 'local' | 'remote';
};
```

#### Frontend: `TypingPerformance.tsx`

Show a small badge or label indicating session type when available.
No behavioral changes — purely informational.

---

## Decision Framework

Once data is collected, the dominant cost pattern determines the next architectural action:

| Dominant cost                               | Indicates                                | Next action                                                                             |
| ------------------------------------------- | ---------------------------------------- | --------------------------------------------------------------------------------------- |
| `mutexWait` > 50% of `sendKeys`             | Shared-mutex bottleneck                  | Per-session stdin channels or dedicated input SSH channel                               |
| `executeNet` dominates, single session      | SSH round-trip cost                      | Fire-and-forget `send-keys` or command pipelining                                       |
| `executeCount` P99 is high                  | Over-splitting by `ClassifyKeyRuns`      | Coalesce adjacent compatible key runs                                                   |
| Health probe RTT diverges from `executeNet` | Contention vs. network latency separable | Use probe RTT as network baseline; `executeNet - probeRTT` ≈ FIFO head-of-line blocking |

## Files Changed

| File                                                    | Level   | Change                                                                                   |
| ------------------------------------------------------- | ------- | ---------------------------------------------------------------------------------------- |
| `internal/remote/controlmode/client.go`                 | 1       | `Execute` returns `(string, time.Duration, error)`, `SendKeys` returns `SendKeysTimings` |
| `internal/remote/controlmode/keyclassify.go`            | 1       | `SendKeysTimings` type definition                                                        |
| `internal/remote/connection.go`                         | 1, 2    | `SendKeys` returns `(SendKeysTimings, error)`, `ExecuteHealthProbe` method               |
| `internal/session/controlsource.go`                     | 1       | `SendKeys` signature: `(controlmode.SendKeysTimings, error)`                             |
| `internal/session/controlsource_test.go`                | 1       | `MockControlSource.SendKeys` returns `(SendKeysTimings{}, nil)`                          |
| `internal/session/localsource.go`                       | 1       | `SendKeys` propagates real timings from `client.SendKeys()`                              |
| `internal/session/remotesource.go`                      | 1, 2    | `SendKeys` returns timings, health probe goroutine with jitter, `healthProbe` field      |
| `internal/session/tracker.go`                           | 1, 2    | `SendInput` returns timings, probe extraction via type switch                            |
| `internal/session/tmux_health.go`                       | —       | No changes (but add tests — see Test Plan)                                               |
| `internal/dashboard/latency_collector.go`               | 1       | New fields on `LatencySample` and `LatencyPercentiles`                                   |
| `internal/dashboard/latency_collector_test.go`          | 1       | Extend existing tests for new percentile fields                                          |
| `internal/dashboard/websocket.go`                       | 1, 3    | `inputResult` new fields, `inputEcho` new JSON keys, `WSStatsMessage.SessionType`        |
| `internal/dashboard/websocket_test.go`                  | 1, 3    | New and updated tests (see Test Plan)                                                    |
| `internal/dashboard/handlers_tell.go`                   | 1       | `_, err := conn.SendKeys(...)`                                                           |
| `internal/dashboard/clipboard.go`                       | 1       | `_, err := conn.SendKeys(...)` (two call sites)                                          |
| `internal/session/manager.go`                           | 1       | `_, err := conn.SendKeys(...)` in signal monitor                                         |
| `assets/dashboard/src/lib/inputLatency.ts`              | 1, 3    | New optional fields on `ServerSegmentTuple`, `ServerLatencySegments`, `LatencyBreakdown` |
| `assets/dashboard/src/lib/terminalStream.ts`            | 1, 3    | Plumb new fields from `inputEcho` and `stats`                                            |
| `assets/dashboard/src/components/TypingPerformance.tsx` | 1, 3    | New sub-segments, session type badge                                                     |
| `docs/api.md`                                           | 1, 2, 3 | Document new message fields                                                              |

## Interface Dependency Chain

```
controlmode.Client.Execute
  returns (string, time.Duration, error)       ◄── ~20 call sites update to _, _, err
       │
       ▼
controlmode.Client.SendKeys
  returns (controlmode.SendKeysTimings, error)
       │
       ▼
remote.Connection.SendKeys
  returns (controlmode.SendKeysTimings, error)  ◄── passthrough
       │                                             (tell, clipboard, signal: _, err)
       ▼
session.ControlSource.SendKeys
  returns (controlmode.SendKeysTimings, error)  ◄── interface change
       │
   ┌───┴───┐
   ▼       ▼
LocalSource  RemoteSource     (both propagate real timings)
   │              │
   ▼              ▼
SessionTracker.SendInput
  returns (controlmode.SendKeysTimings, error)
       │
       ▼
websocket.go async input sender
  inputResult gains mutex/execute fields
       │
       ▼
LatencyCollector.Add(LatencySample{...})
       │
       ▼
inputEcho sideband JSON + stats percentiles
       │
       ▼
Frontend InputLatencyTracker
       │
       ▼
TypingPerformance UI
```

## Test Plan

| Test                                                                                             | What it verifies                                                                   |
| ------------------------------------------------------------------------------------------------ | ---------------------------------------------------------------------------------- |
| Mock `ControlSource` returning known `SendKeysTimings` → verify fields reach `inputResult`       | Timings propagate through the full interface chain                                 |
| `LatencyPercentiles` computation with new fields                                                 | `MutexWaitP50`/`P99` and `ExecuteNetP50`/`P99` are non-zero when samples have data |
| `inputEcho` JSON contains `mutexWaitMs`, `executeNetMs`, `executeCount` when timings are present | Sideband message format matches frontend expectations                              |
| `inputEcho` JSON contains `sessionType` field                                                    | Level 3 tagging works                                                              |
| `WSStatsMessage` includes `sessionType`                                                          | Stats ticker carries session type                                                  |
| `MutexWait + ExecuteNet <= SendKeys` for all samples                                             | Non-overlapping invariant holds                                                    |
| `TmuxHealthProbe` basic operations: Record, Stats, Snapshot, ring buffer overflow                | Probe subsystem works correctly (currently untested)                               |
| `Execute` returns non-zero `mutexWait` under simulated contention                                | Mutex timing instrumentation works                                                 |

## Deployment

**Backend-first, then frontend.** The changes are purely additive on the wire:

- **New backend + old frontend**: Frontend's `JSON.parse` + `?? 0` pattern ignores
  unknown fields (`mutexWaitMs`, `executeNetMs`, `sessionType`). No breakage.
- **New frontend + old backend**: New TypeScript fields are optional (`mutexWait?`,
  `executeNet?`). When absent, sub-segments don't render. No breakage.

No protocol versioning or feature flags needed.

## Risk Assessment

- **`Execute` signature change** — Touches ~20 call sites but the compiler finds them all. Each is a mechanical `output, _, err :=` update. No judgment calls.
- **`ControlSource.SendKeys` signature change** — Two-way door. 3 implementations + 1 caller in-tree. Compile errors guide the migration.
- **Health probes on shared connection** — Adds `stdinMu` contention (2 `Execute()`/sec with 10 sessions). Intentional — measures what we care about. Jitter prevents post-reconnect spikes. If it's a problem, the probe data itself will show it.
- **`time.Now()` overhead** — Two extra `time.Now()` calls per `Execute` call. ~25ns each on darwin. Negligible.
- **`ExecuteNet` negativity** — Guarded by `max(0, execDur - mutexWait)`. Structurally impossible under normal operation; guard is defense-in-depth against clock granularity edge cases.
