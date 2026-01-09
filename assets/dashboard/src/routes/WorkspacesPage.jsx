import React, { useCallback, useEffect, useState } from 'react';
import { Link, useNavigate } from 'react-router-dom';
import { disposeSession, disposeWorkspace, getSessions, getWorkspaces, scanWorkspaces } from '../lib/api.js';
import { copyToClipboard, extractRepoName, formatRelativeTime } from '../lib/utils.js';
import { useToast } from '../components/ToastProvider.jsx';
import { useModal } from '../components/ModalProvider.jsx';
import { useConfig, useRequireConfig } from '../contexts/ConfigContext.jsx';
import SessionTableRow from '../components/SessionTableRow.jsx';
import WorkspaceTableRow from '../components/WorkspaceTableRow.jsx';
import Tooltip from '../components/Tooltip.jsx';
import ScanResultsModal from '../components/ScanResultsModal.jsx';

export default function WorkspacesPage() {
  const { config } = useConfig();
  useRequireConfig();
  const [workspaces, setWorkspaces] = useState([]);
  const [sessionsByWorkspace, setSessionsByWorkspace] = useState({});
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState('');
  const [expanded, setExpanded] = useState({});
  const [scanResult, setScanResult] = useState(null);
  const [scanning, setScanning] = useState(false);
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

  // Auto-refresh (silent mode - no flicker)
  useEffect(() => {
    const pollInterval = config.internal?.sessions_poll_interval_ms || 5000;
    const interval = setInterval(() => {
      loadWorkspaces({ silent: true });
    }, pollInterval);
    return () => clearInterval(interval);
  }, [loadWorkspaces, config]);

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

  const handleDisposeWorkspace = async (workspaceId) => {
    const accepted = await confirm(`Dispose workspace ${workspaceId}?`, { danger: true });
    if (!accepted) return;
    try {
      await disposeWorkspace(workspaceId);
      success('Workspace disposed');
      await loadWorkspaces();
    } catch (err) {
      toastError(`Failed to dispose workspace: ${err.message}`);
    }
  };

  const handleScan = async () => {
    setScanning(true);
    setError('');
    try {
      const result = await scanWorkspaces();
      await loadWorkspaces();
      setScanResult(result);
    } catch (err) {
      toastError(`Failed to scan workspaces: ${err.message}`);
    } finally {
      setScanning(false);
    }
  };

  const empty = workspaces.length === 0 && !loading && !error;

  return (
    <>
      <div className="page-header">
        <h1 className="page-header__title">Workspaces</h1>
        <div className="page-header__actions">
          <button className="btn btn--ghost" onClick={handleScan} disabled={scanning}>
            <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2">
              <circle cx="12" cy="12" r="10"></circle>
              <line x1="12" y1="8" x2="12" y2="12"></line>
              <line x1="12" y1="16" x2="12.01" y2="16"></line>
            </svg>
            {scanning ? 'Scanning...' : 'Scan'}
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

          return (
            <WorkspaceTableRow
              key={ws.id}
              workspace={ws}
              onToggle={() => setExpanded((curr) => ({ ...curr, [ws.id]: !curr[ws.id] }))}
              expanded={expanded[ws.id]}
              sessionCount={sessionCount}
              actions={
                <>
                  <Tooltip content="View git diff">
                    <button
                      className="btn btn--sm btn--ghost"
                      onClick={(event) => {
                        event.stopPropagation();
                        navigate(`/diff/${ws.id}`);
                      }}
                      aria-label={`View diff for ${ws.id}`}
                    >
                      <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2">
                        <path d="M11 4H4a2 2 0 0 0-2 2v14a2 2 0 0 0 2 2h14a2 2 0 0 0 2-2v-7"></path>
                        <path d="M18.5 2.5a2.121 2.121 0 0 1 3 3L12 15l-4 1 1-4 9.5-9.5z"></path>
                      </svg>
                      Diff
                    </button>
                  </Tooltip>
                  <Tooltip content="Spawn session in this workspace">
                    <button
                      className="btn btn--sm btn--primary"
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
                      Spawn
                    </button>
                  </Tooltip>
                  <Tooltip content="Dispose workspace and all sessions" variant="warning">
                    <button
                      className="btn btn--sm btn--ghost btn--danger"
                      onClick={(event) => {
                        event.stopPropagation();
                        handleDisposeWorkspace(ws.id);
                      }}
                      aria-label={`Dispose ${ws.id}`}
                    >
                      <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2">
                        <polyline points="3 6 5 6 21 6"></polyline>
                        <path d="M19 6v14a2 2 0 0 1-2 2H7a2 2 0 0 1-2-2V6m3 0V4a2 2 0 0 1 2-2h4a2 2 0 0 1 2 2v2"></path>
                      </svg>
                      Dispose
                    </button>
                  </Tooltip>
                </>
              }
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

      {scanResult && (
        <ScanResultsModal
          result={scanResult}
          onClose={() => setScanResult(null)}
        />
      )}
    </>
  );
}
