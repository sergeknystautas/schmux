package session

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/sergeknystautas/schmux/internal/remote/controlmode"
	"github.com/sergeknystautas/schmux/internal/state"
)

// newTestTracker creates a SessionRuntime backed by a MockControlSource for testing.
func newTestTracker(sessionID string) (*SessionRuntime, *MockControlSource) {
	st := state.New("", nil)
	mock := NewMockControlSource(100)
	tracker := NewSessionRuntime(sessionID, mock, st, "", nil, nil, nil)
	return tracker, mock
}

// newTestSessionRuntime creates a SessionRuntime suitable for unit-testing
// fanOut and clipboard wiring. The dashboard subscriber goroutine is NOT
// wired (that happens in dashboard server startup); tests that interact with
// sr.clipboardCh directly are safe from races because no consumer is reading.
func newTestSessionRuntime(t *testing.T) *SessionRuntime {
	t.Helper()
	tracker, _ := newTestTracker("test-session")
	return tracker
}

func TestSessionRuntimeInputResizeWithoutControlMode(t *testing.T) {
	tracker, _ := newTestTracker("s1")

	// MockControlSource returns nil for SendKeys/Resize (always "attached"),
	// so these won't error with the mock. Test the real LocalSource path instead.
	source := NewLocalSource("s1", "tmux-s1", nil, nil)
	st := state.New("", nil)
	localTracker := NewSessionRuntime("s1", source, st, "", nil, nil, nil)

	if _, err := localTracker.SendInput("abc"); err == nil {
		t.Fatal("expected error when control mode is not attached")
	}
	err := localTracker.Resize(80, 24)
	if err == nil {
		t.Fatal("expected error when control mode is not attached")
	}
	_ = tracker // use the mock tracker elsewhere
}

func TestControlSourceInterfaceConformance(t *testing.T) {
	var _ ControlSource = (*LocalSource)(nil)
	var _ ControlSource = (*RemoteSource)(nil)
}

func TestTrackerCounters_Increment(t *testing.T) {
	var c TrackerCounters

	t.Run("basic increments are recorded", func(t *testing.T) {
		c.EventsDelivered.Add(5)
		c.BytesDelivered.Add(1024)
		c.Reconnects.Add(1)
		c.FanOutDrops.Add(3)

		if c.EventsDelivered.Load() != 5 {
			t.Errorf("EventsDelivered = %d, want 5", c.EventsDelivered.Load())
		}
		if c.BytesDelivered.Load() != 1024 {
			t.Errorf("BytesDelivered = %d, want 1024", c.BytesDelivered.Load())
		}
		if c.Reconnects.Load() != 1 {
			t.Errorf("Reconnects = %d, want 1", c.Reconnects.Load())
		}
		if c.FanOutDrops.Load() != 3 {
			t.Errorf("FanOutDrops = %d, want 3", c.FanOutDrops.Load())
		}
	})

	t.Run("concurrent increments are race-free", func(t *testing.T) {
		var counters TrackerCounters
		const goroutines = 10
		const increments = 100

		var wg sync.WaitGroup
		wg.Add(goroutines)
		for i := 0; i < goroutines; i++ {
			go func() {
				defer wg.Done()
				for j := 0; j < increments; j++ {
					counters.EventsDelivered.Add(1)
					counters.BytesDelivered.Add(10)
					counters.Reconnects.Add(1)
					counters.FanOutDrops.Add(1)
				}
			}()
		}
		wg.Wait()

		want := int64(goroutines * increments)
		if got := counters.EventsDelivered.Load(); got != want {
			t.Errorf("EventsDelivered = %d, want %d", got, want)
		}
		if got := counters.BytesDelivered.Load(); got != want*10 {
			t.Errorf("BytesDelivered = %d, want %d", got, want*10)
		}
		if got := counters.Reconnects.Load(); got != want {
			t.Errorf("Reconnects = %d, want %d", got, want)
		}
		if got := counters.FanOutDrops.Load(); got != want {
			t.Errorf("FanOutDrops = %d, want %d", got, want)
		}
	})
}

