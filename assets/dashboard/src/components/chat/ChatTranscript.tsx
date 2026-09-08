import { useEffect, useImperativeHandle, useRef } from 'react';
import styles from './chat.module.css';
import UserMessageBubble from './UserMessageBubble';
import AssistantTurnView from './AssistantTurnView';
import type { Conversation } from '../../lib/chat/types';
import type { QuestionAnswer } from '../../lib/chat-answers';
import type { ChatFocus } from '../../lib/chat-focus';

export interface TranscriptHandle {
  /** Scroll to the bottom and resume following new content. */
  jumpToBottom(): void;
  /**
   * Focus a field on a pending question card. Returns false when the target
   * is not rendered (request resolved or not in history yet).
   */
  focusQuestionTarget(
    requestId: string,
    questionId: string,
    kind: 'other-input' | 'option',
    label?: string,
    position?: number
  ): boolean;
}

interface ChatTranscriptProps {
  conversation: Conversation;
  /**
   * Same contract as the terminal stream's onResume: called with true when
   * the user has scrolled away from the bottom (show the Resume control) and
   * false when following again.
   */
  onResume(showing: boolean): void;
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
  initialAnswers?: Record<string, Record<string, QuestionAnswer>>;
  onAnswerChange?(requestId: string, questionId: string, answer: QuestionAnswer): void;
  onFocusChange?(focus: ChatFocus): void;
  ref?: React.Ref<TranscriptHandle>;
}

const bottomThreshold = 8;

export default function ChatTranscript({
  conversation,
  onResume,
  onPermission,
  onAnswer,
  onAbort,
  initialAnswers,
  onAnswerChange,
  onFocusChange,
  ref,
}: ChatTranscriptProps) {
  const containerRef = useRef<HTMLDivElement>(null);
  const atBottomRef = useRef(true);

  const scrollToBottom = () => {
    const el = containerRef.current;
    if (el) el.scrollTop = el.scrollHeight - el.clientHeight;
  };

  const setAtBottom = (atBottom: boolean) => {
    if (atBottomRef.current === atBottom) return;
    atBottomRef.current = atBottom;
    onResume(!atBottom);
  };

  useImperativeHandle(ref, () => ({
    jumpToBottom: () => {
      scrollToBottom();
      setAtBottom(true);
    },
    focusQuestionTarget: (requestId, questionId, kind, label, position) => {
      const root = containerRef.current;
      if (!root) return false;
      // Question ids are question text (arbitrary characters), so match on
      // dataset fields rather than a CSS attribute selector.
      for (const el of root.querySelectorAll<HTMLElement>('[data-chat-question-target]')) {
        const d = el.dataset;
        if (d.requestId !== requestId || d.questionId !== questionId) continue;
        if (kind === 'option' && d.optionLabel !== label) continue;
        if (kind === 'other-input' && d.optionLabel !== undefined) continue;
        el.focus();
        if (kind === 'other-input' && position !== undefined && el instanceof HTMLInputElement) {
          const pos = Math.min(position, el.value.length);
          el.selectionStart = el.selectionEnd = pos;
        }
        return true;
      }
      return false;
    },
  }));

  // Follow the tail: while the user is at the bottom, new content keeps the
  // view at the bottom; once they scroll up, their position is respected.
  useEffect(() => {
    if (atBottomRef.current) {
      requestAnimationFrame(scrollToBottom);
    }
  }, [conversation]);

  const handleScroll = () => {
    const el = containerRef.current;
    if (!el) return;
    setAtBottom(el.scrollHeight - el.scrollTop - el.clientHeight < bottomThreshold);
  };

  return (
    <div
      className={styles.transcript}
      data-testid="chat-transcript"
      ref={containerRef}
      onScroll={handleScroll}
    >
      {conversation.items.map((item, i) =>
        item.kind === 'user' ? (
          <UserMessageBubble key={item.id} message={item} />
        ) : (
          <AssistantTurnView
            key={`turn-${i}`}
            turn={item}
            onPermission={onPermission}
            onAnswer={onAnswer}
            onAbort={onAbort}
            initialAnswers={initialAnswers}
            onAnswerChange={onAnswerChange}
            onFocusChange={onFocusChange}
          />
        )
      )}
    </div>
  );
}
