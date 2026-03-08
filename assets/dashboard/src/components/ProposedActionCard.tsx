import { useState } from 'react';
import type { SpawnEntry } from '../lib/types.generated';
import styles from './ProposedActionCard.module.css';

function ConfidenceDots({ confidence }: { confidence: number }) {
  const filled = Math.round(confidence * 5);
  return (
    <span className={styles.confidenceDots} title={`Confidence: ${Math.round(confidence * 100)}%`}>
      {Array.from({ length: 5 }, (_, i) => (
        <span key={i} className={`${styles.dot}${i < filled ? ` ${styles.dotFilled}` : ''}`} />
      ))}
    </span>
  );
}

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

/** Parse skill_content frontmatter for triggers, procedure, quality_criteria. */
function parseSkillContent(content: string) {
  const result: { triggers: string[]; procedure: string; qualityCriteria: string } = {
    triggers: [],
    procedure: '',
    qualityCriteria: '',
  };
  if (!content.startsWith('---\n')) return result;
  const endIdx = content.indexOf('\n---', 4);
  if (endIdx < 0) return result;

  // Parse triggers from frontmatter
  const frontmatter = content.slice(4, endIdx);
  const triggerMatch = frontmatter.match(/triggers:\n((?:\s+-\s+.*\n?)*)/);
  if (triggerMatch) {
    result.triggers = triggerMatch[1]
      .split('\n')
      .map((line) =>
        line
          .replace(/^\s+-\s+/, '')
          .replace(/^["']|["']$/g, '')
          .trim()
      )
      .filter(Boolean);
  }

  // Parse body sections
  const body = content.slice(endIdx + 4);
  const procMatch = body.match(/## Procedure\s*\n([\s\S]*?)(?=\n## |$)/);
  if (procMatch) result.procedure = procMatch[1].trim();
  const qcMatch = body.match(/## Quality Criteria\s*\n([\s\S]*?)(?=\n## |$)/);
  if (qcMatch) result.qualityCriteria = qcMatch[1].trim();

  return result;
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
  const [expanded, setExpanded] = useState(false);
  const meta = entry.metadata;
  const parsed = meta?.skill_content ? parseSkillContent(meta.skill_content) : null;

  return (
    <div className={styles.actionCard} data-testid={`proposed-action-${entry.id}`}>
      <div className={styles.actionCardHeader}>
        <span className={styles.actionBadge} data-state={entry.state}>
          {entry.state}
        </span>
        <span className={styles.actionName}>{entry.name}</span>
        {meta && <ConfidenceDots confidence={meta.confidence} />}
        <ProvenanceMarker source={entry.source} />
      </div>

      {entry.prompt && <div className={styles.actionTemplate}>{entry.prompt}</div>}
      {entry.command && <div className={styles.actionTemplate}>{entry.command}</div>}

      {entry.description && <div className={styles.actionDescription}>{entry.description}</div>}

      {entry.skill_ref && !meta && (
        <div className={styles.learnedDefaults}>
          <span>
            <span className={styles.learnedLabel}>skill: </span>
            <span className={styles.learnedValue}>{entry.skill_ref}</span>
          </span>
        </div>
      )}

      {parsed && parsed.triggers.length > 0 && (
        <div className={styles.triggersList}>
          <span className={styles.sectionLabel}>triggers: </span>
          {parsed.triggers.map((t, i) => (
            <span key={i} className={styles.triggerChip}>
              {t}
            </span>
          ))}
        </div>
      )}

      {parsed && (parsed.procedure || parsed.qualityCriteria) && (
        <div className={styles.expandSection}>
          <button className={styles.expandToggle} onClick={() => setExpanded(!expanded)}>
            {expanded ? '▾ Hide details' : '▸ Show procedure & criteria'}
          </button>
          {expanded && (
            <div className={styles.expandedContent}>
              {parsed.procedure && (
                <div className={styles.skillSection}>
                  <div className={styles.sectionLabel}>Procedure</div>
                  <pre className={styles.skillPre}>{parsed.procedure}</pre>
                </div>
              )}
              {parsed.qualityCriteria && (
                <div className={styles.skillSection}>
                  <div className={styles.sectionLabel}>Quality Criteria</div>
                  <pre className={styles.skillPre}>{parsed.qualityCriteria}</pre>
                </div>
              )}
            </div>
          )}
        </div>
      )}

      {meta && meta.evidence && meta.evidence.length > 0 && (
        <div className={styles.evidenceList}>
          <div className={styles.evidenceLabel}>evidence ({meta.evidence_count} signals)</div>
          <ul>
            {meta.evidence.map((e, i) => (
              <li key={i}>{e}</li>
            ))}
          </ul>
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
