# Plan: Timelapse — Record and Replay Agent Sessions (v2)

**Goal**: Always-on terminal recording with on-demand export to compressed asciicast v2 `.cast` files, stripping idle time from agent sessions.

**Architecture**: ControlSource interface unifies local/remote streaming -> SessionTracker -> OutputLog -> Recorder writes NDJSON -> Exporter replays through in-memory VT100 emulator -> screen-diff compression -> `.cast` file.

**Design spec**: `docs/specs/2026-03-31-timelapse-design.md`

**Tech Stack**: Go (backend), TypeScript/React (dashboard), standard Go testing

**Test command**: `./test.sh`

---

## Changes from previous version

1. **Fixed Go module path**: Changed `github.com/schmux/schmux` to `github.com/sergeknystautas/schmux` in all import paths.
2. **Fixed `SourceEvent.Data` type**: Changed from `[]byte` to `string` to match existing `controlmode.OutputEvent.Data`. Documented the conversion boundary (OutputLog receives `[]byte(event.Data)`).
3. **Fixed dashboard file path**: Changed `assets/dashboard/src/routes/sessions/SessionDetail.tsx` to `assets/dashboard/src/routes/SessionDetailPage.tsx`.
4. **Fixed sidebar path**: Changed `assets/dashboard/src/components/Sidebar.tsx` to `assets/dashboard/src/components/AppShell.tsx`.
5. **Split timelapse route registration for CSRF**: GET routes registered outside CSRF group, POST/DELETE routes inside CSRF group, matching the existing pattern in `server.go`.
6. **Expanded Step 0.7 (RemoteSource wiring)**: Broken into sub-steps addressing provisioning state, signal monitor responsibilities, queued spawn path, and immediate spawn path. Time estimate increased.
7. **Split `CapturePane` interface**: Replaced single `CapturePane(opts CaptureOpts)` with `CaptureVisible() (string, error)` and `CaptureLines(n int) (string, error)` to match the two existing methods on both local and remote code paths.
8. **Fixed dependency group**: Steps 0.4 and 0.5 are now sequential (0.4 then 0.5), not parallel, since 0.5 depends on the new `NewSessionTracker` signature from 0.4.
9. **Expanded Step 0.3**: Broken into sub-steps covering FIFO sync, pane discovery, health probe, pause-after handling, and the output event loop. Reflects that `attachControlMode()` is ~200 lines of control mode logic.
10. **Expanded Step 1.3**: Broken into sub-steps for the main run loop, buffer overrun detection, size cap, and gap handling. Reflects complexity beyond a 2-5 minute task.
11. **Fixed `Record.T` omitempty**: Changed `T` field to `*float64` to avoid silently dropping `t: 0.0` from the first output record.
12. **Removed `./test.sh --quick`** from the plan header per CLAUDE.md rules. Only `./test.sh` is listed.
13. **Noted `config.go` is a large file** in Step 1.7 with instructions to use targeted reads.
14. **Noted `sync.Cond` Broadcast placement** in Step 1.1: Broadcast must be called outside the lock to avoid unnecessary contention.

---

## Phase 0: ControlSource Unification

### Step 0.1: Define SourceEvent types and ControlSource interface

**File**: `internal/session/controlsource.go` (create)

```go
package session

import "github.com/sergeknystautas/schmux/internal/remote/controlmode"

type SourceEventType int

const (
	SourceOutput SourceEventType = iota
	SourceGap
	SourceResize
	SourceClosed
)

type SourceEvent struct {
	Type     SourceEventType
	Data     string  // SourceOutput — matches controlmode.OutputEvent.Data (string)
	Reason   string  // SourceGap
	Snapshot string  // SourceGap: capture-pane on reconnect
	Width    int     // SourceResize
	Height   int     // SourceResize
	Err      error   // SourceClosed: nil = clean, non-nil = permanent
}

// ControlSource is the input boundary for SessionTracker.
// Implementations own reconnection logic; the tracker just drains Events().
//
// Data type rationale: Data is string (not []byte) because the upstream
// controlmode.OutputEvent.Data is string. The conversion to []byte happens
// at the OutputLog boundary: outputLog.Append([]byte(event.Data)).
type ControlSource interface {
	Events() <-chan SourceEvent
	SendKeys(keys string) error
	CaptureVisible() (string, error)           // visible screen (no scrollback)
	CaptureLines(n int) (string, error)        // last N lines of scrollback
	GetCursorState() (controlmode.CursorState, error)
	Close() error
}
```

**Test**: `internal/session/controlsource_test.go` — verify event construction helpers, type constants.

```bash
go test ./internal/session/ -run TestSourceEvent -v
```

---

### Step 0.2: Create MockControlSource for testing

**File**: `internal/session/controlsource_test.go` (create)

Build a `MockControlSource` that implements `ControlSource` with a buffered `Events()` channel. Tests push events into it, consumer drains them. This mock is used by all subsequent Phase 0 tests.

```go
type MockControlSource struct {
	events chan SourceEvent
	closed bool
}

func NewMockControlSource(bufSize int) *MockControlSource {
	return &MockControlSource{events: make(chan SourceEvent, bufSize)}
}

func (m *MockControlSource) Events() <-chan SourceEvent { return m.events }
func (m *MockControlSource) SendKeys(keys string) error { return nil }
func (m *MockControlSource) CaptureVisible() (string, error) { return "", nil }
func (m *MockControlSource) CaptureLines(n int) (string, error) { return "", nil }
func (m *MockControlSource) GetCursorState() (controlmode.CursorState, error) {
	return controlmode.CursorState{}, nil
}
func (m *MockControlSource) Close() error {
	if !m.closed {
		m.closed = true
		close(m.events)
	}
	return nil
}
func (m *MockControlSource) Emit(e SourceEvent) { m.events <- e }
```

