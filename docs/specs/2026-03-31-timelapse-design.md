# Timelapse — Record and Replay Agent Sessions Without the Dead Time

## Problem

Agent sessions are compelling to watch but take too long to consume. A 30-minute coding session might have 5 minutes of interesting content — code being written, commands run, results displayed — and 25 minutes of spinners, LLM thinking pauses, and progress indicators. There's no way to share a condensed version of what the agent did.

The goal is tutorial and teaching content: someone records an agent session, exports a compressed version that strips all the filler, and shares it for others to watch at their own pace.

## Architecture

Three-stage pipeline, all within schmux. The output is a standard asciicast v2 `.cast` file playable by any existing player (asciinema player, `asciinema play` CLI, third-party web players).

```
RECORD (always-on)           EXPORT (on-demand)              OUTPUT
──────────────────           ─────────────────────────       ──────

SessionTracker.OutputLog     Read recording.jsonl            .cast file
    │                            │                           (asciicast v2)
    └→ Recorder polls            ▼                               │
       OutputLog via         Feed bytes into in-memory           ▼
       sequence numbers:     VT100 emulator, snapshot        Any asciicast
       {type, t, seq, d}    every 500ms                     player
       {type, t, seq, d}        │
       ...                      ▼
                             Screen-diff compression
  Cost: one buffered         (text-only cell comparison)
  file write per                 │
  output event.                  ▼
  No tmux calls.             Drop filler bytes,
  No snapshots.              inject keyframes,
  No processing.             rewrite timestamps

                             Cost: milliseconds (in-memory),
                             only when user asks.
```

### Prerequisite: ControlSource Unification

Local and remote sessions currently have divergent streaming paths. Local sessions flow through `SessionTracker` (with OutputLog, sequencing, gap detection, diagnostics). Remote sessions bypass the tracker entirely, subscribing directly to `remote.Connection` with no OutputLog, no sequencing, and a different wire format.

Before timelapse recording can work for all sessions, the streaming architecture must be unified. `SessionTracker` will be refactored to consume output from a pluggable `ControlSource` interface:

```go
type SourceEventType int

const (
    SourceOutput SourceEventType = iota
    SourceGap
    SourceResize
    SourceClosed
)

type SourceEvent struct {
    Type     SourceEventType
    Data     []byte  // Output: raw terminal bytes
    Reason   string  // Gap: why it happened
    Snapshot string  // Gap: capture-pane on reconnect
    Width    int     // Resize
    Height   int     // Resize
    Err      error   // Closed: nil = clean, non-nil = permanent failure
}

type ControlSource interface {
    Events() <-chan SourceEvent
    SendKeys(keys string) error
    CapturePane(opts CaptureOpts) (string, error)
    GetCursorState() (CursorState, error)
    Close() error
}
```

```
                    ControlSource interface
                    Events() <-chan SourceEvent
                    SendKeys(keys) error
                    CapturePane(opts) (string, error)
                    GetCursorState() (CursorState, error)
                    Close() error
                          ▲                 ▲
                          │                 │
                    LocalSource         RemoteSource
                    (tmux -C attach,    (remote.Connection,
                     reconnection)       SSH lifecycle)
                          │                 │
                          └────────┬────────┘
                                   │
                             SessionTracker
                             (OutputLog, fan-out, sequencing,
                              diagnostics, recording)
                                   │
                          ┌────────┼────────┐
                          │        │        │
                       WebSocket  Recorder  Diagnostics
```

Key design choices for `ControlSource`:

- **No `paneID` on any method.** The source is configured with its target pane at construction. Keeps the interface simple — the tracker is single-pane-per-session anyway.
- **Discriminated union on a single channel.** `Events()` returns one channel carrying `Output`, `Gap`, `Resize`, and `Closed` events. One channel to drain, no `select` across multiples. The event types map 1:1 to the recording format's record types.
- **Source owns reconnection.** `LocalSource` contains the retry loop (currently in `tracker.go` lines 362–394). `RemoteSource` delegates to `Connection`'s reconnection. When reconnection happens, the source emits a `Gap` event (with a `capture-pane` snapshot) and continues sending `Output` events. The tracker never retries — it just drains the channel.
- **Channel closure = permanent stop.** A `Closed` event with `Err == nil` means clean shutdown. `Err != nil` means permanent failure (e.g., session no longer exists). Temporary failures (reconnections) are the source's problem.

