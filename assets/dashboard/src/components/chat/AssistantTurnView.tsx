import { memo, type AnchorHTMLAttributes } from 'react';
import { operationForTool, type ActivityState } from '../../lib/chat/activity';
import ReactMarkdown from 'react-markdown';
import remarkGfm from 'remark-gfm';
import styles from './chat.module.css';
import ToolCallRow from './ToolCallRow';
import ThinkingDisclosure from './ThinkingDisclosure';
import PermissionCard from './PermissionCard';
import QuestionCard from './QuestionCard';
import UserMessageBubble from './UserMessageBubble';
import type { AssistantTurn } from '../../lib/chat/types';
import type { QuestionAnswer } from '../../lib/chat-answers';
import type { ChatFocus } from '../../lib/chat-focus';
import { resolveWorkspaceFileLink } from '../../lib/fileNavigation';

interface AssistantTurnViewProps {
  turn: AssistantTurn;
  activity?: Pick<ActivityState, 'operations' | 'toolIndex'>;
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
}

function AssistantTurnViewInner({
  turn,
  activity,
  onPermission,
  onAnswer,
  onAbort,
  initialAnswers,
  onAnswerChange,
  onFocusChange,
  workspaceId,
  workspacePath,
  onOpenWorkspaceFile,
}: AssistantTurnViewProps) {
  const markdownComponents = {
    a: ({ href, onClick, ...props }: AnchorHTMLAttributes<HTMLAnchorElement>) => {
      const target = resolveWorkspaceFileLink(href, workspaceId, workspacePath);
      return (
        <a
          href={target?.href ?? href}
          {...props}
          onClick={(event) => {
            onClick?.(event);
            if (
              event.defaultPrevented ||
              !target ||
              !onOpenWorkspaceFile ||
              event.button !== 0 ||
              event.metaKey ||
              event.ctrlKey ||
              event.shiftKey ||
              event.altKey ||
              (event.currentTarget.target && event.currentTarget.target !== '_self') ||
              event.currentTarget.hasAttribute('download')
            ) {
              return;
            }
            event.preventDefault();
            onOpenWorkspaceFile(target.filePath);
          }}
        />
      );
    },
  };

  return (
    <div className={styles.turn} data-testid="chat-turn">
      {turn.segments.map((s, i) => {
        switch (s.kind) {
          case 'prose':
            return (
              <div
                className="markdown-preview-content markdown-preview-content--inline"
                data-testid="chat-prose"
                key={i}
              >
                <ReactMarkdown remarkPlugins={[remarkGfm]} components={markdownComponents}>
                  {s.text}
                </ReactMarkdown>
              </div>
            );
          case 'thinking':
            return s.text.trim() ? <ThinkingDisclosure key={i} text={s.text} /> : null;
          case 'tool':
            return <ToolCallRow key={i} tool={s} activity={activity} />;
          case 'pending':
            return s.questions ? (
              <QuestionCard
                key={i}
                pending={s}
                onAnswer={onAnswer}
                initialAnswers={initialAnswers?.[s.requestId]}
                onAnswerChange={(questionId, answer) =>
                  onAnswerChange?.(s.requestId, questionId, answer)
                }
                onFocusChange={onFocusChange}
              />
            ) : (
              <PermissionCard key={i} pending={s} onPermission={onPermission} onAbort={onAbort} />
            );
          case 'user':
            return (
              <UserMessageBubble
                key={i}
                message={{ kind: 'user', id: s.id, text: s.text, images: s.images, queued: false }}
              />
            );
          default:
            return null;
        }
      })}
      {turn.thinking && (
        <div className={styles.thinkingLine} data-testid="chat-thinking">
          Thinking…
        </div>
      )}
      {turn.end?.state === 'stopped' && (
        <div className={styles.endStopped} data-testid="chat-turn-end" data-end-state="stopped">
          Stopped
        </div>
      )}
      {turn.end?.state === 'error' && (
        <div className={styles.endError} data-testid="chat-turn-end" data-end-state="error">
          {turn.end.text}
        </div>
      )}
    </div>
  );
}

// Only changes to the turn, its linked operations, or its interactive props
// invalidate a closed turn. Identity comes from the same link ToolCallRow uses.
const AssistantTurnView = memo(AssistantTurnViewInner, (prev, next) => {
  if (
    prev.turn !== next.turn ||
    prev.onPermission !== next.onPermission ||
    prev.onAnswer !== next.onAnswer ||
    prev.onAbort !== next.onAbort ||
    prev.initialAnswers !== next.initialAnswers ||
    prev.onAnswerChange !== next.onAnswerChange ||
    prev.onFocusChange !== next.onFocusChange ||
    prev.workspaceId !== next.workspaceId ||
    prev.workspacePath !== next.workspacePath ||
    prev.onOpenWorkspaceFile !== next.onOpenWorkspaceFile
  )
    return false;
  return prev.turn.segments.every(
    (s) =>
      s.kind !== 'tool' ||
      !!s.endedActivity ||
      operationForTool(prev.activity, s.id) === operationForTool(next.activity, s.id)
  );
});

export default AssistantTurnView;