func TestCaptureLastLinesDelegatesToSource(t *testing.T) {
	tracker, mock := newTestTracker("s1")
	mock.CaptureContent = "line1\nline2\n❯\n"

	content, err := tracker.CaptureLastLines(context.Background(), 100)
	if err != nil {
		t.Fatalf("CaptureLastLines error: %v", err)
	}
	if content != "line1\nline2\n❯\n" {
		t.Errorf("CaptureLastLines = %q, want %q", content, "line1\nline2\n❯\n")
	}
}

func TestCaptureLastLinesPropagatesSourceError(t *testing.T) {
	tracker, mock := newTestTracker("s1")
	mock.CaptureErr = fmt.Errorf("SSH connection closed")

	_, err := tracker.CaptureLastLines(context.Background(), 100)
	if err == nil {
		t.Fatal("expected error from CaptureLastLines")
	}
	if !strings.Contains(err.Error(), "SSH connection closed") {
		t.Errorf("error = %q, want it to contain 'SSH connection closed'", err.Error())
	}
}

func TestSubscribeUnsubscribeOutput(t *testing.T) {
	tracker, _ := newTestTracker("s1")

	// Subscribe creates a channel that stays open (survives reconnections)
	ch := tracker.SubscribeOutput()

	// Verify it's in the subscriber list
	tracker.subsMu.Lock()
	if len(tracker.subs) != 1 {
		t.Fatalf("expected 1 subscriber, got %d", len(tracker.subs))
	}
	tracker.subsMu.Unlock()

	// Unsubscribe removes it (but does NOT close the channel — that would
	// race with fanOut sending to it)
	tracker.UnsubscribeOutput(ch)

	tracker.subsMu.Lock()
	if len(tracker.subs) != 0 {
		t.Fatalf("expected 0 subscribers after unsubscribe, got %d", len(tracker.subs))
	}
	tracker.subsMu.Unlock()

	// Channel should NOT be closed after unsubscribe (to prevent send-to-closed-channel
	// panics in fanOut). It stays open; GC reclaims it.
	select {
	case <-ch:
		t.Fatal("channel should not be closed or readable after unsubscribe")
	default:
		// expected: channel is open but empty
	}
}

func TestCapturePane_NoControlMode(t *testing.T) {
	source := NewLocalSource("test-id", "test-tmux", nil, nil)
	st := state.New("", nil)
	tracker := NewSessionRuntime("test-id", source, st, "", nil, nil, nil)

	_, err := tracker.CapturePane(context.Background())
	if err == nil {
		t.Fatal("expected error when control mode is not attached")
	}
}

func TestTrackerOutputLog_FanOutRecordsSequences(t *testing.T) {
	tracker, _ := newTestTracker("s1")

	// Subscribe so we can also verify events arrive
	ch := tracker.SubscribeOutput()
	defer tracker.UnsubscribeOutput(ch)

	// Simulate fan-out (normally called by run() draining source events)
	tracker.fanOut(controlmode.OutputEvent{PaneID: "%0", Data: "hello"})
	tracker.fanOut(controlmode.OutputEvent{PaneID: "%0", Data: "world"})

	// Verify output log captured the data
	if tracker.OutputLog().CurrentSeq() != 2 {
		t.Fatalf("expected currentSeq=2, got %d", tracker.OutputLog().CurrentSeq())
	}

	// Verify subscriber events carry correct sequence numbers
	ev1 := <-ch
	if ev1.Seq != 0 || ev1.Data != "hello" {
		t.Errorf("event 1: seq=%d data=%q, want seq=0 data='hello'", ev1.Seq, ev1.Data)
	}
	ev2 := <-ch
	if ev2.Seq != 1 || ev2.Data != "world" {
		t.Errorf("event 2: seq=%d data=%q, want seq=1 data='world'", ev2.Seq, ev2.Data)
	}

	entries := tracker.OutputLog().ReplayFrom(0)
	if len(entries) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(entries))
	}
	if string(entries[0].Data) != "hello" {
		t.Errorf("entry 0 data=%q, want 'hello'", entries[0].Data)
	}
}

