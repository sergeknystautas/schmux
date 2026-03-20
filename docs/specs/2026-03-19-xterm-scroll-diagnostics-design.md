# Xterm Scroll Reset Diagnostics

## Problem

The terminal intermittently loses scroll-to-bottom position. The Resume button appears without user scrolling, and sometimes the screen appears to reload. This is not consistently reproducible.

## Goal

Add diagnostic instrumentation to the existing `StreamDiagnostics` system so that when the issue reproduces, clicking "Capture" saves enough scroll state data for the diagnostic agent to identify the root cause.

## Scope Constraint

This is instrumentation only. No changes to scroll suppression logic, `writeTerminal`, `fitTerminal`, `handleSync`, `setFollow`, or WebSocket lifecycle behavior. Any fix to the underlying bug is a separate follow-up. If a code path needs a behavioral change, it gets a separate spec.

## Hypotheses the Data Must Distinguish

1. **`writingToTerminal` suppression gap**: The `writeTerminal()` coalescing path (when `scrollRAFPending` is already true) skips scheduling a new rAF. If the pending rAF fires and clears `writingToTerminal` before xterm finishes rendering a subsequent write, a scroll event slips through the suppression guard and sets `followTail = false`.

2. **Resize/write flag collision**: `fitTerminal()` and `writeTerminal()` both set `writingToTerminal = true` and schedule independent `requestAnimationFrame` callbacks to clear it. If a resize occurs during active output, whichever rAF fires first clears the flag, leaving the other path unprotected.

3. **Terminal recreation from dependency change**: The terminal stream React effect depends on `[sessionData?.id, remoteDisconnected]`. If `remoteDisconnected` briefly toggles from a dashboard WebSocket broadcast, the terminal tears down and recreates, causing `terminal.reset()` before `writeTerminal()` sets the suppression flag.

## Design

Follow the existing pattern established by gap detection telemetry: counters and a ring buffer in `StreamDiagnostics`, recorded at instrumentation points in `TerminalStream`, included in the diagnostic capture POST to `/api/dev/diagnostic-append`, written to files in the diagnostic directory, and displayed live in `StreamMetricsPanel`.

### Data Captured

**Ring buffer** (last 100 entries) of `ScrollDiagnosticEvent`:

| Field               | Type                             | Purpose                                                                                                     |
| ------------------- | -------------------------------- | ----------------------------------------------------------------------------------------------------------- |
| `ts`                | `number`                         | `Date.now()` (epoch ms) — correlates with the ring buffer's ISO wall-clock timestamps                       |
| `trigger`           | `'userScroll' \| 'jumpToBottom'` | What caused the event                                                                                       |
| `followBefore`      | `boolean`                        | `followTail` value before the transition                                                                    |
| `followAfter`       | `boolean`                        | `followTail` value after the transition                                                                     |
| `writingToTerminal` | `boolean`                        | Was the suppression flag active?                                                                            |
| `scrollRAFPending`  | `boolean`                        | Was a coalesced rAF already queued?                                                                         |
| `viewportY`         | `number`                         | xterm buffer viewport position                                                                              |
| `baseY`             | `number`                         | xterm buffer base position (bottom = viewportY >= baseY)                                                    |
| `lastReceivedSeq`   | `string`                         | Frame sequence number at time of event — correlate with ring buffer to see what content caused displacement |

**Counters (in StreamDiagnostics):**

| Counter                 | Purpose                                                                                  | Hypothesis                                                                                 |
| ----------------------- | ---------------------------------------------------------------------------------------- | ------------------------------------------------------------------------------------------ |
| `followLostCount`       | Times `followTail` went `true → false`                                                   | All — this IS the symptom                                                                  |
| `scrollSuppressedCount` | Times `handleUserScroll` was suppressed by `writingToTerminal=true`                      | Baseline — confirms suppression fires correctly                                            |
| `scrollCoalesceHits`    | Times `writeTerminal` callback found `scrollRAFPending=true`                             | H1 — measures how often the coalescing path fires                                          |
| `resizeCount`           | Times `fitTerminal()` fired (runtime resizes only, not `fitTerminalSync` initialization) | H2 — frequency baseline                                                                    |
| `lastResizeTs`          | `Date.now()` of most recent `fitTerminal()` call                                         | H2 — agent checks `scrollEvent.ts - lastResizeTs < 100ms` to detect resize/write collision |

**Component-level counter (in SessionDetailPage React state):**

| Counter                   | Purpose                                                                                                                          | Hypothesis                                       |
| ------------------------- | -------------------------------------------------------------------------------------------------------------------------------- | ------------------------------------------------ |
| `terminalRecreationCount` | Incremented each time the terminal stream effect runs (survives across `TerminalStream` instances since it lives in React state) | H3 — if > 1 at capture time, recreation occurred |

This solves the problem that `StreamDiagnostics` is destroyed and recreated with the `TerminalStream`, so `bootstrapCount` always resets to 0 on a new instance. `terminalRecreationCount` lives outside the stream and accumulates across recreations.

To get `terminalRecreationCount` into the capture POST: expose a `recreationCount` property on `TerminalStream` that `SessionDetailPage` sets after incrementing state. The `onDiagnosticResponse` handler in `enableDiagnostics()` reads `this.recreationCount` when constructing the POST body and includes it in `scrollStats`.

### Instrumentation Points

All instrumentation is gated on `if (this.diagnostics)` checks. `handleUserScroll` is on the hot path (fires on every scroll event), so the gate must be cheap — a single null check before any work.

**`setFollow()`** — the single mutation point for `followTail`. Record the event here rather than in each caller (`handleUserScroll`, `jumpToBottom`), so any future callers are automatically instrumented. Only record when `follow !== this.followTail` (actual state change).

