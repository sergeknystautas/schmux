# Typing Diagnostics Redesign (final)

## Changes from v1

This document is the final revision, incorporating feedback from two review
rounds.

**Critical #1 (v1) — jsQueue is now per-tuple.** The MessageChannel probe lag is
paired with its corresponding keystroke tuple at recording time instead of being
stored in a separate `receiveLagSamples` array. This makes `jsQueue` a per-tuple
field that participates in cohort medians like every other segment.

**Critical #1 (v2) — MessageChannel probe race eliminated.** The v2 design's
`pendingReceiveLag` stash-and-fold mechanism had a race: the MessageChannel probe
is a task (not a microtask), and it will not have fired by the time
`recordServerSegments()` reads `pendingReceiveLag`, because both WebSocket frames
(binary + inputEcho sideband) are sent back-to-back by the server and typically
arrive in rapid succession. The fix: fire the MessageChannel probe from
`recordServerSegments()` instead of `markReceived()`. Since
`recordServerSegments()` is the last step in the keystroke lifecycle, the probe
callback fires afterward and stores the lag directly into the most recently
pushed tuple. This measures event loop congestion at sideband-processing time
rather than binary-frame-receive time — a slightly different but still valid
measurement point, and it avoids the timing race entirely. The
`pendingReceiveLag` field, the stash-and-fold mechanism, and the
`receiveLagSamples` array are all removed.

**Critical #2 (v1) — residual computed per-tuple, then medianed.** Each tuple now
computes its own residual (clamped to zero). Cohort medians are then taken across
ALL segments including the residual. The displayed total is the cohort's median
RTT, not the sum of displayed segment medians. A note explains that segment
medians may not sum to the displayed total because medians of parts do not equal
parts of medians, but each segment value is an honest representation of that
segment's distribution within the cohort.

**Suggestion 3 (v1) — minimum cohort size.** Cohorts with fewer than 5 tuples
show an "insufficient data" indicator instead of a bar.

**Suggestion 4 (v1) — causal ordering tradeoff acknowledged.** The design
explicitly notes that causal ordering creates non-contiguous schmux segments and
explains why causal ordering is preferred. Color coding and tooltips provide the
ownership grouping.

**Suggestion 5 (v1) — display-label-only rename.** Internal field names in
`LatencyBreakdown` remain unchanged. Only display labels change, via the
`SEGMENT_LABELS` map, to minimize churn across code and tests.

**Suggestion 2 (v2) — bar width overflow resolved.** The `LatencyBreakdown` type
returns both `total` (cohort median RTT, used for the label and for cross-bar
scaling) and `segmentSum` (sum of segment medians, used as the denominator for
segment width percentages within a bar). The component uses `segmentSum` for
`(value / segmentSum) * 100%` per-segment widths and `total` for the label and
the outer fill scaling (`total / maxTotal`). This prevents visual overflow when
segment medians sum to more than the cohort median RTT, while keeping the total
label honest.

**Suggestion 4 (v2) — cohort selection uses paired tuple RTTs.** P25/P75/P95
boundaries are computed from the RTTs of valid paired tuples (after the
`serverTotal > clientRTT` mismatch filter), not from the raw `samples` array or
histogram stats. This prevents mismatched high-RTT samples from shifting
percentile thresholds and causing wrong tuples to land in the outlier cohort.

---

## Problem

The typing performance diagnostic widget shows a per-keystroke latency
breakdown, but the current implementation is misleading in two ways:

1. **Statistical method is flawed.** The P50/P99 breakdown picks a single tuple
   whose total RTT is closest to the target percentile. The segment values come
   from that one keystroke, not from representative distributions. The residual
   ("network") absorbs the mismatch between the picked tuple's segments and the
   actual percentile total, making it appear as a dominant segment when it's
   really a statistical artifact.

2. **Segment naming is misleading.** The residual is labeled "network" but
   measures nothing related to network transit -- it's a catch-all for
   unmeasured overhead. Segments like "tmux cmd" change meaning dramatically
   between local (0.2ms Unix socket) and remote (77ms SSH round-trip) without
   the label reflecting this.

The result: the user sees large "network" segments on localhost and can't tell
what's actionable vs what's outside our control.

