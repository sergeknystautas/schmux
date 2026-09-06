import styles from './chat.module.css';
import type { PendingSegment } from '../../lib/chat/types';
import { summarizeTool } from './ToolCallRow';

interface PermissionCardProps {
  pending: PendingSegment;
  onPermission(
    requestId: string,
    allow: boolean,
    updatedInput?: Record<string, unknown>,
    message?: string
  ): void;
}

export default function PermissionCard({ pending, onPermission }: PermissionCardProps) {
  const summary = summarizeTool({
    name: pending.toolName,
    input: pending.input,
    inputJson: JSON.stringify(pending.input),
  });
  return (
    <div className={styles.card} data-testid="chat-permission-card">
      <div className={styles.cardTitle}>{pending.toolName} needs permission</div>
      <div className={styles.cardSummary}>{summary}</div>
      {typeof pending.input.reason === 'string' && pending.input.reason && (
        <div className={styles.cardSummary} data-testid="chat-permission-reason">
          {pending.input.reason}
        </div>
      )}
      <div className={styles.cardActions}>
        <button
          type="button"
          className="btn btn--primary btn--sm"
          onClick={() => onPermission(pending.requestId, true, pending.input)}
        >
          Allow
        </button>
        <button
          type="button"
          className="btn btn--secondary btn--sm"
          onClick={() =>
            onPermission(pending.requestId, false, undefined, 'Denied from the schmux chat')
          }
        >
          Deny
        </button>
      </div>
    </div>
  );
}
