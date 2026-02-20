# Control Mode Terminal Streaming

Migrate local session terminal streaming from PTY attachment to tmux control mode, switch the WebSocket transport from JSON text frames to raw binary frames, and support multiple concurrent viewers.

**Status**: Proposed
**Supersedes**: `docs/specs/scrollback-integrity.md` (option analysis), `docs/specs/terminal-hybrid-streaming.md` (historical)

## Problem Statement

When an AI agent produces output at high throughput, users see **gaps in scrollback** — missing lines when they scroll up in the xterm.js terminal. This was confirmed empirically: outputting 100 numbered lines resulted in lines 1-58 being absent from scrollback, while the tracker's drop-count logging showed zero channel drops.

The root cause is the PTY attachment model. The `SessionTracker` streams output by running `tmux attach-session` inside a PTY. tmux treats this attached PTY as a display client — it sends rendered screen frames, not a raw byte stream. During rapid output, lines that scroll past between screen renders are never transmitted. They exist in tmux's internal scrollback but are structurally absent from the PTY output.

This is not a bug in the tracker, the channel, or the WebSocket layer. It is a fundamental property of how tmux renders to attached clients.

## Solution

Replace the PTY attachment with **tmux control mode** (`tmux -CC attach-session`). Control mode delivers `%output` events for every byte a pane produces — not screen snapshots. This infrastructure already exists and is battle-tested in the remote session path (`internal/remote/controlmode/`).

Simultaneously, replace JSON text-frame WebSocket transport with raw binary frames, and support multiple concurrent WebSocket clients per session.

## Architecture

```
Agent process
    │
    ▼
tmux session (history-limit: 10000)
    │
    ├── control mode client (tmux -CC attach-session)
    │     │
    │     ├── %output events → Parser → Client.processOutput()
    │     │     │
    │     │     ├──→ subscriber chan (client A) ──→ binary WS ──→ xterm.js
    │     │     ├──→ subscriber chan (client B) ──→ binary WS ──→ xterm.js
    │     │     └──→ outputCallback (preview autodetect)
    │     │
    │     ├── capture-pane (bootstrap on each WS connect, 5000 lines)
    │     ├── send-keys (input from operator client)
    │     └── resize-window (resize from operator client)
    │
    └── (normal tmux attach still works for human debugging)
```

### How Control Mode Works

tmux control mode is a text-based protocol for programmatic interaction. Instead of rendering screen frames to a PTY, tmux sends structured events on stdout:

```
%output %0 \033[32mhello\033[0m\012       ← every byte the pane produces
%output %0 line 2\012                     ← escaped octal, one event per write
%begin 1363006971 2 1                     ← command response start
0: ksh* (1 panes) [80x24]                ← response content
%end 1363006971 2 1                       ← command response end
```

The Go server parses these events. The browser never sees the control mode protocol — it receives the same raw ANSI terminal data it gets today, just delivered more completely.

```
tmux control mode stdout:
  %output %0 \033[32mhello\033[0m\012

controlmode.Parser.parseLine():
  → matches %output regex
  → extracts pane ID: "%0"
  → UnescapeOutput(): \033 → ESC byte, \012 → newline
  → emits OutputEvent{PaneID: "%0", Data: "\x1b[32mhello\x1b[0m\n"}

WebSocket handler:
  → receives OutputEvent from subscriber channel
  → conn.WriteMessage(websocket.BinaryMessage, []byte(event.Data))

Browser:
  → ws.onmessage receives ArrayBuffer
  → new TextDecoder().decode(event.data)
  → terminal.write(data)
```

### Why Not Keep the PTY?

The PTY attachment model is inherently lossy during high-throughput output:

```
Agent writes 100 lines in ~10ms

tmux internal behavior:
  - Processes all 100 lines into its internal scrollback buffer
  - Renders screen updates to attached clients every ~16ms
  - Only the FINAL visible screen state is sent over the PTY
  - Lines 1-58 scrolled past between renders → never transmitted

Control mode behavior:
  - Every byte produces a %output event
  - All 100 lines delivered, in order, no gaps
```

Three previous fix attempts using the PTY model were reverted (`7ef6b0c3`):

- `sendCoalesced` backpressure — addressed channel drops, not the actual cause
- `requestAnimationFrame` batching — correct but reverted as collateral
- Scrollback sync via `capture-pane` — concept was sound, but `\x1b[2J → \x1b[999S` escape rewriting caused a rendering glitch

Control mode eliminates the root cause rather than working around it.

## Detailed Design

### SessionTracker Refactor

The current tracker does everything through one PTY: reads output, writes input, handles resize. The new version uses control mode for all three.

**Current struct (simplified):**

```go
type SessionTracker struct {
    clientCh  chan []byte        // single subscriber
    ptmx      *os.File          // PTY for everything
    attachCmd *exec.Cmd         // tmux attach-session process
}
```

