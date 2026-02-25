# Event Monitor Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use 10x-engineer:executing-plans to implement this plan task-by-task.

**Goal:** Add a dev-mode-only event monitor that streams unified events to the dashboard in real-time — a compact sidebar panel plus a full page view at `/events`.

**Architecture:** A new `EventMonitorHandler` (Go) forwards all event types over the existing `/ws/dashboard` WebSocket as `"event"` messages. The frontend stores events in a ring buffer and renders them in a collapsible sidebar panel and a filterable full-page table. A history REST endpoint bootstraps pre-connection events.

**Tech Stack:** Go (chi router, gorilla/websocket), React (TypeScript, React Router), CSS (global.css patterns)

**Spec:** `docs/specs/event-monitor.md`

---

### Task 1: EventMonitorHandler (Go backend)

Create the handler that forwards all event types to the dashboard server.

**Files:**

- Create: `internal/events/monitorhandler.go`
- Test: `internal/events/monitorhandler_test.go`

**Step 1: Write the test**

Create `internal/events/monitorhandler_test.go`:

```go
package events

import (
	"context"
	"encoding/json"
	"testing"
)

func TestMonitorHandler_ForwardsAllEventTypes(t *testing.T) {
	var received []struct {
		sessionID string
		rawType   string
		data      []byte
	}

	h := NewMonitorHandler(func(sessionID string, raw RawEvent, data []byte) {
		received = append(received, struct {
			sessionID string
			rawType   string
			data      []byte
		}{sessionID, raw.Type, data})
	})

	events := []struct {
		eventType string
		payload   any
	}{
		{"status", StatusEvent{Ts: "2024-01-01T00:00:00Z", Type: "status", State: "working"}},
		{"failure", FailureEvent{Ts: "2024-01-01T00:00:01Z", Type: "failure", Tool: "bash", Error: "not found"}},
		{"reflection", ReflectionEvent{Ts: "2024-01-01T00:00:02Z", Type: "reflection", Text: "use X instead"}},
		{"friction", FrictionEvent{Ts: "2024-01-01T00:00:03Z", Type: "friction", Text: "slow build"}},
	}

	for _, e := range events {
		data, _ := json.Marshal(e.payload)
		raw := RawEvent{Ts: "2024-01-01T00:00:00Z", Type: e.eventType}
		h.HandleEvent(context.Background(), "s1", raw, data)
	}

	if len(received) != 4 {
		t.Fatalf("expected 4 events forwarded, got %d", len(received))
	}

	for i, e := range events {
		if received[i].sessionID != "s1" {
			t.Errorf("event %d: sessionID = %q, want %q", i, received[i].sessionID, "s1")
		}
		if received[i].rawType != e.eventType {
			t.Errorf("event %d: type = %q, want %q", i, received[i].rawType, e.eventType)
		}
	}
}
```

**Step 2: Run test to verify it fails**

```bash
go test ./internal/events/ -run TestMonitorHandler -v
```

Expected: FAIL — `NewMonitorHandler` undefined.

**Step 3: Write the implementation**

Create `internal/events/monitorhandler.go`:

```go
package events

import "context"

// MonitorCallback is called for every event, regardless of type.
type MonitorCallback func(sessionID string, raw RawEvent, data []byte)

// MonitorHandler forwards all events to a callback for dev-mode monitoring.
type MonitorHandler struct {
	callback MonitorCallback
}

// NewMonitorHandler creates a handler that forwards all events.
func NewMonitorHandler(callback MonitorCallback) *MonitorHandler {
	return &MonitorHandler{callback: callback}
}

func (h *MonitorHandler) HandleEvent(ctx context.Context, sessionID string, raw RawEvent, data []byte) {
	h.callback(sessionID, raw, data)
}
```

**Step 4: Run test to verify it passes**

```bash
go test ./internal/events/ -run TestMonitorHandler -v
```

Expected: PASS

---

### Task 2: BroadcastEvent method on dashboard server

Add a method to broadcast event messages over the dashboard WebSocket.

**Files:**

- Modify: `internal/dashboard/server.go` (add `BroadcastEvent` method)

