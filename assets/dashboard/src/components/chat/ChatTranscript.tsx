import {
  useCallback,
  useEffect,
  useImperativeHandle,
  useLayoutEffect,
  useRef,
  useState,
} from 'react';
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
  /**
   * Scroll to and focus the tool row whose id matches `toolId`.
   * Returns false when no such tool is rendered.
   */
  focusTranscriptTool(toolId: string): boolean;
}

interface ChatTranscriptProps {
  conversation: Conversation;
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
  workspaceId?: string;
  workspacePath?: string;
  onOpenWorkspaceFile?(filePath: string): void;
  ref?: React.Ref<TranscriptHandle>;
}

const bottomThreshold = 8;

export default function ChatTranscript({
  conversation,
  onPermission,
  onAnswer,
  onAbort,
  initialAnswers,
  onAnswerChange,
  onFocusChange,
  workspaceId,
  workspacePath,
  onOpenWorkspaceFile,
  ref,
}: ChatTranscriptProps) {
  const containerRef = useRef<HTMLDivElement>(null);
  const contentRef = useRef<HTMLDivElement>(null);
  const atBottomRef = useRef(true);
  const lastSizeRef = useRef({ viewport: 0, content: 0 });
  // The Resume control floats over the transcript, right-aligned above
  // whatever panels sit below it (activity, composer).
  const [showResume, setShowResume] = useState(false);

  const scrollToBottom = useCallback(() => {
    const el = containerRef.current;
    if (!el) return;
    lastSizeRef.current = { viewport: el.clientHeight, content: el.scrollHeight };
    el.scrollTop = el.scrollHeight - el.clientHeight;
  }, []);

  const setAtBottom = (atBottom: boolean) => {
    if (atBottomRef.current === atBottom) return;
    atBottomRef.current = atBottom;
    setShowResume(!atBottom);
  };

  const jumpToBottom = () => {
    scrollToBottom();
    setAtBottom(true);
  };

  useImperativeHandle(ref, () => ({
    jumpToBottom,
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
    focusTranscriptTool: (toolId: string) => {
      const root = containerRef.current;
      if (!root) return false;
      // Match on a data attribute rather than element id (the tool's id
      // may include characters that are not safe in a CSS selector).
      const el = Array.from(root.querySelectorAll<HTMLElement>('[data-tool-id]')).find(
        (node) => node.dataset.toolId === toolId
      );
      if (!el) return false;
      setAtBottom(false);
      if (typeof el.scrollIntoView === 'function') {
        el.scrollIntoView({ block: 'center', behavior: 'instant' });
      }
      if (typeof el.focus === 'function') el.focus({ preventScroll: true });
      return true;
    },
  }));

  // Follow the tail: while the user is at the bottom, new content keeps the
  // view at the bottom; once they scroll up, their position is respected.
  useEffect(() => {
    if (atBottomRef.current) {
      const frame = requestAnimationFrame(() => {
        if (atBottomRef.current) scrollToBottom();
      });
      return () => cancelAnimationFrame(frame);
    }
  }, [conversation, scrollToBottom]);

  // Activity, plan disclosures, and composer growth resize the viewport
  // without changing the conversation. Content can also resize after render.
  // Keep following through both, but preserve the user's place when detached.
  useLayoutEffect(() => {
    const viewport = containerRef.current;
    const content = contentRef.current;
    if (!viewport || !content || typeof ResizeObserver === 'undefined') return;
    const observer = new ResizeObserver(() => {
      if (atBottomRef.current) scrollToBottom();
      lastSizeRef.current = { viewport: viewport.clientHeight, content: viewport.scrollHeight };
    });
    observer.observe(viewport);
    observer.observe(content);
    return () => observer.disconnect();
  }, [scrollToBottom]);

  const handleScroll = () => {
    const el = containerRef.current;
    if (!el) return;
    const resized =
      lastSizeRef.current.viewport !== el.clientHeight ||
      lastSizeRef.current.content !== el.scrollHeight;
    lastSizeRef.current = { viewport: el.clientHeight, content: el.scrollHeight };
    // Browsers can send a scroll event for a resize before ResizeObserver
    // runs. That event must not be mistaken for the user scrolling away.
    if (resized && atBottomRef.current) {
      scrollToBottom();
      return;
    }
    setAtBottom(el.scrollHeight - el.scrollTop - el.clientHeight < bottomThreshold);
  };

  return (
    <div className={styles.transcriptWrap}>
      <div
        className={styles.transcript}
        data-testid="chat-transcript"
        ref={containerRef}
        onScroll={handleScroll}
      >
        <div className={styles.transcriptContent} ref={contentRef}>
          {conversation.items.map((item, i) =>
            item.kind === 'user' ? (
              <UserMessageBubble key={item.id} message={item} />
            ) : (
              <AssistantTurnView
                key={`turn-${i}`}
                turn={item}
                activity={conversation.activity}
                onPermission={onPermission}
                onAnswer={onAnswer}
                onAbort={onAbort}
                initialAnswers={initialAnswers}
                onAnswerChange={onAnswerChange}
                onFocusChange={onFocusChange}
                workspaceId={workspaceId}
                workspacePath={workspacePath}
                onOpenWorkspaceFile={onOpenWorkspaceFile}
              />
            )
          )}
        </div>
      </div>
      {showResume ? (
        <button
          className={`btn btn--primary btn--sm ${styles.resume}`}
          data-testid="chat-resume"
          onClick={jumpToBottom}
        >
          Resume
        </button>
      ) : null}
    </div>
  );
}
