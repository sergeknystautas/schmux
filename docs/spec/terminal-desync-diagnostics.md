# Terminal Desync Diagnostics

## Problem

The xterm.js terminal in the browser occasionally gets out of sync with the actual tmux session state. Symptoms are garbled rendering — wrong colors, misaligned text, cursor in wrong place, overlapping text — most commonly during interactive TUI apps (vim, fzf, Claude Code). Refreshing the page fixes it, confirming the issue is in the streaming pipeline rather than tmux itself.

## Root Cause Candidates

1. **Dropped `%output` events** — The control mode parser uses non-blocking sends on a buffered channel (cap=100). If the channel fills, events are silently dropped. A TUI redraw emits many escape sequences rapidly; dropped sequences leave xterm.js in a diverged state.

2. **Split escape sequences** — An ANSI escape sequence like `\033[38;2;128;128;128m` could be split across two `%output` lines. While xterm.js's parser is stateful, edge cases in the octal unescaping or chunk boundaries could cause misinterpretation.

3. **Bootstrap race** — When the page loads, `capture-pane` snapshots the screen while a TUI is actively redrawing. The snapshot captures a partial redraw, and the queued live events assume a different starting state.

4. **Input filtering false positives** — The WebSocket handler filters terminal query responses (DA1, DA2, OSC 10/11). If TUI output matches these patterns, it gets silently eaten.

## Design

Two complementary systems: an always-on metrics panel and an on-demand diagnostic capture with agent-assisted analysis. **The entire diagnostics system is gated behind dev mode** — ring buffers are not allocated, stats are not sent, and the diagnostic button is not rendered in production.

### Dev Mode Gating

The diagnostics system follows the existing dev mode pattern:

- **Backend:** `s.devMode` on the Server struct controls whether ring buffers are allocated per WebSocket connection, whether stats text frames are sent, and whether `{"type": "diagnostic"}` messages are handled. When `devMode` is false, the WebSocket handler behaves exactly as it does today — zero overhead.
- **Frontend:** `versionInfo?.dev_mode` (from `/api/healthz` via `useVersionInfo()`) controls whether the metrics panel is rendered in the session detail page and whether the diagnostic capture button is shown. When false, no frontend ring buffer is allocated and no sequence break scanning occurs in `handleOutput`.

### WebSocket Protocol

The terminal WebSocket (`/ws/terminal/{id}`) already multiplexes two types of data using WebSocket's built-in frame types:

- **Binary frames** (server → client): raw terminal output bytes — the hot path
- **Text frames** (bidirectional): JSON control messages

Currently, the client sends text frames for input (`{"type": "input", ...}`) and resize (`{"type": "resize", ...}`), and the server sends text frames for displacement notifications (`{"type": "displaced"}`). The diagnostics system adds new message types to the text channel without touching the binary terminal data path:

**New server → client text messages:**

- `{"type": "stats", ...}` — periodic pipeline health metrics (every 2-5s)
- `{"type": "diagnostic", ...}` — diagnostic response with counters and tmux capture

**New client → server text messages:**

- `{"type": "diagnostic"}` — triggers a diagnostic capture

The frontend's `handleOutput` already branches on frame type (`event.data instanceof ArrayBuffer`), so the routing is straightforward:

```
Binary frame → terminal.write()           (existing, unchanged)
Text frame   → JSON.parse → switch(type)  (existing, extended)
                ├── "displaced"            (existing)
                ├── "stats"                (new → update metrics panel)
                └── "diagnostic"           (new → process diagnostic response)
```

Terminal data stays as binary frames with zero parsing overhead. Stats and diagnostic messages arrive as infrequent text frames on the same connection, routed by their `type` field. The two never interfere because WebSocket frames are atomic — a binary frame and a text frame can't interleave mid-message.

### Always-On Metrics Panel

A lightweight, always-on system that tracks terminal pipeline health in real-time, following the same pattern as `InputLatencyTracker`. Exposed on the session detail page as a collapsible panel.

**Backend metrics (sent as `{"type": "stats"}` text frames every 2-5 seconds):**

