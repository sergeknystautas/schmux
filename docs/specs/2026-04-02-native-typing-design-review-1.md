VERDICT: NEEDS_REVISION

## Summary Assessment

The core concept is sound -- client-side buffering of printable keystrokes with flush-on-Enter is the right approach for the agentic use case, and restricting scope to non-TUI typing is wise. However, the cursor reconciliation mechanism has a fundamental correctness bug (viewport-relative vs absolute positioning), and the design underspecifies interactions with several existing systems that will produce real failures.

## Critical Issues (must fix)

### 1. CUP escape addresses viewport rows, not buffer rows -- cursor rewind will target the wrong position

The design says to save `localEchoStart` as `{row, col}` using `terminal.buffer.active.cursorY` (+ `baseY` for absolute row), then rewind with `\x1b[{row};{col}H`. This is wrong.

`\x1b[row;colH` (CUP) addresses **viewport-relative** rows (1 = top of visible viewport, N = bottom). It does NOT address absolute scrollback buffer positions. `cursorY` from xterm.js is already viewport-relative (0-indexed). `baseY` is the scrollback offset and must NOT be added.

The design parenthetically mentions "(+ `baseY` for absolute row)" which suggests confusion about the coordinate system. The correct approach is: save `cursorY` and `cursorX` (both 0-indexed viewport-relative), then rewind with `\x1b[{cursorY+1};{cursorX+1}H`. Adding `baseY` would produce row numbers in the thousands for sessions with any scrollback, and CUP would silently clamp to the last viewport row -- placing the cursor at the wrong position.

Additionally, if the user scrolls up (viewport is not at bottom), `cursorY` is still relative to the viewport origin (top of visible area), but CUP addresses the viewport, not the buffer. Writing locally-echoed text while scrolled up would render at a visually correct position relative to the viewport, but the saved coordinates would be wrong if the user scrolls back to bottom before flushing. The design should specify that local echo only activates when `followTail` is true (viewport is at bottom), or flush the buffer on scroll-away.

### 2. Server-side `ClassifyKeyRuns` only treats ASCII 32-126 as printable -- multi-byte UTF-8 characters are silently dropped

The design says "Unicode characters (multi-byte: accented letters, CJK, emoji)" should be buffered and locally echoed. When the buffer is flushed, the batched string (e.g., "cafe\u0301") is sent to the server. The server passes this through `ClassifyKeyRuns()` in `keyclassify.go`, which only recognizes bytes in range 32-126 as printable literal text (line 40: `keys[j] >= 32 && keys[j] < 127`).

Multi-byte UTF-8 characters have byte values >= 128, so they break the printable run. Bytes outside the recognized ranges (not a control character, not ESC, not a known special) fall through to the `default` case, which only handles ctrl characters (1-26). Unrecognized bytes produce an empty `keyName` and are silently skipped via `if keyName != ""` on line 141.

This means **any non-ASCII text typed by the user would be silently eaten by the server** when sent as a batch. This is an existing bug in the codebase, but the native typing design explicitly claims Unicode support and would make this bug far more visible (currently per-keystroke input sends one character at a time, which may mask the issue differently).

The design must either: (a) document that only ASCII text is supported for local echo, or (b) require fixing `ClassifyKeyRuns` to handle multi-byte UTF-8 as part of the implementation.

### 3. Sync mechanism interaction is unaddressed

The design says "No changes to `handleOutput()`" and that server echo will overwrite local echo seamlessly. But it does not address the sync/correction mechanism at all.

