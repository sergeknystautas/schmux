import React, { useState, useEffect, useRef, useCallback } from 'react';
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
    const base = d.toLocaleTimeString('en-US', {
      hour12: false,
      hour: '2-digit',
      minute: '2-digit',
      second: '2-digit',
    });
    const ms = String(d.getMilliseconds()).padStart(3, '0');
    return `${base}.${ms}`;
  } catch {
    return ts;
  }
}

function eventDetail(event: MonitorEvent['event']): string {
  if (event.type === 'status') return event.state || '';
  if (event.type === 'failure') return event.tool || '';
  if (event.type === 'reflection' || event.type === 'friction') {
    const text = event.text || '';
    return text.length > 60 ? text.slice(0, 60) + '\u2026' : text;
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
              <React.Fragment key={`${ev.event.ts}-${ev.session_id}-${i}`}>
                <tr
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
                  <tr className="events-page__expanded">
                    <td colSpan={5}>
                      <pre className="events-page__json">{JSON.stringify(ev.event, null, 2)}</pre>
                    </td>
                  </tr>
                )}
              </React.Fragment>
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
          &darr; Jump to latest
        </button>
      )}
    </div>
  );
}
