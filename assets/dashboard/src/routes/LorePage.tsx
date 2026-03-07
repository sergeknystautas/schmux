import { useState, useEffect, useCallback, useRef } from 'react';
import { useSearchParams } from 'react-router-dom';
import ReactDiffViewer, { DiffMethod } from 'react-diff-viewer-continued';
import {
  getLoreProposals,
  getLoreEntries,
  getLoreStatus,
  dismissLoreProposal,
  updateLoreRule,
  startLoreMerge,
  applyLoreMerge,
  clearLoreEntries,
  getLoreCurations,
  getLoreCurationLog,
  getActions,
  getProposedActions,
  pinAction,
  dismissAction,
  getErrorMessage,
} from '../lib/api';
import type { CurationRunInfo } from '../lib/api';
import type { Action } from '../lib/types.generated';
import { useConfig } from '../contexts/ConfigContext';
import { useCuration } from '../contexts/CurationContext';
import { useToast } from '../components/ToastProvider';
import { useModal } from '../components/ModalProvider';
import useTheme from '../hooks/useTheme';
import CuratorTerminal from '../components/CuratorTerminal';
import { ProposedActionCard, PinnedActionRow } from '../components/ProposedActionCard';
import type {
  LoreProposal,
  LoreEntry,
  LoreStatusResponse,
  CuratorStreamEvent,
  LoreRule,
  LoreLayer,
} from '../lib/types';
import styles from '../styles/lore.module.css';

const LAYER_LABELS: Record<LoreLayer, string> = {
  repo_public: 'Public',
  repo_private: 'Private',
  user_global: 'Global',
};

function RuleRow({
  rule,
  repoName,
  proposalID,
  onUpdate,
}: {
  rule: LoreRule;
  repoName: string;
  proposalID: string;
  onUpdate: (updated: LoreProposal) => void;
}) {
  const [editing, setEditing] = useState(false);
  const [editText, setEditText] = useState(rule.text);
  const [saving, setSaving] = useState(false);
  const { alert } = useModal();

  const effectiveLayer = rule.chosen_layer || rule.suggested_layer;

  const handleStatusChange = async (status: 'approved' | 'dismissed') => {
    setSaving(true);
    try {
      const updated = await updateLoreRule(repoName, proposalID, rule.id, { status });
      onUpdate(updated);
    } catch (err) {
      alert('Update Failed', getErrorMessage(err, 'Failed to update rule'));
    } finally {
      setSaving(false);
    }
  };

  const handleLayerChange = async (layer: LoreLayer) => {
    setSaving(true);
    try {
      const updated = await updateLoreRule(repoName, proposalID, rule.id, {
        chosen_layer: layer,
      });
      onUpdate(updated);
    } catch (err) {
      alert('Update Failed', getErrorMessage(err, 'Failed to update rule layer'));
    } finally {
      setSaving(false);
    }
  };

  const handleSaveText = async () => {
    if (editText === rule.text) {
      setEditing(false);
      return;
    }
    setSaving(true);
    try {
      const updated = await updateLoreRule(repoName, proposalID, rule.id, { text: editText });
      onUpdate(updated);
      setEditing(false);
    } catch (err) {
      alert('Update Failed', getErrorMessage(err, 'Failed to update rule text'));
    } finally {
      setSaving(false);
    }
  };

  return (
    <div className={styles.ruleRow} data-status={rule.status} data-testid={`rule-${rule.id}`}>
      <div className={styles.ruleHeader}>
        <span className={styles.ruleBadge} data-status={rule.status}>
          {rule.status}
        </span>
        {rule.category && <span className={styles.ruleCategory}>{rule.category}</span>}
        <span className={styles.ruleLayer}>{LAYER_LABELS[effectiveLayer]}</span>
      </div>

      {editing ? (
        <div className={styles.ruleEditArea}>
          <textarea
            className={styles.ruleTextarea}
            value={editText}
            onChange={(e) => setEditText(e.target.value)}
            rows={3}
          />
          <div className={styles.ruleEditActions}>
            <button className={styles.dismissButton} onClick={() => setEditing(false)}>
              Cancel
            </button>
            <button className={styles.applyButton} onClick={handleSaveText} disabled={saving}>
              Save
            </button>
          </div>
        </div>
      ) : (
        <div
          className={styles.ruleText}
          onClick={() => rule.status === 'pending' && setEditing(true)}
        >
          {rule.text}
        </div>
      )}

      {rule.status === 'pending' && (
        <div className={styles.ruleActions}>
          <div className={styles.layerPicker}>
            {(['repo_public', 'repo_private', 'user_global'] as LoreLayer[]).map((layer) => (
              <label key={layer} className={styles.layerLabel}>
                <input
                  type="radio"
                  name={`layer-${rule.id}`}
                  checked={effectiveLayer === layer}
                  onChange={() => handleLayerChange(layer)}
                  disabled={saving}
                />
                {LAYER_LABELS[layer]}
              </label>
            ))}
          </div>
          <div className={styles.ruleButtons}>
            <button
              className={styles.dismissButton}
              onClick={() => handleStatusChange('dismissed')}
              disabled={saving}
            >
              Dismiss
            </button>
            <button
              className={styles.applyButton}
              onClick={() => handleStatusChange('approved')}
              disabled={saving}
            >
              Approve
            </button>
          </div>
        </div>
      )}
    </div>
  );
}

