import React, { useState, useRef, useEffect } from 'react';
import { createPortal } from 'react-dom';
import { useNavigate, useLocation } from 'react-router-dom';
import { disposeSession, dismissLinearSyncResolveConflictState, getErrorMessage } from '../lib/api';
import {
  formatRelativeTime,
  formatTimestamp,
  nudgeStateEmoji,
  formatNudgeSummary,
  WorkingSpinner,
} from '../lib/utils';
import { useToast } from './ToastProvider';
import { useModal } from './ModalProvider';
import { useConfig } from '../contexts/ConfigContext';
import { useSessions } from '../contexts/SessionsContext';
import { useSyncState } from '../contexts/SyncContext';
import { useKeyboardMode } from '../contexts/KeyboardContext';
import Tooltip from './Tooltip';
import ActionDropdown from './ActionDropdown';
import type { SessionResponse, WorkspaceResponse } from '../lib/types';
import {
  DndContext,
  closestCenter,
  PointerSensor,
  useSensor,
  useSensors,
  type DragStartEvent,
  type DragEndEvent,
} from '@dnd-kit/core';
import { SortableContext, horizontalListSortingStrategy, useSortable } from '@dnd-kit/sortable';
import { CSS } from '@dnd-kit/utilities';
import { useTabOrder } from '../hooks/useTabOrder';

function SortableSessionTab({
  sess,
  isCurrent,
  disabled,
  isWorkingState,
  nudgePreviewElement,
  activityDisplay,
  onTabClick,
  onDispose,
}: {
  sess: SessionResponse;
  isCurrent: boolean;
  disabled: boolean;
  isWorkingState: boolean;
  nudgePreviewElement: React.ReactNode;
  activityDisplay: string;
  onTabClick: (id: string) => void;
  onDispose: (id: string, e: React.MouseEvent) => void;
}) {
  const { attributes, listeners, setNodeRef, transform, transition, isDragging } = useSortable({
    id: sess.id,
  });

  const style: React.CSSProperties = {
    transform: CSS.Transform.toString(transform),
    transition,
    ...(disabled ? { opacity: 0.5, cursor: 'not-allowed' } : {}),
  };

  return (
    <div
      ref={setNodeRef}
      {...attributes}
      {...listeners}
      className={`session-tab${isCurrent ? ' session-tab--active' : ''}${disabled ? ' session-tab--disabled' : ''}${isDragging ? ' session-tab--dragging' : ''}`}
      onClick={() => !disabled && onTabClick(sess.id)}
      role="button"
      tabIndex={disabled ? -1 : 0}
      onKeyDown={(e) => {
        if (disabled) return;
        if (e.key === 'Enter' || e.key === ' ') {
          e.preventDefault();
          onTabClick(sess.id);
        }
      }}
      style={style}
    >
      <div className="session-tab__row1">
        {isWorkingState && <WorkingSpinner />}
        <span className="session-tab__name">
          {sess.nickname || sess.xterm_title || sess.target}
        </span>
        <Tooltip
          content={
            !sess.running
              ? 'Session stopped'
              : sess.last_output_at
                ? formatTimestamp(sess.last_output_at)
                : 'Never'
          }
        >
          <span className="session-tab__activity">{activityDisplay}</span>
        </Tooltip>
        <Tooltip content="Dispose session" variant="warning">
          <button
            className="btn btn--sm btn--ghost btn--danger session-tab__dispose"
            onClick={(e) => !disabled && onDispose(sess.id, e)}
            aria-label={`Dispose ${sess.id}`}
            disabled={disabled}
          >
            <svg
              width="10"
              height="10"
              viewBox="0 0 24 24"
              fill="none"
              stroke="currentColor"
              strokeWidth="3"
              strokeLinecap="round"
            >
              <line x1="4" y1="4" x2="20" y2="20"></line>
              <line x1="20" y1="4" x2="4" y2="20"></line>
            </svg>
          </button>
        </Tooltip>
      </div>
      {nudgePreviewElement && <div className="session-tab__row2">{nudgePreviewElement}</div>}
    </div>
  );
}

type SessionTabsProps = {
  sessions: SessionResponse[];
  currentSessionId?: string;
  workspace?: WorkspaceResponse;
  activeDiffTab?: boolean;
  activeSpawnTab?: boolean;
  activeGitTab?: boolean;
  activePreviewId?: string;
  activeLinearSyncResolveConflictTab?: boolean;
};

