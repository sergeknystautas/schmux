import { useState, useEffect, useCallback, useRef } from 'react';
import {
  getLoreProposals,
  getLoreEntries,
  applyLoreProposal,
  dismissLoreProposal,
  triggerLoreCuration,
  getErrorMessage,
} from '../lib/api';
import { useConfig } from '../contexts/ConfigContext';
import { useToast } from '../components/ToastProvider';
import type { LoreProposal, LoreEntry } from '../lib/types';
import styles from '../styles/lore.module.css';

type SelectedProposal = {
  proposal: LoreProposal;
  activeFile: string;
};

export default function LorePage() {
  const { config } = useConfig();
  const repos = config?.repos || [];
  const { success: toastSuccess, error: toastError } = useToast();

  const [activeRepo, setActiveRepo] = useState(repos[0]?.name || '');
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState('');
  const [proposals, setProposals] = useState<LoreProposal[]>([]);
  const [entries, setEntries] = useState<LoreEntry[]>([]);
  // Track all unique agents/types from unfiltered entries so filter dropdowns don't narrow
  const [allAgents, setAllAgents] = useState<string[]>([]);
  const [allTypes, setAllTypes] = useState<string[]>([]);
  const [selected, setSelected] = useState<SelectedProposal | null>(null);
  const [applying, setApplying] = useState(false);
  const [showEntries, setShowEntries] = useState(false);

  // Edit mode state
  const [editing, setEditing] = useState(false);
  const [editedFiles, setEditedFiles] = useState<Record<string, string>>({});

  // Re-curate state
  const [curating, setCurating] = useState(false);

  // Entry filter state
  const [entryFilters, setEntryFilters] = useState<{
    state?: string;
    agent?: string;
    type?: string;
  }>({});

  // Sync activeRepo when repos list changes (e.g., config loaded after mount)
  useEffect(() => {
    if (repos.length > 0 && !repos.find((r) => r.name === activeRepo)) {
      setActiveRepo(repos[0].name);
    }
  }, [repos, activeRepo]);

  const loadProposals = useCallback(async () => {
    if (!activeRepo) return;
    try {
      const proposalData = await getLoreProposals(activeRepo);
      setProposals(proposalData.proposals || []);
    } catch (err) {
      setError(getErrorMessage(err, 'Failed to load lore proposals'));
    }
  }, [activeRepo]);

  const loadEntries = useCallback(async () => {
    if (!activeRepo) return;
    try {
      const entryData = await getLoreEntries(activeRepo, entryFilters);
      setEntries(entryData.entries || []);
    } catch (err) {
      setError(getErrorMessage(err, 'Failed to load lore entries'));
    }
  }, [activeRepo, entryFilters]);

  // Load all unique agents/types from unfiltered entries (S10 fix)
  const loadFilterOptions = useCallback(async () => {
    if (!activeRepo) return;
    try {
      const entryData = await getLoreEntries(activeRepo);
      const agents = new Set<string>();
      const types = new Set<string>();
      for (const e of entryData.entries || []) {
        if (e.agent) agents.add(e.agent);
        if (e.type) types.add(e.type);
      }
      setAllAgents(Array.from(agents).sort());
      setAllTypes(Array.from(types).sort());
    } catch {
      // Filter options are non-critical; silently ignore errors
    }
  }, [activeRepo]);

  const loadData = useCallback(async () => {
    setLoading(true);
    setError('');
    await Promise.all([loadProposals(), loadEntries(), loadFilterOptions()]);
    setLoading(false);
  }, [loadProposals, loadEntries, loadFilterOptions]);

  // Initial load when repo changes
  useEffect(() => {
    loadData();
  }, [loadData]);

  // Reload only entries when filters change (skip on initial mount)
  const filtersInitialized = useRef(false);
  useEffect(() => {
    if (!filtersInitialized.current) {
      filtersInitialized.current = true;
      return;
    }
    loadEntries();
  }, [entryFilters]); // eslint-disable-line react-hooks/exhaustive-deps

  const handleTabChange = (repoName: string) => {
    setActiveRepo(repoName);
    // Reset per-repo UI state
    setSelected(null);
    setEditing(false);
    setEditedFiles({});
    setShowEntries(false);
    setEntryFilters({});
    filtersInitialized.current = false;
  };

  const handleApply = async (proposal: LoreProposal, overrides?: Record<string, string>) => {
    if (!activeRepo) return;
    setApplying(true);
    try {
      const result = await applyLoreProposal(activeRepo, proposal.id, overrides);
      toastSuccess(`Applied! Branch: ${result.branch}`);
      setEditing(false);
      setEditedFiles({});
      loadData();
      setSelected(null);
    } catch (err) {
      toastError(getErrorMessage(err, 'Failed to apply proposal'));
    } finally {
      setApplying(false);
    }
  };

  const handleDismiss = async (proposal: LoreProposal) => {
    if (!activeRepo) return;
    try {
      await dismissLoreProposal(activeRepo, proposal.id);
      toastSuccess('Proposal dismissed');
      loadData();
      setSelected(null);
    } catch (err) {
      toastError(getErrorMessage(err, 'Failed to dismiss proposal'));
    }
  };

  const handleReCurate = async () => {
    if (!activeRepo) return;
    setCurating(true);
    try {
      await triggerLoreCuration(activeRepo);
      toastSuccess('Re-curation triggered');
      loadData();
    } catch (err) {
      toastError(getErrorMessage(err, 'Failed to trigger curation'));
    } finally {
      setCurating(false);
    }
  };

  const handleEditAndApply = () => {
    if (!selected) return;
    setEditing(true);
    setEditedFiles({ ...selected.proposal.proposed_files });
  };

  const handleCancelEdit = () => {
    setEditing(false);
    setEditedFiles({});
  };

  const handleSaveAndApply = () => {
    if (!selected) return;
    handleApply(selected.proposal, editedFiles);
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

  // Use pre-loaded unfiltered agents/types for filter dropdowns
  const uniqueAgents = allAgents;
  const uniqueTypes = allTypes;

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

  // Render file content with line numbers and diff-style highlighting
  const renderFileContent = (content: string) => {
    const lines = content.split('\n');
    return (
      <div className={styles.codeBlock}>
        <div className={styles.lineNumbers}>
          {lines.map((_, i) => (
            <span key={i} className={styles.lineNumber}>
              {i + 1}
            </span>
          ))}
        </div>
        <pre className={styles.codeLines}>
          {lines.map((line, i) => (
            <span key={i} className={styles.codeLine}>
              {line}
              {'\n'}
            </span>
          ))}
        </pre>
      </div>
    );
  };

  return (
    <div className={styles.container} data-testid="lore-page">
      <div className={styles.header}>
        <h2>Lore</h2>
        <span className={styles.summary}>
          {pendingCount} pending proposal{pendingCount !== 1 ? 's' : ''} · {rawEntries.length} raw
          entries
        </span>
      </div>

      {/* Repo tabs */}
      {repos.length > 1 && (
        <div className="repo-tabs">
          {repos.map((repo) => (
            <button
              key={repo.name}
              className={`repo-tab${activeRepo === repo.name ? ' repo-tab--active' : ''}`}
              onClick={() => handleTabChange(repo.name)}
            >
              {repo.name}
            </button>
          ))}
        </div>
      )}

      {/* Proposals Section */}
      <section className={styles.section}>
        <h3>Proposals</h3>
        {proposals.length === 0 ? (
          <p className={styles.empty}>
            No proposals yet. Lore entries will be curated into proposals when sessions are
            disposed.
          </p>
        ) : (
          <div className={styles.proposalList} data-testid="lore-proposals">
            {proposals.map((p) => (
              <div
                key={p.id}
                data-testid={`lore-proposal-card-${p.id}`}
                className={`${styles.proposalCard} ${selected?.proposal.id === p.id ? styles.selected : ''}`}
                onClick={() => {
                  setEditing(false);
                  setEditedFiles({});
                  setSelected({
                    proposal: p,
                    activeFile: Object.keys(p.proposed_files)[0] || '',
                  });
                }}
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

          {/* Diff summary banner */}
          {selected.proposal.diff_summary && (
            <div className={styles.diffSummaryBanner}>{selected.proposal.diff_summary}</div>
          )}

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

          {editing ? (
            <textarea
              className={styles.editTextarea}
              value={editedFiles[selected.activeFile] || ''}
              onChange={(e) =>
                setEditedFiles({ ...editedFiles, [selected.activeFile]: e.target.value })
              }
              spellCheck={false}
            />
          ) : (
            renderFileContent(selected.proposal.proposed_files[selected.activeFile] || '')
          )}

          <div className={styles.actions} data-testid="lore-actions">
            {selected.proposal.status === 'pending' && !editing && (
              <>
                <button
                  className={styles.applyButton}
                  data-testid="lore-apply-button"
                  onClick={() => handleApply(selected.proposal)}
                  disabled={applying}
                >
                  {applying ? 'Applying...' : 'Apply'}
                </button>
                <button
                  className={styles.editApplyButton}
                  data-testid="lore-edit-apply-button"
                  onClick={handleEditAndApply}
                >
                  Edit & Apply
                </button>
                <button
                  className={styles.dismissButton}
                  data-testid="lore-dismiss-button"
                  onClick={() => handleDismiss(selected.proposal)}
                >
                  Dismiss
                </button>
              </>
            )}
            {selected.proposal.status === 'pending' && editing && (
              <>
                <button
                  className={styles.applyButton}
                  data-testid="lore-save-apply-button"
                  onClick={handleSaveAndApply}
                  disabled={applying}
                >
                  {applying ? 'Applying...' : 'Save & Apply'}
                </button>
                <button
                  className={styles.dismissButton}
                  data-testid="lore-cancel-edit-button"
                  onClick={handleCancelEdit}
                >
                  Cancel
                </button>
              </>
            )}
            {selected.proposal.status === 'stale' && (
              <>
                <button
                  className={styles.reCurateButton}
                  data-testid="lore-curate-button"
                  onClick={handleReCurate}
                  disabled={curating}
                >
                  {curating ? 'Re-curating...' : 'Re-curate'}
                </button>
                <button
                  className={styles.dismissButton}
                  data-testid="lore-dismiss-stale-button"
                  onClick={() => handleDismiss(selected.proposal)}
                >
                  Dismiss
                </button>
              </>
            )}
          </div>

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
            {showEntries ? '\u25BC' : '\u25B6'} Raw Entries ({rawEntries.length})
          </button>
        </h3>
        {showEntries && (
          <>
            <div className={styles.filterBar} data-testid="lore-filter-bar">
              <select
                className={styles.filterSelect}
                data-testid="lore-filter-state"
                value={entryFilters.state || ''}
                onChange={(e) =>
                  setEntryFilters({ ...entryFilters, state: e.target.value || undefined })
                }
              >
                <option value="">All states</option>
                <option value="raw">raw</option>
                <option value="proposed">proposed</option>
                <option value="applied">applied</option>
                <option value="dismissed">dismissed</option>
              </select>
              <select
                className={styles.filterSelect}
                data-testid="lore-filter-agent"
                value={entryFilters.agent || ''}
                onChange={(e) =>
                  setEntryFilters({ ...entryFilters, agent: e.target.value || undefined })
                }
              >
                <option value="">All agents</option>
                {uniqueAgents.map((a) => (
                  <option key={a} value={a}>
                    {a}
                  </option>
                ))}
              </select>
              <select
                className={styles.filterSelect}
                data-testid="lore-filter-type"
                value={entryFilters.type || ''}
                onChange={(e) =>
                  setEntryFilters({ ...entryFilters, type: e.target.value || undefined })
                }
              >
                <option value="">All types</option>
                {uniqueTypes.map((t) => (
                  <option key={t} value={t}>
                    {t}
                  </option>
                ))}
              </select>
            </div>
            <div className={styles.entriesList} data-testid="lore-entries">
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
          </>
        )}
      </section>
    </div>
  );
}