function RuleReviewCard({
  proposal,
  repoName,
  onProposalUpdate,
  onDismissProposal,
}: {
  proposal: LoreProposal;
  repoName: string;
  onProposalUpdate: (p: LoreProposal) => void;
  onDismissProposal: (p: LoreProposal) => void;
}) {
  const { theme } = useTheme();
  const { alert } = useModal();
  const { success: toastSuccess } = useToast();
  const [editedPreviews, setEditedPreviews] = useState<Record<string, string>>({});
  const [applying, setApplying] = useState(false);

  const approvedCount = proposal.rules.filter((r) => r.status === 'approved').length;
  const pendingCount = proposal.rules.filter((r) => r.status === 'pending').length;
  const totalCount = proposal.rules.length;

  const isMerging = proposal.status === 'merging';
  const previews = proposal.merge_previews;
  const mergeError = proposal.merge_error;

  // Poll while merging to pick up previews when the background job finishes.
  useEffect(() => {
    if (!isMerging) return;
    const interval = setInterval(async () => {
      try {
        const updated = await getLoreProposals(repoName);
        const refreshed = (updated.proposals || []).find((p) => p.id === proposal.id);
        if (refreshed && refreshed.status !== 'merging') {
          onProposalUpdate(refreshed);
        }
      } catch {
        // ignore polling errors
      }
    }, 3000);
    return () => clearInterval(interval);
  }, [isMerging, repoName, proposal.id, onProposalUpdate]);

  // Initialize editedPreviews when previews arrive from the server
  useEffect(() => {
    if (previews && previews.length > 0) {
      const initial: Record<string, string> = {};
      for (const p of previews) {
        initial[p.layer] = p.merged_content;
      }
      setEditedPreviews(initial);
    }
  }, [previews]);

  const handleMerge = async () => {
    try {
      await startLoreMerge(repoName, proposal.id);
      // Optimistically update the proposal status to show the spinner
      onProposalUpdate({
        ...proposal,
        status: 'merging',
        merge_previews: undefined,
        merge_error: undefined,
      });
    } catch (err) {
      alert('Merge Failed', getErrorMessage(err, 'Failed to start merge'));
    }
  };

  const handleDismissPreviews = async () => {
    // Clear previews from the proposal on the server
    onProposalUpdate({ ...proposal, merge_previews: undefined, merge_error: undefined });
  };

  const handleApplyMerge = async () => {
    if (!previews) return;
    setApplying(true);
    try {
      const merges = previews.map((p) => ({
        layer: p.layer,
        content: editedPreviews[p.layer] ?? p.merged_content,
      }));
      await applyLoreMerge(repoName, proposal.id, merges);
      toastSuccess('Merge applied successfully');
      // Reload proposal to reflect applied status
      const updated = await getLoreProposals(repoName);
      const refreshed = (updated.proposals || []).find((p) => p.id === proposal.id);
      if (refreshed) onProposalUpdate(refreshed);
    } catch (err) {
      alert('Apply Failed', getErrorMessage(err, 'Failed to apply merge'));
    } finally {
      setApplying(false);
    }
  };

  return (
    <div className={styles.proposalCard} data-testid={`lore-proposal-card-${proposal.id}`}>
      <div className={styles.proposalCardHeader}>
        <span className={styles.proposalCardBadge} data-status={proposal.status}>
          {proposal.status}
        </span>
        <span className={styles.proposalCardSummary}>
          {totalCount} rules · {approvedCount} approved · {pendingCount} pending
        </span>
        <span className={styles.proposalCardDate}>
          {new Date(proposal.created_at).toLocaleDateString()}
        </span>
      </div>

      {proposal.discarded && proposal.discarded.length > 0 && (
        <div className={styles.discardedSection}>
          <span className={styles.discardedLabel}>{proposal.discarded.length} discarded</span>
        </div>
      )}

      <div className={styles.ruleList}>
        {proposal.rules.map((rule) => (
          <RuleRow
            key={rule.id}
            rule={rule}
            repoName={repoName}
            proposalID={proposal.id}
            onUpdate={onProposalUpdate}
          />
        ))}
      </div>

      {/* Merge / Dismiss controls */}
      {proposal.status === 'pending' && !previews && !mergeError && (
        <div className={styles.actions} data-testid="lore-actions">
          <button
            className={styles.dismissButton}
            data-testid="lore-dismiss-button"
            onClick={() => onDismissProposal(proposal)}
          >
            Dismiss All
          </button>
          <button
            className={styles.applyButton}
            data-testid="lore-merge-button"
            onClick={handleMerge}
            disabled={approvedCount === 0}
          >
            {`Merge ${approvedCount} Rules`}
          </button>
        </div>
      )}

      {/* Merging in progress */}
      {isMerging && (
        <div className={styles.mergingStatus} data-testid="lore-merging-status">
          <div className="spinner spinner--small" />
          <span>Generating merge preview — this may take a minute…</span>
        </div>
      )}

      {/* Merge error */}
      {mergeError && (
        <div className={styles.mergeError} data-testid="lore-merge-error">
          <span>Merge failed: {mergeError}</span>
          <button className={styles.dismissButton} onClick={handleMerge}>
            Retry
          </button>
        </div>
      )}

      {/* Merge previews */}
      {previews && previews.length > 0 && (
        <div className={styles.mergePreviewSection}>
          <h4 className={styles.mergePreviewTitle}>Merge Preview</h4>
          {previews.map((preview) => (
            <div key={preview.layer} className={styles.mergePreviewCard}>
              <div className={styles.mergePreviewHeader}>
                <span className={styles.ruleLayer}>{LAYER_LABELS[preview.layer]}</span>
                <span className={styles.mergePreviewSummary}>{preview.summary}</span>
              </div>
              <div className={styles.diffWrapper}>
                <ReactDiffViewer
                  oldValue={preview.current_content}
                  newValue={editedPreviews[preview.layer] ?? preview.merged_content}
                  splitView={false}
                  useDarkTheme={theme === 'dark'}
                  hideLineNumbers={false}
                  showDiffOnly={true}
                  compareMethod={DiffMethod.TRIMMED_LINES}
                  disableWordDiff={true}
                  extraLinesSurroundingDiff={3}
                />
              </div>
            </div>
          ))}
          <div className={styles.actions}>
            <button
              className={styles.dismissButton}
              onClick={handleDismissPreviews}
              disabled={applying}
            >
              Cancel
            </button>
            <button className={styles.applyButton} onClick={handleApplyMerge} disabled={applying}>
              {applying && <span className="spinner spinner--small" />}
              {applying ? 'Applying…' : 'Apply Merge'}
            </button>
          </div>
        </div>
      )}
    </div>
  );
}

