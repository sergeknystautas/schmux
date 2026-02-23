# Terminal Sync: Self-Correcting Desync Recovery

## Problem Statement

### Background

schmux streams terminal output from tmux sessions to browser-based xterm.js terminals via WebSocket. The pipeline is:

```
tmux control mode (%output events)
  → SessionTracker.fanOut()
    → WebSocket handler (binary frames)
      → browser xterm.js terminal.write()
```

When a WebSocket client connects, it needs an initial screen state before it can start processing incremental updates. The bootstrap sequence in `websocket.go:247-293` does this:

1. Capture the current tmux pane content via `tracker.CaptureLastLines()` (which runs `capture-pane -e -p -S -5000` over control mode)
2. Query cursor position and visibility via `tracker.GetCursorState()`
3. Append cursor restoration escape sequences (`CSI row;colH` + `DECTCEM`)
4. Send everything as a single binary WebSocket frame
5. The frontend (`terminalStream.ts:494-557`) receives this first binary frame, calls `terminal.reset()`, and writes the captured content

After bootstrap, live output from tmux control mode's `%output` events is streamed as binary WebSocket frames and written directly to xterm.js. These events contain raw terminal escape sequences — cursor movements, SGR color codes, line erases, etc.

### The race condition

The bootstrap capture is a point-in-time snapshot. tmux control mode output is a continuous stream. There is a window between "capture the screen" and "start forwarding live output" where the screen can change. The WebSocket handler mitigates this by flushing any output events that arrived during capture (`websocket.go:296-317`), but this doesn't help when the problem is the capture itself containing transient state.

TUI applications like Claude Code perform multi-step redraws that are not atomic from tmux's perspective. A typical redraw sequence might be:

1. Write a notification banner (2 lines of text)
2. Process tool results and prepare new layout
3. Erase the notification banner (`[2K][1A][2K][1A][2K]` — erase line, cursor up, repeat)
4. Write the new layout using relative cursor movements (`[6A` to reach the content area, `[1B` to step through rows)

If the bootstrap capture fires between steps 1 and 3, xterm.js starts with the notification visible. tmux continues to step 3 and beyond, sending the erase and redraw sequences. But these sequences use **relative cursor movements**, which means their correctness depends on the cursor being at the right absolute position — which in turn depends on the screen content being what the TUI expects.

### Observed failure: diagnostic capture `2026-02-22T14-31-47`

The diagnostic capture for session `schmux-003-010b7480` provides a concrete example of this failure mode. The pipeline counters confirm that no data was lost:

```json
{
  "bytesDelivered": 47975,
  "eventsDropped": 0,
  "controlModeReconnects": 0,
  "eventsDelivered": 426
}
```

Every byte that tmux produced was delivered to xterm.js. The desync is not a transport problem.

**What the bootstrap captured (ringbuffer-frontend.txt, initial screen):**

The initial screen sent to xterm.js included a 2-line Claude Code installer notification at terminal rows 31-32:

```
Row 31: "Claude Code has switched from npm to native installer. Run `claude install` or see"
Row 32: "https://docs.anthropic.com/en/docs/claude-code/getting-started for more options."
```

This notification was a transient banner displayed briefly by the Claude Code TUI. The rest of the screen (rows 1-30, 33-53) matched the expected layout: logo, user prompt, tool outputs, spinner, input prompt, and footer.

**What tmux showed at capture time (screen-tmux.txt):**

The tmux screen did **not** contain the notification. Rows 31-32 were part of the normal content layout — the spinner animation area and the input separator. tmux had already processed the escape sequences that erased the notification.

**How the desync propagated:**

After bootstrap, the TUI sent an erase sequence to remove the notification (visible in both ring buffers at approximately the same position):

```
[2K]  — erase entire current line
[1A]  — cursor up 1
[2K]  — erase entire line
[1A]  — cursor up 1
[2K]  — erase entire line
[G]   — cursor to column 1
[1A]  — cursor up 1
(3 lines of spaces)
[2A]  — cursor up 2
```

This sequence erases 3 rows upward from the cursor position (2 notification lines + 1 blank). In tmux, the cursor was at the correct row and the right lines were erased. In xterm.js, the cursor was offset by the 2 extra notification lines still on screen, so the erase hit the **wrong rows**.

