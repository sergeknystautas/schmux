# Scrollback Integrity During High-Throughput Output

Design spec for fixing terminal scrollback gaps that appear when agents produce output faster than the rendering pipeline can deliver.

**Status**: Proposed

## Problem Statement

When an AI agent produces output at high throughput (e.g., printing hundreds of lines in quick succession), users see **gaps in scrollback** — missing lines when they scroll up in the xterm.js terminal. The missing content is never delivered to the browser.

This was confirmed empirically: outputting 100 numbered lines from Claude Code resulted in lines 1-58 being absent from xterm.js scrollback, while the tracker's drop-count logging showed **zero channel drops**. The data loss occurs before the Go channel.

### What the gap looks like

```
┌─────────────────────────────────────────┐
│ (previous output, e.g. code diff)       │
│ ...                                     │
│     t.sessionID, t.droppedBytes)        │  ← end of previous content
│  59                                     │  ← gap: lines 1-58 missing
│  60                                     │
│  61                                     │
│  ...                                    │
│  100                                    │
└─────────────────────────────────────────┘
```

## Root Cause

The `SessionTracker` streams terminal output by running `tmux attach-session -t =<name>` inside a PTY (pseudo-terminal). tmux treats this attached PTY as a **display client** — it sends rendered screen frames, not a raw byte stream of all output.

During rapid output:

```
Agent writes 100 lines in ~10ms

tmux internal behavior:
  - Processes all 100 lines into its internal scrollback buffer
  - Renders screen updates to attached clients every ~16ms
  - Only the FINAL visible screen state is sent over the PTY
  - Lines 1-58 scrolled past between renders and are never transmitted

What the PTY receives:
  - One or two screen snapshots containing lines ~59-100
  - Lines 1-58 exist in tmux's scrollback but were never sent
```

This is a fundamental property of tmux's client model. tmux is designed for interactive terminal use where a human watches the screen — screen-snapshot delivery is correct for that use case. But for a scrollback-preserving web viewer, it means content is structurally lost during bursts.

### The pipeline has three independent loss mechanisms

```
┌──────────────────────────────────────────────────────────────┐
│ 1. tmux PTY attachment (screen snapshots)      CONFIRMED     │
│    Lines that scroll past between renders are               │
│    never sent to the attached PTY client.                   │
│    This is the PRIMARY cause of scrollback gaps.            │
├──────────────────────────────────────────────────────────────┤
│ 2. Go channel overflow (64 slots, silent drop)  NOT OBSERVED │
│    Added logging confirms zero drops during                 │
│    normal agent output bursts. Would only trigger           │
│    under extreme sustained throughput.                      │
├──────────────────────────────────────────────────────────────┤
│ 3. xterm.js scrollback limit (1000 lines)       STRUCTURAL  │
│    Oldest content evicted when buffer fills.                │
│    Not a bug — just a capacity limit.                       │
└──────────────────────────────────────────────────────────────┘
```

## Prior Art: What Was Tried

Three commits attempted to fix this and were reverted (`7ef6b0c3`):

| Commit     | Approach                                                                                                                                                  | Outcome                                                                                                                                                |
| ---------- | --------------------------------------------------------------------------------------------------------------------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------ |
| `8bed0d39` | `sendCoalesced` — buffer dropped chunks instead of discarding, blocking send at 1MB                                                                       | Addressed problem #2 (channel drops), not #1 (the actual cause)                                                                                        |
| `894eae57` | `requestAnimationFrame` write batching on browser side                                                                                                    | Correct optimization, reduced xterm.js render pressure. Did not cause any bug.                                                                         |
| `d6812459` | Scrollback sync — after burst quiets, re-capture from tmux and send `"full"` resync. Also replaced `\x1b[2J` (Erase Display) with `\x1b[999S` (Scroll Up) | **The `\x1b[2J→\x1b[999S` replacement caused a rendering glitch** (terminal scrolling up one line during typing). The resync concept itself was sound. |

Key takeaway: the resync idea was correct, the escape sequence rewriting was the bug. The rAF batching was also correct but was reverted as collateral.

## Prior Art: Remote Sessions Already Solve This

The remote session infrastructure (`internal/remote/controlmode/`) uses **tmux control mode** (`tmux -C`) which provides structured `%output` events containing **every byte** a pane produces — not screen snapshots. This is already working in production for remote hosts.

```
Control mode protocol:
  %output %5 line 1\012
  %output %5 line 2\012
  %output %5 line 3\012
  ...every line is delivered as a separate event...
```

---

## Options

### Option A: Periodic capture-pane Resync (Scrollback Sync v2)

After a burst of output (measured by byte volume over a time window), pause briefly, then run `tmux capture-pane` to get the authoritative scrollback from tmux and send it as a `"full"` message to resync xterm.js.

