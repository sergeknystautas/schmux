# Terminal Desync Diagnostics Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use 10x-engineer:executing-plans to implement this plan task-by-task.

**Goal:** Build a dev-mode-only diagnostics system that tracks terminal pipeline health in real-time and captures actionable snapshots when xterm.js desyncs from tmux.

**Architecture:** Two systems — (1) always-on metrics panel with atomic counters and periodic stats WebSocket messages, (2) on-demand diagnostic capture that diffs the xterm.js screen buffer against a tmux `capture-pane` snapshot, writes the results to inspectable files, and spawns an agent session to analyze the root cause.

**Tech Stack:** Go (backend ring buffer, WebSocket stats/diagnostic messages, capture-pane), TypeScript/React (frontend ring buffer, metrics panel UI, screen buffer extraction), xterm.js (screen buffer API)

**Spec:** `docs/spec/terminal-desync-diagnostics.md`

---

### Task 1: Backend Ring Buffer

A fixed-size byte ring buffer for recording terminal output in the WebSocket handler.

**Files:**

- Create: `internal/dashboard/ringbuffer.go`
- Create: `internal/dashboard/ringbuffer_test.go`

**Step 1: Write the failing tests**

```go
// ringbuffer_test.go
package dashboard

import (
	"testing"
)

func TestRingBuffer_WriteAndSnapshot(t *testing.T) {
	rb := NewRingBuffer(16) // 16-byte buffer
	rb.Write([]byte("hello"))
	snap := rb.Snapshot()
	if string(snap) != "hello" {
		t.Errorf("got %q, want %q", string(snap), "hello")
	}
}

func TestRingBuffer_Wraps(t *testing.T) {
	rb := NewRingBuffer(8)
	rb.Write([]byte("abcdefgh")) // fills exactly
	rb.Write([]byte("ij"))       // overwrites first 2 bytes
	snap := rb.Snapshot()
	// should return data in order: "cdefghij"
	if string(snap) != "cdefghij" {
		t.Errorf("got %q, want %q", string(snap), "cdefghij")
	}
}

func TestRingBuffer_Empty(t *testing.T) {
	rb := NewRingBuffer(16)
	snap := rb.Snapshot()
	if len(snap) != 0 {
		t.Errorf("expected empty snapshot, got %d bytes", len(snap))
	}
}

func TestRingBuffer_LargerThanBuffer(t *testing.T) {
	rb := NewRingBuffer(4)
	rb.Write([]byte("abcdefgh")) // write 8 bytes into 4-byte buffer
	snap := rb.Snapshot()
	// should keep only last 4 bytes
	if string(snap) != "efgh" {
		t.Errorf("got %q, want %q", string(snap), "efgh")
	}
}

func TestRingBuffer_MultipleSmallWrites(t *testing.T) {
	rb := NewRingBuffer(8)
	rb.Write([]byte("ab"))
	rb.Write([]byte("cd"))
	rb.Write([]byte("ef"))
	rb.Write([]byte("gh"))
	rb.Write([]byte("ij")) // overwrites "ab"
	snap := rb.Snapshot()
	if string(snap) != "cdefghij" {
		t.Errorf("got %q, want %q", string(snap), "cdefghij")
	}
}
```

**Step 2: Run tests to verify they fail**

```bash
go test ./internal/dashboard/ -run TestRingBuffer -v
```

Expected: FAIL — `NewRingBuffer` not defined.

**Step 3: Write minimal implementation**

```go
// ringbuffer.go
package dashboard

// RingBuffer is a fixed-size circular byte buffer.
// It is not thread-safe — callers must ensure single-writer access.
type RingBuffer struct {
	buf    []byte
	cursor int
	full   bool
}

func NewRingBuffer(size int) *RingBuffer {
	return &RingBuffer{buf: make([]byte, size)}
}

func (rb *RingBuffer) Write(data []byte) {
	n := len(data)
	size := len(rb.buf)
	if n >= size {
		// data larger than buffer — keep only the last `size` bytes
		copy(rb.buf, data[n-size:])
		rb.cursor = 0
		rb.full = true
		return
	}
	end := rb.cursor + n
	if end <= size {
		copy(rb.buf[rb.cursor:], data)
	} else {
		first := size - rb.cursor
		copy(rb.buf[rb.cursor:], data[:first])
		copy(rb.buf, data[first:])
	}
	rb.cursor = end % size
	if end >= size {
		rb.full = true
	}
}

func (rb *RingBuffer) Snapshot() []byte {
	if !rb.full {
		return append([]byte(nil), rb.buf[:rb.cursor]...)
	}
	out := make([]byte, len(rb.buf))
	n := copy(out, rb.buf[rb.cursor:])
	copy(out[n:], rb.buf[:rb.cursor])
	return out
}
```

**Step 4: Run tests to verify they pass**

```bash
go test ./internal/dashboard/ -run TestRingBuffer -v
```

Expected: PASS

**Step 5: Commit**

```bash
git commit -m "feat(diagnostics): add ring buffer for terminal output recording"
```

---

### Task 2: Backend Pipeline Counters

Add atomic counters to the session tracker and expose existing parser drop counters.

**Files:**

- Modify: `internal/session/tracker.go` — add counter fields and getter methods
- Modify: `internal/remote/controlmode/parser.go` — add getter methods for existing drop counters
- Create: `internal/session/tracker_test.go` (or add to existing) — test counter increments

**Step 1: Write the failing tests**

```go
// Add to tracker_test.go (or create if it doesn't exist)
func TestTrackerCounters_Increment(t *testing.T) {
	var c TrackerCounters
	c.EventsDelivered.Add(5)
	c.BytesDelivered.Add(1024)
	c.Reconnects.Add(1)

	if c.EventsDelivered.Load() != 5 {
		t.Errorf("EventsDelivered = %d, want 5", c.EventsDelivered.Load())
	}
	if c.BytesDelivered.Load() != 1024 {
		t.Errorf("BytesDelivered = %d, want 1024", c.BytesDelivered.Load())
	}
	if c.Reconnects.Load() != 1 {
		t.Errorf("Reconnects = %d, want 1", c.Reconnects.Load())
	}
}
```