After the erase, the spinner animation updates switched from `[9A` (cursor up 9) to `[6A` (cursor up 6), reflecting that the TUI's content area shrank by 3 rows. In tmux, `[6A` from the new cursor position correctly reached the spinner icon. In xterm.js, `[6A` from the wrong cursor position landed on a different row.

**Resulting visual state (screen-diff.txt):**

Every row from row 22 onward was shifted. Selected examples:

| Row | tmux (correct)                              | xterm.js (desynced)                         |
| --- | ------------------------------------------- | ------------------------------------------- |
| 22  | _(empty line)_                              | `⏺      1 file (ctrl+o to expand)`          |
| 23  | `meta:code_search(Explore test runner CLI)` | `✶ Shimmying…`                              |
| 24  | `⎿  Running PreToolUse hook…`               | `meta:code_search(Explore test runner CLI)` |
| 29  | `+6 more tool uses (ctrl+o to expand)`      | `+6 more tool uses (ctrl+o to expand)`      |
| 31  | `✶ Shimmying…`                              | _(empty)_                                   |

Row 22 is particularly revealing. The tmux screen shows an empty line, but xterm.js shows `⏺      1 file`. The 6-space gap between `⏺` and `1` is the ghost of a `[5C` (cursor forward 5) escape sequence that was designed to skip over the pre-existing word "Read" — but since the cursor landed on a blank row due to the offset, it produced spaces instead of overlapping with existing text.

The desync was **permanent** — it persisted for the lifetime of the session because no subsequent output contained absolute cursor positioning that could have re-anchored the display. All of Claude Code's TUI rendering used relative cursor movements exclusively.

### Why this class of bug is hard to prevent

The bootstrap race is inherent to the architecture. Any system that takes a snapshot and then applies incremental deltas is vulnerable to the snapshot being taken at an inopportune moment. The mitigations are either:

1. **Prevent stale snapshots** (debounce captures until output is quiescent) — impractical because Claude Code's spinner animations produce output every 50-80ms, leaving no quiescent window during active tool use
2. **Make desyncs self-correcting** — periodically verify that xterm.js matches tmux and correct if not

This design pursues option 2.

## Solution Design

### Overview

Introduce a periodic sync mechanism where the server sends a full `tmux capture-pane` snapshot to the frontend over the existing WebSocket connection. The frontend compares the snapshot against its own xterm.js buffer. On mismatch, xterm.js resets and replays the snapshot, correcting any desync. On match (the common case), no action is taken.

tmux is the single source of truth. The comparison happens in the browser. No server-side shadow terminal emulator is introduced — adding a shadow terminal would create a third rendering implementation whose correctness would itself need verification, compounding the problem rather than solving it.

### Non-goals

- Preventing desyncs from occurring (the bootstrap race is inherent to the architecture)
- Resumable streaming from byte offsets (too complex for the payoff)
- Attribute-level comparison (text content match is sufficient for detecting desyncs)

### Sync message format

The server sends a new JSON text frame over the existing terminal WebSocket:

```json
{
  "type": "sync",
  "screen": "<captured screen with ANSI escape codes>",
  "cursor": { "row": 24, "col": 3, "visible": true }
}
```

This reuses the existing text-frame control message pattern already used for `displaced`, `stats`, `controlMode`, and `diagnostic` messages. The `screen` field contains the output of `tmux capture-pane -e -p` (visible screen only, not scrollback). The `cursor` field contains the result of the existing `GetCursorState()` call.

The visible-screen-only capture is intentional. Scrollback content cannot desync — only the visible viewport is affected by relative cursor movements. Limiting to the visible screen keeps the payload small (~6KB for a 114x53 terminal).

### Server-side behavior

A new goroutine in `handleTerminalWebSocket` runs after bootstrap completes. Its logic:

1. Wait 500ms (post-bootstrap correction window)
2. Run a sync check
3. Then loop: wait 10s, run a sync check
4. Exit when the WebSocket closes

A sync check:

1. Call `tracker.CapturePane()` (new method, visible screen only — no `-S` scrollback flag)
2. Call `tracker.GetCursorState()`
3. Marshal into the `sync` JSON message
4. Send as a text frame via `ws.WriteJSON()`