| Metric                  | Source                                   | Cost                   |
| ----------------------- | ---------------------------------------- | ---------------------- |
| Events dropped          | Parser channel non-blocking send counter | atomic increment, ~1ns |
| Events delivered        | Counter at WebSocket send                | atomic increment, ~1ns |
| Bytes delivered         | Sum of frame sizes                       | atomic add, ~1ns       |
| Throughput              | bytes/sec over 5-second sliding window   | timestamp + division   |
| Control mode reconnects | Tracker reconnect counter                | atomic increment, ~1ns |

**Frontend metrics (tracked in browser):**

| Metric                   | Source                                                | Cost   |
| ------------------------ | ----------------------------------------------------- | ------ |
| Frames received          | WebSocket onmessage counter                           | ~1ns   |
| Bytes received           | Sum of frame sizes                                    | ~1ns   |
| Bootstrap count          | Counter in handleOutput                               | ~1ns   |
| Sequence break detection | Scan for incomplete ESC sequences at frame boundaries | ~1-5μs |

**Display:** Collapsible panel in session detail page:

```
Stream: 1.2K frames | 847KB | 0 drops | 0 seq breaks
```

Expanding shows the full stats table with per-metric values.

### On-Demand Diagnostic Capture

When a desync is spotted, the user triggers a diagnostic capture via a dashboard button or keyboard shortcut. The system captures data from all pipeline layers and produces an actionable report.

#### Step 1: Freeze the Moment (<1ms)

The button sends `{"type": "diagnostic"}` over the terminal WebSocket.

- **Frontend** snapshots the xterm.js screen buffer — iterates `terminal.buffer.active` to extract every cell's character, foreground/background color, and attributes for all visible rows.
- **Frontend** freezes its ring buffer write cursor.

#### Step 2: Capture Ground Truth (1-10ms)

The backend receives the diagnostic message and:

- Sends `capture-pane -e -p` via control mode to get what tmux thinks the screen looks like (with ANSI escape sequences for colors/attributes).
- Snapshots the backend ring buffer.
- Snapshots all counters (drops, events, bytes, reconnects).
- Sends everything back as a JSON diagnostic response on the same WebSocket.

#### Step 3: Diff the Two Grids (<1ms)

The frontend parses the tmux capture into the same cell-grid format as the xterm.js snapshot and does a cell-by-cell comparison, producing a list of differing cells with their positions and values.

#### Step 4: Automated Checks

A decision tree runs through known failure modes:

1. **Drop check** — Were any events dropped? If yes, that's the answer.
2. **Diff check** — Are there any differing cells? If no, screens match (self-corrected or already refreshed).
3. **Sequence break scan** — Does the ring buffer contain incomplete escape sequences at chunk boundaries?
4. **Reconnect check** — Did control mode reconnect recently? Bootstrap after reconnect may have captured a partial state.
5. **Fallback** — No obvious cause found; likely a bootstrap race during a TUI redraw.

#### Step 5: Write Diagnostic Directory

The diagnostic is written as a directory of plain files at `~/.schmux/diagnostics/<timestamp>-<sessionId>/`. Ring buffer data and screen captures are stored as raw text files (not base64-encoded JSON), making them directly inspectable by humans and agents alike.

```
~/.schmux/diagnostics/2026-02-21T10-15-30-abc123/
├── meta.json                # Metadata, counters, automated findings
├── ringbuffer-backend.txt   # Raw terminal data as sent by the backend
├── ringbuffer-frontend.txt  # Raw terminal data as received by xterm.js
├── screen-tmux.txt          # capture-pane output (ANSI escape sequences intact)
├── screen-xterm.txt         # xterm.js buffer dump (ANSI escape sequences intact)
└── screen-diff.txt          # Human-readable diff of the two screens
```

**`meta.json`** — structured data only (no large blobs):

