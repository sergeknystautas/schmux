# Cursor Position Loss Analysis

## Root Cause Identified

**tmux's `capture-pane` command does NOT include cursor position escape sequences in its output.**

This is the fundamental issue. When the bootstrap process captures the screen content and writes it to the log file, the cursor position information is lost because:

1. `tmux capture-pane -e -p -S -N` captures the **content** of the pane (with escape sequences for colors/attributes)
2. But it does **NOT** capture the **cursor position** as an ANSI escape sequence
3. When this content is replayed in a terminal emulator, the cursor defaults to position (0,0) or the end of the content
4. The user sees the content but the cursor is not where they left it

## Evidence from Testing

### Test 1: Basic Capture Test

```bash
# Created tmux session with content
# Cursor position: x=41, y=18
# Captured last 20 lines with tmux capture-pane -e -p -S -20
# Result: No cursor position sequence found in capture
```

### Test 2: ANSI Cursor Position Sequence Format

The ANSI escape sequence for cursor position is:

```
ESC [ <row> ; <col> H
```

For a cursor at position (x=41, y=18), the sequence should be:

```
ESC [ 19 ; 42 H
```

In hex: `1b 5b 31 39 3b 34 32 48`

### Test 3: Verification

Searched the captured output for any `ESC [ ... H` sequences - **none found**.

This confirms that `tmux capture-pane` does not include cursor position information.

### Test 4: Comparison of capture-pane flags

Tested `-e` (include escape sequences) vs `-C` (alternative flag):

- `-e`: Captures screen content with color/attribute escape sequences
- `-C`: Escapes escape sequences (makes them literal) - NOT useful for us
- Neither includes cursor position sequences

### Test 5: Full scrollback vs limited capture

Tested `-S -` (full scrollback) vs `-S -1000` (last 1000 lines):

- Both produce identical output format
- Neither includes cursor position sequences
- Capturing more lines doesn't solve the problem

## Why This Matters

When a terminal emulator receives text without cursor positioning:

1. It renders the content line by line
2. The cursor ends up at the end of the last line
3. Any terminal UI (TUI) applications expect the cursor to be at a specific position
4. The TUI rendering becomes incorrect or garbled

## Double Cursor Issue (January 2026)

### Symptom

When viewing Claude Code sessions via xterm.js websocket, two cursors appear:

- **Correct cursor** at line 35 (where Claude's input prompt is)
- **Phantom cursor** at line 39 (4 lines below)

### Root Cause Analysis

1. Claude Code draws its own visual cursor using **reverse video** (`\x1b[7m` space `\x1b[27m`)
2. Claude's UI does `\x1b[4A` (cursor up 4) to update status line, then 4 newlines to return
3. After processing all escape sequences, xterm.js cursor ends at line 39
4. Claude's reverse video block is at line 35
5. Both are visible - hence "double cursor"

**Key insight:** The phantom cursor (line 39) is xterm.js's actual cursor position. The "correct" cursor (line 35) is Claude's visual indicator. The 4-line difference matches the `\x1b[4A` pattern.

### Why tmux doesn't have this problem

When attached directly to tmux:

- tmux maintains a **grid data structure** with correct cursor position
- On attach, tmux **regenerates** output from the grid (not replay of escape sequences)
- Cursor is positioned correctly as part of the redraw

Our approach:

- capture-pane gives historical escape sequences
- pipe-pane gives raw PTY output
- Neither is the "grid-regenerated" output that real tmux clients receive

## Approaches That Have Been Tried

### ❌ Approach 1: Append cursor position sequence to captured content

**What was tried:**

1. Query cursor position using `tmux display-message -p '#{cursor_x} '#{cursor_y}'`
2. Generate ANSI cursor position sequence: `ESC [<row+1>;<col+1>H`
3. Append this sequence to the end of captured content

**Result:** FAILED

- Cursor was still in wrong position (at bottom)
- New content from pipe-pane was written at incorrect positions
- Text appeared intermixed with existing content (e.g., "great thank you" mixed with purple letters)

**Why it failed:**

- Appending cursor position to captured content conflicts with how the terminal processes the captured sequences
- The cursor position we query is the visual position on screen, but when replayed, the terminal interprets it differently
- pipe-pane output contains **relative** cursor movements (like `\x1b[4A`) that assume the cursor is in a specific position - our repositioning breaks those assumptions

### ❌ Approach 2: Hide xterm.js cursor (send `\x1b[?25l`)

**What was tried:**

1. Prepend cursor hide sequence `\x1b[?25l` to the content sent to xterm.js
2. Rely on Claude Code's reverse video block as the visual cursor

**Result:** PARTIALLY WORKED

- Fixed the double cursor issue for Claude Code sessions
- But broke shell sessions - no cursor visible at all
- Not feasible to detect which application is running to conditionally show/hide

**Note:** Must be sent as part of the content, not as a separate message, because the frontend calls `terminal.reset()` on "full" message type which re-shows the cursor.

### ❌ Approach 3: Control mode or headless terminal

**Research findings:**

- tmux control mode (`tmux -CC`) provides structured `%output` notifications
- BUT `%output` is also raw PTY output - "exactly what the application running in the pane sent to tmux"
- xterm.js has a serialize addon that can save/restore terminal state
- A headless terminal emulator could process pipe-pane output and serialize state for clients
- This is a significant architecture change and was not implemented

## Bugs Fixed During Investigation (January 2026)

### Bug 1: File offset calculation in websocket.go

**Location:** `internal/dashboard/websocket.go` line 571

**Bug:** `offset = int64(len(data))` should be `offset += int64(len(data))`

**Impact:** After initial send, offset was reset to a small value instead of advancing to end-of-file. On next tick, we'd re-send most of the file.

**Symptoms:** Captured websocket data was same size as full log file even when bootstrapping from offset.

### Bug 2: extractANSISequences reading entire file when offset=0

**Location:** `internal/dashboard/websocket.go` in `extractANSISequences()`

**Bug:** When `endOffset == 0`, the function read the entire file instead of reading 0 bytes.

**Impact:** When sending the full file (offset=0), we'd scan the entire file for ANSI sequences AND send the entire file, duplicating content.

**Note:** These bugs were red herrings for the double cursor issue - fixing them didn't resolve the cursor problem.

## Future Ideas to Explore

1. **Headless xterm.js server-side**: Process pipe-pane through a headless terminal emulator, use serialize addon to capture state for clients. Significant architecture change.

2. **Application detection**: Detect when Claude Code vs shell is running, conditionally hide cursor. Complex and fragile.

3. **xterm.js options**: Research if future xterm.js versions add cursor visibility options or better state management.

4. **Filter reverse video from Claude**: Strip Claude's visual cursor blocks from output, rely only on xterm.js cursor. Would require understanding Claude's output patterns.

5. **Synchronized output markers**: Claude uses `\x1b[?2026l` / `\x1b[?2026h` for synchronized output. These could potentially be used to bracket updates and handle cursor differently.

## Current Accepted Behavior

**Status:** Cursor and position are NOT perfectly restored after bootstrap recovery, but the system is functional.

**What works:**

- Users can see what happened (captured content is written to log)
- TUI applications will eventually self-correct and redraw properly as they receive new input
- New output from pipe-pane continues to stream correctly

**Limitations:**

- Cursor may be at the wrong position initially after recovery
- Some manual intervention or waiting may be required for TUI apps to fully restore their state

**This is accepted as good enough** - the alternative (losing all session context) would be worse.

## Technical Details

### tmux cursor position format

- `#{cursor_x}`: column position, 0-indexed
- `#{cursor_y}`: row position, 0-indexed

### ANSI cursor position sequence

- Format: `ESC [<row>;<col>H`
- Values are 1-indexed
- Conversion needed: row = cursor_y + 1, col = cursor_x + 1

### WebSocket client behavior

The WebSocket client correctly processes pipe-pane logs that have cursor sequences (when piping from beginning).
The `extractANSISequences()` function in `internal/dashboard/websocket.go` filters cursor movements for replay optimization,
but this is NOT the problem - the client works correctly for normal pipe-pane logs.

## Repro Case Data (January 2026)

Session `schmux-004-3509c4cf` was used for debugging:

- Session created: 2026-01-23T16:22:00 PST
- Log file created: Jan 23 22:10:42 (bootstrap happened ~6 hours after session start)
- Log file size: ~4.1MB
- Captured websocket data saved to `/tmp/ws2-capture-content.txt` (442KB after bootstrap fix)
- **A copy of the log file was saved to ~/Downloads for future debugging (outside of git)**

Key observations in captured data:

- 662 reverse video on (`\x1b[7m`) sequences, 662 reverse video off (`\x1b[27m`) - balanced
- 0 cursor hide (`\x1b[?25l`) or show (`\x1b[?25h`) sequences
- 0 cursor save (`\x1b7` or `\x1b[s`) or restore (`\x1b8` or `\x1b[u`) sequences
- Many `\x1b[4A` (cursor up 4) sequences matching the 4-line offset between cursors
- Log file starts with pipe-pane style output (cursor movements, sync markers), not capture-pane content

## References

- [tmux GitHub Issue #1949](https://github.com/tmux/tmux/issues/1949) - Copy mode cursor position
- [tmux GitHub Issue #3787](https://github.com/tmux/tmux/issues/3787) - Correlating cursor position with capture-pane
- [ANSI Device Status Report](https://stackoverflow.com/questions/60134860/how-to-read-the-cursor-position-given-by-the-terminal-ansi-device-status-report)
- [tmux Control Mode Wiki](https://github.com/tmux/tmux/wiki/Control-Mode) - Structured protocol for tmux clients
- [tmux screen-redraw.c](https://github.com/tmux/tmux/blob/master/screen-redraw.c) - How tmux regenerates screen from grid
- [xterm.js serialize addon](https://github.com/xtermjs/xterm.js/tree/master/addons/addon-serialize) - Terminal state serialization
- [xterm.js ITerminalOptions](https://xtermjs.org/docs/api/terminal/interfaces/iterminaloptions/) - No built-in cursor hide option
