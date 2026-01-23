import React, { useState } from 'react';
import { useNavigate } from 'react-router-dom';
import { disposeSession, disposeWorkspace, openVSCode, refreshOverlay, getErrorMessage } from '../lib/api'
import { copyToClipboard } from '../lib/utils'
import { useToast } from './ToastProvider'
import { useModal } from './ModalProvider'
import { useConfig } from '../contexts/ConfigContext'
import { useSessions } from '../contexts/SessionsContext'
import WorkspaceTableRow from './WorkspaceTableRow'
import SessionTableRow from './SessionTableRow'
import Tooltip from './Tooltip'
import SpawnDropdown from './SpawnDropdown'
import DiffDropdown from './DiffDropdown'
import VSCodeResultModal from './VSCodeResultModal'
import useLocalStorage, { WORKSPACE_EXPANDED_KEY } from '../hooks/useLocalStorage'
import type { OpenVSCodeResponse, QuickLaunchPreset, SessionResponse, WorkspaceResponse } from '../lib/types';

type WorkspaceFilters = {
  status?: string;
  repo?: string;
};

export type WorkspacesListHandle = {
  disposeSession: (sessionId: string) => void;
};

type WorkspacesListProps = {
  workspaceId?: string;
  currentSessionId?: string;
  filters?: WorkspaceFilters | null;
  onFilterChange?: (key: keyof WorkspaceFilters, value: string) => void;
  showControls?: boolean;
};

/**
 * WorkspacesList - Displays workspaces with their sessions
 *
 * Handles polling, filtering, and expansion state internally.
 * Used by: SessionsPage, SessionDetailPage, DiffPage
 *
 * Props:
 * - workspaceId: Optional - if provided, shows only that workspace
 * - currentSessionId: Optional - highlights this session in the list
 * - filters: Optional - { status, repo } filter state
 * - onFilterChange: Optional - callback when filters change
 * - showControls: Optional - show expand/collapse controls
 */