## Goals

- Show **where time goes** for a typical keystroke and for an outlier keystroke
- Make it obvious **what's actionable** (schmux code) vs **what's outside our
  control** (SSH latency, tmux, agent behavior, browser)
- Ensure every segment **honestly measures what its label claims**

## Design

### Data Collection Fix: jsQueue Becomes Per-Tuple

Currently `receiveLagSamples` is a separate array that is not index-aligned with
keystroke tuples. The current `getBreakdown()` computes a single global
P50/P99 of `receiveLagSamples` and applies it uniformly, meaning jsQueue is
identical in both P50 and P99 bars -- misleading because event loop congestion
correlates with high-latency keystrokes.

**Fix:** Fire the MessageChannel probe from `recordServerSegments()` and attach
the result directly to the per-keystroke tuple.

1. `recordServerSegments()` pushes the new tuple into the tuples array, then
   fires a MessageChannel probe: `channel.port2.postMessage(null)`. The
   `onmessage` handler computes the lag and stores it into the most recently
   pushed tuple's `receiveLag` field.
2. Because `recordServerSegments()` is the last synchronous step in the
   keystroke lifecycle, the probe's task is queued after all keystroke processing
   is complete. When the probe fires (in a subsequent macrotask), it measures
   the actual event loop congestion at that point and writes directly into the
   tuple -- no stashing, no folding, no race.
3. The `FullTuple` type inside `getBreakdown()` uses the per-tuple `receiveLag`
   value instead of a global percentile. This means jsQueue varies per-tuple
   and will differ between the Typical and Outlier cohorts.
4. `getBreakdown()` treats tuples with `receiveLag === undefined` (probe has not
   fired yet or MessageChannel unavailable) as having `receiveLag = 0`.

Fallback: if MessageChannel is unavailable in the environment, fall back to the
legacy `lagSamples` send-time probe. In this case jsQueue uses a global
percentile matched to the cohort level, and the design notes this is an
estimate.

### Statistical Method: Group Medians

Replace single-tuple picking with group-based statistics. Two cohorts:

**Typical breakdown:** Select all tuples whose total RTT falls within the IQR
(P25-P75). Compute the median of each segment independently within this group.
This represents "where time goes on a normal keystroke."

**Outlier breakdown:** Select all tuples whose total RTT is above P95. Compute
the median of each segment independently within this group. This represents
"where time goes when a keystroke feels slow."

#### Cohort selection

P25/P75/P95 boundaries are computed from the RTTs of **valid paired tuples**
(after the `serverTotal > clientRTT` mismatch filter), not from the raw `samples`
array or the histogram stats. This prevents mismatched high-RTT samples (which
are discarded from the tuples array) from shifting percentile thresholds and
causing wrong tuples to land in the outlier cohort.

#### Minimum cohort size

Each cohort requires at least **5 tuples** to produce a bar. If a cohort has
fewer than 5 tuples, the bar is replaced with an "insufficient data" indicator.
This prevents the outlier bar from degenerating into single-tuple picking when
the sample set is small (e.g., with 20 total samples, P95+ is 1 tuple). The
current code has a `pairedCount < 3` guard; this replaces it with a per-cohort
minimum.

#### Residual computation: per-tuple first, then medianed

The residual is computed per-tuple, not derived from cohort aggregates:

1. For each tuple, compute:
   `residual = max(0, clientRTT - handler - transport - tmuxAgent - wsWrite - jsQueue - xterm)`
2. Clamp to zero. A negative value means the sum of measured segments exceeds
   the total for this keystroke (possible due to measurement overlap). Clamping
   discards the negative rather than distorting other segments.