```bash
go test ./internal/session/ -run TestMockControlSource -v
```

---

### Step 0.3: Extract LocalSource from SessionTracker

**File**: `internal/session/localsource.go` (create)

Move `attachControlMode()` (tracker.go, function `attachControlMode`) and the reconnection loop from `run()` (tracker.go, function `run`) into `LocalSource`. LocalSource implements `ControlSource`.

This is a substantial extraction — `attachControlMode()` is ~200 lines covering tmux control mode startup, FIFO sync, pane discovery, health probe, pause-after, and the output event loop. Break it into the following sub-steps:

**Sub-step 0.3a: Scaffold `LocalSource` struct and lifecycle methods**

```go
type LocalSource struct {
	sessionID   string
	tmuxSession string
	paneID      string
	logger      *log.Logger
	events      chan SourceEvent
	stopCh      chan struct{}
	stopCtx     context.Context
	stopCancel  context.CancelFunc
	doneCh      chan struct{}
	// control mode state (moved from SessionTracker):
	// cmClient, cmParser, cmCmd, cmStdin
}

func NewLocalSource(sessionID, tmuxSession, paneID string, logger *log.Logger) *LocalSource
func (s *LocalSource) Events() <-chan SourceEvent
func (s *LocalSource) SendKeys(keys string) error
func (s *LocalSource) CaptureVisible() (string, error)
func (s *LocalSource) CaptureLines(n int) (string, error)
func (s *LocalSource) GetCursorState() (controlmode.CursorState, error)
func (s *LocalSource) Close() error
```

**Sub-step 0.3b: Move reconnection loop into `LocalSource.run()`**

Move the retry logic from `SessionTracker.run()` (lines 362-394) into `LocalSource.run()`. On permanent error, emit `SourceClosed{Err: err}` and return. On transient error, increment reconnect counter, wait, and retry.

**Sub-step 0.3c: Move `attachControlMode()` into `LocalSource.attach()`**

Move the full method including:

- tmux `-C attach-session` startup and pipe setup
- Parser/Client creation
- Control mode ready timeout
- FIFO queue synchronization (sentinel command)
- Pane ID discovery via `discoverPaneID`
- Storing cmClient/cmParser/cmCmd/cmStdin references

**Sub-step 0.3d: Move health probe goroutine**

Move the health probe goroutine (5-second RTT measurement) from `attachControlMode()` into `LocalSource.attach()`. The `HealthProbe` field remains accessible for diagnostics.

**Sub-step 0.3e: Move pause-after handling**

Move the pause-after enable/disable logic and the `client.Pauses()` channel handling. On pause, LocalSource can either expose a syncTrigger channel or handle the `ContinuePane` call internally.

**Sub-step 0.3f: Move output event loop**

Move the `for { select { case event := <-outputCh: ... } }` loop. Translate `controlmode.OutputEvent` to `SourceEvent{Type: SourceOutput, Data: event.Data}` and emit on the events channel. On channel close, return (triggers reconnection in `run()`).

**Sub-step 0.3g: Add gap event emission on reconnect**

When `attach()` reconnects after a failure, call `CaptureVisible()` on the new connection and emit `SourceEvent{Type: SourceGap, Reason: "control_mode_reconnect", Snapshot: capturedScreen}` before resuming output events.

**Remove from tracker.go**: `attachControlMode()`, `run()` reconnection loop, `cmClient`, `cmParser`, `cmCmd`, `cmStdin` fields, `discoverPaneID`, `closeControlMode`, health probe goroutine, pause-after logic.

**Test**: `internal/session/localsource_test.go` — test that `isPermanentError` cases close the source with error, that non-permanent errors trigger retry with Gap events.

```bash
go test ./internal/session/ -run TestLocalSource -v
```

---

### Step 0.4: Refactor SessionTracker to consume ControlSource

**File**: `internal/session/tracker.go` (modify)

Change `NewSessionTracker` to accept a `ControlSource`:

```go
func NewSessionTracker(
	sessionID string,
	source ControlSource,
	st state.StateStore,
	eventFilePath string,
	eventHandlers map[string][]events.EventHandler,
	outputCallback func([]byte),
	logger *log.Logger,
) *SessionTracker
```

Remove `tmuxSession` parameter (the source knows its target). Remove control-mode-related fields from the struct.

Rewrite `run()` to drain `source.Events()`:

```go
func (t *SessionTracker) run() {
	defer close(t.doneCh)
	for event := range t.source.Events() {
		switch event.Type {
		case SourceOutput:
			// Data is string (matching OutputEvent.Data).
			// Conversion to []byte happens at the OutputLog boundary.
			t.fanOut(controlmode.OutputEvent{Data: event.Data})
		case SourceGap:
			t.handleGap(event)
			if t.gapCh != nil {
				t.gapCh <- event
			}
		case SourceResize:
			t.handleResize(event)
			if t.gapCh != nil {
				t.gapCh <- event
			}
		case SourceClosed:
			return
		}
	}
}
```