The goroutine runs independently of the main event loop. The `wsConn` mutex ensures write safety. The sync check does not block or interfere with the main output forwarding loop.

### Client-side behavior

When the frontend receives a `sync` message:

1. **Activity guard**: If the terminal received any binary output data within the last 500ms, discard the sync message. The terminal is mid-update and a comparison against the captured screen would produce a false mismatch.

2. **Extract xterm.js visible text**: Read each line from `terminal.buffer.active` for the visible rows (row 0 through `terminal.rows - 1`), calling `line.translateToString(true)` and trimming trailing whitespace.

3. **Extract sync text**: Strip ANSI escape codes from the `screen` field to produce plain text lines. Trim trailing whitespace from each line.

4. **Compare**: Line-by-line string equality on the plain text.

5. **On match**: Do nothing. Log at debug level for diagnostics.

6. **On mismatch**: Call `terminal.reset()`, then `terminal.write(screen)` with the raw ANSI content (preserving colors and formatting), then apply cursor positioning and visibility. Log the mismatch at info level with the row indices that differed.

## Edge Cases and Failure Modes

### False positives (unnecessary corrections)

A false positive occurs when the comparison reports a mismatch but xterm.js is actually correct. The correction is harmless (the screen redraws to the same content) but causes a visual flicker.

**Mid-redraw comparison**: The TUI is partway through a multi-step redraw when the sync check fires. The `capture-pane` output reflects a partially-drawn screen that doesn't match xterm.js (which may be ahead or behind tmux depending on buffering). The 500ms activity guard mitigates this — if binary data arrived recently, the check is skipped. The 500ms window is conservative; Claude Code's spinner updates arrive every 50-80ms, so any active TUI rendering will suppress the check.

**Trailing whitespace differences**: tmux and xterm.js may disagree on trailing spaces in a line. Both sides trim trailing whitespace before comparison to avoid this.

**Unicode width disagreements**: Characters like `⏺`, `✶`, `▛` occupy display width that tmux and xterm.js might measure differently (particularly for ambiguous-width East Asian characters). Since `translateToString()` returns the text content (not the cell grid), and the tmux capture also produces text, these should agree in content even if they disagree on column width. The comparison is text equality, not cell-position equality, so this is a non-issue for the comparison itself. If there _is_ a width disagreement, it's an existing rendering bug independent of the sync mechanism.

**Cursor-only desync**: The screen content matches but the cursor is at the wrong position. The text comparison won't catch this. However, cursor position desyncs don't compound over time the way content desyncs do — the next TUI redraw that uses absolute cursor positioning (`CSI row;colH`) or writes to the current position will re-anchor the cursor. This is low severity and not worth adding cursor comparison complexity for.

### False negatives (missed desyncs)

A false negative occurs when the comparison reports a match but xterm.js is actually wrong. This can happen if:

**Color-only desync**: The text content matches but SGR attributes (colors, bold) are wrong. Since the comparison strips ANSI codes and compares plain text, attribute-only desyncs are invisible. This is an acceptable trade-off — color desyncs are cosmetic and don't affect the user's ability to read the terminal. A full attribute-level comparison would require parsing SGR sequences on both sides, significantly increasing complexity.

**Scrollback desync**: The visible screen matches but scrollback content differs. Since the sync check only captures the visible screen, scrollback desyncs go undetected. This is acceptable because scrollback is append-only history — it can't be corrupted by relative cursor movements, and any scrollback desync self-corrects on reconnect.

### Reconnect behavior

On WebSocket reconnect, the frontend already resets the terminal and receives a fresh bootstrap snapshot (`terminalStream.ts:505-512`). The sync goroutine for the old connection exits when the WebSocket closes. A new sync goroutine starts after the new bootstrap completes. The 500ms post-bootstrap check is particularly valuable here — reconnects are the most likely moment for a bootstrap race.

### Resize during sync

If the terminal is resized between the server capturing the screen and the frontend comparing it, the row/column counts will disagree. The frontend should check that the sync message's implied dimensions (number of lines, max line length) are compatible with the current terminal dimensions before comparing. If they differ, discard the sync message — the next periodic check will use the correct dimensions.

### Multiple tabs

