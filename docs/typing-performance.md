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
| sendKeys (→ "tmux cmd")  | s3 - s2     | `websocket.go:616` `t3.Sub(t2)`             |
| echo (→ "pane output")   | s4 - s3     | `websocket.go:672` `t4.Sub(pending.t3)`     |
| frameSend (→ "ws write") | s5 - s4     | `websocket.go:673` `t5.Sub(t4)`             |

These are sent to the client as a JSON sideband message (`type: "inputEcho"`) with fields `dispatchMs`, `sendKeysMs`, `echoMs`, `frameSendMs`.

### Client-side (browser clock, precise deltas)

| Segment            | Computation                              | Code location                                                                             |
| ------------------ | ---------------------------------------- | ----------------------------------------------------------------------------------------- |
| total (client RTT) | c2 - c1                                  | `terminalStream.ts` via `inputLatency.markSent()` / `markReceived()`                      |
| js queue           | MessageChannel probe lag at receive time | `inputLatency.ts:markSent()` fires a MessageChannel; lag = `performance.now() - sentTime` |
| xterm (render)     | c3 - c4                                  | `terminalStream.ts` via `inputLatency.markRenderTime()`                                   |

### Residual

| Segment | Computation                                                              |
| ------- | ------------------------------------------------------------------------ |
| network | total - (handler + tmux cmd + pane output + ws write + js queue + xterm) |

This is the only segment that crosses clock boundaries. It captures WebSocket upstream transit, WebSocket downstream transit, and any unmeasured overhead.

## Display Names

The segments were renamed for clarity. The wire protocol names (server JSON) are unchanged.

| Wire name (server→client) | Internal name (ServerSegmentTuple) | Display name (UI) | Bucket        |
| ------------------------- | ---------------------------------- | ----------------- | ------------- |
| dispatchMs                | dispatch                           | handler           | schmux code   |
| sendKeysMs                | sendKeys                           | tmux cmd          | schmux ↔ host |
| echoMs                    | echo                               | pane output       | schmux ↔ host |
| frameSendMs               | frameSend                          | ws write          | schmux code   |
| (computed)                | (evtLoop probe)                    | js queue          | page ↔ schmux |
| (computed)                | (render timer)                     | xterm             | schmux code   |
| (residual)                | (residual)                         | network           | page ↔ schmux |

Segment display order: network, js queue, handler, ws write, xterm, tmux cmd, pane output.

## What Each Segment Captures for Local vs Remote

| Segment     | Local session                                           | Remote session                                                                        |
| ----------- | ------------------------------------------------------- | ------------------------------------------------------------------------------------- |
| handler     | Go handler: decode WS msg, coalesce keystrokes (~0.5ms) | Same (~0.5ms)                                                                         |
| tmux cmd    | Unix socket write + tmux ack (~0.5ms)                   | SSH upstream + tmux dispatch + SSH downstream for ack (~85ms)                         |
| pane output | Program processes key + tmux detects output (~13ms)     | Program processes key + tmux detects output + SSH downstream for %output (~6ms + SSH) |
| ws write    | Serialize frame + WS write (~0.1ms)                     | Same (~0.1ms)                                                                         |
| js queue    | JS event loop delay before processing WS message (~1ms) | Same (~1-3ms)                                                                         |
| xterm       | xterm.js parse + paint (~0.5ms)                         | Same (~0.5ms)                                                                         |
| network     | WS loopback both directions (~1ms)                      | WS loopback + unmeasured SSH overhead (~varies)                                       |

Key insight: for remote sessions, SSH latency hides in `tmux cmd` (2 hops for send-keys ack) and `pane output` (1 hop for %output notification). These two segments are the ones that change dramatically between local and remote.

## Known Issues

### 1. P50/P99 breakdown uses single-tuple picking (inaccurate)

**File**: `inputLatency.ts`, `getBreakdown()` method.

The breakdown picks a single keystroke tuple whose `clientRTT` is closest to the target percentile (P50 or P99). The segment values shown are from that one keystroke, not percentiles of each segment independently.

**Problem**: The picked tuple may not be representative. For example:

- P99 total is 235ms (true percentile across all samples)
- The closest tuple has segments summing to 84ms
- The `network` residual absorbs the 150ms gap
- This makes `network` appear huge when it's really just a statistical artifact

**Better approach**: Compute each segment's percentile independently: P99 of all handler values, P99 of all tmux cmd values, etc. Then `network = total - sum(segment percentiles)`, clamped to zero. The segments won't sum exactly to total (percentiles of parts don't equal percentile of sum), but the individual segment values will be accurate representations of their own distributions.

**Alternative approach**: Instead of percentile-picking, bucket all tuples into P50-adjacent and P99-adjacent groups (e.g., tuples within 10% of the target RTT) and average the segments within each group. This gives a representative breakdown without the single-tuple noise.

### 2. FIFO queue pairing can mismatch keystrokes with output

**File**: `websocket.go`, lines 664-700.

The pending input queue is a FIFO: each keystroke pushes timing data, each `%output` event pops the oldest. If the program emits output that isn't in response to a keystroke (e.g., a timer, background process), it pops the wrong keystroke's timing. The `serverTotal > clientRTT` guard (line 393 in `inputLatency.ts`) discards these, but some mismatches slip through.

### 3. `pane output` for remote includes SSH downstream transit

The `echo` timer (s4 - s3) starts when `SendKeys` returns (the ack arrived over SSH) and ends when `%output` arrives. For remote sessions, the `%output` notification must travel over SSH, so `pane output` = program time + SSH one-way. There's no way to separate these without a clock on the remote host.

### 4. `tmux cmd` for remote includes SSH round-trip

The `sendKeys` timer (s3 - s2) includes: stdin mutex wait, writing the command to the SSH pipe, SSH encrypting + transmitting, tmux processing, SSH return trip. For local sessions this is ~0.5ms (Unix socket). For remote it's ~85ms (dominated by SSH RTT). The `mutexWait` and `executeNet` sub-segments are available in `ServerSegmentTuple` but are no longer exposed in the UI breakdown.

### 5. Stale `lastInputTime` causes bogus samples from non-echo output

**File**: `inputLatency.ts`, `markSent()` / `markReceived()`.

`markSent()` sets `lastInputTime` on every keystroke. `markReceived()` records a sample using the first output frame that arrives after a keystroke. But if the program doesn't immediately echo the keystroke (e.g., typing in a password prompt, or the agent is busy), `lastInputTime` stays non-zero indefinitely. When unrelated output eventually arrives (e.g., Claude streaming a response minutes later), the first frame matches `markReceived()` and records a bogus RTT of seconds or minutes.

**Fix**: Add a staleness timeout. If `performance.now() - lastInputTime > threshold` (e.g., 2 seconds), discard the pending measurement in `markReceived()` and reset `lastInputTime` to zero. Keystrokes that don't produce output within 2s are not meaningful latency samples.

### 6. `network` residual can go negative

If server-reported segments + client segments exceed the client RTT (possible due to clock skew or timing jitter), the residual goes negative. It's clamped to zero, but this means the displayed segments can sum to MORE than total (the excess is hidden by the clamp). This happens rarely but is theoretically possible.

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