**Step 1: Write the `BroadcastEvent` method**

Add to `internal/dashboard/server.go`, near the other `Broadcast*` methods (after `BroadcastCuratorEvent` around line 1307):

```go
// BroadcastEvent sends a raw event to all connected dashboard WebSocket clients.
// Used by the event monitor (dev mode only).
func (s *Server) BroadcastEvent(sessionID string, data json.RawMessage) {
	msg := struct {
		Type      string          `json:"type"`
		SessionID string          `json:"session_id"`
		Event     json.RawMessage `json:"event"`
	}{
		Type:      "event",
		SessionID: sessionID,
		Event:     data,
	}
	payload, err := json.Marshal(msg)
	if err != nil {
		return
	}
	s.broadcastToAllDashboardConns(payload)
}
```

**Step 2: Verify it compiles**

```bash
go build ./internal/dashboard/
```

Expected: clean build.

---

### Task 3: Wire EventMonitorHandler in daemon (dev mode only)

Register the monitor handler for all event types when running in dev mode.

**Files:**

- Modify: `internal/daemon/daemon.go` (around line 539, the event handler wiring)

**Step 1: Update the event handler map**

Find the existing wiring block (around line 535-541):

```go
// Wire event system: event watcher → session manager → dashboard server
dashHandler := events.NewDashboardHandler(func(sessionID, state, message, intent, blockers string) {
    server.HandleStatusEvent(sessionID, state, message, intent, blockers)
})
sm.SetEventHandlers(map[string][]events.EventHandler{
    "status": {dashHandler},
})
```

Replace with:

```go
// Wire event system: event watcher → session manager → dashboard server
dashHandler := events.NewDashboardHandler(func(sessionID, state, message, intent, blockers string) {
    server.HandleStatusEvent(sessionID, state, message, intent, blockers)
})
eventHandlers := map[string][]events.EventHandler{
    "status": {dashHandler},
}

// Dev mode: add monitor handler that forwards all events to WebSocket
if devMode {
    monitorHandler := events.NewMonitorHandler(func(sessionID string, raw events.RawEvent, data []byte) {
        server.BroadcastEvent(sessionID, data)
    })
    for _, eventType := range []string{"status", "failure", "reflection", "friction"} {
        eventHandlers[eventType] = append(eventHandlers[eventType], monitorHandler)
    }
}
sm.SetEventHandlers(eventHandlers)
```

**Step 2: Verify it compiles**

```bash
go build ./internal/daemon/
```

Expected: clean build.

**Step 3: Run existing tests**

```bash
go test ./internal/daemon/ -v -count=1
```

Expected: all existing tests pass.

---

### Task 4: History endpoint (GET /api/dev/events/history)

Add a REST endpoint to read recent events from disk, for bootstrapping the frontend on page load.

**Files:**

- Create: `internal/dashboard/handlers_events.go`

**Step 1: Write the handler**

Create `internal/dashboard/handlers_events.go`:

```go
package dashboard

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

type monitorEvent struct {
	SessionID string          `json:"session_id"`
	Event     json.RawMessage `json:"event"`
	Ts        string          `json:"ts"`
}

func (s *Server) handleEventsHistory(w http.ResponseWriter, r *http.Request) {
	const maxEvents = 200

	var allEvents []monitorEvent

	workspaces := s.state.GetWorkspaces()
	for _, ws := range workspaces {
		eventsDir := filepath.Join(ws.Path, ".schmux", "events")
		entries, err := os.ReadDir(eventsDir)
		if err != nil {
			continue
		}
		for _, entry := range entries {
			if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".jsonl") {
				continue
			}
			sessionID := strings.TrimSuffix(entry.Name(), ".jsonl")
			filePath := filepath.Join(eventsDir, entry.Name())
			data, err := os.ReadFile(filePath)
			if err != nil {
				continue
			}
			for _, line := range strings.Split(string(data), "\n") {
				line = strings.TrimSpace(line)
				if line == "" {
					continue
				}
				// Extract timestamp for sorting
				var envelope struct {
					Ts string `json:"ts"`
				}
				if err := json.Unmarshal([]byte(line), &envelope); err != nil {
					continue
				}
				allEvents = append(allEvents, monitorEvent{
					SessionID: sessionID,
					Event:     json.RawMessage(line),
					Ts:        envelope.Ts,
				})
			}
		}
	}

	// Sort by timestamp descending, take most recent maxEvents
	sort.Slice(allEvents, func(i, j int) bool {
		return allEvents[i].Ts > allEvents[j].Ts
	})
	if len(allEvents) > maxEvents {
		allEvents = allEvents[:maxEvents]
	}

	// Reverse so oldest is first (chronological order)
	for i, j := 0, len(allEvents)-1; i < j; i, j = i+1, j-1 {
		allEvents[i], allEvents[j] = allEvents[j], allEvents[i]
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(allEvents)
}
```