3. Take cohort medians of ALL segments including the residual. Each segment's
   cohort median (including the residual's cohort median) is an honest
   representation of that segment's distribution within the cohort.

The displayed total is the **cohort's median RTT**, not the sum of displayed
segment medians. The segment medians may not sum to the displayed total because
**medians of parts do not equal parts of medians**. This is expected and correct:
each displayed segment value honestly represents the typical value of that
segment for keystrokes in the cohort. The total honestly represents the typical
end-to-end latency. They are independently meaningful statistics from the same
cohort, not components of a decomposition.

Why this works:

- No single-tuple noise -- each segment value is a median across many tuples
- Comparing typical vs outlier instantly reveals which segment blows up under
  jitter
- The residual is computed per-tuple where the sum constraint holds, then
  aggregated, so it starts honest and stays honest
- Over-summing (Known Issue #6) is resolved: per-tuple clamping prevents
  negative residuals, and the displayed total is independent of the segment sum

### Segment Naming

This is a **display-label-only rename**. Internal field names in
`LatencyBreakdown` remain unchanged (`network`, `jsQueue`, `handler`, `wsWrite`,
`xterm`, `tmuxCmd`, `paneOutput`, `total`) to minimize churn across code and
tests. The `SEGMENT_LABELS` map in `TypingPerformance.tsx` handles the mapping
from internal field names to user-facing display labels.

| Internal field | Old display name | New display name | What it measures                                        | Bucket           |
| -------------- | ---------------- | ---------------- | ------------------------------------------------------- | ---------------- |
| `handler`      | handler          | **handler**      | Go WS decode + keystroke coalescing                     | schmux (ours)    |
| `wsWrite`      | ws write         | **ws write**     | Serialize binary frame + WebSocket write                | schmux (ours)    |
| `tmuxCmd`      | tmux cmd         | **transport**    | Local: Unix socket write + ack. Remote: SSH round-trip  | host environment |
| `paneOutput`   | pane output      | **tmux + agent** | tmux delivers key, agent processes, tmux detects output | host environment |
| `jsQueue`      | js queue         | **js queue**     | Browser event loop delay before processing WS message   | browser          |
| `xterm`        | xterm            | **xterm**        | xterm.js parse + paint (terminal.write callback time)   | browser          |
| `network`      | network          | **unmeasured**   | Residual: total minus sum of all measured segments      | catch-all        |

Key changes:

- **"network" becomes "unmeasured"** -- stops implying a network problem when
  there isn't one
- **"tmux cmd" becomes "transport"** -- correctly reflects that for remote
  sessions, this segment is dominated by SSH transit, not tmux processing
- **"pane output" becomes "tmux + agent"** -- names the two actual
  participants: tmux (output detection polling) and the agent (keystroke
  processing)

Note: the `xterm` segment captures `terminal.write()` callback time only. It
does not include subsequent browser paint/composite time, which is unmeasurable
from JavaScript. Any gap between the write callback and the actual pixel update
is absorbed into the unmeasured residual.

### Visual Layout

Two stacked horizontal bars, one above the other:

```
Typical (P50 cohort):
  ┌──────┬──────────────────────────────────────┬─────────┐
  │schmux│         host environment             │ browser │
  └──────┴──────────────────────────────────────┴─────────┘

Outlier (P95+ cohort):
  ┌──────┬──────────────────────────────────────────────────────────┬─────────┐
  │schmux│              host environment                           │ browser │
  └──────┴──────────────────────────────────────────────────────────┴─────────┘
```

- Color families group the buckets: e.g., greens for schmux (ours), grays for
  host environment (theirs), blues for browser
- Same segment order and colors in both bars -- the shape difference IS the
  insight
- Both bars share a common horizontal time axis: a segment of the same pixel
  width represents the same number of milliseconds in both bars. The existing
  `BreakdownRow` scaling via `total/maxTotal` already provides this behavior
  (the Typical bar will be shorter). This must be preserved.
- If the gray section grows disproportionately in the outlier bar, jitter is
  from SSH or the agent
- If the green section grows, we have a code-level performance issue to
  investigate
- "Unmeasured" only appears if non-negligible; it sits at the end as a thin
  slice
- If a cohort has fewer than 5 tuples, the bar is replaced with a centered
  "insufficient data" label in muted text

#### Bar width computation

The `LatencyBreakdown` type returns two fields for sizing:

- **`total`**: the cohort's median RTT. Used for the numeric label displayed on
  the bar and for cross-bar scaling (the outer fill width is
  `total / maxTotal`, preserving the shared time axis between the Typical and
  Outlier bars).
- **`segmentSum`**: the sum of all segment medians (including the residual).
  Used as the denominator for per-segment width percentages within a bar:
  `(segmentValue / segmentSum) * 100%`.

This separation prevents visual overflow when segment medians sum to more than
the cohort median RTT (which is expected, since medians of parts do not equal
parts of medians). Each segment's relative proportion within the bar is honest,
the total label is honest, and the cross-bar scaling is honest. They just use
different denominators because they answer different questions.

### Segment Ordering Within Bars

Left to right, following the causal flow of a keystroke:

1. **handler** (schmux receives and decodes)
2. **transport** (keystroke travels to tmux)
3. **tmux + agent** (program processes and tmux detects output)
4. **ws write** (schmux sends output frame)
5. **js queue** (browser event loop picks it up)
6. **xterm** (terminal renders)
7. **unmeasured** (residual, if any)

This mirrors the actual data flow from keypress to pixels.

**Tradeoff: non-contiguous schmux segments.** Causal ordering places schmux
segments (handler, ws write) in two non-contiguous positions in the bar: at the
start and in the middle. This means the green color family appears, is
interrupted by the gray host environment segments, then appears again. This is a
deliberate tradeoff. Causal ordering is preferred because it tells the story of
a keystroke's journey through the system -- the user reads left-to-right and
follows the data flow. The color coding provides ownership grouping at a glance
(green = ours, gray = theirs, blue = browser), and the tooltip explicitly lists
each segment with its bucket, so the ownership information remains accessible
even though the segments are not spatially grouped by owner.

## Data Quality Fix: Staleness Timeout

Known issue #5 -- stale `lastInputTime` causes bogus samples when unrelated
output arrives long after a keystroke. Even with group-based stats, borderline
stale samples can pollute the outlier cohort.

**Fix:** In `markReceived()`, check `performance.now() - lastInputTime`. If it
exceeds a threshold (2 seconds), discard the pending measurement and reset
`lastInputTime` to zero. Keystrokes that don't produce output within 2 seconds
are not meaningful latency samples -- the agent is thinking, not echoing.

Note: the 2-second threshold is a heuristic tuned for interactive typing. Some
workloads (compiling on save, slow LLM agents) have legitimate echo delays
exceeding 2 seconds, but these are not the kind of "typing latency" this widget
measures. If future use cases require a different threshold, it can be made
configurable.

## Known Limitation: FIFO Mismatch

Known issue #2 -- the FIFO queue can pair the wrong keystroke with the wrong
output when programs emit unprompted output. The existing
`serverTotal > clientRTT` guard catches most of these. With group-based medians,
the occasional mismatched tuple is suppressed by the median. No change needed --
the new statistical method adequately mitigates this.

## Files Affected

| File                                                    | Change                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                     |
| ------------------------------------------------------- | -------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `assets/dashboard/src/lib/inputLatency.ts`              | (1) Move MessageChannel probe into `recordServerSegments()`: after pushing the tuple, fire the probe and have the callback store the lag directly into the most recently pushed tuple's `receiveLag` field. Remove `receiveLagSamples`, `pendingReceiveLag`, and the stash-and-fold mechanism. (2) Replace `getBreakdown()` with cohort-median computation: compute P25/P75/P95 boundaries from valid paired tuple RTTs (not from `samples`), build per-tuple residuals (clamped to 0), select IQR and P95+ cohorts, enforce 5-tuple minimum, compute per-segment medians including residual. Return both `total` (cohort median RTT) and `segmentSum` (sum of segment medians) in `LatencyBreakdown`. (3) Add staleness timeout in `markReceived()`. **Note:** if any new fields are added to the tracker state, the `TrackerSnapshot` type and the `switchMachine()` save/restore logic must be updated to include them. |
| `assets/dashboard/src/components/TypingPerformance.tsx` | (1) Update `SEGMENT_LABELS` map with new display names. (2) Update `SEGMENTS` array to causal ordering. (3) Update `SEGMENT_COLORS` for the three-bucket color families. (4) Add "insufficient data" rendering when cohort is below minimum size. (5) Update `BreakdownRow` to use `segmentSum` as denominator for per-segment width percentages, and `total` for the label and outer fill scaling.                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                        |
| `docs/typing-performance.md`                            | Update segment naming table, statistical method description, mark issues #1, #5, and #6 as resolved.                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                       |