Update `CapturePane`/`CaptureLastLines` to delegate to `source.CaptureVisible()` and `source.CaptureLines(n)` respectively.

**Test**: `internal/session/tracker_test.go` — update existing tests. Use `MockControlSource` to push events, verify OutputLog receives them, verify subscriber channels receive SequencedOutput.

```bash
go test ./internal/session/ -run TestSessionTracker -v
```

---

### Step 0.5: Update fanOut and subscriber tests

**File**: `internal/session/tracker_fanout_test.go` (modify)

**Depends on Step 0.4** — these tests use `NewSessionTracker` which has a new signature after Step 0.4. Cannot be parallelized with Step 0.4.

Update existing fan-out tests to use `MockControlSource` instead of expecting real tmux output. Verify:

- Output events flow through OutputLog and to subscribers
- Gap events are surfaced (via new callback or channel)
- Resize events update `LastTerminalCols`/`LastTerminalRows`
- SourceClosed causes `run()` to exit

```bash
go test ./internal/session/ -run TestFanOut -v
```

---

### Step 0.6: Create RemoteSource wrapping Connection

**File**: `internal/session/remotesource.go` (create)

```go
type RemoteSource struct {
	conn   *remote.Connection
	paneID string
	events chan SourceEvent
	stopCh chan struct{}
	doneCh chan struct{}
}

func NewRemoteSource(conn *remote.Connection, paneID string) *RemoteSource

func (s *RemoteSource) Events() <-chan SourceEvent
func (s *RemoteSource) SendKeys(keys string) error {
	return s.conn.SendKeys(context.Background(), s.paneID, keys)
}
func (s *RemoteSource) CaptureVisible() (string, error) {
	// Connection does not expose CapturePaneVisible; use CaptureLines
	// with a large line count as a fallback, or add CapturePaneVisible
	// to Connection if needed.
	return s.conn.CapturePaneLines(context.Background(), s.paneID, 100)
}
func (s *RemoteSource) CaptureLines(n int) (string, error) {
	return s.conn.CapturePaneLines(context.Background(), s.paneID, n)
}
func (s *RemoteSource) GetCursorState() (controlmode.CursorState, error) {
	return s.conn.GetCursorState(context.Background(), s.paneID)
}
func (s *RemoteSource) Close() error
```

Internal `run()` subscribes to `conn.SubscribeOutput(paneID)`, translates `controlmode.OutputEvent` to `SourceEvent{Type: SourceOutput, Data: event.Data}` (both are `string`, no conversion needed), and emits on the events channel. On channel close, emits `SourceClosed`.

**Test**: `internal/session/remotesource_test.go` — test with mock Connection (or integration test if Connection can be constructed without real SSH).

```bash
go test ./internal/session/ -run TestRemoteSource -v
```

---

### Step 0.7: Update session manager wiring

**File**: `internal/session/manager.go` (modify)

This step is substantially more complex than a simple wiring change. `SpawnRemote()` currently does NOT create a tracker — it creates a session in state, calls `StartRemoteSignalMonitor`, and the WebSocket handler subscribes to `Connection.SubscribeOutput` directly, bypassing the tracker entirely. The remote spawn path also has two distinct flows: queued (provisioning) and immediate.

Break into the following sub-steps:

**Sub-step 0.7a: Update `ensureTrackerFromSession()` for local sessions**

Update `ensureTrackerFromSession()` to create a `LocalSource` and pass it to `NewSessionTracker`:

```go
func (m *Manager) ensureTrackerFromSession(sess state.Session) *SessionTracker {
	// ... existing early-return checks ...
	source := NewLocalSource(sess.ID, sess.TmuxSession, sess.PaneID, m.logger)
	tracker := NewSessionTracker(sess.ID, source, m.state, ...)
	m.trackers[sess.ID] = tracker
	return tracker
}
```

**Sub-step 0.7b: Handle the immediate spawn path in `SpawnRemote()`**

For the immediate path (where `RemotePaneID` is populated immediately), create a `RemoteSource` + `SessionTracker` right after the session is saved to state:

```go
// After remote session is created with a known paneID:
source := NewRemoteSource(conn, sess.RemotePaneID)
tracker := NewSessionTracker(sess.ID, source, m.state, ...)
m.trackers[sess.ID] = tracker
```

**Sub-step 0.7c: Handle the queued/provisioning spawn path in `SpawnRemote()`**

For the queued path (where `RemotePaneID` is initially empty and `Status` is "provisioning"), the tracker cannot be created immediately because there is no pane to subscribe to. Create the tracker in the goroutine callback that fires when `resultCh` delivers the provisioned pane:

```go
go func() {
	result := <-resultCh
	if result.Error == nil {
		// Session now has RemotePaneID — create tracker
		source := NewRemoteSource(conn, result.PaneID)
		tracker := NewSessionTracker(sessionID, source, m.state, ...)
		m.mu.Lock()
		m.trackers[sessionID] = tracker
		m.mu.Unlock()
	}
}()
```

**Sub-step 0.7d: Determine signal monitor fate**

`StartRemoteSignalMonitor` (manager.go:172) creates a watcher pane on the remote host that monitors the event file and dispatches structured events. This is orthogonal to terminal output streaming. Options:

1. Keep `StartRemoteSignalMonitor` as-is alongside the new tracker — it handles event file watching, not terminal output.
2. Fold its event dispatching into RemoteSource — adds complexity to the source interface.

