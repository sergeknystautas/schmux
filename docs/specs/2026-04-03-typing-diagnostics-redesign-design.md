# Typing Diagnostics Redesign

## Problem

The typing performance diagnostic widget shows a per-keystroke latency breakdown, but the current implementation is misleading in two ways:

1. **Statistical method is flawed.** The P50/P99 breakdown picks a single tuple whose total RTT is closest to the target percentile. The segment values come from that one keystroke, not from representative distributions. The residual ("network") absorbs the mismatch between the picked tuple's segments and the actual percentile total, making it appear as a dominant segment when it's really a statistical artifact.

2. **Segment naming is misleading.** The residual is labeled "network" but measures nothing related to network transit — it's a catch-all for unmeasured overhead. Segments like "tmux cmd" change meaning dramatically between local (0.2ms Unix socket) and remote (77ms SSH round-trip) without the label reflecting this.

The result: the user sees large "network" segments on localhost and can't tell what's actionable vs what's physics.

## Goals

- Show **where time goes** for a typical keystroke and for an outlier keystroke
- Make it obvious **what's actionable** (schmux code) vs **what's outside our control** (SSH latency, tmux, agent behavior, browser)
- Ensure every segment **honestly measures what its label claims**

## Design

### Statistical Method: Group Medians

Replace single-tuple picking with group-based statistics. Two cohorts:

**Typical breakdown:** Select all tuples whose total RTT falls within the IQR (P25-P75). Compute the median of each segment independently within this group. This represents "where time goes on a normal keystroke."

**Outlier breakdown:** Select all tuples whose total RTT is above P95. Compute the median of each segment independently within this group. This represents "where time goes when a keystroke feels slow."

Why this works:

- No single-tuple noise — each segment value is a median across many tuples
- Comparing typical vs outlier instantly reveals which segment blows up under jitter
- The residual shrinks to something small and honest, because group medians don't produce the wild mismatches that a single-tuple pick does
- Percentiles of parts don't equal parts of percentiles, so the segments won't sum exactly to the cohort's median total — the residual absorbs the small difference, which is fine because it's now genuinely small

### Segment Naming

Rename segments for honesty and clarity:

| Old display name | New display name | What it measures                                        | Bucket           |
| ---------------- | ---------------- | ------------------------------------------------------- | ---------------- |
| handler          | **handler**      | Go WS decode + keystroke coalescing                     | schmux (ours)    |
| ws write         | **ws write**     | Serialize binary frame + WebSocket write                | schmux (ours)    |
| tmux cmd         | **transport**    | Local: Unix socket write + ack. Remote: SSH round-trip  | host environment |
| pane output      | **tmux + agent** | tmux delivers key, agent processes, tmux detects output | host environment |
| js queue         | **js queue**     | Browser event loop delay before processing WS message   | browser          |
| xterm            | **xterm**        | xterm.js parse + paint                                  | browser          |
| network          | **unmeasured**   | Residual: total minus sum of all measured segments      | catch-all        |

Key changes:

- **"network" becomes "unmeasured"** — stops implying a network problem when there isn't one
- **"tmux cmd" becomes "transport"** — correctly reflects that for remote sessions, this segment is dominated by SSH transit, not tmux processing
- **"pane output" becomes "tmux + agent"** — names the two actual participants: tmux (output detection polling) and the agent (keystroke processing)

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

- Color families group the buckets: e.g., greens for schmux (ours), grays for host environment (theirs), blues for browser
- Same segment order and colors in both bars — the shape difference IS the insight
- If the gray section grows disproportionately in the outlier bar, jitter is from SSH or the agent
- If the green section grows, we have a code-level performance issue to investigate
- "Unmeasured" only appears if non-negligible; it sits at the end as a thin slice

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

## Data Quality Fix: Staleness Timeout

Known issue #5 — stale `lastInputTime` causes bogus samples when unrelated output arrives long after a keystroke. Even with group-based stats, borderline stale samples can pollute the outlier cohort.

**Fix:** In `markReceived()`, check `performance.now() - lastInputTime`. If it exceeds a threshold (2 seconds), discard the pending measurement and reset `lastInputTime` to zero. Keystrokes that don't produce output within 2 seconds are not meaningful latency samples — the agent is thinking, not echoing.

## Known Limitation: FIFO Mismatch

Known issue #2 — the FIFO queue can pair the wrong keystroke with the wrong output when programs emit unprompted output. The existing `serverTotal > clientRTT` guard catches most of these. With group-based medians, the occasional mismatched tuple is suppressed by the median. No change needed — the new statistical method adequately mitigates this.

## Files Affected

| File                                                    | Change                                                                                            |
| ------------------------------------------------------- | ------------------------------------------------------------------------------------------------- |
| `assets/dashboard/src/lib/inputLatency.ts`              | Replace `getBreakdown()` with group-median computation; add staleness timeout in `markReceived()` |
| `assets/dashboard/src/components/TypingPerformance.tsx` | New two-bar layout; updated segment names, colors, and ordering                                   |
| `docs/typing-performance.md`                            | Update segment naming table, statistical method description, mark issues #1 and #5 as resolved    |
