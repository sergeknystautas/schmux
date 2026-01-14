import React from 'react';
import { Link, useNavigate } from 'react-router-dom';
import { formatRelativeTime, formatTimestamp } from '../lib/utils.js';
import { useViewedSessions } from '../contexts/ViewedSessionsContext.jsx';
import Tooltip from './Tooltip.jsx';

const nudgeStateEmoji = {
  'Needs Authorization': '⛔️',
  'Needs Feature Clarification': '🔍',
  'Needs User Testing': '👀',
  'Completed': '✅',
};

function formatNudgeSummary(summary) {
  if (!summary) return null;

  let text = summary.trim();
  if (text.length > 160) {
    text = text.substring(0, 157) + '...';
  }
  return text;
}

export default function SessionTableRow({ sess, onCopyAttach, onDispose, currentSessionId }) {
  const { viewedSessions } = useViewedSessions();
  const navigate = useNavigate();

  const statusClass = sess.running ? 'status-pill--running' : 'status-pill--stopped';
  const statusText = sess.running ? 'Running' : 'Stopped';
  const displayName = sess.nickname || sess.agent;
  const isCurrent = currentSessionId === sess.id;
  const nudgeEmoji = sess.nudge_state ? (nudgeStateEmoji[sess.nudge_state] || '📝') : null;
  const nudgeSummary = formatNudgeSummary(sess.nudge_summary);

  // Check for new updates since last viewed
  const lastViewedAt = viewedSessions[sess.id] || 0;
  const lastOutputTime = sess.last_output_at ? new Date(sess.last_output_at).getTime() : 0;
  const hasNewUpdates = lastOutputTime > 0 && lastOutputTime > lastViewedAt;
  const nudgePreview = nudgeEmoji && nudgeSummary ? `${nudgeEmoji} ${nudgeSummary}` : null;

  return (
    <tbody className="session-row-group">
      <tr className={`session-row${isCurrent ? ' session-row--current' : ''} session-row--has-nudge`} key={sess.id}>
        <td>
          <div style={{ display: 'flex', alignItems: 'center', gap: 'var(--spacing-xs)' }}>
            <Tooltip content="View session">
              <button
                className="session-table__name-link"
                onClick={() => navigate(`/sessions/${sess.id}`)}
              >
                <span className={sess.nickname ? '' : 'mono'}>{displayName}</span>
              </button>
            </Tooltip>
            {sess.nickname ? (
              <span className="badge badge--secondary" style={{ fontSize: '0.75rem' }}>
                {sess.agent}
              </span>
            ) : null}
          </div>
          {!sess.nickname && (
            <Tooltip content="View session">
              <button
                className="session-table__name-link"
                onClick={() => navigate(`/sessions/${sess.id}`)}
                style={{ fontSize: '0.75rem', color: 'var(--color-text-subtle)' }}
              >
                {sess.id}
              </button>
            </Tooltip>
          )}
        </td>
        <td>
          <span className={`status-pill ${statusClass}`}>
            <span className="status-pill__dot"></span>
            {statusText}
          </span>
        </td>
        <td>
          <Tooltip content={formatTimestamp(sess.created_at)}>
            <span>{formatRelativeTime(sess.created_at)}</span>
          </Tooltip>
        </td>
        <td>
          <div style={{ display: 'flex', alignItems: 'center', gap: 'var(--spacing-xs)' }}>
            <Tooltip content="New updates since you last viewed">
              <span className="badge badge--indicator" style={{
                visibility: hasNewUpdates ? 'visible' : 'hidden',
              }}>
                New
              </span>
            </Tooltip>
            <Tooltip content={sess.last_output_at ? formatTimestamp(sess.last_output_at) : 'Never'}>
              <span>{sess.last_output_at ? formatRelativeTime(sess.last_output_at) : '-'}</span>
            </Tooltip>
          </div>
        </td>
        <td>
          <div className="session-table__actions">
            <Tooltip content="View session">
              <Link
                className="btn btn--sm btn--ghost"
                to={`/sessions/${sess.id}`}
                aria-label={`View ${sess.id}`}
              >
                <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2">
                  <path d="M1 12s4-8 11-8 11 8 11 8-4 8-11 8-11-8-11-8z"></path>
                  <circle cx="12" cy="12" r="3"></circle>
                </svg>
              </Link>
            </Tooltip>
            <Tooltip content="Copy attach command">
              <button
                className="btn btn--sm btn--ghost"
                onClick={() => onCopyAttach(sess.attach_cmd)}
                aria-label="Copy attach command"
              >
                <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2">
                  <rect x="9" y="9" width="13" height="13" rx="2" ry="2"></rect>
                  <path d="M5 15H4a2 2 0 0 1-2-2V4a2 2 0 0 1 2-2h9a2 2 0 0 1 2 2v1"></path>
                </svg>
              </button>
            </Tooltip>
            <Tooltip content="Dispose session" variant="warning">
              <button
                className="btn btn--sm btn--ghost btn--danger"
                onClick={() => onDispose(sess.id)}
                aria-label={`Dispose ${sess.id}`}
              >
                <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2">
                  <polyline points="3 6 5 6 21 6"></polyline>
                  <path d="M19 6v14a2 2 0 0 1-2 2H7a2 2 0 0 1-2-2V6m3 0V4a2 2 0 0 1 2-2h4a2 2 0 0 1 2 2v2"></path>
                </svg>
              </button>
            </Tooltip>
          </div>
        </td>
      </tr>
      <tr className={`session-row session-row--nudge${isCurrent ? ' session-row--current' : ''}${nudgePreview ? '' : ' session-row--nudge-empty'}`}>
        <td colSpan="5">
          <Link
            to={`/sessions/${sess.id}`}
            style={{
              fontSize: '0.75rem',
              color: nudgePreview ? 'var(--color-text-muted)' : 'transparent',
              fontStyle: 'italic',
              textDecoration: 'none',
              whiteSpace: 'normal',
            }}
          >
            {nudgePreview || 'placeholder'}
          </Link>
        </td>
      </tr>
    </tbody>
  );
}