When sync is enabled (`GetXtermSyncCheckEnabled()`), the server periodically sends `capture-pane` snapshots. If a sync fires while the user has text in the local buffer, the xterm.js viewport shows the locally-echoed text that tmux does not know about. The `handleSync` comparison will detect a mismatch (locally-echoed text vs tmux's view) and apply a "surgical correction" -- overwriting the locally-echoed text with tmux's version (which has nothing there). This would erase the user's in-progress typing from the screen.

Even though sync is currently disabled by default, it can be enabled via config, and the design should either: (a) explicitly state that native typing and sync are incompatible and enforce mutual exclusion, or (b) suppress sync comparisons while the local buffer is non-empty.

### 4. `inputLatency` instrumentation will produce misleading measurements

The `inputLatency` module calls `markSent()` in `sendRawInput()` and `markReceived()` when the next binary frame arrives. With native typing, `sendRawInput()` is called only on flush (when Enter is pressed), sending a batch of characters. But `markReceived()` fires on the very next server output frame, which might be the echo of the first character in the batch.

This means the measured latency will be: time from flush to first echo frame. For a batch "hello\r", the measured "latency" captures only the round-trip for the entire batch, not per-keystroke latency. The `markSent` is called once, but the batch produces multiple echo frames, so subsequent frames will be matched against nothing (or the wrong keystroke in the pending queue).

The server-side latency collector (`pendingInputQueue` in `websocket.go`) will also mismatch because it expects one `inputEcho` timing per keystroke, but with batched input it gets one timing for the entire batch.

The design should address whether `inputLatency` should be disabled during native typing mode, or adapted to measure flush-to-echo latency instead of keystroke-to-echo latency.

## Suggestions (nice to have)

### 5. Wide character backspace needs `wcwidth` -- xterm.js internal width lookup is not exposed

The design correctly identifies that emoji and CJK characters occupy two terminal columns. It says "the Backspace erase logic needs to account for character width" but does not specify how to determine the width. xterm.js does not expose a public API for character width measurement. The implementation will need to either: use the `Unicode11Addon`'s `wcwidth` (which is loaded -- the terminal-pipeline.md claiming it is disabled is out of date), or use a standalone `wcwidth` implementation. This should be specified.

### 6. Paste handling with mixed content is underspecified

The design says pastes containing Enter characters trigger partial flushes. But pastes can also contain control characters (Ctrl+C in copied terminal output), tab characters, and ANSI escape sequences (copying from a colored terminal). The classification logic needs to handle these consistently -- a paste of "hello\tworld\n" should flush "hello" as buffer, send Tab as immediate, then continue buffering "world", then flush on the newline. The design should clarify whether the entire paste is classified character-by-character or whether a simpler approach (flush entire buffer, send paste as-is) is preferred.

### 7. Consider flushing on any non-printable server output, not just scroll

The design handles terminal resize and scroll as events that invalidate `localEchoStart`. But server output during local echo (item covered under "Server output during local echo") can also invalidate the saved position without scrolling. For example, the agent sending a `\r` (carriage return) to the line where local echo started would move tmux's cursor but not affect the saved position. Adding a simple "flush on any server output while buffer is non-empty" rule would be more robust than trying to enumerate invalidating events.

### 8. The `attachCustomKeyEventHandler` path bypasses `sendInput` for Alt+Enter and Alt+Backspace

Lines 383-401 in `terminalStream.ts` show that `attachCustomKeyEventHandler` intercepts Alt+Enter and Alt+Backspace and calls `this.sendInput()` directly with escape sequences. These calls will flow through the Ctrl+V image check but then to `sendRawInput()`. If the native typing modification only hooks `sendInput()` at the top level, these paths would bypass local echo classification. However, since these produce non-printable sequences (`\x1b\r`, `\x1b\x7f`), they should be classified as "immediate" keys, which means the buffer should be flushed before sending them. The implementation should verify this path is covered.

### 9. Connection drop should attempt to flush before losing buffer

The design says "If the WebSocket disconnects while text is in the local buffer, the buffer is lost." This is acceptable but could be improved: on `ws.onclose`, if the buffer is non-empty and the WebSocket was previously connected, the implementation could stash the buffer contents and restore them after reconnect (writing them back into the terminal via local echo). This would prevent losing a half-typed prompt during transient disconnections, which are common with Cloudflare tunnels.

## Verified Claims (things you confirmed are correct)

- **"The entire feature is client-side. No server changes needed."** Confirmed. The server receives input via binary WebSocket frames (`sendRawInput` encodes with `TextEncoder` and sends as binary). The `startWSMessageReader` in `websocket_helpers.go` routes `BinaryMessage` to `WSMessage{Type: "input", Data: string(msg)}`. The server's input coalescing (`drain` loop in the `case "input"` handler) already batches multiple input messages. Batched text goes through `ClassifyKeyRuns` which handles runs of printable ASCII correctly. The server is genuinely agnostic to per-key vs batched delivery for ASCII text.

- **"sendRawInput() exists and is the right hook point."** Confirmed at line 1054 of `terminalStream.ts`. It encodes via `TextEncoder` and sends as a binary WebSocket message. The `sendInput()` method at line 1022 is the public entry point that handles the Ctrl+V clipboard check before delegating to `sendRawInput()`.

- **"xterm.js onData is the single entry point for keyboard input."** Confirmed at line 404. The `attachCustomKeyEventHandler` at line 387 intercepts a small set of Alt-modified keys before they reach `onData`, but those call `sendInput()` which is also the target for modification.

- **"terminal.buffer.active.cursorX and cursorY are available."** Confirmed -- used in multiple places in the codebase (diagnostics at line 1092-1093, sync at line 1406).

- **"The feature is off by default."** The design is clear about this. `localStorage`-based persistence is consistent with the codebase (existing `localStorage` usage found in `tabOrder.ts`, `accessoryTabOrder.ts`).

- **"Resize handler exists and can be hooked."** Confirmed. `fitTerminal()` handles resize and sends `sendResize()`. The design's plan to flush the buffer on resize is implementable by adding a call in `fitTerminal()`.

- **"Unicode11Addon is loaded."** Despite `terminal-pipeline.md` claiming it is "DISABLED", the code at line 331 of `terminalStream.ts` shows `this.terminal.loadAddon(new Unicode11Addon())` and `this.terminal.unicode.activeVersion = '11'` -- it IS loaded and active. The pipeline doc is stale. The design can rely on Unicode11Addon for wide character width measurement.
