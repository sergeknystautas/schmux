# Terminal Pipeline: Agent → tmux → WebSocket → xterm.js

How terminal output flows from AI agents to the browser, including the sync/correction mechanism, diagnostics, and known edge cases.

**Last updated:** 2026-02-27
**Supersedes:** Previously separate specs for control mode streaming, terminal sync, scrollback integrity, terminal hybrid streaming, and cursor position analysis — all consolidated here.

---

## Data Flow Overview

```
┌──────────────────────┐
│  Agent Process       │  (Claude Code, Codex, etc.)
│  stdout/stderr       │
└──────────┬───────────┘
           │ writes to tmux PTY
           ▼
┌──────────────────────┐
│  tmux session        │  history-limit: 10000
│  (detached)          │  window-size: manual
└──────────┬───────────┘
           │
     ┌─────┴───────────────────────────────────────────┐
     │  tmux -C attach-session (control mode)          │
     │                                                  │
     │  %output events → Parser → Client.processOutput()│
     │    chan(1000)       chan(1000)                    │
     │    drop on full    per-pane fan-out, drop on full│
     │                                                  │
     │  SessionTracker.fanOut()                         │
     │    chan(1000), drop on full                      │
     │    + OutputLog (sequenced ring buffer, 50K entries)
     └──────────┬──────────────────────────────────────┘
                │
                ├──→ subscriber chan (WS client A)
                ├──→ subscriber chan (WS client B)
                └──→ outputCallback (preview autodetect)
                │
                ▼
     ┌──────────────────────────────────────────────────────┐
     │ handleTerminalWebSocket()                            │
     │                                                      │
     │  Bootstrap: replay OutputLog → chunked binary frames │
     │    each frame: [8-byte seq header][terminal data]    │
     │    fallback: capture-pane -S -5000 (if log empty)    │
     │                                                      │
     │  Steady-state: each output event from tracker        │
     │    → escbuf.SplitClean() (escape holdback)           │
     │    → encodeSequencedFrame(seq, data)                 │
     │    → conn.WriteMessage(BinaryMessage)                │
     │                                                      │
     │  wsConn mutex serializes writes                      │
     │  gorilla upgrader: 4KB read / 8KB write bufs         │
     └──────────┬───────────────────────────────────────────┘
                │ WebSocket binary frames (sequenced)
                ▼
     ┌─────────────────────────────────────────────────────┐
     │ Browser                                             │
     │ ws.binaryType = 'arraybuffer'                       │
     │                                                     │
     │ Binary frame:                                       │
     │   parse 8-byte seq header (BigEndian uint64)        │
     │   TextDecoder.decode(payload, {stream: true})       │
     │   terminal.write(text, () => scrollToBottom())      │
     │                                                     │
     │ Gap detection:                                      │
     │   if seq > lastReceivedSeq + 1 → send gap request   │
     │   server replays missing events from OutputLog      │
     │                                                     │
     │ Text frame (JSON control messages):                 │
     │   sync, stats, diagnostic, controlMode, displaced   │
     └──────────┬──────────────────────────────────────────┘
                │
                ▼
     ┌─────────────────────────────────────────┐
     │ xterm.js Terminal                       │
     │  scrollback: 5000 lines                 │
     │  fontSize: 14, Menlo                    │
     │  convertEol: true                       │
     │  Unicode11Addon: DISABLED               │
     └─────────────────────────────────────────┘
```

---

## Layer 1: tmux Session

**File:** `internal/tmux/tmux.go`

Sessions are created with `tmux new-session -d -s <name> -c <dir> <command>` (detached mode). `history-limit` is set to 10,000 lines. `window-size manual` is set and the status bar is configured.

Key functions:

- `CreateSession()` — creates detached session, sets history-limit to 10000
- `ConfigureStatusBar()` — sets status bar format
- `GetWindowSize()` / `ResizeWindow()` — query/set terminal dimensions
- `CaptureLastLines()` — one-shot capture for bootstrap fallback (`-e -p -S -<N>`)
- `CaptureOutput()` — full scrollback capture for REST API

---

## Layer 2: SessionTracker (Control Mode)

**File:** `internal/session/tracker.go`

The `SessionTracker` maintains a long-lived control mode attachment to the tmux session via `tmux -C attach-session -t =<name>`. Control mode delivers `%output` events for every byte a pane produces — not screen snapshots like the old PTY attachment model.

