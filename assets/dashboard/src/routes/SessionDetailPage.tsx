import React, { useEffect, useRef, useState } from 'react';
import { Link, useParams, useNavigate } from 'react-router-dom';
import '@xterm/xterm/css/xterm.css';
import TerminalStream from '../lib/terminalStream';
import { updateNickname } from '../lib/api';
import { copyToClipboard, formatRelativeTime, formatTimestamp } from '../lib/utils';
import { useToast } from '../components/ToastProvider';
import { useModal } from '../components/ModalProvider';
import { useConfig } from '../contexts/ConfigContext';
import { useSessions } from '../contexts/SessionsContext';
import { useViewedSessions } from '../contexts/ViewedSessionsContext';
import Tooltip from '../components/Tooltip';
import useLocalStorage from '../hooks/useLocalStorage';
import WorkspacesList, { type WorkspacesListHandle } from '../components/WorkspacesList';
import type { NudgenikResult } from '../lib/types';

export default function SessionDetailPage() {
  const { sessionId } = useParams();
  const { config, loading: configLoading } = useConfig();
  const { sessionsById, workspaces, loading: sessionsLoading, error: sessionsError, refresh } = useSessions();
  const navigate = useNavigate();
  const [wsStatus, setWsStatus] = useState<'connecting' | 'connected' | 'disconnected' | 'reconnecting' | 'error'>('connecting');
  const [showResume, setShowResume] = useState(false);
  const [followTail, setFollowTail] = useState(true);
  const [sidebarCollapsed, setSidebarCollapsed] = useLocalStorage<boolean>('sessionSidebarCollapsed', false);
  const [nudgenikLoading, setNudgenikLoading] = useState(false);
  const [nudgenikResult, setNudgenikResult] = useState<NudgenikResult | null>(null);
  const [workspaceId, setWorkspaceId] = useState<string | null>(null);
  const terminalRef = useRef<HTMLDivElement | null>(null);
  const terminalStreamRef = useRef<TerminalStream | null>(null);
  const workspacesListRef = useRef<WorkspacesListHandle | null>(null);
  const { success, error: toastError } = useToast();
  const { prompt } = useModal();
  const { markAsViewed } = useViewedSessions();

  const sessionData = sessionId ? sessionsById[sessionId] : null;
  const sessionMissing = !sessionsLoading && !sessionsError && sessionId && !sessionData;
  const workspaceExists = workspaceId && workspaces?.some(ws => ws.id === workspaceId);

  // Remember the workspace_id so we can filter after dispose
  useEffect(() => {
    if (sessionData?.workspace_id) {
      setWorkspaceId(sessionData.workspace_id);
    }
  }, [sessionData?.workspace_id]);

  // If session is missing and we don't have a stored workspaceId, navigate to sessions
  useEffect(() => {
    if (sessionMissing && !workspaceId) {
      navigate('/sessions');
    }
  }, [sessionMissing, workspaceId, navigate]);

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
  }, [sessionData?.id, configLoading, config?.terminal]);

  useEffect(() => {
    if (!sessionData?.id) return;
    setWsStatus('connecting');
    setShowResume(false);
    setFollowTail(true);
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

  const handleDispose = () => {
    if (!sessionId) return;
    workspacesListRef.current?.disposeSession(sessionId);
  };

  const handleFollowChange = (event: React.ChangeEvent<HTMLInputElement>) => {
    const follow = event.target.checked;
    if (terminalStreamRef.current) {
      terminalStreamRef.current.setFollow(follow);
      if (follow) terminalStreamRef.current.jumpToBottom();
    }
    setFollowTail(follow);
  };

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
        // Refresh session data to show updated nickname
        refresh(true);
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

  const handleAskNudgenik = async () => {
    if (!sessionId) return;
    setNudgenikLoading(true);
    try {
      const resp = await fetch(`/api/askNudgenik/${sessionId}`);
      if (!resp.ok) {
        throw new Error(`Failed to ask NudgeNik: ${resp.status}`);
      }
      const data = await resp.json();
      setNudgenikResult(data || null);
    } catch (err) {
      toastError(`Failed to ask NudgeNik: ${err.message}`);
    } finally {
      setNudgenikLoading(false);
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
        <Link to="/sessions" className="btn btn--primary">Back to Sessions</Link>
      </div>
    );
  }

  if (sessionMissing) {
    // No workspaceId means we lost state (e.g., page refresh) - navigate away
    if (!workspaceId) {
      return null;
    }

    return (
      <>
        <WorkspacesList
          ref={workspacesListRef}
          workspaceId={workspaceId}
          showControls={false}
        />
        {workspaceExists && (
          <div className="empty-state">
            <div className="empty-state__icon">⚠️</div>
            <h3 className="empty-state__title">Session unavailable</h3>
            <p className="empty-state__description">This session was disposed or no longer exists. Select another session from the list.</p>
          </div>
        )}
      </>
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
      <WorkspacesList
        ref={workspacesListRef}
        workspaceId={sessionData.workspace_id}
        currentSessionId={sessionId}
        showControls={false}
      />

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
                <Tooltip content="Ask nudgenik what this agent needs">
                  <button
                    className="btn btn--sm btn--primary"
                    onClick={handleAskNudgenik}
                    disabled={nudgenikLoading}
                    style={{ display: 'flex', alignItems: 'center', gap: 'var(--spacing-sm)' }}
                  >
                    {nudgenikLoading ? (
                      <>
                        <span className="spinner spinner--small"></span>
                        Asking...
                      </>
                    ) : 'Ask Nudgenik'}
                  </button>
                </Tooltip>
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
            <div
              key={sessionData.id}
              id="terminal"
              className="log-viewer__output"
              ref={terminalRef}
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

      {nudgenikResult !== null && (
        <div className="modal-overlay" role="dialog" aria-modal="true" aria-labelledby="nudgenik-result-title">
          <div className="modal modal--large">
            <div className="modal__header">
              <h2 className="modal__title" id="nudgenik-result-title">NudgeNik Response</h2>
            </div>
            <div className="modal__body">
              <div className="metadata-field">
                <span className="metadata-field__label">State</span>
                <span className="metadata-field__value">{nudgenikResult.state || 'Unknown'}</span>
              </div>
              <div className="metadata-field">
                <span className="metadata-field__label">Confidence</span>
                <span className="metadata-field__value">{nudgenikResult.confidence || 'Unknown'}</span>
              </div>
              <div className="metadata-field">
                <span className="metadata-field__label">Summary</span>
                <span className="metadata-field__value">{nudgenikResult.summary || 'No summary provided'}</span>
              </div>
              <div className="metadata-field">
                <span className="metadata-field__label">Evidence</span>
                <div className="metadata-field__value">
                  {nudgenikResult.evidence?.length ? (
                    <ul className="nudgenik-modal__evidence">
                      {nudgenikResult.evidence.map((item, idx) => (
                        <li key={idx}>{item}</li>
                      ))}
                    </ul>
                  ) : (
                    <span style={{ color: 'var(--color-text-muted)', fontStyle: 'italic' }}>None</span>
                  )}
                </div>
              </div>
            </div>
            <div className="modal__footer">
              <button className="btn btn--primary" onClick={() => setNudgenikResult(null)}>
                OK
              </button>
            </div>
          </div>
        </div>
      )}
    </>
  );
}
