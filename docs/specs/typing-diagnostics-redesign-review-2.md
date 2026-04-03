VERDICT: NEEDS_REVISION

## Summary Assessment

The revision adequately addresses Critical #2 (residual computation) and most of the suggestions from round 1. However, the per-tuple jsQueue pairing mechanism has a race condition that the design does not acknowledge: the MessageChannel probe is asynchronous and will not have fired by the time `recordServerSegments()` is called, making the proposed `pendingReceiveLag` stash-and-fold approach unreliable without an explicit timing guarantee. There is also a secondary issue with how the bar visualization handles the divergence between segment sum and cohort median total.

## Critical Issues (must fix)

### 1. MessageChannel probe has not fired when recordServerSegments() runs

The design says: "`markReceived()` captures the probe and stashes it; `recordServerSegments()` folds it into the per-keystroke tuple." This relies on the MessageChannel probe callback firing between the `markReceived()` call and the `recordServerSegments()` call. It does not.

Here is the actual event ordering in the browser:

1. **WebSocket binary frame arrives** (macrotask). `handleOutput()` runs synchronously. At line 1265, `markReceived()` fires a MessageChannel probe: `channel.port2.postMessage(null)`. The `onmessage` handler is queued as a **separate task** (MessageChannel dispatches are tasks, not microtasks -- see HTML spec "ports as the basis of an object-oriented event loop").
2. **WebSocket inputEcho text frame arrives** (next macrotask -- the server sends it immediately after the binary frame at websocket.go line 696). `handleOutput()` runs synchronously. At line 1312, `recordServerSegments()` is called.
3. **MessageChannel probe handler fires** (macrotask, queued after step 1). This is when the lag measurement is actually computed.

