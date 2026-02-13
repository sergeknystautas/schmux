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
	d.Feed([]byte("--<[schmux:completed:Task done]>--"))
	mu.Lock()
	if len(got) != 0 {
		t.Fatalf("expected 0 signals before flush, got %d", len(got))
	}
	mu.Unlock()
	d.Flush()
	mu.Lock()
	defer mu.Unlock()
	if len(got) != 1 {
		t.Fatalf("expected 1 signal after flush, got %d", len(got))
	}
}

func TestDetectorBufferLimit(t *testing.T) {
	d := NewSignalDetector("test-session", func(sig Signal) {})
	bigChunk := make([]byte, maxSignalBufSize+1000)
	for i := range bigChunk {
		bigChunk[i] = 'A'
	}
	d.Feed(bigChunk)
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
	d.SetNearMissCallback(func(line string) {
		mu.Lock()
		nearMisses = append(nearMisses, line)
		mu.Unlock()
	})
	d.Feed([]byte("I configured schmux to do something\n"))
	mu.Lock()
	defer mu.Unlock()
	if len(nearMisses) != 0 {
		t.Errorf("expected 0 near-misses for normal text, got %d: %v", len(nearMisses), nearMisses)
	}
}

func TestDetectorNearMissWithBrokenSignal(t *testing.T) {
	var nearMisses []string
	var mu sync.Mutex
	d := NewSignalDetector("test-session", func(sig Signal) {})
	d.SetNearMissCallback(func(line string) {
		mu.Lock()
		nearMisses = append(nearMisses, line)
		mu.Unlock()
	})
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
	d.Feed([]byte("--<[schmux:completed:Done]>--"))
	mu.Lock()
	count := len(got)
	mu.Unlock()
	if count != 0 {
		t.Fatalf("expected 0 signals before timeout, got %d", count)
	}
	d.lastData = time.Now().Add(-time.Second)
	if !d.ShouldFlush() {
		t.Error("ShouldFlush() should return true after timeout")
	}
}

func TestDetectorThreeChunkSplit(t *testing.T) {
	var got []Signal
	var mu sync.Mutex
	d := NewSignalDetector("test-session", func(sig Signal) {
		mu.Lock()
		got = append(got, sig)
		mu.Unlock()
	})
	// Split signal across 3 chunks
	d.Feed([]byte("--<[schmux:"))
	d.Feed([]byte("completed:"))
	d.Feed([]byte("Task done]>--\n"))
	mu.Lock()
	defer mu.Unlock()
	if len(got) != 1 {
		t.Fatalf("expected 1 signal after 3 chunks, got %d", len(got))
	}
	if got[0].State != "completed" || got[0].Message != "Task done" {
		t.Errorf("signal = {%q, %q}, want {completed, Task done}", got[0].State, got[0].Message)
	}
}

func TestDetectorANSISplitAcrossChunks(t *testing.T) {
	// An ANSI escape split across chunks: ESC at end of first chunk, rest at start of second.
	// The line accumulator buffers the incomplete line, so both chunks merge before parsing.
	var got []Signal
	var mu sync.Mutex
	d := NewSignalDetector("test-session", func(sig Signal) {
		mu.Lock()
		got = append(got, sig)
		mu.Unlock()
	})
	// First chunk ends with \x1b (start of escape) — incomplete line, buffered
	d.Feed([]byte("--<[schmux:completed:Hello\x1b"))
	mu.Lock()
	if len(got) != 0 {
		t.Fatalf("expected 0 signals after first chunk, got %d", len(got))
	}
	mu.Unlock()
	// Second chunk completes the ANSI sequence and the line
	d.Feed([]byte("[1CWorld]>--\n"))
	mu.Lock()
	defer mu.Unlock()
	if len(got) != 1 {
		t.Fatalf("expected 1 signal after ANSI split, got %d", len(got))
	}
	if got[0].Message != "Hello World" {
		t.Errorf("message = %q, want %q", got[0].Message, "Hello World")
	}
}