/** Legacy ProposalCard for v1 proposals (no rules array). */
function LegacyProposalCard({
  proposal,
  onDismiss,
}: {
  proposal: LoreProposal;
  onDismiss: (p: LoreProposal) => void;
}) {
  const [activeFile, setActiveFile] = useState(Object.keys(proposal.proposed_files || {})[0] || '');
  const [showEntries, setShowEntries] = useState(false);
  const { theme } = useTheme();
  const files = Object.keys(proposal.proposed_files || {});

  const entriesUsedCount = proposal.entries_used?.length || 0;
  const entriesDiscardedCount = proposal.entries_discarded
    ? Object.keys(proposal.entries_discarded).length
    : 0;

  return (
    <div className={styles.proposalCard} data-testid={`lore-proposal-card-${proposal.id}`}>
      <div className={styles.proposalCardHeader}>
        <span className={styles.proposalCardBadge} data-status={proposal.status}>
          {proposal.status}
        </span>
        <span className={styles.proposalCardSummary}>{proposal.diff_summary}</span>
        <span className={styles.proposalCardDate}>
          {new Date(proposal.created_at).toLocaleDateString()}
        </span>
      </div>

      {/* File tabs (only if 2+ files) */}
      {files.length > 1 && (
        <div className={styles.fileTabs}>
          {files.map((file) => (
            <button
              key={file}
              className={`${styles.fileTab} ${activeFile === file ? styles.activeTab : ''}`}
              onClick={() => setActiveFile(file)}
            >
              {file}
            </button>
          ))}
        </div>
      )}

      {/* File name (when single file) */}
      {files.length === 1 && <div className={styles.fileName}>{files[0]}</div>}

      {/* Inline diff */}
      <div className={styles.diffWrapper}>
        <ReactDiffViewer
          oldValue={proposal.current_files?.[activeFile] || ''}
          newValue={proposal.proposed_files?.[activeFile] || ''}
          splitView={false}
          useDarkTheme={theme === 'dark'}
          hideLineNumbers={false}
          showDiffOnly={true}
          compareMethod={DiffMethod.TRIMMED_LINES}
          disableWordDiff={true}
          extraLinesSurroundingDiff={3}
        />
      </div>

      {/* Entries toggle */}
      {(entriesUsedCount > 0 || entriesDiscardedCount > 0) && (
        <div className={styles.entriesToggle}>
          <button className={styles.toggleButton} onClick={() => setShowEntries(!showEntries)}>
            {showEntries ? '\u25BC' : '\u25B6'} {entriesUsedCount} entries used
            {entriesDiscardedCount > 0 && ` · ${entriesDiscardedCount} discarded`}
          </button>
          {showEntries && (
            <div className={styles.entriesDetail}>
              {(proposal.entries_used?.length ?? 0) > 0 && (
                <div>
                  <h5>Used</h5>
                  <ul>
                    {proposal.entries_used?.map((e, i) => (
                      <li key={i}>{e}</li>
                    ))}
                  </ul>
                </div>
              )}
              {proposal.entries_discarded && Object.keys(proposal.entries_discarded).length > 0 && (
                <div>
                  <h5>Discarded</h5>
                  <ul>
                    {Object.entries(proposal.entries_discarded).map(([text, reason], i) => (
                      <li key={i}>
                        {text} — <span className={styles.discardReason}>{reason}</span>
                      </li>
                    ))}
                  </ul>
                </div>
              )}
            </div>
          )}
        </div>
      )}

      {/* Actions */}
      <div className={styles.actions} data-testid="lore-actions">
        {proposal.status === 'pending' && (
          <button
            className={styles.dismissButton}
            data-testid="lore-dismiss-button"
            onClick={() => onDismiss(proposal)}
          >
            Dismiss
          </button>
        )}
      </div>
    </div>
  );
}