### Control Mode Protocol

tmux control mode is a text-based protocol. Instead of rendering screen frames to a PTY, tmux sends structured events on stdout:

```
%output %0 \033[32mhello\033[0m\012       ← every byte the pane produces
%output %0 line 2\012                     ← escaped octal, one event per write
%begin 1363006971 2 1                     ← command response start
0: ksh* (1 panes) [80x24]                ← response content
%end 1363006971 2 1                       ← command response end
```

### Three-Layer Fan-Out Pipeline

```
tmux -C stdout
       │
       ▼
  ┌─────────┐    chan(1000)    ┌────────┐    chan(1000)
  │ Parser  │ ──────────────▶ │ Client │ ──────────────▶
  │ (octal  │  %output lines  │(per-pane│  per-subscriber
  │ unescape│  drop on full   │ fanout) │  drop on full
  └─────────┘                 └────────┘
                                   │
                              chan(1000)
                                   ▼
                            ┌──────────────────┐
                            │ Tracker           │
                            │ fanOut()          │  drop on full
                            │ + OutputLog       │  (sequenced append)
                            └──────────────────┘
```

Each layer uses non-blocking sends. Slow consumers get events dropped rather than blocking the pipeline. Drops are counted atomically at all three layers and reported in stats/diagnostics.

**Key property:** Tracker-level subscriptions survive control mode reconnections. If control mode drops and reconnects, the tracker re-subscribes to the new client internally, but WebSocket clients keep their tracker-level subscription.

### OutputLog (Sequenced Ring Buffer)

**File:** `internal/session/outputlog.go`

Every output event passing through `fanOut()` is assigned a monotonically increasing sequence number and appended to a bounded ring buffer (50,000 entries, ~5 MB). The log is the source of truth for:

- **Bootstrap replay** — new WebSocket clients receive a replay from the log rather than a `capture-pane` snapshot
- **Gap recovery** — when the frontend detects a sequence gap (dropped events), the server replays missing entries from the log

```go
type LogEntry struct {
    Seq  uint64
    Data []byte
}
```

The log supports `ReplayFrom(seq)` which returns all entries from seq onward, or nil if the requested data has been evicted from the ring buffer.

### Input and Resize

- `SendInput(data)` — sends keystrokes via control mode `send-keys` command
- `Resize(cols, rows)` — sends resize via control mode `resize-window` command

Both go through the control mode stdin pipe (memory write), not process spawning.

### Auto-Reconnect

If control mode fails, the tracker waits 500ms and retries. Permanent errors (session gone) cause exit. Retry errors are logged at most every 15 seconds.

---

## Layer 3: WebSocket Handler

**File:** `internal/dashboard/websocket.go`

### Bootstrap Phase

1. Wait up to 2 seconds for control mode to attach.
2. Start reading client messages; wait up to 100ms for a `resize` message so the pane dimensions are correct before capture.
3. **Replay from OutputLog** — chunked into ~16KB binary frames, each with an 8-byte sequence header. The escape holdback buffer (`escbuf.SplitClean`) ensures no partial ANSI sequences at frame boundaries.
4. **Fallback** — if the log is empty (session pre-dates this change), capture via `tracker.CaptureLastLines(5000)` or `tmux.CaptureLastLines(5000)`.
5. Restore cursor state — query cursor position and visibility via `tracker.GetCursorState()`, append CSI positioning + DECTCEM escape sequences.
6. Subscribe to output **after** replay (TOCTOU prevention — events arriving after subscribe are guaranteed not to be in the replay).

### Sequenced Binary Frame Protocol

Each binary WebSocket frame has an 8-byte header:

```
┌──────────────────┬──────────────────┐
│  seq (uint64 BE) │  terminal data   │
│    8 bytes       │   N bytes        │
└──────────────────┴──────────────────┘
```

For bootstrap frames, `seq` is reserved via `bootstrapFrameSeq()` which appends a zero-length entry to the OutputLog. This ensures the bootstrap frame's seq is strictly less than the first live event's seq, preventing the frontend's dedup logic from dropping the first keystroke echo. For live frames, `seq` is the sequence assigned during `fanOut()`.

### Steady-State Streaming

The main loop `select`s on:

- **`outputCh`**: Each event from the tracker goes through escape holdback, gets a sequence header, and is sent as a binary frame.
- **`sessionDead`**: Background goroutine polls `IsRunning()` every 500ms. Sends `[Session ended]` on death.
- **`controlChan`**: Client messages — `input`, `resize`, `gap`, `syncResult`, `diagnostic`.
- **`statsTickerC`**: In dev mode, every 3 seconds, sends pipeline stats as a JSON text frame.

### Gap Handling

When the frontend detects a sequence gap (received seq > expected seq), it sends a `{"type": "gap", "data": {"fromSeq": "N"}}` message. The server replays missing entries from the OutputLog as chunked binary frames.

### Sync (Defense-in-Depth) — Currently Disabled

> **Status**: The sync goroutine is currently disabled while investigating whether
> it introduces color artifacts (e.g., Claude Code's gray autocompletion text).
> Gap detection + OutputLog replay is the primary consistency mechanism.

When enabled, a background goroutine runs a periodic text comparison as a paranoia check:

1. **Fires every 60 seconds** (initial delay 5 seconds after bootstrap).
2. Captures visible screen via `tracker.CapturePane()` (no scrollback).
3. Captures cursor state.
4. If any output drops have occurred since the last sync, sets `forced: true` to bypass the frontend's activity guard.
5. Sends as a JSON `sync` text frame.

The frontend compares stripped-text line-by-line. On mismatch, it applies **surgical viewport correction** — overwriting only the differing rows using cursor-positioning escape sequences, without destroying scrollback. `terminal.reset()` is never called from the sync path.

### Write Safety

All WebSocket writes go through the `wsConn` wrapper which serializes writes with a mutex. Multiple goroutines write to the WebSocket (main loop, sync, stats, liveness, control mode health).

### Input Filtering

Terminal query responses from xterm.js (DA1, DA2, OSC 10/11) are silently dropped and not forwarded to tmux.

---

## Layer 4: Browser / xterm.js

**File:** `assets/dashboard/src/lib/terminalStream.ts`

### Terminal Configuration

```typescript
new Terminal({
  cols,
  rows, // dynamic, from container
  cursorBlink: true,
  fontSize: 14,
  fontFamily: 'Menlo, Monaco, "Courier New", monospace',
  scrollback: 5000, // 5000 lines of scrollback
  convertEol: true, // \n → \r\n
  macOptionIsMeta: true,
  allowProposedApi: true,
});
```

Addons loaded: `WebLinksAddon`. `Unicode11Addon` is **disabled** (commented out, testing performance impact).

### Output Handling

Binary frames carry terminal data with a sequence header. Text frames carry JSON control messages.

```typescript
handleOutput(data: string | ArrayBuffer) {
  if (data instanceof ArrayBuffer) {
    // Parse 8-byte sequence header
    const seq = new DataView(data).getBigUint64(0, false);

    // Gap detection
    if (this.bootstrapped && seq > this.lastReceivedSeq + 1n) {
      this.sendGapRequest(this.lastReceivedSeq + 1n);
    }
    this.lastReceivedSeq = seq;

    // Decode terminal data (after 8-byte header)
    const text = this.utf8Decoder.decode(new Uint8Array(data, 8), { stream: true });

    if (!this.bootstrapped) {
      this.bootstrapped = true;
      this.terminal.reset();
      this.terminal.write(text, () => {
        if (this.followTail) this.terminal.scrollToBottom();
      });
    } else {
      this.terminal.write(text, () => {
        if (this.followTail) this.terminal.scrollToBottom();
      });
    }
    return;
  }

  // Text frame: JSON control messages
  const msg = JSON.parse(data);
  switch (msg.type) {
    case 'sync':      // defense-in-depth sync check
    case 'stats':     // pipeline metrics (dev mode)
    case 'diagnostic': // diagnostic response (dev mode)
    case 'controlMode': // control mode attach/detach notification
    case 'displaced': // another tab took over (legacy)
    // ...
  }
}
```

Key design: `scrollToBottom()` is called inside `terminal.write()`'s completion callback, not synchronously after it. This ensures xterm.js has fully parsed the data before scrolling, eliminating the "scrolling through thousands of lines" artifact on bootstrap.

### Scroll Position Tracking

- `followTail` boolean controls auto-scroll (default: true)
- `isAtBottom()` checks `buffer.viewportY >= buffer.baseY - threshold`
- `handleUserScroll()` disables auto-follow when user scrolls up
- Resume button appears when `followTail` is false

### Resize Handling

- No FitAddon — custom measurement via xterm.js private API (`_core._renderService.dimensions.css.cell`)
- `ResizeObserver` + `window.resize` for detection
- Debounced at 300ms
- Sends `{"type": "resize", "data": "{cols, rows}"}` to backend
- Backend resizes tmux pane via control mode

### Reconnection

Exponential backoff: `delay = min(1000 * 2^attempt, 30000)`, max 10 attempts. Each reconnect resets `bootstrapped = false` so the next binary frame triggers a full bootstrap.

---

## Sync and Correction

### Sequence-Based Gap Detection (Primary)

The primary consistency mechanism. Each binary frame carries a monotonic sequence number. The frontend tracks `lastReceivedSeq`. If a frame arrives with seq > expected, a gap has been detected — events were dropped at some fan-out layer.

The frontend sends `{"type": "gap", "fromSeq": "N"}` to the server. The server replays missing entries from the OutputLog. The replayed data is **appended** to the terminal — no reset, no scrollback destruction.

If the OutputLog doesn't have the requested entries (they fell off the ring buffer), the server falls back to a capture-pane bootstrap.

### Surgical Viewport Correction (Fallback)

**File:** `assets/dashboard/src/lib/surgicalCorrection.ts`

When the defense-in-depth text comparison finds a mismatch, the correction overwrites only the differing rows using cursor-positioning escape sequences:

```
For each differing row R:
  \x1b7              Save cursor (DECSC)
  \x1b[R+1;1H       Move to row R, column 1
  \x1b[2K            Clear entire line
  \x1b[0m            Reset SGR attributes
  <row content>      Write correct ANSI content
  \x1b8              Restore cursor (DECRC)
```

**Scrollback is never destroyed.** `terminal.reset()` is only called during the initial bootstrap (to clear the "Connecting..." message), never from the sync/correction path.

### Defense-in-Depth Text Comparison

A background goroutine runs every 60 seconds (initial delay 5 seconds after bootstrap):

1. Captures visible tmux screen via `capture-pane`.
2. Frontend compares stripped-text line-by-line against xterm.js viewport.
3. Activity guard: skips if binary data arrived within 2 seconds (bypass if drops detected).
4. On mismatch: applies surgical correction.
5. Sends `syncResult` back to server for observability.

This is a paranoia safety net. The primary consistency mechanism is sequence-based gap detection.

### Bootstrap Race Condition

TUI applications like Claude Code perform multi-step redraws using relative cursor movements. If the bootstrap capture fires mid-redraw, xterm.js starts with a partial state. Subsequent relative cursor movements compound the error, producing permanent desync.

The OutputLog-based bootstrap mitigates this because it replays the exact byte stream (not a point-in-time screenshot), and the defense-in-depth sync provides a correction mechanism within 60 seconds.

---

## Diagnostics (Dev Mode Only)

**Files:** `internal/dashboard/websocket.go`, `assets/dashboard/src/lib/streamDiagnostics.ts`, `assets/dashboard/src/components/StreamMetricsPanel.tsx`

The entire diagnostics system is gated behind dev mode. Ring buffers are not allocated, stats are not sent, and the diagnostic button is not rendered in production. Zero overhead when disabled.

- **Backend:** `s.devMode` on the Server struct controls allocation and message handling.
- **Frontend:** `versionInfo?.dev_mode` (from `/api/healthz` via `useVersionInfo()`) controls rendering.

### Known Desync Root Causes

When the xterm.js terminal gets out of sync with tmux (garbled rendering, wrong colors, misaligned text), these are the known root cause candidates:

1. **Dropped `%output` events** — non-blocking sends on buffered channels (cap=1000) can silently drop events during rapid TUI redraws, leaving xterm.js in a diverged state.
2. **Split escape sequences** — ANSI sequences like `\033[38;2;128;128;128m` split across two `%output` lines or chunk boundaries. Mitigated by `escbuf.SplitClean()`.
3. **Bootstrap race** — `capture-pane` snapshots the screen while a TUI is actively redrawing. The snapshot captures a partial redraw, and queued live events assume a different starting state. Mitigated by OutputLog-based bootstrap.
4. **Input filtering false positives** — the WebSocket handler filters terminal query responses (DA1, DA2, OSC 10/11). If TUI output matches these patterns, it gets silently eaten.

### Always-On Metrics

In dev mode, pipeline health stats are sent every 3 seconds as `{"type": "stats"}` text frames:

| Metric                               | Source                                  | Cost           |
| ------------------------------------ | --------------------------------------- | -------------- |
| Events delivered/dropped             | Atomic counters at all 3 fan-out layers | ~1ns per event |
| Bytes delivered                      | Sum of frame sizes                      | ~1ns per event |
| Throughput (bytes/sec)               | Computed from sliding window            | timestamp + division |
| Control mode reconnects              | Tracker reconnect counter               | ~1ns per event |
| Sync checks sent/corrections/skipped | Per-connection counters                 | ~1ns per event |
| Current seq / log oldest seq         | OutputLog                               | read-only      |

Frontend tracks frames received, bytes, bootstrap count, and incomplete escape sequence detection (~1-5us per event for sequence break scanning).

Display: collapsible `StreamMetricsPanel` in the session detail page.

### Ring Buffers

Both backend (256KB `RingBuffer` in `websocket.go`) and frontend (256KB in `StreamDiagnostics`) maintain fixed-size circular buffers of recent raw bytes for diagnostic capture. Pre-allocated arrays with write cursors, zero GC pressure.

| Scenario | Throughput | Ring buffer covers |
|----------|-----------|-------------------|
| TUI app (normal) | ~50-100 KB/s | 2.5-5 seconds |
| Interactive typing | ~1-10 KB/s | 25-250 seconds |
| Build output (fast scroll) | ~1-10 MB/s | 25-250 ms |

The buffer is most useful during TUI usage — exactly the scenario where desyncs occur.

### On-Demand Diagnostic Capture

Triggered via dashboard button or keyboard shortcut. Captures data from all pipeline layers simultaneously:

1. **Freeze** — frontend snapshots the xterm.js screen buffer (every cell's character, colors, attributes) and freezes the ring buffer write cursor. Sends `{"type": "diagnostic"}` to the backend.
2. **Ground truth** — backend runs `capture-pane -e -p` via control mode, snapshots its ring buffer and counters, sends everything back as a JSON diagnostic response.
3. **Diff** — frontend parses tmux capture into the same cell-grid format and does cell-by-cell comparison.
4. **Automated checks** — decision tree: drop check -> diff check -> sequence break scan -> reconnect check -> fallback verdict.
5. **Write directory** — diagnostic data saved as plain files (not base64 JSON):

```
~/.schmux/diagnostics/<timestamp>-<sessionId>/
├── meta.json                # Counters, automated findings, verdict
├── ringbuffer-backend.txt   # Raw terminal data as sent (cat-able)
├── ringbuffer-frontend.txt  # Raw terminal data as received (cat-able)
├── screen-tmux.txt          # capture-pane output with ANSI escapes
├── screen-xterm.txt         # xterm.js buffer dump with ANSI escapes
└── screen-diff.txt          # Human-readable row-by-row diff
```

6. **Agent analysis** — a schmux agent session is automatically spawned to analyze the directory.
7. **Dashboard display** — visual screen diff (differing cells highlighted), counter stats, automated verdict, and link to the agent session.

### Performance Impact

| Component | Hot path cost | Memory |
|-----------|--------------|--------|
| Atomic counters | ~1ns per event | ~64 bytes |
| Backend ring buffer (256KB) | ~50-200ns per event (memcpy) | 256KB/session |
| Frontend ring buffer | ~1-5us per event | ~256KB/session |
| Screen diff (on-demand) | N/A (not on hot path) | Transient |
| **Total always-on overhead** | **~1-5us per event** | **~512KB/session** |

For context, `terminal.write()` alone takes 500-5000us for complex TUI content. The diagnostic overhead is 0.1-1% of the existing rendering cost.

---

## Escape Sequence Holdback

**File:** `internal/escbuf/escbuf.go`

`SplitClean(holdback, data)` prevents ANSI escape sequences from being split across WebSocket frame boundaries. It scans backward up to 16 bytes from the end looking for ESC (0x1b). If an incomplete CSI or OSC sequence is found, it holds back the trailing bytes for the next frame. This is a pure function — the caller owns the holdback state.

---

## Multi-Client Support

Multiple browser tabs can view the same session simultaneously. Each has its own WebSocket connection, subscriber channel, and independent state (scroll position, follow mode).

Registration uses `map[string][]*wsConn` — append on connect, remove on disconnect. No displacement — opening a second tab doesn't close the first.

---

## Configuration Reference

| Parameter                          | Value                   | Location                    |
| ---------------------------------- | ----------------------- | --------------------------- |
| tmux history-limit                 | 10000 lines             | `tmux.go` (`CreateSession`) |
| Control mode channel buffer        | 1000 events             | `parser.go`                 |
| Client fan-out channel buffer      | 1000 events             | `client.go`                 |
| Tracker fan-out channel buffer     | 1000 events             | `tracker.go`                |
| OutputLog capacity                 | 50000 entries           | `tracker.go`                |
| Bootstrap capture lines (fallback) | 5000 lines              | `websocket.go`              |
| Bootstrap chunk size               | 16384 bytes             | `websocket.go`              |
| Sequence header size               | 8 bytes (uint64 BE)     | `websocket.go`              |
| WS upgrader read buffer            | 4096 bytes              | `websocket.go`              |
| WS upgrader write buffer           | 8192 bytes              | `websocket.go`              |
| WS read limit                      | 65536 bytes             | `websocket_helpers.go`      |
| Escape holdback scan               | 16 bytes                | `escbuf.go`                 |
| Activity debounce                  | 500ms                   | `tracker.go`                |
| Tracker restart delay              | 500ms                   | `tracker.go`                |
| Retry log interval                 | 15s                     | `tracker.go`                |
| Session-dead poll interval         | 500ms                   | `websocket.go`              |
| Control mode health poll           | 1s                      | `websocket.go`              |
| Sync interval (defense-in-depth)   | 60s                     | `websocket.go`              |
| Sync initial delay                 | 5s                      | `websocket.go`              |
| Sync activity guard                | 2000ms                  | `terminalStream.ts`         |
| Stats ticker (dev mode)            | 3s                      | `websocket.go`              |
| Ring buffer (dev mode)             | 256KB                   | `websocket.go`              |
| xterm.js scrollback                | 5000 lines              | `terminalStream.ts`         |
| Resize debounce                    | 300ms                   | `terminalStream.ts`         |
| WS reconnect max attempts          | 10                      | `terminalStream.ts`         |
| WS reconnect backoff               | 1s × 2^attempt, max 30s | `terminalStream.ts`         |

---

## Performance Monitoring

**File:** `assets/dashboard/src/lib/inputLatency.ts`

Tracks WebSocket round-trip latency (input sent → output received) and render time (time in `terminal.write()`). Keeps last 1000 samples with median/p95/p99/max/avg statistics. Exposed as `window.__inputLatency` for Playwright benchmarks.

Benchmark tests:

- `internal/session/tracker_bench_test.go` — control mode echo latency (idle and flood)
- `internal/dashboard/websocket_bench_test.go` — full WebSocket round-trip latency

---

## Historical Context

### Previous Architecture (Superseded)

Before the control mode migration, the `SessionTracker` streamed output by running `tmux attach-session` inside a PTY (using `creack/pty`). tmux treated this attached PTY as a display client, sending rendered screen frames rather than raw content. During rapid output, lines that scrolled past between screen renders were never transmitted — they existed in tmux's scrollback but were structurally absent from the PTY output.

The WebSocket transport used JSON text frames (`{"type": "full", "content": "..."}` and `{"type": "append", "content": "..."}`). A `filterMouseMode()` function stripped mouse tracking and alternate screen sequences that tmux injected for its attached clients.

Three optimization attempts were reverted in `7ef6b0c3` (sendCoalesced backpressure, requestAnimationFrame batching, scrollback sync via capture-pane) because an escape sequence rewrite (`\x1b[2J → \x1b[999S`) caused a rendering glitch.

The control mode migration eliminated the root cause (screen snapshots vs raw bytes) rather than working around it.

### Previous Sync Architecture (Superseded)

The original sync mechanism used `terminal.reset()` + `terminal.write(screen)` to correct any mismatch between tmux and xterm.js. This was destructive — it destroyed all scrollback every time a correction fired. Combined with a 10-second interval, an aggressive 500ms initial delay, and false positives from timing races, users experienced frequent scrollback loss and multi-second rendering delays during bootstrap.

The current architecture replaces this with sequence-based gap detection (for actual data loss) and surgical viewport correction (for the rare cases that slip through). `terminal.reset()` is never called from the sync path.
