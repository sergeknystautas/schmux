import React, { useEffect, useState, useCallback } from 'react';
import { useNavigate } from 'react-router-dom';
import { getWorkspaces, getSessions, disposeSession, disposeWorkspace } from '../lib/api.js';
import { copyToClipboard } from '../lib/utils.js';
import { useToast } from './ToastProvider.jsx';
import { useModal } from './ModalProvider.jsx';
import { useConfig } from '../contexts/ConfigContext.jsx';
import WorkspaceTableRow from './WorkspaceTableRow.jsx';
import SessionTableRow from './SessionTableRow.jsx';
import Tooltip from './Tooltip.jsx';
import SpawnDropdown from './SpawnDropdown.jsx';
import useLocalStorage from '../hooks/useLocalStorage.js';

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
export default function WorkspacesList({
  workspaceId,
  currentSessionId,
  filters = null,
  onFilterChange = null,
  showControls = true,
}) {
  const { config, getRepoName } = useConfig();
  const { success, error: toastError } = useToast();
  const { confirm } = useModal();
  const navigate = useNavigate();
  const [allWorkspaces, setAllWorkspaces] = useState([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState('');
  const [expanded, setExpanded] = useLocalStorage('workspace-expanded', {});

  // Extract commands (non-agentic agents) from config for quick spawn
  const commands = React.useMemo(() => {
    return (config?.agents || []).filter(a => a.agentic === false);
  }, [config?.agents]);

  const loadWorkspaces = useCallback(async (silent = false) => {
    if (!silent) {
      setLoading(true);
    }
    setError('');
    try {
      // Fetch both: all workspaces (including empty) and workspaces with sessions
      const [allWorkspaces, workspacesWithSessions] = await Promise.all([
        getWorkspaces(),
        getSessions()
      ]);

      // Create a map of workspace ID -> sessions for quick lookup
      const sessionsMap = {};
      workspacesWithSessions.forEach(ws => {
        sessionsMap[ws.id] = ws.sessions || [];
      });

      // Merge: add sessions to each workspace from the sessions map
      const merged = allWorkspaces.map(ws => ({
        ...ws,
        sessions: sessionsMap[ws.id] || []
      }));

      setAllWorkspaces(merged);
    } catch (err) {
      if (!silent) {
        setError(err.message || 'Failed to load workspaces');
      }
    } finally {
      if (!silent) {
        setLoading(false);
      }
    }
  }, []);

  useEffect(() => {
    loadWorkspaces();
  }, [loadWorkspaces]);

  // Auto-refresh (silent mode - no flicker)
  useEffect(() => {
    const pollInterval = config.internal?.sessions_poll_interval_ms || 5000;
    const interval = setInterval(() => {
      loadWorkspaces(true);
    }, pollInterval);
    return () => clearInterval(interval);
  }, [loadWorkspaces, config]);

  const toggleExpanded = (workspaceId) => {
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

  const updateFilter = (key, value) => {
    if (onFilterChange) {
      onFilterChange(key, value);
    }
  };

  const handleCopyAttach = async (command) => {
    const ok = await copyToClipboard(command);
    if (ok) {
      success('Copied attach command');
    } else {
      toastError('Failed to copy');
    }
  };

  const handleDispose = async (sessionId) => {
    const accepted = await confirm(`Dispose session ${sessionId}?`, { danger: true });
    if (!accepted) return;

    try {
      await disposeSession(sessionId);
      success('Session disposed');
      loadWorkspaces();
    } catch (err) {
      toastError(`Failed to dispose: ${err.message}`);
    }
  };

  const handleDisposeWorkspace = async (workspaceId) => {
    const accepted = await confirm(`Dispose workspace ${workspaceId}?`, { danger: true });
    if (!accepted) return;

    try {
      await disposeWorkspace(workspaceId);
      success('Workspace disposed');
      loadWorkspaces();
    } catch (err) {
      toastError(`Failed to dispose workspace: ${err.message}`);
    }
  };

  const renderWorkspaceActions = (workspace) => (
    <>
      <Tooltip content="View git diff">
        <button
          className="btn btn--sm btn--ghost"
          onClick={(event) => {
            event.stopPropagation();
            navigate(`/diff/${workspace.id}`);
          }}
          aria-label={`View diff for ${workspace.id}`}
        >
          <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2">
            <path d="M11 4H4a2 2 0 0 0-2 2v14a2 2 0 0 0 2 2h14a2 2 0 0 0 2-2v-7"></path>
            <path d="M18.5 2.5a2.121 2.121 0 0 1 3 3L12 15l-4 1 1-4 9.5-9.5z"></path>
          </svg>
          Diff
        </button>
      </Tooltip>
      <SpawnDropdown workspace={workspace} commands={commands} />
      <Tooltip content="Dispose workspace and all sessions" variant="warning">
        <button
          className="btn btn--sm btn--ghost btn--danger"
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
          Dispose
        </button>
      </Tooltip>
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
            <button className="btn btn--primary" onClick={() => loadWorkspaces()}>
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

        {filteredWorkspaces.map((ws) => {
          let sessions = ws.sessions || [];
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
                  <table className="session-table">
                    <thead>
                      <tr>
                        <th>Session</th>
                        <th>Status</th>
                        <th>Created</th>
                        <th>Last Activity</th>
                        <th className="text-right">Actions</th>
                      </tr>
                    </thead>
                    <tbody>
                      {sessions.map((sess) => (
                        <SessionTableRow
                          key={sess.id}
                          sess={sess}
                          currentSessionId={currentSessionId}
                          onCopyAttach={handleCopyAttach}
                          onDispose={handleDispose}
                        />
                      ))}
                    </tbody>
                  </table>
                ) : (
                  <p style={{ padding: '1rem', color: 'var(--color-text-subtle)' }}>No sessions in this workspace</p>
                )
              }
            />
          );
        })}
      </div>
    </>
  );
}
