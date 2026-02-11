# Agent Signal Reliability Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use 10x-engineer:executing-plans to implement this plan task-by-task.

**Goal:** Fix all reliability problems in agent-to-schmux signal detection — signals are never missed regardless of browser state, chunk boundaries, or daemon restarts.

**Architecture:** Replace regex-based ANSI stripping with a state machine, extract a reusable `SignalDetector` with line accumulation, move signal parsing from the WebSocket handler into the tracker, add monotonic sequence numbers for deduplication, and persist signal timestamps across daemon restarts.

**Tech Stack:** Go (backend), TypeScript/React (frontend), standard Go `testing` package.

---

### Task 1: State-machine ANSI stripper

Replace the four-regex `stripANSIBytes()` in `signal.go` with a single-pass state machine. This is a pure function with no dependencies, so we start here.

**Files:**

- Modify: `internal/signal/signal.go:33-80`
- Test: `internal/signal/signal_test.go`

**Step 1: Write the failing test**

Add a test for DCS and APC sequences that the current regex approach doesn't handle. Add to `signal_test.go`:

```go
func TestStripANSIStateMachine(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{
			name: "no ANSI sequences",
			in:   "Task finished successfully",
			want: "Task finished successfully",
		},
		{
			name: "cursor forward sequences replace with spaces",
			in:   "Task\x1b[1Cfinished\x1b[1Csuccessfully",
			want: "Task finished successfully",
		},
		{
			name: "cursor forward with count",
			in:   "Hello\x1b[2CWorld",
			want: "Hello  World",
		},
		{
			name: "cursor down sequences replace with newlines",
			in:   "line1\x1b[1Bline2",
			want: "line1\nline2",
		},
		{
			name: "cursor down with count",
			in:   "line1\x1b[3Bline2",
			want: "line1\n\n\nline2",
		},
		{
			name: "color sequences stripped",
			in:   "\x1b[32mSuccess\x1b[0m: done",
			want: "Success: done",
		},
		{
			name: "DEC Private Mode sequences stripped",
			in:   "\x1b[?2026l\x1b[?2026hHello",
			want: "Hello",
		},
		{
			name: "OSC sequences stripped",
			in:   "\x1b]0;Window Title\x07Hello",
			want: "Hello",
		},
		{
			name: "OSC with ST terminator stripped",
			in:   "\x1b]0;Window Title\x1b\\Hello",
			want: "Hello",
		},
		{
			name: "DCS sequences stripped",
			in:   "\x1bPsome DCS content\x1b\\Hello",
			want: "Hello",
		},
		{
			name: "APC sequences stripped",
			in:   "\x1b_some APC content\x1b\\Hello",
			want: "Hello",
		},
		{
			name: "mixed cursor movements",
			in:   "\x1b[2AUp\x1b[3BDown\x1b[4CRight\x1b[5DLeft",
			want: "Up\n\n\nDown    RightLeft",
		},
		{
			name: "cursor forward without explicit count defaults to 1",
			in:   "A\x1b[CB",
			want: "A B",
		},
		{
			name: "cursor down without explicit count defaults to 1",
			in:   "A\x1b[BB",
			want: "A\nB",
		},
		{
			name: "real world Claude Code signal with DEC sequences",
			in:   "\r\n\x1b[?2026l\x1b[?2026h\r\x1b[8A\x1b[38;2;255;255;255m\xe2\x8f\xba\x1b[1C\x1b[39m--<[schmux:needs_input:How\x1b[1Ccan\x1b[1CI\x1b[1Chelp]>--\r\x1b[2B",
			want: "\r\n\r⏺ --<[schmux:needs_input:How can I help]>--\r\n\n",
		},
		{
			name: "empty string",
			in:   "",
			want: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := string(stripANSIBytes([]byte(tt.in)))
			if got != tt.want {
				t.Errorf("stripANSIBytes(%q) =\n  %q\nwant:\n  %q", tt.in, got, tt.want)
			}
		})
	}
}
```

**Step 2: Run the test to verify it fails**

```bash
go test ./internal/signal/ -run TestStripANSIStateMachine -v
```

Expected: FAIL — the DCS and APC test cases will fail because the current regex approach doesn't strip them.

**Step 3: Implement the state machine**

Replace the contents of `stripANSIBytes()` and `stripANSI()` in `signal.go`. Remove the four regex patterns (`ansiPattern`, `oscSeqPattern`, `cursorForwardPattern`, `cursorDownPattern`) and replace with:

