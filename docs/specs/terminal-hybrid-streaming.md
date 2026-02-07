# Terminal Hybrid Streaming Architecture

Design notes for restoring xterm.js UX while keeping real-time PTY-attached streaming.

**Status**: Proposed

## Problem Statement

Commit `dd4e203` ("Replace log polling with PTY-attached tmux client for WebSocket terminals") simplified the terminal streaming architecture but broke several xterm.js UX features:

1. **Resizing broken** — PTY resize approach doesn't properly update the tmux window
2. **Mouse scrolling captured** — Scroll events go through PTY to tmux, not xterm.js buffer
3. **Keyboard input captured** — All input goes to PTY, breaking React-side shortcuts
4. **Scrollback history lost** — Users can't scroll back to see previous output in the browser

The goal is to restore browser-native terminal UX while keeping the performance benefits of PTY-attached streaming.

## Historical Context

### Phase 1: Pipe-Pane + File Polling (Pre-dd4e203)

The original architecture used `tmux pipe-pane` to stream session output to a log file, with the WebSocket handler polling the file:

```
tmux session → pipe-pane → log file → WebSocket handler (polling) → browser
                                              ↑
                              (100ms intervals, offset tracking)
```

**Input path**: `browser → WebSocket → tmux.SendKeys() → tmux session`
**Resize path**: `browser → WebSocket → tmux.ResizeWindow() → tmux session`

This approach had complexity:
- Log file rotation when size exceeded threshold (~1MB)
- Byte offset tracking to avoid re-sending content
- Safe start point detection to avoid mid-escape-sequence reads
- ANSI sequence extraction for terminal state priming
- Concurrency controls for rotation locks

**But it worked well for UX**:
- xterm.js owned the scrollback buffer
- Mouse wheel scrolled the xterm.js viewport (not sent to tmux)
- Links were clickable (WebLinksAddon)
- Text selection worked
- Browser resize events sent `tmux resize-window` commands

The `websocket.go` file was 615 lines with rotation, offset tracking, and ANSI parsing logic.

### Phase 2: Full PTY Attach (dd4e203)

The new architecture attaches a dedicated tmux client over a PTY:

```
tmux session ← → PTY (tmux attach-session) → WebSocket handler → browser
                         ↑
            (real-time bidirectional, all input/output through PTY)
```

**Input path**: `browser → WebSocket → ptmx.Write() → PTY → tmux client → tmux session`
**Resize path**: `browser → WebSocket → pty.Setsize() → PTY only (not tmux window)`

Benefits:
- Eliminated file polling latency
- Removed log rotation complexity
- Removed ANSI extraction logic
- Simplified to 233 lines

**But broke UX**:
- tmux's mouse mode captures scroll events → sent through PTY → scroll goes to tmux, not xterm.js
- All keyboard input goes through PTY → no way to intercept for React shortcuts
- `pty.Setsize()` only resizes the PTY, doesn't call `tmux resize-window`
- xterm.js scrollback buffer is bypassed when tmux handles scrolling

## Architectural Options Considered

### Option 1: Decoupled I/O (No PTY Attach)

Separate input and output channels entirely:
- Output: Stream via `tmux pipe-pane` or periodic `capture-pane -p -e`
- Input: Send via `tmux send-keys` API calls
- Resize: Use `tmux resize-pane` commands

**Pros**: xterm.js scrollback works natively, no keyboard/mouse capture
**Cons**: Output latency (polling or pipe-pane lifecycle), sync issues

### Option 2: Modal Interaction (Observation/Interactive Modes)

Default to read-only observation mode, explicit toggle to interact:
- Observe mode (default): Stream output only, browser scroll/shortcuts work
- Interactive mode (click-in or hotkey): Full PTY capture for typing

**Pros**: Clear mental model, best of both when needed
**Cons**: Mode switching friction, potential state sync issues when switching

### Option 3: Focus-Based Capture

Terminal only captures input when explicitly focused:
- Click inside terminal → captures keyboard
- Click outside (or Escape) → releases capture
- Mouse wheel always scrolls xterm.js buffer

**Pros**: Familiar pattern (like web IDEs), minimal friction
**Cons**: Still need scroll handling, accidental typing in wrong context

### Option 4: xterm.js Scrollback + PTY Passthrough

Keep PTY for input, but fix scrolling at the xterm.js layer:
- Configure xterm.js with large scrollback buffer
- Intercept mouse wheel events → scroll xterm.js buffer, NOT PTY
- Keyboard capture only when terminal has focus