This means the current reconnection loop in `tracker.go` moves into `LocalSource`. The tracker becomes simpler — it just drains events, feeds OutputLog, and fans out.

Everything downstream of "here's a `SourceEvent`" is identical for local and remote: sequencing, OutputLog, fan-out, WebSocket handler, and recording. The `handleRemoteTerminalWebSocket` code path collapses into `handleTerminalWebSocket`.

## Recording (Always-On)

Every session is recorded automatically. The recorder reads from the session tracker's `OutputLog` — a sequenced, non-dropping circular buffer — rather than subscribing to the lossy fan-out channel. This guarantees no silent data loss: if disk I/O stalls briefly, the recorder catches up by replaying missed entries via sequence number.

OutputLog needs a small addition: a notification mechanism (e.g., `sync.Cond` or channel signal) so the recorder doesn't busy-poll.

**Buffer overrun detection**: OutputLog is a 50,000-entry circular buffer. If the recorder stalls long enough for the buffer to wrap, entries are lost. After each `ReplayFrom()`, the recorder checks whether `OldestSeq()` has advanced past its last-read sequence. If so, the data is gone — write a gap record with `"reason": "buffer_overrun"`. At normal output rates (1–5 MB/hour), this is a non-issue. During error loops, the 50 MB per-session cap stops recording before sustained buffer pressure becomes a problem.

**First-run notice**: On the first daemon start with recording enabled, emit a one-time notice: _"Timelapse recording is enabled. Terminal output is saved to ~/.schmux/recordings/. Run `schmux config` to disable."_ This avoids surprising users who haven't read the config docs.

### Recording format

Typed NDJSON records with explicit schemas. Once recording ships, this format is a long-lived compatibility contract.

```json
{"type": "header", "version": 1, "recordingId": "abc123-1711875300", "sessionId": "abc123", "width": 120, "height": 40, "startTime": "2026-03-31T10:15:00Z"}
{"type": "output", "t": 0.000, "seq": 1, "d": "$ claude \"build a REST API\"\r\n"}
{"type": "output", "t": 0.512, "seq": 2, "d": "Loading...\r\n"}
{"type": "resize", "t": 15.300, "width": 200, "height": 50}
{"type": "gap", "t": 45.200, "reason": "control_mode_reconnect", "lostSeqs": [4521, 4530], "snapshot": "<full screen state as ANSI>"}
{"type": "gap", "t": 67.100, "reason": "buffer_overrun", "lostSeqs": [5000, 5042], "snapshot": null}
{"type": "output", "t": 45.800, "seq": 4531, "d": "...resumed output..."}
{"type": "end", "t": 342.500}
```

Record types:

| Type     | Fields                                                    | Purpose                                                                                                                                                |
| -------- | --------------------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------ |
| `header` | version, recordingId, sessionId, width, height, startTime | Format version and session metadata                                                                                                                    |
| `output` | t, seq, d                                                 | Terminal output with timestamp and sequence number                                                                                                     |
| `resize` | t, width, height                                          | Terminal dimensions changed                                                                                                                            |
| `gap`    | t, reason, lostSeqs, snapshot                             | Data loss event. `reason` is `"control_mode_reconnect"` or `"buffer_overrun"`. `snapshot` is a `capture-pane` on reconnect (null for buffer overruns). |
| `end`    | t                                                         | Recording finished (session disposed or daemon stopped)                                                                                                |

The `recordingId` is `<sessionId>-<unixTimestamp>`, formalizing the filename as a durable key. Recordings carry enough metadata (workspace, agent, timestamps) to stand on their own without cross-referencing the sessions list.

Output payload encoding: raw bytes are UTF-8 encoded in JSON. Arbitrary control bytes are escaped per JSON spec. Malformed UTF-8 sequences are replaced with U+FFFD.

### Gap handling

Two types of gaps, same record format, different recovery:

**Control mode reconnect**: The ControlSource emits a `Gap` event with a `capture-pane` snapshot. The recorder writes a gap record with the snapshot. At export time, the VT100 emulator is re-synced from this snapshot via keyframe injection.

