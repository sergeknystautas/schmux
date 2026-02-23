package bus

import (
	"sort"
	"sync"
	"testing"
	"time"
)

func TestPublishSubscribe(t *testing.T) {
	b := New()
	defer b.Close()
	var got []Event
	var mu sync.Mutex

	b.Subscribe(func(e Event) {
		mu.Lock()
		got = append(got, e)
		mu.Unlock()
	}, "agent.status")

	b.Publish(Event{Type: "agent.status", SessionID: "s1", Payload: map[string]string{"state": "completed"}})

	// Give worker time to process
	time.Sleep(50 * time.Millisecond)

	mu.Lock()
	defer mu.Unlock()
	if len(got) != 1 {
		t.Fatalf("expected 1 event, got %d", len(got))
	}
	if got[0].Type != "agent.status" {
		t.Errorf("type = %q, want %q", got[0].Type, "agent.status")
	}
	if got[0].Seq != 1 {
		t.Errorf("seq = %d, want 1", got[0].Seq)
	}
}

func TestSubscribeFiltersTypes(t *testing.T) {
	b := New()
	defer b.Close()
	var got []Event
	var mu sync.Mutex

	b.Subscribe(func(e Event) {
		mu.Lock()
		got = append(got, e)
		mu.Unlock()
	}, "agent.status")

	b.Publish(Event{Type: "session.created", SessionID: "s1"})
	b.Publish(Event{Type: "agent.status", SessionID: "s1"})

	time.Sleep(50 * time.Millisecond)

	mu.Lock()
	defer mu.Unlock()
	if len(got) != 1 {
		t.Fatalf("expected 1 event (filtered), got %d", len(got))
	}
}

func TestUnsubscribe(t *testing.T) {
	b := New()
	defer b.Close()
	var count int
	var mu sync.Mutex

	unsub := b.Subscribe(func(e Event) {
		mu.Lock()
		count++
		mu.Unlock()
	}, "agent.status")

	b.Publish(Event{Type: "agent.status", SessionID: "s1"})
	time.Sleep(50 * time.Millisecond)

	unsub()

	b.Publish(Event{Type: "agent.status", SessionID: "s1"})
	time.Sleep(50 * time.Millisecond)

	mu.Lock()
	defer mu.Unlock()
	if count != 1 {
		t.Errorf("count = %d, want 1 (handler should not fire after unsub)", count)
	}
}

func TestSequenceMonotonic(t *testing.T) {
	b := New()
	defer b.Close()
	var seqs []uint64
	var mu sync.Mutex

	b.Subscribe(func(e Event) {
		mu.Lock()
		seqs = append(seqs, e.Seq)
		mu.Unlock()
	}, "agent.status", "session.created")

	b.Publish(Event{Type: "agent.status", SessionID: "s1"})
	b.Publish(Event{Type: "session.created", SessionID: "s2"})
	b.Publish(Event{Type: "agent.status", SessionID: "s3"})

	time.Sleep(50 * time.Millisecond)

	mu.Lock()
	defer mu.Unlock()
	if len(seqs) != 3 {
		t.Fatalf("expected 3 events, got %d", len(seqs))
	}
	// With per-subscriber channel, ordering is preserved for a single subscriber
	sort.Slice(seqs, func(i, j int) bool { return seqs[i] < seqs[j] })
	for i := 1; i < len(seqs); i++ {
		if seqs[i] <= seqs[i-1] {
			t.Errorf("seq not monotonic: %v", seqs)
		}
	}
}

func TestMultipleSubscribers(t *testing.T) {
	b := New()
	defer b.Close()
	var count1, count2 int
	var mu sync.Mutex

	b.Subscribe(func(e Event) {
		mu.Lock()
		count1++
		mu.Unlock()
	}, "agent.status")

	b.Subscribe(func(e Event) {
		mu.Lock()
		count2++
		mu.Unlock()
	}, "agent.status")

	b.Publish(Event{Type: "agent.status", SessionID: "s1"})

	time.Sleep(50 * time.Millisecond)

	mu.Lock()
	defer mu.Unlock()
	if count1 != 1 || count2 != 1 {
		t.Errorf("counts = %d, %d; both should be 1", count1, count2)
	}
}

