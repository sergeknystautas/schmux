# File-Based Agent Signaling

**Date**: 2026-02-14
**Status**: Design

## Problem

Agent-to-schmux communication currently works by parsing terminal output for
`--<[schmux:state:message]>--` markers. This is fragile because:

1. **Terminal redraws** cause the same marker to be re-read, requiring 4 layers
   of deduplication (backend signal comparison, NudgeSeq counter, frontend
   localStorage acks, viewed-sessions buffer).
2. **ANSI stripping** requires a full escape-sequence state machine to clean
   terminal output before regex matching.
3. **Regex matching** uses two patterns (strict line-anchored + loose fallback)
   to handle rendering artifacts that merge visual lines.
4. **Daemon restart recovery** requires parsing 200 lines of scrollback with
   suppressed callbacks, then diffing against stored state.

The root cause: terminal output is a display medium, not a communication
channel. Redraws, ANSI sequences, line wrapping, and partial reads all create
ambiguity.

## Solution

Replace in-band terminal markers with **file-based signaling**. Agents write
status to a well-known file; schmux watches the file for changes.

## Protocol

### File Location

```
$WORKSPACE/.schmux/signal/<session-id>
```

The path is provided to agents via the `SCHMUX_STATUS_FILE` environment
variable (alongside existing `SCHMUX_ENABLED`, `SCHMUX_SESSION_ID`).

### File Format

Single line, plain text:

```
STATE MESSAGE
```

- `STATE`: one of `completed`, `needs_input`, `needs_testing`, `error`, `working`
- `MESSAGE`: everything after the first space (optional, can be empty)

Examples:

```
completed Implemented the login feature
needs_input Should I delete these 5 files?
error Build failed - missing dependency
working
```

### Why Plain Text

- LLMs are more reliable writing `echo "completed Done" > file` than valid JSON
- Parsing is `strings.SplitN(line, " ", 2)`
- No quoting or escaping issues

### Agent Instructions

Replace the current ~40-line signaling instructions with:

```
When you need to signal your status to schmux, write to the file at
$SCHMUX_STATUS_FILE (set in your environment). Write a single line:

    echo "STATE message" > $SCHMUX_STATUS_FILE

Valid states: completed, needs_input, needs_testing, error, working
```

## Architecture

### Local Sessions: Go fsnotify

```
Agent (in tmux) --writes--> $WORKSPACE/.schmux/signal/<session-id>
                                    |
                        fsnotify watcher (Go goroutine)
                                    |
                        signalCallback(sessionID, signal)
                                    |
                        HandleAgentSignal (existing path)
```

- One `fsnotify` watcher per session, watching `$WORKSPACE/.schmux/signal/<session-id>`
- On `fsnotify.Write` event: read file, parse `STATE MESSAGE`
- Compare against last-read content (string comparison)
- If different: invoke signal callback
- No ANSI stripping, no regex, no multi-layer dedup

**Daemon restart**: read the file. That's it. The file IS the state. No
scrollback parsing needed.

### Remote Sessions: Persistent Watcher Pane

For remote sessions, a persistent hidden tmux pane watches the status file and
emits changes. Schmux reads those changes via `%output` events over the
existing control mode connection.

```
Agent writes $WORKSPACE/.schmux/signal/<session-id>
    |
Watcher pane detects change      (inotifywait or polling)
    |
Watcher emits sentinel-wrapped output
    |
%output event to schmux          (push, via existing control mode)
    |
schmux parses clean text         (sentinel check, no ANSI stripping)
```

#### Watcher Script

```sh
#!/bin/sh
STATUS_FILE="$1"
LAST=""
check() {
  if [ -f "$STATUS_FILE" ]; then
    CURRENT=$(cat "$STATUS_FILE" 2>/dev/null)
    if [ "$CURRENT" != "$LAST" ]; then
      LAST="$CURRENT"
      echo "__SCHMUX_SIGNAL__${CURRENT}__END__"
    fi
  fi
}

# Try inotifywait first (Linux), fall back to polling
if command -v inotifywait >/dev/null 2>&1; then
  check  # initial check
  while inotifywait -qq -e modify -e create "$STATUS_FILE" 2>/dev/null; do
    sleep 0.1  # debounce
    check
  done
else
  # Fallback: poll every 2 seconds
  while true; do
    check
    sleep 2
  done
fi
```

