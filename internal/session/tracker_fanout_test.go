package session

import (
	"testing"

	"github.com/sergeknystautas/schmux/internal/remote/controlmode"
	"github.com/sergeknystautas/schmux/internal/state"
)

func TestFanOut_SlowConsumerDropped(t *testing.T) {
	st := state.New("", nil)
	tracker := NewSessionTracker("s1", "tmux-s1", st, "", nil, nil, nil)

	// Add a fast consumer via normal subscription
	fastCh := tracker.SubscribeOutput()

	// Add a slow consumer with a tiny buffer directly to subs
	// (SubscribeOutput creates 1000-buffer channels, which would require
	// 1000 fanOuts to fill — instead we add a 1-buffer channel directly)
	slowCh := make(chan SequencedOutput, 1)
	tracker.subsMu.Lock()
	tracker.subs = append(tracker.subs, slowCh)
	tracker.subsMu.Unlock()

	// Fill the slow consumer's buffer
	slowCh <- SequencedOutput{OutputEvent: controlmode.OutputEvent{Data: "filler"}}

	// Now fanOut: fastCh should receive, slowCh should be dropped
	event := controlmode.OutputEvent{Data: "test-data"}
	tracker.fanOut(event)

	// fastCh should have received the event
	select {
	case got := <-fastCh:
		if got.Data != "test-data" {
			t.Errorf("fastCh got Data = %q, want %q", got.Data, "test-data")
		}
	default:
		t.Fatal("fastCh should have received the event")
	}

	// Verify the drop counter incremented for slowCh
	drops := tracker.Counters.FanOutDrops.Load()
	if drops != 1 {
		t.Errorf("FanOutDrops = %d, want 1", drops)
	}

	// Verify event delivery counter
	delivered := tracker.Counters.EventsDelivered.Load()
	if delivered != 1 {
		t.Errorf("EventsDelivered = %d, want 1", delivered)
	}

	// Verify bytes counter
	bytesDelivered := tracker.Counters.BytesDelivered.Load()
	if bytesDelivered != int64(len("test-data")) {
		t.Errorf("BytesDelivered = %d, want %d", bytesDelivered, len("test-data"))
	}

	// Clean up
	tracker.UnsubscribeOutput(fastCh)
	<-slowCh // drain
	tracker.subsMu.Lock()
	for i, ch := range tracker.subs {
		if ch == slowCh {
			tracker.subs = append(tracker.subs[:i], tracker.subs[i+1:]...)
			break
		}
	}
	tracker.subsMu.Unlock()
}

// TestFanOut_MultipleSubscribers_Upstream tests basic multi-subscriber fanOut.
// See also TestFanOut_MultipleSubscribers in tracker_test.go for seq verification.
func TestFanOut_MultipleSubscribers_Upstream(t *testing.T) {
	st := state.New("", nil)
	tracker := NewSessionTracker("s1", "tmux-s1", st, "", nil, nil, nil)

	ch1 := tracker.SubscribeOutput()
	ch2 := tracker.SubscribeOutput()
	ch3 := tracker.SubscribeOutput()

	event := controlmode.OutputEvent{Data: "broadcast"}
	tracker.fanOut(event)

	for i, ch := range []<-chan SequencedOutput{ch1, ch2, ch3} {
		select {
		case got := <-ch:
			if got.Data != "broadcast" {
				t.Errorf("subscriber %d got Data = %q, want %q", i, got.Data, "broadcast")
			}
		default:
			t.Errorf("subscriber %d did not receive the event", i)
		}
	}

	if drops := tracker.Counters.FanOutDrops.Load(); drops != 0 {
		t.Errorf("FanOutDrops = %d, want 0", drops)
	}

	tracker.UnsubscribeOutput(ch1)
	tracker.UnsubscribeOutput(ch2)
	tracker.UnsubscribeOutput(ch3)
}

func TestFanOut_NoSubscribers(t *testing.T) {
	st := state.New("", nil)
	tracker := NewSessionTracker("s1", "tmux-s1", st, "", nil, nil, nil)

	// fanOut with no subscribers should not panic
	event := controlmode.OutputEvent{Data: "orphan"}
	tracker.fanOut(event)

	if delivered := tracker.Counters.EventsDelivered.Load(); delivered != 1 {
		t.Errorf("EventsDelivered = %d, want 1", delivered)
	}
	if drops := tracker.Counters.FanOutDrops.Load(); drops != 0 {
		t.Errorf("FanOutDrops = %d, want 0", drops)
	}
}

func TestFanOut_DropDoesNotAffectOtherSubscribers(t *testing.T) {
	st := state.New("", nil)
	tracker := NewSessionTracker("s1", "tmux-s1", st, "", nil, nil, nil)

	// Two fast subscribers
	fast1 := tracker.SubscribeOutput()
	fast2 := tracker.SubscribeOutput()

	// One slow subscriber (tiny buffer, pre-filled)
	slowCh := make(chan SequencedOutput, 1)
	tracker.subsMu.Lock()
	tracker.subs = append(tracker.subs, slowCh)
	tracker.subsMu.Unlock()
	slowCh <- SequencedOutput{OutputEvent: controlmode.OutputEvent{Data: "blocking"}}

	// Send multiple events
	for i := 0; i < 5; i++ {
		tracker.fanOut(controlmode.OutputEvent{Data: "event"})
	}

	// Both fast consumers should have all 5 events
	for i := 0; i < 5; i++ {
		select {
		case <-fast1:
		default:
			t.Errorf("fast1 missing event %d", i)
		}
		select {
		case <-fast2:
		default:
			t.Errorf("fast2 missing event %d", i)
		}
	}

	// Slow consumer should have caused 5 drops
	if drops := tracker.Counters.FanOutDrops.Load(); drops != 5 {
		t.Errorf("FanOutDrops = %d, want 5", drops)
	}

	tracker.UnsubscribeOutput(fast1)
	tracker.UnsubscribeOutput(fast2)
	<-slowCh
	tracker.subsMu.Lock()
	for i, ch := range tracker.subs {
		if ch == slowCh {
			tracker.subs = append(tracker.subs[:i], tracker.subs[i+1:]...)
			break
		}
	}
	tracker.subsMu.Unlock()
}
