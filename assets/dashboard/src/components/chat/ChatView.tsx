import { useRef, useState } from 'react';
import styles from './chat.module.css';
import ChatTranscript from './ChatTranscript';
import type { TranscriptHandle } from './ChatTranscript';
import Composer from './Composer';
import type { ComposerHandle } from './Composer';
import type { ChatSocketStatus } from '../../lib/chat/socket';
import type { ChatImage, Conversation } from '../../lib/chat/types';

interface ChatViewProps {
  conversation: Conversation;
  status: ChatSocketStatus;
  ended: boolean;
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
  initialDraft?: { text: string; images: ChatImage[] };
  onDraftChange?(draft: { text: string; images: ChatImage[] }): void;
}

export default function ChatView({
  conversation,
  status,
  ended,
  onSend,
  onInterrupt,
  onPermission,
  onAnswer,
  onAbort,
  composerRef,
  transcriptRef,
  initialDraft,
  onDraftChange,
}: ChatViewProps) {
  const running = conversation.phase === 'running';
  const [showResume, setShowResume] = useState(false);
  const localTranscriptRef = useRef<TranscriptHandle>(null);

  const setTranscriptRef = (handle: TranscriptHandle | null) => {
    localTranscriptRef.current = handle;
    if (typeof transcriptRef === 'function') transcriptRef(handle);
    else if (transcriptRef)
      (transcriptRef as React.RefObject<TranscriptHandle | null>).current = handle;
  };

  // Escape interrupts the current turn when the keydown originates inside the
  // chat view. The listener lives on the root element (not window) so a modal
  // or another page region owns its own Escape.
  return (
    <div
      className={styles.chat}
      data-testid="chat-view"
      onKeyDown={(e) => {
        if (e.key === 'Escape' && running) {
          e.preventDefault();
          onInterrupt();
        }
      }}
    >
      <ChatTranscript
        ref={setTranscriptRef}
        conversation={conversation}
        onResume={setShowResume}
        onPermission={onPermission}
        onAnswer={onAnswer}
        onAbort={onAbort}
      />
      {showResume ? (
        <button
          className="log-viewer__new-content"
          data-testid="chat-resume"
          onClick={() => localTranscriptRef.current?.jumpToBottom()}
        >
          Resume
        </button>
      ) : null}
      <Composer
        ref={composerRef}
        disabled={status !== 'connected'}
        ended={ended}
        onSend={onSend}
        initialDraft={initialDraft}
        onDraftChange={onDraftChange}
      />
    </div>
  );
}
