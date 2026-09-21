# Chat Tool Rows: Compact Summaries, Whole-Block Toggle

**Status:** v1 — approved design (2026-09-20).

## Problem

Tool call rows in the chat transcript (`ToolCallRow.tsx`) are hostile to
scanning in two ways.

**Collapsed rows aren't collapsed.** For a Bash call, `summarizeTool` returns
the entire `input.command`, and `.toolSummary` (`word-break: break-all`) lets
it wrap — a 5-line command renders as a 5-line row before you've decided
whether you care. A single-line 400-character command wraps too.

**Expanded blocks are hard to put away.** Only `.toolRow` carries the click
handler. Once a block is expanded — raw input JSON plus full result, often
dozens of wrapped lines — collapsing requires clicking that one header row,
which may have scrolled off screen. Clicking the JSON or result text does
nothing.

## Goals

- A collapsed tool row is always exactly one visual line: the first line of
  the summary, with a visible marker when content was elided. The gray result
  first-line (`.toolResult`) renders only when the block is expanded.
- Clicking anywhere in an expanded tool block collapses it, without breaking
  text selection/copy from the details.
- A collapsed row sits in the muted text tier (`--color-text-muted`, like
  "Thinking…"); expanding restores the default contrast on the command while
  the details stay darker. The state label keeps `--color-text-faint` and the
  dot keeps its state color, so status stays legible when collapsed.

## Non-goals

- **Expanded content rendering** — input JSON stays raw (not pretty-printed),
  details grow to full content height. Explicitly reviewed and left as-is.
- **`summarizeTool` behavior** — it keeps returning the full string;
  `PermissionCard` consumes it and wants the full text.
- **Codex title generation** — `summarizeToolInput` in `lib/chat/codex.ts`
  bakes titles upstream; the render-layer fix below covers whatever string
  reaches the row.

## Design

### One-line summaries

In `ToolCallRow.tsx`, the row renders only the first line of
`summarizeTool(tool)`. When the full summary contains a newline (more lines
exist), a muted `…` marker is appended after the first line so elided content
is distinguishable from a genuinely short command.

In `chat.module.css`, `.toolSummary` changes from `word-break: break-all` to
`white-space: nowrap; overflow: hidden; text-overflow: ellipsis`. This clips
long single-line commands to one visual line with the CSS ellipsis, and works
in combination with the `min-width: 0` flex behavior already in place.

The two mechanisms stack: JS handles vertical elision (extra lines), CSS
handles horizontal elision (overlong first line).

The result first-line (`.toolResult`) is gated on `expanded`: it and the
details appear only in the expanded state, so the collapsed block is exactly
one line.

### Whole-block toggle, selection-safe

Move the `onClick` toggle from `.toolRow` to the outer `.tool` container so
the expanded details (input JSON, result, subtools) are clickable and collapse
the block.

Selection guard in the click handler: if `window.getSelection()` exists and
is not collapsed (`!selection.isCollapsed`), the click was part of a
text-selection gesture — skip the toggle. This preserves copy-from-details.

Keyboard interaction is unchanged: `.toolRow` keeps `role="button"`,
`tabIndex={0}`, and the Enter/Space handler, so focus and screen-reader
behavior stay on the header row.

## Testing

Extend `assets/dashboard/src/components/chat/ToolCallRow.test.tsx`:

- Multi-line command: row shows first line plus the elision marker; remaining
  lines do not appear in the row.
- Single-line command: no elision marker.
- Clicking inside the expanded details collapses the block.
- Clicking with a non-collapsed `window.getSelection()` (mocked) does not
  toggle.

## Files

- `assets/dashboard/src/components/chat/ToolCallRow.tsx`
- `assets/dashboard/src/components/chat/chat.module.css` (`.toolSummary`,
  cursor affordance on expanded block)
- `assets/dashboard/src/components/chat/ToolCallRow.test.tsx`