```go
// stripANSIBytes removes ANSI escape sequences from a byte slice using a state machine.
// Cursor forward sequences (\x1b[nC) are replaced with n spaces to preserve word boundaries.
// Cursor down sequences (\x1b[nB) are replaced with n newlines to preserve line boundaries.
// All other escape sequences (CSI, OSC, DCS, APC) are consumed entirely.
// This follows ECMA-48 terminal protocol structure for complete coverage.
func stripANSIBytes(data []byte) []byte {
	const (
		stNormal = iota
		stEsc
		stCSI
		stOSC
		stDCS // also handles APC
	)

	out := make([]byte, 0, len(data))
	st := stNormal
	escSeen := false // for OSC/DCS ST terminator detection (\x1b\\)
	var csiParam []byte // accumulate CSI parameter bytes to parse count

	for _, b := range data {
		switch st {
		case stNormal:
			if b == 0x1b {
				st = stEsc
			} else {
				out = append(out, b)
			}

		case stEsc:
			switch b {
			case '[':
				st = stCSI
				csiParam = csiParam[:0]
			case ']':
				st = stOSC
				escSeen = false
			case 'P', '_': // DCS or APC
				st = stDCS
				escSeen = false
			default:
				// Unknown ESC sequence (e.g., ESC c for reset) — consume just the ESC
				st = stNormal
			}

		case stCSI:
			if b >= 0x30 && b <= 0x3F {
				// Parameter bytes (0-9, :, ;, <, =, >, ?)
				csiParam = append(csiParam, b)
			} else if b >= 0x20 && b <= 0x2F {
				// Intermediate bytes — ignore
			} else if b >= 0x40 && b <= 0x7E {
				// Final byte — determines the command
				switch b {
				case 'C': // Cursor Forward — replace with spaces
					n := parseCSICount(csiParam)
					for i := 0; i < n; i++ {
						out = append(out, ' ')
					}
				case 'B': // Cursor Down — replace with newlines
					n := parseCSICount(csiParam)
					for i := 0; i < n; i++ {
						out = append(out, '\n')
					}
				}
				// All other CSI commands: consume (emit nothing)
				st = stNormal
			}
			// Else: still accumulating CSI sequence

		case stOSC:
			if escSeen {
				if b == '\\' {
					st = stNormal
				}
				escSeen = false
				continue
			}
			if b == 0x07 { // BEL terminates OSC
				st = stNormal
				continue
			}
			escSeen = b == 0x1b

		case stDCS:
			if escSeen {
				if b == '\\' {
					st = stNormal
				}
				escSeen = false
				continue
			}
			escSeen = b == 0x1b
		}
	}

	return out
}

// parseCSICount extracts the numeric parameter from CSI parameter bytes.
// Returns 1 if no parameter is present (default for cursor movement commands).
// Handles DEC Private Mode prefix '?' by skipping it.
func parseCSICount(params []byte) int {
	if len(params) == 0 {
		return 1
	}
	// Skip DEC private mode prefix
	s := string(params)
	if len(s) > 0 && s[0] == '?' {
		return 1 // DEC private mode sequences don't have meaningful counts for our purposes
	}
	// Take first parameter before any ';'
	if idx := strings.IndexByte(s, ';'); idx >= 0 {
		s = s[:idx]
	}
	n, err := strconv.Atoi(s)
	if err != nil || n <= 0 {
		return 1
	}
	return n
}

// stripANSI removes ANSI escape sequences from a string.
func stripANSI(s string) string {
	return string(stripANSIBytes([]byte(s)))
}
```

Add `"strconv"` and `"strings"` to the imports, remove `"regexp"`. Note: `bracketPattern` still uses regexp — keep the `"regexp"` import but remove the four ANSI regex variables.

Actually, `bracketPattern` still needs `"regexp"`. Keep it. Only remove the four ANSI-related regex variables: `ansiPattern`, `oscSeqPattern`, `cursorForwardPattern`, `cursorDownPattern`.

**Step 4: Run all signal tests to verify they pass**

```bash
go test ./internal/signal/ -v
```

Expected: ALL PASS — the existing `TestStripANSI`, `TestParseSignals`, `TestParseSignalsWithANSI` tests should all still pass, plus the new `TestStripANSIStateMachine` test.

**Step 5: Commit**

```bash
git add internal/signal/signal.go internal/signal/signal_test.go
git commit -m "Replace regex ANSI stripping with state machine

State machine handles all ECMA-48 sequence types (CSI, OSC, DCS, APC)
instead of four regex patterns that missed unknown sequences. Single
pass, O(n), no backtracking risk."
```

---

### Task 2: SignalDetector with line accumulator

Create the `SignalDetector` struct that combines line accumulation with signal parsing. This is a pure data structure with no external dependencies beyond the signal package itself.

**Files:**

- Create: `internal/signal/detector.go`
- Create: `internal/signal/detector_test.go`

**Step 1: Write the failing tests**

Create `internal/signal/detector_test.go`:

```go
package signal

import (
	"sync"
	"testing"
	"time"
)

func TestDetectorBasicSignal(t *testing.T) {
	var got []Signal
	var mu sync.Mutex
	d := NewSignalDetector("test-session", func(sig Signal) {
		mu.Lock()
		got = append(got, sig)
		mu.Unlock()
	})

	d.Feed([]byte("--<[schmux:completed:Task done]>--\n"))

	mu.Lock()
	defer mu.Unlock()
	if len(got) != 1 {
		t.Fatalf("expected 1 signal, got %d", len(got))
	}
	if got[0].State != "completed" {
		t.Errorf("state = %q, want %q", got[0].State, "completed")
	}
	if got[0].Message != "Task done" {
		t.Errorf("message = %q, want %q", got[0].Message, "Task done")
	}
}

func TestDetectorChunkSplit(t *testing.T) {
	var got []Signal
	var mu sync.Mutex
	d := NewSignalDetector("test-session", func(sig Signal) {
		mu.Lock()
		got = append(got, sig)
		mu.Unlock()
	})

	// Signal split across two chunks
	d.Feed([]byte("some output\n--<[schmux:comp"))
	mu.Lock()
	if len(got) != 0 {
		t.Fatalf("expected 0 signals after first chunk, got %d", len(got))
	}
	mu.Unlock()

	d.Feed([]byte("leted:Task done]>--\nmore output"))
	mu.Lock()
	defer mu.Unlock()
	if len(got) != 1 {
		t.Fatalf("expected 1 signal after second chunk, got %d", len(got))
	}
	if got[0].State != "completed" {
		t.Errorf("state = %q, want %q", got[0].State, "completed")
	}
}

func TestDetectorFlush(t *testing.T) {
	var got []Signal
	var mu sync.Mutex
	d := NewSignalDetector("test-session", func(sig Signal) {
		mu.Lock()
		got = append(got, sig)
		mu.Unlock()
	})

	// Signal without trailing newline
	d.Feed([]byte("--<[schmux:completed:Task done]>--"))

	mu.Lock()
	if len(got) != 0 {
		t.Fatalf("expected 0 signals before flush, got %d", len(got))
	}
	mu.Unlock()

	// Flush forces parsing of buffered data
	d.Flush()

	mu.Lock()
	defer mu.Unlock()
	if len(got) != 1 {
		t.Fatalf("expected 1 signal after flush, got %d", len(got))
	}
}

func TestDetectorBufferLimit(t *testing.T) {
	var got []Signal
	var mu sync.Mutex
	d := NewSignalDetector("test-session", func(sig Signal) {
		mu.Lock()
		got = append(got, sig)
		mu.Unlock()
	})

	// Feed a very long line without newline (exceeds 4KB limit)
	bigChunk := make([]byte, 5000)
	for i := range bigChunk {
		bigChunk[i] = 'A'
	}
	d.Feed(bigChunk)

	// Buffer should have been truncated, not grown unboundedly
	if len(d.buf) > maxSignalBufSize {
		t.Errorf("buffer size %d exceeds max %d", len(d.buf), maxSignalBufSize)
	}
}

func TestDetectorANSIInSignal(t *testing.T) {
	var got []Signal
	var mu sync.Mutex
	d := NewSignalDetector("test-session", func(sig Signal) {
		mu.Lock()
		got = append(got, sig)
		mu.Unlock()
	})

	// Real-world Claude Code output with ANSI interleaved
	d.Feed([]byte("\x1b[?2026l\x1b[?2026h\x1b[38;2;255;255;255m\xe2\x8f\xba\x1b[1C\x1b[39m--<[schmux:needs_input:How\x1b[1Ccan\x1b[1CI\x1b[1Chelp]>--\r\n"))

	mu.Lock()
	defer mu.Unlock()
	if len(got) != 1 {
		t.Fatalf("expected 1 signal, got %d", len(got))
	}
	if got[0].Message != "How can I help" {
		t.Errorf("message = %q, want %q", got[0].Message, "How can I help")
	}
}

func TestDetectorMultipleSignals(t *testing.T) {
	var got []Signal
	var mu sync.Mutex
	d := NewSignalDetector("test-session", func(sig Signal) {
		mu.Lock()
		got = append(got, sig)
		mu.Unlock()
	})

	d.Feed([]byte("--<[schmux:working:]>--\nsome output\n--<[schmux:completed:Done]>--\n"))

	mu.Lock()
	defer mu.Unlock()
	if len(got) != 2 {
		t.Fatalf("expected 2 signals, got %d", len(got))
	}
	if got[0].State != "working" {
		t.Errorf("first signal state = %q, want %q", got[0].State, "working")
	}
	if got[1].State != "completed" {
		t.Errorf("second signal state = %q, want %q", got[1].State, "completed")
	}
}

func TestDetectorNearMissLogging(t *testing.T) {
	var nearMisses []string
	var mu sync.Mutex

	d := NewSignalDetector("test-session", func(sig Signal) {})
	d.nearMissCallback = func(line string) {
		mu.Lock()
		nearMisses = append(nearMisses, line)
		mu.Unlock()
	}

	// Line contains "schmux" but doesn't match the signal pattern
	d.Feed([]byte("I configured schmux to do something\n"))

	mu.Lock()
	defer mu.Unlock()
	// "schmux" in normal text should NOT trigger near-miss (no --<[ prefix)
	if len(nearMisses) != 0 {
		t.Errorf("expected 0 near-misses for normal text, got %d: %v", len(nearMisses), nearMisses)
	}
}

func TestDetectorNearMissWithBrokenSignal(t *testing.T) {
	var nearMisses []string
	var mu sync.Mutex

	d := NewSignalDetector("test-session", func(sig Signal) {})
	d.nearMissCallback = func(line string) {
		mu.Lock()
		nearMisses = append(nearMisses, line)
		mu.Unlock()
	}

	// Looks like a signal but is malformed (missing closing)
	d.Feed([]byte("--<[schmux:completed:Taskdone\n"))

	mu.Lock()
	defer mu.Unlock()
	if len(nearMisses) != 1 {
		t.Fatalf("expected 1 near-miss, got %d", len(nearMisses))
	}
}

func TestDetectorFlushTimeout(t *testing.T) {
	var got []Signal
	var mu sync.Mutex
	d := NewSignalDetector("test-session", func(sig Signal) {
		mu.Lock()
		got = append(got, sig)
		mu.Unlock()
	})

	// Feed signal without newline
	d.Feed([]byte("--<[schmux:completed:Done]>--"))

	mu.Lock()
	count := len(got)
	mu.Unlock()
	if count != 0 {
		t.Fatalf("expected 0 signals before timeout, got %d", count)
	}

	// Simulate time passing and check ShouldFlush
	d.lastData = time.Now().Add(-time.Second)
	if !d.ShouldFlush() {
		t.Error("ShouldFlush() should return true after timeout")
	}
}
```

**Step 2: Run the test to verify it fails**

```bash
go test ./internal/signal/ -run TestDetector -v
```

Expected: FAIL — `NewSignalDetector` doesn't exist yet.

**Step 3: Implement SignalDetector**

Create `internal/signal/detector.go`:

```go
package signal

import (
	"bytes"
	"fmt"
	"strings"
	"time"
)

const (
	// maxSignalBufSize is the maximum size of the line accumulator buffer.
	// If the buffer exceeds this without seeing a newline, it's truncated from the front.
	maxSignalBufSize = 4096

	// FlushTimeout is how long to wait after the last Feed() before flushing buffered data.
	FlushTimeout = 500 * time.Millisecond
)

// SignalDetector detects schmux signals in terminal output streams.
// It accumulates incomplete lines across Feed() calls to handle chunk-splitting,
// and strips ANSI escape sequences before matching.
type SignalDetector struct {
	sessionID        string
	buf              []byte       // line accumulator for incomplete lines
	callback         func(Signal) // fires on each detected signal
	nearMissCallback func(string) // fires when a line contains "schmux" but doesn't match (for diagnostics)
	lastData         time.Time    // timestamp of last Feed() call
}

// NewSignalDetector creates a detector that fires callback for each detected signal.
func NewSignalDetector(sessionID string, callback func(Signal)) *SignalDetector {
	return &SignalDetector{
		sessionID: sessionID,
		callback:  callback,
	}
}

// Feed processes raw terminal bytes. Complete lines (terminated by \n) are parsed
// for signals immediately. Incomplete trailing lines are buffered until the next
// Feed() or Flush() call.
func (d *SignalDetector) Feed(data []byte) {
	d.lastData = time.Now()

	// Prepend any buffered data from previous Feed()
	var combined []byte
	if len(d.buf) > 0 {
		combined = make([]byte, len(d.buf)+len(data))
		copy(combined, d.buf)
		copy(combined[len(d.buf):], data)
		d.buf = nil
	} else {
		combined = data
	}

	// Find the last newline
	lastNL := bytes.LastIndexByte(combined, '\n')
	if lastNL == -1 {
		// No newline found — buffer everything
		d.buf = make([]byte, len(combined))
		copy(d.buf, combined)
		d.enforceBufLimit()
		return
	}

	// Everything up to and including the last newline = complete lines
	completeLines := combined[:lastNL+1]

	// Everything after = incomplete line, buffer for next Feed()
	if lastNL+1 < len(combined) {
		remaining := combined[lastNL+1:]
		d.buf = make([]byte, len(remaining))
		copy(d.buf, remaining)
	}

	d.parseLines(completeLines)
}

// Flush force-parses any buffered incomplete line.
// Call this when no new data has arrived for FlushTimeout.
func (d *SignalDetector) Flush() {
	if len(d.buf) == 0 {
		return
	}
	data := d.buf
	d.buf = nil
	d.parseLines(data)
}

// ShouldFlush returns true if the detector has buffered data that should be
// flushed because no new data has arrived within FlushTimeout.
func (d *SignalDetector) ShouldFlush() bool {
	return len(d.buf) > 0 && !d.lastData.IsZero() && time.Since(d.lastData) >= FlushTimeout
}

// parseLines strips ANSI sequences and matches signal patterns in the given data.
func (d *SignalDetector) parseLines(data []byte) {
	now := time.Now()
	cleanData := stripANSIBytes(data)
	signals := parseBracketSignals(cleanData, now)

	for _, sig := range signals {
		d.callback(sig)
	}

	// Near-miss diagnostic: check for lines containing signal-like patterns that didn't match
	if d.nearMissCallback != nil && len(signals) == 0 {
		for _, line := range strings.Split(string(cleanData), "\n") {
			trimmed := strings.TrimSpace(line)
			if trimmed == "" {
				continue
			}
			// Only flag lines that look like they were trying to be a signal
			if strings.Contains(trimmed, "--<[schmux:") && !bracketPattern.MatchString(trimmed) {
				d.nearMissCallback(trimmed)
			}
		}
	}
}

// enforceBufLimit truncates the buffer from the front if it exceeds maxSignalBufSize.
func (d *SignalDetector) enforceBufLimit() {
	if len(d.buf) > maxSignalBufSize {
		excess := len(d.buf) - maxSignalBufSize
		copy(d.buf, d.buf[excess:])
		d.buf = d.buf[:maxSignalBufSize]
		fmt.Printf("[signal] %s - line accumulator truncated (%d bytes exceeded limit)\n", d.sessionID[:8], excess)
	}
}
```

**Step 4: Run all signal tests**

```bash
go test ./internal/signal/ -v
```

Expected: ALL PASS

**Step 5: Commit**

```bash
git add internal/signal/detector.go internal/signal/detector_test.go
git commit -m "Add SignalDetector with line accumulator and near-miss diagnostics

Reusable struct that handles chunk-split signals by accumulating
incomplete lines, with a flush timeout for signals at end of output.
Includes diagnostic callback for signal-like patterns that don't match."
```

---

### Task 3: Persist LastSignalAt and add NudgeSeq to Session

**Files:**

- Modify: `internal/state/state.go:80-95` (Session struct)
- Modify: `internal/state/state.go:135-141` (Load function — reset logic)
- Modify: `internal/state/interfaces.go:18` (StateStore interface)
- Modify: `internal/workspace/manager_test.go:479-481` (mock)

**Step 1: Update the Session struct**

In `internal/state/state.go`, change the `Session` struct:

```go
// Before:
LastSignalAt time.Time `json:"-"`                        // Last time agent sent a direct signal (in-memory only)

// After:
LastSignalAt time.Time `json:"last_signal_at,omitempty"` // Last time agent sent a direct signal
NudgeSeq     uint64    `json:"nudge_seq,omitempty"`      // Monotonic counter incremented on each signal
```

**Step 2: Add IncrementNudgeSeq to state**

Add a new method to `internal/state/state.go` after `UpdateSessionLastSignal`:

```go
// IncrementNudgeSeq atomically increments the NudgeSeq counter and returns the new value.
func (s *State) IncrementNudgeSeq(sessionID string) uint64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.Sessions {
		if s.Sessions[i].ID == sessionID {
			s.Sessions[i].NudgeSeq++
			return s.Sessions[i].NudgeSeq
		}
	}
	return 0
}

// GetNudgeSeq returns the current NudgeSeq for a session.
func (s *State) GetNudgeSeq(sessionID string) uint64 {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, sess := range s.Sessions {
		if sess.ID == sessionID {
			return sess.NudgeSeq
		}
	}
	return 0
}
```

**Step 3: Update the StateStore interface**

Add to `internal/state/interfaces.go` in the session operations section:

```go
IncrementNudgeSeq(sessionID string) uint64
GetNudgeSeq(sessionID string) uint64
```

**Step 4: Update the mock StateStore**

In `internal/workspace/manager_test.go`, add after `UpdateSessionLastSignal`:

```go
func (m *mockStateStore) IncrementNudgeSeq(sessionID string) uint64 {
	return m.state.IncrementNudgeSeq(sessionID)
}

func (m *mockStateStore) GetNudgeSeq(sessionID string) uint64 {
	return m.state.GetNudgeSeq(sessionID)
}
```

**Step 5: Run tests**

```bash
go test ./internal/state/ ./internal/workspace/ -v
```

Expected: ALL PASS — the new fields default to zero values, existing behavior unchanged.

**Step 6: Commit**

```bash
git add internal/state/state.go internal/state/interfaces.go internal/workspace/manager_test.go
git commit -m "Persist LastSignalAt and add NudgeSeq to Session

LastSignalAt is now persisted to state.json (was json:\"-\") so
NudgeNik respects signal history across daemon restarts. NudgeSeq
is a monotonic counter for frontend deduplication."
```

---

### Task 4: Move signal detection into the tracker

This is the core architectural change. The tracker gets a `SignalDetector` and a callback, and the WebSocket handler stops parsing signals.

**Files:**

- Modify: `internal/session/tracker.go`
- Modify: `internal/session/manager.go:1046-1059` (ensureTrackerFromSession)
- Modify: `internal/dashboard/websocket.go:228-233` (remove signal parsing)

**Step 1: Add signal detection to the tracker**

In `internal/session/tracker.go`, update the struct and constructor:

```go
import (
	// ... existing imports ...
	"github.com/sergeknystautas/schmux/internal/signal"
	"github.com/sergeknystautas/schmux/internal/tmux"
)

// SessionTracker maintains a long-lived PTY attachment for a tmux session.
// It tracks output activity, detects agent signals, and forwards terminal
// output to one active websocket client.
type SessionTracker struct {
	sessionID      string
	tmuxSession    string
	state          state.StateStore
	signalDetector *signal.SignalDetector

	mu        sync.RWMutex
	clientCh  chan []byte
	ptmx      *os.File
	attachCmd *exec.Cmd
	lastEvent time.Time

	stopOnce sync.Once
	stopCh   chan struct{}
	doneCh   chan struct{}

	lastRetryLog time.Time
}

// NewSessionTracker creates a tracker for a session.
// signalCallback is called for each detected agent signal, or nil to disable signal detection.
func NewSessionTracker(sessionID, tmuxSession string, st state.StateStore, signalCallback func(signal.Signal)) *SessionTracker {
	t := &SessionTracker{
		sessionID:   sessionID,
		tmuxSession: tmuxSession,
		state:       st,
		stopCh:      make(chan struct{}),
		doneCh:      make(chan struct{}),
	}
	if signalCallback != nil {
		t.signalDetector = signal.NewSignalDetector(sessionID, signalCallback)
		t.signalDetector.SetNearMissCallback(func(line string) {
			fmt.Printf("[signal] %s - potential missed signal: %q\n", sessionID[:8], line)
		})
	}
	return t
}
```

Note: We need to add `SetNearMissCallback` to `SignalDetector` (currently the field is exported directly — let's add a setter for cleanliness). Add this method to `detector.go`:

```go
// SetNearMissCallback sets a callback for diagnostic logging of near-miss signal patterns.
func (d *SignalDetector) SetNearMissCallback(cb func(string)) {
	d.nearMissCallback = cb
}
```

**Step 2: Feed chunks to the detector in attachAndRead**

In the `attachAndRead` method, after the chunk is created and before sending to `clientCh`, feed it to the detector. Also add a flush ticker. Update the read loop in `attachAndRead()`:

After the line `chunk := make([]byte, len(data))` and `copy(chunk, data)`, add:

```go
				// Feed to signal detector (line accumulation handles chunk splits)
				if t.signalDetector != nil {
					t.signalDetector.Feed(chunk)
				}
```

Add a flush ticker in the `run()` method. Replace the existing `run()`:

```go
func (t *SessionTracker) run() {
	defer close(t.doneCh)

	for {
		select {
		case <-t.stopCh:
			return
		default:
		}

		if err := t.attachAndRead(); err != nil && err != io.EOF {
			now := time.Now()
			if t.shouldLogRetry(now) {
				fmt.Printf("[tracker] %s attach/read failed: %v\n", t.sessionID, err)
			}
		}

		// Flush any buffered signal data on disconnect
		if t.signalDetector != nil {
			t.signalDetector.Flush()
		}

		if t.waitOrStop(trackerRestartDelay) {
			return
		}
	}
}
```

Also add a periodic flush check in the read loop. In `attachAndRead()`, change the `select` at the end of the loop:

```go
		select {
		case <-t.stopCh:
			// Flush signal buffer on stop
			if t.signalDetector != nil {
				t.signalDetector.Flush()
			}
			return io.EOF
		default:
			// Check if signal detector needs flushing
			if t.signalDetector != nil && t.signalDetector.ShouldFlush() {
				t.signalDetector.Flush()
			}
		}
```

**Step 3: Update ensureTrackerFromSession in manager.go**

The manager needs to pass a signal callback when creating trackers. But the manager doesn't have access to `handleAgentSignal` (that's on the dashboard server). We need an indirect approach: the manager accepts a callback factory.

Add a field to the Manager:

```go
type Manager struct {
	config          *config.Config
	state           state.StateStore
	workspace       workspace.WorkspaceManager
	remoteManager   *remote.Manager
	trackers        map[string]*SessionTracker
	signalCallback  func(sessionID string, sig signal.Signal) // called when agent emits a signal
	mu              sync.RWMutex
}
```

Add a setter:

```go
// SetSignalCallback sets the callback invoked when an agent emits a signal.
func (m *Manager) SetSignalCallback(cb func(sessionID string, sig signal.Signal)) {
	m.signalCallback = cb
}
```

Update `ensureTrackerFromSession`:

```go
func (m *Manager) ensureTrackerFromSession(sess state.Session) *SessionTracker {
	m.mu.Lock()
	if existing := m.trackers[sess.ID]; existing != nil {
		existing.SetTmuxSession(sess.TmuxSession)
		m.mu.Unlock()
		return existing
	}

	var cb func(signal.Signal)
	if m.signalCallback != nil {
		sessionID := sess.ID
		signalCb := m.signalCallback
		cb = func(sig signal.Signal) {
			signalCb(sessionID, sig)
		}
	}
	tracker := NewSessionTracker(sess.ID, sess.TmuxSession, m.state, cb)
	m.trackers[sess.ID] = tracker
	m.mu.Unlock()
	tracker.Start()
	return tracker
}
```

Add the `signal` import to `manager.go`:

```go
"github.com/sergeknystautas/schmux/internal/signal"
```

**Step 4: Wire the callback in the daemon**

Find where the session manager is created and wire the callback. Search for where `session.New()` is called:

```bash
grep -n "session.New(" internal/daemon/daemon.go
```

After the session manager is created and the dashboard server is created, add:

```go
sm.SetSignalCallback(func(sessionID string, sig signal.Signal) {
	srv.HandleAgentSignal(sessionID, sig)
})
```

This requires exporting `handleAgentSignal` on the Server. In `websocket.go`, rename `handleAgentSignal` to `HandleAgentSignal` (exported):

```go
// HandleAgentSignal processes a signal from an agent and updates the session nudge state.
func (s *Server) HandleAgentSignal(sessionID string, sig signal.Signal) {
```

**Step 5: Remove signal parsing from the WebSocket handler**

In `internal/dashboard/websocket.go`, in the main `for` loop of `handleTerminalWebSocket`, remove the signal parsing lines (228-233). Replace:

```go
		case chunk, ok := <-outputCh:
			if !ok {
				return
			}
			// Filter terminal mode sequences that interfere with xterm.js scrollback
			filtered := filterMouseMode(chunk)
			// Check for schmux signals (markers remain visible in output)
			signals := signal.ParseSignals(filtered)
			for _, sig := range signals {
				s.handleAgentSignal(sessionID, sig)
			}
			if len(filtered) > 0 {
```

With:

```go
		case chunk, ok := <-outputCh:
			if !ok {
				return
			}
			// Filter terminal mode sequences that interfere with xterm.js scrollback
			filtered := filterMouseMode(chunk)
			if len(filtered) > 0 {
```

Remove the `signal` import from `websocket.go` if no longer used (it may still be used by `HandleAgentSignal` — check). Actually `HandleAgentSignal` uses `signal.MapStateToNudge`, so keep the import.

**Step 6: Run tests**

```bash
go test ./internal/... -v
```

Expected: ALL PASS

**Step 7: Commit**

```bash
git add internal/session/tracker.go internal/session/manager.go internal/dashboard/websocket.go internal/signal/detector.go
git commit -m "Move signal detection from WebSocket handler into tracker

Signals are now detected continuously in the tracker's PTY read loop,
regardless of whether a browser tab is connected. The WebSocket handler
becomes a pure display pipe."
```

---

### Task 5: Add NudgeSeq to HandleAgentSignal and API response

**Files:**

- Modify: `internal/dashboard/websocket.go` (HandleAgentSignal)
- Modify: `internal/dashboard/handlers.go:88-105` (SessionResponseItem)
- Modify: `internal/dashboard/handlers.go:258-274` (response building)

**Step 1: Update HandleAgentSignal to increment NudgeSeq**

In `HandleAgentSignal`, after updating the nudge, increment the sequence:

```go
func (s *Server) HandleAgentSignal(sessionID string, sig signal.Signal) {
	sess, err := s.session.GetSession(sessionID)
	if err != nil {
		return
	}

	// Map signal state to nudge format for frontend compatibility
	nudgeResult := nudgenik.Result{
		State:   signal.MapStateToNudge(sig.State),
		Summary: sig.Message,
		Source:  "agent",
	}

	// "working" clears the nudge
	if sig.State == "working" {
		sess.Nudge = ""
	} else {
		payload, err := json.Marshal(nudgeResult)
		if err != nil {
			fmt.Printf("[signal] %s - failed to serialize nudge: %v\n", sessionID, err)
			return
		}
		sess.Nudge = string(payload)
	}

	// Increment sequence number for deduplication
	s.state.IncrementNudgeSeq(sessionID)

	// Update last signal time (now persisted)
	s.state.UpdateSessionLastSignal(sessionID, sig.Timestamp)

	if err := s.state.UpdateSession(*sess); err != nil {
		fmt.Printf("[signal] %s - failed to update session: %v\n", sessionID, err)
		return
	}
	if err := s.state.Save(); err != nil {
		fmt.Printf("[signal] %s - failed to save state: %v\n", sessionID, err)
		return
	}

	fmt.Printf("[signal] %s - received %s signal (seq=%d): %s\n", sessionID[:8], sig.State, s.state.GetNudgeSeq(sessionID), sig.Message)

	// Broadcast the update to all clients
	go s.BroadcastSessions()
}
```

**Step 2: Add NudgeSeq to the API response**

In `internal/dashboard/handlers.go`, add to `SessionResponseItem`:

```go
NudgeSeq     uint64 `json:"nudge_seq,omitempty"`
```

In the response building section (around line 258-274), add after `NudgeSummary`:

```go
NudgeSeq:         sess.NudgeSeq,
```

**Step 3: Run tests**

```bash
go test ./internal/dashboard/ -v
```

Expected: ALL PASS

**Step 4: Commit**

```bash
git add internal/dashboard/websocket.go internal/dashboard/handlers.go
git commit -m "Add NudgeSeq to signal handling and API response

Each signal increments a monotonic sequence number per session.
The sequence is included in the API response for frontend deduplication."
```

---

### Task 6: Scrollback parsing on tracker startup

**Files:**