**Step 2: Register the endpoint**

In `internal/dashboard/server.go`, inside the `if s.devMode {` block (around line 611), add:

```go
r.Get("/dev/events/history", s.handleEventsHistory)
```

Place it right after the existing `r.Get("/dev/status", s.handleDevStatus)` line.

**Step 3: Verify it compiles**

```bash
go build ./internal/dashboard/
```

Expected: clean build.

---

### Task 5: Frontend WebSocket event type + hook plumbing

Add the `"event"` message type to the WebSocket hook and expose it through SessionsContext.

**Files:**

- Modify: `assets/dashboard/src/lib/types.ts` (add `MonitorEvent` type)
- Modify: `assets/dashboard/src/hooks/useSessionsWebSocket.ts` (add type guard, state, dispatch)
- Modify: `assets/dashboard/src/contexts/SessionsContext.tsx` (expose `monitorEvents` + `clearMonitorEvents`)

**Step 1: Add the TypeScript type**

In `assets/dashboard/src/lib/types.ts`, add at the end (before any closing braces — look for a natural grouping spot):

```typescript
/** A raw event from the unified events system, streamed via WebSocket in dev mode. */
export type MonitorEvent = {
  session_id: string;
  event: {
    ts: string;
    type: string;
    // Status fields
    state?: string;
    message?: string;
    intent?: string;
    blockers?: string;
    // Failure fields
    tool?: string;
    input?: string;
    error?: string;
    category?: string;
    // Reflection/friction fields
    text?: string;
  };
};
```

**Step 2: Add the type guard and state to the WebSocket hook**

In `assets/dashboard/src/hooks/useSessionsWebSocket.ts`:

a. Add `MonitorEvent` to the import from `../lib/types`:

```typescript
import type {
  // ... existing imports ...
  MonitorEvent,
} from '../lib/types';
```

b. Add the type guard function (after `isCuratorStateMessage`):

```typescript
function isMonitorEventMessage(
  data: Record<string, unknown>
): data is { type: 'event'; session_id: string; event: Record<string, unknown> } {
  return data.type === 'event' && isString(data.session_id) && isObject(data.event);
}
```

c. Add to `SessionsWebSocketState` type:

```typescript
monitorEvents: MonitorEvent[];
clearMonitorEvents: () => void;
```

d. Add state inside the hook function:

```typescript
const [monitorEvents, setMonitorEvents] = useState<MonitorEvent[]>([]);
```

e. Add the dispatch branch in `ws.onmessage`, after the `isCuratorStateMessage` block:

```typescript
} else if (isMonitorEventMessage(data)) {
  setMonitorEvents((prev) => {
    const next = [...prev, { session_id: data.session_id, event: data.event as MonitorEvent['event'] }];
    // Ring buffer: keep last 200
    return next.length > 200 ? next.slice(next.length - 200) : next;
  });
}
```

f. Add the clear callback:

```typescript
const clearMonitorEvents = useCallback(() => {
  setMonitorEvents([]);
}, []);
```

g. Add to the return object:

```typescript
monitorEvents,
clearMonitorEvents,
```

**Step 3: Expose through SessionsContext**

In `assets/dashboard/src/contexts/SessionsContext.tsx`:

a. Add `MonitorEvent` to the import from `../lib/types`.

b. Add to `SessionsContextValue` type:

```typescript
monitorEvents: MonitorEvent[];
clearMonitorEvents: () => void;
```

c. Destructure from the hook call:

```typescript
const {
  // ... existing destructures ...
  monitorEvents,
  clearMonitorEvents,
} = useSessionsWebSocket({ ... });
```

d. Add to the `value` useMemo and its dependency array:

```typescript
monitorEvents,
clearMonitorEvents,
```

**Step 4: Verify it compiles**

```bash
go run ./cmd/build-dashboard
```

Expected: clean build.

---

### Task 6: EventMonitor sidebar component

Create the collapsible sidebar panel that shows the last 5 events.

**Files:**

- Create: `assets/dashboard/src/components/EventMonitor.tsx`
- Modify: `assets/dashboard/src/styles/global.css` (add styles)
- Modify: `assets/dashboard/src/components/AppShell.tsx` (render in sidebar)

**Step 1: Create the component**

Create `assets/dashboard/src/components/EventMonitor.tsx`:

```tsx
import { useState } from 'react';
import { Link } from 'react-router-dom';
import { useSessions } from '../contexts/SessionsContext';

function eventDotColor(eventType: string): string {
  switch (eventType) {
    case 'status':
      return '#0dbc79';
    case 'failure':
      return '#e5a445';
    case 'reflection':
    case 'friction':
      return '#569cd6';
    default:
      return 'var(--color-text-tertiary)';
  }
}

function eventLabel(event: { type: string; state?: string; tool?: string }): string {
  if (event.type === 'status' && event.state) return `status:${event.state}`;
  if (event.type === 'failure' && event.tool) return `failure:${event.tool}`;
  return event.type;
}

function relativeTime(ts: string): string {
  const diff = Date.now() - new Date(ts).getTime();
  if (diff < 0) return 'now';
  const seconds = Math.floor(diff / 1000);
  if (seconds < 60) return `${seconds}s`;
  const minutes = Math.floor(seconds / 60);
  if (minutes < 60) return `${minutes}m`;
  const hours = Math.floor(minutes / 60);
  return `${hours}h`;
}

function sessionNickname(
  sessionId: string,
  sessionsById: Record<string, { nickname?: string }>
): string {
  const session = sessionsById[sessionId];
  const name = session?.nickname || sessionId;
  return name.length > 8 ? name.slice(0, 8) + '…' : name;
}

export default function EventMonitor() {
  const { monitorEvents, clearMonitorEvents, sessionsById } = useSessions();
  const [collapsed, setCollapsed] = useState(
    () => localStorage.getItem('event-monitor-collapsed') === '1'
  );

  const toggleCollapsed = () => {
    setCollapsed((prev) => {
      const next = !prev;
      localStorage.setItem('event-monitor-collapsed', next ? '1' : '0');
      return next;
    });
  };

  const recentEvents = monitorEvents.slice(-5).reverse();

  return (
    <div className="event-monitor">
      <button className="diag-pane__toggle" onClick={toggleCollapsed}>
        <span className={`diag-pane__chevron${collapsed ? '' : ' diag-pane__chevron--open'}`}>
          ▶
        </span>
        <span className="nav-section-title">Events</span>
        {monitorEvents.length > 0 && (
          <span className="event-monitor__badge">{monitorEvents.length}</span>
        )}
      </button>
      {!collapsed && (
        <>
          {recentEvents.length === 0 && <div className="event-monitor__empty">No events yet</div>}
          {recentEvents.map((ev, i) => (
            <div
              key={`${ev.event.ts}-${ev.session_id}-${i}`}
              className="event-monitor__row"
              title={JSON.stringify(ev.event, null, 2)}
            >
              <span className="event-monitor__dot" style={{ color: eventDotColor(ev.event.type) }}>
                ●
              </span>
              <span className="event-monitor__session">
                {sessionNickname(ev.session_id, sessionsById)}
              </span>
              <span className="event-monitor__label">{eventLabel(ev.event)}</span>
              <span className="event-monitor__time">{relativeTime(ev.event.ts)}</span>
            </div>
          ))}
          <div className="event-monitor__footer">
            <Link to="/events" className="event-monitor__link">
              View All
            </Link>
            {monitorEvents.length > 0 && (
              <button className="event-monitor__clear" onClick={clearMonitorEvents}>
                Clear
              </button>
            )}
          </div>
        </>
      )}
    </div>
  );
}
```