Also verify parser drop getters:

```go
func TestParserDropCounters(t *testing.T) {
	p := &Parser{}
	// droppedOutputs is already atomic.Int64 at parser.go:57
	if p.DroppedOutputs() != 0 {
		t.Errorf("initial DroppedOutputs = %d, want 0", p.DroppedOutputs())
	}
}
```

**Step 2: Run tests to verify they fail**

```bash
go test ./internal/session/ -run TestTrackerCounters -v
go test ./internal/remote/controlmode/ -run TestParserDropCounters -v
```

Expected: FAIL — `TrackerCounters` and `DroppedOutputs()` not defined.

**Step 3: Write minimal implementation**

In `internal/remote/controlmode/parser.go`, add getter methods near the existing drop counter fields (around line 57):

```go
func (p *Parser) DroppedOutputs() int64  { return p.droppedOutputs.Load() }
func (p *Parser) DroppedResponses() int64 { return p.droppedResponses.Load() }
func (p *Parser) DroppedEvents() int64    { return p.droppedEvents.Load() }
```

In `internal/session/tracker.go`, add a counters struct and field:

```go
type TrackerCounters struct {
	EventsDelivered atomic.Int64
	BytesDelivered  atomic.Int64
	Reconnects      atomic.Int64
}
```

Add a `Counters TrackerCounters` field to the `SessionTracker` struct. Increment `Counters.Reconnects` in the `run()` method when `attachControlMode` is retried. Increment `Counters.EventsDelivered` and `Counters.BytesDelivered` in `fanOut()` (once per event, not per subscriber).

Add a method to collect a snapshot including parser drops:

```go
func (t *SessionTracker) DiagnosticCounters() map[string]int64 {
	result := map[string]int64{
		"eventsDelivered":      t.Counters.EventsDelivered.Load(),
		"bytesDelivered":       t.Counters.BytesDelivered.Load(),
		"controlModeReconnects": t.Counters.Reconnects.Load(),
	}
	t.mu.Lock()
	if t.cmParser != nil {
		result["eventsDropped"] = t.cmParser.DroppedOutputs()
	}
	t.mu.Unlock()
	return result
}
```

**Step 4: Run tests to verify they pass**

```bash
go test ./internal/session/ -run TestTrackerCounters -v
go test ./internal/remote/controlmode/ -run TestParserDropCounters -v
```

Expected: PASS

**Step 5: Commit**

```bash
git commit -m "feat(diagnostics): add pipeline counters to tracker and parser"
```

---

### Task 3: Backend Stats WebSocket Message

Send periodic pipeline stats as text frames on the terminal WebSocket, gated behind dev mode.

**Files:**

- Modify: `internal/dashboard/websocket.go` — add stats ticker to the main select loop, add stats message struct, gate behind `s.devMode`

**Step 1: Write the failing test**

Add a test in `internal/dashboard/websocket_test.go` that verifies the stats message struct serializes correctly:

```go
func TestStatsMessage_JSON(t *testing.T) {
	msg := WSStatsMessage{
		Type:              "stats",
		EventsDelivered:   100,
		EventsDropped:     2,
		BytesDelivered:    50000,
		Reconnects:        0,
	}
	data, err := json.Marshal(msg)
	if err != nil {
		t.Fatal(err)
	}
	var decoded map[string]interface{}
	json.Unmarshal(data, &decoded)
	if decoded["type"] != "stats" {
		t.Errorf("type = %v, want stats", decoded["type"])
	}
	if int(decoded["eventsDropped"].(float64)) != 2 {
		t.Errorf("eventsDropped = %v, want 2", decoded["eventsDropped"])
	}
}
```

**Step 2: Run test to verify it fails**

```bash
go test ./internal/dashboard/ -run TestStatsMessage -v
```

Expected: FAIL — `WSStatsMessage` not defined.

**Step 3: Write minimal implementation**

In `internal/dashboard/websocket.go`, add the stats message struct:

```go
type WSStatsMessage struct {
	Type              string `json:"type"`
	EventsDelivered   int64  `json:"eventsDelivered"`
	EventsDropped     int64  `json:"eventsDropped"`
	BytesDelivered    int64  `json:"bytesDelivered"`
	BytesPerSec       int64  `json:"bytesPerSec"`
	Reconnects        int64  `json:"controlModeReconnects"`
}
```

In `handleTerminalWebSocket`, add the following changes — all gated behind `s.devMode`:

1. Before the main select loop, conditionally create a stats ticker and ring buffer:

```go
var statsTicker *time.Ticker
var ringBuf *RingBuffer
if s.devMode {
	statsTicker = time.NewTicker(3 * time.Second)
	defer statsTicker.Stop()
	ringBuf = NewRingBuffer(256 * 1024) // 256KB
}
```

2. In the main select loop, add a case for the stats ticker (only if `statsTicker != nil`):

```go
case <-statsTickerChan(): // helper that returns nil chan if ticker is nil
	counters := tracker.DiagnosticCounters()
	statsMsg := WSStatsMessage{
		Type:            "stats",
		EventsDelivered: counters["eventsDelivered"],
		EventsDropped:   counters["eventsDropped"],
		BytesDelivered:  counters["bytesDelivered"],
		Reconnects:      counters["controlModeReconnects"],
	}
	data, _ := json.Marshal(statsMsg)
	conn.WriteMessage(websocket.TextMessage, data)
```

3. In the output event handling case (around line 248), add ring buffer write before the WebSocket send:

```go
case event := <-outputCh:
	if ringBuf != nil {
		ringBuf.Write([]byte(event.Data))
	}
	// existing WriteMessage call...
```

**Step 4: Run tests to verify they pass**

```bash
go test ./internal/dashboard/ -run TestStatsMessage -v
```

Expected: PASS

**Step 5: Commit**

```bash
git commit -m "feat(diagnostics): send periodic stats text frames on terminal WebSocket"
```

---

### Task 4: Backend Diagnostic Capture Handler

Handle the `{"type": "diagnostic"}` client message: capture tmux screen via `capture-pane`, snapshot ring buffer and counters, and send back a diagnostic response. Write diagnostic files to `~/.schmux/diagnostics/`.

**Files:**

- Modify: `internal/dashboard/websocket.go` — add `"diagnostic"` case to the controlChan switch
- Create: `internal/dashboard/diagnostic.go` — diagnostic file writing logic
- Create: `internal/dashboard/diagnostic_test.go`

**Step 1: Write the failing tests**

```go
// diagnostic_test.go
func TestWriteDiagnosticDir(t *testing.T) {
	dir := t.TempDir()
	diag := &DiagnosticCapture{
		Timestamp:   time.Now(),
		SessionID:   "test-session",
		Cols:        120,
		Rows:        40,
		Counters:    map[string]int64{"eventsDelivered": 100, "eventsDropped": 0},
		TmuxScreen:  "$ hello\n$ world\n",
		RingBuffer:  []byte("\033[1mhello\033[0m\n"),
		Findings:    []string{"No drops detected"},
		Verdict:     "No obvious cause found.",
		DiffSummary: "0 rows differ",
	}
	err := diag.WriteToDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	// Verify files exist
	for _, name := range []string{"meta.json", "ringbuffer-backend.txt", "screen-tmux.txt"} {
		path := filepath.Join(dir, name)
		if _, err := os.Stat(path); os.IsNotExist(err) {
			t.Errorf("missing file: %s", name)
		}
	}
	// Verify meta.json is valid JSON
	data, _ := os.ReadFile(filepath.Join(dir, "meta.json"))
	var meta map[string]interface{}
	if err := json.Unmarshal(data, &meta); err != nil {
		t.Errorf("meta.json is not valid JSON: %v", err)
	}
	if meta["sessionId"] != "test-session" {
		t.Errorf("sessionId = %v, want test-session", meta["sessionId"])
	}
	// Verify ring buffer is raw text, not base64
	rbData, _ := os.ReadFile(filepath.Join(dir, "ringbuffer-backend.txt"))
	if string(rbData) != "\033[1mhello\033[0m\n" {
		t.Errorf("ringbuffer-backend.txt content mismatch")
	}
}
```

**Step 2: Run test to verify it fails**

```bash
go test ./internal/dashboard/ -run TestWriteDiagnosticDir -v
```

Expected: FAIL — `DiagnosticCapture` not defined.

**Step 3: Write minimal implementation**

```go
// diagnostic.go
package dashboard

import (
	"encoding/json"
	"os"
	"path/filepath"
	"time"
)

type DiagnosticCapture struct {
	Timestamp   time.Time
	SessionID   string
	Cols        int
	Rows        int
	Counters    map[string]int64
	TmuxScreen  string
	RingBuffer  []byte
	Findings    []string
	Verdict     string
	DiffSummary string
}

type diagnosticMeta struct {
	Timestamp   string           `json:"timestamp"`
	SessionID   string           `json:"sessionId"`
	TerminalSize struct {
		Cols int `json:"cols"`
		Rows int `json:"rows"`
	} `json:"terminalSize"`
	Counters    map[string]int64 `json:"counters"`
	Findings    []string         `json:"automatedFindings"`
	Verdict     string           `json:"verdict"`
	DiffSummary string           `json:"diffSummary"`
}

func (d *DiagnosticCapture) WriteToDir(dir string) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	// meta.json
	meta := diagnosticMeta{
		Timestamp: d.Timestamp.UTC().Format(time.RFC3339),
		SessionID: d.SessionID,
		Counters:  d.Counters,
		Findings:  d.Findings,
		Verdict:   d.Verdict,
		DiffSummary: d.DiffSummary,
	}
	meta.TerminalSize.Cols = d.Cols
	meta.TerminalSize.Rows = d.Rows
	metaJSON, err := json.MarshalIndent(meta, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(dir, "meta.json"), metaJSON, 0o644); err != nil {
		return err
	}
	// ringbuffer-backend.txt — raw bytes, not base64
	if err := os.WriteFile(filepath.Join(dir, "ringbuffer-backend.txt"), d.RingBuffer, 0o644); err != nil {
		return err
	}
	// screen-tmux.txt
	if err := os.WriteFile(filepath.Join(dir, "screen-tmux.txt"), []byte(d.TmuxScreen), 0o644); err != nil {
		return err
	}
	return nil
}
```

Then in `websocket.go`, add the `"diagnostic"` case to the controlChan switch (around line 300), gated behind `s.devMode`:

```go
case "diagnostic":
	if !s.devMode {
		break
	}
	// Capture tmux screen via control mode
	tmuxScreen, err := tracker.CaptureLastLines(ctx, 0) // 0 = visible area only
	if err != nil {
		log.Printf("diagnostic: capture-pane failed: %v", err)
		break
	}
	counters := tracker.DiagnosticCounters()
	// Build findings from automated checks
	findings := []string{}
	verdict := ""
	if counters["eventsDropped"] > 0 {
		findings = append(findings, fmt.Sprintf("%d events dropped", counters["eventsDropped"]))
		verdict = "Events were dropped due to channel backpressure."
	} else {
		findings = append(findings, "No drops detected")
		verdict = "No obvious cause found. Likely a bootstrap race during TUI redraw."
	}
	// Snapshot ring buffer
	var rbSnapshot []byte
	if ringBuf != nil {
		rbSnapshot = ringBuf.Snapshot()
	}
	// Write diagnostic directory
	diagDir := filepath.Join(os.Getenv("HOME"), ".schmux", "diagnostics",
		fmt.Sprintf("%s-%s", time.Now().Format("2006-01-02T15-04-05"), sessionID))
	diag := &DiagnosticCapture{
		Timestamp:  time.Now(),
		SessionID:  sessionID,
		Counters:   counters,
		TmuxScreen: tmuxScreen,
		RingBuffer: rbSnapshot,
		Findings:   findings,
		Verdict:    verdict,
	}
	if err := diag.WriteToDir(diagDir); err != nil {
		log.Printf("diagnostic: write failed: %v", err)
	}
	// Send response back to client
	resp := map[string]interface{}{
		"type":       "diagnostic",
		"diagDir":    diagDir,
		"counters":   counters,
		"findings":   findings,
		"verdict":    verdict,
		"tmuxScreen": tmuxScreen,
	}
	data, _ := json.Marshal(resp)
	conn.WriteMessage(websocket.TextMessage, data)
```

**Step 4: Run tests to verify they pass**

```bash
go test ./internal/dashboard/ -run TestWriteDiagnosticDir -v
```

Expected: PASS

**Step 5: Commit**

```bash
git commit -m "feat(diagnostics): add diagnostic capture handler and file writer"
```

---

### Task 5: Frontend Ring Buffer and Counters

Add a ring buffer and pipeline counters to the `TerminalStream` class, gated behind dev mode.

**Files:**

- Create: `assets/dashboard/src/lib/streamDiagnostics.ts` — ring buffer, counters, sequence break detection
- Create: `assets/dashboard/src/lib/streamDiagnostics.test.ts`

**Step 1: Write the failing tests**

```typescript
// streamDiagnostics.test.ts
import { describe, it, expect, beforeEach } from 'vitest';
import { StreamDiagnostics } from './streamDiagnostics';

describe('StreamDiagnostics', () => {
  let diag: StreamDiagnostics;

  beforeEach(() => {
    diag = new StreamDiagnostics();
  });

  it('tracks frame count and byte count', () => {
    diag.recordFrame(new Uint8Array([1, 2, 3]));
    diag.recordFrame(new Uint8Array([4, 5]));
    expect(diag.framesReceived).toBe(2);
    expect(diag.bytesReceived).toBe(5);
  });

  it('tracks bootstrap count', () => {
    diag.recordBootstrap();
    diag.recordBootstrap();
    expect(diag.bootstrapCount).toBe(2);
  });

  it('ring buffer stores recent data', () => {
    diag.recordFrame(new TextEncoder().encode('hello'));
    diag.recordFrame(new TextEncoder().encode(' world'));
    const snapshot = diag.ringBufferSnapshot();
    expect(new TextDecoder().decode(snapshot)).toBe('hello world');
  });

  it('ring buffer wraps around', () => {
    const smallDiag = new StreamDiagnostics(8); // 8-byte ring buffer
    smallDiag.recordFrame(new TextEncoder().encode('abcdefgh'));
    smallDiag.recordFrame(new TextEncoder().encode('ij'));
    const snapshot = smallDiag.ringBufferSnapshot();
    expect(new TextDecoder().decode(snapshot)).toBe('cdefghij');
  });

  it('detects incomplete escape sequences at frame boundaries', () => {
    // Frame ending with partial CSI sequence
    diag.recordFrame(new TextEncoder().encode('hello\x1b['));
    expect(diag.sequenceBreaks).toBe(1);

    // Frame ending with complete sequence — no break
    diag.recordFrame(new TextEncoder().encode('hello\x1b[0m'));
    expect(diag.sequenceBreaks).toBe(1); // unchanged
  });

  it('reset clears all counters', () => {
    diag.recordFrame(new Uint8Array([1, 2, 3]));
    diag.recordBootstrap();
    diag.reset();
    expect(diag.framesReceived).toBe(0);
    expect(diag.bytesReceived).toBe(0);
    expect(diag.bootstrapCount).toBe(0);
    expect(diag.sequenceBreaks).toBe(0);
  });
});
```

**Step 2: Run tests to verify they fail**

```bash
cd assets/dashboard && npx vitest run src/lib/streamDiagnostics.test.ts
```

Expected: FAIL — module not found.

**Step 3: Write minimal implementation**

