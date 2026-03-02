package session

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"

	"github.com/sergeknystautas/schmux/internal/remote/controlmode"
	"github.com/sergeknystautas/schmux/internal/state"
)

func TestSessionTrackerInputResizeWithoutControlMode(t *testing.T) {
	st := state.New("", nil)
	tracker := NewSessionTracker("s1", "tmux-s1", st, "", nil, nil, nil)

	if err := tracker.SendInput("abc"); err == nil {
		t.Fatal("expected error when control mode is not attached")
	}
	err := tracker.Resize(80, 24)
	if err == nil {
		t.Fatal("expected error when control mode is not attached")
	}
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

func TestSubscribeUnsubscribeOutput(t *testing.T) {
	st := state.New("", nil)
	tracker := NewSessionTracker("s1", "tmux-s1", st, "", nil, nil, nil)

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
	st := state.New("", nil)
	tracker := NewSessionTracker("test-id", "test-tmux", st, "", nil, nil, nil)

	_, err := tracker.CapturePane(context.Background())
	if err == nil {
		t.Fatal("expected error when control mode is not attached")
	}
}

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
	st := state.New("", nil)
	tracker := NewSessionTracker("s1", "tmux-s1", st, "", nil, nil, nil)

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
	st := state.New("", nil)
	tracker := NewSessionTracker("s1", "tmux-s1", st, "", nil, nil, nil)

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
	st := state.New("", nil)
	tracker := NewSessionTracker("s1", "tmux-s1", st, "", nil, nil, nil)

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
	st := state.New("", nil)
	tracker := NewSessionTracker("s1", "tmux-s1", st, "", nil, nil, nil)

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