**Step 2: Add CSS**

In `assets/dashboard/src/styles/global.css`, add after the `.tmux-diag` styles (around line 305):

```css
/* --- Event Monitor panel --- */
.event-monitor {
  padding: var(--spacing-sm) var(--spacing-md);
  border-top: 1px solid var(--color-border);
  font-size: 11px;
  font-family: Menlo, Monaco, 'Courier New', monospace;
}
.app-shell--collapsed .event-monitor {
  display: none;
}
.event-monitor__badge {
  margin-left: auto;
  background: var(--color-text-tertiary);
  color: var(--color-bg);
  border-radius: 8px;
  padding: 0 5px;
  font-size: 10px;
  min-width: 16px;
  text-align: center;
}
.event-monitor__empty {
  color: var(--color-text-tertiary);
  padding: 4px 0;
  font-style: italic;
}
.event-monitor__row {
  display: flex;
  align-items: center;
  gap: 4px;
  padding: 1px 0;
  white-space: nowrap;
  overflow: hidden;
}
.event-monitor__dot {
  flex-shrink: 0;
  font-size: 8px;
}
.event-monitor__session {
  color: var(--color-text-secondary);
  flex-shrink: 0;
  min-width: 0;
}
.event-monitor__label {
  flex: 1;
  min-width: 0;
  overflow: hidden;
  text-overflow: ellipsis;
}
.event-monitor__time {
  color: var(--color-text-tertiary);
  flex-shrink: 0;
  margin-left: auto;
}
.event-monitor__footer {
  display: flex;
  justify-content: space-between;
  align-items: center;
  padding-top: 4px;
  gap: 8px;
}
.event-monitor__link {
  color: var(--color-link);
  text-decoration: none;
  font-size: 10px;
}
.event-monitor__link:hover {
  text-decoration: underline;
}
.event-monitor__clear {
  background: none;
  border: none;
  color: var(--color-text-tertiary);
  cursor: pointer;
  font-size: 10px;
  padding: 0;
}
.event-monitor__clear:hover {
  color: var(--color-text-secondary);
}
```

**Step 3: Add to AppShell sidebar**

In `assets/dashboard/src/components/AppShell.tsx`:

a. Add import at the top (near the other component imports):

```typescript
import EventMonitor from './EventMonitor';
```

b. Add the component in the sidebar, after the other dev-mode diagnostic panels (after line 868 — `{isDevMode && <TypingPerformance />`):

```tsx
{
  isDevMode && <EventMonitor />;
}
```

**Step 4: Verify it compiles**

```bash
go run ./cmd/build-dashboard
```

Expected: clean build.

---

### Task 7: EventsPage full page view

Create the `/events` route with a filterable event table.

**Files:**

- Create: `assets/dashboard/src/routes/EventsPage.tsx`
- Modify: `assets/dashboard/src/App.tsx` (add route)
- Modify: `assets/dashboard/src/styles/global.css` (add page styles)

**Step 1: Create the page component**

Create `assets/dashboard/src/routes/EventsPage.tsx`:

```tsx
import { useState, useEffect, useRef, useCallback } from 'react';
import { useSessions } from '../contexts/SessionsContext';
import type { MonitorEvent } from '../lib/types';

const EVENT_TYPES = ['status', 'failure', 'reflection', 'friction'] as const;

function typeBadgeClass(eventType: string): string {
  switch (eventType) {
    case 'status':
      return 'events-badge events-badge--status';
    case 'failure':
      return 'events-badge events-badge--failure';
    case 'reflection':
      return 'events-badge events-badge--reflection';
    case 'friction':
      return 'events-badge events-badge--friction';
    default:
      return 'events-badge';
  }
}

function formatTime(ts: string): string {
  try {
    const d = new Date(ts);
    return d.toLocaleTimeString('en-US', {
      hour12: false,
      hour: '2-digit',
      minute: '2-digit',
      second: '2-digit',
      fractionalSecondDigits: 3,
    });
  } catch {
    return ts;
  }
}

function eventDetail(event: MonitorEvent['event']): string {
  if (event.type === 'status') return event.state || '';
  if (event.type === 'failure') return event.tool || '';
  if (event.type === 'reflection' || event.type === 'friction') {
    const text = event.text || '';
    return text.length > 60 ? text.slice(0, 60) + '…' : text;
  }
  return '';
}

function eventMessage(event: MonitorEvent['event']): string {
  if (event.type === 'status') return event.message || '';
  if (event.type === 'failure') return event.error || '';
  if (event.type === 'reflection' || event.type === 'friction') return event.text || '';
  return '';
}

export default function EventsPage() {
  const { monitorEvents, sessionsById } = useSessions();
  const [historyEvents, setHistoryEvents] = useState<MonitorEvent[]>([]);
  const [typeFilter, setTypeFilter] = useState<Set<string>>(new Set(EVENT_TYPES));
  const [sessionFilter, setSessionFilter] = useState<string>('all');
  const [expandedRow, setExpandedRow] = useState<number | null>(null);
  const [autoScroll, setAutoScroll] = useState(true);
  const tableEndRef = useRef<HTMLDivElement>(null);
  const scrollContainerRef = useRef<HTMLDivElement>(null);

  // Fetch history on mount
  useEffect(() => {
    fetch('/api/dev/events/history', { credentials: 'same-origin' })
      .then((res) => (res.ok ? res.json() : []))
      .then((data: MonitorEvent[]) => setHistoryEvents(data))
      .catch(() => {});
  }, []);

  // Merge history + live events, dedup by ts+session_id
  const allEvents = (() => {
    const seen = new Set<string>();
    const merged: MonitorEvent[] = [];
    for (const ev of [...historyEvents, ...monitorEvents]) {
      const key = `${ev.event.ts}:${ev.session_id}`;
      if (!seen.has(key)) {
        seen.add(key);
        merged.push(ev);
      }
    }
    return merged;
  })();

  // Apply filters
  const filteredEvents = allEvents.filter((ev) => {
    if (!typeFilter.has(ev.event.type)) return false;
    if (sessionFilter !== 'all' && ev.session_id !== sessionFilter) return false;
    return true;
  });

  // Auto-scroll
  useEffect(() => {
    if (autoScroll && tableEndRef.current) {
      tableEndRef.current.scrollIntoView({ behavior: 'smooth' });
    }
  }, [filteredEvents.length, autoScroll]);

  // Detect scroll-up to pause auto-scroll
  const handleScroll = useCallback(() => {
    const el = scrollContainerRef.current;
    if (!el) return;
    const atBottom = el.scrollHeight - el.scrollTop - el.clientHeight < 40;
    setAutoScroll(atBottom);
  }, []);

  const toggleType = (type: string) => {
    setTypeFilter((prev) => {
      const next = new Set(prev);
      if (next.has(type)) next.delete(type);
      else next.add(type);
      return next;
    });
  };

  // Unique session IDs for filter dropdown
  const sessionIds = [...new Set(allEvents.map((ev) => ev.session_id))];

  return (
    <div className="events-page">
      <h1 className="events-page__title">Event Monitor</h1>

      <div className="events-page__filters">
        <div className="events-page__type-chips">
          {EVENT_TYPES.map((type) => (
            <button
              key={type}
              className={`events-chip ${typeFilter.has(type) ? 'events-chip--active' : ''} events-chip--${type}`}
              onClick={() => toggleType(type)}
            >
              {type}
            </button>
          ))}
        </div>
        <select
          className="events-page__session-select"
          value={sessionFilter}
          onChange={(e) => setSessionFilter(e.target.value)}
        >
          <option value="all">All Sessions</option>
          {sessionIds.map((id) => (
            <option key={id} value={id}>
              {sessionsById[id]?.nickname || id}
            </option>
          ))}
        </select>
      </div>

      <div className="events-page__table-wrapper" ref={scrollContainerRef} onScroll={handleScroll}>
        <table className="events-page__table">
          <thead>
            <tr>
              <th>Time</th>
              <th>Session</th>
              <th>Type</th>
              <th>Detail</th>
              <th>Message</th>
            </tr>
          </thead>
          <tbody>
            {filteredEvents.map((ev, i) => (
              <>
                <tr
                  key={`${ev.event.ts}-${ev.session_id}-${i}`}
                  className="events-page__row"
                  onClick={() => setExpandedRow(expandedRow === i ? null : i)}
                >
                  <td className="events-page__col-time">{formatTime(ev.event.ts)}</td>
                  <td className="events-page__col-session">
                    {sessionsById[ev.session_id]?.nickname || ev.session_id}
                  </td>
                  <td>
                    <span className={typeBadgeClass(ev.event.type)}>{ev.event.type}</span>
                  </td>
                  <td className="events-page__col-detail">{eventDetail(ev.event)}</td>
                  <td className="events-page__col-message">{eventMessage(ev.event)}</td>
                </tr>
                {expandedRow === i && (
                  <tr key={`expanded-${i}`} className="events-page__expanded">
                    <td colSpan={5}>
                      <pre className="events-page__json">{JSON.stringify(ev.event, null, 2)}</pre>
                    </td>
                  </tr>
                )}
              </>
            ))}
          </tbody>
        </table>
        <div ref={tableEndRef} />
      </div>

      {!autoScroll && (
        <button
          className="events-page__jump"
          onClick={() => {
            setAutoScroll(true);
            tableEndRef.current?.scrollIntoView({ behavior: 'smooth' });
          }}
        >
          ↓ Jump to latest
        </button>
      )}
    </div>
  );
}
```