So at step 2, when `recordServerSegments()` tries to read `pendingReceiveLag`, the probe has not yet fired. The value will be `null` in the common case, not because MessageChannel is unavailable in the environment (the design's stated fallback reason), but because the probe task has not been scheduled yet.

This is not a rare edge case -- it is the expected ordering. The design's statement "the microtask should complete before the server sideband text frame arrives" is incorrect on two counts: (a) MessageChannel handlers are tasks, not microtasks, and (b) the two WebSocket frames are sent back-to-back by the server (lines 659 and 696 of websocket.go, with no yielding between them), so they will typically be delivered to the browser in rapid succession, often within the same network packet.

**Fix options:**

(a) **Defer the fold.** Instead of having `recordServerSegments()` read `pendingReceiveLag`, have `recordServerSegments()` store the tuple in a pending slot, and have the MessageChannel probe callback attach its measurement to the most recently stored tuple. This reverses the dependency: the probe fires later and attaches to the tuple that is already stored. The tuple would initially have `receiveLag: undefined`, and the probe callback would fill it in. `getBreakdown()` would treat tuples with `receiveLag === undefined` as having `receiveLag = 0` (or skip them).

(b) **Move the probe into `recordServerSegments()`.** Since `recordServerSegments()` is called after `markReceived()` and is the last step in the keystroke lifecycle, fire the MessageChannel probe from there instead. The probe then measures event loop congestion at the moment the sideband is processed, which is a slightly different (but still meaningful) measurement point. The probe callback stores the result directly into the tuple that was just pushed.

(c) **Accept the global percentile for jsQueue.** Keep the existing `receiveLagSamples` array and compute jsQueue as a cohort-matched global percentile (median of receiveLagSamples for the Typical cohort, P95 for the Outlier cohort). This is the fallback the design already describes. The design should acknowledge this is the primary path, not a fallback, and explain why it is acceptable (jsQueue is typically small enough that per-tuple variation matters less than for transport or paneOutput).

## Suggestions (nice to have)

### 2. Bar width ambiguity when segment sum exceeds cohort median total

The design says: "The segment bar widths are proportional to their values relative to the sum of segments (so the bar visually fills), but the total label reflects the true median RTT."

This creates a visual inconsistency. The current `BreakdownRow` component computes each segment's width as `(value / total) * 100%` where `total` is `breakdown.total` (line 305 of TypingPerformance.tsx). If `breakdown.total` is the cohort median RTT but the segment medians sum to more than that, the segments will overflow 100% of the fill width. If `breakdown.total` is the sum of segment medians, then the total label will be misleading.

The design needs to pick one:

- **Option A**: Set `breakdown.total` to the cohort median RTT. Use `sum of segment medians` as the denominator for bar width percentages. Then `scale = cohortMedianRTT / maxCohortMedianRTT` for the outer fill. This makes the bar slightly wider than the total label suggests (the segments visually extend beyond what the total would imply), but each segment's relative size is correct.

- **Option B**: Set `breakdown.total` to the cohort median RTT. Use `breakdown.total` as the denominator for bar width percentages (current behavior). If segments sum to more than total, clamp them proportionally so they fit. This changes individual segment values visually but keeps the bar consistent with the total.

- **Option C**: Return both `total` (cohort median RTT for the label) and `segmentSum` (for bar width computation) in the `LatencyBreakdown` type. The component uses `segmentSum` for segment width percentages and `total` for the label.

The current code at line 305 (`const pct = (value / total) * 100`) will produce percentages that sum to >100% if the segment medians exceed the cohort median total. This will cause visual overflow in the bar. The design should specify the concrete fix.

### 3. TrackerSnapshot needs a new field for pendingReceiveLag

The `switchMachine()` method saves and restores all tracker state via `TrackerSnapshot` (lines 90-100 and 124-163 of inputLatency.ts). Whatever mechanism is chosen for per-tuple jsQueue (pending slot, deferred fold, etc.), the snapshot type and the save/restore logic must be updated. If a `pendingReceiveLag` field is added to the tracker, it must be included in the snapshot. The design's "Files Affected" section does not mention this, though it is minor.

### 4. Cohort selection should use the paired tuple RTTs, not all samples

The design says cohorts are selected based on total RTT falling within IQR or above P95. The current `samples` array contains RTT values from `markReceived()`, which includes samples that get discarded by the `serverTotal > clientRTT` guard (line 391). The percentile thresholds should be computed from the filtered `tuples` array (after discarding mismatches), not from the raw `samples` array. Otherwise a mismatched high-RTT sample could shift the P95 threshold, causing the wrong tuples to land in the outlier cohort.

The design does not specify which array the P25/P75/P95 boundaries are computed from. It should state explicitly that boundaries come from the RTTs of the valid paired tuples, not from the global `samples` array or the histogram stats.

### 5. Consider documenting the xterm segment limitation more prominently

The design correctly notes (at the end of the Segment Naming section) that the `xterm` segment only captures `terminal.write()` callback time, not browser paint/composite time. This is good. However, this limitation is somewhat buried. Since the xterm segment will have a "browser" color family, users might conclude that a small xterm value means the browser is fast, when in reality paint time could be significant and is absorbed into the unmeasured residual. Consider adding a note in the tooltip for the xterm segment (e.g., "terminal parse only, excludes paint").

## Verified Claims (things I confirmed are correct)

1. **Critical #2 from round 1 is adequately addressed.** The per-tuple residual computation (step 1-3 in the design) is correct. Computing `residual = max(0, clientRTT - handler - transport - tmuxAgent - wsWrite - jsQueue - xterm)` per-tuple and then taking the cohort median of residuals is sound. The sum constraint holds per-tuple (the residual absorbs the difference), and the median aggregation is an honest representation of the residual's distribution. The design correctly notes that segment medians will not sum to the cohort median total and explains why.

2. **The minimum cohort size of 5 tuples is reasonable.** With 100 samples, the P95+ cohort would have approximately 5 tuples -- just at the threshold. With fewer total samples, the outlier bar will correctly show "insufficient data" rather than degenerating to single-tuple picking. This addresses Suggestion #3 from round 1.

3. **The causal ordering tradeoff is adequately acknowledged.** The design explicitly notes the non-contiguous schmux segments and explains why causal ordering is preferred. This addresses Suggestion #4 from round 1.

4. **The display-label-only rename is clearly specified.** The design explicitly states internal field names remain unchanged, and only `SEGMENT_LABELS` changes. This addresses Suggestion #5 from round 1.

5. **The staleness timeout is documented as a heuristic.** The design notes the 2-second threshold is tunable and explains why it might not suit all workloads. This addresses Suggestion #6 from round 1.

6. **The common time axis behavior is confirmed as preserved.** The design correctly identifies that the existing `BreakdownRow` scaling via `total/maxTotal` provides a shared horizontal scale, and explicitly states this must be preserved. This addresses Suggestion #8 from round 1.

7. **The server sends binary frame then inputEcho back-to-back.** Confirmed at websocket.go lines 659 and 696. The binary frame is sent via `conn.WriteMessage(websocket.BinaryMessage, frameBuf)`, and the inputEcho text frame is sent immediately after via `conn.WriteMessage(websocket.TextMessage, sideband)` with no goroutine yield between them.

8. **The FIFO mismatch mitigation assessment is correct.** The `serverTotal > clientRTT` guard (line 391 of inputLatency.ts) discards obvious mismatches, and group-based medians will suppress the occasional survivor. The design's conclusion that no further change is needed is reasonable.