**Buffer overrun**: The recorder detects that `OldestSeq()` has advanced past its last-read sequence. No snapshot is available (the overrun happened silently). The recorder writes a gap record with `"snapshot": null`. At export time, the exporter continues with whatever screen state the emulator has — the gap may produce rendering artifacts, honestly marked in the recording.

### Storage

```
~/.schmux/recordings/
  <sessionId>-<timestamp>.jsonl
```

One file per session lifecycle. If a session is disposed and re-spawned, each run gets its own recording file.

**Limits**:

- Per-session file size: 50 MB hard cap. When hit, recording stops for that session.
- Total storage budget: 500 MB across all recordings. When hit, oldest recordings are evicted first.
- Age-based pruning: recordings older than `retentionDays` (default 7) are deleted.
- File permissions: `0600` (owner read/write only).

### Performance budget

Recording adds one buffered file write per output event. No tmux subprocess calls, no screen captures, no data processing. The overhead is negligible compared to the tmux control mode parsing already happening.

A busy agent session produces roughly 1–5 MB/hour of raw terminal output under normal conditions. Runaway sessions (error loops) hit the 50 MB cap and stop recording.

### Lifecycle

- Recording starts when the session tracker attaches to its ControlSource
- Recording stops when the session is disposed, the daemon shuts down, or the 50 MB cap is hit
- Resize events are recorded when the ControlSource emits `SourceResize` events
- On control mode reconnect (ControlSource emits `SourceGap`), a gap record with screen snapshot is written, then recording resumes
- On buffer overrun (detected by recorder), a gap record without snapshot is written, then recording continues

### New package

```
internal/timelapse/
  recorder.go      // Recorder — reads OutputLog, writes NDJSON
  exporter.go      // Exporter — VT100 replay, snapshot, compress, write .cast
  compression.go   // screen-diff classifier (text-only cell comparison)
  types.go         // RecordingEntry, typed record schemas, CompressionMap
```

New dependency: a Go VT100 terminal emulator library for the export pipeline (selected during Phase 1 spike).

## Export Pipeline

Export runs on-demand when the user requests it. It reads the raw recording file, replays it through an in-memory VT100 emulator, classifies intervals via screen diffing, and writes a compressed `.cast` file.

**Export of active recordings**: Export can be requested for a still-running session (no `end` record in the file). The exporter processes up to EOF and treats the absence of an `end` record as "recording in progress." Duration is computed from the last event's timestamp.

### Step 1 — Replay and classify (single pass)

Feed the recorded byte stream into the VT100 emulator. Keep exactly one snapshot in memory; compare it to the next, classify the interval immediately, discard the old snapshot. This produces a compression map (list of content/filler intervals) with minimal memory usage.

```
prev_snapshot = nil
compression_map = []

For each record in recording.jsonl:
    if type == "output":
        emulator.Write(d)
        if t - lastSnapshotTime >= 500ms:
            curr_snapshot = emulator.CellText()  // text only, no styles
            if prev_snapshot != nil:
                if curr_snapshot == prev_snapshot:
                    compression_map.append(FILLER, lastSnapshotTime, t)
                else:
                    compression_map.append(CONTENT, lastSnapshotTime, t)
            prev_snapshot = curr_snapshot
            lastSnapshotTime = t

    if type == "resize":
        emulator.Resize(width, height)

    if type == "gap" and snapshot != null:
        emulator.Reset()
        emulator.WriteKeyframe(snapshot)
```

Adjacent intervals of the same type merge.

**Why an in-memory emulator, not tmux replay**: Snapshots are only used for interval classification (content vs. filler), not for the final `.cast` output. The `.cast` file contains the original raw bytes regardless. A Go VT100 library that gets an obscure edge case slightly wrong still correctly identifies "screen changed a lot" vs. "screen didn't change" — which is all the classifier needs. The in-memory approach is synchronous by construction (no FIFO sync issues), requires no subprocess calls, no tmux server isolation, and runs in milliseconds rather than minutes.

### Step 2 — Write `.cast` file (second pass)

Walk the recording file again with the compression map:

**CONTENT intervals**: Keep original `output` bytes with preserved relative timing.