```typescript
// streamDiagnostics.ts

const DEFAULT_RING_BUFFER_SIZE = 256 * 1024; // 256KB

export class StreamDiagnostics {
  framesReceived = 0;
  bytesReceived = 0;
  bootstrapCount = 0;
  sequenceBreaks = 0;

  private ringBuffer: Uint8Array;
  private cursor = 0;
  private full = false;

  constructor(ringBufferSize = DEFAULT_RING_BUFFER_SIZE) {
    this.ringBuffer = new Uint8Array(ringBufferSize);
  }

  recordFrame(data: Uint8Array): void {
    this.framesReceived++;
    this.bytesReceived += data.length;
    this.writeToRingBuffer(data);
    this.checkSequenceBreak(data);
  }

  recordBootstrap(): void {
    this.bootstrapCount++;
  }

  ringBufferSnapshot(): Uint8Array {
    if (!this.full) {
      return this.ringBuffer.slice(0, this.cursor);
    }
    const out = new Uint8Array(this.ringBuffer.length);
    const tail = this.ringBuffer.subarray(this.cursor);
    out.set(tail, 0);
    out.set(this.ringBuffer.subarray(0, this.cursor), tail.length);
    return out;
  }

  reset(): void {
    this.framesReceived = 0;
    this.bytesReceived = 0;
    this.bootstrapCount = 0;
    this.sequenceBreaks = 0;
    this.cursor = 0;
    this.full = false;
  }

  private writeToRingBuffer(data: Uint8Array): void {
    const size = this.ringBuffer.length;
    if (data.length >= size) {
      this.ringBuffer.set(data.subarray(data.length - size));
      this.cursor = 0;
      this.full = true;
      return;
    }
    const end = this.cursor + data.length;
    if (end <= size) {
      this.ringBuffer.set(data, this.cursor);
    } else {
      const first = size - this.cursor;
      this.ringBuffer.set(data.subarray(0, first), this.cursor);
      this.ringBuffer.set(data.subarray(first), 0);
    }
    this.cursor = end % size;
    if (end >= size) {
      this.full = true;
    }
  }

  private checkSequenceBreak(data: Uint8Array): void {
    // Check if frame ends with an incomplete ANSI escape sequence.
    // Look for ESC (\x1b) near the end without a terminating letter.
    const len = data.length;
    if (len === 0) return;

    // Scan backwards from end to find last ESC
    for (let i = len - 1; i >= Math.max(0, len - 16); i--) {
      if (data[i] === 0x1b) {
        // Found ESC — check if sequence is complete
        // A complete CSI sequence ends with a letter (0x40-0x7E)
        const remaining = data.subarray(i + 1);
        if (remaining.length === 0) {
          // Bare ESC at end of frame
          this.sequenceBreaks++;
          return;
        }
        // CSI: ESC [
        if (remaining[0] === 0x5b) {
          // Check if it ends with a terminator
          const last = remaining[remaining.length - 1];
          if (last < 0x40 || last > 0x7e) {
            this.sequenceBreaks++;
          }
        }
        return;
      }
    }
  }
}
```

**Step 4: Run tests to verify they pass**

```bash
cd assets/dashboard && npx vitest run src/lib/streamDiagnostics.test.ts
```

Expected: PASS

**Step 5: Commit**

```bash
git commit -m "feat(diagnostics): add frontend ring buffer and stream counters"
```

---

### Task 6: Integrate Frontend Diagnostics into TerminalStream

Wire `StreamDiagnostics` into the `TerminalStream` class, conditionally enabled based on dev mode.

**Files:**

- Modify: `assets/dashboard/src/lib/terminalStream.ts` — add diagnostics integration in `handleOutput`, `connect`, add `sendDiagnostic()` method

**Step 1: Plan the changes**

In `terminalStream.ts`:

1. Add an optional `diagnostics: StreamDiagnostics | null` field, initialized to `null`.
2. Add an `enableDiagnostics()` method that creates the `StreamDiagnostics` instance.
3. In `handleOutput()` (line 413):
   - In the binary frame branch (line 418), after decoding, call `diagnostics?.recordFrame()` with the raw `Uint8Array`. On bootstrap, also call `diagnostics?.recordBootstrap()`.
   - In the text frame branch (line 436), add cases for `"stats"` and `"diagnostic"` message types. Store the latest stats on the instance. For diagnostic responses, emit a callback.
4. Add a `sendDiagnostic()` method that sends `{"type": "diagnostic"}` as a text frame.
5. Expose `diagnostics` and `latestStats` as public readonly properties.

**Step 2: Write the changes**

In the `handleOutput` binary branch, add before the existing `terminal.write()`:

```typescript
if (this.diagnostics) {
  this.diagnostics.recordFrame(new Uint8Array(event.data as ArrayBuffer));
  if (!this.bootstrapped) {
    this.diagnostics.recordBootstrap();
  }
}
```

In the text frame branch, add the new cases:

```typescript
case "stats":
  this.latestStats = msg;
  this.onStatsUpdate?.(msg);
  break;
case "diagnostic":
  this.onDiagnosticResponse?.(msg);
  break;
```

Add the methods:

```typescript
enableDiagnostics(): void {
  if (!this.diagnostics) {
    this.diagnostics = new StreamDiagnostics();
  }
}

sendDiagnostic(): void {
  if (this.ws?.readyState === WebSocket.OPEN) {
    this.ws.send(JSON.stringify({ type: "diagnostic" }));
  }
}
```

**Step 3: Run existing tests to verify nothing breaks**

```bash
cd assets/dashboard && npx vitest run
```

Expected: All existing tests PASS.

**Step 4: Commit**

```bash
git commit -m "feat(diagnostics): integrate stream diagnostics into TerminalStream"
```

---

### Task 7: Metrics Panel UI Component

Create the collapsible metrics panel shown on the session detail page, gated behind dev mode.

**Files:**

- Create: `assets/dashboard/src/components/StreamMetricsPanel.tsx`
- Create: `assets/dashboard/src/components/StreamMetricsPanel.test.tsx`
- Modify: `assets/dashboard/src/routes/SessionDetailPage.tsx` — add the panel between the header and terminal

**Step 1: Write the failing test**

```typescript
// StreamMetricsPanel.test.tsx
import { describe, it, expect } from "vitest";
import { render, screen } from "@testing-library/react";
import { StreamMetricsPanel } from "./StreamMetricsPanel";

describe("StreamMetricsPanel", () => {
  it("renders summary line with stats", () => {
    render(
      <StreamMetricsPanel
        backendStats={{
          eventsDelivered: 1234,
          eventsDropped: 0,
          bytesDelivered: 867000,
          controlModeReconnects: 0,
        }}
        frontendStats={{
          framesReceived: 1200,
          bytesReceived: 850000,
          bootstrapCount: 1,
          sequenceBreaks: 0,
        }}
      />
    );
    expect(screen.getByText(/1\.2K frames/)).toBeTruthy();
    expect(screen.getByText(/0 drops/)).toBeTruthy();
    expect(screen.getByText(/0 seq breaks/)).toBeTruthy();
  });

  it("highlights non-zero drops in red", () => {
    render(
      <StreamMetricsPanel
        backendStats={{
          eventsDelivered: 100,
          eventsDropped: 5,
          bytesDelivered: 50000,
          controlModeReconnects: 0,
        }}
        frontendStats={{
          framesReceived: 95,
          bytesReceived: 48000,
          bootstrapCount: 1,
          sequenceBreaks: 0,
        }}
      />
    );
    const dropsEl = screen.getByText(/5 drops/);
    expect(dropsEl.className).toContain("warning");
  });
});
```