This is the same concept as the reverted `d6812459`, but without the `\x1b[2J→\x1b[999S` escape sequence rewriting that caused the rendering bug.

```
Normal flow (unchanged):
  PTY read → channel → WebSocket → xterm.js

After burst detected (new):
  output > threshold for T ms, then quiet for 300ms
    → tmux capture-pane -e -p -S -<scrollback>
    → send {"type":"full","content":"…"}
    → xterm.js resets and rewrites with complete scrollback
```

**Implementation:**

- Server-side only: add a timer in `handleTerminalWebSocket()` that fires after 300ms of quiet following a burst (>4KB output).
- On fire: `CaptureLastLines()` and send as `"full"` message.
- Do NOT rewrite any escape sequences — send the capture-pane output as-is.

**Pros:**

- Minimal code change (~30 lines in `websocket.go`)
- No changes to the tracker, no changes to the frontend
- Self-healing: even if PTY attachment drops data, the resync fills it back in
- Works with existing xterm.js scrollback buffer
- Proven concept (the reverted version worked except for the escape rewriting bug)

**Cons:**

- Visual flash: `terminal.reset()` + `terminal.write()` causes a brief flicker when resync fires
- Cursor position may shift if agent is actively writing during the resync window
- Heuristic-based: threshold and quiet-period tuning required (what counts as "a burst"?)
- Resync captures only what tmux still has (default `history-limit` 2000 lines)
- Doesn't fix the gap in real-time — gap exists until the quiet period triggers resync

### Option B: tmux Control Mode for Local Sessions

Replace the PTY attachment (`tmux attach-session`) with control mode (`tmux -CC` or `tmux attach -t <name> -C`) for the output stream. Control mode delivers `%output` events for every byte written to the pane, not screen snapshots.

```
Current (PTY attachment):
  tmux attach-session -t =<name>  →  PTY  →  screen snapshots

Proposed (control mode):
  tmux attach-session -t =<name> -C  →  stdin/stdout  →  %output events
  Every byte the agent writes appears as:
    %output %<paneID> <escaped-data>
```

**Implementation:**

- Modify `SessionTracker.attachAndRead()` to use control mode instead of PTY.
- Reuse the existing `controlmode.Parser` and `controlmode.Client` from `internal/remote/controlmode/`.
- The parser already handles `%output` events and unescapes octal sequences.
- Input would go through `send-keys` commands via control mode (already implemented: `Client.SendKeys()`).
- Resize would go through `resize-window` commands via control mode (already implemented: `Client.ResizeWindow()`).

**Pros:**

- **Solves the root cause**: every byte is delivered, not screen snapshots
- No heuristics, no timers, no resync flicker
- Reuses existing, tested control mode infrastructure
- Consistent architecture: local and remote sessions would use the same streaming model
- No escape sequence rewriting or filtering needed (control mode data is raw pane output)

**Cons:**

- Significant refactor of `SessionTracker` — the PTY model is deeply embedded (PTY sizing, `creack/pty`, `ptmx.Read()` loop, `SendInput` via PTY write)
- Control mode adds protocol parsing overhead (regex matching per line, octal unescaping)
- Control mode output is line-buffered by tmux (one `%output` per write, flushed on newline or buffer threshold) — may slightly increase latency for single-character echo compared to raw PTY reads
- Untested at scale for local sessions — remote sessions have higher inherent latency so control mode overhead is hidden
- Control mode requires managing the tmux command/response protocol (though the Client already handles this)
- Need to handle control mode connection lifecycle (reconnect, session disappears, etc.)

### Option C: Parallel Capture-Pane Log + PTY Stream

Keep the PTY attachment for real-time output (low latency, good for interactive echo), but run a parallel goroutine that periodically captures tmux scrollback and writes to a ring buffer. When xterm.js connects or detects a gap, it can request a full resync from this buffer.

```
                    ┌─── PTY attachment (real-time, lossy) ──→ WebSocket → xterm.js
tmux session ──────┤
                    └─── capture-pane goroutine (periodic, complete) ──→ ring buffer
                                                                           │
                         xterm.js requests resync ─────────────────────────┘
```

**Implementation:**

- New goroutine in `SessionTracker` that runs `capture-pane` every N seconds (e.g., 2s) and stores the result in a ring buffer.
- New WebSocket message type `"resync-request"` from client.
- When client detects potential gap (e.g., large jump in cursor position, or user scrolls to top and sees truncated history), it requests resync.
- Server responds with latest capture-pane snapshot as `"full"` message.

**Pros:**

- No change to the hot path (PTY streaming stays fast)
- Client-driven resync avoids unnecessary flicker
- Ring buffer provides scrollback history even after PTY attachment drops data
- Could increase capture frequency during bursts (adaptive)

**Cons:**