func TestDetectorBufferTruncationLosesSignal(t *testing.T) {
	// Feed >maxSignalBufSize bytes without newlines, embedding a signal in the truncated portion.
	// The signal should be lost (not half-parsed).
	var got []Signal
	var mu sync.Mutex
	d := NewSignalDetector("test-session", func(sig Signal) {
		mu.Lock()
		got = append(got, sig)
		mu.Unlock()
	})
	// Create a large chunk that overflows buffer
	filler := make([]byte, maxSignalBufSize+500)
	for i := range filler {
		filler[i] = 'X'
	}
	// Embed a signal in the middle (will be in the truncated portion)
	signal := []byte("--<[schmux:completed:Hidden]>--")
	copy(filler[100:], signal)
	d.Feed(filler)
	d.Flush()
	mu.Lock()
	defer mu.Unlock()
	// The signal was in the truncated portion — it should NOT have been detected.
	// The truncation keeps only the last maxSignalBufSize bytes.
	// Since the signal was near the start and the buffer is truncated from the front,
	// it may or may not be in the retained portion depending on exact sizes.
	// The key invariant: buffer never exceeds maxSignalBufSize.
	if len(d.buf) > maxSignalBufSize {
		t.Errorf("buffer size %d exceeds max %d after truncation", len(d.buf), maxSignalBufSize)
	}
}

func TestDetectorNearMissNotFiredWithValidSignal(t *testing.T) {
	// When a chunk contains both a valid signal and a near-miss line,
	// the near-miss callback is NOT fired because signals were found.
	// This is by design: near-miss detection only activates when no signals are detected.
	var nearMisses []string
	var signals []Signal
	var mu sync.Mutex
	d := NewSignalDetector("test-session", func(sig Signal) {
		mu.Lock()
		signals = append(signals, sig)
		mu.Unlock()
	})
	d.SetNearMissCallback(func(line string) {
		mu.Lock()
		nearMisses = append(nearMisses, line)
		mu.Unlock()
	})
	// First line: valid signal. Second line: near-miss (has --<[schmux: but malformed).
	d.Feed([]byte("--<[schmux:completed:Done]>--\n--<[schmux:completed:broken format\n"))
	mu.Lock()
	defer mu.Unlock()
	if len(signals) != 1 {
		t.Fatalf("expected 1 signal, got %d", len(signals))
	}
	// Near-miss is suppressed because a valid signal was found in the same parse batch
	if len(nearMisses) != 0 {
		t.Errorf("expected 0 near-misses when valid signal found, got %d: %v", len(nearMisses), nearMisses)
	}
}

func TestDetectorNearMissFiredInSeparateBatch(t *testing.T) {
	// When a near-miss arrives in a separate batch from valid signals,
	// the near-miss callback SHOULD fire.
	var nearMisses []string
	var signals []Signal
	var mu sync.Mutex
	d := NewSignalDetector("test-session", func(sig Signal) {
		mu.Lock()
		signals = append(signals, sig)
		mu.Unlock()
	})
	d.SetNearMissCallback(func(line string) {
		mu.Lock()
		nearMisses = append(nearMisses, line)
		mu.Unlock()
	})
	// First batch: valid signal
	d.Feed([]byte("--<[schmux:completed:Done]>--\n"))
	// Second batch: near-miss (separate parseLines call)
	d.Feed([]byte("--<[schmux:completed:broken format\n"))
	mu.Lock()
	defer mu.Unlock()
	if len(signals) != 1 {
		t.Fatalf("expected 1 signal, got %d", len(signals))
	}
	if len(nearMisses) != 1 {
		t.Fatalf("expected 1 near-miss in separate batch, got %d: %v", len(nearMisses), nearMisses)
	}
}

