import { memo } from 'react';
import ReactMarkdown from 'react-markdown';
import remarkGfm from 'remark-gfm';
import styles from './chat.module.css';
import ToolCallRow from './ToolCallRow';
import ThinkingDisclosure from './ThinkingDisclosure';
import PermissionCard from './PermissionCard';
import QuestionCard from './QuestionCard';
import UserMessageBubble from './UserMessageBubble';
import type { AssistantTurn } from '../../lib/chat/types';

interface AssistantTurnViewProps {
  turn: AssistantTurn;
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
}

function AssistantTurnViewInner({ turn, onPermission, onAnswer, onAbort }: AssistantTurnViewProps) {
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
                <ReactMarkdown remarkPlugins={[remarkGfm]}>{s.text}</ReactMarkdown>
              </div>
            );
          case 'thinking':
            return s.text.trim() ? <ThinkingDisclosure key={i} text={s.text} /> : null;
          case 'tool':
            return <ToolCallRow key={i} tool={s} />;
          case 'pending':
            return s.questions ? (
              <QuestionCard key={i} pending={s} onAnswer={onAnswer} />
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

// Memoize: the parent transcript renders one AssistantTurnView per turn. A
// burst of deltas updates only the open turn's object reference; closed turns
// keep their previous object and skip the render.
const AssistantTurnView = memo(AssistantTurnViewInner, (prev, next) => prev.turn === next.turn);

export default AssistantTurnView;