**New struct:**

```go
type SessionTracker struct {
    // Control mode (output + input + commands)
    cmClient  *controlmode.Client
    cmParser  *controlmode.Parser
    cmCmd     *exec.Cmd          // tmux -CC attach-session process
    cmStdin   io.WriteCloser     // control mode command pipe
    paneID    string             // tmux pane ID (e.g., "%0")

    // No clientCh — subscribers register with cmClient directly
    // No ptmx — input goes through control mode send-keys
}
```

**What gets removed:**

- 8KB PTY read buffer and read loop
- UTF-8 boundary handling (`findValidUTF8Boundary`, `pending []byte`)
- `AttachWebSocket()` / `DetachWebSocket()` (replaced by `cmClient.SubscribeOutput()`)
- `ptmx.Write()` for input (replaced by `cmClient.SendKeys()`)
- `pty.Setsize()` for resize (replaced by `cmClient.ResizeWindow()`)
- `filterMouseMode()` — no longer needed (control mode output is raw pane data, not tmux client rendering commands)
- Single-client displacement logic

**`attachAndRead()` becomes `attachControlMode()`:**

```go
func (t *SessionTracker) attachControlMode() error {
    // 1. Start: tmux -CC attach-session -t =<name>
    //    Get stdin pipe (for commands) and stdout pipe (for parsing)
    // 2. Create Parser from stdout
    // 3. Create Client from stdin + parser
    // 4. Client.Start() — begins processing %output, responses, events
    // 5. Discover pane ID via: list-panes -F '#{pane_id}'
    // 6. Block until stopCh or control mode EOF
}
```

Auto-reconnect on control mode disconnect uses the same retry logic as today (500ms delay, 15s log throttle).

**`SendInput()` becomes:**

```go
func (t *SessionTracker) SendInput(data string) error {
    t.mu.RLock()
    client := t.cmClient
    paneID := t.paneID
    t.mu.RUnlock()
    if client == nil {
        return fmt.Errorf("not attached")
    }
    ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
    defer cancel()
    return client.SendKeys(ctx, paneID, data)
}
```

Input goes through the control mode connection as `send-keys` commands. No process spawning — `SendKeys()` writes to the control mode stdin pipe (`fmt.Fprintf(c.stdin, "%s\n", cmd)`), which is a memory write to a pipe. tmux processes it internally via Unix socket. The existing `SendKeys()` implementation handles escape sequences, special keys (Enter, Tab, arrows, Ctrl combinations), and printable text runs.

**`Resize()` becomes:**

```go
func (t *SessionTracker) Resize(cols, rows int) error {
    t.mu.RLock()
    client := t.cmClient
    t.mu.RUnlock()
    if client == nil {
        return fmt.Errorf("not attached")
    }
    ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
    defer cancel()
    return client.ResizeWindow(ctx, t.paneID, cols, rows)
}
```

### Multi-Client Fan-Out

The control mode client already implements fan-out for remote sessions. Each call to `SubscribeOutput(paneID)` returns a new buffered channel (buffer size 100). The `processOutput()` goroutine dispatches each `%output` event to all subscribers for that pane:

```go
// Already exists in controlmode/client.go
func (c *Client) processOutput() {
    for {
        select {
        case event := <-c.parser.Output():
            c.outputSubsMu.RLock()
            subs := c.outputSubs[event.PaneID]
            for _, ch := range subs {
                select {
                case ch <- event:
                default:
                    // Drop if subscriber can't keep up
                }
            }
            c.outputSubsMu.RUnlock()
        case <-c.closeCh:
            return
        }
    }
}
```

Each WebSocket client independently subscribes and unsubscribes. No single-client enforcement, no displacement.

**Client roles:**

- **Operator** — can send input and resize. First client to connect, or explicitly claimed.
- **Viewer** — receives output only, read-only. Their terminal size does not affect tmux.

Each client has its own xterm.js instance with independent scrollback and scroll position. The server sends the same bytes to all clients. Scroll position is purely client-side state — a viewer can scroll up while the operator watches live output.

### WebSocket Protocol

**Server → Client: Binary frames (raw terminal data)**

No JSON wrapping. Each `%output` event's unescaped data is sent directly as a `websocket.BinaryMessage`. The browser receives `ArrayBuffer`, decodes to string via `TextDecoder`, and calls `terminal.write()`.

Bootstrap (the initial `capture-pane` snapshot) is also sent as binary — it's the first data after the connection opens. The client clears the terminal in `ws.onopen` before any `onmessage` fires, so no message type distinction is needed.

**Client → Server: JSON text frames (input + resize)**

Input and resize messages remain as JSON text frames. They're small, infrequent (human typing speed), and the server needs to distinguish message types. The overhead is irrelevant.