**Step 2: Run test to verify it fails**

```bash
cd assets/dashboard && npx vitest run src/components/StreamMetricsPanel.test.tsx
```

Expected: FAIL — module not found.

**Step 3: Write minimal implementation**

```tsx
// StreamMetricsPanel.tsx
import { useState } from 'react';

interface BackendStats {
  eventsDelivered: number;
  eventsDropped: number;
  bytesDelivered: number;
  controlModeReconnects: number;
  bytesPerSec?: number;
}

interface FrontendStats {
  framesReceived: number;
  bytesReceived: number;
  bootstrapCount: number;
  sequenceBreaks: number;
}

interface Props {
  backendStats: BackendStats | null;
  frontendStats: FrontendStats | null;
  onDiagnosticCapture?: () => void;
}

function formatCount(n: number): string {
  if (n >= 1_000_000) return `${(n / 1_000_000).toFixed(1)}M`;
  if (n >= 1_000) return `${(n / 1_000).toFixed(1)}K`;
  return String(n);
}

function formatBytes(n: number): string {
  if (n >= 1_000_000) return `${(n / 1_000_000).toFixed(1)}MB`;
  if (n >= 1_000) return `${(n / 1_000).toFixed(0)}KB`;
  return `${n}B`;
}

export function StreamMetricsPanel({ backendStats, frontendStats, onDiagnosticCapture }: Props) {
  const [expanded, setExpanded] = useState(false);
  const frames = frontendStats?.framesReceived ?? 0;
  const bytes = frontendStats?.bytesReceived ?? 0;
  const drops = backendStats?.eventsDropped ?? 0;
  const seqBreaks = frontendStats?.sequenceBreaks ?? 0;

  return (
    <div className="stream-metrics-panel">
      <div className="stream-metrics-panel__summary" onClick={() => setExpanded(!expanded)}>
        <span>Stream: {formatCount(frames)} frames</span>
        <span> | {formatBytes(bytes)}</span>
        <span className={drops > 0 ? 'warning' : ''}> | {drops} drops</span>
        <span className={seqBreaks > 0 ? 'warning' : ''}> | {seqBreaks} seq breaks</span>
        {onDiagnosticCapture && (
          <button
            className="stream-metrics-panel__diagnose-btn"
            onClick={(e) => {
              e.stopPropagation();
              onDiagnosticCapture();
            }}
          >
            Diagnose Desync
          </button>
        )}
      </div>
      {expanded && (
        <div className="stream-metrics-panel__details">
          <table>
            <thead>
              <tr>
                <th>Metric</th>
                <th>Value</th>
              </tr>
            </thead>
            <tbody>
              <tr>
                <td>Frames received</td>
                <td>{frames}</td>
              </tr>
              <tr>
                <td>Bytes received</td>
                <td>{formatBytes(bytes)}</td>
              </tr>
              <tr>
                <td>Events delivered (backend)</td>
                <td>{backendStats?.eventsDelivered ?? '—'}</td>
              </tr>
              <tr>
                <td>Events dropped</td>
                <td className={drops > 0 ? 'warning' : ''}>{drops}</td>
              </tr>
              <tr>
                <td>Bytes delivered (backend)</td>
                <td>{formatBytes(backendStats?.bytesDelivered ?? 0)}</td>
              </tr>
              <tr>
                <td>Throughput</td>
                <td>
                  {backendStats?.bytesPerSec ? formatBytes(backendStats.bytesPerSec) + '/s' : '—'}
                </td>
              </tr>
              <tr>
                <td>Bootstrap count</td>
                <td>{frontendStats?.bootstrapCount ?? '—'}</td>
              </tr>
              <tr>
                <td>Sequence breaks</td>
                <td className={seqBreaks > 0 ? 'warning' : ''}>{seqBreaks}</td>
              </tr>
              <tr>
                <td>Control mode reconnects</td>
                <td>{backendStats?.controlModeReconnects ?? 0}</td>
              </tr>
            </tbody>
          </table>
        </div>
      )}
    </div>
  );
}
```

**Step 4: Run tests to verify they pass**

```bash
cd assets/dashboard && npx vitest run src/components/StreamMetricsPanel.test.tsx
```

Expected: PASS

**Step 5: Commit**

```bash
git commit -m "feat(diagnostics): add StreamMetricsPanel component"
```

---

### Task 8: Wire Metrics Panel into SessionDetailPage

Connect the `StreamMetricsPanel` to the session detail page, showing it only in dev mode.

**Files:**

- Modify: `assets/dashboard/src/routes/SessionDetailPage.tsx` — add the panel, wire up stats callbacks from TerminalStream, gate behind dev mode

**Step 1: Plan the wiring**

In `SessionDetailPage.tsx`:

1. Import `useVersionInfo` and `StreamMetricsPanel`.
2. Add state for `backendStats` and `frontendStats`.
3. In the terminal initialization effect (around line 111), if `versionInfo?.dev_mode`, call `stream.enableDiagnostics()` and set up `stream.onStatsUpdate` and a periodic poll of `stream.diagnostics` for frontend stats.
4. Render `<StreamMetricsPanel>` between the header actions (line 559) and the terminal container (line 561), only if `versionInfo?.dev_mode`.
5. Wire the "Diagnose Desync" button to `stream.sendDiagnostic()`.

**Step 2: Write the changes and run existing tests**

After making the changes:

```bash
cd assets/dashboard && npx vitest run
```

Expected: All existing tests PASS (the panel is gated behind dev mode which defaults to false in tests).

**Step 3: Commit**