**Step 2: Add the route**

In `assets/dashboard/src/App.tsx`:

a. Add the import:

```typescript
import EventsPage from './routes/EventsPage';
```

b. Add the route inside the `<Route element={<AppShell />}>` block (after the `/lore` route):

```tsx
<Route path="/events" element={<EventsPage />} />
```

**Step 3: Add page styles**

In `assets/dashboard/src/styles/global.css`, add after the event-monitor panel styles:

```css
/* --- Events page --- */
.events-page {
  padding: var(--spacing-lg);
  display: flex;
  flex-direction: column;
  height: 100%;
}
.events-page__title {
  font-size: 18px;
  font-weight: 600;
  margin: 0 0 var(--spacing-md);
}
.events-page__filters {
  display: flex;
  align-items: center;
  gap: var(--spacing-md);
  margin-bottom: var(--spacing-md);
  flex-wrap: wrap;
}
.events-page__type-chips {
  display: flex;
  gap: 4px;
}
.events-chip {
  padding: 2px 10px;
  border-radius: 12px;
  border: 1px solid var(--color-border);
  background: none;
  color: var(--color-text-secondary);
  cursor: pointer;
  font-size: 12px;
  font-family: inherit;
  transition: opacity 0.15s;
}
.events-chip--active {
  color: var(--color-text);
}
.events-chip--active.events-chip--status {
  border-color: #0dbc79;
  background: rgba(13, 188, 121, 0.1);
}
.events-chip--active.events-chip--failure {
  border-color: #e5a445;
  background: rgba(229, 164, 69, 0.1);
}
.events-chip--active.events-chip--reflection {
  border-color: #569cd6;
  background: rgba(86, 156, 214, 0.1);
}
.events-chip--active.events-chip--friction {
  border-color: #569cd6;
  background: rgba(86, 156, 214, 0.1);
}
.events-chip:not(.events-chip--active) {
  opacity: 0.4;
}
.events-page__session-select {
  padding: 4px 8px;
  border: 1px solid var(--color-border);
  border-radius: 4px;
  background: var(--color-bg);
  color: var(--color-text);
  font-size: 12px;
  font-family: inherit;
}
.events-page__table-wrapper {
  flex: 1;
  overflow-y: auto;
  border: 1px solid var(--color-border);
  border-radius: 4px;
}
.events-page__table {
  width: 100%;
  border-collapse: collapse;
  font-size: 12px;
  font-family: Menlo, Monaco, 'Courier New', monospace;
}
.events-page__table thead {
  position: sticky;
  top: 0;
  background: var(--color-bg-elevated, var(--color-bg));
  z-index: 1;
}
.events-page__table th {
  text-align: left;
  padding: 6px 8px;
  border-bottom: 1px solid var(--color-border);
  font-weight: 600;
  color: var(--color-text-secondary);
  white-space: nowrap;
}
.events-page__row {
  cursor: pointer;
}
.events-page__row:hover {
  background: var(--color-bg-hover, rgba(255, 255, 255, 0.03));
}
.events-page__table td {
  padding: 4px 8px;
  border-bottom: 1px solid var(--color-border-subtle, rgba(255, 255, 255, 0.05));
  vertical-align: top;
}
.events-page__col-time {
  white-space: nowrap;
  color: var(--color-text-tertiary);
}
.events-page__col-session {
  white-space: nowrap;
}
.events-page__col-detail {
  white-space: nowrap;
}
.events-page__col-message {
  word-break: break-word;
  max-width: 400px;
}
.events-badge {
  display: inline-block;
  padding: 1px 6px;
  border-radius: 3px;
  font-size: 10px;
  font-weight: 600;
}
.events-badge--status {
  background: rgba(13, 188, 121, 0.15);
  color: #0dbc79;
}
.events-badge--failure {
  background: rgba(229, 164, 69, 0.15);
  color: #e5a445;
}
.events-badge--reflection {
  background: rgba(86, 156, 214, 0.15);
  color: #569cd6;
}
.events-badge--friction {
  background: rgba(86, 156, 214, 0.15);
  color: #569cd6;
}
.events-page__expanded td {
  background: var(--color-bg-elevated, rgba(255, 255, 255, 0.02));
  padding: 0;
}
.events-page__json {
  margin: 0;
  padding: 8px 12px;
  font-size: 11px;
  color: var(--color-text-secondary);
  white-space: pre-wrap;
  word-break: break-all;
}
.events-page__jump {
  position: fixed;
  bottom: 24px;
  right: 24px;
  padding: 8px 16px;
  border-radius: 20px;
  border: 1px solid var(--color-border);
  background: var(--color-bg-elevated, var(--color-bg));
  color: var(--color-text);
  cursor: pointer;
  font-size: 13px;
  box-shadow: 0 2px 8px rgba(0, 0, 0, 0.3);
  z-index: 10;
}
.events-page__jump:hover {
  background: var(--color-bg-hover, rgba(255, 255, 255, 0.05));
}
```

