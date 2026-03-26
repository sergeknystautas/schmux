import React, { useCallback, useEffect, useRef, useState } from 'react';
import { Link, useParams, useNavigate } from 'react-router-dom';
import '@xterm/xterm/css/xterm.css';
import TerminalStream from '../lib/terminalStream';
import {
  updateNickname,
  disposeSession,
  reconnectRemoteHost,
  spawnSessions,
  getErrorMessage,
} from '../lib/api';
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
import ConnectionProgressModal from '../components/ConnectionProgressModal';
import { StreamMetricsPanel, type BackendStats } from '../components/StreamMetricsPanel';
import {
  IOWorkspaceMetricsPanel,
  type IOWorkspaceStats,
} from '../components/IOWorkspaceMetricsPanel';
import type { SequenceBreakRecord } from '../lib/streamDiagnostics';

export default function SessionDetailPage() {
  const { sessionId } = useParams();
  const { config, loading: configLoading } = useConfig();
  const {
    sessionsById,
    workspaces,
    loading: sessionsLoading,
    error: sessionsError,
    ackSession,
    waitForSession,
  } = useSessions();
  const navigate = useNavigate();
  const [wsStatus, setWsStatus] = useState<
    'connecting' | 'connected' | 'disconnected' | 'reconnecting' | 'error'
  >('connecting');
  const wsStatusRef = useRef(wsStatus);
  useEffect(() => {
    wsStatusRef.current = wsStatus;
  }, [wsStatus]);
  const [showResume, setShowResume] = useState(false);
  const [followTail, setFollowTail] = useState(true);
  const [controlModeAttached, setControlModeAttached] = useState(true);
  const [sidebarCollapsed, setSidebarCollapsed] = useLocalStorage<boolean>(
    SESSION_SIDEBAR_COLLAPSED_KEY,
    false
  );
  const [workspaceId, setWorkspaceId] = useState<string | null>(null);
  const [selectionMode, setSelectionMode] = useState(false);
  const [selectedLines, setSelectedLines] = useState<string[]>([]);
  const terminalRef = useRef<HTMLDivElement | null>(null);
  const terminalStreamRef = useRef<TerminalStream | null>(null);
  const { success, error: toastError } = useToast();
  const { prompt, confirm, alert } = useModal();
  const { markAsViewed } = useViewedSessions();
  const { registerAction, unregisterAction } = useKeyboardMode();
  const [backendStats, setBackendStats] = useState<BackendStats | null>(null);
  const [frontendStats, setFrontendStats] = useState<{
    framesReceived: number;
    bytesReceived: number;
    bootstrapCount: number;
    sequenceBreaks: number;
    recentBreaks: SequenceBreakRecord[];
    frameSizeStats?: { count: number; median: number; p90: number; max: number } | null;
    frameSizeDist?: { buckets: number[]; maxCount: number; maxBytes: number } | null;
    followLostCount?: number;
    scrollSuppressedCount?: number;
    scrollCoalesceHits?: number;
    resizeCount?: number;
  } | null>(null);

  const terminalRecreationCountRef = useRef(0);

  const [ioWorkspaceStats, setIOWorkspaceStats] = useState<IOWorkspaceStats | null>(null);

  // Use a ref for desync config so the terminal effect can read the latest value
  // without adding config to its dependency array (which would cause terminal re-creation).
  const desyncEnabledRef = useRef(config.desync?.enabled ?? false);
  useEffect(() => {
    desyncEnabledRef.current = config.desync?.enabled ?? false;
  }, [config.desync?.enabled]);

  const ioWorkspaceTelemetryEnabledRef = useRef(config.io_workspace_telemetry?.enabled ?? false);
  useEffect(() => {
    ioWorkspaceTelemetryEnabledRef.current = config.io_workspace_telemetry?.enabled ?? false;
  }, [config.io_workspace_telemetry?.enabled]);

  const sessionData = sessionId ? sessionsById[sessionId] : null;
  const sessionMissing = !sessionsLoading && !sessionsError && sessionId && !sessionData;
  const workspaceExists = workspaceId && workspaces?.some((ws) => ws.id === workspaceId);

  // Remote host disconnection state
  const [reconnectModal, setReconnectModal] = useState<{
    hostId: string;
    flavorId: string;
    displayName: string;
    provisioningSessionId: string | null;
  } | null>(null);
  const currentWorkspaceForRemote = workspaces?.find(
    (ws) => ws.id === (sessionData?.workspace_id || workspaceId)
  );
  const isRemoteSession = Boolean(sessionData?.remote_host_id);
  const remoteHostStatus = currentWorkspaceForRemote?.remote_host_status;
  const remoteDisconnected =
    isRemoteSession && remoteHostStatus !== 'connected' && remoteHostStatus !== undefined;

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

  // If session is missing but workspace has other sessions, navigate to a sibling
  useEffect(() => {
    if (sessionMissing && workspaceId && workspaceExists) {
      const ws = workspaces?.find((w) => w.id === workspaceId);
      const sibling = ws?.sessions?.find((s) => s.id !== sessionId);
      if (sibling) {
        navigate(`/sessions/${sibling.id}`, { replace: true });
      }
    }
  }, [sessionMissing, workspaceId, workspaceExists, workspaces, sessionId, navigate]);

  useEffect(() => {
    if (sessionData?.id) {
      markAsViewed(sessionData.id);
      ackSession(sessionData.id);
    }
  }, [sessionData?.id, sessionData?.nudge_seq, markAsViewed, ackSession]);

  // Track slow React renders for diagnostic capture
  const slowRendersRef = useRef<{ ts: number; phase: string; durationMs: number }[]>([]);

  // Ref for diagnostic completion handler to avoid stale closures
  const diagnosticCompleteRef = useRef<
    (result: { diagDir: string; verdict: string; findings: string[] }) => void
  >(() => {});
  const ioWorkspaceDiagnosticCompleteRef = useRef<
    (result: { diagDir: string; verdict: string; findings: string[] }) => void
  >(() => {});

  useEffect(() => {
    if (!sessionData || !terminalRef.current) return;
    // Don't create terminal stream while remote host is disconnected
    if (remoteDisconnected) return;

    const terminalStream = new TerminalStream(sessionData.id, terminalRef.current, {
      followTail: true,
      useWebGL: config.xterm?.use_webgl !== false,
      onResume: (showing) => {
        setShowResume(showing);
        setFollowTail(!showing);
      },
      onStatusChange: (status) => {
        setWsStatus(status);
        // Reset control mode state on new connection — backend will send real status within 1s
        if (status === 'connected') {
          setControlModeAttached(true);
        }
      },
      onSelectedLinesChange: (lines) => setSelectedLines(lines),
    });

    terminalStream.onControlModeChange = (attached) => setControlModeAttached(attached);

    terminalStreamRef.current = terminalStream;
    terminalRecreationCountRef.current += 1;
    terminalStream.recreationCount = terminalRecreationCountRef.current;
    setFollowTail(true);

    // Enable diagnostics on the same stream instance to avoid stale-closure issues.
    // Use ref for desync config so this effect doesn't depend on config changes.
    let diagnosticsInterval: ReturnType<typeof setInterval> | null = null;
    if (desyncEnabledRef.current) {
      terminalStream.lifecycleLogging = true;
      terminalStream.enableDiagnostics();
      terminalStream.enableWriteRaceDiagnostics();
      terminalStream.slowReactRenders = slowRendersRef.current;
      terminalStream.onStatsUpdate = (stats) => {
        setBackendStats(stats as unknown as BackendStats);
      };
      terminalStream.onDiagnosticComplete = (result) => {
        diagnosticCompleteRef.current(result);
      };

      diagnosticsInterval = setInterval(() => {
        const diag = terminalStream.diagnostics;
        if (diag) {
          setFrontendStats({
            framesReceived: diag.framesReceived,
            bytesReceived: diag.bytesReceived,
            bootstrapCount: diag.bootstrapCount,
            sequenceBreaks: diag.sequenceBreaks,
            recentBreaks: diag.recentBreaks,
            frameSizeStats: diag.getFrameSizeStats(),
            frameSizeDist: diag.getFrameSizeDistribution(),
            followLostCount: diag.followLostCount,
            scrollSuppressedCount: diag.scrollSuppressedCount,
            scrollCoalesceHits: diag.scrollCoalesceHits,
            resizeCount: diag.resizeCount,
          });
        }
      }, 3000);
    }

    if (ioWorkspaceTelemetryEnabledRef.current) {
      terminalStream.onIOWorkspaceStatsUpdate = (stats) => {
        setIOWorkspaceStats(stats as unknown as IOWorkspaceStats);
      };
      terminalStream.onIOWorkspaceDiagnosticComplete = (result) => {
        ioWorkspaceDiagnosticCompleteRef.current(result);
      };
    }

    terminalStream.initialized.then(() => {
      terminalStream.connect();
      terminalStream.focus();
    });

    return () => {
      if (diagnosticsInterval) {
        clearInterval(diagnosticsInterval);
      }
      terminalStream.disableWriteRaceDiagnostics();
      terminalStream.disableDiagnostics();
      terminalStream.disconnect();
      setBackendStats(null);
      setFrontendStats(null);
    };
  }, [sessionData?.id, remoteDisconnected]);

  diagnosticCompleteRef.current = async (result: {
    diagDir: string;
    verdict: string;
    findings: string[];
  }) => {
    success(`Diagnostic captured: ${result.verdict} (${result.diagDir})`);

    // Determine target for diagnostic agent.
    // Empty string means the user explicitly chose "None (capture only)".
    const target = config.desync?.target || '';

    // Find the workspace for this session
    const ws = workspaces?.find((w) => w.id === sessionData?.workspace_id);
    if (!target || !ws) {
      // No target or workspace — just show toast with path
      if (!target) {
        success(`Diagnostic files saved to ${result.diagDir}`);
      }
      return;
    }

    // Build diagnostic prompt
    const prompt = `Read .claude/skills/terminal-desync-investigation/SKILL.md and follow it to investigate the diagnostic capture at ${result.diagDir}. Identify root causes and propose fixes.`;

    try {
      const response = await spawnSessions({
        repo: ws.repo,
        branch: ws.branch,
        prompt,
        nickname: 'diagnose',
        targets: { [target]: 1 },
        workspace_id: ws.id,
      });

      const spawnResult = response[0];
      if (spawnResult.error) {
        alert('Diagnostic Agent Failed', `Failed to spawn diagnostic agent: ${spawnResult.error}`);
        return;
      }

      success('Spawned diagnostic agent');
      if (spawnResult.session_id) {
        await waitForSession(spawnResult.session_id);
        navigate(`/sessions/${spawnResult.session_id}`);
      }
    } catch (err) {
      alert(
        'Diagnostic Agent Failed',
        `Failed to spawn diagnostic agent: ${getErrorMessage(err, 'Unknown error')}`
      );
    }
  };

  ioWorkspaceDiagnosticCompleteRef.current = async (result: {
    diagDir: string;
    verdict: string;
    findings: string[];
  }) => {
    success(`IO workspace capture: ${result.verdict} (${result.diagDir})`);

    // Empty string means the user explicitly chose "None (capture only)".
    const target = config.io_workspace_telemetry?.target || '';

    const ws = workspaces?.find((w) => w.id === sessionData?.workspace_id);
    if (!target || !ws) {
      if (!target) {
        success(`IO workspace diagnostic files saved to ${result.diagDir}`);
      }
      return;
    }

    const ioPrompt = `Investigate the IO workspace telemetry diagnostic capture at ${result.diagDir}. Read the meta.json, commands-ringbuffer.txt, slow-commands.txt, and by-workspace.txt to understand the git command patterns. Then analyze the relevant source code to understand why these patterns exist and identify root causes of any performance issues. Propose concrete optimizations if warranted.`;

    try {
      const response = await spawnSessions({
        repo: ws.repo,
        branch: ws.branch,
        prompt: ioPrompt,
        nickname: 'io-diagnose',
        targets: { [target]: 1 },
        workspace_id: ws.id,
      });

      const spawnResult = response[0];
      if (spawnResult.error) {
        alert('IO Diagnostic Failed', `Failed to spawn IO diagnostic agent: ${spawnResult.error}`);
        return;
      }

      success('Spawned IO diagnostic agent');
      if (spawnResult.session_id) {
        await waitForSession(spawnResult.session_id);
        navigate(`/sessions/${spawnResult.session_id}`);
      }
    } catch (err) {
      alert(
        'IO Diagnostic Failed',
        `Failed to spawn IO diagnostic agent: ${getErrorMessage(err, 'Unknown error')}`
      );
    }
  };

  useEffect(() => {
    if (!sessionData?.id) return;
    setWsStatus('connecting');
    setShowResume(false);
    setFollowTail(true);
    setControlModeAttached(true);
    // Reset selection mode when switching sessions
    setSelectionMode(false);
    setSelectedLines([]);
  }, [sessionData?.id]);

  // Keep marking as viewed while WebSocket is connected (you're seeing output live)
  useEffect(() => {
    const seenInterval = config.nudgenik?.seen_interval_ms || 2000;
    const interval = setInterval(() => {
      if (wsStatusRef.current === 'connected') {
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
    if (sessionData?.status === 'disposing') return;

    const sessionDisplay = sessionData?.nickname
      ? `${sessionData.nickname} (${sessionId})`
      : sessionId;

    const accepted = await confirm(`Dispose session ${sessionDisplay}?`, { danger: true });
    if (!accepted) return;

    try {
      await disposeSession(sessionId);
      success('Session disposed');
    } catch (err) {
      alert('Dispose Failed', `Failed to dispose: ${getErrorMessage(err, 'Unknown error')}`);
    }
  }, [sessionId, sessionData?.nickname, confirm, success, alert]);

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

  // Register keyboard shortcut for resume/scroll to bottom (Down arrow)
  useEffect(() => {
    if (!sessionId) return;
    const scope = { type: 'session', id: sessionId } as const;
    const action = {
      key: 'ArrowDown',
      description: 'Resume / scroll to bottom',
      handler: () => {
        terminalStreamRef.current?.jumpToBottom();
        terminalStreamRef.current?.focus();
      },
      scope,
    };

    registerAction(action);

    return () => unregisterAction('ArrowDown', false, scope);
  }, [registerAction, unregisterAction, sessionId]);

  const handleEditNickname = async () => {
    if (!sessionId || !sessionData) return;
    let newNickname: string | null = sessionData.nickname || '';
    let errorMessage = '';

    // Keep prompting until successful or cancelled
    while (true) {
      newNickname = await prompt('Edit Nickname', {
        defaultValue: newNickname,
        placeholder: 'Enter nickname (optional)',
        confirmText: 'Save',
        errorMessage,
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
          alert(
            'Nickname Update Failed',
            `Failed to update nickname: ${getErrorMessage(err, 'Unknown error')}`
          );
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
        <Link to="/" className="btn btn--primary">
          Back to Home
        </Link>
      </div>
    );
  }

  // Get the current workspace data
  const currentWorkspace = workspaces?.find(
    (ws) => ws.id === (sessionData?.workspace_id || workspaceId)
  );

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
          <p className="empty-state__description">
            This session was disposed or no longer exists. Select another session from the tabs
            above.
          </p>
        </div>
      </>
    );
  }

  // At this point sessionData is guaranteed non-null by the early returns above
  if (!sessionData) return null;

  const statusClass = sessionData.running ? 'status-pill--running' : 'status-pill--stopped';
  const statusText = sessionData.running ? 'Running' : 'Stopped';
  const wsPillClass =
    wsStatus === 'connected'
      ? controlModeAttached
        ? 'connection-pill--connected'
        : 'connection-pill--reconnecting'
      : wsStatus === 'disconnected'
        ? 'connection-pill--offline'
        : 'connection-pill--reconnecting';
  const wsPillText =
    wsStatus === 'connected'
      ? controlModeAttached
        ? 'Live'
        : 'Stalled'
      : wsStatus === 'disconnected'
        ? 'Offline'
        : 'Connecting...';

  return (
    <React.Profiler
      id="SessionDetailPage"
      onRender={(_id, phase, actualDuration) => {
        if (actualDuration > 50) {
          slowRendersRef.current.push({
            ts: Date.now(),
            phase,
            durationMs: Math.round(actualDuration),
          });
          if (slowRendersRef.current.length > 20) {
            slowRendersRef.current = slowRendersRef.current.slice(-20);
          }
        }
      }}
    >
      <>
        {currentWorkspace && (
          <>
            <WorkspaceHeader workspace={currentWorkspace} />
            <SessionTabs
              sessions={currentWorkspace.sessions || []}
              currentSessionId={sessionId}
              workspace={currentWorkspace}
            />
          </>
        )}

        <div
          className={`session-detail${sidebarCollapsed ? ' session-detail--sidebar-collapsed' : ''}`}
        >
          <div className="session-detail__main">
            {remoteDisconnected ? (
              <div
                className="empty-state"
                style={{
                  height: '100%',
                  display: 'flex',
                  flexDirection: 'column',
                  alignItems: 'center',
                  justifyContent: 'center',
                }}
              >
                <div className="empty-state__icon">
                  <svg
                    width="48"
                    height="48"
                    viewBox="0 0 24 24"
                    fill="none"
                    stroke="var(--color-error)"
                    strokeWidth="1.5"
                  >
                    <line x1="1" y1="1" x2="23" y2="23" />
                    <path d="M16.72 11.06A10.94 10.94 0 0 1 19 12.55" />
                    <path d="M5 12.55a10.94 10.94 0 0 1 5.17-2.39" />
                    <path d="M10.71 5.05A16 16 0 0 1 22.56 9" />
                    <path d="M1.42 9a15.91 15.91 0 0 1 4.7-2.88" />
                    <path d="M8.53 16.11a6 6 0 0 1 6.95 0" />
                    <line x1="12" y1="20" x2="12.01" y2="20" />
                  </svg>
                </div>
                <h3 className="empty-state__title">Remote host disconnected</h3>
                <p className="empty-state__description">
                  The connection to{' '}
                  {sessionData.remote_hostname ||
                    sessionData.remote_flavor_name ||
                    'the remote host'}{' '}
                  has been lost. Reconnect to resume terminal streaming.
                </p>
                <button
                  className="btn btn--primary"
                  style={{ marginTop: 'var(--spacing-md)' }}
                  onClick={async () => {
                    if (!sessionData.remote_host_id) return;
                    try {
                      const result = await reconnectRemoteHost(sessionData.remote_host_id);
                      setReconnectModal({
                        hostId: sessionData.remote_host_id,
                        flavorId: result.flavor_id,
                        displayName: result.hostname || sessionData.remote_flavor_name || 'Remote',
                        provisioningSessionId: result.provisioning_session_id || null,
                      });
                    } catch (err) {
                      alert('Reconnect Failed', getErrorMessage(err, 'Failed to reconnect'));
                    }
                  }}
                >
                  <svg
                    width="14"
                    height="14"
                    viewBox="0 0 24 24"
                    fill="none"
                    stroke="currentColor"
                    strokeWidth="2"
                  >
                    <polyline points="23 4 23 10 17 10" />
                    <path d="M20.49 15a9 9 0 1 1-2.12-9.36L23 10" />
                  </svg>
                  Reconnect
                </button>
              </div>
            ) : (
              <div className="log-viewer" data-tour="terminal-log-viewer">
                <div
                  className="log-viewer__header"
                  style={
                    sessionData.persona_color
                      ? { borderBottom: `6px solid ${sessionData.persona_color}` }
                      : undefined
                  }
                >
                  <div className="log-viewer__info">
                    <Tooltip
                      content={
                        wsStatus === 'connected' && !controlModeAttached
                          ? 'Terminal output stalled — tmux control mode reconnecting'
                          : wsStatus === 'connected'
                            ? 'WebSocket connected - receiving real-time terminal output'
                            : wsStatus === 'disconnected'
                              ? 'WebSocket disconnected - unable to receive terminal output'
                              : 'WebSocket connecting...'
                      }
                    >
                      <div className={`connection-pill ${wsPillClass}`}>
                        <span className="connection-pill__dot"></span>
                        <span>{wsPillText}</span>
                      </div>
                    </Tooltip>
                    <Tooltip
                      content={
                        sessionData.running
                          ? 'Agent process is running'
                          : 'Agent process has stopped'
                      }
                    >
                      <div className={`status-pill ${statusClass}`} data-testid="session-status">
                        <span className="status-pill__dot"></span>
                        <span>{statusText}</span>
                      </div>
                    </Tooltip>
                    {!sessionData.remote_host_id &&
                      config.system_capabilities?.iterm2_available && (
                        <Tooltip content="Open tmux session in iTerm2">
                          <a
                            className="iterm2-link"
                            href={`iterm2:///command?c=${encodeURIComponent(sessionData.attach_cmd)}`}
                          >
                            <svg
                              width="14"
                              height="14"
                              viewBox="0 0 24 24"
                              fill="none"
                              stroke="currentColor"
                              strokeWidth="2"
                            >
                              <path d="M18 13v6a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2V8a2 2 0 0 1 2-2h6" />
                              <polyline points="15 3 21 3 21 9" />
                              <line x1="10" y1="14" x2="21" y2="3" />
                            </svg>
                            <span>iTerm2</span>
                          </a>
                        </Tooltip>
                      )}
                    {config.desync?.enabled && (
                      <StreamMetricsPanel
                        backendStats={backendStats}
                        frontendStats={frontendStats}
                        onDiagnosticCapture={() => terminalStreamRef.current?.sendDiagnostic()}
                      />
                    )}
                    {config.io_workspace_telemetry?.enabled && (
                      <IOWorkspaceMetricsPanel
                        stats={ioWorkspaceStats}
                        onCapture={() => terminalStreamRef.current?.sendIOWorkspaceDiagnostic()}
                      />
                    )}
                  </div>
                  <div className="log-viewer__actions">
                    {selectionMode ? (
                      <>
                        <Tooltip
                          content={`Copy ${selectedLines.length} selected line${selectedLines.length !== 1 ? 's' : ''}`}
                        >
                          <button
                            className="btn btn--sm btn--primary"
                            onClick={handleCopySelectedLines}
                            disabled={selectedLines.length === 0}
                          >
                            <svg
                              width="14"
                              height="14"
                              viewBox="0 0 24 24"
                              fill="none"
                              stroke="currentColor"
                              strokeWidth="2"
                            >
                              <rect x="9" y="9" width="13" height="13" rx="2" ry="2"></rect>
                              <path d="M5 15H4a2 2 0 0 1-2-2V4a2 2 0 0 1 2-2h9a2 2 0 0 1 2 2v1"></path>
                            </svg>
                            <span>Copy</span>
                          </button>
                        </Tooltip>
                        <Tooltip content="Cancel selection">
                          <button className="btn btn--sm" onClick={handleCancelSelection}>
                            Cancel
                          </button>
                        </Tooltip>
                      </>
                    ) : (
                      <Tooltip content="Select lines to copy">
                        <button className="btn btn--sm" onClick={handleToggleSelectionMode}>
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
                        <svg
                          width="14"
                          height="14"
                          viewBox="0 0 24 24"
                          fill="none"
                          stroke="currentColor"
                          strokeWidth="2"
                        >
                          <path d="M21 15v4a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2v-4"></path>
                          <polyline points="7 10 12 15 17 10"></polyline>
                          <line x1="12" y1="15" x2="12" y2="3"></line>
                        </svg>
                      </button>
                    </Tooltip>
                    <Tooltip content="Toggle sidebar">
                      <button className="btn btn--sm sidebar-toggle-btn" onClick={toggleSidebar}>
                        <svg
                          width="14"
                          height="14"
                          viewBox="0 0 24 24"
                          fill="none"
                          stroke="currentColor"
                          strokeWidth="2"
                        >
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
                  data-tour="terminal-viewport"
                  ref={terminalRef}
                  data-testid="terminal-viewport"
                  style={{ cursor: selectionMode ? 'pointer' : undefined }}
                ></div>

                {showResume ? (
                  <button
                    className="log-viewer__new-content"
                    onClick={() => {
                      terminalStreamRef.current?.jumpToBottom();
                      terminalStreamRef.current?.focus();
                    }}
                  >
                    Resume
                  </button>
                ) : null}
              </div>
            )}
          </div>

          <aside
            className="session-detail__sidebar"
            data-tour="session-detail-sidebar"
            data-testid="session-sidebar"
          >
            <div className="metadata-field">
              <span className="metadata-field__label">Session ID</span>
              <span className="metadata-field__value metadata-field__value--mono">
                {sessionData.id}
              </span>
            </div>

            <div className="metadata-field">
              <span className="metadata-field__label">Target</span>
              <span className="metadata-field__value">{sessionData.target}</span>
            </div>

            {sessionData.model && sessionData.model.context_window ? (
              <div className="metadata-field">
                <span className="metadata-field__label">Context Window</span>
                <span className="metadata-field__value">
                  {(sessionData.model.context_window / 1000).toFixed(0)}K tokens
                </span>
              </div>
            ) : null}
            {sessionData.model &&
            (sessionData.model.cost_input_per_mtok || sessionData.model.cost_output_per_mtok) ? (
              <div className="metadata-field">
                <span className="metadata-field__label">Pricing</span>
                <span className="metadata-field__value">
                  ${sessionData.model.cost_input_per_mtok || 0} / $
                  {sessionData.model.cost_output_per_mtok || 0} per MTok
                </span>
              </div>
            ) : null}

            {sessionData.nickname ? (
              <div className="metadata-field" data-testid="session-nickname">
                <div
                  style={{
                    display: 'flex',
                    alignItems: 'center',
                    justifyContent: 'space-between',
                    width: '100%',
                  }}
                >
                  <span className="metadata-field__label">Nickname</span>
                  <Tooltip content="Edit nickname">
                    <button className="btn btn--sm btn--ghost" onClick={handleEditNickname}>
                      <svg
                        width="12"
                        height="12"
                        viewBox="0 0 24 24"
                        fill="none"
                        stroke="currentColor"
                        strokeWidth="2"
                      >
                        <path d="M11 4H4a2 2 0 0 0-2 2v14a2 2 0 0 0 2 2h14a2 2 0 0 0 2-2v-7"></path>
                        <path d="M18.5 2.5a2.121 2.121 0 0 1 3 3L12 15l-4 1 1-4 9.5-9.5z"></path>
                      </svg>
                    </button>
                  </Tooltip>
                </div>
                <span className="metadata-field__value">{sessionData.nickname}</span>
              </div>
            ) : (
              <div className="metadata-field" data-testid="session-nickname">
                <div
                  style={{
                    display: 'flex',
                    alignItems: 'center',
                    justifyContent: 'space-between',
                    width: '100%',
                  }}
                >
                  <span className="metadata-field__label">Nickname</span>
                  <Tooltip content="Add nickname">
                    <button className="btn btn--sm btn--ghost" onClick={handleEditNickname}>
                      <svg
                        width="12"
                        height="12"
                        viewBox="0 0 24 24"
                        fill="none"
                        stroke="currentColor"
                        strokeWidth="2"
                      >
                        <line x1="12" y1="5" x2="12" y2="19"></line>
                        <line x1="5" y1="12" x2="19" y2="12"></line>
                      </svg>
                    </button>
                  </Tooltip>
                </div>
                <span
                  className="metadata-field__value"
                  style={{ color: 'var(--color-text-muted)', fontStyle: 'italic' }}
                >
                  Not set
                </span>
              </div>
            )}

            {sessionData.persona_id && (
              <div className="metadata-field">
                <span className="metadata-field__label">Persona</span>
                <span className="metadata-field__value">
                  {sessionData.persona_icon && (
                    <span style={{ color: sessionData.persona_color, marginRight: '4px' }}>
                      {sessionData.persona_icon}
                    </span>
                  )}
                  {sessionData.persona_name || sessionData.persona_id}
                </span>
              </div>
            )}

            <div className="metadata-field">
              <span className="metadata-field__label">Created</span>
              <Tooltip content={formatTimestamp(sessionData.created_at)}>
                <span className="metadata-field__value" style={{ alignSelf: 'flex-start' }}>
                  {formatRelativeTime(sessionData.created_at)}
                </span>
              </Tooltip>
            </div>

            <div className="metadata-field">
              <span className="metadata-field__label">Last Activity</span>
              <Tooltip
                content={
                  sessionData.last_output_at ? formatTimestamp(sessionData.last_output_at) : 'Never'
                }
              >
                <span className="metadata-field__value" style={{ alignSelf: 'flex-start' }}>
                  {sessionData.last_output_at
                    ? formatRelativeTime(sessionData.last_output_at)
                    : 'Never'}
                </span>
              </Tooltip>
            </div>

            {sessionData.remote_host_id && (
              <>
                <hr
                  style={{
                    border: 'none',
                    borderTop: '1px solid var(--color-border)',
                    margin: 'var(--spacing-md) 0',
                  }}
                />
                <div className="metadata-field">
                  <span className="metadata-field__label">Environment</span>
                  <span
                    className="metadata-field__value"
                    style={{ display: 'flex', alignItems: 'center', gap: 'var(--spacing-xs)' }}
                  >
                    <svg
                      width="14"
                      height="14"
                      viewBox="0 0 24 24"
                      fill="none"
                      stroke="currentColor"
                      strokeWidth="2"
                    >
                      <rect x="1" y="4" width="22" height="16" rx="2" ry="2" />
                      <line x1="1" y1="10" x2="23" y2="10" />
                    </svg>
                    {sessionData.remote_flavor_name || 'Remote'}
                  </span>
                </div>
                {sessionData.remote_hostname && (
                  <div className="metadata-field">
                    <span className="metadata-field__label">Hostname</span>
                    <span
                      className="metadata-field__value metadata-field__value--mono"
                      style={{ fontSize: '0.75rem' }}
                    >
                      {sessionData.remote_hostname}
                    </span>
                  </div>
                )}
              </>
            )}

            <hr
              style={{
                border: 'none',
                borderTop: '1px solid var(--color-border)',
                margin: 'var(--spacing-md) 0',
              }}
            />

            <div className="form-group">
              <label className="form-group__label">Attach Command</label>
              <div className="copy-field">
                <span className="copy-field__value">{sessionData.attach_cmd}</span>
                <Tooltip content="Copy attach command">
                  <button className="copy-field__btn" onClick={handleCopyAttach}>
                    <svg
                      width="14"
                      height="14"
                      viewBox="0 0 24 24"
                      fill="none"
                      stroke="currentColor"
                      strokeWidth="2"
                    >
                      <rect x="9" y="9" width="13" height="13" rx="2" ry="2"></rect>
                      <path d="M5 15H4a2 2 0 0 1-2-2V4a2 2 0 0 1 2-2h9a2 2 0 0 1 2 2v1"></path>
                    </svg>
                  </button>
                </Tooltip>
              </div>
            </div>

            <div style={{ marginTop: 'auto' }}>
              <button
                className="btn btn--danger"
                style={{ width: '100%' }}
                onClick={handleDispose}
                disabled={sessionData?.status === 'disposing'}
                data-testid="dispose-session"
              >
                <svg
                  width="14"
                  height="14"
                  viewBox="0 0 24 24"
                  fill="none"
                  stroke="currentColor"
                  strokeWidth="2"
                >
                  <polyline points="3 6 5 6 21 6"></polyline>
                  <path d="M19 6v14a2 2 0 0 1-2 2H7a2 2 0 0 1-2-2V6m3 0V4a2 2 0 0 1 2-2h4a2 2 0 0 1 2 2v2"></path>
                </svg>
                Dispose Session
              </button>
            </div>
          </aside>
        </div>

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
      </>
    </React.Profiler>
  );
}