When multiple browser tabs view the same session, each has its own WebSocket connection and its own sync goroutine. This is correct — each tab may have connected at a different time and may have a different desync state. The sync messages are small (~6KB) and infrequent (every 10s), so the overhead of multiple tabs is negligible.

## Testing Strategy

### Unit tests (Go)

**Sync message construction**: Test that `buildSyncMessage()` correctly marshals the capture-pane output and cursor state into the expected JSON format. Feed it known screen content with ANSI escape codes and verify the output structure.

**Timer lifecycle**: Test that the sync goroutine starts after bootstrap, fires at the expected intervals, and exits cleanly when the WebSocket connection closes or the context is cancelled. Use a `time.Ticker` that can be replaced with a channel in tests.

### Unit tests (TypeScript)

**Activity guard**: Create a `TerminalStream`, simulate binary data arriving, then immediately deliver a sync message. Verify the sync is discarded. Then wait >500ms with no binary data, deliver another sync message with mismatched content, and verify the correction fires.

**Comparison logic**: The comparison function should be extracted as a pure function (e.g., `compareScreens(xtermLines: string[], syncLines: string[]): boolean`) for easy unit testing. Test cases:

- Identical content → match
- Trailing whitespace differences → match (both sides trim)
- Row shifted by 1 line → mismatch
- Partial row difference (the `⏺      1 file` ghost) → mismatch
- Empty lines vs missing lines at the bottom → match (trailing empty rows are equivalent)
- Different number of rows (resize race) → skip comparison

**ANSI stripping**: Test the function that strips SGR codes from the sync payload. Verify it handles nested codes, 24-bit color (`[38;2;R;G;Bm`), bold/reset (`[1m`/`[0m`), and leaves plain text intact.

**Correction**: Mock an xterm.js terminal, deliver a sync message with a known mismatch, verify that `terminal.reset()` and `terminal.write()` are called with the raw ANSI content from the sync message.

### Integration test

Add a scenario test that reproduces the bootstrap race:

1. Start a tmux session
2. Write content that includes a transient element (write a line, wait briefly, erase it)
3. Connect a WebSocket client during the transient window
4. Verify that after the first sync check (500ms), the xterm.js content matches tmux

This is inherently timing-sensitive and may be flaky in CI. An alternative is a deterministic variant: manually inject a wrong bootstrap state into the frontend, then verify the sync mechanism corrects it within one check cycle.

### Manual verification

Use the existing diagnostic capture infrastructure. After implementing the sync mechanism, trigger a diagnostic capture and verify:

- `screen-diff.txt` shows 0 differing rows
- `meta.json` automated findings show "No drops detected" and no row mismatches
- The ring buffers show sync messages being sent and (if applicable) corrections being applied

Add a dev-mode log entry when a sync correction fires, including which rows differed. This makes it easy to spot in the dashboard's stats overlay whether corrections are happening and how often.

## Rollout and Observability

### Observability

Add three counters to the existing `PipelineCounters` struct in the WebSocket handler:

- `syncChecksSent`: Total sync messages sent to the frontend
- `syncCorrections`: Number of times the frontend reported applying a correction
- `syncSkippedActive`: Number of times the frontend discarded a sync due to the activity guard

The frontend reports corrections back to the server via a new `syncResult` message type:

```json
{ "type": "syncResult", "corrected": true, "diffRows": [22, 23, 24, 25] }
```

These counters appear in the existing dev-mode stats overlay (`WSStatsMessage`) and in diagnostic captures (`meta.json`), making it easy to monitor the feature's behavior without adding new infrastructure.

### Rollout

The sync mechanism is additive — it sends a new message type that old frontends will ignore (the `handleOutput` text frame handler has a default case that drops unknown types). No feature flag is needed. If the sync messages cause unexpected issues, the sync goroutine can be disabled by setting the interval to 0.

### Performance budget

Per session per sync check:

- Server: one `capture-pane` control mode command (~1ms), one JSON marshal (~0.1ms)
- Network: ~6KB text frame over localhost WebSocket
- Client: 53 `translateToString()` calls + 53 string comparisons (~0.5ms), no DOM work unless correcting

At the default 10s interval with 10 sessions: 10 capture-pane calls every 10s, 60KB/10s = 6KB/s network. Negligible on all axes.
