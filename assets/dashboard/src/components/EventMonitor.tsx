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
  return name.length > 8 ? name.slice(0, 8) + '\u2026' : name;
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
          &#9654;
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
                &#9679;
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
