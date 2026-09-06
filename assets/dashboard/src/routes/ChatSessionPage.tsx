import { useCallback, useEffect, useRef, useState } from 'react';
import { useParams, useNavigate } from 'react-router';
import WorkspaceHeader from '../components/WorkspaceHeader';
import SessionTabs from '../components/SessionTabs';
import SessionSidebar from '../components/SessionSidebar';
import RestartSessionModal from '../components/RestartSessionModal';
import Tooltip from '../components/Tooltip';
import ChatView from '../components/chat/ChatView';
import type { ComposerHandle } from '../components/chat/Composer';
import type { TranscriptHandle } from '../components/chat/ChatTranscript';
import { useSessions } from '../contexts/SessionsContext';
import { useConfig } from '../contexts/ConfigContext';
import { useKeyboardMode } from '../contexts/KeyboardContext';
import { useModal } from '../components/ModalProvider';
import { useChatSocket } from '../hooks/useChatSocket';
import { useSessionActions } from '../hooks/useSessionActions';
import useLocalStorage, { SESSION_SIDEBAR_COLLAPSED_KEY } from '../hooks/useLocalStorage';
import { restartSession, getErrorMessage } from '../lib/api';
import { loadChatDraft, saveChatDraft, type ChatDraft } from '../lib/chat-draft';

