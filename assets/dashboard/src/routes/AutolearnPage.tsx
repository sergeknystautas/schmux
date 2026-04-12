import { useState, useEffect, useCallback, useRef } from 'react';
import ReactDiffViewer, { DiffMethod } from 'react-diff-viewer-continued';
import {
  getLoreProposals,
  getLoreStatus,
  getLoreEntries,
  clearLoreEntries,
  updateLoreRule,
  applyLoreMerge,
  getLorePendingMerge,
  startLoreUnifiedMerge,
  pushLoreMerge,
  updateLorePendingMerge,
  deleteLorePendingMerge,
  getErrorMessage,
} from '../lib/api';
import { getAllSpawnEntries, pinSpawnEntry, dismissSpawnEntry } from '../lib/spawn-api';
import type { SpawnEntry } from '../lib/types.generated';
import { useConfig } from '../contexts/ConfigContext';
import { useCuration } from '../contexts/CurationContext';
import { useSessions } from '../contexts/SessionsContext';
import { useToast } from '../components/ToastProvider';
import { useModal } from '../components/ModalProvider';
import { LoreCard } from '../components/LoreCard';
import useTheme from '../hooks/useTheme';
import type {
  LoreEntry,
  LoreRule,
  LoreLayer,
  LoreStatusResponse,
  PendingMerge,
} from '../lib/types';
import styles from '../styles/autolearn.module.css';

type DuplicateRef = { proposalId: string; ruleId: string; repoName: string };

type CardItem =
  | {
      kind: 'instruction';
      rule: LoreRule;
      repoName: string;
      proposalId: string;
      createdAt: string;
      duplicates: DuplicateRef[];
    }
  | { kind: 'action'; action: SpawnEntry; repoName: string; createdAt: string };

function normalizeRuleText(text: string): string {
  return text.trim().toLowerCase().replace(/\s+/g, ' ');
}

const LAYER_LABELS: Record<string, string> = {
  repo_public: 'Public',
  repo_private: 'Private',
  cross_repo_private: 'Cross-Repo Private',
};