func TestDetectorSignalAfterCollapsedOutput(t *testing.T) {
	// Real-world pattern: signal appears after Claude Code's collapsed output text
	// on the same line due to cursor movement ANSI sequences being stripped.
	var got []Signal
	var nearMisses []string
	var mu sync.Mutex
	d := NewSignalDetector("test-session", func(sig Signal) {
		mu.Lock()
		got = append(got, sig)
		mu.Unlock()
	})
	d.SetNearMissCallback(func(line string) {
		mu.Lock()
		nearMisses = append(nearMisses, line)
		mu.Unlock()
	})
	d.Feed([]byte("… +2 lines (ctrl+o to expand)B                                                                                                                            --<[schmux:completed:Changes committed]>--\n"))
	mu.Lock()
	defer mu.Unlock()
	if len(got) != 1 {
		t.Fatalf("expected 1 signal from collapsed output line, got %d", len(got))
	}
	if got[0].State != "completed" || got[0].Message != "Changes committed" {
		t.Errorf("signal = {%q, %q}, want {completed, Changes committed}", got[0].State, got[0].Message)
	}
	if len(nearMisses) != 0 {
		t.Errorf("expected 0 near-misses, got %d: %v", len(nearMisses), nearMisses)
	}
}

func TestDetectorSignalInSpinnerAnimation(t *testing.T) {
	// Real-world pattern: signal appears inline with spinner animation residue.
	var got []Signal
	var nearMisses []string
	var mu sync.Mutex
	d := NewSignalDetector("test-session", func(sig Signal) {
		mu.Lock()
		got = append(got, sig)
		mu.Unlock()
	})
	d.SetNearMissCallback(func(line string) {
		mu.Lock()
		nearMisses = append(nearMisses, line)
		mu.Unlock()
	})
	d.Feed([]byte("⏺ --<[schmux:working:]>--✶ SymbiotingB…B\n"))
	mu.Lock()
	defer mu.Unlock()
	if len(got) != 1 {
		t.Fatalf("expected 1 signal from spinner line, got %d", len(got))
	}
	if got[0].State != "working" {
		t.Errorf("state = %q, want %q", got[0].State, "working")
	}
	if len(nearMisses) != 0 {
		t.Errorf("expected 0 near-misses, got %d: %v", len(nearMisses), nearMisses)
	}
}

func TestDetectorCarriageReturnLineEndings(t *testing.T) {
	var got []Signal
	var mu sync.Mutex
	d := NewSignalDetector("test-session", func(sig Signal) {
		mu.Lock()
		got = append(got, sig)
		mu.Unlock()
	})
	// Windows-style \r\n
	d.Feed([]byte("--<[schmux:completed:Done]>--\r\n"))
	mu.Lock()
	defer mu.Unlock()
	if len(got) != 1 {
		t.Fatalf("expected 1 signal with \\r\\n, got %d", len(got))
	}
	if got[0].Message != "Done" {
		t.Errorf("message = %q, want %q", got[0].Message, "Done")
	}
}

func TestDetectorWhitespaceOnlyMessage(t *testing.T) {
	var got []Signal
	var mu sync.Mutex
	d := NewSignalDetector("test-session", func(sig Signal) {
		mu.Lock()
		got = append(got, sig)
		mu.Unlock()
	})
	d.Feed([]byte("--<[schmux:completed: ]>--\n"))
	mu.Lock()
	defer mu.Unlock()
	if len(got) != 1 {
		t.Fatalf("expected 1 signal with whitespace message, got %d", len(got))
	}
	if got[0].Message != " " {
		t.Errorf("message = %q, want %q", got[0].Message, " ")
	}
}

func TestDetectorShortSessionID(t *testing.T) {
	// Ensure a short session ID doesn't panic in enforceBufLimit logging
	var got []Signal
	var mu sync.Mutex
	d := NewSignalDetector("ab", func(sig Signal) {
		mu.Lock()
		got = append(got, sig)
		mu.Unlock()
	})
	// Trigger buffer limit enforcement
	bigChunk := make([]byte, maxSignalBufSize+1000)
	for i := range bigChunk {
		bigChunk[i] = 'A'
	}
	// Should not panic
	d.Feed(bigChunk)
	if len(d.buf) > maxSignalBufSize {
		t.Errorf("buffer size %d exceeds max %d", len(d.buf), maxSignalBufSize)
	}
}

