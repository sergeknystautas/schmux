# Typing Performance Diagnostic

The typing performance widget measures end-to-end keystroke latency: the time from when the user presses a key to when the character appears on screen. It is a dev-mode-only diagnostic panel in the sidebar.

## Architecture

```
Browser (client clock)              Go Server (server clock)              tmux
─────────────────────               ────────────────────────              ────

c1: WS send ───────────────────────> s1: controlChan receive
                                     s2: pre-dispatch (after coalesce)
                                         tracker.SendInput()
                                         stdinMu.Lock()
                                         fmt.Fprintf(stdin, "send-keys...")
                                         wait for %begin/%end ack
                                     s3: SendKeys returns
                                                                          program processes key
                                                                          tmux sends %output
                                     s4: outputCh receives %output
                                         build binary frame
                                         WS write
                                     s5: WS write complete
c2: WS message received <───────────
c4: event loop processes message
    xterm.js renders
c3: render complete
```

## Timestamps and Segments

### Server-side (Go clock, precise deltas)

| Segment                  | Computation | Code location                               |
| ------------------------ | ----------- | ------------------------------------------- |
| dispatch (→ "handler")   | s2 - s1     | `websocket.go:618` `batch.t2.Sub(batch.t1)` |
| sendKeys (→ "transport") | s3 - s2     | `websocket.go:616` `t3.Sub(t2)`             |
| echo (→ "tmux + agent")  | s4 - s3     | `websocket.go:672` `t4.Sub(pending.t3)`     |
| frameSend (→ "ws write") | s5 - s4     | `websocket.go:673` `t5.Sub(t4)`             |

These are sent to the client as a JSON sideband message (`type: "inputEcho"`) with fields `dispatchMs`, `sendKeysMs`, `echoMs`, `frameSendMs`.

### Client-side (browser clock, precise deltas)

| Segment            | Computation                              | Code location                                                                             |
| ------------------ | ---------------------------------------- | ----------------------------------------------------------------------------------------- |
| total (client RTT) | c2 - c1                                  | `terminalStream.ts` via `inputLatency.markSent()` / `markReceived()`                      |
| js queue           | MessageChannel probe lag at receive time | `inputLatency.ts:markSent()` fires a MessageChannel; lag = `performance.now() - sentTime` |
| xterm (render)     | c3 - c4                                  | `terminalStream.ts` via `inputLatency.markRenderTime()`                                   |

### Residual

| Segment    | Computation                                                                |
| ---------- | -------------------------------------------------------------------------- |
| system | total - (handler + transport + tmux + agent + ws write + js queue + xterm) |

This is the only segment that crosses clock boundaries. It captures WebSocket upstream transit, WebSocket downstream transit, and any system overhead. The residual is computed per-tuple (clamped to 0) then medianed across the cohort. The displayed total is the cohort's median RTT, independent of segment sum.

## Display Names

The segments were renamed for clarity. The wire protocol names (server JSON) are unchanged.

| Wire name (server→client) | Internal name (ServerSegmentTuple) | Display name (UI) | Bucket        |
| ------------------------- | ---------------------------------- | ----------------- | ------------- |
| dispatchMs                | dispatch                           | handler           | schmux code   |
| sendKeysMs                | sendKeys                           | transport         | schmux ↔ host |
| echoMs                    | echo                               | tmux + agent      | schmux ↔ host |
| frameSendMs               | frameSend                          | ws write          | schmux code   |
| (computed)                | (evtLoop probe)                    | js queue          | page ↔ schmux |
| (computed)                | (render timer)                     | xterm             | schmux code   |
| (residual)                | (residual)                         | system        | page ↔ schmux |

Segment display order (causal): handler, transport, tmux + agent, ws write, js queue, xterm, system.

## What Each Segment Captures for Local vs Remote

| Segment      | Local session                                           | Remote session                                                                        |
| ------------ | ------------------------------------------------------- | ------------------------------------------------------------------------------------- |
| handler      | Go handler: decode WS msg, coalesce keystrokes (~0.5ms) | Same (~0.5ms)                                                                         |
| transport    | Unix socket write + tmux ack (~0.5ms)                   | SSH upstream + tmux dispatch + SSH downstream for ack (~85ms)                         |
| tmux + agent | Program processes key + tmux detects output (~13ms)     | Program processes key + tmux detects output + SSH downstream for %output (~6ms + SSH) |
| ws write     | Serialize frame + WS write (~0.1ms)                     | Same (~0.1ms)                                                                         |
| js queue     | JS event loop delay before processing WS message (~1ms) | Same (~1-3ms)                                                                         |
| xterm        | xterm.js parse + paint (~0.5ms)                         | Same (~0.5ms)                                                                         |
| system   | WS loopback both directions (~1ms)                      | WS loopback + system SSH overhead (~varies)                                       |

Key insight: for remote sessions, SSH latency hides in `transport` (2 hops for send-keys ack) and `tmux + agent` (1 hop for %output notification). These two segments are the ones that change dramatically between local and remote.

## Known Issues

### ~~1. P50/P99 breakdown uses single-tuple picking~~ (resolved)

Replaced with cohort-median computation. The breakdown now shows two cohorts: **Typical** (IQR, P25-P75 tuples) and **Outlier** (P95+ tuples). Each segment value is the median within that cohort, not from a single picked tuple. Minimum 5 tuples per cohort; below that, the bar shows "insufficient data." The displayed total is the cohort's median RTT (independent of segment sum), and bar widths use `segmentSum` as their denominator so segments fill proportionally without overflowing.

### 2. FIFO queue pairing can mismatch keystrokes with output

**File**: `websocket.go`, lines 664-700.

