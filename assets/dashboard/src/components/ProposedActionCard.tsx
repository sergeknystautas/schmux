import type { SpawnEntry } from '../lib/types.generated';
import styles from './ProposedActionCard.module.css';

function ProvenanceMarker({ source }: { source: string }) {
  switch (source) {
    case 'built-in':
      return (
        <span className={styles.provenanceMarker} title="Built-in">
          &#9632;
        </span>
      );
    case 'emerged':
      return (
        <span className={styles.provenanceMarker} title="Emerged">
          &#9673;
        </span>
      );
    case 'manual':
      return (
        <span className={styles.provenanceMarker} title="Manual">
          &#9675;
        </span>
      );
    default:
      return null;
  }
}

export function ProposedActionCard({
  entry,
  onPin,
  onDismiss,
}: {
  entry: SpawnEntry;
  onPin: (e: SpawnEntry) => void;
  onDismiss: (e: SpawnEntry) => void;
}) {
  return (
    <div className={styles.actionCard} data-testid={`proposed-action-${entry.id}`}>
      <div className={styles.actionCardHeader}>
        <span className={styles.actionBadge} data-state={entry.state}>
          {entry.state}
        </span>
        <span className={styles.actionName}>{entry.name}</span>
        <ProvenanceMarker source={entry.source} />
      </div>

      {entry.prompt && <div className={styles.actionTemplate}>{entry.prompt}</div>}
      {entry.command && <div className={styles.actionTemplate}>{entry.command}</div>}

      {entry.skill_ref && (
        <div className={styles.learnedDefaults}>
          <span>
            <span className={styles.learnedLabel}>skill: </span>
            <span className={styles.learnedValue}>{entry.skill_ref}</span>
          </span>
        </div>
      )}

      {/* Actions */}
      <div className={styles.actionActions}>
        <button className={styles.dismissButton} onClick={() => onDismiss(entry)}>
          Dismiss
        </button>
        <button className={styles.pinButton} onClick={() => onPin(entry)}>
          Pin
        </button>
      </div>
    </div>
  );
}

export function PinnedActionRow({ entry }: { entry: SpawnEntry }) {
  return (
    <div className={styles.pinnedRow}>
      <ProvenanceMarker source={entry.source} />
      <span className={styles.pinnedName}>{entry.name}</span>
      <span className={styles.pinnedTemplate}>{entry.prompt || entry.command || ''}</span>
      <span className={styles.pinnedUseCount}>
        {entry.use_count > 0 ? `${entry.use_count} use${entry.use_count !== 1 ? 's' : ''}` : ''}
      </span>
    </div>
  );
}
