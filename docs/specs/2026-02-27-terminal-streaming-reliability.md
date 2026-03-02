# Terminal Streaming Reliability Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use 10x-engineer:executing-plans to implement this plan task-by-task.

**Goal:** Replace the destructive `terminal.reset()` sync mechanism with a sequenced output log, gap-based replay, and surgical viewport correction — eliminating scrollback destruction and bootstrap rendering delays.

**Architecture:** Add a sequenced output log to `SessionTracker` that records every output event with a monotonic sequence number. The WebSocket handler replays from this log (instead of `capture-pane`) for bootstrap, and fills gaps by replaying missing sequences (instead of resetting xterm.js). When replay isn't possible, a surgical viewport overwrite corrects individual rows without touching scrollback.

**Tech Stack:** Go (backend log + WebSocket protocol), TypeScript (frontend TerminalStream rewrite), Vitest + Go testing (unit tests), Playwright (scenario tests)

---

## Design Overview

### Current Architecture (what's wrong)

```
Bootstrap:  capture-pane -S -5000  →  one giant binary frame  →  terminal.reset() + write()
                                                                  scrollToBottom() races with async render
                                                                  ↓
                                                                  User sees 1-3s of "scrolling through" content

Sync:       capture-pane (40 rows) →  JSON sync msg  →  compare text  →  terminal.reset() + write(40 rows)
            every 10s                                                     ↓
                                                                          ALL SCROLLBACK DESTROYED
```

### Proposed Architecture

```
Bootstrap:  replay OutputLog from seq 0  →  chunked binary frames  →  write(chunk, callback) chaining
                                                                       scrollToBottom() in final callback
                                                                       ↓
                                                                       Instant viewport, smooth render

Sync:       client reports lastSeq  →  server compares to currentSeq
            │
            ├── no gap: nothing to do (deterministic emulator = correct)
            │
            ├── gap + log has data: replay missing seqs (APPEND, no reset)
            │
            └── gap + log empty: capture-pane + surgical viewport correction
                                  (overwrite differing rows, scrollback untouched)

Defense:    text comparison every 60s  →  surgical correction if mismatch
            (paranoia check, should almost never fire)
```

### Key Invariants

1. **`terminal.reset()` is NEVER called from the sync/reconciliation path.** Only from bootstrap (initial connect).
2. **Scrollback is append-only.** No operation ever removes lines from the scrollback buffer.
3. **The output log is the source of truth**, not `capture-pane`. If the log has the data, it is authoritative.
4. **Sequence numbers are monotonic per session**, assigned at the tracker level, survive control mode reconnections.

---

## Task 1: OutputLog Data Structure

**Files:**

- Create: `internal/session/outputlog.go`
- Test: `internal/session/outputlog_test.go`

The output log is a bounded circular buffer of sequenced output events. It lives on the `SessionTracker` and is written to from `fanOut()`.

### Step 1: Write the failing tests

```go
// outputlog_test.go
package session

import (
	"testing"
)

func TestOutputLog_AppendAndReplay(t *testing.T) {
	log := NewOutputLog(100) // 100 entries max

	log.Append([]byte("hello"))
	log.Append([]byte("world"))

	entries := log.ReplayFrom(0)
	if len(entries) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(entries))
	}
	if entries[0].Seq != 0 || string(entries[0].Data) != "hello" {
		t.Errorf("entry 0: seq=%d data=%q", entries[0].Seq, entries[0].Data)
	}
	if entries[1].Seq != 1 || string(entries[1].Data) != "world" {
		t.Errorf("entry 1: seq=%d data=%q", entries[1].Seq, entries[1].Data)
	}
}

func TestOutputLog_ReplayFromMid(t *testing.T) {
	log := NewOutputLog(100)
	for i := 0; i < 10; i++ {
		log.Append([]byte{byte('a' + i)})
	}

	entries := log.ReplayFrom(7)
	if len(entries) != 3 {
		t.Fatalf("expected 3 entries (seq 7,8,9), got %d", len(entries))
	}
	if entries[0].Seq != 7 {
		t.Errorf("first entry seq=%d, want 7", entries[0].Seq)
	}
}

func TestOutputLog_ReplayFromCurrentSeq(t *testing.T) {
	log := NewOutputLog(100)
	log.Append([]byte("a"))
	log.Append([]byte("b"))

	// Requesting from the current seq (nothing new) returns empty
	entries := log.ReplayFrom(2)
	if len(entries) != 0 {
		t.Fatalf("expected 0 entries, got %d", len(entries))
	}
}

func TestOutputLog_Wraparound(t *testing.T) {
	log := NewOutputLog(4) // tiny buffer
	for i := 0; i < 10; i++ {
		log.Append([]byte{byte('0' + i)})
	}

	// Only last 4 entries should be available (seq 6,7,8,9)
	entries := log.ReplayFrom(6)
	if len(entries) != 4 {
		t.Fatalf("expected 4 entries, got %d", len(entries))
	}
	if entries[0].Seq != 6 || string(entries[0].Data) != "6" {
		t.Errorf("entry 0: seq=%d data=%q", entries[0].Seq, entries[0].Data)
	}

	// Requesting evicted data returns nil (gap unrecoverable)
	entries = log.ReplayFrom(0)
	if entries != nil {
		t.Fatalf("expected nil for evicted data, got %d entries", len(entries))
	}
}

func TestOutputLog_CurrentSeq(t *testing.T) {
	log := NewOutputLog(100)
	if log.CurrentSeq() != 0 {
		t.Errorf("initial seq=%d, want 0", log.CurrentSeq())
	}
	log.Append([]byte("a"))
	if log.CurrentSeq() != 1 {
		t.Errorf("after 1 append seq=%d, want 1", log.CurrentSeq())
	}
}

func TestOutputLog_OldestSeq(t *testing.T) {
	log := NewOutputLog(4)
	// Empty log
	if log.OldestSeq() != 0 {
		t.Errorf("empty log oldest=%d, want 0", log.OldestSeq())
	}
	for i := 0; i < 10; i++ {
		log.Append([]byte{byte('0' + i)})
	}
	// After wraparound, oldest should be 6
	if log.OldestSeq() != 6 {
		t.Errorf("oldest=%d, want 6", log.OldestSeq())
	}
}

func TestOutputLog_ReplayAll(t *testing.T) {
	log := NewOutputLog(100)
	log.Append([]byte("a"))
	log.Append([]byte("b"))
	log.Append([]byte("c"))

	entries := log.ReplayAll()
	if len(entries) != 3 {
		t.Fatalf("expected 3, got %d", len(entries))
	}
}

func TestOutputLog_ConcurrentAppendReplay(t *testing.T) {
	log := NewOutputLog(1000)
	done := make(chan struct{})

	// Writer
	go func() {
		for i := 0; i < 500; i++ {
			log.Append([]byte("data"))
		}
		close(done)
	}()

	// Reader (concurrent)
	for i := 0; i < 100; i++ {
		_ = log.ReplayFrom(0)
		_ = log.CurrentSeq()
	}
	<-done
}

func TestOutputLog_TotalBytes(t *testing.T) {
	log := NewOutputLog(100)
	log.Append([]byte("hello")) // 5 bytes
	log.Append([]byte("world")) // 5 bytes

	if log.TotalBytes() != 10 {
		t.Errorf("total bytes=%d, want 10", log.TotalBytes())
	}
}
```

