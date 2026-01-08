import React, { useCallback, useEffect, useState } from 'react';
import { Link, useNavigate } from 'react-router-dom';
import { disposeSession, getSessions, getWorkspaces } from '../lib/api.js';
import { copyToClipboard, extractRepoName } from '../lib/utils.js';
import { useToast } from '../components/ToastProvider.jsx';
import { useModal } from '../components/ModalProvider.jsx';
import SessionTableRow from '../components/SessionTableRow.jsx';

export default function WorkspacesPage() {
  const [workspaces, setWorkspaces] = useState([]);
  const [sessionsByWorkspace, setSessionsByWorkspace] = useState({});
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState('');
  const [expanded, setExpanded] = useState({});
  const { success, error: toastError } = useToast();
  const { confirm } = useModal();
  const navigate = useNavigate();

  const loadWorkspaces = useCallback(async (options = {}) => {
    const { silent = false } = options;
    if (!silent) setLoading(true);
    setError('');
    try {
      const [workspaceData, sessionData] = await Promise.all([
        getWorkspaces(),
        getSessions()
      ]);

      setWorkspaces(workspaceData);
      const map = {};
      sessionData.forEach((ws) => {
        map[ws.id] = ws.sessions;
      });
      setSessionsByWorkspace(map);
      setExpanded((current) => {
        const next = { ...current };
        workspaceData.forEach((ws) => {
          if (next[ws.id] === undefined) {
            next[ws.id] = true;
          }
        });
        return next;
      });
    } catch (err) {
      setError(err.message || 'Failed to load workspaces');
    } finally {
      if (!silent) setLoading(false);
    }
  }, []);

  useEffect(() => {
    loadWorkspaces();
  }, [loadWorkspaces]);

  const expandAll = () => {
    const next = {};
    workspaces.forEach((ws) => {
      next[ws.id] = true;
    });
    setExpanded(next);
  };

  const collapseAll = () => {
    const next = {};
    workspaces.forEach((ws) => {
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
    const accepted = await confirm(`Dispose session ${sessionId}?`);
    if (!accepted) return;
    try {
      await disposeSession(sessionId);
      success('Session disposed');
      await loadWorkspaces();
    } catch (err) {
      toastError(`Failed to dispose: ${err.message}`);
    }
  };

  const empty = workspaces.length === 0 && !loading && !error;

  return (
    <>
      <div className="page-header">
        <h1 className="page-header__title">Workspaces</h1>
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
            <button className="btn btn--primary" onClick={loadWorkspaces}>
              Try Again
            </button>
          </div>
        )}

        {empty && (
          <div className="empty-state">
            <h3 className="empty-state__title">No workspaces found</h3>
            <p className="empty-state__description">Workspaces will appear here when you spawn sessions</p>
            <Link to="/spawn" className="btn btn--primary">Spawn Sessions</Link>
          </div>
        )}

        {workspaces.map((ws) => {
          const sessions = sessionsByWorkspace[ws.id] || [];
          const sessionCount = sessions.length;
          const repoName = extractRepoName(ws.repo);

          return (
            <div className="workspace-item" key={ws.id}>
              <div className="workspace-item__header" onClick={() => setExpanded((curr) => ({ ...curr, [ws.id]: !curr[ws.id] }))}>
                <div className="workspace-item__info">
                  <span className={`workspace-item__toggle${expanded[ws.id] ? '' : ' workspace-item__toggle--collapsed'}`}>
                    <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2">
                      <polyline points="6 9 12 15 18 9"></polyline>
                    </svg>
                  </span>
                  <span className="workspace-item__name">{ws.id}</span>
                  <span className="workspace-item__meta">{repoName} · {ws.branch}</span>
                  <span className="badge badge--neutral">{sessionCount} session{sessionCount !== 1 ? 's' : ''}</span>
                </div>
                <button
                  className="btn btn--sm"
                  onClick={(event) => {
                    event.stopPropagation();
                    navigate(`/spawn?workspace_id=${ws.id}`);
                  }}
                  aria-label={`Add agent to ${ws.id}`}
                >
                  <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2">
                    <circle cx="12" cy="12" r="10"></circle>
                    <line x1="12" y1="8" x2="12" y2="16"></line>
                    <line x1="8" y1="12" x2="16" y2="12"></line>
                  </svg>
                  Add Agent
                </button>
              </div>

              <div className={`workspace-item__sessions${expanded[ws.id] ? ' workspace-item__sessions--expanded' : ''}`}>
                {sessionCount > 0 ? (
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
                          showSessionId={true}
                        />
                      ))}
                    </tbody>
                  </table>
                ) : (
                  <p style={{ padding: '1rem', color: 'var(--color-text-subtle)' }}>No sessions in this workspace</p>
                )}
              </div>
            </div>
          );
        })}
      </div>
    </>
  );
}
