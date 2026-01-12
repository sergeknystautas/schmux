import React, { useEffect, useRef, useState } from 'react';
import { Link, useNavigate, useParams } from 'react-router-dom';
import '@xterm/xterm/css/xterm.css';
import TerminalStream from '../lib/terminalStream.js';
import { disposeSession, getSessions, updateNickname } from '../lib/api.js';
import { copyToClipboard, formatRelativeTime, formatTimestamp, truncateStart } from '../lib/utils.js';
import { useToast } from '../components/ToastProvider.jsx';
import { useModal } from '../components/ModalProvider.jsx';
import { useConfig } from '../contexts/ConfigContext.jsx';
import { useViewedSessions } from '../contexts/ViewedSessionsContext.jsx';
import Tooltip from '../components/Tooltip.jsx';
import useLocalStorage from '../hooks/useLocalStorage.js';
import WorkspacesList from '../components/WorkspacesList.jsx';

export default function SessionDetailPage() {
  const { sessionId } = useParams();
  const { config } = useConfig();
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState('');
  const [sessionData, setSessionData] = useState(null);
  const [wsStatus, setWsStatus] = useState('connecting');
  const [showResume, setShowResume] = useState(false);
  const [followTail, setFollowTail] = useState(true);
  const [sidebarCollapsed, setSidebarCollapsed] = useLocalStorage('sessionSidebarCollapsed', false);
  const terminalRef = useRef(null);
  const terminalStreamRef = useRef(null);
  const { success, error: toastError } = useToast();
  const { show, prompt } = useModal();
  const navigate = useNavigate();
  const { markAsViewed } = useViewedSessions();

  useEffect(() => {
    let active = true;

    const load = async () => {
      setLoading(true);
      setError('');
      try {
        const workspaces = await getSessions();
        let session = null;
        let workspaceId = null;
        for (const ws of workspaces) {
          const found = ws.sessions.find((s) => s.id === sessionId);
          if (found) {
            workspaceId = ws.id;
            session = {
              ...found,
              workspace_id: ws.id,
              workspace_path: ws.path,
              repo: ws.repo,
              branch: ws.branch
            };
            break;
          }
        }

        if (!active) return;
        if (!session) {
          setError('Session not found');
          setLoading(false);
          return;
        }

        setSessionData({ ...session, workspaceId });
        setLoading(false);

        // Mark session as viewed
        markAsViewed(sessionId);
      } catch (err) {
        if (!active) return;
        setError(`Failed to load session: ${err.message}`);
        setLoading(false);
      }
    };

    if (sessionId) {
      load();
    } else {
      setError('No session ID provided');
      setLoading(false);
    }

    return () => {
      active = false;
    };
  }, [sessionId]);

  useEffect(() => {
    if (!sessionData || !terminalRef.current) return;

    const terminalStream = new TerminalStream(sessionData.id, terminalRef.current, {
      followTail: true,
      onResume: (showing) => {
        setShowResume(showing);
        setFollowTail(!showing);
      },
      onStatusChange: (status) => setWsStatus(status)
    });

    terminalStreamRef.current = terminalStream;
    setFollowTail(true);

    terminalStream.initialized.then(() => {
      terminalStream.connect();
    });

    return () => {
      terminalStream.disconnect();
    };
  }, [sessionData?.id]);

  // Keep marking as viewed while WebSocket is connected (you're seeing output live)
  useEffect(() => {
    const seenInterval = config.internal?.session_seen_interval_ms || 2000;
    const interval = setInterval(() => {
      if (wsStatus === 'connected') {
        markAsViewed(sessionId);
      }
    }, seenInterval);

    return () => clearInterval(interval);
  }, [sessionId, wsStatus, markAsViewed, config]);

  const toggleSidebar = () => {
    const wasAtBottom = terminalStreamRef.current?.isAtBottom?.(10) || false;
    setSidebarCollapsed((prev) => !prev);
    setTimeout(() => {
      terminalStreamRef.current?.resizeTerminal?.();
      if (wasAtBottom) {
        terminalStreamRef.current?.terminal?.scrollToBottom?.();
      }
    }, 250);
  };

  const handleCopyAttach = async () => {
    if (!sessionData) return;
    const ok = await copyToClipboard(sessionData.attach_cmd);
    if (ok) {
      success('Copied attach command');
    } else {
      toastError('Failed to copy');
    }
  };

  const handleDispose = async () => {
    const accepted = await show(
      'Dispose Session',
      `Dispose session ${sessionId}?`,
      {
        danger: true,
        confirmText: 'Dispose',
        detailedMessage: 'This will stop tracking the session and clean up resources. The tmux session will remain available if you need to attach manually.'
      }
    );
    if (!accepted) return;

    try {
      await disposeSession(sessionId);
      success('Session disposed');
      navigate('/sessions');
    } catch (err) {
      toastError(`Failed to dispose: ${err.message}`);
    }
  };

  const handleFollowChange = (event) => {
    const follow = event.target.checked;
    if (terminalStreamRef.current) {
      terminalStreamRef.current.setFollow(follow);
      if (follow) terminalStreamRef.current.jumpToBottom();
    }
    setFollowTail(follow);
  };

  const handleEditNickname = async () => {
    let newNickname = sessionData.nickname || '';
    let errorMessage = '';

    // Keep prompting until successful or cancelled
    while (true) {
      newNickname = await prompt('Edit Nickname', {
        defaultValue: newNickname,
        placeholder: 'Enter nickname (optional)',
        confirmText: 'Save',
        errorMessage
      });

      if (newNickname === null) return; // User cancelled

      try {
        await updateNickname(sessionId, newNickname);
        success('Nickname updated');
        // Refresh session data to show updated nickname
        setSessionData(prev => ({ ...prev, nickname: newNickname }));
        return; // Success, exit loop
      } catch (err) {
        if (err.isConflict) {
          // Show error and re-prompt
          errorMessage = err.message;
        } else {
          toastError(`Failed to update nickname: ${err.message}`);
          return; // Other errors, don't re-prompt
        }
      }
    }
  };

  const handleSessionCopyAttach = async (command) => {
    const ok = await copyToClipboard(command);
    if (ok) {
      success('Copied attach command');
    } else {
      toastError('Failed to copy');
    }
  };

  if (loading) {
    return (
      <div className="loading-state">
        <div className="spinner"></div>
        <span>Loading session...</span>
      </div>
    );
  }

  if (error) {
    return (
      <div className="empty-state">
        <div className="empty-state__icon">⚠️</div>
        <h3 className="empty-state__title">Error</h3>
        <p className="empty-state__description">{error}</p>
        <Link to="/sessions" className="btn btn--primary">Back to Sessions</Link>
      </div>
    );
  }

  const statusClass = sessionData.running ? 'status-pill--running' : 'status-pill--stopped';
  const statusText = sessionData.running ? 'Running' : 'Stopped';
  const titleText = sessionData.nickname || sessionData.id.substring(0, 12);
  const wsPillClass = wsStatus === 'connected'
    ? 'connection-pill--connected'
    : wsStatus === 'disconnected'
      ? 'connection-pill--offline'
      : 'connection-pill--reconnecting';
  const wsPillText = wsStatus === 'connected' ? 'Live' : wsStatus === 'disconnected' ? 'Offline' : 'Connecting...';

  return (
    <>
      {sessionData && (
        <WorkspacesList
          workspaceId={sessionData.workspaceId}
          currentSessionId={sessionId}
          showControls={false}
        />
      )}

      <div className="page-header">
        <h1 className="page-header__title">Session <span className="mono">{titleText}</span></h1>
        <div className="page-header__actions">
          <Tooltip content="Toggle sidebar">
            <button className="btn btn--ghost" onClick={toggleSidebar}>
              <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2">
                <rect x="3" y="3" width="18" height="18" rx="2" ry="2"></rect>
                <line x1="9" y1="3" x2="9" y2="21"></line>
              </svg>
            </button>
          </Tooltip>
        </div>
      </div>

      <div className={`session-detail${sidebarCollapsed ? ' session-detail--sidebar-collapsed' : ''}`}>
        <div className="session-detail__main">
          <div className="log-viewer">
            <div className="log-viewer__header">
              <div className="log-viewer__info">
                <div className={`connection-pill ${wsPillClass}`}>
                  <span className="connection-pill__dot"></span>
                  <span>{wsPillText}</span>
                </div>
                <div className={`status-pill ${wsStatus === 'connected' ? statusClass : ''}`}>
                  <span className="status-pill__dot"></span>
                  <span>{wsStatus === 'connected' ? statusText : ''}</span>
                </div>
              </div>
              <div className="log-viewer__actions">
                <label className="toggle-switch">
                  <input type="checkbox" checked={followTail} onChange={handleFollowChange} />
                  <span>Follow</span>
                </label>
                <Tooltip content="Download log">
                  <button
                    className="btn btn--sm"
                    onClick={() => {
                      terminalStreamRef.current?.downloadOutput();
                      success('Downloaded session log');
                    }}
                  >
                    <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2">
                      <path d="M21 15v4a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2v-4"></path>
                      <polyline points="7 10 12 15 17 10"></polyline>
                      <line x1="12" y1="15" x2="12" y2="3"></line>
                    </svg>
                  </button>
                </Tooltip>
              </div>
            </div>
            <div id="terminal" className="log-viewer__output" ref={terminalRef}></div>
            {showResume ? (
              <button className="log-viewer__new-content" onClick={() => terminalStreamRef.current?.jumpToBottom()}>
                Resume
              </button>
            ) : null}
          </div>
        </div>

        <aside className="session-detail__sidebar">
          <div className="metadata-field">
            <span className="metadata-field__label">Session ID</span>
            <span className="metadata-field__value metadata-field__value--mono">{sessionData.id}</span>
          </div>

          <div className="metadata-field">
            <span className="metadata-field__label">Agent</span>
            <span className="metadata-field__value">{sessionData.agent}</span>
          </div>

          {sessionData.nickname ? (
            <div className="metadata-field">
              <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between', width: '100%' }}>
                <span className="metadata-field__label">Nickname</span>
                <Tooltip content="Edit nickname">
                  <button className="btn btn--sm btn--ghost" onClick={handleEditNickname}>
                    <svg width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2">
                      <path d="M11 4H4a2 2 0 0 0-2 2v14a2 2 0 0 0 2 2h14a2 2 0 0 0 2-2v-7"></path>
                      <path d="M18.5 2.5a2.121 2.121 0 0 1 3 3L12 15l-4 1 1-4 9.5-9.5z"></path>
                    </svg>
                  </button>
                </Tooltip>
              </div>
              <span className="metadata-field__value">{sessionData.nickname}</span>
            </div>
          ) : (
            <div className="metadata-field">
              <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between', width: '100%' }}>
                <span className="metadata-field__label">Nickname</span>
                <Tooltip content="Add nickname">
                  <button className="btn btn--sm btn--ghost" onClick={handleEditNickname}>
                    <svg width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2">
                      <line x1="12" y1="5" x2="12" y2="19"></line>
                      <line x1="5" y1="12" x2="19" y2="12"></line>
                    </svg>
                  </button>
                </Tooltip>
              </div>
              <span className="metadata-field__value" style={{ color: 'var(--color-text-muted)', fontStyle: 'italic' }}>Not set</span>
            </div>
          )}

          <div className="metadata-field">
            <span className="metadata-field__label">Created</span>
            <Tooltip content={formatTimestamp(sessionData.created_at)}>
              <span className="metadata-field__value" style={{ alignSelf: 'flex-start' }}>{formatRelativeTime(sessionData.created_at)}</span>
            </Tooltip>
          </div>

          <div className="metadata-field">
            <span className="metadata-field__label">Last Activity</span>
            <Tooltip content={sessionData.last_output_at ? formatTimestamp(sessionData.last_output_at) : 'Never'}>
              <span className="metadata-field__value" style={{ alignSelf: 'flex-start' }}>
                {sessionData.last_output_at ? formatRelativeTime(sessionData.last_output_at) : 'Never'}
              </span>
            </Tooltip>
          </div>

          <div className="metadata-field">
            <span className="metadata-field__label">Status</span>
            <div>
              <span className={`status-pill ${statusClass}`}>
                <span className="status-pill__dot"></span>
                {statusText}
              </span>
            </div>
          </div>

          <hr style={{ border: 'none', borderTop: '1px solid var(--color-border)', margin: 'var(--spacing-md) 0' }} />

          <div className="form-group">
            <label className="form-group__label">Attach Command</label>
            <div className="copy-field">
              <span className="copy-field__value">{sessionData.attach_cmd}</span>
              <Tooltip content="Copy attach command">
                <button className="copy-field__btn" onClick={handleCopyAttach}>
                  <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2">
                    <rect x="9" y="9" width="13" height="13" rx="2" ry="2"></rect>
                    <path d="M5 15H4a2 2 0 0 1-2-2V4a2 2 0 0 1 2-2h9a2 2 0 0 1 2 2v1"></path>
                  </svg>
                </button>
              </Tooltip>
            </div>
          </div>

          <div style={{ marginTop: 'auto' }}>
            <button className="btn btn--danger" style={{ width: '100%' }} onClick={handleDispose}>
              <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2">
                <polyline points="3 6 5 6 21 6"></polyline>
                <path d="M19 6v14a2 2 0 0 1-2 2H7a2 2 0 0 1-2-2V6m3 0V4a2 2 0 0 1 2-2h4a2 2 0 0 1 2 2v2"></path>
              </svg>
              Dispose Session
            </button>
          </div>
        </aside>
      </div>
    </>
  );
}