export default function AutolearnPage() {
  const { config } = useConfig();
  const repos = config?.repos || [];
  const { success: toastSuccess, error: toastError } = useToast();
  const { alert } = useModal();
  const { startCuration, activeCurations, pendingCurations, onComplete, invalidateProposals } =
    useCuration();
  const { curatorEvents } = useSessions();
  const { theme } = useTheme();
  const isDebugMode = !!config?.debug_ui;

  const [loading, setLoading] = useState(true);
  const [error, setError] = useState('');
  const [cards, setCards] = useState<CardItem[]>([]);
  const [loreStatus, setLoreStatus] = useState<LoreStatusResponse | null>(null);

  // Server-driven pending merges keyed by repo name
  const [pendingMerges, setPendingMerges] = useState<Record<string, PendingMerge>>({});

  const [applying, setApplying] = useState(false);

  // Diff/Edit toggle for merge review
  const [activeTab, setActiveTab] = useState<'diff' | 'edit'>('diff');

  // Debounced edit save
  const editTimerRef = useRef<ReturnType<typeof setTimeout>>();

  // Dev mode debug
  const [showDebug, setShowDebug] = useState(false);
  const [debugEntries, setDebugEntries] = useState<(LoreEntry & { _repo: string })[]>([]);

  const loadData = useCallback(async () => {
    setLoading(true);
    setError('');

    const statusPromise = getLoreStatus()
      .then(setLoreStatus)
      .catch(() => {});

    const allCards: CardItem[] = [];

    await Promise.allSettled(
      repos.map(async (repo) => {
        const [proposalRes, actionRes] = await Promise.all([
          getLoreProposals(repo.name),
          getAllSpawnEntries(repo.name),
        ]);

        for (const proposal of proposalRes.batches || []) {
          // Proposals currently being merged are handled
          // via the pending merge system now — skip them in the card wall
          if (proposal.status === 'merging') continue;

          // Pending proposals — show their pending rules as cards
          if (proposal.status !== 'pending') continue;
          for (const rule of proposal.learnings || []) {
            if (rule.status !== 'pending') continue;
            const normalizedText = normalizeRuleText(rule.title);
            const existingIdx = allCards.findIndex(
              (c) => c.kind === 'instruction' && normalizeRuleText(c.rule.title) === normalizedText
            );
            if (existingIdx !== -1) {
              // Track duplicate on the primary card instead of showing it
              const existing = allCards[existingIdx] as CardItem & { kind: 'instruction' };
              existing.duplicates.push({
                proposalId: proposal.id,
                ruleId: rule.id,
                repoName: repo.name,
              });
              continue;
            }
            allCards.push({
              kind: 'instruction',
              rule,
              repoName: repo.name,
              proposalId: proposal.id,
              createdAt: proposal.created_at,
              duplicates: [],
            });
          }
        }

        for (const entry of actionRes || []) {
          if (entry.state !== 'proposed') continue;
          allCards.push({
            kind: 'action',
            action: entry,
            repoName: repo.name,
            createdAt: entry.metadata?.emerged_at || '',
          });
        }
      })
    );

    // Fetch pending merges per repo
    const mergeResults: Record<string, PendingMerge> = {};
    await Promise.allSettled(
      repos.map(async (repo) => {
        const pm = await getLorePendingMerge(repo.name);
        if (pm) mergeResults[repo.name] = pm;
      })
    );

    allCards.sort((a, b) => b.createdAt.localeCompare(a.createdAt));

    await statusPromise;
    setCards(allCards);
    setPendingMerges(mergeResults);
    setLoading(false);
  }, [repos]);

  // Initial load
  useEffect(() => {
    loadData();
  }, [loadData]);

  // Reload on curation completion
  useEffect(() => {
    return onComplete(() => {
      loadData();
    });
  }, [onComplete, loadData]);

  // Listen for autolearn_merge_complete WebSocket events and re-fetch pending merges
  const mergeCompleteSeenRef = useRef<Set<string>>(new Set());
  useEffect(() => {
    for (const [repo, events] of Object.entries(curatorEvents)) {
      if (events.length === 0) continue;
      const last = events[events.length - 1];
      if (last.event_type === 'autolearn_merge_complete') {
        // Deduplicate by timestamp to avoid re-fetching for the same event
        const key = `${repo}:${last.timestamp}`;
        if (!mergeCompleteSeenRef.current.has(key)) {
          mergeCompleteSeenRef.current.add(key);
          loadData();
        }
      }
    }
  }, [curatorEvents, loadData]);

  // --- Card callbacks ---

  const handleApprove = async (card: CardItem) => {
    if (card.kind === 'instruction') {
      try {
        const updated = await updateLoreRule(card.repoName, card.proposalId, card.rule.id, {
          status: 'approved',
        });
        // Also approve any tracked duplicates in other proposals
        for (const dup of card.duplicates) {
          await updateLoreRule(dup.repoName, dup.proposalId, dup.ruleId, {
            status: 'approved',
          }).catch(() => {});
        }
        setCards((prev) =>
          prev.map((c) => {
            if (
              c.kind === 'instruction' &&
              c.proposalId === card.proposalId &&
              c.rule.id === card.rule.id
            ) {
              const updatedRule = updated.learnings.find((r: LoreRule) => r.id === card.rule.id);
              return updatedRule ? { ...c, rule: updatedRule } : c;
            }
            return c;
          })
        );
        invalidateProposals();
      } catch (err) {
        alert('Update Failed', getErrorMessage(err, 'Failed to approve rule'));
      }
    } else {
      try {
        await pinSpawnEntry(card.repoName, card.action.id);
        setCards((prev) =>
          prev.filter((c) => !(c.kind === 'action' && c.action.id === card.action.id))
        );
        toastSuccess(`Pinned "${card.action.name}"`);
        invalidateProposals();
      } catch (err) {
        toastError(getErrorMessage(err, 'Failed to pin action'));
      }
    }
  };

  const handleUnapprove = async (card: CardItem) => {
    if (card.kind !== 'instruction') return;
    try {
      const updated = await updateLoreRule(card.repoName, card.proposalId, card.rule.id, {
        status: 'pending',
      });
      // Also unapprove duplicates
      for (const dup of card.duplicates) {
        await updateLoreRule(dup.repoName, dup.proposalId, dup.ruleId, { status: 'pending' }).catch(
          () => {}
        );
      }
      setCards((prev) =>
        prev.map((c) => {
          if (
            c.kind === 'instruction' &&
            c.proposalId === card.proposalId &&
            c.rule.id === card.rule.id
          ) {
            const updatedRule = updated.learnings.find((r: LoreRule) => r.id === card.rule.id);
            return updatedRule ? { ...c, rule: updatedRule } : c;
          }
          return c;
        })
      );
      invalidateProposals();
    } catch (err) {
      alert('Update Failed', getErrorMessage(err, 'Failed to undo approval'));
    }
  };

  const handleDismiss = async (card: CardItem) => {
    if (card.kind === 'instruction') {
      try {
        await updateLoreRule(card.repoName, card.proposalId, card.rule.id, { status: 'dismissed' });
        // Also dismiss duplicates
        for (const dup of card.duplicates) {
          await updateLoreRule(dup.repoName, dup.proposalId, dup.ruleId, {
            status: 'dismissed',
          }).catch(() => {});
        }
        setCards((prev) =>
          prev.filter(
            (c) =>
              !(
                c.kind === 'instruction' &&
                c.proposalId === card.proposalId &&
                c.rule.id === card.rule.id
              )
          )
        );
        invalidateProposals();
      } catch (err) {
        alert('Dismiss Failed', getErrorMessage(err, 'Failed to dismiss rule'));
      }
    } else {
      try {
        await dismissSpawnEntry(card.repoName, card.action.id);
        setCards((prev) =>
          prev.filter((c) => !(c.kind === 'action' && c.action.id === card.action.id))
        );
        toastSuccess(`Dismissed "${card.action.name}"`);
        invalidateProposals();
      } catch (err) {
        toastError(getErrorMessage(err, 'Failed to dismiss action'));
      }
    }
  };

  const handleEdit = async (card: CardItem, newText: string) => {
    if (card.kind !== 'instruction') return;
    try {
      const updated = await updateLoreRule(card.repoName, card.proposalId, card.rule.id, {
        title: newText,
      });
      setCards((prev) =>
        prev.map((c) => {
          if (
            c.kind === 'instruction' &&
            c.proposalId === card.proposalId &&
            c.rule.id === card.rule.id
          ) {
            const updatedRule = updated.learnings.find((r: LoreRule) => r.id === card.rule.id);
            return updatedRule ? { ...c, rule: updatedRule } : c;
          }
          return c;
        })
      );
    } catch (err) {
      alert('Update Failed', getErrorMessage(err, 'Failed to update rule text'));
    }
  };

  const handleLayerChange = async (card: CardItem, layer: LoreLayer) => {
    if (card.kind !== 'instruction') return;
    try {
      const updated = await updateLoreRule(card.repoName, card.proposalId, card.rule.id, {
        chosen_layer: layer,
      });
      setCards((prev) =>
        prev.map((c) => {
          if (
            c.kind === 'instruction' &&
            c.proposalId === card.proposalId &&
            c.rule.id === card.rule.id
          ) {
            const updatedRule = updated.learnings.find((r: LoreRule) => r.id === card.rule.id);
            return updatedRule ? { ...c, rule: updatedRule } : c;
          }
          return c;
        })
      );
    } catch (err) {
      alert('Update Failed', getErrorMessage(err, 'Failed to update layer'));
    }
  };

  const handleApproveAll = async () => {
    const pending = cards.filter((c) =>
      c.kind === 'instruction' ? c.rule.status === 'pending' : true
    );
    for (const card of pending) {
      await handleApprove(card);
    }
  };

  // --- Apply flow ---

  const effectiveLayer = (rule: LoreRule): LoreLayer => {
    return rule.chosen_layer || rule.suggested_layer;
  };

  const handleApply = async () => {
    const approvedCards = cards.filter(
      (c) => c.kind === 'instruction' && c.rule.status === 'approved'
    ) as (CardItem & { kind: 'instruction' })[];

    if (approvedCards.length === 0) {
      toastError('No approved rules to apply');
      return;
    }

    // Group by proposal
    const groups = new Map<
      string,
      { repoName: string; proposalId: string; learnings: LoreRule[] }
    >();
    for (const card of approvedCards) {
      const key = `${card.repoName}::${card.proposalId}`;
      if (!groups.has(key)) {
        groups.set(key, { repoName: card.repoName, proposalId: card.proposalId, learnings: [] });
      }
      groups.get(key)!.learnings.push(card.rule);
    }

    // Separate private-layer rules (applied via applyLoreMerge per-proposal)
    // from public-layer rules (applied via unified merge per-repo)
    const privateGroups: typeof groups = new Map();
    // Group public rules by repo: Map<repoName, { batch_id, learning_ids }[]>
    const publicByRepo = new Map<string, { batch_id: string; learning_ids: string[] }[]>();

    for (const [key, group] of groups) {
      const privateRules = group.learnings.filter((r) => effectiveLayer(r) !== 'repo_public');
      const publicRules = group.learnings.filter((r) => effectiveLayer(r) === 'repo_public');
      if (privateRules.length > 0) {
        privateGroups.set(key, { ...group, learnings: privateRules });
      }
      if (publicRules.length > 0) {
        if (!publicByRepo.has(group.repoName)) {
          publicByRepo.set(group.repoName, []);
        }
        publicByRepo.get(group.repoName)!.push({
          batch_id: group.proposalId,
          learning_ids: publicRules.map((r) => r.id),
        });
      }
    }

    setApplying(true);
    try {
      // Apply private layers directly via apply-merge (no LLM merge needed)
      if (privateGroups.size > 0) {
        for (const [, group] of privateGroups) {
          const merges = group.learnings.map((r) => ({
            layer: effectiveLayer(r),
            content: r.title,
          }));
          await applyLoreMerge(group.repoName, group.proposalId, merges);
        }
        if (publicByRepo.size === 0) {
          toastSuccess(`${approvedCards.length} rules saved`);
        }
      }

      // Start unified merge for public layers (per-repo)
      if (publicByRepo.size > 0) {
        for (const [repoName, proposals] of publicByRepo) {
          await startLoreUnifiedMerge(repoName, proposals);
        }
        toastSuccess(
          'Merge started. You can leave this page — the diff will be here when you return.'
        );
      }

      invalidateProposals();
      await loadData();
    } catch (err) {
      await alert('Apply Failed', getErrorMessage(err, 'Failed to apply rules'));
    } finally {
      setApplying(false);
    }
  };

  const handleCommitAndPush = async (repoName: string) => {
    setApplying(true);
    try {
      await pushLoreMerge(repoName);
      const mode = config?.lore?.public_rule_mode || 'direct_push';
      toastSuccess(mode === 'create_pr' ? 'PR created' : `Pushed to ${repoName}`);
      setPendingMerges((prev) => {
        const next = { ...prev };
        delete next[repoName];
        return next;
      });
      invalidateProposals();
    } catch (err) {
      const msg = getErrorMessage(err, 'Push failed');
      await alert('Push Failed', msg);
    } finally {
      setApplying(false);
    }
  };

  const handleDismissMergeReview = async (repoName: string) => {
    try {
      await deleteLorePendingMerge(repoName);
      setPendingMerges((prev) => {
        const next = { ...prev };
        delete next[repoName];
        return next;
      });
    } catch {
      // Best-effort dismiss — reload to sync
      await loadData();
    }
  };

  const handleEditChange = (repoName: string, value: string) => {
    setPendingMerges((prev) => ({
      ...prev,
      [repoName]: { ...prev[repoName], edited_content: value },
    }));
    clearTimeout(editTimerRef.current);
    editTimerRef.current = setTimeout(() => {
      updateLorePendingMerge(repoName, value).catch(() => {});
    }, 1000);
  };

  // --- Derived state ---

  const pendingCards = cards.filter((c) =>
    c.kind === 'instruction' ? c.rule.status === 'pending' : true
  );
  const approvedCards = cards.filter(
    (c) => c.kind === 'instruction' && c.rule.status === 'approved'
  );
  const allTriaged = pendingCards.length === 0 && approvedCards.length > 0;

  // Pending merge repos by status
  const readyMerges = Object.entries(pendingMerges).filter(([, pm]) => pm.status === 'ready');
  const mergingMerges = Object.entries(pendingMerges).filter(([, pm]) => pm.status === 'merging');
  const errorMerges = Object.entries(pendingMerges).filter(([, pm]) => pm.status === 'error');
  const hasPendingMerges = Object.keys(pendingMerges).length > 0;

  // --- Render ---

  if (loading) {
    return <div className="page-loading">Loading autolearn...</div>;
  }

  if (error) {
    return (
      <div className="page-error">
        <p>{error}</p>
        <button onClick={loadData}>Retry</button>
      </div>
    );
  }

  return (
    <div className={styles.container} data-testid="autolearn-page">
      <div className={styles.header}>
        <h2>Autolearn</h2>
        <p className={styles.headerSubtitle}>Schmux continual learning system</p>
      </div>

      {/* Warning banner */}
      {loreStatus && loreStatus.issues && loreStatus.issues.length > 0 && (
        <div className={styles.warningBanner} data-testid="autolearn-warning-banner">
          {loreStatus.issues.map((issue, i) => (
            <p key={i}>{issue}</p>
          ))}
          <a href="/config?tab=advanced">Open config</a>
        </div>
      )}

      {/* Autolearn disabled */}
      {loreStatus && !loreStatus.enabled ? (
        <div className="empty-state">
          <div className="empty-state__icon">!</div>
          <h3 className="empty-state__title">Autolearn Disabled</h3>
          <p className="empty-state__description">
            Autolearn is disabled. <a href="/config?tab=advanced">Enable it in config</a> to start
            capturing agent learnings.
          </p>
        </div>
      ) : (
        <>
          {/* Pending merges with errors */}
          {errorMerges.map(([repoName, pm]) => (
            <div
              key={`merge-error-${repoName}`}
              className={styles.mergeError}
              style={{ marginBottom: '1rem' }}
            >
              <span>
                Merge failed for {repoName}: {pm.error || 'Unknown error'}
              </span>
              <button
                className={styles.applyButton}
                onClick={() => {
                  // Retry: dismiss and let user re-apply
                  handleDismissMergeReview(repoName);
                }}
                style={{ marginLeft: 'auto' }}
              >
                Dismiss
              </button>
            </div>
          ))}

          {/* Merging in progress indicator */}
          {mergingMerges.length > 0 && (
            <div className={styles.mergingStatus} style={{ marginBottom: '1rem' }}>
              <span className="spinner spinner--small" />
              Merging rules in the background...
            </div>
          )}

          {/* Merge reviews ready (server-persisted — survives navigation) */}
          {readyMerges.map(([repoName, pm]) => (
            <div
              key={`merge-review-${repoName}`}
              className={styles.proposalCard}
              style={{ marginBottom: '1rem' }}
            >
              <h3>Review Changes</h3>
              <p style={{ fontSize: '0.8rem', color: 'var(--text-secondary)', margin: '0.5rem 0' }}>
                {pm.summary}
              </p>

              {/* Diff/Edit tab toggle */}
              <div style={{ display: 'flex', gap: '0.25rem', marginBottom: '0.75rem' }}>
                <button
                  className={activeTab === 'diff' ? styles.applyButton : styles.dismissButton}
                  style={{ padding: '0.25rem 0.75rem', fontSize: '0.8rem' }}
                  onClick={() => setActiveTab('diff')}
                >
                  Diff
                </button>
                <button
                  className={activeTab === 'edit' ? styles.applyButton : styles.dismissButton}
                  style={{ padding: '0.25rem 0.75rem', fontSize: '0.8rem' }}
                  onClick={() => setActiveTab('edit')}
                >
                  Edit
                </button>
              </div>

              {activeTab === 'diff' ? (
                <>
                  <div className={styles.diffWrapper}>
                    <ReactDiffViewer
                      oldValue={pm.current_content}
                      newValue={pm.edited_content ?? pm.merged_content}
                      splitView={false}
                      useDarkTheme={theme === 'dark'}
                      hideLineNumbers={false}
                      showDiffOnly={true}
                      compareMethod={DiffMethod.TRIMMED_LINES}
                      disableWordDiff={true}
                      extraLinesSurroundingDiff={3}
                    />
                  </div>
                  <div className={styles.actions}>
                    <button
                      className={styles.dismissButton}
                      onClick={() => handleDismissMergeReview(repoName)}
                      disabled={applying}
                    >
                      Back
                    </button>
                    <button
                      className={styles.applyButton}
                      onClick={() => handleCommitAndPush(repoName)}
                      disabled={applying}
                    >
                      {applying && <span className="spinner spinner--small" />}
                      {(config?.lore?.public_rule_mode || 'direct_push') === 'create_pr'
                        ? 'Create PR'
                        : 'Commit & Push'}
                    </button>
                  </div>
                </>
              ) : (
                <>
                  <textarea
                    value={pm.edited_content ?? pm.merged_content}
                    onChange={(e) => handleEditChange(repoName, e.target.value)}
                    style={{
                      width: '100%',
                      minHeight: '300px',
                      fontFamily: 'var(--font-mono, monospace)',
                      fontSize: '0.8rem',
                      lineHeight: '1.5',
                      padding: '0.75rem',
                      background: 'var(--color-surface, #1a1a2e)',
                      color: 'var(--text-primary, #ddd)',
                      border: '1px solid var(--color-border, #333)',
                      borderRadius: '4px',
                      resize: 'vertical',
                      marginBottom: '0.75rem',
                    }}
                  />
                  <div className={styles.actions}>
                    <button
                      className={styles.dismissButton}
                      onClick={() => handleDismissMergeReview(repoName)}
                      disabled={applying}
                    >
                      Back
                    </button>
                    <button className={styles.applyButton} onClick={() => setActiveTab('diff')}>
                      Review Diff
                    </button>
                  </div>
                </>
              )}
            </div>
          ))}

          {/* Summary banner when all cards are triaged */}
          {allTriaged && !hasPendingMerges && (
            <div className={styles.proposalCard} style={{ marginBottom: '1rem' }}>
              <h3>{approvedCards.length} insights approved</h3>
              <div style={{ margin: '1rem 0', fontSize: '0.875rem' }}>
                {(() => {
                  const priv = approvedCards.filter(
                    (c) => c.kind === 'instruction' && effectiveLayer(c.rule) === 'repo_private'
                  );
                  const privAll = approvedCards.filter(
                    (c) =>
                      c.kind === 'instruction' && effectiveLayer(c.rule) === 'cross_repo_private'
                  );
                  const pub = approvedCards.filter(
                    (c) => c.kind === 'instruction' && effectiveLayer(c.rule) === 'repo_public'
                  );
                  return (
                    <>
                      {priv.length > 0 && (
                        <p>{priv.length} private (this repo) -- saved immediately</p>
                      )}
                      {privAll.length > 0 && (
                        <p>{privAll.length} private (all repos) -- saved immediately</p>
                      )}
                      {pub.length > 0 && (
                        <p>{pub.length} public -- will be merged into CLAUDE.md</p>
                      )}
                    </>
                  );
                })()}
              </div>
              <div className={styles.actions}>
                <button className={styles.applyButton} onClick={handleApply} disabled={applying}>
                  {applying && <span className="spinner spinner--small" />}
                  Apply
                </button>
              </div>
            </div>
          )}

          {/* Approve All button */}
          {pendingCards.length >= 2 && (
            <div className={styles.actions} style={{ marginBottom: '1rem' }}>
              <button className={styles.applyButton} onClick={handleApproveAll}>
                Approve All ({pendingCards.length})
              </button>
            </div>
          )}

          {/* Card wall */}
          {cards.length > 0 ? (
            <div className={styles.proposalList}>
              {cards.map((card) => {
                const key =
                  card.kind === 'instruction'
                    ? `rule-${card.proposalId}-${card.rule.id}`
                    : `action-${card.action.id}`;
                return card.kind === 'instruction' ? (
                  <LoreCard
                    key={key}
                    type="instruction"
                    rule={card.rule}
                    repoName={card.repoName}
                    proposalId={card.proposalId}
                    onApprove={() => handleApprove(card)}
                    onDismiss={() => handleDismiss(card)}
                    onEdit={(_, text) => handleEdit(card, text)}
                    onLayerChange={(_, layer) => handleLayerChange(card, layer)}
                    onUnapprove={() => handleUnapprove(card)}
                  />
                ) : (
                  <LoreCard
                    key={key}
                    type="action"
                    action={card.action}
                    repoName={card.repoName}
                    onApprove={() => handleApprove(card)}
                    onDismiss={() => handleDismiss(card)}
                    onEdit={() => {}}
                  />
                );
              })}
            </div>
          ) : !hasPendingMerges ? (
            <div className="empty-state">
              <p className="empty-state__description">
                Nothing to review. New insights will appear here as agents work.
              </p>
            </div>
          ) : null}
        </>
      )}

      {isDebugMode && (
        <section style={{ marginTop: '2rem' }}>
          <button
            className={styles.toggleButton}
            onClick={() => {
              const next = !showDebug;
              setShowDebug(next);
              if (next) {
                Promise.all(
                  repos.map((r) =>
                    getLoreEntries(r.name)
                      .then((res) => (res.entries || []).map((e) => ({ ...e, _repo: r.name })))
                      .catch(() => [] as (LoreEntry & { _repo: string })[])
                  )
                ).then((results) => {
                  const all = results.flat().sort((a, b) => b.ts.localeCompare(a.ts));
                  setDebugEntries(all);
                });
              }
            }}
          >
            {showDebug ? '\u25BC' : '\u25B6'} Debug
          </button>
          {showDebug && (
            <div style={{ marginTop: '0.75rem' }}>
              <div style={{ display: 'flex', gap: '0.5rem', margin: '0.5rem 0', flexWrap: 'wrap' }}>
                {repos.map((r) => (
                  <button
                    key={r.name}
                    className={styles.curateButton}
                    style={{ marginLeft: 0 }}
                    onClick={() => startCuration(r.name)}
                    disabled={!!activeCurations[r.name] || pendingCurations.has(r.name)}
                  >
                    {activeCurations[r.name] || pendingCurations.has(r.name)
                      ? `Curating ${r.name}...`
                      : `Curate ${r.name}`}
                  </button>
                ))}
                <button
                  className={styles.deleteButton}
                  style={{ marginLeft: 'auto' }}
                  onClick={async () => {
                    try {
                      const results = await Promise.all(repos.map((r) => clearLoreEntries(r.name)));
                      const total = results.reduce((sum, r) => sum + r.cleared, 0);
                      toastSuccess(`Deleted ${total} signal file(s)`);
                      setDebugEntries([]);
                    } catch (err) {
                      alert('Clear Failed', getErrorMessage(err, 'Failed to clear signals'));
                    }
                  }}
                  disabled={debugEntries.length === 0}
                >
                  Delete All Signals
                </button>
              </div>
              <div className={styles.entriesList}>
                {debugEntries
                  .filter(
                    (e) =>
                      !e.state_change &&
                      !(e.type === 'reflection' && (!e.text || e.text === 'none'))
                  )
                  .map((e, i) => (
                    <div key={i} className={styles.entryCard} data-entry-type={e.type}>
                      <div className={styles.entryMeta}>
                        <span className={styles.entryType}>{e.type}</span>
                        <span className={styles.entryAgent}>{e.agent}</span>
                        {e.tool && <span className={styles.entryTool}>{e.tool}</span>}
                        <span className={styles.entryTs}>{new Date(e.ts).toLocaleString()}</span>
                        <span className={styles.entryRepo}>{e._repo}</span>
                      </div>
                      <div className={styles.entryText}>
                        {e.type === 'failure'
                          ? `${e.input_summary} \u2192 "${e.error_summary}"`
                          : e.text}
                      </div>
                    </div>
                  ))}
                {debugEntries.length === 0 && (
                  <p className={styles.empty}>No raw signal entries.</p>
                )}
              </div>
            </div>
          )}
        </section>
      )}
    </div>
  );
}
