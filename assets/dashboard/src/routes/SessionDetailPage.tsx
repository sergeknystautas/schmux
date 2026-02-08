import React, { useCallback, useEffect, useRef, useState } from 'react';
import { Link, useParams, useNavigate } from 'react-router-dom';
import '@xterm/xterm/css/xterm.css';
import TerminalStream from '../lib/terminalStream';
import { updateNickname, disposeSession, getErrorMessage } from '../lib/api';
import { copyToClipboard, formatRelativeTime, formatTimestamp } from '../lib/utils';
import { useToast } from '../components/ToastProvider';
import { useModal } from '../components/ModalProvider';
import { useConfig } from '../contexts/ConfigContext';
import { useSessions } from '../contexts/SessionsContext';
import { useViewedSessions } from '../contexts/ViewedSessionsContext';
import { useKeyboardMode } from '../contexts/KeyboardContext';
import Tooltip from '../components/Tooltip';
import useLocalStorage, { SESSION_SIDEBAR_COLLAPSED_KEY } from '../hooks/useLocalStorage';
import WorkspaceHeader from '../components/WorkspaceHeader';
import SessionTabs from '../components/SessionTabs';

export default function SessionDetailPage() {
  const { sessionId } = useParams();
  const { config, loading: configLoading } = useConfig();
  const { sessionsById, workspaces, loading: sessionsLoading, error: sessionsError } = useSessions();
  const navigate = useNavigate();
  const [wsStatus, setWsStatus] = useState<'connecting' | 'connected' | 'disconnected' | 'reconnecting' | 'error'>('connecting');
  const [showResume, setShowResume] = useState(false);
  const [followTail, setFollowTail] = useState(true);
  const [sidebarCollapsed, setSidebarCollapsed] = useLocalStorage<boolean>(SESSION_SIDEBAR_COLLAPSED_KEY, false);
  const [workspaceId, setWorkspaceId] = useState<string | null>(null);
  const [selectionMode, setSelectionMode] = useState(false);
  const [selectedLines, setSelectedLines] = useState<string[]>([]);
  const terminalRef = useRef<HTMLDivElement | null>(null);
  const terminalStreamRef = useRef<TerminalStream | null>(null);
  const { success, error: toastError } = useToast();
  const { prompt, confirm } = useModal();
  const { markAsViewed } = useViewedSessions();
  const { registerAction, unregisterAction } = useKeyboardMode();

  const sessionData = sessionId ? sessionsById[sessionId] : null;
  const sessionMissing = !sessionsLoading && !sessionsError && sessionId && !sessionData;
  const workspaceExists = workspaceId && workspaces?.some(ws => ws.id === workspaceId);

  // Remember the workspace_id so we can filter after dispose
  useEffect(() => {
    if (sessionData?.workspace_id) {
      setWorkspaceId(sessionData.workspace_id);
    }
  }, [sessionData?.workspace_id]);

  // If session is missing and we don't have a stored workspaceId, navigate to home
  useEffect(() => {
    if (sessionMissing && !workspaceId) {
      navigate('/');
    }
  }, [sessionMissing, workspaceId, navigate]);

  // If session is missing and workspace was disposed, navigate to home
  useEffect(() => {
    if (sessionMissing && workspaceId && !workspaceExists) {
      navigate('/');
    }
  }, [sessionMissing, workspaceId, workspaceExists, navigate]);

  useEffect(() => {
    if (sessionData?.id) {
      markAsViewed(sessionData.id);
    }
  }, [sessionData?.id, markAsViewed]);

  useEffect(() => {
    if (!sessionData || !terminalRef.current) return;
    if (configLoading) return;
    if (!config?.terminal || typeof config.terminal.width !== 'number' || typeof config.terminal.height !== 'number') {
      return;
    }

    const terminalStream = new TerminalStream(sessionData.id, terminalRef.current, {
      followTail: true,
      terminalSize: config?.terminal || null,
      onResume: (showing) => {
        setShowResume(showing);
        setFollowTail(!showing);
      },
      onStatusChange: (status) => setWsStatus(status),
      onSelectedLinesChange: (lines) => setSelectedLines(lines)
    });

    terminalStreamRef.current = terminalStream;
    setFollowTail(true);

    terminalStream.initialized.then(() => {
      terminalStream.connect();
      terminalStream.focus();
    });

    return () => {
      terminalStream.disconnect();
    };
  }, [sessionData?.id, configLoading, config?.terminal]);

  useEffect(() => {
    if (!sessionData?.id) return;
    setWsStatus('connecting');
    setShowResume(false);
    setFollowTail(true);
    // Reset selection mode when switching sessions
    setSelectionMode(false);
    setSelectedLines([]);
  }, [sessionData?.id]);

  // Keep marking as viewed while WebSocket is connected (you're seeing output live)
  useEffect(() => {
    const seenInterval = config.nudgenik?.seen_interval_ms || 2000;
    const interval = setInterval(() => {
      if (wsStatus === 'connected') {
        if (sessionId) {
          markAsViewed(sessionId);
        }
      }
    }, seenInterval);

    return () => clearInterval(interval);
  }, [sessionId, markAsViewed]); // Only depends on stable markAsViewed

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

  const handleDispose = useCallback(async () => {
    if (!sessionId) return;

    const sessionDisplay = sessionData?.nickname
      ? `${sessionData.nickname} (${sessionId})`
      : sessionId;

    const accepted = await confirm(`Dispose session ${sessionDisplay}?`, { danger: true });
    if (!accepted) return;

    try {
      await disposeSession(sessionId);
      success('Session disposed');
    } catch (err) {
      toastError(`Failed to dispose: ${getErrorMessage(err, 'Unknown error')}`);
    }
  }, [sessionId, sessionData?.nickname, confirm, success, toastError]);

  // Register keyboard shortcut for dispose (W key)
  useEffect(() => {
    if (!sessionId) return;
    const scope = { type: 'session', id: sessionId } as const;
    const action = {
      key: 'w',
      description: 'Dispose session',
      handler: handleDispose,
      scope,
    };

    registerAction(action);

    return () => unregisterAction('w', false, scope);
  }, [registerAction, unregisterAction, handleDispose, sessionId]);

  const handleEditNickname = async () => {
    if (!sessionId || !sessionData) return;
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
        return; // Success, exit loop
      } catch (err) {
        if ((err as { isConflict?: boolean }).isConflict) {
          // Show error and re-prompt
          errorMessage = getErrorMessage(err, 'Nickname conflict');
        } else {
          toastError(`Failed to update nickname: ${getErrorMessage(err, 'Unknown error')}`);
          return; // Other errors, don't re-prompt
        }
      }
    }
  };

  const handleToggleSelectionMode = () => {
    const newMode = terminalStreamRef.current?.toggleSelectionMode() ?? false;
    setSelectionMode(newMode);
  };

  const handleCancelSelection = () => {
    terminalStreamRef.current?.toggleSelectionMode(); // This will clear selection
    setSelectionMode(false);
    setSelectedLines([]);
  };

  const handleCopySelectedLines = async () => {
    if (selectedLines.length === 0) {
      toastError('No lines selected');
      return;
    }
    const content = selectedLines.join('\n');
    const ok = await copyToClipboard(content);
    if (ok) {
      success(`Copied ${selectedLines.length} line${selectedLines.length !== 1 ? 's' : ''}`);
      // Exit selection mode after successful copy
      handleCancelSelection();
    } else {
      toastError('Failed to copy');
    }
  };

  if (sessionsLoading && !sessionData && !sessionsError) {
    return (
      <div className="loading-state">
        <div className="spinner"></div>
        <span>Loading session...</span>
      </div>
    );
  }

  if (sessionsError || !sessionId) {
    const message = !sessionId
      ? 'No session ID provided'
      : `Failed to load session: ${sessionsError}`;
    return (
      <div className="empty-state">
        <div className="empty-state__icon">⚠️</div>
        <h3 className="empty-state__title">Error</h3>
        <p className="empty-state__description">{message}</p>
        <Link to="/" className="btn btn--primary">Back to Home</Link>
      </div>
    );
  }

  // Get the current workspace data
  const currentWorkspace = workspaces?.find(ws => ws.id === (sessionData?.workspace_id || workspaceId));

  if (sessionMissing) {
    // No workspaceId means we lost state (e.g., page refresh) - navigate away
    if (!workspaceId) {
      return null;
    }

    // Workspace was disposed (no longer in global state) - navigate home
    if (!workspaceExists) {
      return null;
    }

    return (
      <>
        {currentWorkspace && (
          <>
            <WorkspaceHeader workspace={currentWorkspace} />
            <SessionTabs sessions={currentWorkspace.sessions || []} workspace={currentWorkspace} />
          </>
        )}
        <div className="empty-state">
          <div className="empty-state__icon">⚠️</div>
          <h3 className="empty-state__title">Session unavailable</h3>
          <p className="empty-state__description">This session was disposed or no longer exists. Select another session from the tabs above.</p>
        </div>
      </>
    );
  }

  const statusClass = sessionData.running ? 'status-pill--running' : 'status-pill--stopped';
  const statusText = sessionData.running ? 'Running' : 'Stopped';
  const wsPillClass = wsStatus === 'connected'
    ? 'connection-pill--connected'
    : wsStatus === 'disconnected'
      ? 'connection-pill--offline'
      : 'connection-pill--reconnecting';
  const wsPillText = wsStatus === 'connected' ? 'Live' : wsStatus === 'disconnected' ? 'Offline' : 'Connecting...';

  return (
    <>
      {currentWorkspace && (
        <>
          <WorkspaceHeader workspace={currentWorkspace} />
          <SessionTabs sessions={currentWorkspace.sessions || []} currentSessionId={sessionId} workspace={currentWorkspace} />
        </>
      )}

      <div className={`session-detail${sidebarCollapsed ? ' session-detail--sidebar-collapsed' : ''}`}>
        <div className="session-detail__main">
          <div className="log-viewer">
            <div className="log-viewer__header">
              <div className="log-viewer__info">
                <Tooltip content={wsStatus === 'connected' ? 'WebSocket connected - receiving real-time terminal output' : wsStatus === 'disconnected' ? 'WebSocket disconnected - unable to receive terminal output' : 'WebSocket connecting...'}>
                  <div className={`connection-pill ${wsPillClass}`}>
                    <span className="connection-pill__dot"></span>
                    <span>{wsPillText}</span>
                  </div>
                </Tooltip>
                <Tooltip content={sessionData.running ? 'Agent process is running' : 'Agent process has stopped'}>
                  <div className={`status-pill ${statusClass}`}>
                    <span className="status-pill__dot"></span>
                    <span>{statusText}</span>
                  </div>
                </Tooltip>
              </div>
              <div className="log-viewer__actions">
                {selectionMode ? (
                  <>
                    <Tooltip content={`Copy ${selectedLines.length} selected line${selectedLines.length !== 1 ? 's' : ''}`}>
                      <button
                        className="btn btn--sm btn--primary"
                        onClick={handleCopySelectedLines}
                        disabled={selectedLines.length === 0}
                      >
                        <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2">
                          <rect x="9" y="9" width="13" height="13" rx="2" ry="2"></rect>
                          <path d="M5 15H4a2 2 0 0 1-2-2V4a2 2 0 0 1 2-2h9a2 2 0 0 1 2 2v1"></path>
                        </svg>
                        <span>Copy</span>
                      </button>
                    </Tooltip>
                    <Tooltip content="Cancel selection">
                      <button
                        className="btn btn--sm"
                        onClick={handleCancelSelection}
                      >
                        Cancel
                      </button>
                    </Tooltip>
                  </>
                ) : (
                  <Tooltip content="Select lines to copy">
                    <button
                      className="btn btn--sm"
                      onClick={handleToggleSelectionMode}
                    >
                      Select lines
                    </button>
                  </Tooltip>
                )}
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
                <Tooltip content="Toggle sidebar">
                  <button className="btn btn--sm" onClick={toggleSidebar}>
                    <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2">
                      <rect x="3" y="3" width="18" height="18" rx="2" ry="2"></rect>
                      <line x1="9" y1="3" x2="9" y2="21"></line>
                    </svg>
                  </button>
                </Tooltip>
              </div>
            </div>
            <div
              key={sessionData.id}
              id="terminal"
              className="log-viewer__output"
              ref={terminalRef}
              style={{ cursor: selectionMode ? 'pointer' : undefined }}
            ></div>

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
            <span className="metadata-field__label">Branch</span>
            <span className="metadata-field__value metadata-field__value--mono">{sessionData.branch}</span>
          </div>

          <div className="metadata-field">
            <span className="metadata-field__label">Target</span>
            <span className="metadata-field__value">{sessionData.target}</span>
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

          {sessionData.remote_host_id && (
            <>
              <hr style={{ border: 'none', borderTop: '1px solid var(--color-border)', margin: 'var(--spacing-md) 0' }} />
              <div className="metadata-field">
                <span className="metadata-field__label">Environment</span>
                <span className="metadata-field__value" style={{ display: 'flex', alignItems: 'center', gap: 'var(--spacing-xs)' }}>
                  <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2">
                    <rect x="1" y="4" width="22" height="16" rx="2" ry="2" />
                    <line x1="1" y1="10" x2="23" y2="10" />
                  </svg>
                  {sessionData.remote_flavor_name || 'Remote'}
                </span>
              </div>
              {sessionData.remote_hostname && (
                <div className="metadata-field">
                  <span className="metadata-field__label">Hostname</span>
                  <span className="metadata-field__value metadata-field__value--mono" style={{ fontSize: '0.75rem' }}>
                    {sessionData.remote_hostname}
                  </span>
                </div>
              )}
            </>
          )}

          <hr style={{ border: 'none', borderTop: '1px solid var(--color-border)', margin: 'var(--spacing-md) 0' }} />

          <div className="form-group">
            <label className="form-group__label">Attach Command</label>
            {sessionData.remote_host_id ? (
              <div style={{ fontSize: '0.875rem', color: 'var(--color-text-muted)' }}>
                Remote session on {sessionData.remote_hostname || 'remote host'}
                {sessionData.remote_pane_id && (
                  <div style={{ marginTop: 'var(--spacing-xs)', fontFamily: 'var(--font-mono)', fontSize: '0.75rem' }}>
                    Pane: {sessionData.remote_pane_id}
                  </div>
                )}
              </div>
            ) : (
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
            )}
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