const WorkspacesListInner = React.forwardRef<WorkspacesListHandle, WorkspacesListProps>(function WorkspacesList({
  workspaceId,
  currentSessionId,
  filters = null,
  onFilterChange = null,
  showControls = true,
}, ref) {
  const { config, getRepoName } = useConfig();
  const { workspaces: allWorkspaces, loading, error, refresh } = useSessions();
  const { success, error: toastError } = useToast();
  const { confirm } = useModal();
  const navigate = useNavigate();
  const [expanded, setExpanded] = useLocalStorage<Record<string, boolean>>(WORKSPACE_EXPANDED_KEY, {});
  const [vsCodeResult, setVSCodeResult] = useState<OpenVSCodeResponse | null>(null);
  const [openingVSCode, setOpeningVSCode] = useState<string | null>(null); // Track which workspace is opening VS Code
  const [refreshingOverlay, setRefreshingOverlay] = useState<string | null>(null); // Track which workspace is refreshing overlay
  const [overlayRefreshResult, setOverlayRefreshResult] = useState<{ success: boolean; message?: string; workspaceId?: string } | null>(null); // Result of overlay refresh

  const quickLaunch = React.useMemo<QuickLaunchPreset[]>(() => {
    return config?.quick_launch || [];
  }, [config?.quick_launch]);

  const toggleExpanded = (workspaceId: string) => {
    setExpanded((curr) => ({ ...curr, [workspaceId]: !curr[workspaceId] }));
  };

  const expandAll = () => {
    const next = {};
    filteredWorkspaces.forEach((ws) => {
      next[ws.id] = true;
    });
    setExpanded(next);
  };

  const collapseAll = () => {
    setExpanded({});
  };

  const updateFilter = (key: keyof WorkspaceFilters, value: string) => {
    if (onFilterChange) {
      onFilterChange(key, value);
    }
  };

  const handleCopyAttach = async (command: string) => {
    const ok = await copyToClipboard(command);
    if (ok) {
      success('Copied attach command');
    } else {
      toastError('Failed to copy');
    }
  };

  const handleDispose = async (sessionId: string) => {
    // Find session to get nickname for display
    let sessionDisplay = sessionId;
    for (const ws of allWorkspaces) {
      const sess = ws.sessions?.find(s => s.id === sessionId);
      if (sess?.nickname) {
        sessionDisplay = `${sess.nickname} (${sessionId})`;
        break;
      }
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

  // Expose methods to parent via ref
  React.useImperativeHandle(ref, () => ({
    disposeSession: handleDispose
  }), [handleDispose]);

  const handleDisposeWorkspace = async (workspaceId: string) => {
    const accepted = await confirm(`Dispose workspace ${workspaceId}?`, { danger: true });
    if (!accepted) return;

    try {
      await disposeWorkspace(workspaceId);
      success('Workspace disposed');
      refresh();
    } catch (err) {
      // Display detailed error message from backend
      toastError(getErrorMessage(err, 'Failed to dispose workspace'));
    }
  };

  const handleOpenVSCode = async (workspace: WorkspaceResponse) => {
    setOpeningVSCode(workspace.id);
    try {
      const result = await openVSCode(workspace.id);
      setVSCodeResult(result);
    } catch (err) {
      setVSCodeResult({ success: false, message: getErrorMessage(err, 'Failed to open VS Code') });
    } finally {
      setOpeningVSCode(null);
    }
  };

  const handleRefreshOverlay = async (workspace: WorkspaceResponse) => {
    setRefreshingOverlay(workspace.id);
    try {
      await refreshOverlay(workspace.id);
      setOverlayRefreshResult({ success: true, workspaceId: workspace.id });
      refresh();
    } catch (err) {
      setOverlayRefreshResult({ success: false, message: getErrorMessage(err, 'Failed to refresh overlay') });
    } finally {
      setRefreshingOverlay(null);
    }
  };

  const renderWorkspaceActions = (workspace: WorkspaceResponse) => (
    <>
      <Tooltip content="Open in VS Code">
        <button
          className="btn btn--sm btn--ghost btn--bordered"
          disabled={openingVSCode === workspace.id}
          onClick={(event) => {
            event.stopPropagation();
            handleOpenVSCode(workspace);
          }}
          aria-label={`Open ${workspace.id} in VS Code`}
        >
          {openingVSCode === workspace.id ? (
            <div className="spinner--small"></div>
          ) : (
            <svg width="16" height="16" viewBox="0 0 24 24" fill="none" xmlns="http://www.w3.org/2000/svg">
              <path d="M23.15 2.587L18.21.21a1.494 1.494 0 0 0-1.705.29l-9.46 8.63-4.12-3.128a.999.999 0 0 0-1.276.057L.327 7.261A1 1 0 0 0 .326 8.74L3.899 12 .326 15.26a1 1 0 0 0 .001 1.479L1.65 17.94a.999.999 0 0 0 1.276.057l4.12-3.128 9.46 8.63a1.492 1.492 0 0 0 1.704.29l4.942-2.377A1.5 1.5 0 0 0 24 20.06V3.939a1.5 1.5 0 0 0-.85-1.352zm-5.146 14.861L10.826 12l7.178-5.448v10.896z" fill="#007ACC"/>
            </svg>
          )}
        </button>
      </Tooltip>
      <DiffDropdown workspace={workspace} externalDiffCommands={config?.external_diff_commands || []} />
      <Tooltip content="View on GitHub">
        <a
          href={workspace.repo}
          target="_blank"
          rel="noopener noreferrer"
          className="btn btn--sm btn--ghost btn--bordered"
          aria-label={`View ${workspace.repo} on GitHub`}
          onClick={(event) => event.stopPropagation()}
        >
          <svg width="16" height="16" viewBox="0 0 24 24" fill="currentColor" aria-hidden="true">
            <path d="M12 0c-6.626 0-12 5.373-12 12 0 5.302 3.438 9.8 8.207 11.387.599.111.793-.261.793-.577v-2.234c-3.338.726-4.033-1.416-4.033-1.416-.546-1.387-1.333-1.756-1.333-1.756-1.089-.745.083-.729.083-.729 1.205.084 1.839 1.237 1.839 1.237 1.07 1.834 2.807 1.304 3.492.997.107-.775.418-1.305.762-1.604-2.665-.305-5.467-1.334-5.467-5.931 0-1.311.469-2.381 1.236-3.221-.124-.303-.535-1.524.117-3.176 0 0 1.008-.322 3.301 1.23.957-.266 1.983-.399 3.003-.404 1.02.005 2.047.138 3.006.404 2.291-1.552 3.297-1.23 3.297-1.23.653 1.653.242 2.874.118 3.176.77.84 1.235 1.911 1.235 3.221 0 4.609-2.807 5.624-5.479 5.921.43.372.823 1.102.823 2.222v3.293c0 .319.192.694.801.576 4.765-1.589 8.199-6.086 8.199-11.386 0-6.627-5.373-12-12-12z"/>
          </svg>
        </a>
      </Tooltip>
      <Tooltip content="Refresh overlay files">
        <button
          className="btn btn--sm btn--ghost btn--bordered"
          disabled={refreshingOverlay === workspace.id}
          onClick={(event) => {
            event.stopPropagation();
            handleRefreshOverlay(workspace);
          }}
          aria-label={`Refresh overlay for ${workspace.id}`}
        >
          {refreshingOverlay === workspace.id ? (
            <div className="spinner--small"></div>
          ) : (
            <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2">
              <path d="M21.5 2v6h-6M21.34 5.5A10 10 0 1 1 11.26 2.25"/>
            </svg>
          )}
        </button>
      </Tooltip>
      <Tooltip content="Dispose workspace and all sessions" variant="warning">
        <button
          className="btn btn--sm btn--ghost btn--danger btn--bordered"
          onClick={(event) => {
            event.stopPropagation();
            handleDisposeWorkspace(workspace.id);
          }}
          aria-label={`Dispose ${workspace.id}`}
        >
          <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2">
            <polyline points="3 6 5 6 21 6"></polyline>
            <path d="M19 6v14a2 2 0 0 1-2 2H7a2 2 0 0 1-2-2V6m3 0V4a2 2 0 0 1 2-2h4a2 2 0 0 1 2 2v2"></path>
          </svg>
        </button>
      </Tooltip>
      <SpawnDropdown workspace={workspace} quickLaunch={quickLaunch} />
    </>
  );

  // Apply filters
  let filteredWorkspaces = allWorkspaces;
  if (filters?.status) {
    filteredWorkspaces = filteredWorkspaces.filter((ws) => {
      const hasSessionsWithStatus = ws.sessions?.some((s) =>
        filters.status === 'running' ? s.running : !s.running
      );
      return hasSessionsWithStatus;
    });
  }
  if (filters?.repo) {
    filteredWorkspaces = filteredWorkspaces.filter((ws) => ws.repo === filters.repo);
  }

  // If workspaceId is specified, filter to just that workspace
  if (workspaceId) {
    filteredWorkspaces = filteredWorkspaces.filter((ws) => ws.id === workspaceId);
  }

  const empty = filteredWorkspaces.length === 0 && !loading && !error && allWorkspaces.length > 0;
  const noWorkspaces = allWorkspaces.length === 0 && !loading && !error;

  // Get unique repo URLs and their display names for filter dropdown
  const repoOptions = React.useMemo(() => {
    const urls = [...new Set(allWorkspaces.map((ws) => ws.repo))];
    return urls
      .map(url => ({ url, name: getRepoName(url) }))
      .sort((a, b) => a.name.localeCompare(b.name));
  }, [allWorkspaces, getRepoName]);

  return (
    <>
      {filters && onFilterChange && (
        <div className="filter-bar">
          <span className="filter-bar__label">Filters:</span>
          <div className="filter-bar__filters">
            <select
              className="select"
              aria-label="Filter by status"
              value={filters.status || ''}
              onChange={(event) => updateFilter('status', event.target.value)}
            >
              <option value="">All Status</option>
              <option value="running">Running</option>
              <option value="stopped">Stopped</option>
            </select>
            <select
              className="select"
              aria-label="Filter by repository"
              value={filters.repo || ''}
              onChange={(event) => updateFilter('repo', event.target.value)}
            >
              <option value="">All Repos</option>
              {repoOptions.map((option) => (
                <option key={option.url} value={option.url}>{option.name}</option>
              ))}
            </select>
          </div>
        </div>
      )}

      {showControls && (
        <div className="workspace-controls">
          <button className="btn btn--sm btn--ghost" onClick={expandAll}>
            <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2">
              <polyline points="6 9 12 15 18 9"></polyline>
            </svg>
            Expand All
          </button>
          <button className="btn btn--sm btn--ghost" onClick={collapseAll}>
            <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2">
              <polyline points="18 15 12 9 6 15"></polyline>
            </svg>
            Collapse All
          </button>
        </div>
      )}

      <div className="workspace-list">
        {loading && (
          <div className="loading-state">
            <div className="spinner"></div>
            <span>Loading workspaces...</span>
          </div>
        )}

        {error && (
          <div className="empty-state">
            <div className="empty-state__icon">⚠️</div>
            <h3 className="empty-state__title">Failed to load workspaces</h3>
            <p className="empty-state__description">{error}</p>
            <button className="btn btn--primary" onClick={() => refresh()}>
              Try Again
            </button>
          </div>
        )}

        {empty && (
          <div className="empty-state">
            <h3 className="empty-state__title">No workspaces match your filters</h3>
            <p className="empty-state__description">Try adjusting your filters to see more results</p>
          </div>
        )}

        {noWorkspaces && (
          <div className="empty-state">
            <h3 className="empty-state__title">No workspaces found</h3>
            <p className="empty-state__description">Workspaces will appear here when you spawn sessions</p>
          </div>
        )}

        {filteredWorkspaces.length > 0 && !loading && !error && (
          <table className="session-table session-table--header">
            <thead>
              <tr>
                <th>Session</th>
                <th>Status</th>
                <th>Created</th>
                <th>Last Activity</th>
                <th className="text-right">Actions</th>
              </tr>
            </thead>
          </table>
        )}

        {filteredWorkspaces.map((ws) => {
          let sessions: SessionResponse[] = ws.sessions || [];
          if (filters?.status) {
            sessions = sessions.filter((s) =>
              filters.status === 'running' ? s.running : !s.running
            );
          }
          const sessionCount = sessions.length;

          return (
            <WorkspaceTableRow
              key={ws.id}
              workspace={ws}
              expanded={expanded[ws.id]}
              onToggle={() => toggleExpanded(ws.id)}
              sessionCount={sessionCount}
              actions={renderWorkspaceActions(ws)}
              sessions={
                sessionCount > 0 ? (
                  <table className="session-table session-table--no-header">
                    {sessions.map((sess) => (
                      <SessionTableRow
                        key={sess.id}
                        sess={sess}
                        currentSessionId={currentSessionId}
                        onCopyAttach={handleCopyAttach}
                        onDispose={handleDispose}
                      />
                    ))}
                  </table>
                ) : (
                  <p style={{ padding: '1rem', color: 'var(--color-text-subtle)' }}>No sessions in this workspace</p>
                )
              }
            />
          );
        })}
      </div>

      {vsCodeResult && (
        <VSCodeResultModal
          success={vsCodeResult.success}
          message={vsCodeResult.message}
          onClose={() => setVSCodeResult(null)}
        />
      )}

      {overlayRefreshResult && (
        <div className="modal-overlay" role="dialog" aria-modal="true" aria-labelledby="overlay-modal-title">
          <div className="modal">
            <div className="modal__header">
              <h2 className="modal__title" id="overlay-modal-title">
                {overlayRefreshResult.success ? 'Overlay refreshed' : 'Overlay refresh failed'}
              </h2>
            </div>
            <div className="modal__body">
              {overlayRefreshResult.success ? (
                <p>Overlay files have been refreshed for workspace <strong>{overlayRefreshResult.workspaceId}</strong>.</p>
              ) : (
                <p>{overlayRefreshResult.message}</p>
              )}
            </div>
            <div className="modal__footer">
              <button className="btn btn--primary" onClick={() => setOverlayRefreshResult(null)}>
                OK
              </button>
            </div>
          </div>
        </div>
      )}
    </>
  );
});

export default WorkspacesListInner;
