# Native Typing: Client-Side Input Buffering for Low-Latency Typing

**Date:** 2026-04-02
**Status:** Design (v2 — revised after review)
**Branch:** `feature/native-typing`

---

## Problem

When the schmux server is hosted on a remote machine (e.g., cloud VM with 30ms ping), every keystroke takes a 60ms+ round trip: key press -> WebSocket -> server -> tmux send-keys -> agent echo -> tmux %output -> WebSocket -> xterm.js. This makes typing feel sluggish compared to a native terminal. For local connections the latency is imperceptible, but for remote access via Cloudflare tunnel or SSH, the delay is noticeable and annoying.

## Solution

Buffer printable keystrokes on the client side and render them directly into the xterm.js terminal buffer with zero latency. Send the accumulated text to the server only when the user presses Enter (or any non-buffered key). A cursor repositioning trick on flush ensures the server echo overwrites the locally-rendered text without duplication or flicker.

## Scope

This feature targets the agentic use case: typing prompts into Claude Code, Codex, and similar AI harnesses. Interactive TUI programs (vim, htop, fzf) are out of scope — their per-keystroke input model is incompatible with buffering.

The feature is **off by default** and must be explicitly enabled by the user. When disabled, the input path is completely unchanged.

---

## Design

### Activation Model

Always-on when the feature is enabled. Every printable character is locally echoed regardless of agent state. No idle detection or prompt detection heuristics.

**Rationale:** In the agentic workflow, the user only types when the agent is done and showing a prompt. The agent actively outputting while the user types is rare. If it does happen, any visual glitch self-corrects when the agent's output overwrites the affected region. The simplicity of always-on outweighs the complexity of detection heuristics.

### Input Classification

Every keystroke from xterm.js `onData` is classified into one of two categories:

**Buffered** — written to a local buffer and echoed into xterm.js immediately:

- All printable characters (code point >= 32, excluding control range)
- Unicode characters (multi-byte: accented letters, CJK, emoji)
- Backspace (`\x7f` or `\x08`) — removes last character from buffer and screen

**Immediate** — sent to the server via the existing WebSocket path:

- Enter (`\r`) — flushes buffer first, then sends Enter
- Control characters (Ctrl+C, Ctrl+D, Ctrl+Z, etc.)
- Arrow keys (Up, Down, Left, Right)
- Tab, Escape
- Function keys
- Alt/Meta combinations (except those producing printable characters)
- Alt+Enter (`\x1b\r`) and Alt+Backspace (`\x1b\x7f`) — these arrive via `attachCustomKeyEventHandler` and must also trigger a flush before being sent

**Flush-before-send rule:** When an immediate key is pressed and the local buffer is non-empty, the buffer is flushed before the key is sent. This ensures the server receives the typed text before any special key. Example: user types "helo", presses Left arrow -> flush sends "helo" to server, then Left arrow is sent. The user transitions from local echo mode to server-driven editing.

**Design decision — arrow keys are not buffered:** Arrow key behavior is deeply agent-specific. In Claude Code alone, Up arrow has three modes depending on context (move up within multi-line input, jump to start of single-line input, recall history). Replicating agent-specific key bindings locally is impractical and fragile. The tradeoff: typing (the common case) is instant; cursor navigation (the rare case) has latency.

### Local Echo Rendering

When a printable character is received and the feature is enabled:

1. **First character:** Save the current xterm.js cursor position as `localEchoStart = {row, col}`. The cursor position is read from `terminal.buffer.active.cursorY` and `terminal.buffer.active.cursorX`. Both are **viewport-relative** and **0-indexed**. Do NOT add `baseY` — CUP escape sequences address viewport rows, not buffer-absolute rows.
2. **Write to buffer:** Append the character to `localBuffer` string.
3. **Write to terminal:** Call `terminal.write(char)`. xterm.js renders the character at the cursor position and advances the cursor. Line wrapping is handled automatically by xterm.js.

**Scroll guard:** Local echo only activates when `followTail` is true (viewport is at the bottom). If the user has scrolled up to read history, keystrokes bypass local echo and are sent immediately. This prevents the saved cursor position from becoming invalid due to viewport offset.

For Backspace when the buffer is non-empty:

1. Remove the last character from `localBuffer`.
2. Determine the display width of the removed character. Use xterm.js's `Unicode11Addon` (which is loaded and active) to get the `wcwidth` — standard characters are 1 column, emoji and CJK are 2 columns.
3. If the cursor is not at a line wrap boundary: write `\b \b` to xterm.js for single-width characters, or `\b\b  \b\b` for double-width characters.
4. If the cursor is at column 0 (wrapped to new line): move cursor up one row and to the appropriate column (last column for single-width, second-to-last for double-width), clear the cell(s), and reposition.
5. If the buffer is now empty: clear `localEchoStart`. The next printable character will save a fresh cursor position.

