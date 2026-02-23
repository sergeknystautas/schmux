// Package bus provides an in-process event bus for daemon-internal routing.
// Producers call Publish, consumers call Subscribe with event type filters.
// Each event gets a monotonic sequence number. Dispatch is bounded —
// each subscriber gets a buffered channel and dedicated worker goroutine,
// so total goroutines are proportional to subscribers (not events).
package bus

import (
	"fmt"
	"sync"
	"sync/atomic"
)

// subscriberBufferSize is the per-subscriber event channel capacity.
// Events are dropped (with a warning) if a subscriber falls this far behind.
const subscriberBufferSize = 256

// Event is the envelope for all bus messages.
type Event struct {
	Type      string      // e.g. "agent.status", "session.created", "escalation.set"
	SessionID string      // session that produced the event (empty for workspace events)
	Seq       uint64      // monotonic sequence number, assigned by Publish
	Payload   interface{} // type-specific data
}

// AgentStatusPayload carries agent state change data.
type AgentStatusPayload struct {
	State    string
	Message  string
	Intent   string
	Blockers string
}

// AgentLorePayload carries failure/reflection/friction data.
type AgentLorePayload struct {
	LoreType string // "failure", "reflection", "friction"
	Text     string
	Tool     string // failure only
	Error    string // failure only
	Category string // failure only
}

// LifecyclePayload carries session/workspace lifecycle data.
type LifecyclePayload struct {
	Message string
}

// EscalationPayload carries escalation data.
type EscalationPayload struct {
	Message string
}

// NudgenikPayload carries LLM-classified terminal state.
type NudgenikPayload struct {
	State   string
	Summary string
}

// Handler is a callback for event dispatch.
type Handler func(Event)

type subscriber struct {
	handler Handler
	types   map[string]bool
	ch      chan Event
	done    chan struct{}
}

// Bus is an in-process pub/sub event bus.
type Bus struct {
	mu          sync.RWMutex
	subscribers []*subscriber
	seq         atomic.Uint64
}

// New creates a new event bus.
func New() *Bus {
	return &Bus{}
}

// Subscribe registers a handler for the given event types.
// Returns an unsubscribe function. Safe for concurrent use.
// Each subscriber gets a dedicated goroutine and buffered channel.
func (b *Bus) Subscribe(handler Handler, eventTypes ...string) func() {
	types := make(map[string]bool, len(eventTypes))
	for _, t := range eventTypes {
		types[t] = true
	}
	sub := &subscriber{
		handler: handler,
		types:   types,
		ch:      make(chan Event, subscriberBufferSize),
		done:    make(chan struct{}),
	}

	// Start dedicated worker goroutine for this subscriber
	go func() {
		defer close(sub.done)
		for event := range sub.ch {
			sub.handler(event)
		}
	}()

	b.mu.Lock()
	b.subscribers = append(b.subscribers, sub)
	b.mu.Unlock()

	return func() {
		b.mu.Lock()
		for i, s := range b.subscribers {
			if s == sub {
				b.subscribers = append(b.subscribers[:i], b.subscribers[i+1:]...)
				break
			}
		}
		b.mu.Unlock()
		close(sub.ch)
		<-sub.done
	}
}

// Publish assigns a sequence number and dispatches the event to all
// matching subscribers. Non-blocking: if a subscriber's channel is full,
// the event is dropped with a warning log.
func (b *Bus) Publish(event Event) {
	event.Seq = b.seq.Add(1)

	b.mu.RLock()
	subs := make([]*subscriber, len(b.subscribers))
	copy(subs, b.subscribers)
	b.mu.RUnlock()

	for _, sub := range subs {
		if sub.types[event.Type] {
			select {
			case sub.ch <- event:
			default:
				fmt.Printf("[bus] warning: subscriber channel full, dropping event type=%s seq=%d\n", event.Type, event.Seq)
			}
		}
	}
}

// Close unsubscribes all subscribers and waits for their workers to drain.
// After Close, the bus should not be used.
func (b *Bus) Close() {
	b.mu.Lock()
	subs := b.subscribers
	b.subscribers = nil
	b.mu.Unlock()

	for _, sub := range subs {
		close(sub.ch)
	}
	for _, sub := range subs {
		<-sub.done
	}
}