The pending input queue is a FIFO: each keystroke pushes timing data, each `%output` event pops the oldest. If the program emits output that isn't in response to a keystroke (e.g., a timer, background process), it pops the wrong keystroke's timing. The `serverTotal > clientRTT` guard (line 393 in `inputLatency.ts`) discards these, but some mismatches slip through.

### 3. `tmux + agent` for remote includes SSH downstream transit

The `echo` timer (s4 - s3) starts when `SendKeys` returns (the ack arrived over SSH) and ends when `%output` arrives. For remote sessions, the `%output` notification must travel over SSH, so `tmux + agent` = program time + SSH one-way. There's no way to separate these without a clock on the remote host.

### 4. `transport` for remote includes SSH round-trip

The `sendKeys` timer (s3 - s2) includes: stdin mutex wait, writing the command to the SSH pipe, SSH encrypting + transmitting, tmux processing, SSH return trip. For local sessions this is ~0.5ms (Unix socket). For remote it's ~85ms (dominated by SSH RTT). The `mutexWait` and `executeNet` sub-segments are available in `ServerSegmentTuple` but are no longer exposed in the UI breakdown.

### ~~5. Stale `lastInputTime` causes bogus samples~~ (resolved)

`markReceived()` now discards any pending measurement where `performance.now() - lastInputTime > 2000` (2 seconds). Keystrokes that don't produce output within 2s are not meaningful latency samples — the agent is thinking, not echoing.

### ~~6. `system` residual can go negative~~ (resolved)

Residual is now computed per-tuple (clamped to 0) then medianed across the cohort. The displayed total is the cohort's median RTT, independent of segment sum. Segment medians may not sum to the displayed total (medians of parts ≠ parts of medians), but each value is independently honest. Bar widths use `segmentSum` as their denominator to prevent visual overflow.

## Breakdown Methodology

**File**: `inputLatency.ts`, `getBreakdown()` method.

The breakdown shows where keystroke latency goes for two cohorts:

- **Typical** — all tuples whose total RTT falls in the IQR (P25-P75). Represents a normal keystroke.
- **Outlier** — all tuples whose total RTT exceeds P95. Represents a jittery keystroke.

Each cohort requires at least 5 valid paired tuples. Percentile boundaries are computed from valid paired tuple RTTs (after the `serverTotal > clientRTT` mismatch filter), not from raw samples.

For each cohort, the median of each segment is computed independently. The residual (`system`) is computed per-tuple first (`max(0, clientRTT - sum_of_segments)`), then medianed across the cohort like any other segment.

The `jsQueue` segment uses a per-tuple `receiveLag` value — a MessageChannel probe fired from `recordServerSegments()` that measures event loop congestion at sideband processing time. When `receiveLag` is undefined (probe hasn't fired yet), it defaults to 0.

The `LatencyBreakdown` type returns:

- `total` — cohort's median RTT, used for the label and cross-bar scaling
- `segmentSum` — sum of segment medians, used as the denominator for bar widths

These differ because medians of parts ≠ parts of medians. Both are independently honest.

Segments are ordered by causal flow (handler → transport → tmux + agent → ws write → js queue → xterm → system) and color-coded by ownership: green for schmux code, gray for host environment, blue for browser.

## Per-Machine Tracking

**File**: `inputLatency.ts`, `switchMachine()` method.

Latency data is tracked per machine:

- All local sessions share one dataset (key: `"local"`)
- Each remote host gets its own dataset (key: host ID from `session.remote_host_id`)

When the user navigates to a different session, `TerminalStream.onopen` calls `inputLatency.switchMachine(machineKey)`, which saves the current data and restores (or creates) the target machine's data. This way switching between sessions preserves accumulated data for each machine.

The `machineKey` is passed via `TerminalStreamOptions.machineKey` from `SessionDetailPage.tsx`.

The tmux health widget (`tmuxHealth.ts`) uses the same per-machine pattern via `switchTmuxHealthMachine()`.

## File Inventory

| File                                                    | Role                                                                                                                                              |
| ------------------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------- |
| `assets/dashboard/src/lib/inputLatency.ts`              | Core tracker: sample storage, percentile computation, breakdown calculation, per-machine switching                                                |
| `assets/dashboard/src/lib/terminalStream.ts`            | Connects browser to WS; calls `markSent()`, `markReceived()`, `markRenderTime()`, `recordServerSegments()`; triggers `switchMachine()` on connect |
| `assets/dashboard/src/components/TypingPerformance.tsx` | UI component: histogram, breakdown bars, segment colors/labels/ordering                                                                           |
| `internal/dashboard/websocket.go`                       | Server-side timing: timestamps s1-s5, FIFO queue, sideband JSON emission (lines 570-700)                                                          |
| `internal/remote/controlmode/client.go`                 | `SendKeys()` method: measures `mutexWait` and `executeNet` sub-segments (line 406)                                                                |
| `internal/remote/controlmode/keyclassify.go`            | `SendKeysTimings` type definition (line 153)                                                                                                      |

## Histogram

The histogram shows the distribution of client RTT values (all `samples[]`). One bucket per millisecond. Range capped at P99 to prevent outliers from stretching the chart. Vertical lines mark P50 (solid) and P99 (dashed). Shaded band shows IQR (P25-P75).

**File**: `TypingPerformance.tsx`, `Histogram` component.

## Server Latency Stats

Separate from the per-keystroke breakdown, there's a periodic server latency summary sent via the stats WebSocket message (`inputLatency` field). This contains pre-computed P50/P99 for each server segment. It is stored via `updateServerLatency()` but is NOT used by the breakdown display — the breakdown uses per-keystroke paired samples instead.

**File**: `inputLatency.ts`, `ServerLatencySegments` type, `updateServerLatency()`, `getServerLatency()`.
