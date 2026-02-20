# Terminal Pipeline: Agent → tmux → WebSocket → xterm.js

End-to-end analysis of how terminal output flows from AI agents to the browser, including known bottlenecks and rendering issues.

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
│  tmux session        │  history-limit: 2000 (tmux default, NOT configured)
│  (detached)          │  window-size: manual
└──────────┬───────────┘
           │
     ┌─────┴──────────────────────────────────┐
     │ SessionTracker.attachAndRead()         │
     │ `tmux attach-session -t =<name>`       │
     │ wrapped in PTY via creack/pty          │
     │                                        │
     │  Read loop: 8KB buffer                 │
     │  UTF-8 boundary handling               │
     │  Activity debounce: 500ms              │
     │                                        │
     │  chan []byte (buffer: 64)  ◄── BOTTLENECK
     │  Non-blocking send, drop on full       │
     └──────────┬─────────────────────────────┘
                │
                ▼
     ┌───────────────────────────────────────────┐
     │ handleTerminalWebSocket()                 │
     │                                           │
     │  Bootstrap: capture-pane -S -1000         │
     │    → JSON {"type":"full","content":"…"}   │
     │                                           │
     │  Steady-state: each chunk from channel    │
     │    → filterMouseMode()                    │
     │    → JSON {"type":"append","content":"…"} │
     │                                           │
     │  wsConn mutex serializes writes           │
     │  gorilla upgrader: 4KB read/write bufs    │
     └──────────┬────────────────────────────────┘
                │ WebSocket text frames (JSON)
                ▼
     ┌─────────────────────────────────────────┐
     │ Browser                                 │
     │ ws.onmessage → handleOutput()           │
     │                                         │
     │  "full":   terminal.reset() + write()   │
     │  "append": terminal.write()             │
     │                                         │
     │  if (followTail) scrollToBottom()       │
     └──────────┬──────────────────────────────┘
                │
                ▼
     ┌─────────────────────────────────────────┐
     │ xterm.js Terminal                       │
     │  scrollback: 1000 lines                 │
     │  fontSize: 14, Menlo                    │
     │  convertEol: true                       │
     │  Unicode11Addon: DISABLED               │
     └─────────────────────────────────────────┘
```

---

## Layer 1: tmux Session

**File:** `internal/tmux/tmux.go`

Sessions are created with `tmux new-session -d -s <name> -c <dir> <command>` (detached mode). No `history-limit` is set — tmux uses its default (typically 2000 lines unless overridden in the user's `.tmux.conf`). After creation, `window-size manual` is set and the status bar is configured.

Key functions:

- `CreateSession()` — creates detached session
- `ConfigureStatusBar()` — sets status bar format
- `GetWindowSize()` / `ResizeWindow()` — query/set terminal dimensions
- `CaptureLastLines()` — one-shot capture for bootstrap (`-e -p -S -<N>`)
- `CaptureOutput()` — full scrollback capture for REST API

---

## Layer 2: SessionTracker (PTY Attachment)

**File:** `internal/session/tracker.go`

The `SessionTracker` maintains a long-lived PTY attachment to the tmux session via `tmux attach-session -t =<name>`, started as a child process wrapped in a pseudo-terminal (using `creack/pty`). This provides real-time streaming rather than periodic polling.

### Read Loop

```go
buf := make([]byte, 8192)          // 8KB read buffer
var pending []byte                  // incomplete UTF-8 sequence