Recommended: keep `StartRemoteSignalMonitor` separate. It watches a different pane (the watcher pane, not the session pane) and handles structured events, not terminal output. The tracker and signal monitor serve different purposes.

**Sub-step 0.7e: Test wiring**

Test that both spawn paths produce a working tracker:

- Local session: `ensureTrackerFromSession` creates LocalSource + tracker
- Remote immediate: `SpawnRemote` with available connection creates RemoteSource + tracker
- Remote queued: `SpawnRemote` with queued connection creates tracker after provisioning completes

```bash
go test ./internal/session/ -run TestManager -v
```

---

### Step 0.8: Collapse WebSocket handlers

**File**: `internal/dashboard/websocket.go` (modify)

Remove the `IsRemoteSession()` fork at line 214. All sessions now have a `SessionTracker`, so `handleTerminalWebSocket` works for both:

```go
// Remove this block:
// if sess.IsRemoteSession() {
//     s.handleRemoteTerminalWebSocket(w, r, sess)
//     return
// }

tracker, err := s.session.GetTracker(sessionID)
// ... same code path for all sessions
```

Remove `handleRemoteTerminalWebSocket` (line 1267 onward) entirely.

**Test**: Existing WebSocket tests should continue to pass. If there are remote-specific WebSocket tests, update them to use the unified path.

```bash
go test ./internal/dashboard/ -run TestWebSocket -v
```

---

### Step 0.9: Phase 0 integration verification

Run the full test suite and verify no regressions:

```bash
./test.sh
```

Manually verify with a local session: start daemon, spawn a session, confirm terminal streaming works via the dashboard.

If remote infrastructure is available, verify a remote session also streams correctly through the unified path.

---

### Phase 0 Dependency Groups

