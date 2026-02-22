# Event Bus Consolidation Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use 10x-engineer:executing-plans to implement this plan task-by-task.

**Goal:** Replace ad-hoc signal callbacks with an in-process event bus, eliminate the dual-write signal/event file system, and fix the double audio ping bug.

**Architecture:** A new `internal/bus/` package provides typed pub/sub within the daemon process. Event files remain the agent→daemon interface. The signal file system (`$SCHMUX_STATUS_FILE`, `FileWatcher`, `.schmux/signal/`) is deleted. All daemon-internal routing for agent status, lifecycle, escalation, and nudgenik flows through the bus. The frontend merges its two notification `useEffect` hooks into one.

**Tech Stack:** Go (bus, backend changes), TypeScript/React (frontend notification consolidation)

**Design doc:** `docs/specs/event-bus-consolidation-design.md`

---

### Task 1: Create `internal/bus/` package

**Files:**

- Create: `internal/bus/bus.go`
- Test: `internal/bus/bus_test.go`

**Step 1: Write the failing test**

Create `internal/bus/bus_test.go`:

```go
package bus

import (
	"sync"
	"testing"
	"time"
)

func TestPublishSubscribe(t *testing.T) {
	b := New()
	var got []Event
	var mu sync.Mutex

	b.Subscribe(func(e Event) {
		mu.Lock()
		got = append(got, e)
		mu.Unlock()
	}, "agent.status")

	b.Publish(Event{Type: "agent.status", SessionID: "s1", Payload: map[string]string{"state": "completed"}})

	// Give goroutine time to dispatch
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
	for i := 1; i < len(seqs); i++ {
		if seqs[i] <= seqs[i-1] {
			t.Errorf("seq not monotonic: %v", seqs)
		}
	}
}

func TestMultipleSubscribers(t *testing.T) {
	b := New()
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
```

**Step 2: Run test to verify it fails**

```bash
go test ./internal/bus/
```

Expected: compilation failure — package doesn't exist yet.

**Step 3: Write minimal implementation**

Create `internal/bus/bus.go`:

```go
// Package bus provides an in-process event bus for daemon-internal routing.
// Producers call Publish, consumers call Subscribe with event type filters.
// Each event gets a monotonic sequence number. Dispatch is concurrent —
// each handler runs in its own goroutine so slow consumers don't block others.
package bus

import (
	"sync"
	"sync/atomic"
)

// Event is the envelope for all bus messages.
type Event struct {
	Type      string      // e.g. "agent.status", "session.created", "escalation.set"
	SessionID string      // session that produced the event (empty for workspace events)
	Seq       uint64      // monotonic sequence number, assigned by Publish
	Payload   interface{} // type-specific data
}

// Handler is a callback for event dispatch.
type Handler func(Event)

type subscriber struct {
	handler Handler
	types   map[string]bool
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
func (b *Bus) Subscribe(handler Handler, eventTypes ...string) func() {
	types := make(map[string]bool, len(eventTypes))
	for _, t := range eventTypes {
		types[t] = true
	}
	sub := &subscriber{handler: handler, types: types}

	b.mu.Lock()
	b.subscribers = append(b.subscribers, sub)
	b.mu.Unlock()

	return func() {
		b.mu.Lock()
		defer b.mu.Unlock()
		for i, s := range b.subscribers {
			if s == sub {
				b.subscribers = append(b.subscribers[:i], b.subscribers[i+1:]...)
				return
			}
		}
	}
}

// Publish assigns a sequence number and dispatches the event to all
// matching subscribers. Each handler runs in its own goroutine.
func (b *Bus) Publish(event Event) {
	event.Seq = b.seq.Add(1)

	b.mu.RLock()
	subs := make([]*subscriber, len(b.subscribers))
	copy(subs, b.subscribers)
	b.mu.RUnlock()

	for _, sub := range subs {
		if sub.types[event.Type] {
			go sub.handler(event)
		}
	}
}
```

**Step 4: Run test to verify it passes**

```bash
go test ./internal/bus/
```

Expected: PASS

**Step 5: Commit**

```bash
git add internal/bus/ && git commit -m "feat(bus): add in-process event bus package"
```

---

### Task 2: Define bus event payload types

**Files:**

- Modify: `internal/bus/bus.go`
- Test: `internal/bus/bus_test.go`

**Step 1: Write the failing test**

Add to `internal/bus/bus_test.go`:

```go
func TestAgentStatusPayload(t *testing.T) {
	b := New()
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
```