```json
{"type":"input","data":"ls\r"}
{"type":"resize","data":"{\"cols\":120,\"rows\":40}"}
```

**Connection lifecycle via close codes:**

| Code       | Meaning              | Client behavior                      |
| ---------- | -------------------- | ------------------------------------ |
| 1000       | Session ended        | Show `[Session ended]`, no reconnect |
| 1001       | Server shutting down | Reconnect with backoff               |
| (abnormal) | Connection lost      | Reconnect with backoff               |

No in-band control messages. The `"displaced"` message type is eliminated (multi-client support means no displacement). The `"full"` and `"append"` types are eliminated (all output is raw binary, no type distinction needed).

### Browser-Side Changes

`terminalStream.ts` simplifies:

```typescript
// Connection setup
this.ws = new WebSocket(wsUrl);
this.ws.binaryType = 'arraybuffer';

this.ws.onopen = () => {
  this.connected = true;
  this.reconnectAttempt = 0;
  this.terminal.reset();
  this.onStatusChange('connected');
  if (this.tmuxCols && this.tmuxRows) {
    this.sendResize(this.tmuxCols, this.tmuxRows);
  }
};

this.ws.onmessage = (event) => {
  const data = new TextDecoder().decode(event.data);
  this.terminal.write(data);
  if (this.followTail) {
    this.terminal.scrollToBottom();
  }
};

this.ws.onclose = (event) => {
  this.connected = false;
  if (event.code === 1000) {
    this.terminal.writeln('\r\n[Session ended]');
    this.onStatusChange('disconnected');
    return;
  }
  // ... existing reconnect logic with exponential backoff ...
};
```

**What gets removed:**

- `JSON.parse()` in `onmessage`
- `handleOutput()` method with the `switch` on message type
- `wasDisplaced` flag and displacement handling
- The `TerminalOutputMessage` type

**What stays the same:**

- `sendInput()` and `sendResize()` — still JSON text frames client→server
- Scroll position tracking (`isAtBottom`, `handleUserScroll`, `followTail`)
- Resize debouncing (150ms)
- Reconnection with exponential backoff
- `inputLatency` tracking (measure from send to next `onmessage`)
- Selection mode, download, all other xterm.js features

### Scrollback Configuration

Applied alongside the migration:

| Setting                 | Current        | New   | Location               |
| ----------------------- | -------------- | ----- | ---------------------- |
| tmux `history-limit`    | 2000 (default) | 10000 | `tmux.CreateSession()` |
| `bootstrapCaptureLines` | 1000           | 5000  | `websocket.go`         |
| xterm.js `scrollback`   | 1000           | 5000  | `terminalStream.ts`    |

On connect, the client receives up to 5000 lines of history via `capture-pane`. Since control mode delivers every byte going forward, the live session accumulates complete, gap-free scrollback in xterm.js up to the 5000-line limit.

### Server Registration Changes

`server.go` WebSocket registration changes from single-client to multi-client:

**Current:**

```go
func (s *Server) RegisterWebSocket(sessionID string, conn *wsConn) {
    s.wsMu.Lock()
    defer s.wsMu.Unlock()
    if existing, ok := s.wsConns[sessionID]; ok {
        // Displace existing connection
        existing.WriteJSON(WSOutputMessage{Type: "displaced", Content: "..."})
        existing.Close()
    }
    s.wsConns[sessionID] = conn
}
```

**New:**

```go
func (s *Server) RegisterWebSocket(sessionID string, conn *wsConn) {
    s.wsMu.Lock()
    defer s.wsMu.Unlock()
    s.wsConns[sessionID] = append(s.wsConns[sessionID], conn)
}

func (s *Server) UnregisterWebSocket(sessionID string, conn *wsConn) {
    s.wsMu.Lock()
    defer s.wsMu.Unlock()
    conns := s.wsConns[sessionID]
    for i, c := range conns {
        if c == conn {
            s.wsConns[sessionID] = append(conns[:i], conns[i+1:]...)
            break
        }
    }
}
```

The `wsConns` map type changes from `map[string]*wsConn` to `map[string][]*wsConn`.

## Backpressure and Data Integrity

The fan-out uses non-blocking sends with per-subscriber channel buffers of 100. A slow WebSocket client can still lose chunks if its channel fills. This is acceptable because:

1. The primary source of data loss (tmux screen snapshots) is eliminated
2. Channel overflow requires sustained throughput exceeding what the WebSocket can drain — an extreme edge case for local connections
3. The drop logging added to the tracker carries over to monitor this

If profiling later shows channel overflow is a problem, options include:

- Larger channel buffers (1000, 10000)
- Ring buffer replacing channels (readers always get latest N bytes)
- Capture-pane resync as a safety net after bursts

## ANSI Filtering