func TestFanOut_ConcurrentSequences(t *testing.T) {
	tracker, _ := newTestTracker("s1")

	ch := tracker.SubscribeOutput()
	defer tracker.UnsubscribeOutput(ch)

	const N = 500
	var wg sync.WaitGroup
	wg.Add(N)
	for i := 0; i < N; i++ {
		go func(i int) {
			defer wg.Done()
			tracker.fanOut(controlmode.OutputEvent{PaneID: "%0", Data: fmt.Sprintf("msg-%d", i)})
		}(i)
	}
	wg.Wait()

	// Drain all events and verify seq numbers are unique and monotonically increasing
	seen := make(map[uint64]bool, N)
	for i := 0; i < N; i++ {
		ev := <-ch
		if seen[ev.Seq] {
			t.Fatalf("duplicate seq %d", ev.Seq)
		}
		seen[ev.Seq] = true
	}
	if len(seen) != N {
		t.Errorf("expected %d unique seqs, got %d", N, len(seen))
	}
	// All seqs should be 0..N-1
	for i := uint64(0); i < N; i++ {
		if !seen[i] {
			t.Errorf("missing seq %d", i)
		}
	}
}

func TestFanOut_SlowConsumerDrop(t *testing.T) {
	tracker, _ := newTestTracker("s1")

	slowCh := tracker.SubscribeOutput()
	defer tracker.UnsubscribeOutput(slowCh)

	fastCh := tracker.SubscribeOutput()
	defer tracker.UnsubscribeOutput(fastCh)

	// Fill the slow consumer's buffer (capacity is 1000)
	for i := 0; i < 1000; i++ {
		tracker.fanOut(controlmode.OutputEvent{PaneID: "%0", Data: fmt.Sprintf("fill-%d", i)})
	}
	// Drain fast consumer so it's ready for the next event
	for i := 0; i < 1000; i++ {
		<-fastCh
	}

	// Now send one more — slow consumer should get dropped, fast should receive
	dropsBefore := tracker.Counters.FanOutDrops.Load()
	tracker.fanOut(controlmode.OutputEvent{PaneID: "%0", Data: "overflow"})

	// Fast consumer should receive it
	ev := <-fastCh
	if ev.Data != "overflow" {
		t.Errorf("fast consumer got %q, want 'overflow'", ev.Data)
	}

	// Slow consumer drop counter should have incremented
	dropsAfter := tracker.Counters.FanOutDrops.Load()
	if dropsAfter <= dropsBefore {
		t.Errorf("FanOutDrops: before=%d after=%d, expected increment", dropsBefore, dropsAfter)
	}
}

func TestFanOut_MultipleSubscribers(t *testing.T) {
	tracker, _ := newTestTracker("s1")

	const numSubs = 3
	channels := make([]<-chan SequencedOutput, numSubs)
	for i := 0; i < numSubs; i++ {
		channels[i] = tracker.SubscribeOutput()
	}
	defer func() {
		for _, ch := range channels {
			tracker.UnsubscribeOutput(ch)
		}
	}()

	// Send 5 events
	for i := 0; i < 5; i++ {
		tracker.fanOut(controlmode.OutputEvent{PaneID: "%0", Data: fmt.Sprintf("event-%d", i)})
	}

	// All 3 subscribers should receive the same 5 events with identical seqs
	for subIdx, ch := range channels {
		for i := 0; i < 5; i++ {
			ev := <-ch
			if ev.Seq != uint64(i) {
				t.Errorf("sub %d event %d: seq=%d, want %d", subIdx, i, ev.Seq, i)
			}
			expected := fmt.Sprintf("event-%d", i)
			if ev.Data != expected {
				t.Errorf("sub %d event %d: data=%q, want %q", subIdx, i, ev.Data, expected)
			}
		}
	}
}

func TestIsPermanentError(t *testing.T) {
	tests := []struct {
		name     string
		err      error
		expected bool
	}{
		{
			name:     "nil error",
			err:      nil,
			expected: false,
		},
		{
			name:     "can't find session error",
			err:      errors.New("can't find session: my-session"),
			expected: true,
		},
		{
			name:     "no session found error",
			err:      errors.New("no session found: test"),
			expected: true,
		},
		{
			name:     "transient error",
			err:      errors.New("connection refused"),
			expected: false,
		},
		{
			name:     "timeout error",
			err:      errors.New("operation timed out"),
			expected: false,
		},
		{
			name:     "permission denied error",
			err:      errors.New("permission denied"),
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := isPermanentError(tt.err)
			if result != tt.expected {
				t.Errorf("isPermanentError(%v) = %v, want %v", tt.err, result, tt.expected)
			}
		})
	}
}