**Step 2: Run test to verify it fails**

```bash
go test ./internal/bus/
```

Expected: compilation failure — `AgentStatusPayload` not defined.

**Step 3: Write minimal implementation**

Add to `internal/bus/bus.go` after the `Event` struct:

```go
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
```

**Step 4: Run test to verify it passes**

```bash
go test ./internal/bus/
```

Expected: PASS

**Step 5: Commit**

```bash
git add internal/bus/ && git commit -m "feat(bus): add typed event payload structs"
```

---

### Task 3: Wire bus into daemon and create dashboard broadcaster consumer

This is the first consumer. It replaces `HandleAgentSignal` for the `agent.status` event type.

**Files:**

- Modify: `internal/daemon/daemon.go` (create bus, pass to server)
- Modify: `internal/dashboard/server.go` (accept bus, subscribe)
- Modify: `internal/dashboard/websocket.go` (move HandleAgentSignal logic into bus subscriber)
- Test: `internal/dashboard/websocket_test.go` (update tests)

**Step 1: Write the failing test**

Update `internal/dashboard/websocket_test.go` — change `TestHandleAgentSignalIntegration` to publish via bus instead of calling `HandleAgentSignal` directly. The test verifies that nudge state, nudge seq, and broadcast still work correctly when events come through the bus.

The existing test calls `srv.HandleAgentSignal("s1", sig)`. After the change, it should call `srv.bus.Publish(bus.Event{...})` and verify the same outcomes.

This test will fail until the bus subscriber is wired.

**Step 2: Implement the bus subscriber in `server.go`**

Add a `SetBus(*bus.Bus)` method to `Server`. In it, subscribe to `agent.status` events with a handler that does exactly what `HandleAgentSignal` currently does — but with one change: `NudgeSeq` only increments for attention states (`completed`, `needs_input`, `needs_testing`, `error`), not for `working`.

**Step 3: Wire in daemon.go**

Create the bus in `daemon.go` after config loading. Pass it to the server via `server.SetBus(eventBus)`. Keep the old `SetSignalCallback` wiring in place temporarily — both paths active. The bus subscriber handles nudge/broadcast; the old callback still handles floor manager injection (migrated in a later task).

**Step 4: Run tests**

```bash
go test ./internal/dashboard/ ./internal/bus/
```

Expected: PASS. The `TestHandleAgentSignalRapidSignals` test needs updating — `working` no longer increments seq, so expected seq changes from 6 to 4.

**Step 5: Commit**

```bash
git add internal/daemon/daemon.go internal/dashboard/server.go internal/dashboard/websocket.go internal/dashboard/websocket_test.go && git commit -m "feat(bus): wire dashboard broadcaster as bus consumer"
```

---

### Task 4: Wire floor manager injector as bus consumer

**Files:**

- Modify: `internal/daemon/daemon.go` (subscribe injector to bus instead of signal callback)
- Modify: `internal/floormanager/injector.go` (accept bus events)

**Step 1: Add a bus subscription in daemon.go**

Subscribe to `agent.status`, `session.created`, `session.disposed` event types. The handler replaces the inline logic currently in the signal callback (lines 502-538) and the lifecycle callback (lines 543-554):

- Skip events from the floor manager's own session
- Handle `rotate` state by triggering rotation
- Clear escalation when floor manager sends a new signal
- Call `injector.Inject()` for agent status events
- Call `injector.InjectLifecycle()` for lifecycle events

**Step 2: Remove `SetSignalCallback` and `SetLifecycleCallback` from daemon.go**

Delete the signal callback closure (lines 499-539), the lifecycle callback closure (lines 543-554), and the `sm.SetSignalCallback()` / `sm.SetLifecycleCallback()` / `wm.SetLifecycleCallback()` calls.

**Step 3: Run tests**

```bash
go test ./internal/daemon/ ./internal/floormanager/ ./internal/bus/
```

Expected: PASS

**Step 4: Commit**

```bash
git add internal/daemon/daemon.go internal/floormanager/injector.go && git commit -m "feat(bus): wire floor manager injector as bus consumer"
```

---

### Task 5: Wire escalation as bus consumer

**Files:**

- Modify: `internal/dashboard/handlers_floor_manager.go` (publish to bus instead of direct state mutation)
- Modify: `internal/daemon/daemon.go` (subscribe escalation consumer)
- Test: `internal/dashboard/handlers_test.go` (update escalation tests if any)

**Step 1: Update `handleEscalate` to publish to bus**

