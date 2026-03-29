import React, { useEffect, useMemo, useRef, useState } from 'react';
import { NavLink, Outlet, useNavigate, useParams, useLocation } from 'react-router-dom';
import useTheme from '../hooks/useTheme';
import useVersionInfo from '../hooks/useVersionInfo';
import useLocalStorage from '../hooks/useLocalStorage';
import Tooltip from './Tooltip';
import KeyboardModeIndicator from './KeyboardModeIndicator';
import TypingPerformance from './TypingPerformance';
import CurationStatus from './CurationStatus';
import TmuxDiagnostic from './TmuxDiagnostic';
import EventMonitor from './EventMonitor';
import ConnectionProgressModal from './ConnectionProgressModal';
import { useConfig } from '../contexts/ConfigContext';
import { useSessions } from '../contexts/SessionsContext';
import { useSyncState } from '../contexts/SyncContext';
import { useOverlay } from '../contexts/OverlayContext';
import { useRemoteAccess } from '../contexts/RemoteAccessContext';
import { useKeyboardMode } from '../contexts/KeyboardContext';
import { useHelpModal } from './KeyboardHelpModal';
import { useSync } from '../hooks/useSync';
import {
  formatRelativeTime,
  nudgeStateEmoji,
  formatNudgeSummary,
  WorkingSpinner,
  isRemoteClient,
} from '../lib/utils';
import { sortSessionsByTabOrder, TAB_ORDER_CHANGED_EVENT } from '../lib/tabOrder';
import { navigateToWorkspace, findNextWorkspaceWithSessions } from '../lib/navigation';
import { useModal } from './ModalProvider';
import { useToast } from './ToastProvider';
import {
  disposeWorkspace,
  getErrorMessage,
  openVSCode,
  reconnectRemoteHost,
  getDevStatus,
  devRebuild,
  getLoreProposals,
  type DevStatus,
} from '../lib/api';
import type { WorkspaceResponse } from '../lib/types';
import RemoteAccessPanel from './RemoteAccessPanel';
import ToolsSection from './ToolsSection';
import { useFeatures } from '../contexts/FeaturesContext';

const NAV_COLLAPSED_KEY = 'schmux-nav-collapsed';
const WORKSPACE_SORT_KEY = 'schmux-workspace-sort';
type WorkspaceSortMode = 'alpha' | 'time';

