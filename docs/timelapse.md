# Timelapse

Timelapse records all terminal output from agent sessions continuously, then exports time-compressed `.cast` files (asciicast v2) that strip dead time -- LLM thinking pauses, spinners, progress bars -- so a 30-minute session becomes a few minutes of meaningful content playable in any asciinema-compatible player.

## Key files

| File                                       | Purpose                                                                                  |
| ------------------------------------------ | ---------------------------------------------------------------------------------------- |
| `internal/timelapse/recorder.go`           | Tails `OutputLog`, writes raw asciicast v2 `.cast` files                                 |
| `internal/timelapse/exporter.go`           | Single-pass time-compression: VT100 replay, scroll detection, timestamp rewriting        |
| `internal/timelapse/compression.go`        | `ClassifyIntervals` and `detectScroll` -- screen-diff classifier                         |
| `internal/timelapse/emulator.go`           | Wraps `vt10x` library for in-memory VT100 terminal emulation                             |
| `internal/timelapse/castwriter.go`         | Writes asciicast v2 NDJSON (header + `[timestamp, "o", data]` events)                    |
| `internal/timelapse/types.go`              | Typed record schemas and `.cast` parser                                                  |
| `internal/timelapse/storage.go`            | `ListRecordings`, `PruneRecordings` -- filesystem listing, age/size-based eviction       |
| `internal/session/controlsource.go`        | `ControlSource` interface and `SourceEvent` -- unified input boundary for SessionRuntime |
| `internal/session/tracker.go`              | Wires `RecorderFactory`, forwards gap/resize events to recorder                          |
| `internal/dashboard/handlers_timelapse.go` | HTTP handlers: list, export, download, delete recordings                                 |

## Architecture decisions

- **Always-on recording.** Recording adds one buffered file write per output event -- no tmux calls, no snapshots, no processing. Users do not need to remember to start recording before a good session happens.
- **OutputLog, not fan-out.** The recorder reads from the session tracker's `OutputLog` (a 50,000-entry sequenced circular buffer) rather than subscribing to the fan-out channel. Fan-out channels have drop-on-full semantics, which is correct for live UI but fatal for archival recording.
- **Asciicast v2 format.** Recordings are written directly as `.cast` files. Raw recordings are immediately playable with `asciinema play` or any web player -- no export step required for uncompressed playback.
- **Time-compression via scroll detection.** The exporter rewrites timestamps in a single pass: events where the screen scrolled (new content appeared) get a 300ms pause; everything else (spinners, idle, thinking) gets 0.001s. Scroll detection compares consecutive screen grids looking for row-shift patterns (at least 40% of compared rows shifted by k positions).
- **Text-only cell comparison.** Diffing uses character content only, not attributes. This avoids false positives from cosmetic formatting churn (color changes, style resets).
- **In-memory VT100, not tmux replay.** The exporter feeds bytes through `vt10x` in-process. No subprocess calls, no tmux server isolation, runs in milliseconds.
- **RecorderFactory injection.** `SessionRuntime` does not import `internal/timelapse`. Instead, `tracker.RecorderFactory` is a `func(outputLog, gapCh) Runnable` set by the session manager. This keeps the dependency one-directional.

### Rejected alternatives

- **Opt-in recording**: Users forget to start recording before the session they want to share.
- **Custom recording format**: Writing asciicast v2 directly means recordings are playable without export.
- **tmux-based replay for export**: Requires subprocess calls, FIFO synchronization, and an isolated tmux server. Slower by orders of magnitude.

## Data flow

```
SessionRuntime.run()
    |
    +-- SourceOutput events --> outputLog.Append() --> Recorder.Run()
    |                                                    |
    +-- SourceGap events -----> gapCh ----------------> Recorder (drainGapCh)
    |                                                    |
    +-- SourceResize events --> gapCh ----------------> Recorder.writeResizeEvent()
    |                                                    |
    +-- SourceClosed ---------> run() returns --------> Recorder.Stop()
                                                         |
                                                         v
                                                  ~/.schmux/recordings/<id>.cast
                                                         |
                                              (on-demand export request)
                                                         |
                                                         v
                                                  Exporter.Export()
                                                    |
                                                    +-- Read all events
                                                    +-- Feed to ScreenEmulator
                                                    +-- Compare consecutive grids
                                                    +-- Rewrite timestamps
                                                    +-- Write <id>.timelapse.cast
```

## Storage

```
~/.schmux/recordings/
  <sessionId>-<unixTimestamp>.cast              # raw recording (asciicast v2)
  <sessionId>-<unixTimestamp>.timelapse.cast    # compressed export
  .notice-shown                                 # first-run notice marker
```

Limits (configured in `config.json` under `timelapse`):

- Per-session: 50 MB hard cap (`maxFileSizeMB`). Recorder stops when hit.
- Total budget: 500 MB across all recordings (`maxTotalStorageMB`). Oldest evicted first.
- Retention: 7 days default (`retentionDays`). In-progress recordings are never evicted.

## Gotchas

- **Asciicast on disk, not the spec's NDJSON format.** The design spec describes typed NDJSON records. The implementation writes asciicast v2 directly (`[timestamp, "o", data]`). Do not write code expecting the spec's NDJSON format.
- **Gap events flow through `gapCh`, not OutputLog.** `SourceGap` and `SourceResize` events are forwarded via a dedicated channel (capacity 100), not through the `OutputLog`.
- **Export is synchronous.** Despite the spec calling for async with WebSocket progress, the implementation runs compression synchronously in the HTTP handler.
- **Two scroll detectors.** `compression.go` has `detectScroll` (used by `ClassifyIntervals`), `exporter.go` has `detectScrollGrid` (used by export). Same algorithm, different signatures. Update both when modifying scroll heuristics.
- **UTF-8 buffering across chunks.** The recorder buffers incomplete multi-byte UTF-8 sequences at chunk boundaries (`utf8Pending` field) to prevent splitting characters across asciicast events.
- **Two file convention.** Raw recordings are `<id>.cast`. Compressed exports are `<id>.timelapse.cast`. The export handler caches compressed files and skips re-running if the cache is newer than the source.

## Common modification patterns

- **Tuning compression sensitivity:** Adjust `scrollBeatDuration` (0.3s) and `fillerEventDuration` (0.001s) in `exporter.go`. The scroll detector requires 3+ compared rows and 40% match ratio at shifts 1-5 (`minScrollMatchRatio` in `compression.go`).
- **Adding a new record type:** Add a `RecordType` constant in `types.go`, update `ReadCastEvents` to handle the new asciicast event type code.
- **Changing storage limits:** Update both `Recorder.Run()` (per-session `maxBytes`) and `PruneRecordings` in `storage.go` (age/budget eviction).
- **Switching VT100 library:** The emulator is isolated behind `ScreenEmulator` in `emulator.go`. Replace the `vt10x` import and update `NewScreenEmulator`, `Write`, `Resize`, `CellText`/`CellGrid`, `RenderKeyframe`, and `Reset`.