func TestSlowConsumerDoesNotBlock(t *testing.T) {
	b := New()
	defer b.Close()
	done := make(chan struct{})

	// Slow subscriber
	b.Subscribe(func(e Event) {
		time.Sleep(500 * time.Millisecond)
	}, "agent.status")

	// Fast subscriber
	b.Subscribe(func(e Event) {
		close(done)
	}, "agent.status")

	b.Publish(Event{Type: "agent.status", SessionID: "s1"})

	select {
	case <-done:
		// Fast subscriber completed despite slow one
	case <-time.After(200 * time.Millisecond):
		t.Fatal("fast subscriber blocked by slow subscriber")
	}
}

func TestAgentStatusPayload(t *testing.T) {
	b := New()
	defer b.Close()
	var got Event
	var mu sync.Mutex

	b.Subscribe(func(e Event) {
		mu.Lock()
		got = e
		mu.Unlock()
	}, "agent.status")

	b.Publish(Event{
		Type:      "agent.status",
		SessionID: "s1",
		Payload: AgentStatusPayload{
			State:   "completed",
			Message: "Done",
			Intent:  "Build feature",
		},
	})

	time.Sleep(50 * time.Millisecond)

	mu.Lock()
	defer mu.Unlock()
	p, ok := got.Payload.(AgentStatusPayload)
	if !ok {
		t.Fatalf("payload type = %T, want AgentStatusPayload", got.Payload)
	}
	if p.State != "completed" {
		t.Errorf("state = %q, want %q", p.State, "completed")
	}
}

func TestClose(t *testing.T) {
	b := New()
	var count int
	var mu sync.Mutex

	b.Subscribe(func(e Event) {
		mu.Lock()
		count++
		mu.Unlock()
	}, "agent.status")

	b.Publish(Event{Type: "agent.status", SessionID: "s1"})
	time.Sleep(50 * time.Millisecond)

	// Close should drain all workers
	b.Close()

	mu.Lock()
	defer mu.Unlock()
	if count != 1 {
		t.Errorf("count = %d, want 1", count)
	}
}

func TestChannelFullDropsEvent(t *testing.T) {
	b := New()
	defer b.Close()

	// Create a subscriber that blocks indefinitely
	blocked := make(chan struct{})
	b.Subscribe(func(e Event) {
		<-blocked // never returns
	}, "agent.status")

	// Fill the channel buffer + 1 (the worker goroutine picks up the first event,
	// then the channel can hold subscriberBufferSize more)
	for i := 0; i < subscriberBufferSize+2; i++ {
		b.Publish(Event{Type: "agent.status", SessionID: "s1"})
	}

	// If we get here without deadlocking, the drop worked
	close(blocked)
}

func TestPerSubscriberOrdering(t *testing.T) {
	b := New()
	defer b.Close()
	var seqs []uint64
	var mu sync.Mutex
	done := make(chan struct{})

	const n = 100
	b.Subscribe(func(e Event) {
		mu.Lock()
		seqs = append(seqs, e.Seq)
		if len(seqs) == n {
			close(done)
		}
		mu.Unlock()
	}, "agent.status")

	for i := 0; i < n; i++ {
		b.Publish(Event{Type: "agent.status", SessionID: "s1"})
	}

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for events")
	}

	mu.Lock()
	defer mu.Unlock()
	// Per-subscriber channel preserves FIFO ordering
	for i := 1; i < len(seqs); i++ {
		if seqs[i] <= seqs[i-1] {
			t.Errorf("events not in order: seqs[%d]=%d, seqs[%d]=%d", i-1, seqs[i-1], i, seqs[i])
			break
		}
	}
}