export default function SessionTabs({
  sessions,
  currentSessionId,
  workspace,
  activeDiffTab,
  activeSpawnTab,
  activeGitTab,
  activePreviewId,
  activeLinearSyncResolveConflictTab,
}: SessionTabsProps) {
  const navigate = useNavigate();
  const location = useLocation();
  const { success, error: toastError } = useToast();
  const { alert, confirm } = useModal();
  const { config } = useConfig();
  const { waitForSession } = useSessions();
  const {
    linearSyncResolveConflictStates,
    clearLinearSyncResolveConflictState,
    workspaceLockStates,
  } = useSyncState();
  const { setContext, clearContext } = useKeyboardMode();

  // Spawn dropdown state
  const [spawnMenuOpen, setSpawnMenuOpen] = useState(false);
  const [menuPosition, setMenuPosition] = useState({ top: 0, left: 0 });
  const [placementAbove, setPlacementAbove] = useState(false);
  const spawnButtonRef = useRef<HTMLButtonElement | null>(null);
  const spawnMenuRef = useRef<HTMLDivElement | null>(null);
  const crState = workspace ? linearSyncResolveConflictStates[workspace.id] : undefined;
  const resolveInProgress = crState?.status === 'in_progress';
  const lockState = workspace ? workspaceLockStates[workspace.id] : undefined;
  const isLocked = resolveInProgress || lockState?.locked;

  // Desktop-only drag: match the CSS mobile breakpoint (768px)
  const [isDesktop, setIsDesktop] = useState(() =>
    typeof window !== 'undefined' ? window.innerWidth > 768 : true
  );
  useEffect(() => {
    const mql = window.matchMedia('(min-width: 769px)');
    const handler = (e: MediaQueryListEvent) => setIsDesktop(e.matches);
    mql.addEventListener('change', handler);
    setIsDesktop(mql.matches);
    return () => mql.removeEventListener('change', handler);
  }, []);

  const { orderedSessions, reorder, startDrag, endDrag } = useTabOrder(workspace?.id, sessions);
  const dragEnabled = isDesktop && !isLocked && !!workspace;

  const pointerSensor = useSensor(PointerSensor, {
    activationConstraint: { distance: 5 },
  });
  const sensors = useSensors(pointerSensor);

  const handleDragStart = (_event: DragStartEvent) => {
    startDrag();
  };

  const handleDragEnd = (event: DragEndEvent) => {
    const { active, over } = event;
    if (over && active.id !== over.id) {
      reorder(String(active.id), String(over.id));
    } else {
      endDrag();
    }
  };

  // VCS-specific UI should appear for workspaces with VCS support.
  // Local workspaces: show for git (default when vcs is omitted).
  // Remote workspaces: always show (backend handles VCS abstraction).
  const isRemote = Boolean(workspace?.remote_host_id);
  const isVCS =
    isRemote || !workspace?.vcs || workspace.vcs === 'git' || workspace.vcs === 'sapling';
  const isGit =
    !workspace?.vcs ||
    workspace.vcs === 'git' ||
    workspace.vcs === 'git-worktree' ||
    workspace.vcs === 'git-clone';

  // Calculate if we should show diff tab
  const linesAdded = workspace?.lines_added ?? 0;
  const linesRemoved = workspace?.lines_removed ?? 0;
  const filesChanged = workspace?.files_changed ?? 0;
  const hasChanges = filesChanged > 0 || linesAdded > 0 || linesRemoved > 0;

  // Calculate spawn menu position
  useEffect(() => {
    if (spawnMenuOpen && spawnButtonRef.current) {
      const rect = spawnButtonRef.current.getBoundingClientRect();
      const gap = 4;
      const edgePadding = 8;
      const estimatedMenuHeight = spawnMenuRef.current?.offsetHeight || 300;
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
  }, [spawnMenuOpen]);

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
    if (isLocked && spawnMenuOpen) {
      setSpawnMenuOpen(false);
    }
  }, [isLocked, spawnMenuOpen]);

  useEffect(() => {
    if (!workspace || !isLocked) return;
    const target = resolveInProgress ? `/resolve-conflict/${workspace.id}` : `/git/${workspace.id}`;
    if (location.pathname !== target) {
      navigate(target, { replace: true });
    }
  }, [workspace, isLocked, resolveInProgress, location.pathname, navigate]);

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

  const handlePreviewTabClick = (previewId: string) => {
    if (workspace) {
      navigate(`/preview/${workspace.id}/${previewId}`);
    }
  };

  const handleDispose = async (sessionId: string, event: React.MouseEvent) => {
    event.stopPropagation();

    const sess = sessions.find((s) => s.id === sessionId);
    if (sess?.status === 'disposing') return;
    let sessionDisplay = sessionId;
    if (sess?.nickname) {
      sessionDisplay = `${sess.nickname} (${sessionId})`;
    }

    const accepted = await confirm(`Dispose session ${sessionDisplay}?`, {
      danger: true,
    });
    if (!accepted) return;

    try {
      await disposeSession(sessionId);
      success('Session disposed');
    } catch (err) {
      alert('Dispose Failed', `Failed to dispose: ${getErrorMessage(err, 'Unknown error')}`);
    }
  };

  const handleTabClick = (sessionId: string) => {
    navigate(`/sessions/${sessionId}`);
  };

  const nudgenikEnabled = Boolean(config?.nudgenik?.target);

  // Helper to render a session tab
  const renderSessionTab = (sess: SessionResponse) => {
    const isCurrent = sess.id === currentSessionId;
    const displayName = sess.nickname || sess.xterm_title || sess.target;
    const disabled = isLocked || sess.status === 'disposing';

    // run_targets are command-only now; if not in run_targets, it's a model = promptable
    const isCommand = (config?.run_targets || []).some((t) => t.name === sess.target);
    const isPromptable = !isCommand;

    const nudgeSummary = formatNudgeSummary(sess.nudge_summary);

    // "Working" is an operational state, not an attention signal — show spinner
    // inline in row1 (left of name) to avoid reflow from row2 appearing/disappearing.
    const isWorkingState =
      sess.nudge_state === 'Working' ||
      (nudgenikEnabled && !sess.nudge_state && isPromptable && sess.running);

    const isIdleState = sess.nudge_state === 'Idle';

    // Show nudge indicators if there's a nudge_state (from signals or nudgenik)
    // Suppress for the currently focused session — the user is already looking at it
    let nudgePreviewElement: React.ReactNode = null;
    if (!isWorkingState && !isIdleState && !isCurrent) {
      const nudgeEmoji = sess.nudge_state ? nudgeStateEmoji[sess.nudge_state] || null : null;
      if (nudgeEmoji) {
        nudgePreviewElement = nudgeSummary
          ? `${nudgeEmoji} ${nudgeSummary}`
          : `${nudgeEmoji} ${sess.nudge_state}`;
      }
    }

    // Show "Stopped" for stopped sessions, otherwise show last activity time
    const activityDisplay = !sess.running
      ? 'Stopped'
      : sess.last_output_at
        ? formatRelativeTime(sess.last_output_at)
        : '-';

    if (dragEnabled) {
      return (
        <SortableSessionTab
          key={sess.id}
          sess={sess}
          isCurrent={isCurrent}
          disabled={!!disabled}
          isWorkingState={isWorkingState}
          nudgePreviewElement={nudgePreviewElement}
          activityDisplay={activityDisplay}
          onTabClick={handleTabClick}
          onDispose={handleDispose}
        />
      );
    }

    // Non-draggable fallback (mobile / locked) — keep existing inline JSX
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
            e.preventDefault();
            handleTabClick(sess.id);
          }
        }}
        style={disabled ? { opacity: 0.5, cursor: 'not-allowed' } : undefined}
      >
        <div className="session-tab__row1">
          {isWorkingState && <WorkingSpinner />}
          <span className="session-tab__name">{displayName}</span>
          <Tooltip
            content={
              !sess.running
                ? 'Session stopped'
                : sess.last_output_at
                  ? formatTimestamp(sess.last_output_at)
                  : 'Never'
            }
          >
            <span className="session-tab__activity">{activityDisplay}</span>
          </Tooltip>
          <Tooltip content="Dispose session" variant="warning">
            <button
              className="btn btn--sm btn--ghost btn--danger session-tab__dispose"
              onClick={(e) => !disabled && handleDispose(sess.id, e)}
              aria-label={`Dispose ${sess.id}`}
              disabled={disabled}
            >
              <svg
                width="10"
                height="10"
                viewBox="0 0 24 24"
                fill="none"
                stroke="currentColor"
                strokeWidth="3"
                strokeLinecap="round"
              >
                <line x1="4" y1="4" x2="20" y2="20"></line>
                <line x1="20" y1="4" x2="4" y2="20"></line>
              </svg>
            </button>
          </Tooltip>
        </div>
        {nudgePreviewElement && <div className="session-tab__row2">{nudgePreviewElement}</div>}
      </div>
    );
  };

  // Helper to render the diff tab (always shown)
  const renderDiffTab = () => (
    <div
      className={`session-tab session-tab--diff${activeDiffTab ? ' session-tab--active' : ''}${isLocked ? ' session-tab--disabled' : ''}`}
      onClick={() => !isLocked && handleDiffTabClick()}
      role="button"
      tabIndex={isLocked ? -1 : 0}
      data-tour="diff-tab"
      onKeyDown={(e) => {
        if (isLocked) return;
        if (e.key === 'Enter' || e.key === ' ') {
          e.preventDefault();
          handleDiffTabClick();
        }
      }}
      style={isLocked ? { opacity: 0.5, cursor: 'not-allowed' } : undefined}
    >
      <div className="session-tab__row1">
        <span className="session-tab__name">
          {filesChanged} file{filesChanged !== 1 ? 's' : ''} changed
        </span>
        {hasChanges && (
          <span className="session-tab__diff-stats">
            {linesAdded > 0 && <span className="text-success">+{linesAdded}</span>}
            {linesRemoved > 0 && (
              <span
                style={{
                  color: 'var(--color-error)',
                  marginLeft: linesAdded > 0 ? '4px' : '0',
                }}
              >
                -{linesRemoved}
              </span>
            )}
          </span>
        )}
      </div>
    </div>
  );

  // Helper to render the git tab
  const renderGitTab = () => (
    <div
      className={`session-tab session-tab--diff${activeGitTab ? ' session-tab--active' : ''}${isLocked ? ' session-tab--disabled' : ''}`}
      onClick={() => !isLocked && handleGitTabClick()}
      role="button"
      tabIndex={isLocked ? -1 : 0}
      data-tour="git-tab"
      onKeyDown={(e) => {
        if (isLocked) return;
        if (e.key === 'Enter' || e.key === ' ') {
          e.preventDefault();
          handleGitTabClick();
        }
      }}
      style={isLocked ? { opacity: 0.5, cursor: 'not-allowed' } : undefined}
    >
      <div className="session-tab__row1">
        <span className="session-tab__name">commit graph</span>
      </div>
    </div>
  );

  const renderPreviewTab = (preview: NonNullable<WorkspaceResponse['previews']>[number]) => {
    const isActive = activePreviewId === preview.id;
    const disabled = isLocked;
    const statusTitle =
      preview.status === 'degraded'
        ? preview.last_error || 'Upstream server unavailable'
        : `Preview ${preview.target_port}`;
    return (
      <div
        key={preview.id}
        className={`session-tab session-tab--diff${isActive ? ' session-tab--active' : ''}${disabled ? ' session-tab--disabled' : ''}`}
        onClick={() => !disabled && handlePreviewTabClick(preview.id)}
        role="button"
        tabIndex={disabled ? -1 : 0}
        title={statusTitle}
        onKeyDown={(e) => {
          if (disabled) return;
          if (e.key === 'Enter' || e.key === ' ') {
            e.preventDefault();
            handlePreviewTabClick(preview.id);
          }
        }}
        style={disabled ? { opacity: 0.5, cursor: 'not-allowed' } : undefined}
      >
        <div className="session-tab__row1">
          <span className="session-tab__name">web:{preview.target_port}</span>
          <span
            style={{
              marginLeft: 8,
              width: 8,
              height: 8,
              borderRadius: '50%',
              backgroundColor:
                preview.status === 'degraded' ? 'var(--color-warning)' : 'var(--color-success)',
              flexShrink: 0,
            }}
          />
        </div>
      </div>
    );
  };

  // Helper to render the resolve conflict tab (only when state exists)
  const renderResolveConflictTab = () => {
    if (!crState && !activeLinearSyncResolveConflictTab) return null;
    const hash = crState?.hash ? crState.hash.substring(0, 7) : '...';
    const isActive = crState ? crState.status === 'in_progress' : true;
    const isFailed = crState?.status === 'failed';
    const label = isActive
      ? 'Resolving conflict on'
      : isFailed
        ? 'Resolve conflict failed on'
        : 'Resolve conflict on';

    const handleDismissTab = async (event: React.MouseEvent) => {
      event.stopPropagation();
      if (!workspace) return;
      clearLinearSyncResolveConflictState(workspace.id);
      const firstSession = workspace.sessions?.[0];
      navigate(firstSession ? `/sessions/${firstSession.id}` : '/');
      try {
        await dismissLinearSyncResolveConflictState(workspace.id);
      } catch {
        // State will be cleared via next WS broadcast
      }
    };

    return (
      <div
        className={`session-tab session-tab--diff${activeLinearSyncResolveConflictTab ? ' session-tab--active' : ''}`}
        onClick={handleResolveConflictTabClick}
        role="button"
        tabIndex={0}
        onKeyDown={(e) => {
          if (e.key === 'Enter' || e.key === ' ') {
            e.preventDefault();
            handleResolveConflictTabClick();
          }
        }}
      >
        <div className="session-tab__row1">
          <span
            className="session-tab__name"
            style={{ display: 'flex', alignItems: 'center', gap: 6, flex: 1, minWidth: 0 }}
          >
            {isActive && (
              <div
                className="spinner spinner--small"
                style={{ width: 10, height: 10, borderWidth: 2, flexShrink: 0 }}
              />
            )}
            <span className="truncate">
              {label} {hash}
            </span>
          </span>
          {!isActive && (
            <Tooltip content="Dismiss" variant="warning">
              <button
                className="btn btn--sm btn--ghost btn--danger session-tab__dispose"
                onClick={handleDismissTab}
                aria-label="Dismiss conflict resolution"
              >
                <svg
                  width="10"
                  height="10"
                  viewBox="0 0 24 24"
                  fill="none"
                  stroke="currentColor"
                  strokeWidth="3"
                  strokeLinecap="round"
                >
                  <line x1="4" y1="4" x2="20" y2="20"></line>
                  <line x1="20" y1="4" x2="4" y2="20"></line>
                </svg>
              </button>
            </Tooltip>
          )}
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
          if (isLocked) return;
          e.stopPropagation();
          setSpawnMenuOpen(!spawnMenuOpen);
        }}
        disabled={isLocked}
        aria-expanded={spawnMenuOpen}
        aria-haspopup="menu"
        aria-label="Spawn new session"
        data-tour="session-tab-add"
        style={isLocked ? { opacity: 0.5, cursor: 'not-allowed' } : undefined}
      >
        <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2.5">
          <line x1="12" y1="5" x2="12" y2="19"></line>
          <line x1="5" y1="12" x2="19" y2="12"></line>
        </svg>
      </button>
      {spawnMenuOpen &&
        workspace &&
        createPortal(
          <div
            ref={spawnMenuRef}
            style={{
              position: 'fixed',
              top: placementAbove ? 'auto' : `${menuPosition.top}px`,
              bottom: placementAbove ? `${window.innerHeight - menuPosition.top}px` : 'auto',
              left: `${menuPosition.left}px`,
              zIndex: 9999,
            }}
          >
            <ActionDropdown
              workspace={workspace}
              onClose={() => setSpawnMenuOpen(false)}
              placementAbove={placementAbove}
            />
          </div>,
          document.body
        )}
    </>
  );

  // Determine if we're showing the add button
  const showAddButton = Boolean(workspace);

  return (
    <div className="session-tabs" data-tour="session-tabs">
      {/* Session tabs + add button (wrapped so mobile can reorder) */}
      <div className="session-tabs__main">
        {dragEnabled ? (
          <DndContext
            sensors={sensors}
            collisionDetection={closestCenter}
            onDragStart={handleDragStart}
            onDragEnd={handleDragEnd}
          >
            <SortableContext
              items={orderedSessions.map((s) => s.id)}
              strategy={horizontalListSortingStrategy}
            >
              {orderedSessions.map((sess) => renderSessionTab(sess))}
            </SortableContext>
          </DndContext>
        ) : (
          orderedSessions.map((sess) => renderSessionTab(sess))
        )}

        {activeSpawnTab && (
          <div
            className={`session-tab session-tab--active${isLocked ? ' session-tab--disabled' : ''}`}
            onClick={() => !isLocked && handleSpawnTabClick()}
            role="button"
            tabIndex={isLocked ? -1 : 0}
            style={isLocked ? { opacity: 0.5, cursor: 'not-allowed' } : undefined}
          >
            <div className="session-tab__row1">
              <span className="session-tab__name">Spawning...</span>
            </div>
          </div>
        )}

        {/* Add button */}
        {showAddButton && renderAddButton()}
      </div>

      {/* Spacer pushes accessory tabs to the right on desktop */}
      <div className="session-tabs__spacer" />

      {/* Accessory tabs: on mobile, CSS order moves these below the content pane */}
      <div className="session-tabs__accessory">
        {(workspace?.previews || []).map((preview) => renderPreviewTab(preview))}

        {isGit && renderResolveConflictTab()}

        {isGit && renderDiffTab()}

        {isGit && renderGitTab()}
      </div>
    </div>
  );
}