```bash
git commit -m "feat(diagnostics): wire metrics panel into session detail page"
```

---

### Task 9: Frontend Screen Buffer Extraction

Add a method to extract the xterm.js screen buffer as text with ANSI escape sequences, for comparison with the tmux `capture-pane` output.

**Files:**

- Create: `assets/dashboard/src/lib/screenCapture.ts`
- Create: `assets/dashboard/src/lib/screenCapture.test.ts`

**Step 1: Write the failing tests**

```typescript
// screenCapture.test.ts
import { describe, it, expect } from 'vitest';
import { extractScreenText } from './screenCapture';

describe('extractScreenText', () => {
  it('extracts text from a mock buffer', () => {
    // Create a minimal mock of xterm.js buffer
    const mockBuffer = {
      length: 2,
      getLine: (y: number) => ({
        length: 5,
        getCell: (x: number) => {
          const chars = y === 0 ? 'hello' : 'world';
          return {
            getChars: () => chars[x] || '',
            getWidth: () => 1,
          };
        },
        translateToString: () => (y === 0 ? 'hello' : 'world'),
      }),
    };
    const text = extractScreenText(mockBuffer as any);
    expect(text).toBe('hello\nworld\n');
  });
});
```

**Step 2: Run test to verify it fails**

```bash
cd assets/dashboard && npx vitest run src/lib/screenCapture.test.ts
```

Expected: FAIL — module not found.

**Step 3: Write minimal implementation**

```typescript
// screenCapture.ts
import type { IBuffer } from '@xterm/xterm';

export function extractScreenText(buffer: IBuffer): string {
  const lines: string[] = [];
  for (let y = 0; y < buffer.length; y++) {
    const line = buffer.getLine(y);
    if (line) {
      lines.push(line.translateToString());
    }
  }
  return lines.join('\n') + '\n';
}
```

**Step 4: Run tests to verify they pass**

```bash
cd assets/dashboard && npx vitest run src/lib/screenCapture.test.ts
```

Expected: PASS

**Step 5: Commit**

```bash
git commit -m "feat(diagnostics): add xterm.js screen buffer extraction"
```

---

### Task 10: Frontend Diagnostic Response Handler

Handle the diagnostic response from the backend: extract the xterm.js screen, compute the diff, save the frontend ring buffer, and display results.

**Files:**

- Create: `assets/dashboard/src/lib/screenDiff.ts` — compare tmux and xterm.js screen text
- Create: `assets/dashboard/src/lib/screenDiff.test.ts`
- Modify: `assets/dashboard/src/lib/terminalStream.ts` — add full diagnostic response handling

**Step 1: Write the failing tests for screen diff**

```typescript
// screenDiff.test.ts
import { describe, it, expect } from 'vitest';
import { computeScreenDiff } from './screenDiff';

describe('computeScreenDiff', () => {
  it('returns empty diff for identical screens', () => {
    const diff = computeScreenDiff('hello\nworld\n', 'hello\nworld\n');
    expect(diff.differingRows).toHaveLength(0);
    expect(diff.summary).toBe('0 rows differ');
  });

  it('detects differing rows', () => {
    const diff = computeScreenDiff('line1\nline2\nline3\n', 'line1\nLINE2\nline3\n');
    expect(diff.differingRows).toHaveLength(1);
    expect(diff.differingRows[0].row).toBe(1);
    expect(diff.differingRows[0].tmux).toBe('line2');
    expect(diff.differingRows[0].xterm).toBe('LINE2');
  });

  it('generates human-readable diff text', () => {
    const diff = computeScreenDiff('aaa\nbbb\n', 'aaa\nccc\n');
    expect(diff.diffText).toContain('Row 1:');
    expect(diff.diffText).toContain('tmux:  bbb');
    expect(diff.diffText).toContain('xterm: ccc');
  });
});
```

**Step 2: Run test to verify it fails**

```bash
cd assets/dashboard && npx vitest run src/lib/screenDiff.test.ts
```

Expected: FAIL — module not found.

**Step 3: Write minimal implementation**

```typescript
// screenDiff.ts
export interface ScreenDiff {
  differingRows: Array<{ row: number; tmux: string; xterm: string }>;
  summary: string;
  diffText: string;
}

export function computeScreenDiff(tmuxScreen: string, xtermScreen: string): ScreenDiff {
  const tmuxLines = tmuxScreen.split('\n');
  const xtermLines = xtermScreen.split('\n');
  const maxRows = Math.max(tmuxLines.length, xtermLines.length);
  const differingRows: ScreenDiff['differingRows'] = [];

  for (let i = 0; i < maxRows; i++) {
    const tmuxLine = tmuxLines[i] ?? '';
    const xtermLine = xtermLines[i] ?? '';
    if (tmuxLine !== xtermLine) {
      differingRows.push({ row: i, tmux: tmuxLine, xterm: xtermLine });
    }
  }

  const summary = `${differingRows.length} rows differ`;
  const diffText =
    differingRows.length === 0
      ? 'Screens match.'
      : differingRows
          .map((d) => `Row ${d.row}:\n  tmux:  ${d.tmux}\n  xterm: ${d.xterm}`)
          .join('\n') + `\n---\n${summary}`;

  return { differingRows, summary, diffText };
}
```

**Step 4: Run tests to verify they pass**

```bash
cd assets/dashboard && npx vitest run src/lib/screenDiff.test.ts
```

Expected: PASS

**Step 5: Commit**

```bash
git commit -m "feat(diagnostics): add screen diff computation"
```

---

### Task 11: Full Diagnostic Response Flow

Wire up the complete diagnostic flow: when the backend responds with `{"type": "diagnostic"}`, extract xterm.js screen, compute diff, generate the `screen-xterm.txt`, `screen-diff.txt`, and `ringbuffer-frontend.txt` content, and send it all to the backend via an HTTP endpoint to append to the diagnostic directory.