for {
    n, err := ptmx.Read(buf)        // read from PTY
    // ... UTF-8 boundary handling ...
    // ... activity tracking (500ms debounce) ...

    if clientCh != nil {
        select {
        case clientCh <- chunk:      // try to send
        default:                     // channel full → SILENTLY DROP
        }
    }
}
```

### Channel Buffer

The WebSocket client channel is `make(chan []byte, 64)`. At 8KB per slot, this holds up to 512KB in flight. When the channel is full, chunks are **silently dropped** via the non-blocking `select/default`.

### UTF-8 Safety

`findValidUTF8Boundary()` scans backward from the end of each read to find the last complete UTF-8 character. Incomplete trailing bytes are held in `pending` and prepended to the next read. This prevents sending mid-character byte sequences over the WebSocket.

### Auto-Reconnect

If the PTY read fails (tmux session temporarily unavailable), the tracker waits 500ms (`trackerRestartDelay`) and retries. Retry errors are logged at most every 15 seconds.

### Single-Client Model

Only one WebSocket connection per session. `AttachWebSocket()` returns a new `chan []byte` and closes any previous channel. Opening a second browser tab displaces the first (which receives a `"displaced"` message).

---

## Layer 3: WebSocket Handler

**File:** `internal/dashboard/websocket.go`

### Bootstrap Phase

1. Attach the output channel **before** capturing bootstrap (avoids dropping output during setup).
2. Wait up to 2 seconds for the tracker to attach its PTY.
3. `CaptureLastLines(1000)` — captures the last 1000 lines from tmux, including ANSI escapes.
4. Filter mouse mode sequences from the bootstrap.
5. Send as `{"type":"full","content":"…"}`.
6. Drain any output that accumulated during bootstrap setup.

### Steady-State Streaming

The main loop `select`s on three channels:

- **`outputCh`**: Each chunk from the tracker is filtered through `filterMouseMode()` and sent as `{"type":"append","content":"…"}`. No batching — every chunk becomes one WebSocket message.
- **`sessionDead`**: Background goroutine polls `IsRunning()` every 500ms. Sends `"[Session ended]"` on death.
- **`controlChan`**: Client input (`"input"`, `"resize"` messages).

### ANSI Sequence Filtering

`filterMouseMode()` strips sequences that interfere with xterm.js scrollback:

| Sequence      | Purpose               | Why filtered                    |
| ------------- | --------------------- | ------------------------------- |
| `\x1b[?1000h` | X11 mouse tracking    | Would capture scroll events     |
| `\x1b[?1002h` | Button event tracking | Same                            |
| `\x1b[?1003h` | Any event tracking    | Same                            |
| `\x1b[?1006h` | SGR extended mouse    | Same                            |
| `\x1b[?1015h` | urxvt mouse mode      | Same                            |
| `\x1b[?1049h` | Alternate screen      | Disables scrollback in xterm.js |

### Input Filtering

Terminal query responses from xterm.js (DA1, DA2, OSC 10/11) are silently dropped and not forwarded to tmux.

### Write Safety

All WebSocket writes go through the `wsConn` wrapper which serializes writes with a mutex (gorilla/websocket is not concurrent-write-safe).

### Message Framing

All terminal output is sent as JSON text frames. No binary mode, no chunking, no compression.

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
  scrollback: 1000, // 1000 lines of scrollback
  convertEol: true, // \n → \r\n
  allowProposedApi: true, // for registerDecoration
});
```

Addons loaded: `WebLinksAddon`. `Unicode11Addon` is **disabled** (commented out, testing performance impact).

### Output Handling

```typescript
handleOutput(data: string) {
    let msg = JSON.parse(data);
    switch (msg.type) {
        case 'append':
            this.terminal.write(msg.content);     // one write per message
            break;
        case 'full':
            this.terminal.reset();
            this.terminal.write(msg.content);
            break;
        case 'displaced':
            // show displacement message
            break;
    }
    if (this.followTail) {
        this.terminal.scrollToBottom();
    }
}
```

Every `append` message triggers a separate `terminal.write()` call. There is no batching.

### Scroll Position Tracking

- `isAtBottom()` checks `buffer.viewportY >= buffer.baseY - threshold`
- `handleUserScroll()` disables auto-follow when user scrolls up
- `jumpToBottom()` re-enables follow and scrolls to bottom

### Resize Handling

- Debounced at 150ms on the client side
- Server-side duplicate detection (queries tmux, skips if unchanged)
- Resizes both tmux window and tracker PTY

### Reconnection

Exponential backoff: `delay = min(1000 * 2^attempt, 30000)`, max 10 attempts.

---

## Known Issues: Scrollback Gaps During High-Throughput Output

When agents produce output faster than the pipeline can deliver it, users see gaps when scrolling back — lines are missing from the scrollback buffer.

### Root Cause 1: Silent Channel Drops

The 64-slot Go channel drops chunks silently when full:

```
Time ──────────────────────────────────────────────────►

Agent output (fast):
  ████████████████████████████████████████████████████

PTY reads (~8KB each):
  │1│2│3│4│5│...│64│65│66│67│68│...│200│
                      ▲   ▲   ▲
                      DROPPED (channel full)

Channel (64 slots):
  [1][2][3]...[64]  ← FULL, slots free only when WS write completes

WS writes (serial, mutex):
  [1]---[2]---[3]---  ← each blocked on browser TCP ACK

Browser xterm.js writes:
  [1]━━━━━[2]━━━━━  ← each takes 5-10ms to render
```

The downstream pipeline is slower at every step:

| Step                        | Latency                      |
| --------------------------- | ---------------------------- |
| PTY read                    | ~microseconds per 8KB        |
| JSON encode                 | ~microseconds                |
| WebSocket write             | ~100μs-1ms (mutex + network) |
| Browser JSON.parse          | ~100μs                       |
| xterm.js `terminal.write()` | ~1-10ms (DOM rendering)      |

xterm.js rendering is the real bottleneck. During heavy output it can take 5-10ms per write. The browser's main thread saturates, `onmessage` events back up, Go's `WriteMessage` blocks on TCP backpressure, and the channel fills.

### Root Cause 2: tmux PTY Attachment Model

tmux treats attached clients as displays, sending **rendered screen snapshots** rather than raw content streams:

```
Agent prints 500 lines in 100ms:

tmux renders screen updates every ~16ms:
  → ~6 screen snapshots sent over PTY
  → NOT 500 individual lines

Lines that scrolled past between snapshots exist in tmux's
internal scrollback but are NEVER sent over the attached PTY.
```

This is a fundamental property of the PTY-attach model. It's correct for interactive terminal use, but means scrollback fidelity is inherently limited during fast output bursts.

### Root Cause 3: Scrollback Buffer Mismatch

The three relevant buffers have different sizes:

| Buffer                   | Size       | Notes                             |
| ------------------------ | ---------- | --------------------------------- |
| tmux `history-limit`     | 2000 lines | Default, not configured by schmux |
| Bootstrap `capture-pane` | 1000 lines | `bootstrapCaptureLines` constant  |
| xterm.js `scrollback`    | 1000 lines | Terminal config                   |

Even with perfect delivery, scrolling back more than ~1000 lines is impossible. On reconnect, only the last 1000 lines of tmux's 2000-line buffer are sent.

### What the Gap Looks Like

```
┌─────────────────────────────────────┐
│ Line 1000: "Installing deps..."     │
│ Line 1001: "npm install react"      │
│ Line 1002: "npm install lodash"     │
│                                     │  ← GAP: lines were in dropped
│ Line 1003: "Building bundle..."     │     chunks, never delivered
│ (was actually line ~1848 in agent)  │
│ Line 1004: "✓ Build complete"       │
└─────────────────────────────────────┘
```

### Where Data Lives After a Burst

```
Agent produced 5000 lines
           │
           ▼
tmux scrollback: last 2000 lines (3001-5000)
  Lines 1-3000 evicted from tmux buffer
  Only ~last screen sent over PTY during burst
           │
           ▼
Go channel: some chunks dropped during burst
           │
           ▼
xterm.js: last 1000 lines of DELIVERED content
  Gaps where chunks were dropped
  Missing lines from tmux screen-snapshot behavior
```

---

## Previous Fix Attempts (Reverted)

Three optimization commits were attempted and reverted in `7ef6b0c3` because they introduced a rendering glitch (terminal scrolling up one line periodically during typing):

### 1. `sendCoalesced` backpressure (`8bed0d39`)

Instead of dropping chunks, buffered them in a `coalesce []byte` and merged with the next send. Blocking send when coalesced data exceeded 1MB. Also raised xterm.js scrollback to 5000.

**Problem:** Doesn't fix the PTY attachment model issue — tmux still sends screen snapshots, not raw lines. Coalescing delays drops but doesn't prevent them under sustained load.

### 2. `requestAnimationFrame` write batching (`894eae57`)

Accumulated append data in a `pendingAppend` string on the browser side, flushed once per animation frame via `requestAnimationFrame`. Reduced xterm.js write operations from hundreds/sec to ≤60/sec.

**Assessment:** Correct direction for reducing browser-side pressure. Did not itself cause the rendering glitch, but was reverted along with the other changes.

### 3. Scrollback sync via `capture-pane` (`d6812459`)

After a burst (>4KB output, 300ms quiet period), re-captured tmux scrollback and sent as a `"full"` message to resync the client. Also replaced `\x1b[2J` (Erase Display) with `\x1b[999S` (Scroll Up 999) to push erased content into scrollback, and filtered `\x1b[3J` (Erase Scrollback).

**Confirmed bug:** The `\x1b[2J → \x1b[999S` replacement caused the rendering glitch. The scrollback sync concept is sound but the escape sequence rewriting was wrong.

---

## Bottleneck Summary

| Layer                | Bottleneck                                           | Severity |
| -------------------- | ---------------------------------------------------- | -------- |
| tmux PTY attachment  | Screen snapshots during fast output, not raw content | HIGH     |
| Go channel (tracker) | 64-slot buffer, silent drop, no backpressure         | HIGH     |
| xterm.js rendering   | `terminal.write()` per message, no batching          | HIGH     |
| WebSocket framing    | JSON text frames, no binary mode or compression      | LOW      |
| WebSocket writes     | Mutex-serialized, blocked on browser TCP ACK         | MED      |
| Scrollback mismatch  | tmux 2000, bootstrap 1000, xterm.js 1000             | LOW      |

---

## Configuration Reference

| Parameter                      | Value                   | Location                |
| ------------------------------ | ----------------------- | ----------------------- |
| PTY read buffer                | 8192 bytes              | `tracker.go:237`        |
| Channel buffer size            | 64                      | `tracker.go:118`        |
| Activity debounce              | 500ms                   | `tracker.go:23`         |
| Tracker restart delay          | 500ms                   | `tracker.go:22`         |
| Retry log interval             | 15s                     | `tracker.go:24`         |
| Bootstrap capture lines        | 1000                    | `websocket.go:20`       |
| WS upgrader read/write buffers | 4096 bytes              | `websocket.go:136-137`  |
| Session-dead poll interval     | 500ms                   | `websocket.go:236`      |
| xterm.js scrollback            | 1000 lines              | `terminalStream.ts:146` |
| Resize debounce                | 150ms                   | `terminalStream.ts:213` |
| WS reconnect max attempts      | 10                      | `terminalStream.ts:44`  |
| WS reconnect backoff           | 1s × 2^attempt, max 30s | `terminalStream.ts:352` |

---

## Performance Monitoring

**File:** `assets/dashboard/src/lib/inputLatency.ts`

Tracks WebSocket round-trip latency (input sent → output received) and render time (time in `terminal.write()`). Keeps last 1000 samples with median/p95/p99/max/avg statistics. Exposed as `window.__inputLatency` for Playwright benchmarks.

Benchmark tests:

- `internal/session/tracker_bench_test.go` — PTY-level echo latency (idle and flood)
- `internal/dashboard/websocket_bench_test.go` — full WebSocket round-trip latency