// TestFanOut_ConcurrentUnsubscribe verifies that concurrent fanOut and
// UnsubscribeOutput calls don't panic. This is the regression test for the
// send-to-closed-channel bug where UnsubscribeOutput used to close(sub).
func TestFanOut_ConcurrentUnsubscribe(t *testing.T) {
	tracker, _ := newTestTracker("s1")

	const numGoroutines = 100
	var wg sync.WaitGroup

	// Half the goroutines call fanOut, the other half subscribe then unsubscribe
	wg.Add(numGoroutines * 2)

	// Fan-out goroutines
	for i := 0; i < numGoroutines; i++ {
		go func(i int) {
			defer wg.Done()
			tracker.fanOut(controlmode.OutputEvent{PaneID: "%0", Data: fmt.Sprintf("msg-%d", i)})
		}(i)
	}

	// Subscribe/unsubscribe goroutines (exercises the race path)
	for i := 0; i < numGoroutines; i++ {
		go func() {
			defer wg.Done()
			ch := tracker.SubscribeOutput()
			// Drain one event if available (non-blocking) to keep the test fast
			select {
			case <-ch:
			default:
			}
			tracker.UnsubscribeOutput(ch)
		}()
	}

	wg.Wait()
}

// TestTrackerRecorderFactory verifies that the RecorderFactory is called
// and the recorder runs alongside the tracker.
func TestTrackerRecorderFactory(t *testing.T) {
	mock := NewMockControlSource(100)
	st := state.New("", nil)
	tracker := NewSessionRuntime("s1", mock, st, "", nil, nil, nil)

	started := make(chan struct{})
	stopped := make(chan struct{})

	tracker.RecorderFactory = func(ol *OutputLog, gapCh <-chan SourceEvent) Runnable {
		return &testRunnable{started: started, stopped: stopped}
	}

	tracker.Start()

	// Emit an event to trigger the run loop
	mock.Emit(SourceEvent{Type: SourceOutput, Data: "hello"})

	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("recorder was not started")
	}

	mock.Close()
	tracker.Stop()

	select {
	case <-stopped:
	case <-time.After(2 * time.Second):
		t.Fatal("recorder was not stopped")
	}
}

type testRunnable struct {
	started chan struct{}
	stopped chan struct{}
}

func (r *testRunnable) Run()  { close(r.started); <-r.stopped }
func (r *testRunnable) Stop() { close(r.stopped) }

// TestTrackerRunDrainsSourceEvents verifies that the tracker's run() method
// correctly drains events from the source and fans them out.
func TestTrackerRunDrainsSourceEvents(t *testing.T) {
	tracker, mock := newTestTracker("s1")

	ch := tracker.SubscribeOutput()
	defer tracker.UnsubscribeOutput(ch)

	tracker.Start()

	// Emit output events through the mock source
	mock.Emit(SourceEvent{Type: SourceOutput, Data: "hello"})
	mock.Emit(SourceEvent{Type: SourceOutput, Data: "world"})

	// Verify they arrive at the subscriber
	ev1 := <-ch
	if ev1.Data != "hello" {
		t.Errorf("event 1 data = %q, want 'hello'", ev1.Data)
	}
	ev2 := <-ch
	if ev2.Data != "world" {
		t.Errorf("event 2 data = %q, want 'world'", ev2.Data)
	}

	// Close the source, which should cause run() to exit
	mock.Close()
	tracker.Stop()
}

// TestTrackerCountersHasClipboardDrops verifies that the new ClipboardDrops
// counter is wired in TrackerCounters and behaves like the other atomics.
func TestTrackerCountersHasClipboardDrops(t *testing.T) {
	var c TrackerCounters
	c.ClipboardDrops.Add(1)
	if c.ClipboardDrops.Load() != 1 {
		t.Errorf("ClipboardDrops not present or not atomic")
	}
}

