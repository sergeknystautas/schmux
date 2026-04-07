import { useState } from 'react';
import type { LoreRule, LoreLayer, RuleSourceEntry } from '../lib/types';
import type { SpawnEntry } from '../lib/types.generated';
import styles from './LoreCard.module.css';

const LAYER_LABELS: Record<string, string> = {
  repo_public: 'Public',
  repo_private: 'Private \u00b7 this repo',
  cross_repo_private: 'Private \u00b7 all repos',
};

type InstructionCardProps = {
  type: 'instruction';
  rule: LoreRule;
  repoName: string;
  proposalId: string;
  onApprove: (ruleId: string) => void;
  onDismiss: (ruleId: string) => void;
  onEdit: (ruleId: string, newText: string) => void;
  onLayerChange: (ruleId: string, layer: LoreLayer) => void;
  onUnapprove?: (ruleId: string) => void;
};

type ActionCardProps = {
  type: 'action';
  action: SpawnEntry;
  repoName: string;
  proposalId?: never;
  onApprove: (id: string) => void;
  onDismiss: (id: string) => void;
  onEdit: (id: string, newText: string) => void;
  onLayerChange?: never;
  onUnapprove?: never;
};

type LoreCardProps = InstructionCardProps | ActionCardProps;

function SourceSignals({ entries }: { entries: RuleSourceEntry[] }) {
  if (!entries || entries.length === 0) return null;
  return (
    <>
      <div className={styles.signalDivider} />
      {entries.map((signal, i) => (
        <div key={i} className={styles.signalRow} data-signal-type={signal.type}>
          {signal.type === 'failure'
            ? `${signal.input_summary} \u2192 "${signal.error_summary}"`
            : signal.text}
        </div>
      ))}
    </>
  );
}

function EvidenceSignals({ evidence }: { evidence: string[] }) {
  if (!evidence || evidence.length === 0) return null;
  return (
    <>
      <div className={styles.signalDivider} />
      {evidence.map((e, i) => (
        <div key={i} className={styles.signalRow} data-signal-type="reflection">
          {e}
        </div>
      ))}
    </>
  );
}

function layerFromChecks(commitToRepo: boolean, applyAll: boolean): LoreLayer {
  if (applyAll) return 'cross_repo_private';
  if (commitToRepo) return 'repo_public';
  return 'repo_private';
}

export function LoreCard(props: LoreCardProps) {
  const { type, repoName, onApprove, onDismiss, onEdit } = props;

  const id = type === 'instruction' ? props.rule.id : props.action.id;
  const currentText =
    type === 'instruction'
      ? props.rule.text
      : props.action.prompt || props.action.command || props.action.description || '';
  const category = type === 'instruction' ? props.rule.category : undefined;
  const status = type === 'instruction' ? props.rule.status : props.action.state;
  const chosenLayer =
    type === 'instruction' ? props.rule.chosen_layer || props.rule.suggested_layer : undefined;

  const [editing, setEditing] = useState(false);
  const [editText, setEditText] = useState(currentText);
  const [dismissing, setDismissing] = useState(false);

  // Privacy controls state (instruction only)
  const [commitToRepo, setCommitToRepo] = useState(chosenLayer === 'repo_public');
  const [applyAll, setApplyAll] = useState(chosenLayer === 'cross_repo_private');

  // Approved / collapsed state
  if (status === 'approved') {
    return (
      <div className={styles.cardCollapsed} data-testid={`lore-card-${id}`}>
        <span className={styles.collapsedCheck}>{'\u2713'}</span>
        <span className={styles.collapsedText}>
          {currentText.length > 80 ? currentText.slice(0, 80) + '\u2026' : currentText}
        </span>
        {chosenLayer && (
          <span className={styles.collapsedLayer}>{LAYER_LABELS[chosenLayer] || chosenLayer}</span>
        )}
        {type === 'instruction' && props.onUnapprove && (
          <button
            className={styles.undoButton}
            onClick={() => props.onUnapprove!(id)}
            aria-label="Undo approval"
          >
            Undo
          </button>
        )}
      </div>
    );
  }

  const handleDismiss = () => {
    setDismissing(true);
    setTimeout(() => onDismiss(id), 200);
  };

  const handleSave = () => {
    onEdit(id, editText);
    setEditing(false);
  };

  const handleCancel = () => {
    setEditText(currentText);
    setEditing(false);
  };

  const handleCommitToRepoChange = (checked: boolean) => {
    setCommitToRepo(checked);
    if (!checked) setApplyAll(false);
    const layer = layerFromChecks(checked, false);
    if (type === 'instruction') {
      props.onLayerChange(id, layer);
    }
  };

  const handleApplyAllChange = (checked: boolean) => {
    setApplyAll(checked);
    const layer = layerFromChecks(commitToRepo, checked);
    if (type === 'instruction') {
      props.onLayerChange(id, layer);
    }
  };

  const cardClassName = `${styles.card}${dismissing ? ` ${styles.cardDismissing}` : ''}`;

  return (
    <div className={cardClassName} data-testid={`lore-card-${id}`}>
      {/* Header */}
      <div className={styles.cardHeader}>
        <div className={styles.headerLeft}>
          <span className={styles.typeLabel}>{type}</span>
          {category && <span className={styles.categoryTag}>{category}</span>}
        </div>
        <span className={styles.repoName}>{repoName}</span>
      </div>

      {/* Body */}
      {editing ? (
        <div className={styles.editArea}>
          <textarea
            className={styles.editTextarea}
            value={editText}
            onChange={(e) => setEditText(e.target.value)}
            rows={3}
          />
          <div className={styles.editActions}>
            <button className={styles.dismissButton} onClick={handleCancel}>
              Cancel
            </button>
            <button className={styles.approveButton} onClick={handleSave}>
              Save
            </button>
          </div>
        </div>
      ) : type === 'instruction' ? (
        <div className={styles.ruleText}>{currentText}</div>
      ) : (
        <>
          <div className={styles.ruleText}>{props.action.name}</div>
          {currentText && <div className={styles.actionPrompt}>{currentText}</div>}
        </>
      )}

      {/* Source signals */}
      {type === 'instruction' && <SourceSignals entries={props.rule.source_entries} />}
      {type === 'action' && <EvidenceSignals evidence={props.action.metadata?.evidence || []} />}

      {/* Privacy controls (instruction only) */}
      {type === 'instruction' && !editing && (
        <div className={styles.privacyControls}>
          <label className={styles.privacyLabel}>
            <input
              type="checkbox"
              checked={commitToRepo}
              onChange={(e) => handleCommitToRepoChange(e.target.checked)}
            />
            Commit to repo (visible to collaborators)
          </label>
          {commitToRepo && (
            <label className={`${styles.privacyLabel} ${styles.privacyNested}`}>
              <input
                type="checkbox"
                checked={applyAll}
                onChange={(e) => handleApplyAllChange(e.target.checked)}
              />
              Apply to all my repos
            </label>
          )}
        </div>
      )}

      {/* Action buttons */}
      {!editing && (
        <div className={styles.actions}>
          <button className={styles.dismissButton} onClick={handleDismiss}>
            Dismiss
          </button>
          <button className={styles.editButton} onClick={() => setEditing(true)}>
            Edit
          </button>
          <button className={styles.approveButton} onClick={() => onApprove(id)}>
            Approve
          </button>
        </div>
      )}
    </div>
  );
}
