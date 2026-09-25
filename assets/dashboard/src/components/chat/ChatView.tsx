import { useRef, useState } from 'react';
import styles from './chat.module.css';
import ChatActivity from './ChatActivity';
import ChatTranscript from './ChatTranscript';
import type { TranscriptHandle } from './ChatTranscript';
import Composer from './Composer';
import type { ComposerHandle } from './Composer';
import type { ChatSocketStatus } from '../../lib/chat/socket';
import type { ChatImage, Conversation } from '../../lib/chat/types';
import type { QuestionAnswer } from '../../lib/chat-answers';
import type { ChatFocus } from '../../lib/chat-focus';
import type { ChatScroll } from '../../lib/chat-scroll';
import type { ChatDraft } from '../../lib/chat-draft';

interface ChatViewProps {
  conversation: Conversation;
  status: ChatSocketStatus;
  ended: boolean;
  /** True once the history frame for the current connection has been applied. */
  historyLoaded: boolean;
  chatLoadProfiling?: boolean;
  /** Optional socket error to surface. */
  socketError: string | null;
  onSend(text: string, images: ChatImage[]): void;
  onInterrupt(): void;
  onPermission(
    requestId: string,
    allow: boolean,
    updatedInput?: Record<string, unknown>,
    message?: string
  ): void;
  onAnswer(
    requestId: string,
    answers: Record<string, string[]>,
    input: Record<string, unknown>
  ): void;
  onAbort(requestId: string): void;
  composerRef?: React.Ref<ComposerHandle>;
  /** Same handle the terminal page uses for its Down-arrow "resume" action. */
  transcriptRef?: React.Ref<TranscriptHandle>;
  initialDraft?: ChatDraft;
  onDraftChange?(draft: ChatDraft): void;
  /** Called with the composer caret position whenever it moves. */
  onCaretChange?(position: number): void;
  initialAnswers?: Record<string, Record<string, QuestionAnswer>>;
  onAnswerChange?(requestId: string, questionId: string, answer: QuestionAnswer): void;
  onFocusChange?(focus: ChatFocus): void;
  /** Saved transcript scroll record, restored once history has loaded. */
  initialScroll?: ChatScroll | null;
  onScrollChange?(record: ChatScroll): void;
  workspaceId?: string;
  workspacePath?: string;
  onOpenWorkspaceFile?(filePath: string): void;
  /** Signed-out recovery: banner + composer lock while the login is absent. */
  signedOut: boolean;
  signedOutProtocol: string;
  onReauth: () => void;
}

const SIGNED_OUT_COPY: Record<string, string> = {
  'claude-stream-json': "Claude is signed out — messages won't reach it until you sign in again.",
  'codex-app-server': 'Codex is signed out. After signing in, restart this session.',
};