The signature changes from `setFollow(follow: boolean)` to `setFollow(follow: boolean, trigger?: 'userScroll' | 'jumpToBottom')`. The optional `trigger` parameter is only used for diagnostics — the existing behavior (set flag, update checkbox, call `onResume`) is unchanged. Callers that don't pass a trigger (if any exist) simply produce events without a trigger label.

**`handleUserScroll()`** — the only code path that can set `followTail = false`:

- When `writingToTerminal` is true: increment `scrollSuppressedCount` (existing early return, add counter before it)
- When `writingToTerminal` is false: call `setFollow` with trigger context `'userScroll'`

**`writeTerminal()` callback** — the coalescing path:

- When `scrollRAFPending` is already true (the callback runs but skips the rAF scheduling block): increment `scrollCoalesceHits` via an explicit `else` branch

**`jumpToBottom()`** — user recovery action:

- Pass trigger context `'jumpToBottom'` to `setFollow`

**`fitTerminal()`** — runtime resize path only:

- Increment `resizeCount` and set `lastResizeTs = Date.now()`
- `fitTerminalSync()` is initialization-only and excluded, since H2 is about resizes colliding with active output

**`SessionDetailPage` terminal effect** — React lifecycle:

- Increment `terminalRecreationCount` state each time the effect body runs

### Diagnostic Capture Pipeline

The existing `enableDiagnostics()` handler in `terminalStream.ts` POSTs to `/api/dev/diagnostic-append` with fields: `xtermScreen`, `screenDiff`, `ringBufferFrontend`, `gapStats`, `cursorXterm`. Data is sent as pre-serialized JSON strings (string blobs), matching the established pattern. No backend parsing or schema validation.

Add two new string fields to this POST body (in `terminalStream.ts`, inside the `enableDiagnostics()` method's `onDiagnosticResponse` handler):

- `scrollEvents` — JSON string of the scroll event ring buffer
- `scrollStats` — JSON string of the counters object (all four StreamDiagnostics counters + `terminalRecreationCount` passed in from SessionDetailPage)

The backend handler writes them to `scroll-events.json` and `scroll-stats.json` in the diagnostic directory alongside the existing files. Best-effort writes (ignore `os.WriteFile` errors), same as existing fields.

### Live Display

Add counters to `StreamMetricsPanel`:

- "Follow lost" (with warning styling when > 0) — in the pill bar and dropdown
- "Scroll suppressed" — in the dropdown
- "Write coalesce hits" — in the dropdown
- "Resizes" — in the dropdown

These are polled via the existing 3-second `setInterval` in `SessionDetailPage` that already reads from `diagnostics`. Both the `FrontendStats` interface in `StreamMetricsPanel` and the inline `frontendStats` state type in `SessionDetailPage` must be extended with the new counter fields.

### Files Modified

| File                                                          | Change                                                                                                                                                 |
| ------------------------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------ |
| `assets/dashboard/src/lib/streamDiagnostics.ts`               | New type, fields, methods, constant                                                                                                                    |
| `assets/dashboard/src/lib/terminalStream.ts`                  | Record events in `setFollow`, add counters in `handleUserScroll`/`writeTerminal`/`fitTerminal`, add scroll data to capture POST in `enableDiagnostics` |
| `assets/dashboard/src/lib/terminalStream.test.ts`             | New test cases for scroll diagnostic recording                                                                                                         |
| `assets/dashboard/src/components/StreamMetricsPanel.tsx`      | Extend `FrontendStats` interface, add counters to pill bar and dropdown                                                                                |
| `assets/dashboard/src/components/StreamMetricsPanel.test.tsx` | New tests for follow-lost warning and counter rendering                                                                                                |
| `assets/dashboard/src/routes/SessionDetailPage.tsx`           | Add `terminalRecreationCount` state, extend inline `frontendStats` type, include new counters in interval poll, pass recreation count to capture       |
| `internal/dashboard/handlers_diagnostic.go`                   | Accept and write 2 new fields                                                                                                                          |
| `docs/api.md`                                                 | Document new `scrollEvents` and `scrollStats` fields on `POST /api/dev/diagnostic-append`                                                              |

### What Does NOT Change

This is instrumentation only:

- No changes to scroll suppression logic (`writingToTerminal` flag, `scrollRAFPending` coalescing)
- No changes to `writeTerminal`, `fitTerminal`, `fitTerminalSync`, `handleSync`, or `setFollow` behavior
- No changes to WebSocket lifecycle, reconnection, or bootstrap
- No runtime impact when diagnostics are disabled (all instrumentation gated on `if (this.diagnostics)`)
- No new dependencies

### Testing

**`terminalStream.test.ts`** — new test cases following existing patterns (mock terminal, `vi.mocked`, rAF interception):

1. `handleUserScroll` with diagnostics enabled records event when `followTail` changes
2. `handleUserScroll` increments `scrollSuppressedCount` when `writingToTerminal` is true
3. `writeTerminal` increments `scrollCoalesceHits` when `scrollRAFPending` is true
4. Scroll event ring buffer caps at 100 entries
5. No errors when diagnostics are disabled
6. `jumpToBottom` records recovery event
7. `scrollSnapshot` returns correct shape with events and counters
8. `reset()` clears scroll events and counters

**`StreamMetricsPanel.test.tsx`** — new test cases following existing patterns (render, fireEvent, screen queries):

9. "Follow lost" warning appears in pill bar when `followLostCount > 0`
10. "Follow lost" warning does not appear when `followLostCount` is 0
11. New counter rows render in expanded dropdown with correct values

### Verification

1. `./test.sh` — full suite passes including new tests
2. `go run ./cmd/build-dashboard` — dashboard compiles
3. Peer review by separate agent