**Step 4: Verify it compiles**

```bash
go run ./cmd/build-dashboard
```

Expected: clean build.

---

### Task 8: Add Events nav link (dev mode only)

Add a navigation link to `/events` in the sidebar, visible only in dev mode.

**Files:**

- Modify: `assets/dashboard/src/components/AppShell.tsx`

**Step 1: Add the nav link**

In `AppShell.tsx`, inside the `<div className="nav-links">` section, add a dev-mode-only link. Place it after the `{isDevMode && <EventMonitor />}` panel (before the Personas link), wrapped in an `isDevMode` guard:

```tsx
{
  isDevMode && (
    <NavLink
      to="/events"
      className={({ isActive }) => `nav-link${isActive ? ' nav-link--active' : ''}`}
    >
      <svg
        className="nav-link__icon"
        viewBox="0 0 24 24"
        fill="none"
        stroke="currentColor"
        strokeWidth="2"
      >
        <polyline points="22 12 18 12 15 21 9 3 6 12 2 12"></polyline>
      </svg>
      <span>Events</span>
    </NavLink>
  );
}
```

The SVG is a pulse/activity icon (zigzag line).

**Step 2: Verify it compiles**

```bash
go run ./cmd/build-dashboard
```

Expected: clean build.

---

### Task 9: Run all tests

Verify the full test suite passes with all changes.

**Step 1: Run backend tests**

```bash
go test ./...
```

Expected: all pass.

**Step 2: Run frontend tests**

```bash
./test.sh --quick
```

Expected: all pass (backend + frontend).

**Step 3: Commit**

Use `/commit` to commit all changes.