export default function AppShell() {
  const { toggleTheme } = useTheme();
  const { isNotConfigured, config, getRepoName } = useConfig();
  const { versionInfo } = useVersionInfo();
  const { workspaces, connected, sessionsById } = useSessions();
  const {
    linearSyncResolveConflictStates,
    workspaceLockStates,
    syncResultEvents,
    clearSyncResultEvents,
  } = useSyncState();
  const { overlayUnreadCount, markOverlaysRead } = useOverlay();
  const { features } = useFeatures();
  const { remoteAccessStatus, simulateRemote } = useRemoteAccess();
  const navigate = useNavigate();
  const location = useLocation();
  const { sessionId } = useParams();
  const [navCollapsed, setNavCollapsed] = useLocalStorage(NAV_COLLAPSED_KEY, false);
  const [workspaceSort, setWorkspaceSort] = useState<WorkspaceSortMode>(() => {
    return (localStorage.getItem(WORKSPACE_SORT_KEY) as WorkspaceSortMode) || 'alpha';
  });
  const { mode, registerAction, unregisterAction, context } = useKeyboardMode();
  const { show: showHelp } = useHelpModal();
  const { alert, confirm, show } = useModal();
  const { success, error: toastError } = useToast();
  const { startConflictResolution } = useSync();
  const syncResultProcessingRef = useRef(false);

  // State for reconnect modal (used by sidebar Reconnect button)
  const [reconnectModal, setReconnectModal] = useState<{
    hostId: string;
    flavorId: string;
    displayName: string;
    provisioningSessionId: string | null;
  } | null>(null);

  // Ref for scrolling active workspace into view
  const activeWorkspaceRef = useRef<HTMLDivElement | null>(null);

  // Debounce workspace sort during keyboard navigation:
  // Freeze the sort order for 2s after the last Cmd+Up/Down keypress
  const navSnapshotRef = useRef<WorkspaceResponse[] | null>(null);
  const navTimerRef = useRef<ReturnType<typeof setTimeout> | null>(null);

  // Bump to force re-render when tab order changes (via drag in SessionTabs)
  const [, setTabOrderVersion] = useState(0);
  useEffect(() => {
    const handleTabOrderChanged = () => setTabOrderVersion((v) => v + 1);
    window.addEventListener(TAB_ORDER_CHANGED_EVENT, handleTabOrderChanged);
    return () => window.removeEventListener(TAB_ORDER_CHANGED_EVENT, handleTabOrderChanged);
  }, []);

  // Dev mode state
  const isDevMode = !!versionInfo?.dev_mode;
  const isRemoteAccess = isRemoteClient() || simulateRemote;
  const [devStatus, setDevStatus] = useState<DevStatus | null>(null);
  const [devRebuilding, setDevRebuilding] = useState(false);
  const [devRebuildTarget, setDevRebuildTarget] = useState<string | null>(null);
  const [devRebuildPhase, setDevRebuildPhase] = useState<'building' | 'restarting' | null>(
    'building'
  );

  // Persist workspace sort preference
  useEffect(() => {
    localStorage.setItem(WORKSPACE_SORT_KEY, workspaceSort);
  }, [workspaceSort]);

  // Sort workspaces based on current preference
  const sortedWorkspaces = useMemo(() => {
    if (!workspaces) return workspaces;

    const sorted = [...workspaces];

    if (workspaceSort === 'alpha') {
      sorted.sort((a, b) => {
        const repoA = a.repo_name || getRepoName(a.repo);
        const repoB = b.repo_name || getRepoName(b.repo);
        if (repoA !== repoB) return repoA.localeCompare(repoB);
        return a.branch.localeCompare(b.branch);
      });
    } else {
      // Time sort: most recent session activity first
      sorted.sort((a, b) => {
        const getTime = (ws: WorkspaceResponse): number => {
          const times =
            ws.sessions
              ?.filter((s) => s.last_output_at)
              .map((s) => new Date(s.last_output_at!).getTime()) || [];
          return times.length > 0 ? Math.max(...times) : 0;
        };
        const timeA = getTime(a);
        const timeB = getTime(b);
        // Most recent first, workspaces with no sessions go to bottom
        if (timeA === 0 && timeB === 0) {
          const repoA = a.repo_name || getRepoName(a.repo);
          const repoB = b.repo_name || getRepoName(b.repo);
          if (repoA !== repoB) return repoA.localeCompare(repoB);
          return a.branch.localeCompare(b.branch);
        }
        if (timeA === 0) return 1;
        if (timeB === 0) return -1;
        if (timeA !== timeB) return timeB - timeA;
        // Equal timestamps: secondary sort alphabetically
        const repoA = a.repo_name || getRepoName(a.repo);
        const repoB = b.repo_name || getRepoName(b.repo);
        if (repoA !== repoB) return repoA.localeCompare(repoB);
        return a.branch.localeCompare(b.branch);
      });
    }

    return sorted;
  }, [workspaces, workspaceSort, getRepoName]);

  useEffect(() => {
    if (syncResultProcessingRef.current || syncResultEvents.length === 0) return;
    syncResultProcessingRef.current = true;

    const process = async () => {
      for (const event of syncResultEvents) {
        if (event.conflicting_hash) {
          const commitCount = event.success_count ?? 0;
          const resolveConfirmed = await show(
            'Unable to fully sync',
            `We were able to fast forward ${commitCount} commits cleanly. You can have an agent resolve the conflict at ${event.conflicting_hash}.`,
            {
              confirmText: 'Resolve',
              cancelText: 'Close',
              danger: true,
            }
          );
          if (resolveConfirmed) {
            await startConflictResolution(event.workspace_id);
          }
        } else if (event.success) {
          const branch = event.branch || 'main';
          const count = event.success_count ?? 0;
          success(`Synced ${count} commit${count === 1 ? '' : 's'} from ${branch}.`);
        } else {
          await alert('Error', event.message || 'Failed to sync from main');
        }
      }
      clearSyncResultEvents();
    };

    process().finally(() => {
      syncResultProcessingRef.current = false;
    });
  }, [syncResultEvents, clearSyncResultEvents, alert, show, startConflictResolution]);

  useEffect(() => {
    if (!isDevMode) return;
    getDevStatus()
      .then(setDevStatus)
      .catch(() => {});
  }, [isDevMode, connected]);

  // Lore pending proposal counts
  const [loreCounts, setLoreCounts] = useState<Record<string, number>>({});
  const repoNamesKey = useMemo(
    () => (config?.repos || []).map((r) => r.name).join(','),
    [config?.repos]
  );

  useEffect(() => {
    if (!repoNamesKey) return;
    const repoNames = repoNamesKey.split(',');
    const fetchCounts = async () => {
      const results = await Promise.allSettled(repoNames.map((name) => getLoreProposals(name)));
      const counts: Record<string, number> = {};
      results.forEach((result, i) => {
        if (result.status === 'fulfilled') {
          counts[repoNames[i]] = (result.value.proposals || []).filter(
            (p) => p.status === 'pending'
          ).length;
        }
      });
      setLoreCounts(counts);
    };
    fetchCounts();
  }, [repoNamesKey]);

  const totalLorePending = useMemo(
    () => Object.values(loreCounts).reduce((sum, n) => sum + n, 0),
    [loreCounts]
  );

  // Identify which workspaces are dev-eligible (same repo as source)
  const devSourceWorkspace = workspaces?.find((ws) => ws.path === devStatus?.source_workspace);
  const devSourceRepo = devSourceWorkspace?.repo;

  const handleDevRebuild = async (workspaceId: string, type: 'frontend' | 'backend' | 'both') => {
    try {
      setDevRebuilding(true);
      setDevRebuildTarget(workspaceId);
      setDevRebuildPhase('building');
      await devRebuild(workspaceId, type);
      // API responded — daemon is about to exit, now waiting for restart
      setDevRebuildPhase('restarting');
    } catch (err) {
      setDevRebuilding(false);
      setDevRebuildTarget(null);
      setDevRebuildPhase(null);
      toastError(err instanceof Error ? err.message : 'Rebuild failed');
    }
  };

  // Reset rebuilding state when we reconnect after a rebuild.
  // Track whether we went through a disconnection to avoid clearing
  // the dialog immediately (connected is still true when the handler first sets state).
  const devSawDisconnect = useRef(false);
  useEffect(() => {
    if (devRebuilding && !connected) {
      devSawDisconnect.current = true;
    }
    if (devRebuilding && connected && devSawDisconnect.current) {
      devSawDisconnect.current = false;
      setDevRebuilding(false);
      setDevRebuildTarget(null);
    }
  }, [connected, devRebuilding]);

  // Check if we're on a workspace-scoped page
  const diffMatch = location.pathname.match(/^\/diff\/(.+)$/);
  const previewMatch = location.pathname.match(/^\/preview\/([^\/]+)\/([^\/]+)$/);
  const resolveConflictMatch = location.pathname.match(/^\/resolve-conflict\/([^\/]+)$/);
  const gitMatch = location.pathname.match(/^\/git\/([^\/]+)$/);
  const spawnWorkspaceId =
    location.pathname === '/spawn'
      ? new URLSearchParams(location.search).get('workspace_id')
      : null;
  const activeWorkspaceId =
    diffMatch?.[1] ??
    previewMatch?.[1] ??
    resolveConflictMatch?.[1] ??
    gitMatch?.[1] ??
    spawnWorkspaceId ??
    null;

  // Check if we're on a session detail page and get workspace info
  const sessionMatch = location.pathname.match(/^\/sessions\/([^\/]+)$/);
  const currentSession = sessionMatch && sessionId ? sessionsById[sessionId] : null;
  const currentWorkspaceId = currentSession?.workspace_id || activeWorkspaceId || previewMatch?.[1];
  const currentWorkspace = currentWorkspaceId
    ? workspaces?.find((ws) => ws.id === currentWorkspaceId)
    : null;

  const showUpdateBadge = features.update && versionInfo?.update_available;
  const nudgenikEnabled = Boolean(config?.nudgenik?.target);

  const handleWorkspaceClick = (workspaceId: string) => {
    const ws = workspaces?.find((w) => w.id === workspaceId);
    if (ws?.status === 'disposing') return;
    navigateToWorkspace(navigate, workspaces || [], workspaceId);
  };

  const handleSessionClick = (sessId: string) => {
    navigate(`/sessions/${sessId}`);
  };

  // Scroll active workspace into view when it changes or sort order reshuffles
  useEffect(() => {
    if (activeWorkspaceRef.current) {
      activeWorkspaceRef.current.scrollIntoView({
        behavior: 'smooth',
        block: 'nearest',
      });
    }
  }, [currentWorkspaceId, activeWorkspaceId, sortedWorkspaces]);

  // Register global keyboard actions (always available)
  useEffect(() => {
    // N - context-aware spawn (workspace-specific when available)
    registerAction({
      key: 'n',
      description: 'Spawn new session (context-aware)',
      handler: () => {
        if (context.workspaceId) {
          navigate(`/spawn?workspace_id=${context.workspaceId}`);
        } else {
          navigate('/spawn');
        }
      },
      scope: { type: 'global' },
    });

    // Shift+N - always general spawn
    registerAction({
      key: 'n',
      shiftKey: true,
      description: 'Spawn new session (always general)',
      handler: () => navigate('/spawn'),
      scope: { type: 'global' },
    });

    // ? - show help modal
    registerAction({
      key: '?',
      description: 'Show keyboard shortcuts help',
      handler: () => showHelp(),
      scope: { type: 'global' },
    });

    // H - go home
    registerAction({
      key: 'h',
      description: 'Go to home',
      handler: () => navigate('/'),
      scope: { type: 'global' },
    });

    return () => {
      unregisterAction('n');
      unregisterAction('n', true);
      unregisterAction('?');
      unregisterAction('h');
    };
  }, [registerAction, unregisterAction, navigate, showHelp, context.workspaceId]);

  // Deprecation notice for Cmd+K (changed to Cmd+/)
  useEffect(() => {
    const handleKeyDown = (e: KeyboardEvent) => {
      if ((e.metaKey || e.ctrlKey) && e.key === 'k' && !e.shiftKey) {
        e.preventDefault();
        const modKey = e.metaKey ? '⌘' : 'Ctrl';
        success(`${modKey}+K has been changed to ${modKey}+/`, 5000);
      }
    };

    window.addEventListener('keydown', handleKeyDown);
    return () => window.removeEventListener('keydown', handleKeyDown);
  }, [success]);

  // beforeunload prevents accidental tab close (Cmd+W, browser X button, etc.)
  const confirmBeforeClose = config?.notifications?.confirm_before_close ?? false;
  useEffect(() => {
    if (!confirmBeforeClose) return;

    const handleBeforeUnload = (e: BeforeUnloadEvent) => {
      e.preventDefault();
    };

    window.addEventListener('beforeunload', handleBeforeUnload);
    return () => window.removeEventListener('beforeunload', handleBeforeUnload);
  }, [confirmBeforeClose]);

  // Direct Cmd+Arrow shortcuts for navigation (no keyboard mode required)
  useEffect(() => {
    const handleKeyDown = (e: KeyboardEvent) => {
      // Only handle when Cmd/Ctrl is pressed with arrow keys
      if (!e.metaKey && !e.ctrlKey) return;
      if (!['ArrowLeft', 'ArrowRight', 'ArrowUp', 'ArrowDown'].includes(e.key)) return;

      // Build ordered list of tab routes for cycling: [sessions..., diff, git]
      if (e.key === 'ArrowLeft' || e.key === 'ArrowRight') {
        if (!context.workspaceId) return;
        const workspace = workspaces?.find((ws) => ws.id === context.workspaceId);
        if (!workspace) return;
        const isVCS = !workspace.vcs || workspace.vcs === 'git';

        const tabs: string[] = sortSessionsByTabOrder(workspace.id, workspace.sessions || []).map(
          (s) => `/sessions/${s.id}`
        );
        (workspace.previews || []).forEach((p) => tabs.push(`/preview/${workspace.id}/${p.id}`));
        if (isVCS) {
          tabs.push(`/diff/${workspace.id}`);
          tabs.push(`/git/${workspace.id}`);
        }
        if (tabs.length <= 1) return;

        const currentIndex = tabs.indexOf(location.pathname);
        if (currentIndex === -1) return;

        const delta = e.key === 'ArrowRight' ? 1 : -1;
        const nextIndex = (currentIndex + delta + tabs.length) % tabs.length;

        e.preventDefault();
        navigate(tabs[nextIndex]);
        return;
      }

      // Cmd+Up / Cmd+Down: Navigate workspaces using a frozen sort snapshot
      // to prevent reshuffling mid-navigation (especially in time-sort mode).
      if (e.key === 'ArrowUp' || e.key === 'ArrowDown') {
        if (!sortedWorkspaces?.length) return;

        // Snapshot the current sort order on first keypress; hold for 2s after last
        if (!navSnapshotRef.current) {
          navSnapshotRef.current = sortedWorkspaces;
        }
        if (navTimerRef.current) clearTimeout(navTimerRef.current);
        navTimerRef.current = setTimeout(() => {
          navSnapshotRef.current = null;
          navTimerRef.current = null;
        }, 2000);

        const frozen = navSnapshotRef.current;

        if (e.key === 'ArrowUp') {
          const currentIndex = context.workspaceId
            ? frozen.findIndex((ws) => ws.id === context.workspaceId)
            : -1;
          if (currentIndex <= 0) return; // Already at first or not in a workspace

          const targetIndex = findNextWorkspaceWithSessions(frozen, currentIndex, -1);
          if (targetIndex === -1) return;

          e.preventDefault();
          navigateToWorkspace(navigate, frozen, frozen[targetIndex].id);
          return;
        }

        // ArrowDown
        const currentIndex = context.workspaceId
          ? frozen.findIndex((ws) => ws.id === context.workspaceId)
          : -1;

        // If not in any workspace, find first workspace with sessions
        if (currentIndex === -1) {
          const targetIndex = findNextWorkspaceWithSessions(frozen, -1, 1);
          if (targetIndex === -1) return;

          e.preventDefault();
          navigateToWorkspace(navigate, frozen, frozen[targetIndex].id);
          return;
        }

        const targetIndex = findNextWorkspaceWithSessions(frozen, currentIndex, 1);
        if (targetIndex === -1) return;

        e.preventDefault();
        navigateToWorkspace(navigate, frozen, frozen[targetIndex].id);
        return;
      }
    };

    window.addEventListener('keydown', handleKeyDown, { capture: true });
    return () => {
      window.removeEventListener('keydown', handleKeyDown, { capture: true });
      if (navTimerRef.current) {
        clearTimeout(navTimerRef.current);
        navSnapshotRef.current = null;
        navTimerRef.current = null;
      }
    };
  }, [
    workspaces,
    sortedWorkspaces,
    context.workspaceId,
    context.sessionId,
    navigate,
    location.pathname,
  ]);

  // Register workspace-specific keyboard actions based on active context
  useEffect(() => {
    if (!context.workspaceId) return;
    const workspace = workspaces?.find((ws) => ws.id === context.workspaceId);
    if (!workspace) return;

    const scope = { type: 'workspace', id: context.workspaceId } as const;

    // D - go to diff page
    registerAction({
      key: 'd',
      description: 'Go to diff page',
      handler: () => {
        navigate(`/diff/${workspace.id}`);
      },
      scope,
    });

    // G - go to git graph
    registerAction({
      key: 'g',
      description: 'Go to git graph',
      handler: () => {
        navigate(`/git/${workspace.id}`);
      },
      scope,
    });

    // V - open workspace in VS Code (local only)
    if (!isRemoteAccess) {
      registerAction({
        key: 'v',
        description: 'Open workspace in VS Code',
        handler: async () => {
          try {
            const result = await openVSCode(workspace.id);
            if (!result.success) {
              await alert('Unable to open VS Code', result.message);
            }
          } catch (err) {
            await alert('Unable to open VS Code', getErrorMessage(err, 'Failed to open VS Code'));
          }
        },
        scope,
      });
    }

    // Shift+W - dispose workspace (same restrictions as dispose button)
    registerAction({
      key: 'w',
      shiftKey: true,
      description: 'Dispose workspace',
      handler: async () => {
        const resolveInProgress =
          linearSyncResolveConflictStates[workspace.id]?.status === 'in_progress';
        if (resolveInProgress) return;
        const isDevLive =
          devStatus?.source_workspace === workspace.path && !!devStatus?.source_workspace;
        if (isDevLive) return;
        const hasRunningSessions = workspace.sessions?.some((s) => s.running) ?? false;
        if (hasRunningSessions) return;
        if (workspace.status === 'disposing') return;
        const accepted = await confirm(`Dispose workspace ${workspace.id}?`, { danger: true });
        if (!accepted) return;

        try {
          await disposeWorkspace(workspace.id);
          success('Workspace disposed');
          navigate('/');
        } catch (err) {
          toastError(getErrorMessage(err, 'Failed to dispose workspace'));
        }
      },
      scope,
    });

    return () => {
      unregisterAction('d');
      unregisterAction('g');
      if (!isRemoteAccess) unregisterAction('v');
      unregisterAction('w', true);
    };
  }, [
    context.workspaceId,
    workspaces,
    isRemoteAccess,
    registerAction,
    unregisterAction,
    navigate,
    alert,
    confirm,
    linearSyncResolveConflictStates,
    success,
    toastError,
    devStatus,
  ]);

  return (
    <div className={`app-shell${navCollapsed ? ' app-shell--collapsed' : ''}`}>
      <KeyboardModeIndicator />
      <nav className="app-shell__nav">
        <div className="nav-top">
          {(remoteAccessStatus.state === 'connected' || simulateRemote) && (
            <div className="remote-banner" data-testid="remote-banner">
              <svg
                width="12"
                height="12"
                viewBox="0 0 24 24"
                fill="none"
                stroke="currentColor"
                strokeWidth="2"
                strokeLinecap="round"
                strokeLinejoin="round"
              >
                <path d="M1 6v16l7-4 8 4 7-4V2l-7 4-8-4-7 4z" />
                <line x1="8" y1="2" x2="8" y2="18" />
                <line x1="16" y1="6" x2="16" y2="22" />
              </svg>
              Remote Access
            </div>
          )}
          <div className="nav-header">
            <div className="nav-header__left">
              <NavLink to="/" className="logo">
                <span
                  className={`nav-header__connection-dot ${connected ? 'nav-header__connection-dot--connected' : 'nav-header__connection-dot--offline'}`}
                  data-testid="connection-status"
                  data-connected={connected ? 'true' : 'false'}
                  title={connected ? 'Connected' : 'Offline'}
                ></span>
                schmux
                {showUpdateBadge && (
                  <span
                    className="update-badge"
                    title={`Update available: ${versionInfo.latest_version}`}
                  ></span>
                )}
              </NavLink>
              <span className="nav-header__version">
                {versionInfo?.version
                  ? versionInfo.version === 'dev'
                    ? 'dev'
                    : `v${versionInfo.version}`
                  : ''}
              </span>
            </div>
            <div className="nav-header__actions">
              {mode === 'active' && <div className="keyboard-mode-pill">KB</div>}
              <Tooltip content="Toggle theme">
                <button
                  id="themeToggle"
                  className="icon-btn icon-btn--sm"
                  aria-label="Toggle theme"
                  onClick={toggleTheme}
                >
                  <span className="icon-theme"></span>
                </button>
              </Tooltip>
              <Tooltip content="View on GitHub">
                <a
                  href="https://github.com/sergeknystautas/schmux"
                  target="_blank"
                  rel="noopener noreferrer"
                  className="icon-btn icon-btn--sm"
                  aria-label="View on GitHub"
                >
                  <svg
                    className="icon-github"
                    viewBox="0 0 24 24"
                    fill="currentColor"
                    aria-hidden="true"
                  >
                    <path d="M12 0c-6.626 0-12 5.373-12 12 0 5.302 3.438 9.8 8.207 11.387.599.111.793-.261.793-.577v-2.234c-3.338.726-4.033-1.416-4.033-1.416-.546-1.387-1.333-1.756-1.333-1.756-1.089-.745.083-.729.083-.729 1.205.084 1.839 1.237 1.839 1.237 1.07 1.834 2.807 1.304 3.492.997.107-.775.418-1.305.762-1.604-2.665-.305-5.467-1.334-5.467-5.931 0-1.311.469-2.381 1.236-3.221-.124-.303-.535-1.524.117-3.176 0 0 1.008-.322 3.301 1.23.957-.266 1.983-.399 3.003-.404 1.02.005 2.047.138 3.006.404 2.291-1.552 3.297-1.23 3.297-1.23.653 1.653.242 2.874.118 3.176.77.84 1.235 1.911 1.235 3.221 0 4.609-2.807 5.624-5.479 5.921.43.372.823 1.102.823 2.222v3.293c0 .319.192.694.801.576 4.765-1.589 8.199-6.086 8.199-11.386 0-6.627-5.373-12-12-12z" />
                  </svg>
                </a>
              </Tooltip>
              <button
                className="nav-collapse-btn"
                onClick={() => setNavCollapsed(!navCollapsed)}
                aria-label={navCollapsed ? 'Expand navigation' : 'Collapse navigation'}
              >
                <svg
                  width="16"
                  height="16"
                  viewBox="0 0 24 24"
                  fill="none"
                  stroke="currentColor"
                  strokeWidth="2"
                >
                  {navCollapsed ? (
                    <polyline points="9 18 15 12 9 6"></polyline>
                  ) : (
                    <polyline points="15 18 9 12 15 6"></polyline>
                  )}
                </svg>
              </button>
            </div>
          </div>

          <div className="nav-spawn-btn-container">
            <button
              className="btn nav-spawn-btn"
              data-tour="sidebar-add-workspace"
              onClick={() => navigate('/spawn')}
            >
              <svg
                width="14"
                height="14"
                viewBox="0 0 24 24"
                fill="none"
                stroke="currentColor"
                strokeWidth="2.5"
              >
                <line x1="12" y1="5" x2="12" y2="19"></line>
                <line x1="5" y1="12" x2="19" y2="12"></line>
              </svg>
              Add Workspace
            </button>
          </div>

          <div className="nav-workspaces" data-tour="sidebar-workspace-list">
            <div className="nav-section-header">
              <span className="nav-section-title">Workspaces ({workspaces?.length ?? 0})</span>
              <div className="nav-sort-toggle">
                <button
                  className={`nav-sort-toggle__btn${workspaceSort === 'alpha' ? ' nav-sort-toggle__btn--active' : ''}`}
                  onClick={() => setWorkspaceSort('alpha')}
                  title="Sort alphabetically"
                >
                  abc
                </button>
                <button
                  className={`nav-sort-toggle__btn${workspaceSort === 'time' ? ' nav-sort-toggle__btn--active' : ''}`}
                  onClick={() => setWorkspaceSort('time')}
                  title="Sort by most recent activity"
                >
                  12:00
                </button>
              </div>
            </div>
            {(!workspaces || workspaces.length === 0) && (
              <div className="nav-empty-state">
                <p>No workspaces yet</p>
              </div>
            )}
            {sortedWorkspaces?.map((workspace, wsIndex) => {
              const wsLockState = workspaceLockStates[workspace.id];
              const wsResolveState = linearSyncResolveConflictStates[workspace.id];
              const wsLocked = !!wsLockState?.locked || wsResolveState?.status === 'in_progress';
              const linesAdded = workspace.lines_added ?? 0;
              const linesRemoved = workspace.lines_removed ?? 0;
              const isGit = !workspace.vcs || workspace.vcs === 'git';
              const hasChanges = isGit && (linesAdded > 0 || linesRemoved > 0);
              const isWorkspaceActive = workspace.id === (currentWorkspaceId || activeWorkspaceId);

              // For remote workspaces, use hostname from first session if branch matches repo (fallback case)
              const isRemote = !!workspace.remote_host_id;
              const remoteHostname = workspace.sessions?.find(
                (s) => s.remote_hostname
              )?.remote_hostname;
              const displayBranch =
                isRemote && remoteHostname && workspace.branch === getRepoName(workspace.repo)
                  ? remoteHostname
                  : workspace.branch;
              const remoteDisconnected = isRemote && workspace.remote_host_status !== 'connected';

              // Dev mode: is this workspace eligible and is it the live one?
              const isDevEligible =
                isDevMode && !isRemote && (!devSourceRepo || workspace.repo === devSourceRepo);
              const isDevLive = isDevEligible && devStatus?.source_workspace === workspace.path;

              return (
                <div
                  key={workspace.id}
                  ref={isWorkspaceActive ? activeWorkspaceRef : null}
                  className={`nav-workspace${isWorkspaceActive ? ' nav-workspace--active' : ''}${isDevLive ? ' nav-workspace--dev-live' : ''}${workspace.status === 'disposing' ? ' nav-workspace--disposing' : ''}`}
                >
                  <div
                    className="nav-workspace__header"
                    onClick={() => handleWorkspaceClick(workspace.id)}
                    role="button"
                    tabIndex={0}
                    onKeyDown={(e) => {
                      if (e.key === 'Enter' || e.key === ' ') {
                        e.preventDefault();
                        handleWorkspaceClick(workspace.id);
                      }
                    }}
                  >
                    <div className="nav-workspace__top-row">
                      <span className="nav-workspace__name">
                        {isRemote && (
                          <span
                            style={{
                              width: '8px',
                              height: '8px',
                              borderRadius: '50%',
                              backgroundColor: remoteDisconnected
                                ? 'var(--color-error)'
                                : 'var(--color-success)',
                              display: 'inline-block',
                              marginRight: '6px',
                              flexShrink: 0,
                            }}
                            title={remoteDisconnected ? 'Disconnected' : 'Connected'}
                          />
                        )}
                        {displayBranch}
                      </span>
                      {wsLocked ? (
                        <span className="nav-workspace__changes">
                          <WorkingSpinner />
                        </span>
                      ) : hasChanges ? (
                        <span className="nav-workspace__changes">
                          {linesAdded > 0 && <span className="text-success">+{linesAdded}</span>}
                          {linesRemoved > 0 && (
                            <span
                              className="text-error"
                              style={{ marginLeft: linesAdded > 0 ? '2px' : '0' }}
                            >
                              -{linesRemoved}
                            </span>
                          )}
                        </span>
                      ) : null}
                      {isDevEligible && (
                        <button
                          className="nav-workspace__dev-btn"
                          title={
                            isDevLive
                              ? 'Rebuild backend and restart Vite'
                              : 'Switch to this workspace (rebuild + restart)'
                          }
                          onClick={(e) => {
                            e.stopPropagation();
                            handleDevRebuild(workspace.id, 'both');
                          }}
                          disabled={devRebuilding}
                        >
                          {isDevLive ? 'Rebuild' : 'Test'}
                        </button>
                      )}
                    </div>
                    <div
                      className="nav-workspace__repo"
                      style={
                        remoteDisconnected
                          ? {
                              display: 'flex',
                              alignItems: 'center',
                              justifyContent: 'space-between',
                              gap: '4px',
                            }
                          : undefined
                      }
                    >
                      <span className="truncate">
                        {isRemote && workspace.remote_flavor_name
                          ? `${workspace.remote_flavor_name} · ${workspace.remote_flavor || getRepoName(workspace.repo)}`
                          : getRepoName(workspace.repo)}
                      </span>
                      {remoteDisconnected && (
                        <button
                          className="btn btn--sm"
                          style={{
                            fontSize: '0.65rem',
                            padding: '1px 6px',
                            margin: 0,
                            color: 'var(--color-warning)',
                            borderColor: 'var(--color-warning)',
                            flexShrink: 0,
                            lineHeight: 1.2,
                          }}
                          onClick={async (e) => {
                            e.stopPropagation();
                            try {
                              const result = await reconnectRemoteHost(workspace.remote_host_id!);
                              setReconnectModal({
                                hostId: workspace.remote_host_id!,
                                flavorId: result.flavor_id,
                                displayName: result.hostname || workspace.branch,
                                provisioningSessionId: result.provisioning_session_id || null,
                              });
                            } catch (err) {
                              toastError(getErrorMessage(err, 'Failed to reconnect'));
                            }
                          }}
                        >
                          Reconnect
                        </button>
                      )}
                    </div>
                  </div>
                  <div className="nav-workspace__sessions">
                    {sortSessionsByTabOrder(workspace.id, workspace.sessions || []).map(
                      (sess, sessIndex) => {
                        const isActive = sess.id === sessionId;
                        const activityDisplay = !sess.running
                          ? 'Stopped'
                          : sess.last_output_at
                            ? formatRelativeTime(sess.last_output_at)
                            : '-';

                        // run_targets are command-only now; if not in run_targets, it's a model = promptable
                        const isCommand = (config?.run_targets || []).some(
                          (t) => t.name === sess.target
                        );
                        const isPromptable = !isCommand;

                        const nudgeSummary = formatNudgeSummary(sess.nudge_summary, 40);

                        // "Working" is an operational state — show spinner inline
                        // in row1 to avoid reflow from row2 appearing/disappearing.
                        const isWorkingState =
                          sess.nudge_state === 'Working' ||
                          (nudgenikEnabled && !sess.nudge_state && isPromptable && sess.running);

                        const isIdleState = sess.nudge_state === 'Idle';

                        // Determine what to show in row2
                        // Show nudge indicators if there's a nudge_state (from signals or nudgenik)
                        // Suppress for the currently focused session — the user is already looking at it
                        let nudgePreviewElement: React.ReactNode = null;
                        if (!isWorkingState && !isIdleState && !isActive) {
                          const nudgeEmoji = sess.nudge_state
                            ? nudgeStateEmoji[sess.nudge_state] || null
                            : null;
                          if (nudgeEmoji) {
                            nudgePreviewElement = nudgeSummary
                              ? `${nudgeEmoji} ${nudgeSummary}`
                              : `${nudgeEmoji} ${sess.nudge_state}`;
                          }
                        }

                        return (
                          <div
                            key={sess.id}
                            className={`nav-session${isActive ? ' nav-session--active' : ''}${sess.status === 'disposing' ? ' nav-session--disposing' : ''}`}
                            data-tour={
                              wsIndex === 0 && sessIndex === 0 ? 'sidebar-session' : undefined
                            }
                            onClick={() =>
                              sess.status !== 'disposing' && handleSessionClick(sess.id)
                            }
                            role="button"
                            tabIndex={0}
                            onKeyDown={(e) => {
                              if (sess.status === 'disposing') return;
                              if (e.key === 'Enter' || e.key === ' ') {
                                e.preventDefault();
                                handleSessionClick(sess.id);
                              }
                            }}
                          >
                            <div className="nav-session__row1">
                              {wsLocked ? (
                                <span style={{ marginRight: '4px', fontSize: '11px' }}>🔒</span>
                              ) : (
                                isWorkingState && <WorkingSpinner />
                              )}
                              <span className="nav-session__name">
                                {sess.remote_host_id && (
                                  <svg
                                    width="12"
                                    height="12"
                                    viewBox="0 0 24 24"
                                    fill="none"
                                    stroke="currentColor"
                                    strokeWidth="2"
                                    style={{
                                      marginRight: '4px',
                                      verticalAlign: 'text-bottom',
                                      opacity: 0.7,
                                    }}
                                    aria-label={sess.remote_flavor_name || 'Remote'}
                                  >
                                    <rect x="1" y="4" width="22" height="16" rx="2" ry="2" />
                                    <line x1="1" y1="10" x2="23" y2="10" />
                                  </svg>
                                )}
                                {sess.nickname || sess.xterm_title || sess.target}
                              </span>
                              {sess.persona_icon && (
                                <span
                                  className="nav-session__persona-badge"
                                  title={sess.persona_name}
                                  style={{ color: sess.persona_color }}
                                >
                                  {sess.persona_icon}
                                </span>
                              )}
                              <span
                                className="nav-session__activity"
                                data-tour={
                                  wsIndex === 0 && sessIndex === 0
                                    ? 'sidebar-session-status'
                                    : undefined
                                }
                              >
                                {activityDisplay}
                              </span>
                            </div>
                            {!wsLocked && nudgePreviewElement && (
                              <div
                                className="nav-session__row2"
                                data-tour={
                                  nudgePreviewElement && wsIndex === 0 && sessIndex === 1
                                    ? 'sidebar-nudge'
                                    : undefined
                                }
                              >
                                {nudgePreviewElement}
                              </div>
                            )}
                          </div>
                        );
                      }
                    )}
                  </div>
                </div>
              );
            })}
          </div>

          {isDevMode && <CurationStatus />}
          {isDevMode && <EventMonitor />}
          {isDevMode && <TmuxDiagnostic />}
          {isDevMode && <TypingPerformance />}
          {features.tunnel && <RemoteAccessPanel />}
          <ToolsSection navCollapsed={navCollapsed} />
        </div>
      </nav>

      <main className="app-shell__content">
        {reconnectModal && (
          <ConnectionProgressModal
            flavorId={reconnectModal.flavorId}
            flavorName={reconnectModal.displayName}
            provisioningSessionId={reconnectModal.provisioningSessionId}
            onClose={() => setReconnectModal(null)}
            onConnected={() => {
              setReconnectModal(null);
            }}
          />
        )}

        {devRebuilding && devRebuildTarget && (
          <div className="modal-overlay">
            <div className="modal dev-rebuild-dialog">
              <div
                className="modal__body"
                style={{ textAlign: 'center', padding: 'var(--spacing-xl)' }}
              >
                <div className="dev-rebuild-dialog__spinner"></div>
                <div className="dev-rebuild-dialog__phase">
                  {devRebuildPhase === 'building' ? 'Building...' : 'Restarting...'}
                </div>
                <div className="dev-rebuild-dialog__detail">
                  {devRebuildPhase === 'building'
                    ? `Compiling from ${workspaces?.find((ws) => ws.id === devRebuildTarget)?.branch || devRebuildTarget}`
                    : 'Waiting for daemon to restart'}
                </div>
              </div>
            </div>
          </div>
        )}

        <Outlet />
      </main>
      <div className="tools-section--mobile-only">
        <ToolsSection navCollapsed={false} disableCollapse />
      </div>
    </div>
  );
}
