import React, { useState, useRef, useEffect } from 'react';
import { createPortal } from 'react-dom';
import { useNavigate, useLocation } from 'react-router-dom';
import { disposeSession, closeTab, getErrorMessage } from '../lib/api';
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
import PastebinDropdown from './PastebinDropdown';
import type { SessionResponse, WorkspaceResponse } from '../lib/types';
import type { Tab } from '../lib/types.generated';
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
import { useAccessoryTabOrder } from '../hooks/useAccessoryTabOrder';

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

function SortableAccessoryTab({
  tab,
  isActive,
  badgeContent,
  onTabClick,
  onClose,
}: {
  tab: Tab;
  isActive: boolean;
  badgeContent: React.ReactNode;
  onTabClick: () => void;
  onClose?: (e: React.MouseEvent) => void;
}) {
  const { attributes, listeners, setNodeRef, transform, transition, isDragging } = useSortable({
    id: tab.id,
  });

  const style: React.CSSProperties = {
    transform: CSS.Transform.toString(transform),
    transition,
  };

  return (
    <div
      ref={setNodeRef}
      {...attributes}
      {...listeners}
      className={`session-tab session-tab--diff${isActive ? ' session-tab--active' : ''}${isDragging ? ' session-tab--dragging' : ''}`}
      onClick={onTabClick}
      role="button"
      tabIndex={0}
      data-tour={`${tab.kind}-tab`}
      onKeyDown={(e) => {
        if (e.key === 'Enter' || e.key === ' ') {
          e.preventDefault();
          onTabClick();
        }
      }}
      style={style}
    >
      <div className="session-tab__row1">
        <span className="session-tab__name">
          {tab.label.length > 20 ? tab.label.slice(0, 17) + '…' : tab.label}
        </span>
        {badgeContent}
        {tab.closable && onClose && (
          <Tooltip content="Close tab" variant="warning">
            <button
              className="btn btn--sm btn--ghost btn--danger session-tab__dispose"
              onClick={onClose}
              aria-label={`Close ${tab.label}`}
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
}

type SessionTabsProps = {
  sessions: SessionResponse[];
  currentSessionId?: string;
  workspace?: WorkspaceResponse;
  activeSpawnTab?: boolean;
  activeGitTab?: boolean;
  activePreviewId?: string;
  activeLinearSyncResolveConflictTab?: boolean;
  onPaste?: (content: string) => void;
};

export default function SessionTabs({
  sessions,
  currentSessionId,
  workspace,
  activeSpawnTab,
  activeGitTab,
  activePreviewId,
  activeLinearSyncResolveConflictTab,
  onPaste,
}: SessionTabsProps) {
  const navigate = useNavigate();
  const location = useLocation();
  const { success, error: toastError } = useToast();
  const { alert, confirm } = useModal();
  const { config } = useConfig();
  const { waitForSession } = useSessions();
  const { workspaceLockStates, linearSyncResolveConflictStates } = useSyncState();
  const { setContext, clearContext, registerAction, unregisterAction } = useKeyboardMode();

  // Spawn dropdown state
  const [spawnMenuOpen, setSpawnMenuOpen] = useState(false);
  const [menuPosition, setMenuPosition] = useState({ top: 0, left: 0 });
  const [placementAbove, setPlacementAbove] = useState(false);
  const spawnButtonRef = useRef<HTMLButtonElement | null>(null);
  const spawnMenuRef = useRef<HTMLDivElement | null>(null);

  // Pastebin dropdown state
  const [pastebinOpen, setPastebinOpen] = useState(false);
  const [pastebinMenuPosition, setPastebinMenuPosition] = useState({ top: 0, left: 0 });
  const [pastebinPlacementAbove, setPastebinPlacementAbove] = useState(false);
  const pastebinButtonRef = useRef<HTMLButtonElement | null>(null);
  const pastebinMenuRef = useRef<HTMLDivElement | null>(null);
  const crState = workspace ? linearSyncResolveConflictStates[workspace.id] : undefined;
  const resolveInProgress = crState?.status === 'in_progress';
  const lockState = workspace ? workspaceLockStates[workspace.id] : undefined;
  const isLocked = resolveInProgress || lockState?.locked;
  const pastebinEntries = config?.pastebin || [];

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
  const {
    orderedTabs: orderedAccessoryTabs,
    reorder: reorderAccessory,
    startDrag: startAccessoryDrag,
    endDrag: endAccessoryDrag,
  } = useAccessoryTabOrder(workspace?.id, workspace?.tabs || []);
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

  // Calculate pastebin menu position
  useEffect(() => {
    if (pastebinOpen && pastebinButtonRef.current) {
      const rect = pastebinButtonRef.current.getBoundingClientRect();
      const gap = 4;
      const edgePadding = 8;
      const estimatedMenuHeight = pastebinMenuRef.current?.offsetHeight || 300;
      const spaceBelow = window.innerHeight - rect.bottom - gap;
      const spaceAbove = rect.top - gap;
      const shouldPlaceAbove = spaceBelow < estimatedMenuHeight && spaceAbove > spaceBelow;
      setPastebinPlacementAbove(shouldPlaceAbove);

      let left = rect.left;
      const menuWidth = pastebinMenuRef.current?.offsetWidth;
      if (menuWidth) {
        const rightEdge = left + menuWidth;
        if (rightEdge > window.innerWidth - edgePadding) {
          left = window.innerWidth - menuWidth - edgePadding;
        }
      }

      if (shouldPlaceAbove) {
        setPastebinMenuPosition({ top: rect.top - gap, left });
      } else {
        setPastebinMenuPosition({ top: rect.bottom + gap, left });
      }
    }
  }, [pastebinOpen]);

  // Close pastebin menu when clicking outside
  useEffect(() => {
    const handleClickOutside = (event: MouseEvent) => {
      const target = event.target as Node | null;
      if (!target) return;
      if (pastebinButtonRef.current?.contains(target)) return;
      if (pastebinMenuRef.current?.contains(target)) return;
      setPastebinOpen(false);
    };

    if (pastebinOpen) {
      document.addEventListener('mousedown', handleClickOutside);
    }
    return () => {
      document.removeEventListener('mousedown', handleClickOutside);
    };
  }, [pastebinOpen]);

  useEffect(() => {
    if (isLocked && spawnMenuOpen) {
      setSpawnMenuOpen(false);
    }
    if (isLocked && pastebinOpen) {
      setPastebinOpen(false);
    }
  }, [isLocked, spawnMenuOpen, pastebinOpen]);

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

  // Register W keyboard action: dispose active session or close active closable accessory tab
  useEffect(() => {
    if (!workspace) return;
    const scope = { type: 'workspace', id: workspace.id } as const;

    const handleCloseTab = async () => {
      if (!workspace) return;

      // If a session is active, dispose it (with confirmation)
      if (currentSessionId) {
        const sess = sessions.find((s) => s.id === currentSessionId);
        if (sess?.status === 'disposing') return;
        const sessionDisplay = sess?.nickname
          ? `${sess.nickname} (${currentSessionId})`
          : currentSessionId;
        const accepted = await confirm(`Dispose session ${sessionDisplay}?`, { danger: true });
        if (!accepted) return;
        try {
          await disposeSession(currentSessionId);
          success('Session disposed');
        } catch (err) {
          alert('Dispose Failed', `Failed to dispose: ${getErrorMessage(err, 'Unknown error')}`);
        }
        return;
      }

      // Otherwise, close the active closable accessory tab
      const activeTab = (workspace.tabs || []).find(
        (t) =>
          t.closable &&
          (location.pathname === t.route || location.pathname.startsWith(t.route + '/'))
      );
      if (!activeTab) return;
      try {
        await closeTab(workspace.id, activeTab.id);
        const firstSession = workspace.sessions?.[0];
        navigate(firstSession ? `/sessions/${firstSession.id}` : '/');
      } catch (err) {
        toastError(`Failed to close tab: ${getErrorMessage(err, 'Unknown error')}`);
      }
    };

    registerAction({
      key: 'w',
      description: currentSessionId ? 'Dispose session' : 'Close tab',
      handler: handleCloseTab,
      scope,
    });

    return () => unregisterAction('w', false, scope);
  }, [
    workspace,
    currentSessionId,
    sessions,
    location.pathname,
    registerAction,
    unregisterAction,
    confirm,
    success,
    alert,
    toastError,
    navigate,
  ]);

  const handleSpawnTabClick = () => {
    if (workspace) {
      navigate(`/spawn?workspace_id=${workspace.id}`);
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

  // Compute per-tab derived data for accessory tabs
  const getAccessoryTabProps = (tab: Tab) => {
    // A tab is active if the URL matches its route, but only if no other tab
    // has a longer (more specific) route that also matches.
    const allTabs = workspace?.tabs || [];
    const pathMatches = (route: string) =>
      location.pathname === route || location.pathname.startsWith(route + '/');
    const hasMoreSpecificMatch =
      pathMatches(tab.route) &&
      allTabs.some(
        (other) =>
          other.id !== tab.id && pathMatches(other.route) && other.route.length > tab.route.length
      );
    const isActive = pathMatches(tab.route) && !hasMoreSpecificMatch;

    const handleClose = tab.closable
      ? async (event: React.MouseEvent) => {
          event.stopPropagation();
          if (!workspace) return;
          try {
            await closeTab(workspace.id, tab.id);
            if (isActive) {
              const firstSession = workspace.sessions?.[0];
              navigate(firstSession ? `/sessions/${firstSession.id}` : '/');
            }
          } catch (err) {
            toastError(`Failed to close tab: ${getErrorMessage(err, 'Unknown error')}`);
          }
        }
      : undefined;

    let badgeContent: React.ReactNode = null;
    if (tab.kind === 'diff') {
      const linesAdded = workspace?.lines_added ?? 0;
      const linesRemoved = workspace?.lines_removed ?? 0;
      if (linesAdded > 0 || linesRemoved > 0) {
        badgeContent = (
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
        );
      }
    } else if (tab.kind === 'preview') {
      const status = tab.meta?.status;
      badgeContent = (
        <span
          style={{
            marginLeft: 8,
            width: 8,
            height: 8,
            borderRadius: '50%',
            backgroundColor:
              status === 'degraded' ? 'var(--color-warning)' : 'var(--color-success)',
            flexShrink: 0,
          }}
        />
      );
    } else if (tab.kind === 'resolve-conflict' && tab.meta?.status === 'in_progress') {
      badgeContent = (
        <div
          className="spinner spinner--small"
          style={{ width: 10, height: 10, borderWidth: 2, flexShrink: 0, marginLeft: 6 }}
        />
      );
    }

    return { isActive, badgeContent, handleClose, handleClick: () => navigate(tab.route) };
  };

  // Render an accessory tab without sortable wrapping (used in non-DnD mode)
  const renderAccessoryTab = (tab: Tab) => {
    const { isActive, badgeContent, handleClose, handleClick } = getAccessoryTabProps(tab);
    return (
      <div
        key={tab.id}
        className={`session-tab session-tab--diff${isActive ? ' session-tab--active' : ''}`}
        onClick={handleClick}
        role="button"
        tabIndex={0}
        data-tour={`${tab.kind}-tab`}
        onKeyDown={(e) => {
          if (e.key === 'Enter' || e.key === ' ') {
            e.preventDefault();
            handleClick();
          }
        }}
      >
        <div className="session-tab__row1">
          <span className="session-tab__name">
            {tab.label.length > 20 ? tab.label.slice(0, 17) + '…' : tab.label}
          </span>
          {badgeContent}
          {tab.closable && handleClose && (
            <Tooltip content="Close tab" variant="warning">
              <button
                className="btn btn--sm btn--ghost btn--danger session-tab__dispose"
                onClick={handleClose}
                aria-label={`Close ${tab.label}`}
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

  // Helper to render the pastebin button
  const renderPastebinButton = () => {
    const hasActiveSession = Boolean(currentSessionId);
    const disabled = !hasActiveSession;

    return (
      <>
        <button
          ref={pastebinButtonRef}
          className="session-tab--add"
          onClick={(e) => {
            if (isLocked || disabled) return;
            e.stopPropagation();
            setPastebinOpen(!pastebinOpen);
          }}
          disabled={isLocked || disabled}
          aria-expanded={pastebinOpen}
          aria-haspopup="menu"
          aria-label="Pastebin"
          style={disabled || isLocked ? { opacity: 0.5, cursor: 'not-allowed' } : undefined}
        >
          <svg viewBox="0 0 256 256" fill="currentColor" width="20" height="20">
            <path
              d="M47.174 74.383c0-9.94 8.067-17.87 17.997-17.712l21.398.34s4.011-10.206 17.826-10.206c13.814 0 16.6 9.58 16.6 9.58h23.35c8.841 0 16.066 7.169 16.137 16.007l.123 15.5 11.998-.08c2.206-.015 5.354 1.143 7.02 2.577l27.06 23.28c1.671 1.439 3.026 4.403 3.026 6.608v85.158a3.984 3.984 0 0 1-4.005 3.985l-106.865-.5a4.98 4.98 0 0 1-4.953-5.025l.247-27.695-30.963.195A15.87 15.87 0 0 1 47.174 160.5V74.383zM104 72c4.418 0 8-3.134 8-7s-3.582-7-8-7-8 3.134-8 7 3.582 7 8 7zm-39.622 2.883l1.193 82.848a2.004 2.004 0 0 0 2.037 1.973l23.907-.298a2.043 2.043 0 0 0 2.017-2.023l.23-53.884a15.692 15.692 0 0 1 16.06-15.678l32.068.64a1.975 1.975 0 0 0 2.014-1.966l.12-11.571a2.042 2.042 0 0 0-1.986-2.051l-20.593-.477s-4.914 10.667-17.418 10.667-17.133-11.101-17.133-11.101l-20.544.84a2.067 2.067 0 0 0-1.972 2.08zm107.671 33.413c-1.674-1.437-2.943-.822-2.833 1.401l.727 14.677c.11 2.21 1.986 4.01 4.198 4.017l17.418.062c2.209.008 2.647-1.146.968-2.587l-20.478-17.57zm-59.97-3.415l-.326 88.575 80.378.57V144.46h-32.189c-4.418 0-8.019-3.575-8.041-8.007l-.167-32.275-39.656.704z"
              fillRule="evenodd"
            />
          </svg>
        </button>
        {pastebinOpen &&
          createPortal(
            <div
              ref={pastebinMenuRef}
              style={{
                position: 'fixed',
                top: pastebinPlacementAbove ? 'auto' : `${pastebinMenuPosition.top}px`,
                bottom: pastebinPlacementAbove
                  ? `${window.innerHeight - pastebinMenuPosition.top}px`
                  : 'auto',
                left: `${pastebinMenuPosition.left}px`,
                zIndex: 9999,
              }}
            >
              <PastebinDropdown
                entries={pastebinEntries}
                onPaste={(content) => onPaste?.(content)}
                onClose={() => setPastebinOpen(false)}
                disabled={disabled}
              />
            </div>,
            document.body
          )}
      </>
    );
  };

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
        {showAddButton && renderPastebinButton()}
      </div>

      {/* Spacer pushes accessory tabs to the right on desktop */}
      <div className="session-tabs__spacer" />

      {/* Accessory tabs: on mobile, CSS order moves these below the content pane */}
      <div className="session-tabs__accessory">
        {dragEnabled ? (
          <DndContext
            sensors={sensors}
            collisionDetection={closestCenter}
            onDragStart={() => startAccessoryDrag()}
            onDragEnd={(event: DragEndEvent) => {
              const { active, over } = event;
              if (over && active.id !== over.id) {
                reorderAccessory(String(active.id), String(over.id));
              } else {
                endAccessoryDrag();
              }
            }}
          >
            <SortableContext
              items={orderedAccessoryTabs.map((t) => t.id)}
              strategy={horizontalListSortingStrategy}
            >
              {orderedAccessoryTabs.map((tab) => {
                const { isActive, badgeContent, handleClose, handleClick } =
                  getAccessoryTabProps(tab);
                return (
                  <SortableAccessoryTab
                    key={tab.id}
                    tab={tab}
                    isActive={isActive}
                    badgeContent={badgeContent}
                    onTabClick={handleClick}
                    onClose={handleClose}
                  />
                );
              })}
            </SortableContext>
          </DndContext>
        ) : (
          orderedAccessoryTabs.map((tab) => renderAccessoryTab(tab))
        )}
      </div>
    </div>
  );
}