**Pros**: Minimal backend changes, browser-like scroll UX
**Cons**: Still need focus management, doesn't address resize

### Option 5: Hybrid — PTY for Output, Commands for Input (CHOSEN)

Keep PTY-attached streaming for real-time output, but route input/resize through tmux commands:

```
┌─────────────────────────────────────────────────────────────────────┐
│  PTY-attached client (READ-ONLY)                                    │
│  - Reads output from tmux session                                   │
│  - Streams to browser via WebSocket                                 │
│  - Never writes to PTY (except for resize signal)                   │
└────────────────────────────┬────────────────────────────────────────┘
                             │ output stream (real-time)
                             ▼
┌─────────────────────────────────────────────────────────────────────┐
│  xterm.js                                                           │
│  - Receives output, builds scrollback buffer                        │
│  - Mouse wheel scrolls buffer locally (not sent to backend)         │
│  - Links clickable (WebLinksAddon)                                  │
│  - Text selection works                                             │
│  - Follow mode auto-scrolls when enabled                            │
└────────────────────────────┬────────────────────────────────────────┘
                             │ keyboard input only
                             ▼
┌─────────────────────────────────────────────────────────────────────┐
│  tmux send-keys / resize-window                                     │
│  - Input sent via tmux.SendKeys() command                           │
│  - Resize sent via tmux.ResizeWindow() + pty.Setsize()              │
│  - Separate from the PTY read channel                               │
└─────────────────────────────────────────────────────────────────────┘
```

**Why this approach**:
1. **Real-time output**: PTY attach is faster than file polling (no 100ms intervals)
2. **xterm.js UX restored**: Mouse scroll stays in browser, links work, selection works
3. **Proven input path**: `tmux send-keys` was used in Phase 1 and worked
4. **Proven resize path**: `tmux resize-window` was used in Phase 1 and worked
5. **Minimal changes**: Only the input/resize handlers change, not the output streaming

## Detailed Design

### Backend Changes: `internal/dashboard/websocket.go`

#### Input Handler (lines ~199-214)

**Current** (broken):
```go
case "input":
    // nudge clearing...
    if _, err := ptmx.Write([]byte(msg.Data)); err != nil {
        return
    }
```

**Proposed**:
```go
case "input":
    // nudge clearing... (keep existing logic)

    // Send input via tmux command, not PTY write
    inputCtx, inputCancel := context.WithTimeout(context.Background(),
        time.Duration(s.config.GetXtermOperationTimeoutMs())*time.Millisecond)
    if err := tmux.SendKeys(inputCtx, sess.TmuxSession, msg.Data); err != nil {
        inputCancel()
        fmt.Printf("[terminal] error sending keys to tmux: %v\n", err)
        // Don't return - input failure shouldn't kill connection
    }
    inputCancel()
```

**Rationale**: `tmux send-keys` sends the raw key data to the tmux session without going through the attached PTY client. This is the same approach used in Phase 1.

#### Resize Handler (lines ~215-230)

**Current** (broken):
```go
case "resize":
    var resizeData struct { Cols int; Rows int }
    json.Unmarshal([]byte(msg.Data), &resizeData)
    if err := pty.Setsize(ptmx, &pty.Winsize{...}); err != nil {
        fmt.Printf("[terminal] error resizing tmux client PTY: %v\n", err)
    }
```

**Proposed**:
```go
case "resize":
    var resizeData struct { Cols int; Rows int }
    if err := json.Unmarshal([]byte(msg.Data), &resizeData); err != nil {
        fmt.Printf("[terminal] error parsing resize data: %v\n", err)
        continue
    }
    if resizeData.Cols <= 0 || resizeData.Rows <= 0 {
        continue
    }

    // 1. Resize the tmux window (what gets rendered to the session)
    resizeCtx, resizeCancel := context.WithTimeout(context.Background(),
        time.Duration(s.config.GetXtermOperationTimeoutMs())*time.Millisecond)
    if err := tmux.ResizeWindow(resizeCtx, sess.TmuxSession, resizeData.Cols, resizeData.Rows); err != nil {
        fmt.Printf("[terminal] error resizing tmux window: %v\n", err)
    }
    resizeCancel()

    // 2. Also resize the attached PTY so it receives correctly-sized output
    if err := pty.Setsize(ptmx, &pty.Winsize{
        Cols: uint16(resizeData.Cols),
        Rows: uint16(resizeData.Rows),
    }); err != nil {
        fmt.Printf("[terminal] error resizing PTY: %v\n", err)
    }
```