**FILLER intervals**: Drop all original `output` bytes. Inject one keyframe — the full screen state at the end of the filler interval (captured from the emulator during pass 1, or re-derived in pass 2). Set timestamp to a 300ms pause so the viewer perceives a beat — "the agent thought here."

**Why text-only comparison in classification**: The VT100 emulator maintains a grid of cells (character + attributes). Diffing on cell text only avoids overfitting to cosmetic formatting churn (color changes, style resets) that doesn't represent meaningful content changes.

**Why keyframe injection instead of time-only compression**: Collapsing 30 seconds of spinner redraws to 300ms but keeping all events produces a visual seizure (hundreds of redraws in 300ms). Dropping filler bytes and injecting a clean screen state eliminates event volume while preserving terminal state.

### Step 3 — Write `.cast` header and output

Output is asciicast v2 format (NDJSON). Stock players play it as-is. Extended metadata in the header enables future custom players to show compression stats.

```json
{"version": 2, "width": 120, "height": 40, "duration": 48.2, "title": "Building a REST API with Claude Code", "env": {"TERM": "xterm-256color"}, "x-schmux": {"originalDuration": 342.5, "compressionRatio": 7.1, "recordingId": "abc123-1711875300", "agent": "claude-code", "fillerStrategy": "idle-v1"}}
[0.000, "o", "$ claude \"build a REST API\"\r\n"]
[0.500, "o", "\u001b[1mI'll create the project structure...\u001b[0m\r\n"]
[2.100, "o", "\u001b[2J\u001b[H<keyframe: full screen state>"]
[2.400, "o", "I'll create a function that parses...\r\n"]
```