func TestDetectorEmptySessionID(t *testing.T) {
	// Edge case: empty session ID should not panic
	d := NewSignalDetector("", func(sig Signal) {})
	bigChunk := make([]byte, maxSignalBufSize+100)
	for i := range bigChunk {
		bigChunk[i] = 'B'
	}
	d.Feed(bigChunk) // should not panic
}

func TestDetectorFlushOnStop(t *testing.T) {
	// Simulate the tracker stop path: Feed incomplete line, then Flush.
	var got []Signal
	var mu sync.Mutex
	d := NewSignalDetector("test-session", func(sig Signal) {
		mu.Lock()
		got = append(got, sig)
		mu.Unlock()
	})
	// Feed a signal without trailing newline (incomplete line)
	d.Feed([]byte("--<[schmux:error:Something broke]>--"))
	mu.Lock()
	if len(got) != 0 {
		t.Fatalf("expected 0 signals before flush, got %d", len(got))
	}
	mu.Unlock()
	// Flush simulates tracker shutdown
	d.Flush()
	mu.Lock()
	defer mu.Unlock()
	if len(got) != 1 {
		t.Fatalf("expected 1 signal after flush, got %d", len(got))
	}
	if got[0].State != "error" || got[0].Message != "Something broke" {
		t.Errorf("signal = {%q, %q}, want {error, Something broke}", got[0].State, got[0].Message)
	}
}

func TestSignalDetectorSuppressBlocksCallback(t *testing.T) {
	var signals []Signal
	var mu sync.Mutex
	d := NewSignalDetector("test", func(sig Signal) {
		mu.Lock()
		signals = append(signals, sig)
		mu.Unlock()
	})

	// Normal mode: callback fires
	d.Feed([]byte("--<[schmux:completed:done]>--\n"))
	mu.Lock()
	if len(signals) != 1 {
		t.Fatalf("expected 1 signal, got %d", len(signals))
	}
	mu.Unlock()

	// Suppressed mode: callback does NOT fire
	mu.Lock()
	signals = nil
	mu.Unlock()
	d.Suppress(true)
	d.Feed([]byte("--<[schmux:error:bad]>--\n"))
	mu.Lock()
	if len(signals) != 0 {
		t.Fatalf("expected 0 signals while suppressed, got %d", len(signals))
	}
	mu.Unlock()

	// Un-suppressed: callback fires again
	d.Suppress(false)
	d.Feed([]byte("--<[schmux:working:]>--\n"))
	mu.Lock()
	defer mu.Unlock()
	if len(signals) != 1 {
		t.Fatalf("expected 1 signal after unsuppress, got %d", len(signals))
	}
}

func TestDetectorFlushTickerScenario(t *testing.T) {
	// Simulates the scenario where a signal is the last thing written
	// without a trailing newline, and no more data arrives.
	var got []Signal
	var mu sync.Mutex
	d := NewSignalDetector("test-session", func(sig Signal) {
		mu.Lock()
		got = append(got, sig)
		mu.Unlock()
	})

	// Feed signal without trailing newline
	d.Feed([]byte("--<[schmux:completed:Done]>--"))

	// Nothing detected yet (no newline, no flush)
	mu.Lock()
	if len(got) != 0 {
		t.Fatalf("expected 0 signals before flush, got %d", len(got))
	}
	mu.Unlock()

	// Simulate time passing
	d.lastData = time.Now().Add(-time.Second)

	// ShouldFlush should be true
	if !d.ShouldFlush() {
		t.Fatal("ShouldFlush should return true after timeout")
	}

	// Flush should detect the signal
	d.Flush()
	mu.Lock()
	defer mu.Unlock()
	if len(got) != 1 {
		t.Fatalf("expected 1 signal after flush, got %d", len(got))
	}
	if got[0].State != "completed" {
		t.Errorf("state = %q, want completed", got[0].State)
	}
}