**Rationale**: Both resizes are needed:
- `ResizeWindow` changes the actual tmux window dimensions (what the session sees)
- `pty.Setsize` tells the attached PTY client about the new size (so output is formatted correctly)

The order matters: resize tmux first, then the PTY, so the PTY receives output already sized for the new dimensions.

### Backend Cleanup: `internal/session/manager.go`

#### Remove pipe-pane startup (lines ~139-142)

**Current**:
```go
// Start pipe-pane to log file
if err := tmux.StartPipePane(ctx, tmuxSession, logPath); err != nil {
    return nil, fmt.Errorf("failed to start pipe-pane (session created): %w", err)
}
```

**Proposed**: Remove this block from Spawn and SpawnCommand. Pipe-pane is no longer used for WebSocket streaming. Keep `ensureLogFile()` for logging/debugging purposes.

Note: The daemon bootstrap code (`EnsurePipePane`, `daemon.go` recovery logic) is separate logging infrastructure and out of scope for this change.

### Frontend: No Changes Expected

The frontend (`terminalStream.ts`) already has the correct behavior:
- `scrollback: 1000` — maintains local buffer
- `WebLinksAddon` — clickable links
- `handleUserScroll()` / `followTail` — follow mode with scroll detection
- `sendResize()` — sends resize messages to backend
- `sendInput()` — sends keyboard input to backend

The issue was backend-side: writing input to PTY and only resizing PTY (not tmux window).

### Existing Safeguards

Session creation already calls (in `session/manager.go`):
```go
tmux.SetWindowSizeManual(ctx, tmuxSession)  // Prevent client size fighting
tmux.ResizeWindow(ctx, tmuxSession, width, height)  // Set initial size
```

This ensures the attached PTY client doesn't override explicit resize commands.

## Known Risks and Concerns

| Risk | Severity | Mitigation |
|------|----------|------------|
| **SendKeys edge cases** | Medium | Test special chars, Unicode, large pastes, bracketed paste mode |
| **Process spawn overhead** | Low | One `send-keys` per keystroke batch; Phase 1 worked fine |
| **Escape sequence splitting** | Low | xterm.js batches sequences; `\x1b[A` sent as unit |
| **Resize race conditions** | Low | ResizeWindow before Setsize; tmux updates first |
| **Scroll handling** | Medium | Verify Phase 1 scroll logic (follow mode, viewport events) works with hybrid approach |
| **Alternate screen apps** | Accepted | xterm.js scrollback doesn't work with vim/less/htop; user can use Ctrl+B, [ for tmux copy mode |

## Verification Plan

### Manual Testing

1. **Basic typing**: Open session, type commands, verify execution
2. **Special keys**: Enter, Tab, Backspace, arrow keys, Ctrl+C, Ctrl+D
3. **Resize**: Resize browser window, verify terminal reflows correctly
4. **Scrollback**: Generate output (e.g., `seq 1000`), scroll up with mouse wheel
5. **Links**: Output a URL, verify it's clickable
6. **Follow mode**: Scroll up (pauses), scroll to bottom or click Resume (resumes)
7. **Text selection**: Select text, copy to clipboard
8. **Session end**: Kill tmux process, verify "[Session ended]" message
9. **Large paste**: Paste a large block of text, verify it arrives correctly
10. **Unicode**: Type/paste Unicode characters, verify rendering

### Regression Testing

- Existing E2E tests should still pass
- Add specific test for resize behavior if not covered

## Open Questions

1. **Mouse wheel in mouse mode**: If tmux enables mouse reporting, does xterm.js forward wheel events as escape sequences? May need to intercept at DOM level.

2. **SendKeys vs SendLiteral**: Current code uses `SendKeys` (interprets key names). Should we use `SendLiteral` with `-l` flag for raw passthrough? Phase 1 used `SendKeys` and worked.

3. **Log file cleanup**: Are log files used for anything else (debugging, export)? If so, keep `ensureLogFile` but remove `StartPipePane`.

4. **Multiple browser clients**: If two browsers connect to the same session, both read from PTY (works), but both send input via `send-keys` (should work, but verify no conflicts).

## Appendix: Key Code References

- `internal/dashboard/websocket.go` — WebSocket handler, PTY attach, input/resize
- `internal/tmux/tmux.go` — `SendKeys()`, `ResizeWindow()`, `SetWindowSizeManual()`
- `internal/session/manager.go` — Session creation, pipe-pane setup
- `assets/dashboard/src/lib/terminalStream.ts` — xterm.js wrapper, resize detection
- Commit `dd4e203` — The change that introduced the current (broken) architecture
