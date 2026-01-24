import React from 'react';
import { useNavigate } from 'react-router-dom';
import { disposeSession, getErrorMessage } from '../lib/api';
import { formatRelativeTime, formatTimestamp } from '../lib/utils';
import { useToast } from './ToastProvider';
import { useModal } from './ModalProvider';
import { useConfig } from '../contexts/ConfigContext';
import { useSessions } from '../contexts/SessionsContext';
import Tooltip from './Tooltip';
import type { SessionResponse, WorkspaceResponse } from '../lib/types';

const nudgeStateEmoji: Record<string, string> = {
  'Needs Authorization': '\u26D4\uFE0F',
  'Needs Feature Clarification': '\uD83D\uDD0D',
  'Needs User Testing': '\uD83D\uDC40',
  'Completed': '\u2705',
};

function formatNudgeSummary(summary?: string) {
  if (!summary) return null;
  let text = summary.trim();
  if (text.length > 100) {
    text = text.substring(0, 97) + '...';
  }
  return text;
}

function WorkingSpinner() {
  return <span className="working-spinner"></span>;
}

type SessionTabsProps = {
  sessions: SessionResponse[];
  currentSessionId?: string;
  workspace?: WorkspaceResponse;
  activeDiffTab?: boolean;
};

export default function SessionTabs({ sessions, currentSessionId, workspace, activeDiffTab }: SessionTabsProps) {
  const navigate = useNavigate();
  const { success, error: toastError } = useToast();
  const { confirm } = useModal();
  const { config } = useConfig();
  const { refresh } = useSessions();

  // Calculate if we should show diff tab
  const linesAdded = workspace?.git_lines_added ?? 0;
  const linesRemoved = workspace?.git_lines_removed ?? 0;
  const filesChanged = workspace?.git_files_changed ?? 0;
  const hasChanges = filesChanged > 0 || linesAdded > 0 || linesRemoved > 0;

  const handleDiffTabClick = () => {
    if (workspace) {
      navigate(`/diff/${workspace.id}`);
    }
  };

  const handleDispose = async (sessionId: string, event: React.MouseEvent) => {
    event.stopPropagation();

    const sess = sessions.find(s => s.id === sessionId);
    let sessionDisplay = sessionId;
    if (sess?.nickname) {
      sessionDisplay = `${sess.nickname} (${sessionId})`;
    }

    const accepted = await confirm(`Dispose session ${sessionDisplay}?`, { danger: true });
    if (!accepted) return;

    try {
      await disposeSession(sessionId);
      success('Session disposed');
      refresh();
    } catch (err) {
      toastError(`Failed to dispose: ${getErrorMessage(err, 'Unknown error')}`);
    }
  };

  const handleTabClick = (sessionId: string) => {
    navigate(`/sessions/${sessionId}`);
  };

  const nudgenikEnabled = Boolean(config?.nudgenik?.target);

  return (
    <div className="session-tabs">
      {sessions.map((sess) => {
        const isCurrent = sess.id === currentSessionId;
        const displayName = sess.nickname || sess.target;

        const runTarget = (config?.run_targets || []).find(t => t.name === sess.target);
        const isPromptable = runTarget ? runTarget.type === 'promptable' : true;

        const nudgeEmoji = sess.nudge_state ? (nudgeStateEmoji[sess.nudge_state] || '\uD83D\uDCDD') : null;
        const nudgeSummary = formatNudgeSummary(sess.nudge_summary);

        let nudgePreview = nudgenikEnabled && nudgeEmoji && nudgeSummary ? `${nudgeEmoji} ${nudgeSummary}` : null;
        let nudgePreviewElement: React.ReactNode = null;

        if (nudgenikEnabled && !nudgePreview && isPromptable && sess.running) {
          nudgePreviewElement = (
            <span style={{ display: 'flex', alignItems: 'center', gap: 'var(--spacing-xs)' }}>
              <WorkingSpinner />
              <span>Working...</span>
            </span>
          );
        } else if (nudgePreview) {
          nudgePreviewElement = nudgePreview;
        }

        // Show "Stopped" for stopped sessions, otherwise show last activity time
        const activityDisplay = !sess.running
          ? 'Stopped'
          : sess.last_output_at
            ? formatRelativeTime(sess.last_output_at)
            : '-';

        return (
          <div
            key={sess.id}
            className={`session-tab${isCurrent ? ' session-tab--active' : ''}`}
            onClick={() => handleTabClick(sess.id)}
            role="button"
            tabIndex={0}
            onKeyDown={(e) => {
              if (e.key === 'Enter' || e.key === ' ') {
                handleTabClick(sess.id);
              }
            }}
          >
            <div className="session-tab__row1">
              <span className={`session-tab__name${sess.nickname ? '' : ' mono'}`}>
                {displayName}
              </span>
              <Tooltip content={!sess.running ? 'Session stopped' : (sess.last_output_at ? formatTimestamp(sess.last_output_at) : 'Never')}>
                <span className="session-tab__activity">
                  {activityDisplay}
                </span>
              </Tooltip>
              <Tooltip content="Dispose session" variant="warning">
                <button
                  className="btn btn--sm btn--ghost btn--danger session-tab__dispose"
                  onClick={(e) => handleDispose(sess.id, e)}
                  aria-label={`Dispose ${sess.id}`}
                >
                  <svg width="10" height="10" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2">
                    <polyline points="3 6 5 6 21 6"></polyline>
                    <path d="M19 6v14a2 2 0 0 1-2 2H7a2 2 0 0 1-2-2V6m3 0V4a2 2 0 0 1 2-2h4a2 2 0 0 1 2 2v2"></path>
                  </svg>
                </button>
              </Tooltip>
            </div>
            {nudgePreviewElement && (
              <div className="session-tab__row2">
                {nudgePreviewElement}
              </div>
            )}
          </div>
        );
      })}
      {hasChanges && (
        <div
          className={`session-tab session-tab--diff${activeDiffTab ? ' session-tab--active' : ''}`}
          onClick={handleDiffTabClick}
          role="button"
          tabIndex={0}
          onKeyDown={(e) => {
            if (e.key === 'Enter' || e.key === ' ') {
              handleDiffTabClick();
            }
          }}
        >
          <div className="session-tab__row1">
            <span className="session-tab__name">
              {filesChanged} file{filesChanged !== 1 ? 's' : ''}
            </span>
            <span className="session-tab__diff-stats">
              {linesAdded > 0 && <span style={{ color: 'var(--color-success)' }}>+{linesAdded}</span>}
              {linesRemoved > 0 && <span style={{ color: 'var(--color-error)', marginLeft: linesAdded > 0 ? '4px' : '0' }}>-{linesRemoved}</span>}
            </span>
          </div>
        </div>
      )}
    </div>
  );
}