Key design choices:

- **Shell-side dedup**: `$LAST` comparison means only actual changes produce output
- **Sentinel framing**: `__SCHMUX_SIGNAL__...__END__` makes parsing unambiguous
- **No ANSI codes**: non-interactive script, clean text output
- **Graceful degradation**: inotifywait for instant notification, polling fallback

#### Schmux Side (Go)

At spawn time:

1. Create hidden window: `new-window -d -n schmux-signal-{sessionID}`
2. Type the watcher script into it (via send-keys)
3. Subscribe to `%output` events for the watcher pane
4. On `%output` containing `__SCHMUX_SIGNAL__`: parse, invoke signalCallback

At dispose time:

1. Kill the watcher window
2. Unsubscribe from output

### Comparison with Current System

| Aspect              | Current (terminal parsing)    | New (file-based) |
| ------------------- | ----------------------------- | ---------------- |
| ANSI stripping      | Full state machine            | None needed      |
| Dedup               | 4-layer system                | String compare   |
| Regex matching      | Two patterns (strict + loose) | Sentinel check   |
| Near-miss detection | Regex fallback + logging      | N/A (structured) |
| Daemon restart      | 200-line scrollback parse     | Read the file    |
| Terminal redraws    | Source of all fragility       | Don't exist      |
| Agent instructions  | ~40 lines                     | ~5 lines         |

## Error Handling

### Watcher pane dies (remote)

- Detect via tmux `%window-close` async event (already parsed)
- Recreate watcher pane and re-subscribe
- Watcher script wraps in restart loop for resilience

### File doesn't exist yet

- `inotifywait -e create` watches for file creation
- Polling fallback: `[ -f "$STATUS_FILE" ]` check
- Local fsnotify: watch the directory, filter for filename

### SSH connection drops and reconnects

- On reconnection, watcher panes are re-created during session recovery
- Current file content is read immediately (no scrollback needed)

### Agent writes garbage

- Invalid state string: ignore, log warning
- Valid state with no message: status-only update
- Empty file: ignore (truncated mid-write)

### Multiple rapid writes

- `inotifywait` debounce (100ms sleep) coalesces changes
- `fsnotify` debounce in Go
- Even without debounce, reads are idempotent (string compare dedup)

### Workspace directory read-only

- `.schmux/` created by schmux at provision time (already happens)
- If read-only: agent can't signal, NudgeNik fallback still works

## Migration

**Hard cutover**: replace the terminal signal detector with file-based signaling.

1. Implement `FileWatcher` for local sessions
2. Implement persistent watcher pane for remote sessions
3. Update `SignalingInstructions` to use file-write instructions
4. Remove `SignalDetector`, ANSI stripper, dual regex matching
5. Remove scrollback recovery logic
6. Remove multi-layer dedup (backend signal comparison becomes string compare
   on file content)

### What stays unchanged

- NudgeNik (LLM polling for agents that don't signal at all)
- Nudge clearing on user input
- Frontend notification/sound system
- `NudgeSeq` counter and localStorage acks
- WebSocket broadcast debouncing
- `HandleAgentSignal` callback interface

### What gets removed

- `internal/signal/detector.go` (line buffering, flush tickers, dedup state)
- `internal/signal/signal.go` ANSI stripping state machine
- Dual regex patterns (strict + loose)
- Near-miss detection heuristics
- Scrollback recovery in `tracker.go` and `manager.go`
- Small-chunk optimization in tracker

## Future Work (Not In Scope)

### Schmux-to-Agent Communication (Reverse Channel)

A `$WORKSPACE/.schmux/inbox` file for schmux-to-agent messages (abort, context
injection). Deferred because most agents lack a mechanism to poll files
mid-conversation. Claude Code hooks could enable this for Claude specifically,
but agent-agnostic support requires harnesses to add file-watching hooks.

### Richer Signal Payloads

The file format could evolve to support structured data (progress percentage,
sub-task status) while remaining backward compatible by keeping STATE on the
first line.