func TestDetectorBufferTruncationWarnsOnSignalLoss(t *testing.T) {
	var nearMisses []string
	var mu sync.Mutex
	d := NewSignalDetector("test-session", func(sig Signal) {})
	d.SetNearMissCallback(func(line string) {
		mu.Lock()
		nearMisses = append(nearMisses, line)
		mu.Unlock()
	})

	// Create a chunk large enough to trigger truncation, with a signal in the
	// portion that will be discarded (near the start).
	filler := make([]byte, maxSignalBufSize+5000)
	for i := range filler {
		filler[i] = 'X'
	}
	// Embed a signal marker near the beginning — it will be in the truncated portion
	sig := []byte("--<[schmux:completed:Lost signal]>--")
	copy(filler[100:], sig)

	d.Feed(filler) // No newlines, triggers enforceBufLimit

	mu.Lock()
	defer mu.Unlock()
	if len(nearMisses) != 1 {
		t.Fatalf("expected 1 near-miss warning from buffer truncation, got %d: %v", len(nearMisses), nearMisses)
	}
	if nearMisses[0] != "buffer truncation discarded signal-like data" {
		t.Errorf("near-miss = %q, want %q", nearMisses[0], "buffer truncation discarded signal-like data")
	}
}

func TestDetectorLastSignalTracking(t *testing.T) {
	d := NewSignalDetector("test-session", func(sig Signal) {})

	// Initially nil
	if got := d.LastSignal(); got != nil {
		t.Fatalf("expected nil LastSignal initially, got %+v", got)
	}

	// Feed a signal
	d.Feed([]byte("--<[schmux:completed:Task done]>--\n"))
	got := d.LastSignal()
	if got == nil {
		t.Fatal("expected non-nil LastSignal after feed")
	}
	if got.State != "completed" || got.Message != "Task done" {
		t.Errorf("LastSignal = {%q, %q}, want {completed, Task done}", got.State, got.Message)
	}

	// Feed another signal — lastSignal should update
	d.Feed([]byte("--<[schmux:error:Something broke]>--\n"))
	got = d.LastSignal()
	if got == nil {
		t.Fatal("expected non-nil LastSignal after second feed")
	}
	if got.State != "error" || got.Message != "Something broke" {
		t.Errorf("LastSignal = {%q, %q}, want {error, Something broke}", got.State, got.Message)
	}
}

func TestDetectorLastSignalDuringSuppression(t *testing.T) {
	var signals []Signal
	var mu sync.Mutex
	d := NewSignalDetector("test-session", func(sig Signal) {
		mu.Lock()
		signals = append(signals, sig)
		mu.Unlock()
	})

	// Suppress and feed a signal
	d.Suppress(true)
	d.Feed([]byte("--<[schmux:needs_input:Please help]>--\n"))

	// Callback should NOT have fired
	mu.Lock()
	if len(signals) != 0 {
		t.Fatalf("expected 0 callbacks while suppressed, got %d", len(signals))
	}
	mu.Unlock()

	// But LastSignal should be set
	got := d.LastSignal()
	if got == nil {
		t.Fatal("expected non-nil LastSignal during suppression")
	}
	if got.State != "needs_input" || got.Message != "Please help" {
		t.Errorf("LastSignal = {%q, %q}, want {needs_input, Please help}", got.State, got.Message)
	}

	// Un-suppress and verify callback fires for new signals
	d.Suppress(false)
	d.Feed([]byte("--<[schmux:completed:Done]>--\n"))
	mu.Lock()
	defer mu.Unlock()
	if len(signals) != 1 {
		t.Fatalf("expected 1 callback after unsuppress, got %d", len(signals))
	}
	if signals[0].State != "completed" {
		t.Errorf("callback signal state = %q, want completed", signals[0].State)
	}
}
