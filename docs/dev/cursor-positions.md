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
- This causes new pipe-pane output to be written at wrong positions

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

## References

- [tmux GitHub Issue #1949](https://github.com/tmux/tmux/issues/1949) - Copy mode cursor position
- [tmux GitHub Issue #3787](https://github.com/tmux/tmux/issues/3787) - Correlating cursor position with capture-pane
- [ANSI Device Status Report](https://stackoverflow.com/questions/60134860/how-to-read-the-cursor-position-given-by-the-terminal-ansi-device-status-report)