export default function ChatSessionPage() {
  const { sessionId } = useParams();
  const { sessionsById, workspaces, waitForSession } = useSessions();
  const { config } = useConfig();
  const { confirm, alert } = useModal();
  const navigate = useNavigate();
  const composerRef = useRef<ComposerHandle>(null);
  const transcriptRef = useRef<TranscriptHandle>(null);
  const { registerAction, unregisterAction } = useKeyboardMode();
  const [showRestartModal, setShowRestartModal] = useState(false);
  // Same sidebar state as the terminal page, so collapsing it once holds across both.
  const [sidebarCollapsed, setSidebarCollapsed] = useLocalStorage<boolean>(
    SESSION_SIDEBAR_COLLAPSED_KEY,
    false
  );

  const sessionData = sessionId ? sessionsById[sessionId] : null;
  const workspace = workspaces?.find((ws) => ws.id === sessionData?.workspace_id);

  const { conversation, status, send, interrupt, answerPermission, answerQuestion } = useChatSocket(
    sessionId,
    sessionData?.running ?? false
  );
  const { editNickname, dispose, copyAttach } = useSessionActions(sessionId, sessionData);

  // In-progress message per session, restored when you come back to the tab.
  // The view is keyed on the session id below so a switch remounts the
  // composer with this session's draft instead of carrying the other one over.
  const initialDraft = sessionId ? (loadChatDraft(sessionId) ?? undefined) : undefined;
  const handleDraftChange = useCallback(
    (draft: ChatDraft) => {
      if (sessionId) saveChatDraft(sessionId, draft);
    },
    [sessionId]
  );

  // Criterion 5: arriving at a chat session, or switching to it, puts focus in
  // the composer. The composer is disabled until the socket is connected and a
  // disabled textarea cannot take focus, so this runs on connect, not on mount.
  useEffect(() => {
    if (status === 'connected') composerRef.current?.focus();
  }, [sessionId, status]);

  // Same Down-arrow action the terminal page registers: resume following and
  // put focus back in the input.
  useEffect(() => {
    if (!sessionId) return;
    const scope = { type: 'session', id: sessionId } as const;
    registerAction({
      key: 'ArrowDown',
      description: 'Resume / scroll to bottom',
      handler: () => {
        transcriptRef.current?.jumpToBottom();
        composerRef.current?.focus();
      },
      scope,
    });
    return () => unregisterAction('ArrowDown', false, scope);
  }, [registerAction, unregisterAction, sessionId]);

  const handleRestart = useCallback(
    async (e?: React.MouseEvent) => {
      if (e?.shiftKey) {
        setShowRestartModal(true);
        return;
      }
      if (!sessionId) return;
      const accepted = await confirm(
        'Restart session? The running agent is interrupted, then resumed with updated settings.',
        { danger: true }
      );
      if (!accepted) return;
      try {
        const result = await restartSession(sessionId);
        if (result.session_id) {
          await waitForSession(result.session_id);
          navigate(`/sessions/${result.session_id}`);
        }
      } catch (err) {
        alert('Restart Failed', `Failed to restart: ${getErrorMessage(err, 'Unknown error')}`);
      }
    },
    [sessionId, confirm, navigate, alert, waitForSession]
  );

  if (!sessionData) {
    return null;
  }

  const connText =
    status === 'connected'
      ? 'Live'
      : status === 'gone'
        ? 'Ended'
        : status === 'disconnected'
          ? 'Reconnecting'
          : 'Connecting…';

  return (
    <>
      {workspace && (
        <>
          <WorkspaceHeader workspace={workspace} />
          <SessionTabs
            sessions={workspace.sessions || []}
            currentSessionId={sessionId}
            workspace={workspace}
            onPaste={(content) => composerRef.current?.insert(content)}
          />
        </>
      )}

      <div
        className={`session-detail${sidebarCollapsed ? ' session-detail--sidebar-collapsed' : ''}`}
      >
        <div className="session-detail__main">
          {/* Same wrapper as the terminal page: the log viewer owns the height
              chain (flex column, min-height 0, overflow hidden) and the chat
              fills it exactly as the terminal output does. */}
          <div className="log-viewer" data-testid="chat-log-viewer">
            <div className="log-viewer__header" data-testid="chat-status-row">
              <div className="log-viewer__info">
                <Tooltip content="Chat connection">
                  <div
                    className={`connection-pill ${
                      status === 'connected'
                        ? 'connection-pill--connected'
                        : status === 'gone'
                          ? 'connection-pill--offline'
                          : 'connection-pill--reconnecting'
                    }`}
                    data-testid="chat-connection"
                  >
                    <span className="connection-pill__dot"></span>
                    <span>{connText}</span>
                  </div>
                </Tooltip>
                <Tooltip
                  content={
                    sessionData.running ? 'Agent process is running' : 'Agent process has stopped'
                  }
                >
                  <div
                    className={`status-pill ${
                      sessionData.running ? 'status-pill--running' : 'status-pill--stopped'
                    }`}
                    data-testid="session-status"
                  >
                    <span className="status-pill__dot"></span>
                    <span>{sessionData.running ? 'Running' : 'Stopped'}</span>
                  </div>
                </Tooltip>
                <Tooltip
                  content={sessionData.fence ? 'Session is fenced' : 'Session is not fenced'}
                >
                  <div
                    className={`status-pill ${
                      sessionData.fence ? 'status-pill--fenced' : 'status-pill--not-fenced'
                    }`}
                    data-testid="session-fence-status"
                  >
                    <span className="status-pill__dot"></span>
                    <span>{sessionData.fence ? 'Fenced' : 'Not fenced'}</span>
                  </div>
                </Tooltip>
                {sessionData.resume_id && !sessionData.remote_host_id && (
                  <Tooltip content="Shift-click for fence / endpoint options">
                    <button
                      className="btn btn--sm btn--secondary"
                      onClick={handleRestart}
                      data-testid="restart-session"
                    >
                      Restart
                    </button>
                  </Tooltip>
                )}
                {conversation.phase === 'running' && status === 'connected' && (
                  <Tooltip content="Interrupt the current turn (Escape)">
                    <button
                      className="btn btn--sm btn--danger"
                      onClick={interrupt}
                      data-testid="chat-stop"
                    >
                      Stop
                    </button>
                  </Tooltip>
                )}
              </div>
              <div className="log-viewer__actions">
                <Tooltip content="Toggle sidebar">
                  <button
                    className="btn btn--sm sidebar-toggle-btn"
                    onClick={() => setSidebarCollapsed((prev) => !prev)}
                    data-testid="chat-sidebar-toggle"
                  >
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
            <ChatView
              key={sessionId}
              conversation={conversation}
              status={status}
              ended={!sessionData.running}
              onSend={send}
              onInterrupt={interrupt}
              onPermission={answerPermission}
              onAnswer={answerQuestion}
              composerRef={composerRef}
              transcriptRef={transcriptRef}
              initialDraft={initialDraft}
              onDraftChange={handleDraftChange}
            />
          </div>
        </div>

        <SessionSidebar
          session={sessionData}
          config={config}
          showAttach={false}
          onEditNickname={editNickname}
          onCopyAttach={copyAttach}
          onDispose={dispose}
        />
      </div>

      {showRestartModal && sessionId && (
        <RestartSessionModal sessionId={sessionId} onClose={() => setShowRestartModal(false)} />
      )}
    </>
  );
}