**Files:**

- Modify: `assets/dashboard/src/lib/terminalStream.ts` — implement the `onDiagnosticResponse` callback
- Create: `internal/dashboard/handlers_diagnostic.go` — HTTP endpoint to append frontend files to diagnostic dir
- Modify: `internal/dashboard/server.go` — register the endpoint (dev mode only)

**Step 1: Add the HTTP endpoint on the backend**

In `internal/dashboard/handlers_diagnostic.go`:

```go
func (s *Server) handleDiagnosticAppend(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req struct {
		DiagDir             string `json:"diagDir"`
		XtermScreen         string `json:"xtermScreen"`
		ScreenDiff          string `json:"screenDiff"`
		RingBufferFrontend  string `json:"ringBufferFrontend"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	// Write the frontend files to the diagnostic directory
	os.WriteFile(filepath.Join(req.DiagDir, "screen-xterm.txt"), []byte(req.XtermScreen), 0o644)
	os.WriteFile(filepath.Join(req.DiagDir, "screen-diff.txt"), []byte(req.ScreenDiff), 0o644)
	os.WriteFile(filepath.Join(req.DiagDir, "ringbuffer-frontend.txt"), []byte(req.RingBufferFrontend), 0o644)
	w.WriteHeader(http.StatusOK)
}
```

Register in `server.go` inside the `if s.devMode` block:

```go
mux.HandleFunc("/api/dev/diagnostic-append", s.handleDiagnosticAppend)
```

**Step 2: Implement the frontend response handler**

In the `onDiagnosticResponse` callback in `terminalStream.ts`:

```typescript
// When diagnostic response arrives from backend:
// 1. Extract xterm.js screen buffer
// 2. Compute diff against tmux screen from backend response
// 3. Post frontend files back to backend to append to diagnostic dir
const xtermScreen = extractScreenText(this.terminal.buffer.active);
const diff = computeScreenDiff(msg.tmuxScreen, xtermScreen);
const frontendRingBuffer = this.diagnostics
  ? new TextDecoder().decode(this.diagnostics.ringBufferSnapshot())
  : '';

fetch('/api/dev/diagnostic-append', {
  method: 'POST',
  headers: { 'Content-Type': 'application/json' },
  body: JSON.stringify({
    diagDir: msg.diagDir,
    xtermScreen,
    screenDiff: diff.diffText,
    ringBufferFrontend: frontendRingBuffer,
  }),
});
```

**Step 3: Run all tests**

```bash
go test ./internal/dashboard/ -v
cd assets/dashboard && npx vitest run
```

Expected: All PASS.

**Step 4: Commit**

```bash
git commit -m "feat(diagnostics): complete diagnostic capture flow with frontend file append"
```

---

### Task 12: End-to-End Integration Test

Write a test that verifies the full diagnostic flow from button press to file output.

**Files:**

- Create: `internal/dashboard/diagnostic_integration_test.go` — verifies the file writing and struct serialization

**Step 1: Write the integration test**

```go
func TestDiagnosticCapture_FullFlow(t *testing.T) {
	dir := t.TempDir()
	diag := &DiagnosticCapture{
		Timestamp:   time.Now(),
		SessionID:   "integration-test",
		Cols:        80,
		Rows:        24,
		Counters:    map[string]int64{"eventsDelivered": 500, "eventsDropped": 2, "bytesDelivered": 100000, "controlModeReconnects": 1},
		TmuxScreen:  "$ ls\nfile1.txt\nfile2.txt\n",
		RingBuffer:  []byte("\033[1m$ ls\033[0m\nfile1.txt\nfile2.txt\n"),
		Findings:    []string{"2 events dropped"},
		Verdict:     "Events were dropped due to channel backpressure.",
		DiffSummary: "1 row differs",
	}
	if err := diag.WriteToDir(dir); err != nil {
		t.Fatal(err)
	}
	// Verify meta.json content
	data, _ := os.ReadFile(filepath.Join(dir, "meta.json"))
	var meta map[string]interface{}
	json.Unmarshal(data, &meta)
	counters := meta["counters"].(map[string]interface{})
	if int(counters["eventsDropped"].(float64)) != 2 {
		t.Errorf("eventsDropped = %v, want 2", counters["eventsDropped"])
	}
	// Verify raw files are not base64
	tmuxData, _ := os.ReadFile(filepath.Join(dir, "screen-tmux.txt"))
	if !strings.Contains(string(tmuxData), "$ ls") {
		t.Error("screen-tmux.txt should contain raw text")
	}
	rbData, _ := os.ReadFile(filepath.Join(dir, "ringbuffer-backend.txt"))
	if !strings.Contains(string(rbData), "\033[1m") {
		t.Error("ringbuffer-backend.txt should contain raw ANSI sequences")
	}
}
```

**Step 2: Run test**

```bash
go test ./internal/dashboard/ -run TestDiagnosticCapture_FullFlow -v
```

Expected: PASS

**Step 3: Commit**

```bash
git commit -m "test(diagnostics): add integration test for diagnostic file writing"
```

---

### Task 13: Final Verification and Cleanup

Run the full test suite, format code, and verify everything works together.

**Step 1: Run all Go tests**

```bash
go test ./...
```

Expected: All PASS.

**Step 2: Run all frontend tests**

```bash
cd assets/dashboard && npx vitest run
```

Expected: All PASS.

**Step 3: Format code**

```bash
./format.sh
```

**Step 4: Build the dashboard**

```bash
go run ./cmd/build-dashboard
```

Expected: Build succeeds.

**Step 5: Build the binary**

```bash
go build ./cmd/schmux
```

Expected: Build succeeds.

**Step 6: Run full test suite**

```bash
./test.sh --all
```

Expected: All PASS.

**Step 7: Commit any formatting changes**

```bash
git commit -m "chore: format code"
```
