import React, { useEffect, useState, useCallback } from 'react';
import { getWorkspaces, getSessions, disposeSession } from '../lib/api.js';
import { copyToClipboard } from '../lib/utils.js';
import { useToast } from './ToastProvider.jsx';
import { useModal } from './ModalProvider.jsx';
import { useConfig } from '../contexts/ConfigContext.jsx';
import WorkspaceTableRow from './WorkspaceTableRow.jsx';
import SessionTableRow from './SessionTableRow.jsx';
import Tooltip from './Tooltip.jsx';
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
 * - renderActions: Optional - function to render actions for each workspace
 * - renderSessionActions: Optional - function to render actions for each session
 */
export default function WorkspacesList({
  workspaceId,
  currentSessionId,
  filters = null,
  onFilterChange = null,
  showControls = true,
  renderActions = null,
  renderSessionActions = null,
}) {
  const { config, getRepoName } = useConfig();
  const { success, error: toastError } = useToast();
  const { confirm } = useModal();
  const [allWorkspaces, setAllWorkspaces] = useState([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState('');
  const [expanded, setExpanded] = useLocalStorage('workspace-expanded', {});

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
              actions={renderActions ? renderActions(ws) : null}
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
                          onDispose={renderSessionActions ?
                            () => renderSessionActions('dispose', sess) :
                            handleDispose
                          }
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