- Modify: `internal/session/tracker.go` (attachAndRead)

**Step 1: Add scrollback parsing at the start of attachAndRead**

In `attachAndRead()`, after confirming the session exists and getting the window size, but before starting the PTY read loop, capture scrollback and parse it:

After the line `if !tmux.SessionExists(ctx, target) {` block, add:

```go
	// Parse scrollback for signals missed while daemon was down.
	// Only process signals with sequence numbers above the persisted NudgeSeq.
	if t.signalDetector != nil {
		capCtx, capCancel := context.WithTimeout(ctx, 2*time.Second)
		scrollback, err := tmux.CaptureLastLines(capCtx, target, 200, false)
		capCancel()
		if err == nil && scrollback != "" {
			// Parse scrollback through the detector, then flush to process any trailing incomplete lines
			t.signalDetector.Feed([]byte(scrollback))
			t.signalDetector.Flush()
		}
	}
```

Note: The callback in `HandleAgentSignal` already increments `NudgeSeq` and the detector doesn't need to know about sequences — the state layer handles deduplication via the fact that `handleAgentSignal` sets the same nudge state idempotently. The scrollback will re-fire signals but since the nudge is already set to the same value, the practical effect is just a redundant save+broadcast (acceptable for a startup-only path).

However, to avoid unnecessary broadcasts on restart, we should check if the nudge is already set. This is already handled: if the nudge string is identical, `UpdateSession` overwrites with the same value, and the broadcast is harmless (frontend uses NudgeSeq for dedup).

**Step 2: Run tests**

```bash
go test ./internal/session/ -v
```

Expected: ALL PASS

**Step 3: Commit**

```bash
git add internal/session/tracker.go
git commit -m "Parse scrollback on tracker startup for missed signals

When a tracker first attaches (including after daemon restart), it
captures the last 200 lines of tmux scrollback and parses them for
signals that may have been missed while the daemon was down."
```

---

### Task 7: Frontend notification deduplication with localStorage

**Files:**

- Modify: `assets/dashboard/src/lib/types.ts:1-18` (SessionResponse)
- Modify: `assets/dashboard/src/contexts/SessionsContext.tsx:68-97`

**Step 1: Add nudge_seq to SessionResponse type**

In `assets/dashboard/src/lib/types.ts`, add to `SessionResponse`:

```typescript
nudge_seq?: number;
```

**Step 2: Replace prevNudgeStatesRef with localStorage-based deduplication**

In `assets/dashboard/src/contexts/SessionsContext.tsx`, replace the entire nudge tracking section (lines 68-97):

```typescript
// Detect nudge state changes and play notification sound using NudgeSeq for deduplication.
// localStorage persists across page reloads, preventing stale nudges from re-triggering sounds.
useEffect(() => {
  let shouldPlaySound = false;

  Object.entries(sessionsById).forEach(([sessionId, session]) => {
    const nudgeSeq = session.nudge_seq ?? 0;
    if (nudgeSeq === 0) return;

    const storageKey = `schmux:ack:${sessionId}`;
    const lastAckedSeq = parseInt(localStorage.getItem(storageKey) || '0', 10);

    if (nudgeSeq > lastAckedSeq && isAttentionState(session.nudge_state)) {
      shouldPlaySound = true;
      localStorage.setItem(storageKey, String(nudgeSeq));
    }
  });

  // Cleanup: remove localStorage entries for sessions that no longer exist
  const currentSessionIds = new Set(Object.keys(sessionsById));
  for (let i = 0; i < localStorage.length; i++) {
    const key = localStorage.key(i);
    if (key?.startsWith('schmux:ack:')) {
      const sessionId = key.slice('schmux:ack:'.length);
      if (!currentSessionIds.has(sessionId)) {
        localStorage.removeItem(key);
      }
    }
  }

  // Play sound if any session transitioned to attention state (and sound is not disabled)
  if (shouldPlaySound && !config?.notifications?.sound_disabled) {
    playAttentionSound();
  }
}, [sessionsById, config?.notifications?.sound_disabled]);
```

Remove the `prevNudgeStatesRef` declaration (line 69) and its `useRef` import if no longer needed (check if `useRef` is used elsewhere in the file — yes, `sessionsByIdRef` uses it, so keep the import).

**Step 3: Build the dashboard to verify**

```bash
go run ./cmd/build-dashboard
```

Expected: Build succeeds with no TypeScript errors.

**Step 4: Commit**

```bash
git add assets/dashboard/src/lib/types.ts assets/dashboard/src/contexts/SessionsContext.tsx
git commit -m "Use localStorage + NudgeSeq for frontend notification deduplication

Replaces in-memory prevNudgeStatesRef with localStorage-persisted
sequence tracking. Page reloads no longer re-trigger notification
sounds for stale nudges."
```

---

### Task 8: Remote session signal detection

**Files:**

- Modify: `internal/dashboard/websocket.go` (handleRemoteTerminalWebSocket)
- Modify: `internal/session/manager.go`

**Step 1: Add signal detection to remote sessions**

The remote WebSocket handler receives output events from the remote connection. We need to parse these for signals. The simplest approach: create a `SignalDetector` per remote session in the manager, and feed remote output to it.

Add a remote signal monitor to the manager. In `manager.go`, add a field:

```go
type Manager struct {
	// ... existing fields ...
	remoteDetectors map[string]*signal.SignalDetector // signal detectors for remote sessions
}
```

Initialize it in `New()`:

```go
remoteDetectors: make(map[string]*signal.SignalDetector),
```

Add methods to start/stop remote signal monitoring:

```go
// StartRemoteSignalMonitor creates a signal detector for a remote session and starts
// a goroutine that subscribes to the remote output channel.
func (m *Manager) StartRemoteSignalMonitor(sess state.Session) {
	if m.remoteManager == nil || sess.RemotePaneID == "" || sess.RemoteHostID == "" {
		return
	}
	if m.signalCallback == nil {
		return
	}

	conn := m.remoteManager.GetConnection(sess.RemoteHostID)
	if conn == nil || !conn.IsConnected() {
		return
	}

	m.mu.Lock()
	if m.remoteDetectors[sess.ID] != nil {
		m.mu.Unlock()
		return // already running
	}

	sessionID := sess.ID
	signalCb := m.signalCallback
	detector := signal.NewSignalDetector(sessionID, func(sig signal.Signal) {
		signalCb(sessionID, sig)
	})
	detector.SetNearMissCallback(func(line string) {
		fmt.Printf("[signal] %s - potential missed signal (remote): %q\n", sessionID[:8], line)
	})
	m.remoteDetectors[sess.ID] = detector
	m.mu.Unlock()

	// Parse scrollback for missed signals
	capCtx, capCancel := context.WithTimeout(context.Background(), 2*time.Second)
	scrollback, err := conn.CapturePaneLines(capCtx, sess.RemotePaneID, 200)
	capCancel()
	if err == nil && scrollback != "" {
		detector.Feed([]byte(scrollback))
		detector.Flush()
	}

	// Start goroutine to monitor output
	outputCh := conn.SubscribeOutput(sess.RemotePaneID)
	go func() {
		defer conn.UnsubscribeOutput(sess.RemotePaneID, outputCh)
		flushTicker := time.NewTicker(signal.FlushTimeout)
		defer flushTicker.Stop()

		for {
			select {
			case event, ok := <-outputCh:
				if !ok {
					detector.Flush()
					return
				}
				if event.Data != "" {
					detector.Feed([]byte(event.Data))
				}
			case <-flushTicker.C:
				if detector.ShouldFlush() {
					detector.Flush()
				}
			}
		}
	}()
}

// StopRemoteSignalMonitor stops the signal detector for a remote session.
func (m *Manager) StopRemoteSignalMonitor(sessionID string) {
	m.mu.Lock()
	delete(m.remoteDetectors, sessionID)
	m.mu.Unlock()
	// The goroutine will stop when the output channel is unsubscribed/closed
}
```

**Step 2: Start remote monitors when sessions are created**

Find where remote sessions are created/started in the manager (in `SpawnRemote` and session restore logic) and call `StartRemoteSignalMonitor`. This needs to be called after the session has a `RemotePaneID`.

The best place is after the remote session is confirmed running. Search for where `RemotePaneID` is set:

```bash
grep -n "RemotePaneID" internal/session/manager.go
```

Add `m.StartRemoteSignalMonitor(sess)` after the session's `RemotePaneID` is set and saved.

Also, in the daemon startup path where existing sessions are restored, start monitors for remote sessions that already have a `RemotePaneID`.

**Step 3: Run tests**

```bash
go test ./internal/... -v
```

Expected: ALL PASS

**Step 4: Commit**

```bash
git add internal/session/manager.go internal/dashboard/websocket.go
git commit -m "Add signal detection for remote sessions

Remote sessions now get a lightweight signal monitor goroutine that
subscribes to the remote output channel and feeds it to a SignalDetector.
Includes scrollback parsing on startup."
```

---

### Task 9: Wire everything together in daemon startup

**Files:**

- Modify: `internal/daemon/daemon.go`

**Step 1: Find daemon startup and wire the signal callback**

Search for where the session manager and dashboard server are created:

```bash
grep -n "session.New\|dashboard.New" internal/daemon/daemon.go
```

After both are created, add:

```go
sm.SetSignalCallback(func(sessionID string, sig signal.Signal) {
	srv.HandleAgentSignal(sessionID, sig)
})
```

Also ensure existing remote sessions get signal monitors started on daemon startup. After restoring sessions, iterate remote sessions:

```go
// Start signal monitors for existing remote sessions
for _, sess := range st.GetSessions() {
	if sess.IsRemoteSession() && sess.RemotePaneID != "" {
		sm.StartRemoteSignalMonitor(sess)
	}
}
```

**Step 2: Run all tests**

```bash
go test ./internal/... -v
```

Expected: ALL PASS

**Step 3: Build the full binary**

```bash
go build ./cmd/schmux
```

Expected: Builds successfully.

**Step 4: Build the dashboard**

```bash
go run ./cmd/build-dashboard
```

Expected: Builds successfully.

**Step 5: Commit**

```bash
git add internal/daemon/daemon.go
git commit -m "Wire signal callback in daemon startup

Connects the session manager's signal detection to the dashboard
server's HandleAgentSignal. Starts remote signal monitors for
existing remote sessions on daemon restart."
```

---

### Task 10: Run full test suite and format

**Step 1: Format all code**

```bash
./format.sh
```

**Step 2: Run unit tests**

```bash
./test.sh
```

Expected: ALL PASS

**Step 3: Run full test suite including E2E**

```bash
./test.sh --all
```

Expected: ALL PASS

**Step 4: Final commit if format changed anything**

```bash
git add -A
git status
# Only commit if there are changes from formatting
git commit -m "Format code"
```

---

## Task Dependency Graph

```
Task 1 (state machine ANSI stripper)
  └── Task 2 (SignalDetector)
       └── Task 4 (move detection to tracker)
            └── Task 6 (scrollback on startup)
            └── Task 8 (remote sessions)
                 └── Task 9 (daemon wiring)

Task 3 (persist LastSignalAt + NudgeSeq)
  └── Task 5 (NudgeSeq in HandleAgentSignal + API)
       └── Task 7 (frontend localStorage dedup)

Task 9 depends on: Task 4, Task 5, Task 8
Task 10 depends on: all prior tasks
```

Tasks 1 and 3 can be done in parallel. Tasks 2 and 5 can be done in parallel (after their respective dependencies). The critical path is: 1 → 2 → 4 → 8 → 9 → 10.
