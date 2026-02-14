import { useState, useEffect, useCallback } from 'react';
import { useParams } from 'react-router-dom';
import {
  getLoreProposals,
  getLoreEntries,
  applyLoreProposal,
  dismissLoreProposal,
  getErrorMessage,
} from '../lib/api';
import { useToast } from '../components/ToastProvider';
import type { LoreProposal, LoreEntry } from '../lib/types';
import styles from '../styles/lore.module.css';

type SelectedProposal = {
  proposal: LoreProposal;
  activeFile: string;
};

export default function LorePage() {
  const { repoName } = useParams<{ repoName: string }>();
  const { success: toastSuccess, error: toastError } = useToast();

  const [loading, setLoading] = useState(true);
  const [error, setError] = useState('');
  const [proposals, setProposals] = useState<LoreProposal[]>([]);
  const [entries, setEntries] = useState<LoreEntry[]>([]);
  const [selected, setSelected] = useState<SelectedProposal | null>(null);
  const [applying, setApplying] = useState(false);
  const [showEntries, setShowEntries] = useState(false);

  const loadData = useCallback(async () => {
    if (!repoName) return;
    try {
      setLoading(true);
      setError('');
      const [proposalData, entryData] = await Promise.all([
        getLoreProposals(repoName),
        getLoreEntries(repoName),
      ]);
      setProposals(proposalData.proposals || []);
      setEntries(entryData.entries || []);
    } catch (err) {
      setError(getErrorMessage(err, 'Failed to load lore data'));
    } finally {
      setLoading(false);
    }
  }, [repoName]);

  useEffect(() => {
    loadData();
  }, [loadData]);

  const handleApply = async (proposal: LoreProposal) => {
    if (!repoName) return;
    setApplying(true);
    try {
      const result = await applyLoreProposal(repoName, proposal.id);
      toastSuccess(`Applied! Branch: ${result.branch}`);
      loadData();
      setSelected(null);
    } catch (err) {
      toastError(getErrorMessage(err, 'Failed to apply proposal'));
    } finally {
      setApplying(false);
    }
  };

  const handleDismiss = async (proposal: LoreProposal) => {
    if (!repoName) return;
    try {
      await dismissLoreProposal(repoName, proposal.id);
      toastSuccess('Proposal dismissed');
      loadData();
      setSelected(null);
    } catch (err) {
      toastError(getErrorMessage(err, 'Failed to dismiss proposal'));
    }
  };

  const statusBadge = (status: string) => {
    const cls =
      status === 'pending'
        ? styles.badgePending
        : status === 'applied'
          ? styles.badgeApplied
          : status === 'dismissed'
            ? styles.badgeDismissed
            : styles.badgeStale;
    return <span className={cls}>{status}</span>;
  };

  if (loading) {
    return <div className="page-loading">Loading lore...</div>;
  }

  if (error) {
    return (
      <div className="page-error">
        <p>{error}</p>
        <button onClick={loadData}>Retry</button>
      </div>
    );
  }

  const pendingCount = proposals.filter((p) => p.status === 'pending').length;
  const rawEntries = entries.filter((e) => !e.state_change);

  return (
    <div className={styles.container}>
      <div className={styles.header}>
        <h2>Lore — {repoName}</h2>
        <span className={styles.summary}>
          {pendingCount} pending proposal{pendingCount !== 1 ? 's' : ''} · {rawEntries.length} raw
          entries
        </span>
      </div>

      {/* Proposals Section */}
      <section className={styles.section}>
        <h3>Proposals</h3>
        {proposals.length === 0 ? (
          <p className={styles.empty}>
            No proposals yet. Lore entries will be curated into proposals when sessions are
            disposed.
          </p>
        ) : (
          <div className={styles.proposalList}>
            {proposals.map((p) => (
              <div
                key={p.id}
                className={`${styles.proposalCard} ${selected?.proposal.id === p.id ? styles.selected : ''}`}
                onClick={() =>
                  setSelected({
                    proposal: p,
                    activeFile: Object.keys(p.proposed_files)[0] || '',
                  })
                }
              >
                <div className={styles.proposalHeader}>
                  {statusBadge(p.status)}
                  <span className={styles.proposalDate}>
                    {new Date(p.created_at).toLocaleString()}
                  </span>
                </div>
                <div className={styles.proposalSummary}>{p.diff_summary}</div>
                <div className={styles.proposalMeta}>
                  {p.source_count} entries from {p.sources?.length || 0} workspace
                  {(p.sources?.length || 0) !== 1 ? 's' : ''}
                  {' · '}
                  {Object.keys(p.proposed_files).length} file
                  {Object.keys(p.proposed_files).length !== 1 ? 's' : ''}
                </div>
              </div>
            ))}
          </div>
        )}
      </section>

      {/* Selected Proposal Detail */}
      {selected && (
        <section className={styles.section}>
          <h3>Proposal Detail</h3>
          <div className={styles.fileTabs}>
            {Object.keys(selected.proposal.proposed_files).map((file) => (
              <button
                key={file}
                className={`${styles.fileTab} ${selected.activeFile === file ? styles.activeTab : ''}`}
                onClick={() => setSelected({ ...selected, activeFile: file })}
              >
                {file}
              </button>
            ))}
          </div>
          <pre className={styles.fileContent}>
            {selected.proposal.proposed_files[selected.activeFile]}
          </pre>
          {selected.proposal.status === 'pending' && (
            <div className={styles.actions}>
              <button
                className={styles.applyButton}
                onClick={() => handleApply(selected.proposal)}
                disabled={applying}
              >
                {applying ? 'Applying...' : 'Apply'}
              </button>
              <button
                className={styles.dismissButton}
                onClick={() => handleDismiss(selected.proposal)}
              >
                Dismiss
              </button>
            </div>
          )}
          {selected.proposal.entries_used?.length > 0 && (
            <div className={styles.entriesUsed}>
              <h4>Entries Used</h4>
              <ul>
                {selected.proposal.entries_used.map((e, i) => (
                  <li key={i}>{e}</li>
                ))}
              </ul>
            </div>
          )}
        </section>
      )}

      {/* Raw Entries Section */}
      <section className={styles.section}>
        <h3>
          <button className={styles.toggleButton} onClick={() => setShowEntries(!showEntries)}>
            {showEntries ? '▼' : '▶'} Raw Entries ({rawEntries.length})
          </button>
        </h3>
        {showEntries && (
          <div className={styles.entriesList}>
            {rawEntries.length === 0 ? (
              <p className={styles.empty}>No raw lore entries yet.</p>
            ) : (
              rawEntries.map((e, i) => (
                <div key={i} className={styles.entryCard}>
                  <div className={styles.entryMeta}>
                    <span className={styles.entryAgent}>{e.agent}</span>
                    <span className={styles.entryType}>{e.type}</span>
                    <span className={styles.entryWs}>{e.ws}</span>
                    <span className={styles.entryTs}>{new Date(e.ts).toLocaleString()}</span>
                  </div>
                  <div className={styles.entryText}>{e.text}</div>
                </div>
              ))
            )}
          </div>
        )}
      </section>
    </div>
  );
}
