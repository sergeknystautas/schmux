# Event Monitor — Dev Mode Diagnostic Tool

## Overview

A real-time event monitor for the unified events system, available only in dev mode. Two UI surfaces: a compact collapsible sidebar panel showing recent activity, and a full page view at `/events` for detailed inspection with filtering.

## Architecture

Events flow from the existing `EventWatcher` through a new `EventMonitorHandler` that forwards all event types to the dashboard server. The server broadcasts these over the existing `/ws/dashboard` WebSocket as a new `"event"` message type. Only active in dev mode — production builds skip it entirely.

The frontend stores events in a bounded ring buffer (200 events). The sidebar renders the most recent 5; the full page renders the complete buffer with filters.

### Data Flow

```
Hook/Agent → event file (.schmux/events/<session>.jsonl)
          → EventWatcher (fsnotify)
          → EventMonitorHandler (new, dev-mode only)
          → Dashboard Server
          → /ws/dashboard broadcast (message type: "event")
          → Frontend ring buffer
          → Sidebar panel / Full page view
```

## Backend

### EventMonitorHandler

New handler in `internal/events/` implementing `EventHandler`. Unlike `DashboardHandler` (which handles status only), this forwards all event types via a callback:

```go
type MonitorCallback func(sessionID string, raw RawEvent, data []byte)
```

Registered for all four event types (`status`, `failure`, `reflection`, `friction`) but only when running in dev mode.

### WebSocket Message

```json
{
  "type": "event",
  "session_id": "ws1-abc123",
  "event": { "ts": "...", "type": "status", "state": "completed", "message": "Done" }
}
```

Broadcast through the existing `wsConn` writer (mutex-protected).

### History Endpoint

`GET /api/events/history` — scans all `<workspace>/.schmux/events/*.jsonl` files across active workspaces, parses each line, tags with session ID (derived from filename), and returns the most recent 200 events sorted by timestamp. Dev mode only.

## Frontend: Sidebar Panel

Component: `EventMonitor` in `assets/dashboard/src/components/`, following the TmuxDiagnostic/StreamMetricsPanel collapsible pattern.

### Header

- Title: "Event Monitor"
- Count badge showing events received since last clear (updates even when collapsed)

### Expanded View (last 5 events)

Each row:

- Color dot: green (status), orange (failure), blue (reflection/friction)
- Session nickname (truncated to ~8 chars)
- Event type label: `status:completed`, `failure:bash`, `reflection`, `friction`
- Relative timestamp: `2s`, `1m`, `5m`
- No message text (to save space). Hover shows the full event JSON as a tooltip.

### Controls

- "View All" link navigates to `/events`
- "Clear" button resets the buffer and count badge

### State Management

Subscribes to `"event"` messages from the existing SessionsContext WebSocket. Events are stored in a `useRef` ring buffer (capacity 200) with a `useState` counter for re-renders only when the visible portion changes.

## Frontend: Full Page View (/events)

Route: `/events`, accessible from the sidebar "View All" link and from the main nav (dev mode only).

### Table Columns

| Column           | Content                                                                                  |
| ---------------- | ---------------------------------------------------------------------------------------- |
| **Time**         | Absolute HH:MM:SS.ms                                                                     |
| **Session**      | Nickname, color-coded per session                                                        |
| **Type**         | Badge-styled                                                                             |
| **State/Detail** | State for status events, tool name for failures, truncated text for reflections/friction |
| **Message**      | Full text, wrapping allowed                                                              |

### Filter Bar

- **Type filter**: toggleable chips per event type (all active by default)
- **Session filter**: dropdown of active sessions by nickname ("All" by default)

### Behavior

- Auto-scrolls to bottom (live tail). Pauses when the user scrolls up. A "Jump to latest" button appears when paused.
- On mount, fetches `GET /api/events/history` for pre-connection events. Deduplicates by timestamp + session ID.
- Clicking a row expands inline to show the full raw JSON.
- Ring buffer capped at 200 — no pagination needed.

## Scope Boundaries

- Dev mode only — no production impact
- Read-only — no event injection or replay
- No changes to existing event dispatch pipeline
- Monitor handler is purely additive (observes, does not modify)