/** Returns true if a proposal uses the v2 per-rule model. */
function isV2Proposal(p: LoreProposal): boolean {
  return Array.isArray(p.rules) && p.rules.length > 0;
}

export default function LorePage() {
  const { config } = useConfig();
  const repos = config?.repos || [];
  const { success: toastSuccess, error: toastError } = useToast();
  const { alert } = useModal();
  const { activeCurations, pendingCurations, startCuration, onComplete, invalidateProposals } =
    useCuration();
  const [searchParams] = useSearchParams();

  const [activeRepo, setActiveRepo] = useState(() => {
    const paramRepo = searchParams.get('repo');
    if (paramRepo && repos.find((r) => r.name === paramRepo)) return paramRepo;
    return repos[0]?.name || '';
  });
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState('');
  const [proposals, setProposals] = useState<LoreProposal[]>([]);
  const [entries, setEntries] = useState<LoreEntry[]>([]);
  const [allAgents, setAllAgents] = useState<string[]>([]);
  const [allTypes, setAllTypes] = useState<string[]>([]);

  const curationState = activeRepo ? activeCurations[activeRepo] : undefined;
  const curating = !!curationState || pendingCurations.has(activeRepo);

  // Lore system status
  const [loreStatus, setLoreStatus] = useState<LoreStatusResponse | null>(null);

  // Collapsible sections
  const [showHistory, setShowHistory] = useState(false);
  const [showSignals, setShowSignals] = useState(
    () => localStorage.getItem('lore-signals-open') === 'true'
  );
  const [showPastRuns, setShowPastRuns] = useState(false);

  // Past curation runs
  const [pastRuns, setPastRuns] = useState<CurationRunInfo[]>([]);
  const [pastRunEvents, setPastRunEvents] = useState<CuratorStreamEvent[] | null>(null);
  const [pastRunActiveId, setPastRunActiveId] = useState<string | null>(null);
  const [pastRunLoading, setPastRunLoading] = useState(false);

  // Entry filter state
  const [entryFilters, setEntryFilters] = useState<{
    state?: string;
    agent?: string;
    type?: string;
  }>({});

  // Per-repo pending counts for tab badges
  const [repoPendingCounts, setRepoPendingCounts] = useState<Record<string, number>>({});

  // Sub-tabs: instructions vs actions
  const [activeSubTab, setActiveSubTab] = useState<'instructions' | 'actions'>(() => {
    const paramTab = searchParams.get('tab');
    return paramTab === 'actions' ? 'actions' : 'instructions';
  });

  // Actions tab state
  const [proposedActions, setProposedActions] = useState<Action[]>([]);
  const [pinnedActions, setPinnedActions] = useState<Action[]>([]);

  // Sync activeRepo when repos list changes (e.g., config loaded after mount)
  useEffect(() => {
    if (repos.length > 0 && !repos.find((r) => r.name === activeRepo)) {
      const paramRepo = searchParams.get('repo');
      const match = paramRepo && repos.find((r) => r.name === paramRepo);
      setActiveRepo(match ? paramRepo : repos[0].name);
    }
  }, [repos, activeRepo, searchParams]);

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

  // Load all unique agents/types from unfiltered entries
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

  const loadPastRuns = useCallback(async () => {
    if (!activeRepo) return;
    try {
      const data = await getLoreCurations(activeRepo);
      setPastRuns(data.runs || []);
    } catch {
      // Non-critical; silently ignore errors
    }
  }, [activeRepo]);

  const loadActions = useCallback(async () => {
    if (!activeRepo) return;
    try {
      const [proposed, all] = await Promise.all([
        getProposedActions(activeRepo),
        getActions(activeRepo),
      ]);
      setProposedActions(proposed || []);
      setPinnedActions((all || []).filter((a: Action) => a.state === 'pinned'));
    } catch {
      // Non-critical
    }
  }, [activeRepo]);

  const loadData = useCallback(async () => {
    setLoading(true);
    setError('');
    const statusPromise = getLoreStatus()
      .then(setLoreStatus)
      .catch(() => {});
    await Promise.all([
      loadProposals(),
      loadEntries(),
      loadFilterOptions(),
      loadPastRuns(),
      loadActions(),
      statusPromise,
    ]);

    // Fetch pending counts for all repos (for tab badges)
    if (repos.length > 1) {
      const results = await Promise.allSettled(repos.map((r) => getLoreProposals(r.name)));
      const counts: Record<string, number> = {};
      results.forEach((result, i) => {
        if (result.status === 'fulfilled') {
          counts[repos[i].name] = (result.value.proposals || []).filter(
            (p: LoreProposal) => p.status === 'pending'
          ).length;
        }
      });
      setRepoPendingCounts(counts);
    }

    setLoading(false);
  }, [loadProposals, loadEntries, loadFilterOptions, loadPastRuns, loadActions, repos]);

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

  // Handle curation completion (refresh data)
  useEffect(() => {
    return onComplete((repoName) => {
      if (repoName !== activeRepo) return;
      loadData();
    });
  }, [activeRepo, onComplete, loadData]);

  const handleTabChange = (repoName: string) => {
    setActiveRepo(repoName);
    setEntryFilters({});
    filtersInitialized.current = false;
    setPastRunActiveId(null);
    setPastRunEvents(null);
  };

  const handleDismiss = async (proposal: LoreProposal) => {
    if (!activeRepo) return;
    try {
      await dismissLoreProposal(activeRepo, proposal.id);
      toastSuccess('Proposal dismissed');
      invalidateProposals();
      loadData();
    } catch (err) {
      alert('Dismiss Failed', getErrorMessage(err, 'Failed to dismiss proposal'));
    }
  };

  const handleProposalUpdate = (updated: LoreProposal) => {
    setProposals((prev) => prev.map((p) => (p.id === updated.id ? updated : p)));
    invalidateProposals();
  };

  const handleReCurate = () => {
    if (!activeRepo) return;
    startCuration(activeRepo);
  };

  const handlePinAction = async (action: Action) => {
    if (!activeRepo) return;
    try {
      await pinAction(activeRepo, action.id);
      toastSuccess(`Pinned "${action.name}"`);
      loadActions();
    } catch (err) {
      toastError(getErrorMessage(err, 'Failed to pin action'));
    }
  };

  const handleDismissAction = async (action: Action) => {
    if (!activeRepo) return;
    try {
      await dismissAction(activeRepo, action.id);
      toastSuccess(`Dismissed "${action.name}"`);
      loadActions();
    } catch (err) {
      toastError(getErrorMessage(err, 'Failed to dismiss action'));
    }
  };

  const handleClearSignals = async () => {
    if (!activeRepo) return;
    try {
      const result = await clearLoreEntries(activeRepo);
      toastSuccess(`Deleted ${result.cleared} signal file(s)`);
      loadData();
    } catch (err) {
      alert('Clear Signals Failed', getErrorMessage(err, 'Failed to clear signals'));
    }
  };

  const handleViewPastRun = async (runId: string) => {
    if (!activeRepo) return;
    if (pastRunActiveId === runId) {
      setPastRunActiveId(null);
      setPastRunEvents(null);
      return;
    }
    setPastRunActiveId(runId);
    setPastRunLoading(true);
    try {
      const data = await getLoreCurationLog(activeRepo, runId);
      const events: CuratorStreamEvent[] = (data.events || []).map((raw) => ({
        repo: activeRepo,
        timestamp: (raw.timestamp as string) || '',
        event_type: (raw.type as string) || 'unknown',
        subtype: (raw.subtype as string) || '',
        raw: raw as Record<string, unknown>,
      }));
      setPastRunEvents(events);
    } catch (err) {
      alert('Load Failed', getErrorMessage(err, 'Failed to load curation log'));
      setPastRunActiveId(null);
    } finally {
      setPastRunLoading(false);
    }
  };

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

  if (loreStatus && !loreStatus.enabled) {
    return (
      <div className="empty-state">
        <div className="empty-state__icon">!</div>
        <h3 className="empty-state__title">Lore Disabled</h3>
        <p className="empty-state__description">
          The lore system is disabled. <a href="/config?tab=advanced">Enable it in config</a> to
          start capturing agent learnings.
        </p>
      </div>
    );
  }

  const pendingProposals = proposals.filter(
    (p) => p.status === 'pending' || p.status === 'merging'
  );
  const historyProposals = proposals.filter(
    (p) => p.status === 'applied' || p.status === 'dismissed'
  );
  const rawEntries = entries.filter(
    (e) => !e.state_change && !(e.type === 'reflection' && (!e.text || e.text === 'none'))
  );

  return (
    <div className={styles.container} data-testid="lore-page">
      <div className={styles.header}>
        <h2>Lore</h2>
      </div>

      {/* Warning banner */}
      {loreStatus && loreStatus.issues && loreStatus.issues.length > 0 && (
        <div className={styles.warningBanner} data-testid="lore-warning-banner">
          {loreStatus.issues.map((issue, i) => (
            <p key={i}>{issue}</p>
          ))}
          <a href="/config?tab=advanced">Open config</a>
        </div>
      )}

      {/* Repo tabs — use session-tab classes */}
      {repos.length > 1 && (
        <div className="session-tabs">
          {repos.map((repo) => (
            <button
              key={repo.name}
              className={`session-tab ${activeRepo === repo.name ? 'session-tab--active' : ''}`}
              data-testid="repo-tab"
              aria-selected={activeRepo === repo.name}
              onClick={() => handleTabChange(repo.name)}
            >
              <div className="session-tab__row1">
                <span className="session-tab__name">{repo.name}</span>
                {repoPendingCounts[repo.name] > 0 && <span className={styles.repoBadge} />}
              </div>
            </button>
          ))}
        </div>
      )}

      <div className={repos.length > 1 ? styles.tabPanel : undefined}>
        {/* Sub-tab switcher */}
        <div className={styles.subTabs}>
          <button
            className={`${styles.subTab} ${activeSubTab === 'instructions' ? styles.subTabActive : ''}`}
            onClick={() => setActiveSubTab('instructions')}
          >
            Instructions
          </button>
          <button
            className={`${styles.subTab} ${activeSubTab === 'actions' ? styles.subTabActive : ''}`}
            onClick={() => setActiveSubTab('actions')}
          >
            Actions
            {proposedActions.length > 0 && (
              <span style={{ marginLeft: '0.5rem', fontSize: '0.75rem', opacity: 0.7 }}>
                ({proposedActions.length})
              </span>
            )}
          </button>
        </div>

        {activeSubTab === 'instructions' && (
          <>
            {/* Pending proposals */}
            {pendingProposals.length > 0 ? (
              <div className={styles.proposalList}>
                {pendingProposals.map((p) =>
                  isV2Proposal(p) ? (
                    <RuleReviewCard
                      key={p.id}
                      proposal={p}
                      repoName={activeRepo}
                      onProposalUpdate={handleProposalUpdate}
                      onDismissProposal={handleDismiss}
                    />
                  ) : (
                    <LegacyProposalCard key={p.id} proposal={p} onDismiss={handleDismiss} />
                  )
                )}
              </div>
            ) : (
              <div className="empty-state">
                <p className="empty-state__description">
                  No pending proposals for agents instructions changes.
                </p>
              </div>
            )}

            {/* History — collapsed by default */}
            {historyProposals.length > 0 && (
              <section className={styles.section}>
                <button
                  className={styles.toggleButton}
                  onClick={() => setShowHistory(!showHistory)}
                >
                  {showHistory ? '\u25BC' : '\u25B6'} History ({historyProposals.length})
                </button>
                {showHistory && (
                  <div className={styles.historyList}>
                    {historyProposals.map((p) => (
                      <div key={p.id} className={styles.historyItem}>
                        <span className={styles.historyIcon}>
                          {p.status === 'applied' ? '\u2713' : '\u2717'}
                        </span>
                        <span className={styles.historyStatus}>{p.status}</span>
                        <span className={styles.historyDate}>
                          {new Date(p.created_at).toLocaleDateString()}
                        </span>
                        <span className={styles.historySummary}>{p.diff_summary}</span>
                      </div>
                    ))}
                  </div>
                )}
              </section>
            )}

            {/* Past Runs — collapsed by default */}
            {pastRuns.length > 0 && (
              <section className={styles.section}>
                <button
                  className={styles.toggleButton}
                  onClick={() => setShowPastRuns(!showPastRuns)}
                >
                  {showPastRuns ? '\u25BC' : '\u25B6'} Past Runs ({pastRuns.length})
                </button>
                {showPastRuns && (
                  <div className={styles.historyList}>
                    {pastRuns.map((run) => (
                      <div key={run.id}>
                        <button
                          className={`${styles.historyItem} ${pastRunActiveId === run.id ? styles.activeTab : ''}`}
                          style={{
                            cursor: 'pointer',
                            width: '100%',
                            textAlign: 'left',
                            background: 'none',
                            border: 'none',
                            padding: 0,
                          }}
                          onClick={() => handleViewPastRun(run.id)}
                        >
                          <span className={styles.historyDate}>
                            {new Date(run.created_at).toLocaleString()}
                          </span>
                          <span className={styles.historySummary}>{run.id}</span>
                          <span className={styles.entryTool}>
                            {run.size_bytes > 1024
                              ? `${Math.round(run.size_bytes / 1024)}KB`
                              : `${run.size_bytes}B`}
                          </span>
                        </button>
                        {pastRunActiveId === run.id && (
                          <div style={{ marginTop: '0.5rem', marginBottom: '0.5rem' }}>
                            {pastRunLoading ? (
                              <p className={styles.empty}>Loading log...</p>
                            ) : pastRunEvents ? (
                              <CuratorTerminal events={pastRunEvents} />
                            ) : null}
                          </div>
                        )}
                      </div>
                    ))}
                  </div>
                )}
              </section>
            )}

            {/* Raw Signals — collapsed, persisted to localStorage */}
            <section className={styles.section}>
              <button
                className={styles.toggleButton}
                onClick={() => {
                  const next = !showSignals;
                  setShowSignals(next);
                  localStorage.setItem('lore-signals-open', String(next));
                }}
              >
                {showSignals ? '\u25BC' : '\u25B6'} Raw Signals ({rawEntries.length})
              </button>
              {showSignals && (
                <>
                  <div className={styles.filterBar} data-testid="lore-filter-bar">
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
                    <button
                      className={styles.deleteButton}
                      onClick={handleClearSignals}
                      disabled={curating || rawEntries.length === 0}
                    >
                      Delete Signals
                    </button>
                    <div className={styles.curateArea}>
                      <button
                        className={styles.curateButton}
                        onClick={handleReCurate}
                        disabled={curating}
                      >
                        {curating ? 'Curating...' : 'Trigger Curation'}
                      </button>
                      {curationState && (
                        <span className={styles.curateStatus}>
                          {curationState.message}
                          <span className={styles.curateElapsed}>{curationState.elapsed}s</span>
                        </span>
                      )}
                    </div>
                  </div>
                  <div className={styles.entriesList} data-testid="lore-entries">
                    {rawEntries.length === 0 ? (
                      <p className={styles.empty}>No raw signal entries yet.</p>
                    ) : (
                      rawEntries.map((e, i) => (
                        <div key={i} className={styles.entryCard} data-entry-type={e.type}>
                          <div className={styles.entryMeta}>
                            <span className={styles.entryType}>{e.type}</span>
                            <span className={styles.entryAgent}>{e.agent}</span>
                            {e.tool && <span className={styles.entryTool}>{e.tool}</span>}
                            {e.category && (
                              <span className={styles.entryCategory}>{e.category}</span>
                            )}
                            <span className={styles.entryTs}>
                              {new Date(e.ts).toLocaleString()}
                            </span>
                          </div>
                          <div className={styles.entryText}>
                            {e.type === 'failure'
                              ? `${e.input_summary} → "${e.error_summary}"`
                              : e.text}
                          </div>
                        </div>
                      ))
                    )}
                  </div>
                </>
              )}
            </section>
          </>
        )}

        {activeSubTab === 'actions' && (
          <>
            {/* Proposed actions */}
            {proposedActions.length > 0 && (
              <>
                <div className={styles.actionsSubheading}>Proposed ({proposedActions.length})</div>
                <div className={styles.actionsList}>
                  {proposedActions.map((a) => (
                    <ProposedActionCard
                      key={a.id}
                      action={a}
                      onPin={handlePinAction}
                      onDismiss={handleDismissAction}
                    />
                  ))}
                </div>
              </>
            )}

            {proposedActions.length === 0 && (
              <div className="empty-state">
                <p className="empty-state__description">
                  No proposed actions. Actions are generated automatically from agent work sessions.
                </p>
              </div>
            )}

            {/* Pinned actions */}
            {pinnedActions.length > 0 && (
              <>
                <div className={styles.actionsSubheading}>Pinned ({pinnedActions.length})</div>
                <div>
                  {pinnedActions.map((a) => (
                    <PinnedActionRow key={a.id} action={a} />
                  ))}
                </div>
              </>
            )}
          </>
        )}
      </div>
    </div>
  );
}