**`filterMouseMode()` is removed.** The current code strips mouse tracking and alternate screen sequences because the PTY attachment receives tmux client rendering commands (tmux enables mouse mode on its attached clients). Control mode `%output` events deliver raw pane output — what the agent's process actually wrote to stdout. If a program inside the session enables mouse mode, that sequence should reach xterm.js.

**Input filtering stays.** Terminal query responses (DA1, DA2, OSC 10/11) from xterm.js are still filtered before forwarding to `SendKeys()`. These are responses to queries that tmux sends during initialization.

## Migration Strategy

The control mode infrastructure already exists and is tested in the remote session path. The migration reuses `controlmode.Parser`, `controlmode.Client`, `SubscribeOutput()`, `UnsubscribeOutput()`, `SendKeys()`, `ResizeWindow()`, and `CapturePaneLines()`.

### Implementation Order

1. **Scrollback configuration** — increase `history-limit`, bootstrap capture, xterm.js scrollback. Zero-risk, immediate benefit on reconnect.
2. **SessionTracker refactor** — replace PTY attachment with control mode. This is the core change.
3. **WebSocket binary frames** — switch server→client from JSON text to binary. Update `terminalStream.ts`.
4. **Multi-client support** — change registration from single to multi, remove displacement logic.
5. **Cleanup** — remove `filterMouseMode()`, `AttachWebSocket()`/`DetachWebSocket()`, UTF-8 boundary handling, `handleOutput()` switch statement.

Each step is independently testable and deployable.

### Files Modified

| File                                         | Changes                                                                     |
| -------------------------------------------- | --------------------------------------------------------------------------- |
| `internal/session/tracker.go`                | Replace PTY with control mode, remove read loop, UTF-8 handling             |
| `internal/dashboard/websocket.go`            | Binary frames, remove JSON marshaling, remove filterMouseMode, multi-client |
| `internal/dashboard/server.go`               | Multi-client WebSocket registration                                         |
| `assets/dashboard/src/lib/terminalStream.ts` | Binary WebSocket, remove JSON parsing, remove displacement handling         |
| `internal/tmux/tmux.go`                      | Set `history-limit` on session creation                                     |

### Files Unchanged

| File                                       | Why                                 |
| ------------------------------------------ | ----------------------------------- |
| `internal/remote/controlmode/client.go`    | Reused as-is                        |
| `internal/remote/controlmode/parser.go`    | Reused as-is                        |
| `assets/dashboard/src/lib/inputLatency.ts` | Still measures send→receive latency |

## Risks

| Risk                                             | Severity | Mitigation                                                                                                                                                                                       |
| ------------------------------------------------ | -------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| Control mode `SendKeys` latency vs raw PTY write | Medium   | Each `Execute()` round-trips through Unix socket — sub-millisecond. Measure with existing `inputLatency` benchmarks. Fire-and-forget optimization available if needed.                           |
| Control mode output line buffering               | Low      | tmux flushes `%output` events per write syscall from the pane process, not per newline. Verified by remote session behavior.                                                                     |
| Control mode connection lifecycle                | Low      | Same reconnect logic as current PTY attachment (500ms retry, auto-reconnect).                                                                                                                    |
| Multi-client resize conflicts                    | Low      | Only operator client sends resize. `window-size manual` already set on sessions.                                                                                                                 |
| `filterMouseMode` removal                        | Low      | If agent programs enable mouse mode, xterm.js handles it correctly. If tmux status bar enables mouse mode, it won't appear in `%output` (control mode delivers pane content, not client chrome). |
| Binary WebSocket frame handling                  | Low      | Provisioning terminal already uses binary frames successfully. `TextDecoder` is a browser native API.                                                                                            |

## Verification Plan

### Automated

- Existing unit tests for `controlmode.Parser`, `controlmode.Client`
- Existing E2E tests for session spawn/connect/interact
- Add: benchmark test comparing `SendKeys` latency via control mode vs old PTY write
- Add: test that generates 1000 lines rapidly and verifies all arrive at subscriber

### Manual

1. **Scrollback integrity** — run `seq 1 1000` in a session, scroll up, verify no gaps
2. **Input latency** — type in a session, verify no perceptible delay
3. **Special keys** — Enter, Tab, Backspace, arrows, Ctrl-C, Ctrl-D, Escape
4. **Resize** — resize browser window, verify terminal reflows
5. **Multi-client** — open same session in two tabs, verify both receive output, only operator can type
6. **Reconnect** — kill WebSocket, verify reconnect with bootstrap
7. **Session end** — kill tmux session, verify `[Session ended]` appears
8. **Large paste** — paste large text block, verify it arrives
9. **Unicode** — type/paste Unicode, verify rendering
10. **vim/less** — run full-screen programs, verify alternate screen works