### Flush and Cursor Reconciliation

When the local buffer is flushed (Enter or any immediate key), three things happen in order:

**Step 1 — Cursor rewind.** Write `\x1b[{row};{col}H` to xterm.js, where row = `localEchoStart.row + 1` and col = `localEchoStart.col + 1` (converting from 0-indexed viewport-relative to 1-indexed ANSI CUP coordinates). This moves xterm.js's internal cursor back to where typing began. The locally-echoed text remains visible on screen — only the cursor moves.

**Step 2 — Send input to server.** Send the buffered string via the existing `sendRawInput()` binary WebSocket path. If the flush was triggered by Enter, send the buffer contents followed by `\r`. If triggered by another immediate key, send the buffer contents followed by that key's byte sequence.

**Step 3 — Clear local state.** Reset `localBuffer` to empty string, set `localEchoStart` to null.

**Why this works:** From tmux's perspective, the cursor never moved during local echo — no keystrokes were sent. When the server receives the input and the agent echoes it, the echo writes starting from the same position where our rewound cursor sits. The server echo overwrites the locally-rendered text cell by cell. No duplication, no flicker, no dependency on agent behavior.

If the agent adds formatting (colors, attributes) to the echo, the overwrite replaces our plain-text local echo with the styled version — a visual upgrade that happens seamlessly.

### Local Buffer State

```
localBuffer: string                       // accumulated text, empty when inactive
localEchoStart: {row: number, col: number} | null  // viewport-relative, 0-indexed cursor position when first char was buffered
```

The buffer is a simple string. Characters are appended at the end, Backspace removes from the end. There is no cursor index within the buffer — the cursor is always at the end (arrow keys flush and exit local echo mode).

### Interaction with Existing Systems

**Sync mechanism.** When sync is enabled (`GetXtermSyncCheckEnabled()`), the server periodically sends `capture-pane` snapshots for comparison. If a sync fires while the local buffer is non-empty, the comparison would detect the locally-echoed text as a "mismatch" and surgically erase it. **Fix:** On the frontend, skip sync comparison while `localBuffer` is non-empty. The sync check is a defense-in-depth paranoia mechanism — deferring it by a few seconds while the user types has zero impact on terminal consistency.

**Input latency instrumentation.** The `inputLatency` module assumes 1:1 keystroke-to-echo pairing (`markSent` on each keystroke, `markReceived` on next output frame). With batched input, a single `markSent` fires for the entire batch, producing meaningless measurements. **Fix:** Skip `inputLatency.markSent()` calls while native typing is enabled. The latency measurement is irrelevant when local echo provides instant feedback — the whole point of the feature is to make round-trip latency invisible.

**Server output during local echo.** If the agent sends output while the user has text in the local buffer, flush the buffer immediately. This is safer than trying to maintain local echo state while the terminal is being modified by server output. The flush sends the text typed so far to the server and clears local state. Any remaining typing resumes with fresh local echo state after the server output settles.

### Edge Cases

**Backspace at line wrap boundary.** A simple `\b \b` does not cross line boundaries in xterm.js. When the cursor is at column 0 after a wrap, Backspace must move the cursor up one row to the last column, clear that cell, and reposition. The terminal column count is available from xterm.js.

**Wide character backspace.** Emoji and CJK characters occupy two terminal columns. Use the `Unicode11Addon`'s `wcwidth` (loaded and active at `terminalStream.ts:331`) to determine the display width of the character being removed. Erase the correct number of columns.

**Paste.** Large pastes arrive as a single `onData` event with multiple characters. The entire paste is classified character-by-character. Printable characters are appended to the local buffer and written to xterm.js. If the paste contains non-printable characters (Enter, Tab, control characters, ANSI escape sequences), each triggers a flush of the accumulated buffer before the non-printable is sent immediately. This handles mixed-content pastes correctly: "hello\tworld\n" flushes "hello", sends Tab, buffers "world", flushes and sends Enter.

**Terminal resize during local echo.** Resizing changes the wrapping geometry and invalidates `localEchoStart`. On resize, flush the buffer immediately. The text is sent to the server, the agent re-renders at the new dimensions, and local echo state resets.

**Scroll during local echo.** If the user scrolls away from the bottom (`followTail` becomes false) while text is in the local buffer, flush the buffer immediately. The saved viewport-relative cursor position would be invalid after scrolling.

**Connection drop.** If the WebSocket disconnects while text is in the local buffer, the buffer is lost. This matches existing behavior — typing into a disconnected terminal is lost. On reconnect, bootstrap replays server state.

