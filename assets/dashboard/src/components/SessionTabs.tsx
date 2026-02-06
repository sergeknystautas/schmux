import React, { useState, useRef, useEffect } from 'react';
import { createPortal } from 'react-dom';
import { useNavigate, useLocation } from 'react-router-dom';
import { disposeSession, spawnSessions, getErrorMessage } from '../lib/api';
import { formatRelativeTime, formatTimestamp } from '../lib/utils';
import { useToast } from './ToastProvider';
import { useModal } from './ModalProvider';
import { useConfig } from '../contexts/ConfigContext';
import { useSessions } from '../contexts/SessionsContext';
import { useKeyboardMode } from '../contexts/KeyboardContext';
import Tooltip from './Tooltip';
import type { SessionResponse, WorkspaceResponse } from '../lib/types';
import { mergeQuickLaunchNames } from '../lib/quicklaunch';

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
  activeSpawnTab?: boolean;
  activeGitTab?: boolean;
  activeLinearSyncResolveConflictTab?: boolean;
};

export default function SessionTabs({ sessions, currentSessionId, workspace, activeDiffTab, activeSpawnTab, activeGitTab, activeLinearSyncResolveConflictTab }: SessionTabsProps) {
  const navigate = useNavigate();
  const location = useLocation();
  const { success, error: toastError } = useToast();
  const { confirm } = useModal();
  const { config } = useConfig();
  const { waitForSession, linearSyncResolveConflictStates } = useSessions();
  const { setContext, clearContext } = useKeyboardMode();

  // Spawn dropdown state
  const [spawnMenuOpen, setSpawnMenuOpen] = useState(false);
  const [spawning, setSpawning] = useState(false);
  const [menuPosition, setMenuPosition] = useState({ top: 0, left: 0 });
  const [placementAbove, setPlacementAbove] = useState(false);
  const spawnButtonRef = useRef<HTMLButtonElement | null>(null);
  const spawnMenuRef = useRef<HTMLDivElement | null>(null);
  const crState = workspace ? linearSyncResolveConflictStates[workspace.id] : undefined;
  const resolveInProgress = crState?.status === 'in_progress';

  const quickLaunch = React.useMemo<string[]>(() => {
    const globalNames = (config?.quick_launch || []).map((item) => item.name);
    return mergeQuickLaunchNames(globalNames, workspace?.quick_launch || []);
  }, [config?.quick_launch, workspace?.quick_launch]);

  // Calculate if we should show diff tab
  const linesAdded = workspace?.git_lines_added ?? 0;
  const linesRemoved = workspace?.git_lines_removed ?? 0;
  const filesChanged = workspace?.git_files_changed ?? 0;
  const hasChanges = filesChanged > 0 || linesAdded > 0 || linesRemoved > 0;

  // Calculate spawn menu position
  useEffect(() => {
    if (spawnMenuOpen && spawnButtonRef.current) {
      const rect = spawnButtonRef.current.getBoundingClientRect();
      const gap = 4;
      const edgePadding = 8;
      const estimatedMenuHeight = spawnMenuRef.current?.offsetHeight ||
        Math.min(300, 60 + (quickLaunch?.length || 0) * 52 + 40);
      const spaceBelow = window.innerHeight - rect.bottom - gap;
      const spaceAbove = rect.top - gap;
      const shouldPlaceAbove = spaceBelow < estimatedMenuHeight && spaceAbove > spaceBelow;
      setPlacementAbove(shouldPlaceAbove);

      // Calculate left position, ensuring menu stays on screen
      let left = rect.left;
      const menuWidth = spawnMenuRef.current?.offsetWidth;
      if (menuWidth) {
        const rightEdge = left + menuWidth;
        if (rightEdge > window.innerWidth - edgePadding) {
          left = window.innerWidth - menuWidth - edgePadding;
        }
      }

      if (shouldPlaceAbove) {
        setMenuPosition({ top: rect.top - gap, left });
      } else {
        setMenuPosition({ top: rect.bottom + gap, left });
      }
    }
  }, [spawnMenuOpen, quickLaunch?.length]);

  // Close spawn menu when clicking outside
  useEffect(() => {
    const handleClickOutside = (event: MouseEvent) => {
      const target = event.target as Node | null;
      if (!target) return;
      if (spawnButtonRef.current?.contains(target)) return;
      if (spawnMenuRef.current?.contains(target)) return;
      setSpawnMenuOpen(false);
    };

    if (spawnMenuOpen) {
      document.addEventListener('mousedown', handleClickOutside);
    }
    return () => {
      document.removeEventListener('mousedown', handleClickOutside);
    };
  }, [spawnMenuOpen]);

  useEffect(() => {
    if (resolveInProgress && spawnMenuOpen) {
      setSpawnMenuOpen(false);
    }
  }, [resolveInProgress, spawnMenuOpen]);

  useEffect(() => {
    if (!workspace || !resolveInProgress) return;
    const target = `/resolve-conflict/${workspace.id}`;
    if (location.pathname !== target) {
      navigate(target, { replace: true });
    }
  }, [workspace, resolveInProgress, location.pathname, navigate]);

  // Set keyboard context for the active workspace/session
  useEffect(() => {
    if (!workspace) return;
    setContext({
      workspaceId: workspace.id,
      sessionId: currentSessionId || null,
    });

    return () => {
      clearContext();
    };
  }, [workspace?.id, currentSessionId, setContext, clearContext]);

  const handleDiffTabClick = () => {
    if (workspace) {
      navigate(`/diff/${workspace.id}`);
    }
  };

  const handleGitTabClick = () => {
    if (workspace) {
      navigate(`/git/${workspace.id}`);
    }
  };

  const handleResolveConflictTabClick = () => {
    if (workspace) {
      navigate(`/resolve-conflict/${workspace.id}`);
    }
  };

  const handleSpawnTabClick = () => {
    if (workspace) {
      navigate(`/spawn?workspace_id=${workspace.id}`);
    }
  };

  const handleCustomSpawn = (event: React.MouseEvent) => {
    event.stopPropagation();
    setSpawnMenuOpen(false);
    if (workspace) {
      navigate(`/spawn?workspace_id=${workspace.id}`);
    }
  };

  const handleQuickLaunchSpawn = async (name: string, event: React.MouseEvent) => {
    event.stopPropagation();
    if (!workspace) return;
    setSpawnMenuOpen(false);
    setSpawning(true);

    try {
      const response = await spawnSessions({
        repo: workspace.repo,
        branch: workspace.branch,
        prompt: '',
        nickname: name,
        workspace_id: workspace.id,
        quick_launch_name: name,
      });

      const result = response[0];
      if (result.error) {
        toastError(`Failed to spawn ${name}: ${result.error}`);
      } else {
        success(`Spawned ${name} session`);
        await waitForSession(result.session_id);
        navigate(`/sessions/${result.session_id}`);
      }
    } catch (err) {
      toastError(`Failed to spawn: ${getErrorMessage(err, 'Unknown error')}`);
    } finally {
      setSpawning(false);
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
    } catch (err) {
      toastError(`Failed to dispose: ${getErrorMessage(err, 'Unknown error')}`);
    }
  };

  const handleTabClick = (sessionId: string) => {
    navigate(`/sessions/${sessionId}`);
  };

  const nudgenikEnabled = Boolean(config?.nudgenik?.target);

  // Helper to render a session tab
  const renderSessionTab = (sess: SessionResponse) => {
    const isCurrent = sess.id === currentSessionId;
    const displayName = sess.nickname || sess.target;
    const disabled = resolveInProgress;

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
        className={`session-tab${isCurrent ? ' session-tab--active' : ''}${disabled ? ' session-tab--disabled' : ''}`}
        onClick={() => !disabled && handleTabClick(sess.id)}
        role="button"
        tabIndex={disabled ? -1 : 0}
        onKeyDown={(e) => {
          if (disabled) return;
          if (e.key === 'Enter' || e.key === ' ') {
            handleTabClick(sess.id);
          }
        }}
        style={disabled ? { opacity: 0.5, cursor: 'not-allowed' } : undefined}
      >
        <div className="session-tab__row1">
          <span className="session-tab__name">
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
              onClick={(e) => !disabled && handleDispose(sess.id, e)}
              aria-label={`Dispose ${sess.id}`}
              disabled={disabled}
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
  };

  // Helper to render the diff tab (always shown)
  const renderDiffTab = () => (
    <div
      className={`session-tab session-tab--diff${activeDiffTab ? ' session-tab--active' : ''}${resolveInProgress ? ' session-tab--disabled' : ''}`}
      onClick={() => !resolveInProgress && handleDiffTabClick()}
      role="button"
      tabIndex={resolveInProgress ? -1 : 0}
      onKeyDown={(e) => {
        if (resolveInProgress) return;
        if (e.key === 'Enter' || e.key === ' ') {
          handleDiffTabClick();
        }
      }}
      style={resolveInProgress ? { opacity: 0.5, cursor: 'not-allowed' } : undefined}
    >
      <div className="session-tab__row1">
        <span className="session-tab__name">
          {filesChanged} file{filesChanged !== 1 ? 's' : ''} changed
        </span>
        {hasChanges && (
          <span className="session-tab__diff-stats">
            {linesAdded > 0 && <span style={{ color: 'var(--color-success)' }}>+{linesAdded}</span>}
            {linesRemoved > 0 && <span style={{ color: 'var(--color-error)', marginLeft: linesAdded > 0 ? '4px' : '0' }}>-{linesRemoved}</span>}
          </span>
        )}
      </div>
    </div>
  );

  // Helper to render the git tab
  const renderGitTab = () => (
    <div
      className={`session-tab session-tab--diff${activeGitTab ? ' session-tab--active' : ''}${resolveInProgress ? ' session-tab--disabled' : ''}`}
      onClick={() => !resolveInProgress && handleGitTabClick()}
      role="button"
      tabIndex={resolveInProgress ? -1 : 0}
      onKeyDown={(e) => {
        if (resolveInProgress) return;
        if (e.key === 'Enter' || e.key === ' ') {
          handleGitTabClick();
        }
      }}
      style={resolveInProgress ? { opacity: 0.5, cursor: 'not-allowed' } : undefined}
    >
      <div className="session-tab__row1">
        <span className="session-tab__name">git graph</span>
      </div>
    </div>
  );

  // Helper to render the resolve conflict tab (only when state exists)
  const renderResolveConflictTab = () => {
    if (!crState && !activeLinearSyncResolveConflictTab) return null;
    const hash = crState?.hash ? crState.hash.substring(0, 7) : '...';
    const isActive = crState ? crState.status === 'in_progress' : true;
    const isFailed = crState?.status === 'failed';
    const label = isActive ? 'Resolving conflict on' : isFailed ? 'Resolve conflict failed on' : 'Resolve conflict on';
    return (
      <div
        className={`session-tab session-tab--diff${activeLinearSyncResolveConflictTab ? ' session-tab--active' : ''}`}
        onClick={handleResolveConflictTabClick}
        role="button"
        tabIndex={0}
        onKeyDown={(e) => {
          if (e.key === 'Enter' || e.key === ' ') {
            handleResolveConflictTabClick();
          }
        }}
      >
        <div className="session-tab__row1">
          <span className="session-tab__name" style={{ display: 'flex', alignItems: 'center', gap: 6 }}>
            {isActive && <div className="spinner--small" style={{ width: 10, height: 10, borderWidth: 2 }} />}
            {label} {hash}
          </span>
        </div>
      </div>
    );
  };

  // Helper to render the add button
  const renderAddButton = () => (
    <>
      <button
        ref={spawnButtonRef}
        className="session-tab--add"
        onClick={(e) => {
          if (resolveInProgress) return;
          e.stopPropagation();
          setSpawnMenuOpen(!spawnMenuOpen);
        }}
        disabled={spawning || resolveInProgress}
        aria-expanded={spawnMenuOpen}
        aria-haspopup="menu"
        aria-label="Spawn new session"
        style={resolveInProgress ? { opacity: 0.5, cursor: 'not-allowed' } : undefined}
      >
        {spawning ? (
          <span className="spinner spinner--small"></span>
        ) : (
          <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2.5">
            <line x1="12" y1="5" x2="12" y2="19"></line>
            <line x1="5" y1="12" x2="19" y2="12"></line>
          </svg>
        )}
      </button>
      {spawnMenuOpen && !spawning && createPortal(
        <div
          ref={spawnMenuRef}
          className={`spawn-dropdown__menu spawn-dropdown__menu--portal${placementAbove ? ' spawn-dropdown__menu--above' : ''}`}
          role="menu"
          style={{
            position: 'fixed',
            top: placementAbove ? 'auto' : `${menuPosition.top}px`,
            bottom: placementAbove ? `${window.innerHeight - menuPosition.top}px` : 'auto',
            left: `${menuPosition.left}px`,
          }}
        >
          <button
            className="spawn-dropdown__item"
            onClick={handleCustomSpawn}
            role="menuitem"
          >
            <span className="spawn-dropdown__item-label">Custom...</span>
            <span className="spawn-dropdown__item-hint">Open spawn wizard</span>
          </button>

          {quickLaunch.length > 0 && (
            <>
              <div className="spawn-dropdown__separator" role="separator"></div>
              {quickLaunch.map((name) => (
                <button
                  key={name}
                  className="spawn-dropdown__item"
                  onClick={(e) => handleQuickLaunchSpawn(name, e)}
                  role="menuitem"
                >
                  <span className="spawn-dropdown__item-label">{name}</span>
                </button>
              ))}
            </>
          )}

          {quickLaunch.length === 0 && (
            <div className="spawn-dropdown__empty">
              No quick launch presets
            </div>
          )}
        </div>,
        document.body
      )}
    </>
  );

  // Determine if we're showing the add button
  const showAddButton = workspace && !activeSpawnTab;

  return (
    <div className="session-tabs">
      {sessions.map((sess) => renderSessionTab(sess))}

      {/* Diff tab — always shown */}
      {renderDiffTab()}

      {/* Git tab — always shown */}
      {renderGitTab()}

      {/* Resolve conflict tab — shown when state exists */}
      {renderResolveConflictTab()}

      {/* Add button */}
      {showAddButton && renderAddButton()}

      {activeSpawnTab && (
        <div
          className={`session-tab session-tab--active${resolveInProgress ? ' session-tab--disabled' : ''}`}
          onClick={() => !resolveInProgress && handleSpawnTabClick()}
          role="button"
          tabIndex={resolveInProgress ? -1 : 0}
          style={resolveInProgress ? { opacity: 0.5, cursor: 'not-allowed' } : undefined}
        >
          <div className="session-tab__row1">
            <span className="session-tab__name">
              Spawning...
            </span>
          </div>
        </div>
      )}
    </div>
  );
}