export default function ChatView({
  conversation,
  status,
  ended,
  historyLoaded,
  chatLoadProfiling = false,
  socketError,
  onSend,
  onInterrupt,
  onPermission,
  onAnswer,
  onAbort,
  composerRef,
  transcriptRef,
  initialDraft,
  onDraftChange,
  onCaretChange,
  initialAnswers,
  onAnswerChange,
  onFocusChange,
  initialScroll,
  onScrollChange,
  workspaceId,
  workspacePath,
  onOpenWorkspaceFile,
  signedOut,
  signedOutProtocol,
  onReauth,
}: ChatViewProps) {
  const running = conversation.phase === 'running';
  const localTranscriptRef = useRef<TranscriptHandle>(null);
  const localComposerRef = useRef<ComposerHandle>(null);
  const [attachmentAvailable, setAttachmentAvailable] = useState(false);
  const [fileDragInside, setFileDragInside] = useState(false);
  const fileDragDepthRef = useRef(0);

  const setTranscriptRef = (handle: TranscriptHandle | null) => {
    localTranscriptRef.current = handle;
    if (typeof transcriptRef === 'function') transcriptRef(handle);
    else if (transcriptRef)
      (transcriptRef as React.RefObject<TranscriptHandle | null>).current = handle;
  };

  const setComposerRef = (handle: ComposerHandle | null) => {
    localComposerRef.current = handle;
    if (typeof composerRef === 'function') composerRef(handle);
    else if (composerRef) (composerRef as React.RefObject<ComposerHandle | null>).current = handle;
  };

  const hasFiles = (types: readonly string[]) => Array.from(types).includes('Files');
  const clearFileDrag = () => {
    fileDragDepthRef.current = 0;
    setFileDragInside(false);
  };

  // Escape interrupts the current turn when the keydown originates inside the
  // chat view. The listener lives on the root element (not window) so a modal
  // or another page region owns its own Escape. An active file drag cancels
  // its own feedback first so cancellation never reaches the chat interrupt.
  return (
    <div
      className={styles.chat}
      data-testid="chat-view"
      onDragEnter={(event) => {
        if (!hasFiles(event.dataTransfer.types)) return;
        event.preventDefault();
        fileDragDepthRef.current += 1;
        setFileDragInside(true);
      }}
      onDragOver={(event) => {
        if (!hasFiles(event.dataTransfer.types)) return;
        event.preventDefault();
        event.dataTransfer.dropEffect = attachmentAvailable ? 'copy' : 'none';
      }}
      onDragLeave={(event) => {
        if (!hasFiles(event.dataTransfer.types)) return;
        fileDragDepthRef.current = Math.max(0, fileDragDepthRef.current - 1);
        if (fileDragDepthRef.current === 0) setFileDragInside(false);
      }}
      onDragEnd={clearFileDrag}
      onDrop={(event) => {
        if (!hasFiles(event.dataTransfer.types)) return;
        event.preventDefault();
        const files = Array.from(event.dataTransfer.files);
        clearFileDrag();
        if (attachmentAvailable) localComposerRef.current?.attachFiles(files);
      }}
      onKeyDown={(e) => {
        if (e.key === 'Escape' && fileDragInside) {
          e.preventDefault();
          clearFileDrag();
          return;
        }
        if (e.key === 'Escape' && running) {
          e.preventDefault();
          onInterrupt();
        }
      }}
    >
      {fileDragInside && attachmentAvailable ? (
        <div className={styles.fileDropOverlay} data-testid="chat-file-drop-overlay">
          <div className={styles.fileDropPrompt} role="status">
            Drop files to attach
          </div>
        </div>
      ) : null}
      <ChatTranscript
        ref={setTranscriptRef}
        conversation={conversation}
        chatLoadProfiling={chatLoadProfiling}
        onPermission={onPermission}
        onAnswer={onAnswer}
        onAbort={onAbort}
        initialAnswers={initialAnswers}
        onAnswerChange={onAnswerChange}
        onFocusChange={onFocusChange}
        historyLoaded={historyLoaded}
        initialScroll={initialScroll}
        onScrollChange={onScrollChange}
        workspaceId={workspaceId}
        workspacePath={workspacePath}
        onOpenWorkspaceFile={onOpenWorkspaceFile}
      />
      <ChatActivity
        conversation={conversation}
        status={status}
        historyLoaded={historyLoaded}
        ended={ended}
        onInterrupt={onInterrupt}
        onJumpToTool={(toolId) => {
          localTranscriptRef.current?.focusTranscriptTool(toolId);
        }}
      />
      {ended ? (
        <div
          className={`banner banner--warning ${styles.endedBanner}`}
          role="alert"
          data-testid="chat-ended-banner"
        >
          Session ended — the agent process is no longer running.
        </div>
      ) : null}
      {socketError ? (
        <div className="error-banner" role="alert" data-testid="chat-send-error">
          {socketError}
        </div>
      ) : null}
      {signedOut ? (
        <div className={styles.signedOutBanner} role="alert" data-testid="signed-out-banner">
          <span>{SIGNED_OUT_COPY[signedOutProtocol] ?? SIGNED_OUT_COPY['claude-stream-json']}</span>
          <button
            className="btn btn--sm btn--secondary"
            onClick={onReauth}
            data-testid="signed-out-reauth"
          >
            Sign in
          </button>
        </div>
      ) : null}
      <Composer
        ref={setComposerRef}
        workspaceId={workspaceId}
        disabled={signedOut || status !== 'connected'}
        disabledReason={signedOut ? 'Signed out — sign in to continue' : undefined}
        ended={ended}
        onSend={onSend}
        initialDraft={initialDraft}
        onDraftChange={onDraftChange}
        onCaretChange={onCaretChange}
        onAttachmentAvailabilityChange={setAttachmentAvailable}
      />
    </div>
  );
}