Replace the direct `state.UpdateSessionEscalation` + `state.Save` + `BroadcastSessions` calls with `bus.Publish(Event{Type: "escalation.set", ...})` for POST and `bus.Publish(Event{Type: "escalation.cleared", ...})` for DELETE.

**Step 2: Add escalation consumer in the dashboard broadcaster**

Extend the bus subscriber from Task 3 to also handle `escalation.set` and `escalation.cleared` events. On `escalation.set`, update state + increment NudgeSeq on the floor manager session (so the frontend's seq-based dedup handles it). On `escalation.cleared`, clear the field + broadcast.

**Step 3: Remove auto-clear from signal callback**

The inline escalation clear that was in the old signal callback (now in the floor manager bus subscriber from Task 4) should clear escalation when the floor manager sends a new `agent.status` — publish `escalation.cleared` to the bus instead of direct state mutation.

**Step 4: Run tests**

```bash
go test ./internal/dashboard/ ./internal/daemon/
```

Expected: PASS

**Step 5: Commit**

```bash
git add internal/dashboard/handlers_floor_manager.go internal/daemon/daemon.go internal/dashboard/server.go && git commit -m "feat(bus): wire escalation through event bus"
```

---

### Task 6: Wire nudgenik as bus producer

**Files:**

- Modify: `internal/daemon/daemon.go` (nudgenik publishes to bus)

**Step 1: Update nudgenik callback**

In `checkInactiveSessionsForNudge` (daemon.go lines 1152-1205), replace the direct `sess.Nudge = nudge` + `st.UpdateSession` + `st.Save` + `server.BroadcastSessions()` with `bus.Publish(Event{Type: "nudgenik.result", ...})`.

**Step 2: Add nudgenik consumer in dashboard broadcaster**

Extend the bus subscriber to handle `nudgenik.result` events. The handler updates the nudge field on the session in state, saves, and broadcasts. NudgeNik results do NOT increment NudgeSeq (preserving current behavior — nudgenik updates are silent).

**Step 3: Run tests**

```bash
go test ./internal/daemon/
```

Expected: PASS

**Step 4: Commit**

```bash
git add internal/daemon/daemon.go internal/dashboard/server.go && git commit -m "feat(bus): wire nudgenik as bus producer"
```

---

### Task 7: Wire lifecycle events as bus producers

**Files:**

- Modify: `internal/session/manager.go` (publish lifecycle events to bus)
- Modify: `internal/workspace/manager.go` (publish lifecycle events to bus)

**Step 1: Add bus field to session.Manager and workspace.Manager**

Add `bus *bus.Bus` field and `SetBus(*bus.Bus)` method to both managers. Wire in `daemon.go`.

**Step 2: Replace lifecycle callback calls with bus.Publish**

In `session/manager.go`, replace all 6 `lifecycleCallback(fmt.Sprintf(...))` calls (in `Spawn`, `SpawnCommand`, `SpawnRemote`, `Dispose`, `disposeRemoteSession`) with `bus.Publish(Event{Type: "session.created", ...})` or `bus.Publish(Event{Type: "session.disposed", ...})`.

In `workspace/manager.go`, replace all 4 `lifecycleCallback(fmt.Sprintf(...))` calls with `bus.Publish(Event{Type: "workspace.created", ...})` or `bus.Publish(Event{Type: "workspace.deleted", ...})`.

**Step 3: Remove SetLifecycleCallback**

Delete `SetLifecycleCallback` methods from both `session.Manager` and `workspace.Manager`. Delete the `lifecycleCallback` fields.

**Step 4: Run tests**

```bash
go test ./internal/session/ ./internal/workspace/ ./internal/daemon/
```

Expected: PASS

**Step 5: Commit**

```bash
git add internal/session/manager.go internal/workspace/manager.go internal/daemon/daemon.go && git commit -m "feat(bus): wire lifecycle events as bus producers"
```

---

### Task 8: Wire EventWatcher to publish to bus

**Files:**

- Modify: `internal/session/manager.go` (EventWatcher publishes to bus instead of signalCallback)

**Step 1: Change EventWatcher subscription**

In `ensureTrackerFromSession` (session/manager.go lines 1441-1461), change the EventWatcher `"status"` subscription handler from:

```go
ew.Subscribe("status", func(_ string, evt event.Event) {
    if sig := event.ToSignal(evt); sig != nil {
        signalCb(sessionID, *sig)
    }
})
```

to:

```go
ew.Subscribe("status", func(_ string, evt event.Event) {
    if sig := event.ToSignal(evt); sig != nil {
        m.bus.Publish(bus.Event{
            Type:      "agent.status",
            SessionID: sessionID,
            Payload:   bus.AgentStatusPayload{
                State:    sig.State,
                Message:  sig.Message,
                Intent:   sig.Intent,
                Blockers: sig.Blockers,
            },
        })
    }
})
```

Also add a subscription for lore-relevant events (`failure`, `reflection`, `friction`) that publishes `agent.lore` events to the bus.

**Step 2: Remove `SetSignalCallback` from session.Manager**

Delete the `signalCallback` field and `SetSignalCallback` method. The bus replaces this callback entirely.

**Step 3: Remove `signalFilePath` and `FileWatcher` from tracker creation**

In `ensureTrackerFromSession`, stop constructing `signalFilePath`. Remove the `signalFilePath` parameter from `NewSessionTracker`. In `tracker.go`, remove the `fileWatcher` field and all FileWatcher creation/stop logic.

**Step 4: Remove the startup signal recovery fallback to FileWatcher**

In `ensureTrackerFromSession` (lines 1488-1500), delete the "Fall back to signal file watcher if no event recovery" block. Only the EventWatcher `ReadCurrent()` path remains.

**Step 5: Run tests**

```bash
go test ./internal/session/ ./internal/dashboard/ ./internal/bus/
```

Expected: PASS. Update `tracker_test.go` — `NewSessionTracker` signature changed (remove `signalFilePath` and `ew` params, or adjust as needed).

**Step 6: Commit**

```bash
git add internal/session/ internal/bus/ && git commit -m "feat(bus): EventWatcher publishes to bus, remove FileWatcher"
```

---

### Task 9: Delete signal file system

**Files:**

- Delete: `internal/signal/filewatcher.go`
- Delete: `internal/signal/filewatcher_test.go`
- Modify: `internal/signal/signal.go` (remove `ParseSignalFile`, keep `Signal`, `IsValidState`, `MapStateToNudge`, `ShortID`, `StripANSIBytes`)
- Modify: `internal/signal/signal_test.go` (remove `TestParseSignalFile*` tests)
- Modify: `internal/workspace/ensure/manager.go` (event-file-only write commands)
- Modify: `internal/workspace/ensure/manager_test.go` (update hook tests)
- Modify: `internal/session/manager.go` (remove `$SCHMUX_STATUS_FILE`, `.schmux/signal/` creation)

**Step 1: Delete FileWatcher files**

```bash
rm internal/signal/filewatcher.go internal/signal/filewatcher_test.go
```

**Step 2: Remove `ParseSignalFile` from `signal.go`**

Delete `ParseSignalFile()` function (lines 194-233). Keep everything else in the file (`Signal` struct, `IsValidState`, `MapStateToNudge`, `ShortID`, `StripANSIBytes`, `ValidStates`).

**Step 3: Remove `ParseSignalFile` tests from `signal_test.go`**

Delete `TestParseSignalFile`, `TestParseSignalFileWithIntentAndBlockers`, `TestParseRotateSignal`, `TestParseSignalFileWithoutEnrichedFields` (lines 231-317).

**Step 4: Replace dual-write commands with event-file-only commands**

In `ensure/manager.go`, replace `dualWriteCommand()`, `dualWriteCommandWithIntent()`, `dualWriteCommandWithBlockers()` with event-file-only equivalents:

```go
func eventWriteCommand(state string) string {
	return fmt.Sprintf(
		`[ -n "$SCHMUX_EVENTS_FILE" ] && printf "{\"ts\":\"%%s\",\"type\":\"status\",\"state\":\"%s\",\"message\":\"\"}\n" "$(date -u +%%Y-%%m-%%dT%%H:%%M:%%SZ)" >> "$SCHMUX_EVENTS_FILE" || true`,
		state,
	)
}
```

Similarly for intent and blockers variants. Update `buildClaudeHooksMap` to use these.

**Step 5: Remove `$SCHMUX_STATUS_FILE` from session spawn**

In `session/manager.go`:

- `Spawn()`: Remove `"SCHMUX_STATUS_FILE"` from the env map (line 700). Keep `SCHMUX_EVENTS_FILE`.
- `SpawnCommand()`: Remove `"SCHMUX_STATUS_FILE"` from `schmuxEnv` (line 822).
- `SpawnRemote()`: Remove `"SCHMUX_STATUS_FILE"` from the env (line 417).

**Step 6: Remove `.schmux/signal/` directory creation**

In `session/manager.go`:

- `Spawn()`: Delete `os.MkdirAll` for `.schmux/signal` (lines 666-669). Keep `.schmux/events`.
- `SpawnCommand()`: Delete `os.MkdirAll` for `.schmux/signal` (lines 812-815).
- `SpawnRemote()`: Delete the `mkdir -p .schmux/signal` SSH commands (lines 506-513 and 538-543). Add `mkdir -p .schmux/events` instead.

**Step 7: Run tests**

```bash
go test ./internal/signal/ ./internal/session/ ./internal/workspace/ensure/ ./internal/dashboard/
```

Expected: PASS

**Step 8: Commit**

```bash
git add -A && git commit -m "refactor: delete signal file system, event-file-only writes"
```

---

### Task 10: Update remote signal watcher to use event file

**Files:**

- Modify: `internal/signal/remotewatcher.go`
- Modify: `internal/signal/remotewatcher_test.go`
- Modify: `internal/session/manager.go` (update `StartRemoteSignalMonitor`)

**Step 1: Update `WatcherScript`**

Change `WatcherScript()` to watch the event file instead of the signal file. The script uses `tail -n0 -f` with sentinel wrapping per JSON line:

```go
func WatcherScript(eventsFilePath string) string {
	return fmt.Sprintf(`EVENTS_FILE=%s; touch "$EVENTS_FILE"; if command -v inotifywait >/dev/null 2>&1; then tail -n0 -f "$EVENTS_FILE" | while IFS= read -r line; do echo "__SCHMUX_SIGNAL__${line}__END__"; done & TAIL_PID=$!; while inotifywait -qq -e modify "$EVENTS_FILE" 2>/dev/null; do :; done; kill $TAIL_PID 2>/dev/null; else tail -n0 -f "$EVENTS_FILE" | while IFS= read -r line; do echo "__SCHMUX_SIGNAL__${line}__END__"; done; fi`,
		shellutil.Quote(eventsFilePath))
}
```

**Step 2: Update `ProcessOutput` to parse JSON**

Change `ProcessOutput` to parse the sentinel-wrapped content as a JSON event line via `event.ParseEvent()`, then convert to `signal.Signal` via `event.ToSignal()`:

```go
func (w *RemoteSignalWatcher) ProcessOutput(data string) {
	content := ParseSentinelOutput(data)
	if content == "" {
		return
	}
	content = strings.TrimSpace(content)
	if content == "" {
		return
	}

	evt, err := event.ParseEvent(content)
	if err != nil {
		return
	}
	sig := event.ToSignal(evt)
	if sig == nil {
		return
	}

	w.mu.Lock()
	changed := sig.State != w.lastState || sig.Message != w.lastMessage
	if changed {
		w.lastState = sig.State
		w.lastMessage = sig.Message
	}
	w.mu.Unlock()

	if changed {
		w.callback(*sig)
	}
}
```

Update the struct to track `lastState`/`lastMessage` instead of `lastContent`.

**Step 3: Update `StartRemoteSignalMonitor`**

In `session/manager.go`:

- Change `statusFilePath` construction (line 192) from `.schmux/signal/` to `.schmux/events/{sessionID}.jsonl`
- Update the variable name to `eventsFilePath`
- Update the initial state recovery (lines 282-296) from `cat .schmux/signal/` + `ParseSignalFile` to reading the last line of the JSONL file and parsing it as an event

**Step 4: Update tests**

In `remotewatcher_test.go`:

- Update `TestRemoteSignalWatcherProcessOutput` to send JSON event lines instead of `"completed Done"` format
- Update `TestWatcherScript` assertions — it should reference `EVENTS_FILE` instead of `STATUS_FILE`
- Update `TestWatcherScriptInjection` similarly
- Update concurrent test data format

**Step 5: Run tests**

```bash
go test ./internal/signal/ ./internal/session/
```

Expected: PASS

**Step 6: Commit**

```bash
git add internal/signal/remotewatcher.go internal/signal/remotewatcher_test.go internal/session/manager.go && git commit -m "feat(bus): update remote signal watcher to use event file"
```

---

### Task 11: Consolidate frontend notification effects

**Files:**

- Modify: `assets/dashboard/src/contexts/SessionsContext.tsx`

**Step 1: Merge the two `useEffect` hooks**

Replace the nudge seq tracking effect (lines 137-177) and the escalation tracking effect (lines 180-193) with a single effect:

```tsx
// Unified notification effect: plays sound for attention-worthy nudge changes
// and escalation alerts. At most one sound per update cycle.
const lastProcessedNudgeRef = useRef<Record<string, number>>({});
const lastEscalationRef = useRef<string | undefined>(undefined);

useEffect(() => {
  if (config?.notifications?.sound_disabled) return;

  let shouldPlaySound = false;
  let escalationMessage: string | undefined;

  // Check nudge seq changes (agent status transitions)
  Object.entries(sessionsById).forEach(([sessionId, session]) => {
    const nudgeSeq = session.nudge_seq ?? 0;
    if (nudgeSeq === 0) return;
    if (lastProcessedNudgeRef.current[sessionId] === nudgeSeq) return;
    lastProcessedNudgeRef.current[sessionId] = nudgeSeq;

    const storageKey = `schmux:ack:${sessionId}`;
    const lastAckedSeq = parseInt(localStorage.getItem(storageKey) || '0', 10);

    if (nudgeSeq > lastAckedSeq && isAttentionState(session.nudge_state)) {
      shouldPlaySound = true;
      localStorage.setItem(storageKey, String(nudgeSeq));
    }
  });

  // Check escalation changes (floor manager alerts)
  const fmSession = Object.values(sessionsById).find((s) => s.is_floor_manager);
  const escalation = fmSession?.escalation;
  if (escalation && escalation !== lastEscalationRef.current) {
    shouldPlaySound = true;
    escalationMessage = escalation;
  }
  lastEscalationRef.current = escalation;

  // Single sound + optional browser notification
  if (shouldPlaySound) {
    playAttentionSound();
  }
  if (escalationMessage) {
    requestNotificationPermission();
    showBrowserNotification('schmux — Escalation', escalationMessage);
  }

  // Cleanup stale localStorage entries (throttled)
  // ... (keep existing cleanup logic)
}, [sessionsById, config?.notifications?.sound_disabled]);
```

Key behavioral change: if both a nudge seq change and an escalation arrive in the same WebSocket broadcast, only one `playAttentionSound()` call fires.

**Step 2: Run tests**

```bash
cd assets/dashboard && npx vitest run
```

Expected: PASS

**Step 3: Commit**

```bash
git add assets/dashboard/src/contexts/SessionsContext.tsx && git commit -m "fix(notifications): merge notification effects into single dispatcher"
```

---

### Task 12: Remove dead callback code

**Files:**

- Modify: `internal/session/manager.go` (remove `SetSignalCallback`, `signalCallback` field)
- Modify: `internal/dashboard/websocket.go` (remove `HandleAgentSignal` if fully replaced by bus)
- Modify: `internal/dashboard/server.go` (remove `SetFloorManagerToggle` if handled via bus)
- Modify: `internal/daemon/daemon.go` (clean up old wiring code)

**Step 1: Audit remaining callback usage**

Search for any remaining references to the removed callback patterns. Remove dead fields, methods, and wiring code. Keep callbacks that are NOT being migrated to the bus: `SetOutputCallback`, `SetCompoundCallback`, `SetLoreCallback`, `SetTerminalCaptureCallback`.

**Step 2: Run full test suite**

```bash
./test.sh
```

Expected: PASS

**Step 3: Commit**

```bash
git add -A && git commit -m "refactor: remove dead signal callback wiring"
```

---

### Task 13: Update API docs

**Files:**

- Modify: `docs/api.md`

**Step 1: Update docs**

Remove references to `$SCHMUX_STATUS_FILE` and `.schmux/signal/` directory. Update the signaling section to document the event file as the sole mechanism. Note that `nudge_seq` now only increments for attention states.

CI runs `scripts/check-api-docs.sh` to enforce doc updates for API-related package changes. This step must not be skipped.

**Step 2: Commit**

```bash
git add docs/api.md && git commit -m "docs: update API docs for event bus consolidation"
```

---

### Task 14: Run full test suite and format

**Step 1: Format all code**

```bash
./format.sh
```

**Step 2: Run all tests**

```bash
./test.sh --all
```

This runs unit tests, E2E tests, and scenario tests. All must pass.

**Step 3: Fix any failures**

Address test failures found by the full suite. Common issues:

- E2E Dockerfile may reference signal file paths that no longer exist
- Scenario tests may expect `$SCHMUX_STATUS_FILE` behavior

**Step 4: Final commit**

```bash
git add -A && git commit -m "chore: format and fix remaining test issues"
```