```json
{
  "timestamp": "2026-02-21T10:15:30Z",
  "sessionId": "abc123",
  "terminalSize": { "cols": 120, "rows": 40 },
  "counters": {
    "eventsDelivered": 12847,
    "eventsDropped": 0,
    "bytesDelivered": 2100000,
    "controlModeReconnects": 0
  },
  "automatedFindings": [
    "No drops detected",
    "47 cells differ between tmux and xterm.js",
    "No incomplete escape sequences at chunk boundaries",
    "No recent control mode reconnects"
  ],
  "verdict": "No obvious cause found. Likely a bootstrap race during TUI redraw.",
  "diffSummary": "2 rows differ, 47 cells total"
}
```

**`ringbuffer-backend.txt` / `ringbuffer-frontend.txt`** — raw terminal output with ANSI escape sequences intact. These files can be `cat`'d into a terminal to visually replay the data, diffed with standard tools, or read directly by an agent.

**`screen-tmux.txt` / `screen-xterm.txt`** — full screen captures with ANSI escape sequences. `cat screen-tmux.txt` renders what tmux shows; `cat screen-xterm.txt` renders what the browser showed.

**`screen-diff.txt`** — a human-readable diff showing which rows differ:

```
Row 15:
  tmux:  $ cd ~/project
  xterm: $ cd ~/projec█
Row 16:
  tmux:  $ make build
  xterm:  (empty)
---
2 rows differ, 47 cells total
```

#### Step 6: Spawn Agent Session for Analysis

A schmux agent session is spawned automatically with a prompt instructing it to analyze the diagnostic directory. The agent receives:

- `screen-diff.txt` — the human-readable diff showing which rows/cells differ
- `screen-tmux.txt` / `screen-xterm.txt` — the full screen captures, viewable via `cat`
- `ringbuffer-backend.txt` / `ringbuffer-frontend.txt` — raw terminal data, directly readable with ANSI sequences intact
- `meta.json` — counters, automated findings, and preliminary verdict

The agent reasons about the diff pattern, interprets escape sequences in the ring buffer, and reports the root cause.

#### Step 7: Show Results in Dashboard

The dashboard shows:

- **Immediately**: the visual screen diff (matching cells dimmed, differing cells highlighted) + counter stats + automated verdict.
- **Notification**: "Diagnostic captured — agent session `diag-abc123` is analyzing" with a link to the agent session.
- **Agent findings**: visible in the agent's terminal session in real-time.

### Ring Buffer Design

Each active session maintains two ring buffers (backend + frontend) for the diagnostic capture.

**Implementation:** Fixed-size byte ring buffer (pre-allocated `[256*1024]byte` array with write cursor). Zero allocations, zero GC pressure.

**Backend:** Written in the WebSocket handler goroutine, right before `wsConn.WriteMessage()`. One `copy()` call per output event (~50-200ns).

**Frontend:** Written in `handleOutput()`, right before `terminal.write()`. Stores reference to the received ArrayBuffer/string in a circular array (~1-5μs).

**Coverage at typical throughput:**

| Scenario                   | Throughput   | Ring buffer covers |
| -------------------------- | ------------ | ------------------ |
| TUI app (normal)           | ~50-100 KB/s | 2.5-5 seconds      |
| Interactive typing         | ~1-10 KB/s   | 25-250 seconds     |
| Build output (fast scroll) | ~1-10 MB/s   | 25-250 ms          |

The buffer is most useful during TUI usage — exactly the scenario where desyncs occur.

### Performance Impact

| Component                    | Hot path cost                | Memory             |
| ---------------------------- | ---------------------------- | ------------------ |
| Atomic counters              | ~1ns per event               | ~64 bytes          |
| Backend ring buffer (256KB)  | ~50-200ns per event (memcpy) | 256KB/session      |
| Frontend ring buffer         | ~1-5μs per event             | ~256KB/session     |
| Screen diff (on-demand)      | N/A (not on hot path)        | Transient          |
| **Total always-on overhead** | **~1-5μs per event**         | **~512KB/session** |

For context, `terminal.write()` alone takes 500-5000μs for complex TUI content. The diagnostic overhead is 0.1-1% of the existing rendering cost.