- `capture-pane` every 2 seconds adds CPU and tmux command overhead per session
- Multiplied by N sessions, this could become expensive (e.g., 10 sessions = 5 capture-pane calls/sec)
- Client-side gap detection is unreliable — how does xterm.js know lines are missing?
- Two sources of truth (PTY stream and capture buffer) can desync
- Added complexity: ring buffer lifecycle, cleanup, memory management
- Doesn't help in real-time — user still sees gaps until they scroll up and trigger resync

### Option D: Increase Scrollback + Do Nothing Else

Accept that the PTY attachment model is inherently lossy during bursts and mitigate the impact by increasing buffer sizes so more content survives.

**Changes:**

- Set tmux `history-limit` to 10000 (at session creation)
- Increase `bootstrapCaptureLines` to 5000
- Increase xterm.js `scrollback` to 5000
- On WebSocket reconnect, the bootstrap captures 5000 lines from tmux — filling in content that was lost during the live stream

**Pros:**

- Trivial to implement (~5 lines changed)
- No architectural changes
- Reconnecting to a session always shows a complete recent history (from tmux's buffer)
- No flicker, no resync, no timer heuristics

**Cons:**

- Does NOT fix the real-time gap — gaps still appear during the live session
- Only helps when you disconnect and reconnect (or refresh the page)
- Increased memory usage per session (5x scrollback on both tmux and xterm.js sides)
- Users will still see gaps while watching live output — the fix only applies retroactively

### Option E: Hybrid — rAF Batching (re-land) + Capture-Pane Resync v2

Combine the two non-buggy parts of the reverted work:

1. **Re-land `requestAnimationFrame` batching** on the browser side (this was not the source of the rendering bug — the `\x1b[2J→\x1b[999S` rewrite was). This reduces xterm.js render pressure, which in turn reduces WebSocket backpressure, which reduces the likelihood of channel overflow at scale.

2. **Capture-pane resync v2** (Option A) without escape sequence rewriting. After a burst quiets, resync from tmux's authoritative scrollback.

3. **Increase scrollback** (Option D) so the resync captures more history.

```
Browser:
  ws.onmessage → accumulate in pendingAppend
  requestAnimationFrame → flush all pending as single terminal.write()
  Result: ≤60 writes/sec instead of hundreds

Server:
  Burst detected (>4KB output)
  300ms quiet period
  → capture-pane resync as "full" message (no escape rewriting)

Buffers:
  tmux history-limit: 10000
  bootstrap capture: 5000 lines
  xterm.js scrollback: 5000 lines
```

**Pros:**

- Addresses all three loss mechanisms (PTY snapshots, channel overflow, buffer limits)
- rAF batching is a standalone improvement with no risk (was never the source of the bug)
- Resync fills in content lost during burst, no escape rewriting
- Larger buffers mean resync recovers more history
- Each piece is independently testable

**Cons:**

- Resync still causes a visual flash (full terminal reset + rewrite)
- Heuristic tuning still needed (burst threshold, quiet period)
- Three changes to coordinate, though each is small and independent
- Increased memory usage from larger scrollback buffers

---

## Comparison Matrix

| Criterion                | A: Resync v2            | B: Control Mode       | C: Parallel Capture       | D: Bigger Buffers | E: Hybrid             |
| ------------------------ | ----------------------- | --------------------- | ------------------------- | ----------------- | --------------------- |
| Fixes real-time gaps     | Delayed (after quiet)   | Yes, fully            | Delayed (on request)      | No                | Delayed (after quiet) |
| Implementation size      | ~30 lines               | ~200+ lines refactor  | ~100 lines                | ~5 lines          | ~80 lines             |
| Risk of regressions      | Low                     | Medium                | Medium                    | None              | Low                   |
| Visual artifacts         | Flash on resync         | None                  | Flash on resync           | None              | Flash on resync       |
| CPU overhead             | Low (only after bursts) | Low (parsing)         | Medium (periodic capture) | None              | Low                   |
| Reuses existing code     | No                      | Yes (controlmode pkg) | No                        | No                | No                    |
| Architecture consistency | Keeps PTY model         | Unifies with remote   | Dual-path complexity      | Keeps PTY model   | Keeps PTY model       |

## Recommendation

**Short term: Option E (Hybrid)** — re-land rAF batching, add capture-pane resync v2 (without escape rewriting), and increase scrollback. This addresses the observed problem with minimal risk and builds on known-correct code. Each piece is small and independently revertable.

**Long term: Option B (Control Mode)** — migrating local sessions to control mode is architecturally correct and eliminates the root cause. It should be pursued as a separate workstream once the immediate UX issue is addressed, since it's a larger refactor that benefits from careful testing. The control mode infrastructure is already battle-tested in the remote session path.

Option D (bigger buffers) should be applied regardless — it's zero-risk and helps on every reconnect.