| Group | Steps    | Can Parallelize                                                  | Dependencies                                   |
| ----- | -------- | ---------------------------------------------------------------- | ---------------------------------------------- |
| 1     | 0.1, 0.2 | Yes                                                              | None                                           |
| 2     | 0.3      | No                                                               | Group 1                                        |
| 3     | 0.4      | No                                                               | Group 2                                        |
| 4     | 0.5, 0.6 | Yes (0.5 depends on 0.4's signature; 0.6 is independent of both) | 0.5 depends on Group 3; 0.6 depends on Group 2 |
| 5     | 0.7, 0.8 | No (sequential — wiring then WebSocket)                          | Group 4                                        |
| 6     | 0.9      | No                                                               | Group 5                                        |

---

## Phase 1: Recording

### Step 1.1: Add notification mechanism to OutputLog

**File**: `internal/session/outputlog.go` (modify)

Add a `sync.Cond` that wakes the recorder when new entries are appended:

```go
type OutputLog struct {
	// ... existing fields ...
	notify *sync.Cond  // signals on Append
}

func NewOutputLog(capacity int) *OutputLog {
	ol := &OutputLog{...}
	ol.notify = sync.NewCond(&ol.mu)
	return ol
}

func (l *OutputLog) Append(data []byte) uint64 {
	l.mu.Lock()
	// ... existing append logic ...
	l.mu.Unlock()
	// IMPORTANT: Broadcast OUTSIDE the lock to avoid unnecessary contention.
	// If Broadcast is called while holding mu, woken goroutines immediately
	// block on mu, adding latency under high output rates (e.g., fast fanOut).
	l.notify.Broadcast()
	return seq
}

// WaitForNew blocks until new entries are available after `afterSeq`.
func (l *OutputLog) WaitForNew(afterSeq uint64, stopCh <-chan struct{}) bool
```

**Test**: `internal/session/outputlog_test.go` — test that `WaitForNew` blocks until `Append` is called, test that it returns when `stopCh` is closed.

```bash
go test ./internal/session/ -run TestOutputLogNotify -v
```

---

### Step 1.2: Define recording format types

**File**: `internal/timelapse/types.go` (create)

```go
package timelapse

type RecordType string

const (
	RecordHeader RecordType = "header"
	RecordOutput RecordType = "output"
	RecordResize RecordType = "resize"
	RecordGap    RecordType = "gap"
	RecordEnd    RecordType = "end"
)

type Record struct {
	Type        RecordType `json:"type"`
	Version     int        `json:"version,omitempty"`     // header
	RecordingID string     `json:"recordingId,omitempty"` // header
	SessionID   string     `json:"sessionId,omitempty"`   // header
	Width       int        `json:"width,omitempty"`       // header, resize
	Height      int        `json:"height,omitempty"`      // header, resize
	StartTime   string     `json:"startTime,omitempty"`   // header
	T           *float64   `json:"t,omitempty"`           // all except header — pointer to avoid omitting t=0.0
	Seq         uint64     `json:"seq,omitempty"`         // output
	D           string     `json:"d,omitempty"`           // output (base64 or UTF-8)
	Reason      string     `json:"reason,omitempty"`      // gap
	LostSeqs    [2]uint64  `json:"lostSeqs,omitempty"`    // gap: [first, last]
	Snapshot    *string    `json:"snapshot,omitempty"`     // gap: nullable
}

// T field rationale: Using *float64 instead of float64 so that the first
// output record at t=0.000 is correctly serialized as "t":0 rather than
// being silently dropped by omitempty (which treats 0.0 as the zero value).
// Header records set T to nil (omitted from JSON). All other record types
// set T to a non-nil *float64.
```

Add `WriteRecord(w io.Writer, r Record) error` and `ReadRecords(r io.Reader) iter.Seq[Record]` (or a scanner-based reader).

Add a helper to create `*float64` values:

```go
func floatPtr(f float64) *float64 { return &f }
```

**Test**: `internal/timelapse/types_test.go` — roundtrip serialization of each record type, verify NDJSON format. Specifically test that `t: 0.0` is preserved in serialization.

```bash
go test ./internal/timelapse/ -run TestRecordSerialization -v
```

---

### Step 1.3: Implement Recorder

**File**: `internal/timelapse/recorder.go` (create)

This step covers the main recording loop including buffer overrun detection, size caps, and gap handling. Break into sub-steps:

**Sub-step 1.3a: Scaffold Recorder struct and constructor**

```go
type Recorder struct {
	recordingID string
	sessionID   string
	outputLog   *session.OutputLog
	gapCh       <-chan session.SourceEvent  // receives Gap and Resize events from tracker
	file        *os.File
	startTime   time.Time
	lastSeq     uint64
	bytesWritten int64
	maxBytes    int64  // 50 MB default
	stopCh      chan struct{}
	doneCh      chan struct{}
}

func NewRecorder(
	sessionID string,
	outputLog *session.OutputLog,
	gapCh <-chan session.SourceEvent,
	recordingDir string,
	maxBytes int64,
	width, height int,
) (*Recorder, error)

func (r *Recorder) Stop()  // signal stop, wait for done
```

Constructor opens the file with `os.OpenFile(..., 0600)`, generates `recordingID` as `<sessionID>-<unixTimestamp>`, initializes `startTime`.

**Sub-step 1.3b: Implement main run loop**

```go
func (r *Recorder) Run()
```

Main loop:

1. Write header record
2. Loop: `outputLog.WaitForNew(r.lastSeq, r.stopCh)`
3. `entries := outputLog.ReplayFrom(r.lastSeq + 1)`
4. Write output records for each entry
5. Update `r.lastSeq` and `r.bytesWritten`
6. On stop signal: write end record, close file

**Sub-step 1.3c: Add buffer overrun detection**

Before writing output records, check `outputLog.OldestSeq() > r.lastSeq + 1`. If so, entries were lost while the recorder was behind. Write a gap record with `"reason": "buffer_overrun"` and `lostSeqs` covering the missed range. Advance `r.lastSeq` to `outputLog.OldestSeq() - 1` before processing available entries.

**Sub-step 1.3d: Add per-session size cap**

After writing records, check `r.bytesWritten >= r.maxBytes`. If exceeded, write end record and stop. Log a warning that recording was capped.

**Sub-step 1.3e: Add gap and resize channel draining**

After processing output entries, drain `gapCh` non-blocking:

```go
for {
	select {
	case event := <-r.gapCh:
		switch event.Type {
		case session.SourceGap:
			r.writeGapRecord(event)
		case session.SourceResize:
			r.writeResizeRecord(event)
		}
	default:
		break
	}
}
```

**Test**: `internal/timelapse/recorder_test.go`

- Create an OutputLog, append entries, verify Recorder writes correct NDJSON
- Test buffer overrun detection: fill OutputLog past capacity while recorder is slow, verify gap record with correct `lostSeqs`
- Test maxBytes cap: verify recorder stops after limit and writes end record
- Test gap and resize events are recorded

```bash
go test ./internal/timelapse/ -run TestRecorder -v
```

---

### Step 1.4: Wire Recorder into SessionTracker

**File**: `internal/session/tracker.go` (modify)

When a tracker starts, if timelapse recording is enabled, create a `Recorder` and pass it the OutputLog and a channel for Gap/Resize events:

```go
func (t *SessionTracker) run() {
	defer close(t.doneCh)

	var recorder *timelapse.Recorder
	if t.timelapseEnabled {
		recorder, _ = timelapse.NewRecorder(t.sessionID, t.outputLog, t.gapCh, ...)
		go recorder.Run()
		defer recorder.Stop()
	}

	for event := range t.source.Events() {
		switch event.Type {
		case SourceOutput:
			t.fanOut(controlmode.OutputEvent{Data: event.Data})
		case SourceGap:
			t.handleGap(event)
			if t.gapCh != nil {
				t.gapCh <- event  // forward to recorder
			}
		case SourceResize:
			t.handleResize(event)
			if t.gapCh != nil {
				t.gapCh <- event  // forward to recorder
			}
		case SourceClosed:
			return
		}
	}
}
```

**Test**: Verify that with recording enabled, a `.jsonl` file appears in the recordings directory with correct header and output records.

```bash
go test ./internal/session/ -run TestTrackerWithRecorder -v
```

---

### Step 1.5: Storage management — size limits and pruning

**File**: `internal/timelapse/storage.go` (create)

```go
// PruneRecordings deletes recordings older than retentionDays
// and evicts oldest-first when totalBytes exceeds maxTotalBytes.
func PruneRecordings(dir string, retentionDays int, maxTotalBytes int64) error

// RecordingInfo returns metadata for a single recording file (parsed from header + file stats).
type RecordingInfo struct {
	RecordingID string
	SessionID   string
	StartTime   time.Time
	Duration    float64  // from end record, or last event if in-progress
	FileSize    int64
	Width       int
	Height      int
	InProgress  bool
}

func ListRecordings(dir string) ([]RecordingInfo, error)
```

File permissions: `os.OpenFile(..., 0600)` in Recorder constructor.

Pruning runs on daemon start and periodically (e.g., hourly).

**Test**: `internal/timelapse/storage_test.go`

- Create temp dir with recording files of various ages/sizes
- Verify age-based pruning deletes old files
- Verify size-based eviction removes oldest when budget exceeded
- Verify file permissions are 0600

```bash
go test ./internal/timelapse/ -run TestStorage -v
```

---

### Step 1.6: First-run notice

**File**: `internal/timelapse/recorder.go` (modify) or `internal/daemon/daemon.go` (modify)

On daemon start, if timelapse is enabled and a marker file (`~/.schmux/recordings/.notice-shown`) does not exist, log:

```
Timelapse recording is enabled. Terminal output is saved to ~/.schmux/recordings/. Run 'schmux config' to disable.
```

Then create the marker file.

**Test**: Verify notice is logged on first start, not on subsequent starts.

```bash
go test ./internal/timelapse/ -run TestFirstRunNotice -v
```

---

### Step 1.7: Config integration

**File**: `internal/config/config.go` (modify)

**Note**: `config.go` exceeds the 25,000-token read limit. Use targeted searches (`Grep`) to find the insertion points for the new struct and getter methods. Do not attempt to read the full file.

Add timelapse config fields:

```go
type TimelapseConfig struct {
	Enabled         *bool `json:"enabled,omitempty"`          // default true
	RetentionDays   *int  `json:"retentionDays,omitempty"`    // default 7
	MaxFileSizeMB   *int  `json:"maxFileSizeMB,omitempty"`    // default 50
	MaxTotalStorageMB *int `json:"maxTotalStorageMB,omitempty"` // default 500
}
```

Add to main Config struct: `Timelapse *TimelapseConfig \`json:"timelapse,omitempty"\``

Add getter methods with defaults: `GetTimelapseEnabled() bool`, `GetTimelapseRetentionDays() int`, etc.

**File**: `internal/api/contracts/config.go` (modify) — add TimelapseConfig to ConfigResponse and ConfigUpdateRequest.

**Test**: `internal/config/config_test.go` — verify defaults, verify JSON round-trip. (Note: `config_test.go` is also a large file — use targeted reads.)

```bash
go test ./internal/config/ -run TestTimelapse -v
```

---

### Step 1.8: CLI — `schmux timelapse list`

**File**: `cmd/schmux/main.go` (modify)

Add `timelapse` subcommand with `list` action:

```go
case "timelapse":
	if len(os.Args) < 3 {
		fmt.Println("Usage: schmux timelapse <list|export|delete>")
		os.Exit(1)
	}
	switch os.Args[2] {
	case "list":
		handleTimelapseList()
	}
```

`handleTimelapseList()` calls `timelapse.ListRecordings(recordingsDir)` directly (no daemon needed) and prints a table:

```
RECORDING ID              SESSION   DURATION   SIZE     STATUS
abc123-1711875300         abc123    5m42s      2.3 MB   complete
def456-1711876200         def456    12m18s     4.1 MB   in-progress
```

**Test**: `cmd/schmux/timelapse_test.go` — create temp dir with synthetic recording files, call `ListRecordings`, verify output formatting. Also manual verification:

```bash
go build ./cmd/schmux && ./schmux timelapse list
```

---

### Step 1.9: VT100 emulator library spike

**Task**: Evaluate Go VT100 emulator libraries against real recordings from the first few days of Phase 1.

Candidates to evaluate:

- `github.com/hinshun/vt10x`
- `github.com/ActiveState/vt10x`
- `github.com/creack/pty` (not an emulator — exclude)
- Any others found via search

Criteria:

- Alternate screen buffer support (critical — agents use editors)
- Scrolling regions
- UTF-8 / wide characters
- API for inspecting screen buffer (cell text without attributes)
- Resize support
- Maintenance status / license

Write findings to `docs/specs/2026-XX-XX-vt100-emulator-evaluation.md`.

---

### Phase 1 Dependency Groups

| Group | Steps         | Can Parallelize                              | Dependencies                             |
| ----- | ------------- | -------------------------------------------- | ---------------------------------------- |
| 1     | 1.1, 1.2, 1.7 | Yes (all independent)                        | Phase 0 complete                         |
| 2     | 1.3           | No                                           | Group 1 (needs OutputLog notify + types) |
| 3     | 1.4, 1.5      | Yes                                          | Group 2                                  |
| 4     | 1.6, 1.8      | Yes                                          | Group 3                                  |
| 5     | 1.9           | Independent (can run anytime during Phase 1) | Real recordings exist                    |

---

## Phase 2: Export

### Step 2.1: Integrate VT100 emulator library

**File**: `go.mod` (modify), `internal/timelapse/emulator.go` (create)

Wrap the selected VT100 library in a thin adapter:

```go
type ScreenEmulator struct {
	// wraps the selected library
}

func NewScreenEmulator(width, height int) *ScreenEmulator
func (e *ScreenEmulator) Write(data []byte)         // feed terminal bytes
func (e *ScreenEmulator) Resize(width, height int)   // resize terminal
func (e *ScreenEmulator) CellText() [][]rune          // screen grid, text only
func (e *ScreenEmulator) Reset()                      // clear all state
func (e *ScreenEmulator) RenderKeyframe() string      // full screen as ANSI sequences
```

`CellText()` returns the grid without style attributes — used for classification diffing.
`RenderKeyframe()` returns ANSI clear + full redraw — used for filler replacement in `.cast` output.

**Test**: `internal/timelapse/emulator_test.go`

- Feed known ANSI sequences, verify CellText
- Test alternate screen buffer enter/exit
- Test Resize
- Test RenderKeyframe produces valid ANSI

```bash
go test ./internal/timelapse/ -run TestEmulator -v
```

---

### Step 2.2: Implement screen-diff classifier

**File**: `internal/timelapse/compression.go` (create)

```go
type IntervalType int

const (
	Content IntervalType = iota
	Filler
)

type Interval struct {
	Type  IntervalType
	Start float64  // timestamp
	End   float64
}

// ClassifyIntervals reads a recording, replays through the emulator,
// snapshots every 500ms, and returns the compression map.
func ClassifyIntervals(recording io.Reader, emulator *ScreenEmulator) ([]Interval, error)
```

Implementation: single-pass, one snapshot in memory at a time. Compare `prev.CellText()` to `curr.CellText()`. Zero changed cells = Filler. Otherwise = Content. Adjacent same-type intervals merge.

**Test**: `internal/timelapse/compression_test.go`

- Feed a recording with idle gaps (repeated identical output), verify Filler intervals
- Feed a recording with continuous output, verify Content intervals
- Test merging of adjacent intervals

```bash
go test ./internal/timelapse/ -run TestClassify -v
```

---

### Step 2.3: Implement asciicast v2 writer

**File**: `internal/timelapse/castwriter.go` (create)

```go
type CastWriter struct {
	w      io.Writer
	offset float64  // current time offset in compressed timeline
}

func NewCastWriter(w io.Writer, header CastHeader) (*CastWriter, error)

type CastHeader struct {
	Width            int
	Height           int
	Duration         float64
	Title            string
	OriginalDuration float64
	CompressionRatio float64
	RecordingID      string
	Agent            string
	FillerStrategy   string
}

func (c *CastWriter) WriteEvent(timestamp float64, data string) error
func (c *CastWriter) WriteKeyframe(timestamp float64, keyframe string) error
```

`WriteEvent` writes `[timestamp, "o", data]` NDJSON. `WriteKeyframe` writes the same format but with a clear-screen prefix.

**Test**: `internal/timelapse/castwriter_test.go` — verify header format, event format, NDJSON output.

```bash
go test ./internal/timelapse/ -run TestCastWriter -v
```

---

### Step 2.4: Implement Exporter (two-pass pipeline)

**File**: `internal/timelapse/exporter.go` (create)

```go
type Exporter struct {
	recordingPath string
	outputPath    string
	emulator      *ScreenEmulator
	progressFn    func(pct float64)  // optional progress callback
}

func NewExporter(recordingPath, outputPath string, progressFn func(float64)) *Exporter

func (e *Exporter) Export() error
```

`Export()` implementation:

1. **Pass 1**: Read recording, call `ClassifyIntervals()` to produce compression map. Also collect keyframe strings at filler-interval boundaries.
2. **Pass 2**: Read recording again. For each output record:
   - If in a Content interval: write to CastWriter with compressed timestamp
   - If entering a Filler interval: write keyframe, advance timestamp by 300ms
   - If in a Filler interval: skip
3. Write `.cast` file via CastWriter.

**Test**: `internal/timelapse/exporter_test.go`

- Create a synthetic recording with known idle gaps
- Export it, verify `.cast` file has compressed timestamps
- Verify keyframes appear at filler boundaries
- Verify original content bytes are preserved
- Test export of in-progress recording (no end record)

```bash
go test ./internal/timelapse/ -run TestExporter -v
```

---

### Step 2.5: CLI — `schmux timelapse export`

**File**: `cmd/schmux/main.go` (modify)

Add `export` action to the `timelapse` subcommand:

```go
case "export":
	if len(os.Args) < 4 {
		fmt.Println("Usage: schmux timelapse export <recording-id> [-o output.cast]")
		os.Exit(1)
	}
	handleTimelapseExport(os.Args[3], flagOutput)
```

`handleTimelapseExport` finds the recording file, creates an Exporter, runs it, prints progress to stderr, and writes the `.cast` file. Runs offline — no daemon needed.

Prints privacy notice: "Note: recordings may contain sensitive terminal output. Review before sharing."

**Test**: Manual — export a real recording, play it with `asciinema play output.cast`.

```bash
go build ./cmd/schmux && ./schmux timelapse export abc123-1711875300 -o /tmp/test.cast
```

---

### Step 2.6: API — async export endpoint

**File**: `internal/dashboard/handlers_timelapse.go` (create)

```go
func (s *Server) handleTimelapseList(w http.ResponseWriter, r *http.Request)     // GET /api/timelapse
func (s *Server) handleTimelapseExport(w http.ResponseWriter, r *http.Request)   // POST /api/timelapse/{id}/export
func (s *Server) handleTimelapseDownload(w http.ResponseWriter, r *http.Request) // GET /api/timelapse/{id}/download
func (s *Server) handleTimelapseDelete(w http.ResponseWriter, r *http.Request)   // DELETE /api/timelapse/{id}
```

**File**: `internal/dashboard/server.go` (modify) — register routes with CSRF split matching the existing pattern:

```go
// Read-only endpoints (outside CSRF group, around line 578):
r.Get("/timelapse", s.handleTimelapseList)
r.Get("/timelapse/{recordingId}/download", s.handleTimelapseDownload)

// State-changing endpoints (inside r.Group with s.csrfMiddleware, around line 618):
r.Post("/timelapse/{recordingId}/export", s.handleTimelapseExport)
r.Delete("/timelapse/{recordingId}", s.handleTimelapseDelete)
```

Export runs in a goroutine. Returns `202 Accepted` with `{"exportId": "..."}`. Progress broadcast over `/ws/dashboard` as `{"type": "timelapseProgress", "recordingId": "...", "pct": 0.5}`. Completion broadcast as `{"type": "timelapseComplete", "recordingId": "..."}`.

**File**: `docs/api.md` (modify) — document new endpoints.

**Test**: `internal/dashboard/handlers_timelapse_test.go`

- Test list returns correct recording metadata
- Test export returns 202
- Test download returns 404 before export, 200 after
- Test delete removes files

```bash
go test ./internal/dashboard/ -run TestTimelapse -v
```

---

### Phase 2 Dependency Groups

| Group | Steps    | Can Parallelize                   | Dependencies                        |
| ----- | -------- | --------------------------------- | ----------------------------------- |
| 1     | 2.1      | No                                | Phase 1 complete + library selected |
| 2     | 2.2, 2.3 | Yes (independent)                 | Step 2.1                            |
| 3     | 2.4      | No                                | Group 2 (needs classifier + writer) |
| 4     | 2.5, 2.6 | Yes (CLI and API are independent) | Step 2.4                            |

---

## Phase 3: Polish

### Step 3.1: Dashboard UI — Export button on session detail

**File**: `assets/dashboard/src/routes/SessionDetailPage.tsx` (modify)

Add "Export Timelapse" button next to existing session controls. Visible when a recording exists for the session (check via timelapse list API). On click, POST to export endpoint. Show progress bar during export, download link when complete.

**File**: `assets/dashboard/src/lib/types.ts` (modify) — add timelapse-related types.

**File**: `go run ./cmd/gen-types` — regenerate if types are in contracts.

```bash
./test.sh
```

---

### Step 3.2: Dashboard UI — Recordings list page

**File**: `assets/dashboard/src/routes/timelapse/TimelapsePage.tsx` (create)

New route at `/timelapse`. Table listing all recordings with columns: workspace, agent, duration, original duration, size, status. Actions: Export, Download, Delete.

**File**: `assets/dashboard/src/App.tsx` (modify) — add route.
**File**: `assets/dashboard/src/components/AppShell.tsx` (modify) — add nav link to sidebar.

```bash
./test.sh
```

---

### Step 3.3: Export caching

**File**: `internal/timelapse/exporter.go` (modify)

After export completes, save the `.cast` file alongside the recording:

```
~/.schmux/recordings/
  abc123-1711875300.jsonl       # recording
  abc123-1711875300.cast        # cached export
```

On subsequent export requests, return the cached file if it exists and the recording hasn't been modified. Invalidate cache if the recording's mtime is newer than the `.cast` file.

**Test**: Export twice, verify second is instant. Append to recording, verify re-export.

```bash
go test ./internal/timelapse/ -run TestExportCache -v
```

---

### Step 3.4: CLI — `schmux timelapse delete`

**File**: `cmd/schmux/main.go` (modify)

Add `delete` action. Removes both the `.jsonl` recording and any cached `.cast` export.

```bash
go build ./cmd/schmux && ./schmux timelapse delete abc123-1711875300
```

---

### Step 3.5: Spinner detection (future — after real recording data)

**File**: `internal/timelapse/compression.go` (modify)

Add spinner detection as a second classification pass after idle detection:

```go
// Classify a non-idle interval as spinner if:
// - Changed cells <= spinnerThreshold (default 3)
// - Change bounding box <= 4 cells
// - Same region changed in consecutive snapshots
```

Gate behind a config flag or hardcoded off until validated against real recordings.

---

### Step 3.6: End-to-end verification

Full workflow test:

1. Start daemon with timelapse enabled
2. Spawn a session, run some commands, wait for idle periods
3. `schmux timelapse list` — verify recording appears
4. `schmux timelapse export <id> -o /tmp/test.cast` — verify `.cast` file is produced
5. Verify `.cast` plays correctly in asciinema player
6. Verify compressed duration is shorter than original
7. Verify content is preserved, idle gaps are collapsed
8. Dashboard: verify Export button works, download works

```bash
./test.sh
```

---

### Phase 3 Dependency Groups

| Group | Steps              | Can Parallelize       | Dependencies                  |
| ----- | ------------------ | --------------------- | ----------------------------- |
| 1     | 3.1, 3.2, 3.3, 3.4 | Yes (all independent) | Phase 2 complete              |
| 2     | 3.5                | No                    | Real recordings + tuning data |
| 3     | 3.6                | No                    | Group 1                       |

---

## Full Phase Dependencies

```
Phase 0 (ControlSource)  ->  Phase 1 (Recording)  ->  Phase 2 (Export)  ->  Phase 3 (Polish)
        |                          |                       |                      |
  Ships independently       Ships independently      Ships independently   Ships independently
  (unified streaming)       (recordings accumulate)   (export works)        (UI + cache)
```

Each phase delivers value and reduces risk for the next. Phase 0 unifies streaming for all future features. Phase 1 generates real data for Phase 2 decisions. Phase 2 is the core export pipeline. Phase 3 is polish.