// TestSessionRuntime_HasClipboardChannel verifies clipboardCh + extractor are
// initialized in the constructor with the documented capacity-1 drop pattern.
func TestSessionRuntime_HasClipboardChannel(t *testing.T) {
	sr := newTestSessionRuntime(t)
	if sr.clipboardCh == nil {
		t.Fatal("clipboardCh not initialized")
	}
	if cap(sr.clipboardCh) != 1 {
		t.Errorf("clipboardCh capacity = %d, want 1 (drop-on-overflow pattern)", cap(sr.clipboardCh))
	}
	if sr.extractor == nil {
		t.Fatal("extractor not initialized")
	}
	if got := sr.ClipboardCh(); got == nil {
		t.Fatal("ClipboardCh() returned nil")
	}
}

// TestFanOut_StripsOSC52FromOutputLog verifies the extractor removes OSC 52
// bytes from the output log so terminals never render the escape sequence.
func TestFanOut_StripsOSC52FromOutputLog(t *testing.T) {
	sr := newTestSessionRuntime(t)
	sr.fanOut(controlmode.OutputEvent{Data: "\x1b]52;c;aGVsbG8=\x07"})
	entries := sr.outputLog.ReplayFrom(0)
	if len(entries) != 1 || len(entries[0].Data) != 0 {
		t.Errorf("expected one zero-length entry, got %+v", entries)
	}
}

// TestFanOut_EmitsClipboardRequest verifies a ClipboardRequest reaches
// clipboardCh with the decoded text after OSC 52 extraction.
func TestFanOut_EmitsClipboardRequest(t *testing.T) {
	sr := newTestSessionRuntime(t)
	sr.fanOut(controlmode.OutputEvent{Data: "\x1b]52;c;aGVsbG8=\x07"})
	select {
	case req := <-sr.clipboardCh:
		if req.Text != "hello" {
			t.Errorf("Text=%q want hello", req.Text)
		}
	case <-time.After(100 * time.Millisecond):
		t.Fatal("expected ClipboardRequest, got none")
	}
}

// TestFanOut_DropsOnFullChannel verifies the drop-on-overflow path increments
// ClipboardDrops when clipboardCh is already full.
func TestFanOut_DropsOnFullChannel(t *testing.T) {
	sr := newTestSessionRuntime(t)
	sr.clipboardCh <- ClipboardRequest{Text: "first"}
	sr.fanOut(controlmode.OutputEvent{Data: "\x1b]52;c;Yg==\x07"})
	if sr.Counters.ClipboardDrops.Load() != 1 {
		t.Errorf("ClipboardDrops = %d, want 1", sr.Counters.ClipboardDrops.Load())
	}
}

// TestStop_ClosesClipboardChannel verifies Stop() closes clipboardCh after
// run() exits (subscriber-side detect-channel-close pattern).
func TestStop_ClosesClipboardChannel(t *testing.T) {
	sr, mock := newTestTracker("s1")
	sr.Start()
	mock.Close()
	sr.Stop()
	select {
	case _, ok := <-sr.clipboardCh:
		if ok {
			t.Error("clipboardCh delivered after Stop; expected closed")
		}
	case <-time.After(time.Second):
		t.Fatal("clipboardCh not closed within 1s of Stop")
	}
}