Standard players ignore `x-schmux`. The typed recording format supports adding an `event` record type later for chapter annotations (e.g., from schmux's structured events in `.schmux/events/*.jsonl`), but this is deferred from v1.

**asciicast v2 resize limitation**: asciicast v2 specifies terminal dimensions once in the header. If the session resizes mid-stream, content from larger dimensions may be clipped or wrapped in the player; content from smaller dimensions may have dead space. V1 uses the initial session dimensions in the header and accepts this limitation. Most agent sessions don't resize. This is a two-way door — future versions could split exports at resize boundaries or use the maximum dimensions.

**Export progress**: For long recordings, progress events are broadcast over `/ws/dashboard` (consistent with existing schmux async patterns).

## API

| Method   | Endpoint                                | Purpose                                                                                                 |
| -------- | --------------------------------------- | ------------------------------------------------------------------------------------------------------- |
| `GET`    | `/api/timelapse`                        | List recordings (recording ID, session ID, workspace name, agent type, start time, duration, file size) |
| `POST`   | `/api/timelapse/{recordingId}/export`   | Trigger export, returns `202 Accepted`. Progress via WebSocket.                                         |
| `GET`    | `/api/timelapse/{recordingId}/download` | Download the exported `.cast` file (available once export reaches `completed`)                          |
| `DELETE` | `/api/timelapse/{recordingId}`          | Delete a recording and its export                                                                       |

Export states: `queued` → `running` → `completed` | `failed` | `degraded` (e.g., gaps present).

## CLI

```
schmux timelapse list                                    # list recordings
schmux timelapse export <recording-id> [-o file.cast]    # export to .cast
schmux timelapse delete <recording-id>                   # delete recording
```

**CLI does not require a running daemon.** `timelapse list` reads `~/.schmux/recordings/` directly. `timelapse export` is pure file processing (read recording, run VT100 emulator, write `.cast`). `timelapse delete` removes files. The API endpoints require the daemon, but the CLI works offline. This matches existing CLI behavior where `start`, `stop`, and `status` work without a running daemon.

## Dashboard UI

Minimal surface area:

- **Session detail page**: "Export Timelapse" button (visible when a recording exists for the session). Shows progress indicator and download link when export completes.
- **Recordings list**: accessible from settings or a sidebar link, shows all recordings with workspace name, agent type, duration, and export/delete actions.

## Configuration

```json
// ~/.schmux/config.json
{
  "timelapse": {
    "enabled": true,
    "retentionDays": 7,
    "maxFileSizeMB": 50,
    "maxTotalStorageMB": 500
  }
}
```

All fields optional with sensible defaults. `enabled` defaults to `true` — recording is on unless explicitly disabled.

Compression parameters (filler pause duration, snapshot interval) are hardcoded for v1. No config surface until the heuristics are validated against real recordings.

## Privacy

Always-on recording captures all terminal output, which may include prompts, credentials, tokens, or sensitive data.

Mitigations:

- **First-run notice**: one-time daemon log on first start with recording enabled
- **File permissions**: `0600` (owner read/write only)
- **Retention**: auto-pruning by age (default 7 days) and total storage budget (default 500 MB)
- **Opt-out**: `"enabled": false` in config disables recording entirely
- **Export warning**: export endpoint and CLI emit a notice that recordings may contain sensitive content
- **No scrubbing**: automated credential detection is unreliable. The user is responsible for reviewing exports before sharing.

## Key Decisions

| Decision                                         | Choice                                               | Rationale                                                                                                                                                                                                                                                      |
| ------------------------------------------------ | ---------------------------------------------------- | -------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| Always-on vs. opt-in recording                   | Always-on                                            | Recording is cheap (file writes only). Removes friction — users don't need to remember to start recording before a good session.                                                                                                                               |
| Recording source: fan-out vs. OutputLog          | OutputLog                                            | Fan-out channels have drop-on-full semantics — correct for live UI, fatal for archival recording. OutputLog is sequenced and non-dropping. Sequence numbers enable gap detection.                                                                              |
| ControlSource event model                        | Discriminated union on single channel                | One channel carrying Output/Gap/Resize/Closed events. Maps 1:1 to the recording format. No paneID on methods — configured at construction. Source owns reconnection, tracker just drains.                                                                      |
| Snapshots at record time vs. export time         | Export time                                          | Eliminates capture-pane overhead during recording. All CPU cost is deferred to the explicit export action.                                                                                                                                                     |
| Export replay: tmux vs. in-memory VT100 emulator | In-memory VT100                                      | Snapshots are only used for interval classification, not the final output. The `.cast` file uses original raw bytes regardless. In-memory approach is synchronous (no FIFO sync issues), runs in milliseconds, and eliminates subprocess/isolation complexity. |
| Export memory model                              | Two-pass with one snapshot in memory                 | Pass 1: classify intervals (one snapshot in memory at a time, compression map is tiny). Pass 2: write `.cast` using the compression map. No temp storage I/O. Sufficient for idle-only compression.                                                            |
| Screen-diff comparison                           | Text-only cells                                      | Ignore color/style attributes. Avoids overfitting to cosmetic formatting churn.                                                                                                                                                                                |
| V1 compression scope                             | Idle-only (zero-change snapshots)                    | Spinner detection heuristics have high false positive/negative risk without real data to tune against. Idle compression covers the dominant dead time source (LLM thinking pauses). Two-way door — the classifier is an internal intermediate.                 |
| Filler event handling                            | Drop bytes + inject keyframe                         | Time-only compression of filler produces visual seizures (hundreds of spinner redraws in 300ms). Keyframe injection eliminates event volume while preserving terminal state.                                                                                   |
| Gap handling                                     | capture-pane on reconnect + buffer overrun detection | Two gap types: reconnect (has snapshot for re-sync) and buffer overrun (no snapshot, continues with current emulator state). Both are honestly recorded.                                                                                                       |
| Resize in recording                              | Supported via VT100 emulator resize                  | Ignoring resizes breaks spatial classification heuristics. In-memory emulator makes resize support nearly free (`emulator.Resize(cols, rows)`).                                                                                                                |
| Resize in `.cast` output                         | Initial dimensions in header (v1 limitation)         | asciicast v2 declares dimensions once. Mid-stream resizes may cause clipping or dead space in players. Acceptable for v1 — most agent sessions don't resize. Two-way door.                                                                                     |
| Custom format vs. asciicast v2                   | asciicast v2                                         | Ecosystem of existing players. No need to build or host a player. Extended metadata via `x-schmux` header field for future custom players.                                                                                                                     |
| Compression in export vs. in player              | In export                                            | Produces a self-contained `.cast` file that plays correctly in any stock player. No custom player required.                                                                                                                                                    |
| Local vs. remote session support                 | Unified via ControlSource                            | Local and remote sessions must share the exact same streaming pipeline. ControlSource interface at the input boundary of SessionTracker achieves this without rewiring existing code.                                                                          |
| Recording key                                    | recordingId (`<sessionId>-<timestamp>`)              | Sessions are runtime entities. Recordings are historical artifacts. Decoupled key with embedded metadata for standalone listing.                                                                                                                               |
| Export async model                               | 202 + WebSocket progress                             | Consistent with existing schmux async patterns. Export may take seconds for long recordings.                                                                                                                                                                   |
| CLI daemon requirement                           | Not required                                         | Recording files are a simple filesystem structure. `list`, `export`, and `delete` operate directly on `~/.schmux/recordings/`. API endpoints require the daemon.                                                                                               |

## Resolved Questions

1. **Multi-pane sessions**: Not supported. Schmux is one-pane-per-session today. If multi-pane is ever added, the recording format can be versioned.

2. **Export duration for long sessions**: Ship with a fixed 500ms snapshot interval. Benchmark against real recordings. Optimize (e.g., adaptive intervals) only if export times are actually painful.

3. **Filler detection tuning**: Ship with idle-only compression. No config surface yet. Add spinner detection later once real recordings exist to test against.

4. **Remote tmux version**: Store `null` for now. Add version detection when fidelity issues are reported.

5. **Chapter annotations**: The typed recording format supports adding an `event` record type later. Deferred from v1.

6. **VT100 emulator library selection**: Phase 1 includes a spike to evaluate candidate libraries against real recordings. The library must handle cursor movement, screen/line clear, scrolling regions, alternate screen buffer (agents use editors), and UTF-8/wide characters. Perfect fidelity isn't required — classification only needs "changed vs. not changed" — but alternate screen buffer support is critical (without it, every editor open/close looks like a massive content change and wrecks the classifier).

## Implementation Phases

```
Phase 0 — ControlSource Unification
    1. Define ControlSource interface and SourceEvent types
    2. Extract attachControlMode + reconnection into LocalSource
    3. Wrap remote.Connection in RemoteSource
    4. SessionTracker takes ControlSource at construction
    5. Gap signaling: LocalSource emits Gap events on reconnect
       with capture-pane snapshot
    6. Resize signaling: source emits Resize events on dimension change
    7. Collapse handleRemoteTerminalWebSocket into handleTerminalWebSocket

Phase 1 — Recording
    1. OutputLog notification mechanism (sync.Cond or channel signal)
    2. Typed NDJSON recording format + buffer overrun gap detection
    3. Recorder reads from OutputLog, writes to disk
    4. Resize event recording (from ControlSource Resize events)
    5. Gap records from ControlSource Gap events (with snapshot)
    6. Per-session size limits (50 MB) + total storage budget (500 MB)
    7. Age-based pruning (7-day default)
    8. File permissions (0600) + first-run notice
    9. CLI: schmux timelapse list (reads filesystem directly)
   10. VT100 emulator library spike: evaluate candidates against
       real recordings for alternate screen, scrolling, wide chars

Phase 2 — Export
    1. Integrate selected VT100 emulator library
    2. Two-pass export: classify (one snapshot in memory) → write .cast
    3. Idle-only compression with keyframe injection
    4. Text-only screen diffing
    5. Resize support (emulator.Resize at correct timestamps)
    6. Gap re-sync via keyframe injection (from snapshot in gap record)
    7. asciicast v2 writer with x-schmux metadata
    8. Support export of active (in-progress) recordings
    9. Async export API with WebSocket progress (202 + broadcast)
   10. CLI: schmux timelapse export (runs offline, no daemon needed)

Phase 3 — Polish
    1. Dashboard UI (export button, recordings list)
    2. Export caching alongside recording files
    3. List API with workspace/agent context
    4. Spinner detection (test against real recordings from Phase 1)
    5. Privacy export warning on CLI and API
    6. schmux timelapse delete (CLI + API)
```

Phase 0 ships independently — it unifies the streaming architecture regardless of timelapse. Gap and resize signaling are Phase 0 deliverables because the recording layer depends on them.

Phase 1 ships independently — recordings accumulate, providing real data to benchmark export performance, tune the compression heuristic, and evaluate VT100 emulator libraries before Phase 2.
