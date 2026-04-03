VERDICT: NEEDS_REVISION

## Summary Assessment

The design correctly identifies both the single-tuple-picking problem and the misleading "network" label, and proposes a sound statistical remedy (group-based medians). However, there are two critical issues: the `jsQueue` computation for per-tuple cohort members remains structurally flawed under the new approach, and the design silently drops Known Issue #6 (negative residual / segment-sum-exceeds-total) which actually gets worse with independent per-segment medians.

## Critical Issues (must fix)

### 1. jsQueue is still computed from a global percentile, not per-tuple

The design says "compute the median of each segment independently within this group" but does not address how `jsQueue` is derived. In the current code (`inputLatency.ts` lines 358-370), `jsQueue` is NOT a server-reported segment -- it comes from a separate `receiveLagSamples` array that is not index-aligned with the keystroke tuples. The current `getBreakdown()` computes a single global P50/P99 of `receiveLagSamples` and applies it uniformly to every tuple.

Under the proposed cohort approach, you would compute per-segment medians within IQR/P95+ groups. But `jsQueue` has no per-tuple value to take a median of -- it's a global scalar applied to all tuples. This means `jsQueue` will be identical in both the Typical and Outlier bars, which (a) is misleading because event loop congestion DOES correlate with high-latency keystrokes, and (b) breaks the "each segment value is a median across many tuples" invariant.

**Fix options:**

- Associate the receive-time lag probe sample with its corresponding keystroke tuple (pair them by index like the server segments), so `jsQueue` becomes a per-tuple field that can be grouped and medianed.
- Or explicitly acknowledge that `jsQueue` is an estimate and document that it uses a global percentile matching the cohort level (IQR median for typical, P95 for outlier), rather than a per-tuple value.

### 2. Known Issue #6 (negative residual / over-summing) gets worse, not better

The design states "the residual shrinks to something small and honest" but does not address what happens when independent segment medians sum to MORE than the cohort's median total. This is Known Issue #6 from `typing-performance.md`, and it is actually more likely with group-based medians than with single-tuple picking.

With a single tuple, all segments come from one keystroke, so they sum to exactly `serverTotal` (plus render + infra). The residual absorbs the gap between serverTotal and clientRTT, which can be large but is at least consistent.

With group medians, you take the median of each segment independently. The median of the parts does NOT equal the parts of the median. Consider: in the IQR cohort, the tuple with the highest `handler` might have the lowest `transport`, and vice versa. The sum of segment medians can exceed the median of totals. When this happens, `unmeasured` goes negative and must be clamped to zero, meaning the displayed segments silently sum to more than the displayed total. The design explicitly acknowledges "the segments won't sum exactly to the cohort's median total" but dismisses it as "fine because it's now genuinely small." That assumption is not validated and may not hold for high-variance segments like `transport` on remote connections.

**Fix:** The design must either:

- Define how over-summing is handled (clamp residual to zero and accept visual inaccuracy, or proportionally scale segments down to fit the total), and document the tradeoff.
- Or use a different approach: compute per-tuple full breakdowns (including residual), then take cohort medians of complete breakdowns. This preserves the sum constraint per-tuple before aggregating.

## Suggestions (nice to have)

### 3. Minimum cohort size threshold

The design does not specify what happens when the P95+ cohort has fewer than N tuples (e.g., with 20 total samples, P95+ is 1 tuple -- identical to the single-tuple picking problem you are solving). Consider requiring a minimum cohort size (e.g., 5 tuples) and either hiding the outlier bar or showing a "not enough data" indicator below that threshold. The current code has a `pairedCount < 3` guard; the new approach needs an equivalent for each cohort independently.

### 4. Segment ordering change creates a confusing split for "schmux (ours)" segments

The proposed causal ordering places `handler` first, then `transport` and `tmux + agent` (host environment), then `ws write` (schmux code). This means the "green" (schmux) color family appears in two non-contiguous positions in the bar: the left edge and the middle-right. This undermines the design's stated goal of making "color families group the buckets." Users would see green, then gray, then green again, then blue -- which is harder to scan than the current approach of grouping by ownership.

Consider either: keeping the ownership-based grouping (all schmux segments contiguous), or acknowledging the tradeoff and noting that the tooltip provides the full story.

### 5. `LatencyBreakdown` type needs explicit renaming in the design