// TestRun_SourcePasteBufferEmitsClipboardRequest verifies that a
// SourcePasteBuffer event delivered by the source causes the tracker to push
// a ClipboardRequest onto clipboardCh — the same downstream pipeline as the
// OSC 52 byte path. This is the load-buffer / set-buffer fallback that
// catches TUIs which detect tmux control mode and bypass OSC 52.
func TestRun_SourcePasteBufferEmitsClipboardRequest(t *testing.T) {
	mock := NewMockControlSource(10)
	st := state.New("", nil)
	tracker := NewSessionRuntime("paste-sess", mock, st, "", nil, nil, nil)

	tracker.Start()
	defer func() {
		mock.Close()
		tracker.Stop()
	}()

	mock.Emit(SourceEvent{
		Type:                 SourcePasteBuffer,
		Data:                 "hello-from-load-buffer",
		ByteCount:            22,
		StrippedControlChars: 0,
	})

	select {
	case req := <-tracker.ClipboardCh():
		if req.SessionID != "paste-sess" {
			t.Errorf("SessionID = %q, want paste-sess", req.SessionID)
		}
		if req.Text != "hello-from-load-buffer" {
			t.Errorf("Text = %q, want hello-from-load-buffer", req.Text)
		}
		if req.ByteCount != 22 {
			t.Errorf("ByteCount = %d, want 22", req.ByteCount)
		}
		if req.StrippedControlChars != 0 {
			t.Errorf("StrippedControlChars = %d, want 0", req.StrippedControlChars)
		}
		if req.Timestamp.IsZero() {
			t.Error("Timestamp should be set")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("expected ClipboardRequest from SourcePasteBuffer event")
	}
}

// TestRun_SourcePasteBufferDropsOnFullChannel verifies the drop-on-overflow
// path increments ClipboardDrops when clipboardCh is already full. Mirrors
// the OSC 52 path's TestFanOut_DropsOnFullChannel.
func TestRun_SourcePasteBufferDropsOnFullChannel(t *testing.T) {
	mock := NewMockControlSource(10)
	st := state.New("", nil)
	tracker := NewSessionRuntime("paste-sess", mock, st, "", nil, nil, nil)

	// Pre-fill clipboardCh so the next push is forced to drop.
	tracker.clipboardCh <- ClipboardRequest{Text: "first"}

	tracker.Start()
	defer func() {
		mock.Close()
		tracker.Stop()
	}()

	mock.Emit(SourceEvent{
		Type:                 SourcePasteBuffer,
		Data:                 "second",
		ByteCount:            6,
		StrippedControlChars: 0,
	})

	// Wait until the drop counter increments (or fail by timeout).
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if tracker.Counters.ClipboardDrops.Load() == 1 {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Errorf("ClipboardDrops = %d, want 1", tracker.Counters.ClipboardDrops.Load())
}

// TestFanOut_SuppressesOSC52EchoOfRecentInput verifies the headline UX
// improvement: when a TUI emits OSC 52 with content matching what the user
// just typed (the Claude-Code-startup case), the ClipboardRequest never
// reaches clipboardCh and ClipboardSuppressedAsEcho ticks.
func TestFanOut_SuppressesOSC52EchoOfRecentInput(t *testing.T) {
	sr := newTestSessionRuntime(t)

	// User typed this prompt as part of a `claude "..."` invocation.
	const payload = "MARKER-test-12345"
	sr.echo.appendInput([]byte(payload), time.Now())

	// Same content arrives back as OSC 52 within milliseconds.
	// base64("MARKER-test-12345") = "TUFSS0VSLXRlc3QtMTIzNDU="
	sr.fanOut(controlmode.OutputEvent{Data: "\x1b]52;c;TUFSS0VSLXRlc3QtMTIzNDU=\x07"})

	select {
	case req := <-sr.clipboardCh:
		t.Fatalf("expected no ClipboardRequest, got %q", req.Text)
	case <-time.After(50 * time.Millisecond):
		// Expected — banner suppressed.
	}
	if got := sr.Counters.ClipboardSuppressedAsEcho.Load(); got != 1 {
		t.Errorf("ClipboardSuppressedAsEcho=%d, want 1", got)
	}
	if got := sr.Counters.ClipboardDrops.Load(); got != 0 {
		t.Errorf("ClipboardDrops=%d, want 0 (suppression is not an overflow drop)", got)
	}
}

// TestFanOut_DoesNotSuppressWhenInputDoesNotMatch verifies that legitimate
// yanks of generated content still surface a banner. SendInput "hello-world"
// then OSC 52 of unrelated text — request must reach clipboardCh.
func TestFanOut_DoesNotSuppressWhenInputDoesNotMatch(t *testing.T) {
	sr := newTestSessionRuntime(t)

	sr.echo.appendInput([]byte("hello-world-typed-by-user"), time.Now())
	// base64("generated-content-not-typed") = "Z2VuZXJhdGVkLWNvbnRlbnQtbm90LXR5cGVk"
	sr.fanOut(controlmode.OutputEvent{Data: "\x1b]52;c;Z2VuZXJhdGVkLWNvbnRlbnQtbm90LXR5cGVk\x07"})

	select {
	case req := <-sr.clipboardCh:
		if req.Text != "generated-content-not-typed" {
			t.Errorf("Text=%q, want generated-content-not-typed", req.Text)
		}
	case <-time.After(100 * time.Millisecond):
		t.Fatal("expected ClipboardRequest to reach clipboardCh; banner should fire for non-matching content")
	}
	if got := sr.Counters.ClipboardSuppressedAsEcho.Load(); got != 0 {
		t.Errorf("ClipboardSuppressedAsEcho=%d, want 0", got)
	}
}

// TestFanOut_DoesNotSuppressWhenInputIsTooOld verifies that input older than
// the window is no longer eligible. Override inputEchoWindow to a tiny value
// so the test runs in milliseconds.
func TestFanOut_DoesNotSuppressWhenInputIsTooOld(t *testing.T) {
	original := inputEchoWindow
	inputEchoWindow = 10 * time.Millisecond
	defer func() { inputEchoWindow = original }()

	sr := newTestSessionRuntime(t)
	sr.echo.appendInput([]byte("MARKER-old-input-12345"), time.Now())

	// Wait past the window before the OSC 52 arrives.
	time.Sleep(50 * time.Millisecond)

	// base64("MARKER-old-input-12345") = "TUFSS0VSLW9sZC1pbnB1dC0xMjM0NQ=="
	sr.fanOut(controlmode.OutputEvent{Data: "\x1b]52;c;TUFSS0VSLW9sZC1pbnB1dC0xMjM0NQ==\x07"})

	select {
	case req := <-sr.clipboardCh:
		if req.Text != "MARKER-old-input-12345" {
			t.Errorf("Text=%q, want MARKER-old-input-12345", req.Text)
		}
	case <-time.After(100 * time.Millisecond):
		t.Fatal("expected ClipboardRequest to reach clipboardCh; banner should fire when echo is past window")
	}
	if got := sr.Counters.ClipboardSuppressedAsEcho.Load(); got != 0 {
		t.Errorf("ClipboardSuppressedAsEcho=%d, want 0", got)
	}
}

// TestFanOut_DoesNotSuppressShortContent verifies the min-len guard: short
// content (< inputEchoMinLen) is never suppressed even if it matches input.
// Protects against accidental matches on tiny payloads.
func TestFanOut_DoesNotSuppressShortContent(t *testing.T) {
	sr := newTestSessionRuntime(t)
	sr.echo.appendInput([]byte("abc"), time.Now())

	// base64("abc") = "YWJj"
	sr.fanOut(controlmode.OutputEvent{Data: "\x1b]52;c;YWJj\x07"})

	select {
	case req := <-sr.clipboardCh:
		if req.Text != "abc" {
			t.Errorf("Text=%q, want abc", req.Text)
		}
	case <-time.After(100 * time.Millisecond):
		t.Fatal("expected ClipboardRequest; short content should bypass suppression")
	}
	if got := sr.Counters.ClipboardSuppressedAsEcho.Load(); got != 0 {
		t.Errorf("ClipboardSuppressedAsEcho=%d, want 0 for short content", got)
	}
}

// TestRun_SourcePasteBufferSuppressesEchoOfRecentInput is the
// SourcePasteBuffer equivalent of TestFanOut_SuppressesOSC52EchoOfRecentInput.
// A TUI bypassing OSC 52 in favor of tmux load-buffer can still echo input —
// the suppression must apply to that path too.
func TestRun_SourcePasteBufferSuppressesEchoOfRecentInput(t *testing.T) {
	mock := NewMockControlSource(10)
	st := state.New("", nil)
	tracker := NewSessionRuntime("paste-sess", mock, st, "", nil, nil, nil)

	const payload = "MARKER-paste-buffer-echo-12345"
	tracker.echo.appendInput([]byte(payload), time.Now())

	tracker.Start()
	defer func() {
		mock.Close()
		tracker.Stop()
	}()

	mock.Emit(SourceEvent{
		Type:                 SourcePasteBuffer,
		Data:                 payload,
		ByteCount:            len(payload),
		StrippedControlChars: 0,
	})

	// Wait until the suppression counter increments (or fail by timeout).
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if tracker.Counters.ClipboardSuppressedAsEcho.Load() == 1 {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	if got := tracker.Counters.ClipboardSuppressedAsEcho.Load(); got != 1 {
		t.Fatalf("ClipboardSuppressedAsEcho=%d, want 1", got)
	}

	// The suppressed request must NOT have reached clipboardCh.
	select {
	case req := <-tracker.ClipboardCh():
		t.Errorf("expected no ClipboardRequest, got %q", req.Text)
	case <-time.After(50 * time.Millisecond):
		// Expected.
	}
}

// TestSendInput_RecordsInEchoBuffer verifies the wiring from
// SessionRuntime.SendInput → echo.appendInput. We use a LocalSource (which
// errors with "not attached") so we don't need a live tmux, and we assert
// the buffer state directly afterward.
func TestSendInput_RecordsInEchoBuffer(t *testing.T) {
	source := NewLocalSource("s1", "tmux-s1", nil, nil)
	st := state.New("", nil)
	sr := NewSessionRuntime("s1", source, st, "", nil, nil, nil)

	// SendInput will error (no tmux) but should still record the bytes —
	// partial sends can still reach the pane.
	_, _ = sr.SendInput("MARKER-from-SendInput")

	if !sr.echo.matchesRecent("MARKER-from-SendInput", time.Now(), inputEchoWindow) {
		t.Error("SendInput did not record bytes in echo buffer")
	}
}

// TestDiagnosticCounters_ExposesClipboardSuppressedAsEcho verifies the new
// counter is surfaced by DiagnosticCounters() so the diagnose UI can show it.
func TestDiagnosticCounters_ExposesClipboardSuppressedAsEcho(t *testing.T) {
	sr := newTestSessionRuntime(t)
	sr.Counters.ClipboardSuppressedAsEcho.Add(7)

	got := sr.DiagnosticCounters()
	val, ok := got["clipboardSuppressedAsEcho"]
	if !ok {
		t.Fatal("clipboardSuppressedAsEcho not present in DiagnosticCounters output")
	}
	if val != 7 {
		t.Errorf("clipboardSuppressedAsEcho=%d, want 7", val)
	}
}

// TestTrackerResizeForwardsToGapCh verifies that calling Resize() on the
// tracker sends a SourceResize event to the gapCh used by the timelapse recorder.
func TestTrackerResizeForwardsToGapCh(t *testing.T) {
	mock := NewMockControlSource(100)
	st := state.New("", nil)
	tracker := NewSessionRuntime("s1", mock, st, "", nil, nil, nil)

	// Set up a recorder factory that captures the gapCh
	var capturedGapCh <-chan SourceEvent
	started := make(chan struct{})
	stopped := make(chan struct{})

	tracker.RecorderFactory = func(ol *OutputLog, gapCh <-chan SourceEvent) Runnable {
		capturedGapCh = gapCh
		return &testRunnable{started: started, stopped: stopped}
	}

	tracker.Start()

	// Emit an event to trigger the run loop and recorder creation
	mock.Emit(SourceEvent{Type: SourceOutput, Data: "init"})

	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("recorder was not started")
	}

	// Now call Resize — it should forward to gapCh
	if err := tracker.Resize(160, 48); err != nil {
		t.Fatalf("Resize failed: %v", err)
	}

	select {
	case event := <-capturedGapCh:
		if event.Type != SourceResize {
			t.Errorf("event type = %v, want SourceResize", event.Type)
		}
		if event.Width != 160 {
			t.Errorf("event width = %d, want 160", event.Width)
		}
		if event.Height != 48 {
			t.Errorf("event height = %d, want 48", event.Height)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("resize event was not forwarded to gapCh")
	}

	mock.Close()
	tracker.Stop()

	select {
	case <-stopped:
	case <-time.After(2 * time.Second):
		t.Fatal("recorder was not stopped")
	}
}
