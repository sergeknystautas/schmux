import React, { useCallback, useEffect, useMemo, useState } from 'react';
import { Link, useNavigate, useSearchParams } from 'react-router-dom';
import { disposeSession, getSessions } from '../lib/api.js';
import { copyToClipboard, extractRepoName, formatRelativeTime } from '../lib/utils.js';
import { useToast } from '../components/ToastProvider.jsx';
import { useModal } from '../components/ModalProvider.jsx';
import { useConfig, useRequireConfig } from '../contexts/ConfigContext.jsx';
import SessionTableRow from '../components/SessionTableRow.jsx';
import WorkspaceTableRow from '../components/WorkspaceTableRow.jsx';

export default function SessionsPage() {
  const { config } = useConfig();
  useRequireConfig();
  const [workspaces, setWorkspaces] = useState([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState('');
  const [expanded, setExpanded] = useState({});
  const [searchParams, setSearchParams] = useSearchParams();
  const { success, error: toastError } = useToast();
  const { confirm } = useModal();
  const navigate = useNavigate();

  const filters = {
    status: searchParams.get('s') || '',
    repo: searchParams.get('r') || ''
  };

  const repoNames = useMemo(() => {
    return [...new Set(workspaces.map((ws) => extractRepoName(ws.repo)))].sort();
  }, [workspaces]);

  const filteredWorkspaces = useMemo(() => {
    return workspaces.filter((ws) => {
      if (filters.status) {
        const hasMatching = ws.sessions.some((s) =>
          filters.status === 'running' ? s.running : !s.running
        );
        if (!hasMatching) return false;
      }

      if (filters.repo && extractRepoName(ws.repo) !== filters.repo) {
        return false;
      }

      return true;
    });
  }, [workspaces, filters.repo, filters.status]);

  const loadWorkspaces = useCallback(async (options = {}) => {
    const { silent = false } = options;
    if (!silent) setLoading(true);
    setError('');
    try {
      const data = await getSessions();
      setWorkspaces(data);
      setExpanded((current) => {
        const next = { ...current };
        data.forEach((ws) => {
          if (next[ws.id] === undefined) {
            next[ws.id] = true;
          }
        });
        return next;
      });
    } catch (err) {
      setWorkspaces([]);
      setError(err.message || 'Failed to load workspaces');
    } finally {
      if (!silent) setLoading(false);
    }
  }, []);

  useEffect(() => {
    loadWorkspaces();
  }, [loadWorkspaces]);

  // Auto-refresh (silent mode - no flicker)
  useEffect(() => {
    const pollInterval = config.internal?.sessions_poll_interval_ms || 5000;
    const interval = setInterval(() => {
      loadWorkspaces({ silent: true });
    }, pollInterval);
    return () => clearInterval(interval);
  }, [loadWorkspaces, config]);

  const updateFilter = (key, value) => {
    const next = new URLSearchParams(searchParams);
    if (value) {
      next.set(key, value);
    } else {
      next.delete(key);
    }
    setSearchParams(next, { replace: true });
  };

  const toggleWorkspace = (id) => {
    setExpanded((current) => ({
      ...current,
      [id]: !current[id]
    }));
  };

  const expandAll = () => {
    const next = {};
    filteredWorkspaces.forEach((ws) => {
      next[ws.id] = true;
    });
    setExpanded(next);
  };

  const collapseAll = () => {
    const next = {};
    filteredWorkspaces.forEach((ws) => {
      next[ws.id] = false;
    });
    setExpanded(next);
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
      await loadWorkspaces();
    } catch (err) {
      toastError(`Failed to dispose: ${err.message}`);
    }
  };

  const showEmpty = filteredWorkspaces.length === 0 && !loading && !error;

  return (
    <>
      <div className="page-header">
        <h1 className="page-header__title">Sessions</h1>
        <div className="page-header__actions">
          <button className="btn btn--ghost" onClick={loadWorkspaces}>
            <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2">
              <path d="M23 4v6h-6"></path>
              <path d="M1 20v-6h6"></path>
              <path d="M3.51 9a9 9 0 0 1 14.85-3.36L23 10M1 14l4.64 4.36A9 9 0 0 0 20.49 15"></path>
            </svg>
            Refresh
          </button>
          <Link to="/spawn" className="btn btn--primary">
            <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2">
              <circle cx="12" cy="12" r="10"></circle>
              <line x1="12" y1="8" x2="12" y2="16"></line>
              <line x1="8" y1="12" x2="16" y2="12"></line>
            </svg>
            Spawn
          </Link>
        </div>
      </div>

      <div className="filter-bar">
        <span className="filter-bar__label">Filters:</span>
        <div className="filter-bar__filters">
          <select
            className="select"
            aria-label="Filter by status"
            value={filters.status}
            onChange={(event) => updateFilter('s', event.target.value)}
          >
            <option value="">All Status</option>
            <option value="running">Running</option>
            <option value="stopped">Stopped</option>
          </select>
          <select
            className="select"
            aria-label="Filter by repository"
            value={filters.repo}
            onChange={(event) => updateFilter('r', event.target.value)}
          >
            <option value="">All Repos</option>
            {repoNames.map((name) => (
              <option key={name} value={name}>{name}</option>
            ))}
          </select>
        </div>
      </div>

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

      <div className="workspace-list">
        {error && (
          <div className="empty-state">
            <div className="empty-state__icon">⚠️</div>
            <h3 className="empty-state__title">Failed to load workspaces</h3>
            <p className="empty-state__description">{error}</p>
            <button className="btn btn--primary" onClick={loadWorkspaces}>
              Try Again
            </button>
          </div>
        )}

        {showEmpty && (
          <div className="empty-state">
            <h3 className="empty-state__title">No sessions found</h3>
            <p className="empty-state__description">
              {filters.status || filters.repo ? 'Try adjusting your filters' : 'Get started by spawning your first sessions'}
            </p>
            {!(filters.status || filters.repo) ? (
              <Link to="/spawn" className="btn btn--primary">Spawn Sessions</Link>
            ) : null}
          </div>
        )}

        {filteredWorkspaces.map((ws) => {
          const sessions = filters.status
            ? ws.sessions.filter((s) => (filters.status === 'running' ? s.running : !s.running))
            : ws.sessions;

          return (
            <WorkspaceTableRow
              key={ws.id}
              workspace={ws}
              onToggle={() => toggleWorkspace(ws.id)}
              expanded={expanded[ws.id]}
              sessionCount={sessions.length}
              actions={
                <button
                  className="btn btn--sm"
                  onClick={(event) => {
                    event.stopPropagation();
                    navigate(`/spawn?workspace_id=${ws.id}`);
                  }}
                  aria-label={`Spawn in ${ws.id}`}
                >
                  <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2">
                    <circle cx="12" cy="12" r="10"></circle>
                    <line x1="12" y1="8" x2="12" y2="16"></line>
                    <line x1="8" y1="12" x2="16" y2="12"></line>
                  </svg>
                  Add Agent
                </button>
              }
              sessions={
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
                        onCopyAttach={handleCopyAttach}
                        onDispose={handleDispose}
                      />
                    ))}
                  </tbody>
                </table>
              }
            />
          );
        })}
      </div>
    </>
  );
}