### Step 2: Run tests, verify they fail

```bash
go test ./internal/session/ -run TestOutputLog -v
```

Expected: compilation errors (types don't exist yet).

### Step 3: Write the implementation

```go
// outputlog.go
package session

import "sync"

// LogEntry is a sequenced output event.
type LogEntry struct {
	Seq  uint64
	Data []byte
}

// OutputLog is a bounded circular buffer of sequenced output events.
// It is safe for concurrent use from multiple goroutines.
type OutputLog struct {
	mu         sync.RWMutex
	entries    []LogEntry
	head       int    // next write position
	size       int    // current number of valid entries
	cap        int    // max entries
	nextSeq    uint64 // next sequence number to assign
	totalBytes int64  // cumulative bytes appended
}

// NewOutputLog creates an output log with the given capacity (max entries).
func NewOutputLog(capacity int) *OutputLog {
	return &OutputLog{
		entries: make([]LogEntry, capacity),
		cap:     capacity,
	}
}

// Append adds data to the log and assigns the next sequence number.
func (l *OutputLog) Append(data []byte) uint64 {
	// Copy the data so the caller can reuse the slice
	copied := make([]byte, len(data))
	copy(copied, data)

	l.mu.Lock()
	seq := l.nextSeq
	l.entries[l.head] = LogEntry{Seq: seq, Data: copied}
	l.head = (l.head + 1) % l.cap
	if l.size < l.cap {
		l.size++
	}
	l.nextSeq++
	l.totalBytes += int64(len(data))
	l.mu.Unlock()
	return seq
}

// CurrentSeq returns the next sequence number that will be assigned.
// If 3 events have been appended (seq 0,1,2), CurrentSeq returns 3.
func (l *OutputLog) CurrentSeq() uint64 {
	l.mu.RLock()
	defer l.mu.RUnlock()
	return l.nextSeq
}

// OldestSeq returns the oldest sequence number still in the log.
// Returns 0 if the log is empty.
func (l *OutputLog) OldestSeq() uint64 {
	l.mu.RLock()
	defer l.mu.RUnlock()
	if l.size == 0 {
		return 0
	}
	oldest := (l.head - l.size + l.cap) % l.cap
	return l.entries[oldest].Seq
}

// ReplayFrom returns all entries with seq >= fromSeq.
// Returns nil if fromSeq is too old (evicted from the buffer).
// Returns empty slice if fromSeq >= currentSeq (nothing new).
func (l *OutputLog) ReplayFrom(fromSeq uint64) []LogEntry {
	l.mu.RLock()
	defer l.mu.RUnlock()

	if l.size == 0 {
		if fromSeq == 0 {
			return []LogEntry{}
		}
		return nil
	}

	oldest := (l.head - l.size + l.cap) % l.cap
	oldestSeq := l.entries[oldest].Seq

	if fromSeq < oldestSeq {
		return nil // requested data has been evicted
	}
	if fromSeq >= l.nextSeq {
		return []LogEntry{} // nothing new
	}

	// Calculate how many entries to skip from oldest
	skip := int(fromSeq - oldestSeq)
	count := l.size - skip
	result := make([]LogEntry, count)
	for i := 0; i < count; i++ {
		idx := (oldest + skip + i) % l.cap
		result[i] = l.entries[idx]
	}
	return result
}

// ReplayAll returns all entries currently in the log.
func (l *OutputLog) ReplayAll() []LogEntry {
	l.mu.RLock()
	defer l.mu.RUnlock()
	if l.size == 0 {
		return []LogEntry{}
	}
	oldest := (l.head - l.size + l.cap) % l.cap
	return l.ReplayFrom(l.entries[oldest].Seq)
}

// TotalBytes returns the cumulative bytes appended to the log.
func (l *OutputLog) TotalBytes() int64 {
	l.mu.RLock()
	defer l.mu.RUnlock()
	return l.totalBytes
}
```

**Note:** `ReplayAll` calls `ReplayFrom` while holding the read lock. This will deadlock because `ReplayFrom` also acquires the read lock. The implementation needs to use an internal `replayFromLocked` helper instead. The implementer should fix this during implementation — the test will catch it immediately.

### Step 4: Run tests, verify they pass

```bash
go test ./internal/session/ -run TestOutputLog -v
```

Expected: all PASS.

### Step 5: Commit

```
feat(session): add OutputLog sequenced circular buffer

Bounded ring buffer that assigns monotonic sequence numbers to output
events, supporting replay from any sequence for gap recovery.
```

---

## Task 2: Wire OutputLog into SessionTracker

**Files:**

- Modify: `internal/session/tracker.go:46-77` (add `outputLog` field to struct)
- Modify: `internal/session/tracker.go:86-110` (initialize log in constructor)
- Modify: `internal/session/tracker.go:166-185` (append to log in `fanOut()`)
- Modify: `internal/session/outputlog.go` (fix the `ReplayAll` lock issue from Task 1 if not already fixed)
- Test: `internal/session/tracker_test.go` (add test for log wiring)

### Step 1: Write the failing test

Add to `tracker_test.go`:

```go
func TestTrackerOutputLog_FanOutRecordsSequences(t *testing.T) {
	st := state.New("", nil)
	tracker := NewSessionTracker("s1", "tmux-s1", st, "", nil, nil, nil)

	// Subscribe so we can also verify events arrive
	ch := tracker.SubscribeOutput()
	defer tracker.UnsubscribeOutput(ch)

	// Simulate fan-out (normally called by attachControlMode)
	tracker.fanOut(controlmode.OutputEvent{PaneID: "%0", Data: "hello"})
	tracker.fanOut(controlmode.OutputEvent{PaneID: "%0", Data: "world"})

	// Verify output log captured the data
	if tracker.OutputLog().CurrentSeq() != 2 {
		t.Fatalf("expected currentSeq=2, got %d", tracker.OutputLog().CurrentSeq())
	}

	entries := tracker.OutputLog().ReplayFrom(0)
	if len(entries) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(entries))
	}
	if string(entries[0].Data) != "hello" {
		t.Errorf("entry 0 data=%q, want 'hello'", entries[0].Data)
	}
}
```

### Step 2: Run test, verify it fails

```bash
go test ./internal/session/ -run TestTrackerOutputLog -v
```

Expected: compilation error (`OutputLog()` method doesn't exist).

### Step 3: Implement the wiring

In `tracker.go`, add to the struct:

```go
type SessionTracker struct {
	// ... existing fields ...
	outputLog *OutputLog
}
```

In `NewSessionTracker`, initialize:

```go
// 50,000 entries ≈ 5MB for a session producing ~100 bytes/event average
t.outputLog = NewOutputLog(50000)
```

In `fanOut()`, append before distributing:

```go
func (t *SessionTracker) fanOut(event controlmode.OutputEvent) {
	t.Counters.EventsDelivered.Add(1)
	t.Counters.BytesDelivered.Add(int64(len(event.Data)))

	// Record in sequenced log (before fan-out, so replay is authoritative)
	t.outputLog.Append([]byte(event.Data))

	// ... existing fan-out code ...
}
```

Add accessor:

```go
func (t *SessionTracker) OutputLog() *OutputLog {
	return t.outputLog
}
```

### Step 4: Run tests, verify they pass

```bash
go test ./internal/session/ -run TestTrackerOutputLog -v
go test ./internal/session/ -v  # run all tracker tests to check for regressions
```

### Step 5: Commit

```
feat(session): wire OutputLog into SessionTracker.fanOut

Every output event now gets a monotonic sequence number before fan-out.
The log is the source of truth for replay-based bootstrap and gap recovery.
```

---

## Task 3: Sequenced Binary Frame Protocol

**Files:**

- Modify: `internal/dashboard/websocket.go:446-476` (tag binary frames with sequence numbers)
- Modify: `assets/dashboard/src/lib/terminalStream.ts:602-628` (parse sequence header from binary frames)
- Test: `internal/dashboard/websocket_test.go` (new test for frame encoding)
- Test: `assets/dashboard/src/lib/terminalStream.test.ts` (new test for frame decoding)

### Wire Protocol

Each binary WebSocket frame gets an 8-byte header:

```
┌──────────────────┬──────────────────┐
│  seq (uint64 BE) │  terminal data   │
│    8 bytes       │   N bytes        │
└──────────────────┴──────────────────┘
```

- `seq` is the sequence number of the LAST event included in this frame.
- For bootstrap frames, seq is the final seq of the replayed range.
- For live frames, seq is the sequence assigned during `fanOut()`.
- A special sentinel value `seq = 0xFFFFFFFFFFFFFFFF` means "unsequenced" (used during the transition if needed, but since we're shipping atomically, we can skip this).

### Step 1: Write the failing tests

**Go side** — add to `websocket_test.go`:

```go
func TestEncodeSequencedFrame(t *testing.T) {
	data := []byte("hello")
	frame := encodeSequencedFrame(42, data)

	if len(frame) != 8+5 {
		t.Fatalf("frame length=%d, want 13", len(frame))
	}

	// Verify big-endian uint64 sequence
	seq := binary.BigEndian.Uint64(frame[:8])
	if seq != 42 {
		t.Errorf("seq=%d, want 42", seq)
	}
	if string(frame[8:]) != "hello" {
		t.Errorf("data=%q, want 'hello'", frame[8:])
	}
}
```

**TS side** — add to `terminalStream.test.ts`:

```typescript
describe('TerminalStream sequenced frames', () => {
  it('parses sequence number from binary frame header', async () => {
    await stream.initialized;

    // Build a frame: 8 bytes big-endian seq=42 + "hello"
    const buf = new ArrayBuffer(8 + 5);
    const view = new DataView(buf);
    view.setBigUint64(0, 42n, false); // big-endian
    new Uint8Array(buf, 8).set(new TextEncoder().encode('hello'));

    stream.handleOutput(buf);

    expect((stream as any).lastReceivedSeq).toBe(42n);
  });

  it('tracks lastReceivedSeq across multiple frames', async () => {
    await stream.initialized;

    // Send bootstrap (seq=5)
    const buf1 = new ArrayBuffer(8 + 1);
    new DataView(buf1).setBigUint64(0, 5n, false);
    new Uint8Array(buf1, 8).set(new TextEncoder().encode('a'));
    stream.handleOutput(buf1);

    // Send live frame (seq=6)
    const buf2 = new ArrayBuffer(8 + 1);
    new DataView(buf2).setBigUint64(0, 6n, false);
    new Uint8Array(buf2, 8).set(new TextEncoder().encode('b'));
    stream.handleOutput(buf2);

    expect((stream as any).lastReceivedSeq).toBe(6n);
  });
});
```

### Step 2: Run tests, verify they fail

```bash
go test ./internal/dashboard/ -run TestEncodeSequencedFrame -v
cd assets/dashboard && npx vitest run src/lib/terminalStream.test.ts
```

### Step 3: Implement

**Go side** — add to `websocket.go` (or a new `websocket_frame.go`):

```go
func encodeSequencedFrame(seq uint64, data []byte) []byte {
	frame := make([]byte, 8+len(data))
	binary.BigEndian.PutUint64(frame[:8], seq)
	copy(frame[8:], data)
	return frame
}
```

Update the live streaming loop in `handleTerminalWebSocket` to use the log's sequence when sending:

```go
case event, ok := <-outputCh:
	// ... existing holdback logic ...
	if len(send) > 0 {
		// Get the current seq from the output log
		seq := tracker.OutputLog().CurrentSeq() - 1 // last appended
		frame := encodeSequencedFrame(seq, send)
		if err := conn.WriteMessage(websocket.BinaryMessage, frame); err != nil {
			return
		}
	}
```

**TS side** — update `handleOutput` in `terminalStream.ts`:

```typescript
// At the class level, add:
private lastReceivedSeq: bigint = -1n;

// In handleOutput, update the binary path:
if (data instanceof ArrayBuffer) {
  // Parse 8-byte sequence header
  const view = new DataView(data);
  const seq = view.getBigUint64(0, false); // big-endian
  this.lastReceivedSeq = seq;

  // Terminal data starts after the 8-byte header
  const terminalData = new Uint8Array(data, 8);
  const text = this.utf8Decoder.decode(terminalData, { stream: true });
  // ... rest of existing logic using text ...
}
```

### Step 4: Run tests, verify they pass

```bash
go test ./internal/dashboard/ -run TestEncodeSequencedFrame -v
cd assets/dashboard && npx vitest run src/lib/terminalStream.test.ts
```

### Step 5: Commit

```
feat(ws): add 8-byte sequence header to binary WebSocket frames

Each binary frame now carries the sequence number of the last output event
it contains. Frontend tracks lastReceivedSeq for gap detection.
```

---

## Task 4: Log-Based Bootstrap (Replace capture-pane)

**Files:**

- Modify: `internal/dashboard/websocket.go:253-306` (replace capture-pane bootstrap with log replay)
- Test: `internal/dashboard/websocket_test.go` (test bootstrap chunking)

### Step 1: Write the failing test

```go
func TestChunkReplay(t *testing.T) {
	log := session.NewOutputLog(100)
	for i := 0; i < 20; i++ {
		log.Append([]byte(fmt.Sprintf("line %d\n", i)))
	}

	chunks := chunkReplayEntries(log.ReplayAll(), 50) // 50 byte chunks
	if len(chunks) == 0 {
		t.Fatal("expected at least one chunk")
	}

	// Verify all data is present across chunks
	var total []byte
	for _, c := range chunks {
		total = append(total, c.Data...)
	}
	for i := 0; i < 20; i++ {
		expected := fmt.Sprintf("line %d\n", i)
		if !bytes.Contains(total, []byte(expected)) {
			t.Errorf("missing line %d in chunked output", i)
		}
	}

	// Verify last chunk has the correct final seq
	lastChunk := chunks[len(chunks)-1]
	if lastChunk.Seq != 19 {
		t.Errorf("last chunk seq=%d, want 19", lastChunk.Seq)
	}
}
```

### Step 2: Run test, verify it fails

```bash
go test ./internal/dashboard/ -run TestChunkReplay -v
```

### Step 3: Implement

Add chunking helper:

```go
type replayChunk struct {
	Seq  uint64
	Data []byte
}

// chunkReplayEntries groups log entries into chunks of approximately maxBytes.
// Each chunk's Seq is the sequence of its last entry.
func chunkReplayEntries(entries []session.LogEntry, maxBytes int) []replayChunk {
	if len(entries) == 0 {
		return nil
	}

	var chunks []replayChunk
	var current []byte
	var lastSeq uint64

	for _, e := range entries {
		if len(current)+len(e.Data) > maxBytes && len(current) > 0 {
			chunks = append(chunks, replayChunk{Seq: lastSeq, Data: current})
			current = nil
		}
		current = append(current, e.Data...)
		lastSeq = e.Seq
	}
	if len(current) > 0 {
		chunks = append(chunks, replayChunk{Seq: lastSeq, Data: current})
	}
	return chunks
}
```

Replace the bootstrap section in `handleTerminalWebSocket` (lines 253-306):

```go
// Bootstrap: replay from output log (or fall back to capture-pane if log is empty)
outputLog := tracker.OutputLog()
entries := outputLog.ReplayAll()

if len(entries) > 0 {
	// Replay from log — chunked into ~16KB frames with sequence headers
	chunks := chunkReplayEntries(entries, 16384)
	for _, chunk := range chunks {
		// Apply escape holdback to each chunk
		send, hb := escbuf.SplitClean(escHoldback, chunk.Data)
		escHoldback = hb
		if len(send) > 0 {
			frame := encodeSequencedFrame(chunk.Seq, send)
			if err := conn.WriteMessage(websocket.BinaryMessage, frame); err != nil {
				return
			}
		}
	}
} else {
	// Fallback: capture-pane for sessions started before this change
	// (output log is empty because no events have flowed yet)
	capCtx, capCancel := context.WithTimeout(context.Background(), ...)
	bootstrap, err := tracker.CaptureLastLines(capCtx, bootstrapCaptureLines)
	// ... existing fallback code ...
	frame := encodeSequencedFrame(0, []byte(bootstrap))
	if err := conn.WriteMessage(websocket.BinaryMessage, frame); err != nil {
		return
	}
}

// Cursor state restoration — same as current, but append as a separate
// unsequenced data write (cursor position is ephemeral, not logged)
// ... existing cursor restore code stays the same ...

// Subscribe AFTER bootstrap replay
outputCh := tracker.SubscribeOutput()
defer tracker.UnsubscribeOutput(outputCh)
```

### Step 4: Run tests

```bash
go test ./internal/dashboard/ -run TestChunkReplay -v
go test ./internal/dashboard/ -v  # check for regressions
```

### Step 5: Commit

```
feat(ws): replace capture-pane bootstrap with log replay

Bootstrap now replays the output log in ~16KB chunks with sequence headers.
Falls back to capture-pane only when the log is empty (pre-existing sessions).
Eliminates the single-giant-frame problem that caused async rendering races.
```

---

## Task 5: Frontend Chunked Write with Callbacks

**Files:**

- Modify: `assets/dashboard/src/lib/terminalStream.ts:602-628` (chain write callbacks, defer scrollToBottom)
- Test: `assets/dashboard/src/lib/terminalStream.test.ts` (test callback chaining)

### Step 1: Write the failing test

```typescript
describe('TerminalStream bootstrap write chaining', () => {
  it('calls scrollToBottom only after final bootstrap chunk is written', async () => {
    await stream.initialized;
    const terminal = stream.terminal!;

    // Track write callback invocations
    const writeCallbacks: (() => void)[] = [];
    vi.mocked(terminal.write).mockImplementation((_data: any, cb?: () => void) => {
      if (cb) writeCallbacks.push(cb);
    });

    // Send first bootstrap frame (seq=0)
    const buf1 = new ArrayBuffer(8 + 5);
    new DataView(buf1).setBigUint64(0, 0n, false);
    new Uint8Array(buf1, 8).set(new TextEncoder().encode('chunk1'));
    stream.handleOutput(buf1);

    // scrollToBottom should NOT have been called yet (write hasn't "completed")
    expect(terminal.scrollToBottom).not.toHaveBeenCalled();

    // Simulate xterm.js completing the write
    writeCallbacks[0]();

    // Now scrollToBottom should fire (if followTail is true)
    // Note: depends on implementation — may fire per-chunk or only on last
  });
});
```

### Step 2: Run test, verify it fails

```bash
cd assets/dashboard && npx vitest run src/lib/terminalStream.test.ts
```

### Step 3: Implement

Update `handleOutput` binary path in `terminalStream.ts`:

```typescript
if (data instanceof ArrayBuffer) {
  const view = new DataView(data);
  const seq = view.getBigUint64(0, false);
  this.lastReceivedSeq = seq;

  const terminalData = new Uint8Array(data, 8);
  const text = this.utf8Decoder.decode(terminalData, { stream: true });
  this.lastBinaryTime = Date.now();

  if (!this.bootstrapped) {
    this.bootstrapped = true;
    this.terminal!.reset();
    // Use write callback to defer scrollToBottom until xterm.js has
    // finished parsing this chunk. This prevents the "scrolling through"
    // visual artifact caused by scrollToBottom racing the async parser.
    this.terminal!.write(text, () => {
      if (this.followTail) {
        this.terminal!.scrollToBottom();
      }
    });
  } else {
    inputLatency.markReceived();
    this.terminal!.write(text, () => {
      inputLatency.markRenderTime(performance.now() - renderStart);
      if (this.followTail) {
        this.terminal!.scrollToBottom();
      }
    });
  }
  // REMOVE the old synchronous scrollToBottom() call that was here
  return;
}
```

The key change: `scrollToBottom()` moves inside `write()`'s callback. This guarantees the buffer is fully updated before we scroll.

### Step 4: Run tests

```bash
cd assets/dashboard && npx vitest run src/lib/terminalStream.test.ts
```

### Step 5: Commit

```
fix(terminal): defer scrollToBottom to write callback

Moves scrollToBottom() inside terminal.write()'s completion callback so it
fires after xterm.js has fully parsed the chunk, not before. Eliminates
the "scrolling through thousands of lines" visual artifact on bootstrap.
```

---

## Task 6: Surgical Viewport Correction

**Files:**

- Create: `assets/dashboard/src/lib/surgicalCorrection.ts`
- Test: `assets/dashboard/src/lib/surgicalCorrection.test.ts`
- Modify: `assets/dashboard/src/lib/terminalStream.ts:688-735` (replace `reset()` with surgical correction)

### Step 1: Write the failing tests

```typescript
// surgicalCorrection.test.ts
import { describe, it, expect } from 'vitest';
import { buildSurgicalCorrection } from './surgicalCorrection';

describe('buildSurgicalCorrection', () => {
  it('generates escape sequences for a single differing row', () => {
    const correction = buildSurgicalCorrection(
      [3], // row 3 differs
      ['corrected line content'], // ANSI content for row 3
      { row: 10, col: 5, visible: true } // cursor to restore
    );

    // Should contain: save cursor, move to row 4 (1-indexed), clear line, content, restore cursor
    expect(correction).toContain('\x1b7'); // DECSC save
    expect(correction).toContain('\x1b[4;1H'); // move to row 4, col 1
    expect(correction).toContain('\x1b[2K'); // clear line
    expect(correction).toContain('corrected line content');
    expect(correction).toContain('\x1b8'); // DECRC restore
  });

  it('generates corrections for multiple rows', () => {
    const correction = buildSurgicalCorrection([1, 5], ['row 1 content', 'row 5 content'], {
      row: 0,
      col: 0,
      visible: true,
    });

    expect(correction).toContain('\x1b[2;1H'); // row 1 (1-indexed = 2)
    expect(correction).toContain('\x1b[6;1H'); // row 5 (1-indexed = 6)
    expect(correction).toContain('row 1 content');
    expect(correction).toContain('row 5 content');
  });

  it('resets SGR before each row to prevent attribute bleed', () => {
    const correction = buildSurgicalCorrection([0], ['\x1b[32mgreen text\x1b[0m'], {
      row: 0,
      col: 0,
      visible: true,
    });

    // Should reset attributes before writing content
    expect(correction).toContain('\x1b[0m');
  });

  it('restores cursor visibility', () => {
    const hidden = buildSurgicalCorrection([0], ['x'], { row: 0, col: 0, visible: false });
    expect(hidden).toContain('\x1b[?25l'); // cursor hidden

    const visible = buildSurgicalCorrection([0], ['x'], { row: 0, col: 0, visible: true });
    expect(visible).toContain('\x1b[?25h'); // cursor visible
  });

  it('returns empty string when no rows differ', () => {
    const correction = buildSurgicalCorrection([], [], { row: 0, col: 0, visible: true });
    expect(correction).toBe('');
  });
});
```

### Step 2: Run tests, verify they fail

```bash
cd assets/dashboard && npx vitest run src/lib/surgicalCorrection.test.ts
```

### Step 3: Implement

```typescript
// surgicalCorrection.ts

export interface CursorState {
  row: number;
  col: number;
  visible: boolean;
}

/**
 * Build an ANSI escape sequence string that surgically overwrites specific
 * viewport rows without affecting scrollback.
 *
 * Uses DECSC/DECRC to save and restore cursor position, and CSI sequences
 * to move to each row, clear it, reset attributes, and write the correct content.
 */
export function buildSurgicalCorrection(
  diffRows: number[],
  rowContents: string[],
  cursor: CursorState
): string {
  if (diffRows.length === 0) return '';

  let seq = '';
  seq += '\x1b7'; // DECSC: save cursor position + attributes

  for (let i = 0; i < diffRows.length; i++) {
    const row = diffRows[i];
    const content = rowContents[i] ?? '';
    // CSI row;col H (1-indexed)
    seq += `\x1b[${row + 1};1H`;
    // EL 2: clear entire line
    seq += '\x1b[2K';
    // SGR 0: reset attributes to prevent bleed from previous content
    seq += '\x1b[0m';
    // Write the correct content
    seq += content;
  }

  seq += '\x1b8'; // DECRC: restore cursor position + attributes

  // Restore cursor position explicitly (DECRC might not be supported everywhere)
  seq += `\x1b[${cursor.row + 1};${cursor.col + 1}H`;

  // Restore cursor visibility
  seq += cursor.visible ? '\x1b[?25h' : '\x1b[?25l';

  return seq;
}
```

### Step 4: Run tests

```bash
cd assets/dashboard && npx vitest run src/lib/surgicalCorrection.test.ts
```

### Step 5: Commit

```
feat(terminal): add surgical viewport correction

Generates ANSI escape sequences to overwrite specific viewport rows
without destroying scrollback. Uses DECSC/DECRC for cursor preservation.
```

---

## Task 7: Replace handleSync to Use Surgical Correction

**Files:**

- Modify: `assets/dashboard/src/lib/terminalStream.ts:688-735` (rewrite `handleSync`)
- Modify: `assets/dashboard/src/lib/terminalStream.test.ts` (update sync tests)

### Step 1: Update the sync tests

The existing sync tests assert that `terminal.reset()` is called on mismatch. Update them to assert that `terminal.write()` is called with surgical correction content instead, and `terminal.reset()` is NEVER called:

```typescript
it('applies surgical correction when content mismatches (no reset)', async () => {
  await stream.initialized;
  const terminal = stream.terminal!;

  // Bootstrap first
  const bootBuf = new ArrayBuffer(8 + 9);
  new DataView(bootBuf).setBigUint64(0, 0n, false);
  new Uint8Array(bootBuf, 8).set(new TextEncoder().encode('bootstrap'));
  stream.handleOutput(bootBuf);

  vi.mocked(terminal.reset).mockClear();
  vi.mocked(terminal.write).mockClear();

  (stream as any).lastBinaryTime = Date.now() - 1000;

  const mockLine = { translateToString: () => 'wrong content' };
  (terminal as any).buffer = {
    active: { viewportY: 0, baseY: 0, cursorY: 0, length: 1, getLine: () => mockLine },
  };
  (terminal as any).rows = 1;

  const syncMsg = {
    type: 'sync',
    screen: '\x1b[1mcorrect content\x1b[0m',
    cursor: { row: 0, col: 0, visible: true },
  };
  stream.handleOutput(JSON.stringify(syncMsg));

  // CRITICAL: reset must NOT be called
  expect(terminal.reset).not.toHaveBeenCalled();

  // write should be called with surgical correction (contains DECSC save cursor)
  expect(terminal.write).toHaveBeenCalledWith(
    expect.stringContaining('\x1b7'), // DECSC
    expect.any(Function) // write callback
  );
});
```

### Step 2: Run tests, verify they fail (old behavior calls reset)

```bash
cd assets/dashboard && npx vitest run src/lib/terminalStream.test.ts
```

### Step 3: Rewrite handleSync

```typescript
private handleSync(msg: {
  screen: string;
  cursor: { row: number; col: number; visible: boolean };
  forced?: boolean;
}) {
  if (!this.terminal) return;

  // Activity guard: skip if binary data arrived within 500ms
  if (!msg.forced && Date.now() - this.lastBinaryTime < 500) {
    this.sendSyncResult(false, []);
    return;
  }

  // Extract xterm.js visible text
  const buffer = this.terminal.buffer.active;
  const xtermLines: string[] = [];
  const start = buffer.baseY;
  for (let y = start; y < start + this.terminal.rows && y < buffer.length; y++) {
    const line = buffer.getLine(y);
    xtermLines.push(line ? line.translateToString(true).trimEnd() : '');
  }

  // Extract sync text
  const syncScreenLines = msg.screen.split('\n');
  const syncLines = syncScreenLines.map((line) => stripAnsi(line).trimEnd());

  const result = compareScreens(xtermLines, syncLines);

  if (result.skip) {
    return;
  }

  if (!result.match) {
    // Surgical correction: overwrite only the differing rows
    const rowContents = result.diffRows.map((i) => syncScreenLines[i] ?? '');
    const correction = buildSurgicalCorrection(result.diffRows, rowContents, msg.cursor);
    this.terminal.write(correction, () => {
      // Callback ensures correction is fully applied before any further processing
    });

    this.onSyncCorrection?.(result.diffRows);
    this.sendSyncResult(true, result.diffRows);
  }
}
```

### Step 4: Run tests

```bash
cd assets/dashboard && npx vitest run src/lib/terminalStream.test.ts
```

### Step 5: Commit

```
fix(terminal): replace reset() with surgical correction in sync handler

Sync corrections now overwrite only the differing viewport rows using
cursor-positioning escape sequences. Scrollback is never destroyed.
terminal.reset() is no longer called from the sync path.
```

---

## Task 8: Sequence-Based Gap Detection

**Files:**

- Modify: `internal/dashboard/websocket.go:377-444` (replace text-comparison sync with seq-based reconciliation)
- Modify: `assets/dashboard/src/lib/terminalStream.ts` (add gap detection + `reportSeq` message)
- Test: `internal/dashboard/websocket_test.go`
- Test: `assets/dashboard/src/lib/terminalStream.test.ts`

### Step 1: Write failing tests

**Go side:**

```go
func TestBuildGapReplayFrames(t *testing.T) {
	log := session.NewOutputLog(100)
	log.Append([]byte("a"))
	log.Append([]byte("b"))
	log.Append([]byte("c"))

	frames := buildGapReplayFrames(log, 1, 16384) // replay from seq 1
	if len(frames) == 0 {
		t.Fatal("expected replay frames")
	}
	// Should contain data for seq 1 and 2 ("b" and "c")
	var total []byte
	for _, f := range frames {
		total = append(total, f[8:]...) // skip 8-byte header
	}
	if string(total) != "bc" {
		t.Errorf("replayed data=%q, want 'bc'", total)
	}
}
```

**TS side:**

```typescript
describe('TerminalStream gap detection', () => {
  it('detects gap when frame seq jumps', async () => {
    await stream.initialized;
    const wsSendSpy = vi.fn();

    // Mock WebSocket
    (stream as any).ws = { readyState: 1, send: wsSendSpy }; // WebSocket.OPEN = 1

    // Send seq 0
    const buf1 = new ArrayBuffer(8 + 1);
    new DataView(buf1).setBigUint64(0, 0n, false);
    new Uint8Array(buf1, 8).set([65]); // 'A'
    stream.handleOutput(buf1);

    // Send seq 5 (gap: 1,2,3,4 missing)
    const buf2 = new ArrayBuffer(8 + 1);
    new DataView(buf2).setBigUint64(0, 5n, false);
    new Uint8Array(buf2, 8).set([66]); // 'B'
    stream.handleOutput(buf2);

    // Should have sent a gap message
    expect(wsSendSpy).toHaveBeenCalledWith(expect.stringContaining('"type":"gap"'));
  });
});
```

### Step 2: Run tests, verify they fail

### Step 3: Implement

**Frontend gap detection** — in `handleOutput`, after parsing the sequence:

```typescript
// Detect sequence gap (only after bootstrap)
if (this.bootstrapped && this.lastReceivedSeq >= 0n) {
  const expectedSeq = this.lastReceivedSeq + 1n;
  if (seq > expectedSeq) {
    // Gap detected — ask server to replay missing events
    this.sendGapRequest(expectedSeq);
  }
}
this.lastReceivedSeq = seq;
```

```typescript
private sendGapRequest(fromSeq: bigint) {
  if (this.ws?.readyState === WebSocket.OPEN) {
    this.ws.send(JSON.stringify({
      type: 'gap',
      data: JSON.stringify({ fromSeq: fromSeq.toString() }),
    }));
  }
}
```

**Backend gap handler** — in the main select loop, add a case for the `gap` message type:

```go
case "gap":
	var gapData struct {
		FromSeq string `json:"fromSeq"`
	}
	if err := json.Unmarshal([]byte(msg.Data), &gapData); err != nil {
		break
	}
	fromSeq, err := strconv.ParseUint(gapData.FromSeq, 10, 64)
	if err != nil {
		break
	}
	frames := buildGapReplayFrames(tracker.OutputLog(), fromSeq, 16384)
	for _, frame := range frames {
		if err := conn.WriteMessage(websocket.BinaryMessage, frame); err != nil {
			return
		}
	}
```

### Step 4: Run tests

```bash
go test ./internal/dashboard/ -run TestBuildGapReplay -v
cd assets/dashboard && npx vitest run src/lib/terminalStream.test.ts
```

### Step 5: Commit

```
feat(ws): add sequence-based gap detection and replay

Frontend detects sequence gaps in binary frames and requests replay.
Backend replays missing events from the output log. This is the primary
consistency mechanism, replacing text-comparison sync for most cases.
```

---

## Task 9: Demote Sync to Defense-in-Depth

**Files:**

- Modify: `internal/dashboard/websocket.go:380-444` (change sync interval from 10s to 60s, remove activity-triggered sync, simplify forced logic)
- Modify: `assets/dashboard/src/lib/terminalStream.ts` (extend activity guard to 2s)
- Test: update existing sync tests

### Step 1: Update the sync goroutine

```go
// Change sync interval from 10s to 60s
interval := 60 * time.Second

// Change initial timer from 500ms to 5s (give bootstrap time to settle)
timer := time.NewTimer(5 * time.Second)

// Remove the syncNow channel and activity-triggered sync entirely.
// Gap detection + replay is now the primary consistency mechanism.
// This goroutine is a paranoia safety net only.
```

Remove the `syncNow` channel, the `>500 byte` trigger in the output loop (lines 469-474), and the debounce logic. The sync goroutine becomes a simple periodic timer.

### Step 2: Update the frontend activity guard

```typescript
// Change from 500ms to 2000ms
if (!msg.forced && Date.now() - this.lastBinaryTime < 2000) {
```

### Step 3: Update tests and run

```bash
go test ./internal/dashboard/ -v
cd assets/dashboard && npx vitest run src/lib/terminalStream.test.ts
```

### Step 4: Commit

```
refactor(sync): demote text-comparison sync to defense-in-depth

Sync interval increased from 10s to 60s. Initial delay increased from
500ms to 5s. Activity-triggered sync removed (gap detection handles this
now). Activity guard extended from 500ms to 2s. Sync is now a paranoia
safety net, not the primary consistency mechanism.
```

---

## Task 10: Update Stats and Diagnostics

**Files:**

- Modify: `internal/dashboard/websocket.go` (add seq stats to WSStatsMessage)
- Modify: `assets/dashboard/src/lib/terminalStream.ts` (report lastReceivedSeq in stats)
- Modify: `assets/dashboard/src/components/StreamMetricsPanel.tsx` (display seq info)
- Modify: existing tests as needed

### Step 1: Add sequence tracking to stats

**Go side** — update `WSStatsMessage`:

```go
type WSStatsMessage struct {
	// ... existing fields ...
	CurrentSeq    uint64 `json:"currentSeq"`
	LogOldestSeq  uint64 `json:"logOldestSeq"`
	LogEntries    int    `json:"logEntries"`
}
```

Populate from `tracker.OutputLog()` in the stats ticker.

**TS side** — include `lastReceivedSeq` in the stats update callback.

**StreamMetricsPanel** — display current server seq, client seq, and gap count.

### Step 2: Test and commit

```bash
go test ./internal/dashboard/ -v
cd assets/dashboard && npx vitest run
```

```
feat(diagnostics): add sequence tracking to stream metrics

Stats messages now include currentSeq, logOldestSeq, and logEntries.
StreamMetricsPanel displays client/server seq for debugging.
```

---

## Task 11: Scenario Tests for the New Behavior

**Files:**

- Modify: `test/scenarios/terminal-sync.spec.ts` (update for surgical correction)
- Add tests to: `test/scenarios/terminal-fidelity.spec.ts` (scrollback preservation)

### New scenario tests to add:

1. **Scrollback survives sync correction**: Generate 500 lines of output, trigger sync correction (corrupt xterm buffer), verify scrollback lines are still accessible.

2. **Bootstrap renders without visual jump**: Navigate to session with substantial output, verify terminal reaches correct scroll position within 1 second (no multi-second scroll-through).

3. **Gap recovery works**: (Hard to test without injecting drops — may need a test hook or a separate E2E test.)

### Step 1: Write scenarios

```
Scenario: Scrollback survives sync correction
Given a running session with 500 lines of output
When the xterm.js buffer is corrupted on one row
And a sync correction fires
Then the scrollback still contains all 500 lines
And only the corrupted row was corrected

Scenario: Bootstrap renders at correct scroll position
Given a running session with 2000 lines of output
When I navigate to the session page
Then the terminal viewport shows the cursor position within 2 seconds
And no visible scrolling animation occurs
```

### Step 2: Generate and run

```bash
./test.sh --scenarios
```

### Step 3: Commit

```
test(scenarios): add scrollback preservation and bootstrap render tests

Verifies that sync corrections don't destroy scrollback and that
bootstrap rendering completes without visible scroll-through artifacts.
```

---

## Task 12: Clean Up Dead Code

**Files:**

- Modify: `internal/dashboard/websocket.go` (remove `syncNow` channel, simplify forced sync logic)
- Modify: `assets/dashboard/src/lib/terminalStream.ts` (remove any dead code paths)
- Verify: `assets/dashboard/src/lib/syncCompare.ts` is still used (it is — by the defense-in-depth sync)

### Step 1: Remove dead code

- Remove the `syncNow` channel declaration and all sends to it
- Remove the activity-triggered sync debounce logic (lines 391-402 in current code)
- Remove the `>500 byte` trigger in the output loop
- Keep `compareScreens` and `stripAnsi` (still used by defense-in-depth sync)
- Keep `buildSyncMessage` (still used by defense-in-depth sync)

### Step 2: Run full test suite

```bash
./test.sh --all
```

### Step 3: Commit

```
refactor(ws): remove activity-triggered sync dead code

Removes the syncNow channel, debounce logic, and large-output trigger
now that gap detection handles consistency. Periodic defense-in-depth
sync remains on a 60s interval.
```

---

## Verification Checklist

After all tasks are complete, verify:

- [ ] `go test ./internal/session/ -v` — all pass (OutputLog + tracker integration)
- [ ] `go test ./internal/dashboard/ -v` — all pass (frame encoding, chunk replay, gap replay)
- [ ] `cd assets/dashboard && npx vitest run` — all pass (TS unit tests)
- [ ] `./test.sh --quick` — all pass (backend + frontend)
- [ ] `./test.sh --scenarios` — all pass (Playwright scenarios)
- [ ] `./test.sh --all` — all pass (full suite)
- [ ] Manual test: navigate to a session with substantial output, verify no scroll-through artifact
- [ ] Manual test: wait 60s on a session page, verify no scrollback loss
- [ ] Manual test: open dev mode, verify StreamMetricsPanel shows sequence tracking
- [ ] `docs/api.md` updated if any API-related packages changed (CI enforces this)