The design renames display labels but does not specify whether the `LatencyBreakdown` type fields change (e.g., does `tmuxCmd` become `transport`? does `paneOutput` become `tmuxAgent`? does `network` become `unmeasured`?). The current type has fields: `network`, `jsQueue`, `handler`, `wsWrite`, `xterm`, `tmuxCmd`, `paneOutput`. If the field names change, the test file (`inputLatency.test.ts`, 557 lines) and the component test file (`TypingPerformance.test.tsx`, 144 lines) both need updates. If only display labels change, the `SEGMENT_LABELS` map suffices but the code remains confusing with mismatched internal/display names.

The design should state explicitly whether this is a rename-labels-only change or a rename-types-and-labels change.

### 6. The 2-second staleness timeout should be configurable or at least documented as heuristic

The staleness timeout of 2 seconds is reasonable for interactive typing but may discard valid measurements for programs with slow echo (e.g., compiling on save, or a slow LLM agent). The design should note this is a tunable heuristic and mention that it might need adjustment for specific workloads.

### 7. No mention of the `writeLiveFrame` render timing issue

Currently `markRenderTime` is called from the `writeLiveFrame` callback (terminalStream.ts line 1267), but `recordHandleOutputTime` is called immediately after (line 1269), using the same `renderStart`. This means `handleOutputTime` includes both the synchronous setup AND the async render -- it's not purely the render time. The `xterm` segment in the breakdown comes from `renderSamples` (line 387 in inputLatency.ts), which is the callback-measured render time. This is fine, but the design should note that the `xterm` segment only captures the `terminal.write()` callback time, not any subsequent browser paint/composite time.

### 8. Consider whether both bars should share a common time axis

The design shows two bars scaled independently (each bar fills its track proportional to its total). For the visual comparison to work ("the shape difference IS the insight"), both bars should share a common horizontal scale so that a segment of the same pixel width represents the same number of milliseconds. The current `BreakdownRow` scales each bar by `total/maxTotal`, which does provide a common scale -- the P50 bar will be shorter. The design should confirm this behavior is preserved.

## Verified Claims (things I confirmed are correct)

1. **Single-tuple picking is the current behavior.** Confirmed at `inputLatency.ts` lines 414-424: `getBreakdown` finds the tuple with `clientRTT` closest to the target percentile and returns that single tuple's segments.

2. **"network" is a residual, not a measurement.** Confirmed at line 394: `network = Math.max(0, infra - jsQueue)` where `infra = clientRTT - serverTotal - xterm`. It measures nothing directly.

3. **"tmux cmd" DOES include SSH round-trip for remote.** Confirmed in the server code: `sendKeysDur = t3.Sub(t2)` (websocket.go line 616) wraps the entire `tracker.SendInput()` call, which for remote sessions calls `Client.SendKeys()` (controlmode/client.go line 406) that does SSH Execute() calls. The `SendKeysTimings` struct (keyclassify.go line 153) breaks this into `MutexWait` and `ExecuteNet` sub-segments, but the `sendKeys` total sent to the client includes both.

4. **"pane output" DOES include SSH downstream for remote.** Confirmed: `echo = t4.Sub(pending.t3)` (websocket.go line 672) measures from SendKeys return to output channel arrival. For remote, the `%output` notification must travel over SSH.

5. **The sample arrays are index-aligned in the happy path.** `markReceived()` only records when `lastInputTime !== 0` and immediately resets it to 0 (inputLatency.ts line 189), so at most one RTT sample per keystroke. The server sends the binary frame first (websocket.go line 659), then the `inputEcho` sideband (line 696). The client processes them in order: `markReceived()` on the binary frame, `recordServerSegments()` on the `inputEcho` text frame. Both arrays grow by 1 per keystroke.

6. **The sideband is only sent in dev mode.** Confirmed at websocket.go line 683: `if s.devMode { ... }`. This means the breakdown feature only works with dev mode enabled.

7. **The current segment order in the UI is NOT causal.** The current `SEGMENTS` array (TypingPerformance.tsx line 244) is: `network, jsQueue, handler, wsWrite, xterm, tmuxCmd, paneOutput` -- which is grouped by ownership, not by causal flow. The design's proposed reordering to causal flow is a substantive change.

8. **The existing BreakdownRow component already supports the two-bar layout.** The current `LatencyBreakdownBars` renders two `BreakdownRow` components (P50 and P99) with proportional scaling via `maxTotal`. The design's "Typical" and "Outlier" bars would reuse this same structure, just with different labels and data sources.