**Empty buffer + Enter.** Enter with no local buffer is sent immediately as a normal keystroke. No flush logic involved.

### Configuration

**Toggle:** A per-session checkbox in the terminal toolbar (next to "select lines"), persisted in `localStorage` keyed by session ID. **Off by default.** When disabled, all keystrokes go through the existing `sendRawInput()` path unchanged — zero behavior difference, zero risk.

**Scope:** Per-session. The user enables native typing for individual sessions where they're interacting with an AI agent prompt, and leaves it off for sessions running shells or TUI programs. The setting is sticky — it survives page reloads and tab switches via `localStorage`.

**Future enhancement:** Auto-detection based on measured latency. The existing `inputLatency` module tracks WebSocket round-trip times. A threshold-based suggestion ("Latency is 85ms — enable native typing?") or auto-enable could be added in a future iteration.

---

## Implementation Surface

### Server-Side Change

**One required server-side fix:** `ClassifyKeyRuns()` in `internal/remote/controlmode/keyclassify.go` only treats bytes 32-126 as printable. Multi-byte UTF-8 characters (code points >= 128) have byte values >= 0x80 which fall outside this range and are silently dropped. This is an existing bug, but native typing makes it critical — the design promises Unicode support, and the batched input path sends the full string through `ClassifyKeyRuns`.

**Fix:** Extend the printable run detection in `ClassifyKeyRuns` to recognize valid UTF-8 continuation bytes (0x80-0xBF following a leading byte 0xC0-0xF7) as part of literal text runs. The `send-keys -l` command handles UTF-8 correctly — only the classifier needs updating.

### Client-Side Changes

| File                                                       | Change                                                                                                |
| ---------------------------------------------------------- | ----------------------------------------------------------------------------------------------------- |
| `assets/dashboard/src/lib/terminalStream.ts`               | Core implementation: buffer state, input classification, local echo, flush logic, sync/latency guards |
| `assets/dashboard/src/components/SessionDetailPage.tsx`    | Toggle UI for native typing                                                                           |
| `assets/dashboard/src/lib/localStorage.ts` (or equivalent) | Persist toggle state                                                                                  |

### Changes to `terminalStream.ts`

- **New state:** `localBuffer`, `localEchoStart`, `nativeTypingEnabled`
- **Modify `sendInput()`** — existing entry point for all keystrokes. Add classification: printable -> buffer + local echo, Backspace -> local erase, everything else -> flush + send
- **New method `flushLocalBuffer()`** — cursor rewind + send buffered text + clear state
- **New method `echoLocally(char)`** — write to xterm.js buffer, update local state
- **New method `localBackspace()`** — erase last character, handle wrap boundary and wide chars
- **Modify resize handler** — flush buffer on resize
- **Modify scroll handler** — flush buffer when `followTail` becomes false
- **Modify `handleOutput()`** — flush buffer on any incoming server output while buffer is non-empty
- **Modify sync handler** — skip sync comparison while `localBuffer` is non-empty
- **Modify `sendRawInput()`** — skip `inputLatency.markSent()` while native typing is enabled
- **Ensure `attachCustomKeyEventHandler` path** — Alt+Enter and Alt+Backspace trigger flush before send

### What Doesn't Change

- WebSocket binary frame protocol
- Server-side WebSocket handler (`websocket.go`)
- Control mode pipeline (`controlmode/client.go`, `parser.go`)
- Session tracker (`tracker.go`)
- Output path (fan-out, escape holdback, gap detection)

---

## Risks

**Echo mismatch.** If the server echo does not write to the same cursor positions as the local echo (e.g., the agent moves the cursor before echoing), there could be brief visual duplication. This self-corrects when the agent completes its response rendering but may be jarring. Mitigation: the cursor rewind ensures xterm.js and tmux cursors are in sync at flush time, making position-matched echo the expected behavior.

**Backspace at wrap boundaries.** The wrap-aware backspace logic must correctly handle the cursor moving up a row. An off-by-one error here would corrupt the display. Mitigation: thorough testing with strings that wrap at various terminal widths.

**Wide characters.** Emoji and CJK characters occupy two terminal columns but are one character in the buffer string. Backspace must erase the correct number of columns. Use `Unicode11Addon`'s `wcwidth` for width determination.

**Stale localEchoStart.** If server output arrives while the user is typing, the saved cursor position could become invalid. Mitigation: flush buffer immediately on any incoming server output (see "Interaction with Existing Systems" section).

**UTF-8 in ClassifyKeyRuns.** The server-side key classifier must be fixed to handle multi-byte UTF-8 before native typing can support non-ASCII input. Without this fix, Unicode characters typed in local echo mode would be silently dropped when flushed to the server.
