import type { Action } from '../lib/types.generated';
import styles from './ProposedActionCard.module.css';

function ConfidenceDots({ confidence }: { confidence: number }) {
  const filled = Math.round(confidence * 4);
  return (
    <span className={styles.confidenceDots} data-testid="confidence-dots">
      {[0, 1, 2, 3].map((i) => (
        <span
          key={i}
          className={`${styles.dot} ${i < filled ? styles.dotFilled : ''}`}
          data-filled={i < filled}
        />
      ))}
    </span>
  );
}

export function ProposedActionCard({
  action,
  onPin,
  onDismiss,
}: {
  action: Action;
  onPin: (a: Action) => void;
  onDismiss: (a: Action) => void;
}) {
  return (
    <div className={styles.actionCard} data-testid={`proposed-action-${action.id}`}>
      <div className={styles.actionCardHeader}>
        <span className={styles.actionBadge} data-state={action.state}>
          {action.state}
        </span>
        <span className={styles.actionName}>{action.name}</span>
        <ConfidenceDots confidence={action.confidence} />
      </div>

      {action.template && <div className={styles.actionTemplate}>{action.template}</div>}

      {/* Learned defaults */}
      {(action.learned_target || action.learned_persona) && (
        <div className={styles.learnedDefaults}>
          {action.learned_target && (
            <span>
              <span className={styles.learnedLabel}>target: </span>
              <span className={styles.learnedValue}>{action.learned_target.value}</span>
            </span>
          )}
          {action.learned_persona && (
            <span>
              <span className={styles.learnedLabel}>persona: </span>
              <span className={styles.learnedValue}>{action.learned_persona.value}</span>
            </span>
          )}
        </div>
      )}

      {/* Evidence count */}
      {(action.evidence_count ?? 0) > 0 && (
        <div className={styles.evidenceList}>
          <span className={styles.evidenceLabel}>
            Based on {action.evidence_count} similar prompt{action.evidence_count !== 1 ? 's' : ''}
          </span>
        </div>
      )}

      {/* Actions */}
      <div className={styles.actionActions}>
        <button className={styles.dismissButton} onClick={() => onDismiss(action)}>
          Dismiss
        </button>
        <button className={styles.pinButton} onClick={() => onPin(action)}>
          Pin
        </button>
      </div>
    </div>
  );
}

export function PinnedActionRow({ action }: { action: Action }) {
  return (
    <div className={styles.pinnedRow}>
      <ConfidenceDots confidence={action.confidence} />
      <span className={styles.pinnedName}>{action.name}</span>
      <span className={styles.pinnedTemplate}>{action.template || action.command || ''}</span>
      <span className={styles.pinnedUseCount}>
        {(action.use_count ?? 0) > 0
          ? `${action.use_count} use${action.use_count !== 1 ? 's' : ''}`
          : ''}
      </span>
    </div>
  );
}
