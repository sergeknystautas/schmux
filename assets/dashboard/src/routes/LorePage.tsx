import { useState, useEffect, useCallback } from 'react';
import ReactDiffViewer, { DiffMethod } from 'react-diff-viewer-continued';
import {
  getLoreProposals,
  getLoreStatus,
  getLoreEntries,
  clearLoreEntries,
  updateLoreRule,
  startLoreMerge,
  applyLoreMerge,
  getErrorMessage,
} from '../lib/api';
import { getAllSpawnEntries, pinSpawnEntry, dismissSpawnEntry } from '../lib/emergence-api';
import type { SpawnEntry } from '../lib/types.generated';
import { useConfig } from '../contexts/ConfigContext';
import { useCuration } from '../contexts/CurationContext';
import { useToast } from '../components/ToastProvider';
import { useModal } from '../components/ModalProvider';
import { LoreCard } from '../components/LoreCard';
import useTheme from '../hooks/useTheme';
import useDevStatus from '../hooks/useDevStatus';
import type {
  LoreEntry,
  LoreProposal,
  LoreRule,
  LoreLayer,
  LoreMergePreview,
  LoreStatusResponse,
} from '../lib/types';
import styles from '../styles/lore.module.css';

type CardItem =
  | { kind: 'instruction'; rule: LoreRule; repoName: string; proposalId: string; createdAt: string }
  | { kind: 'action'; action: SpawnEntry; repoName: string; createdAt: string };

/** A proposal that has merge_previews ready for user review. */
interface MergeReviewItem {
  repoName: string;
  proposal: LoreProposal;
  previews: LoreMergePreview[];
}

const LAYER_LABELS: Record<string, string> = {
  repo_public: 'Public',
  repo_private: 'Private',
  cross_repo_private: 'Cross-Repo Private',
};

export default function LorePage() {
  const { config } = useConfig();
  const repos = config?.repos || [];
  const { success: toastSuccess, error: toastError } = useToast();
  const { alert } = useModal();
  const { startCuration, activeCurations, pendingCurations, onComplete, invalidateProposals } =
    useCuration();
  const { theme } = useTheme();
  const { isDevMode } = useDevStatus();

  const [loading, setLoading] = useState(true);
  const [error, setError] = useState('');
  const [cards, setCards] = useState<CardItem[]>([]);
  const [loreStatus, setLoreStatus] = useState<LoreStatusResponse | null>(null);

  // Server-derived state: proposals with merge previews ready for review
  const [mergeReviews, setMergeReviews] = useState<MergeReviewItem[]>([]);
  // Server-derived state: proposals currently being merged in the background
  const [mergingCount, setMergingCount] = useState(0);

  // Local UI state for the diff review
  const [editedPreviews, setEditedPreviews] = useState<Record<string, string>>({});
  const [applying, setApplying] = useState(false);

  // Dev mode debug
  const [showDebug, setShowDebug] = useState(false);
  const [debugRepo, setDebugRepo] = useState(repos[0]?.name || '');
  const [debugEntries, setDebugEntries] = useState<LoreEntry[]>([]);

  const loadData = useCallback(async () => {
    setLoading(true);
    setError('');

    const statusPromise = getLoreStatus()
      .then(setLoreStatus)
      .catch(() => {});

    const allCards: CardItem[] = [];
    const allMergeReviews: MergeReviewItem[] = [];
    let merging = 0;

    await Promise.allSettled(
      repos.map(async (repo) => {
        const [proposalRes, actionRes] = await Promise.all([
          getLoreProposals(repo.name),
          getAllSpawnEntries(repo.name),
        ]);

        for (const proposal of proposalRes.proposals || []) {
          // Proposals with merge previews ready for review
          if (
            proposal.merge_previews &&
            proposal.merge_previews.length > 0 &&
            proposal.status !== 'applied'
          ) {
            allMergeReviews.push({
              repoName: repo.name,
              proposal,
              previews: proposal.merge_previews,
            });
            continue;
          }

          // Proposals currently being merged
          if (proposal.status === 'merging') {
            merging++;
            continue;
          }

          // Pending proposals — show their pending rules as cards
          if (proposal.status !== 'pending') continue;
          for (const rule of proposal.rules || []) {
            if (rule.status !== 'pending') continue;
            const normalizedText = rule.text.trim().toLowerCase();
            const existingIdx = allCards.findIndex(
              (c) => c.kind === 'instruction' && c.rule.text.trim().toLowerCase() === normalizedText
            );
            if (existingIdx !== -1) continue;
            allCards.push({
              kind: 'instruction',
              rule,
              repoName: repo.name,
              proposalId: proposal.id,
              createdAt: proposal.created_at,
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

    allCards.sort((a, b) => b.createdAt.localeCompare(a.createdAt));

    await statusPromise;
    setCards(allCards);
    setMergeReviews(allMergeReviews);
    setMergingCount(merging);
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

  // Poll while merges are in progress (every 5s)
  useEffect(() => {
    if (mergingCount === 0) return;
    const interval = setInterval(() => loadData(), 5000);
    return () => clearInterval(interval);
  }, [mergingCount, loadData]);

  // --- Card callbacks ---

  const handleApprove = async (card: CardItem) => {
    if (card.kind === 'instruction') {
      try {
        const updated = await updateLoreRule(card.repoName, card.proposalId, card.rule.id, {
          status: 'approved',
        });
        setCards((prev) =>
          prev.map((c) => {
            if (c.kind === 'instruction' && c.rule.id === card.rule.id) {
              const updatedRule = updated.rules.find((r: LoreRule) => r.id === card.rule.id);
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
      setCards((prev) =>
        prev.map((c) => {
          if (c.kind === 'instruction' && c.rule.id === card.rule.id) {
            const updatedRule = updated.rules.find((r: LoreRule) => r.id === card.rule.id);
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
        setCards((prev) =>
          prev.filter((c) => !(c.kind === 'instruction' && c.rule.id === card.rule.id))
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
        text: newText,
      });
      setCards((prev) =>
        prev.map((c) => {
          if (c.kind === 'instruction' && c.rule.id === card.rule.id) {
            const updatedRule = updated.rules.find((r: LoreRule) => r.id === card.rule.id);
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
          if (c.kind === 'instruction' && c.rule.id === card.rule.id) {
            const updatedRule = updated.rules.find((r: LoreRule) => r.id === card.rule.id);
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
    const groups = new Map<string, { repoName: string; proposalId: string; rules: LoreRule[] }>();
    for (const card of approvedCards) {
      const key = `${card.repoName}::${card.proposalId}`;
      if (!groups.has(key)) {
        groups.set(key, { repoName: card.repoName, proposalId: card.proposalId, rules: [] });
      }
      groups.get(key)!.rules.push(card.rule);
    }

    // Check if any rules are public (need merge + diff review)
    const hasPublic = approvedCards.some((c) => effectiveLayer(c.rule) === 'repo_public');

    if (hasPublic) {
      // Fire off merges in background — user can leave and come back
      try {
        for (const [, group] of groups) {
          await startLoreMerge(group.repoName, group.proposalId);
        }
        setMergingCount(groups.size);
        // Clear cards since they're now being merged
        setCards([]);
        toastSuccess(
          'Merge started. You can leave this page — the diff will be here when you return.'
        );
      } catch (err) {
        alert('Merge Failed', getErrorMessage(err, 'Failed to start merge'));
      }
    } else {
      // Private-only: start merge, poll briefly, apply
      setApplying(true);
      try {
        for (const [, group] of groups) {
          await startLoreMerge(group.repoName, group.proposalId);
        }
        // Poll for completion (private merges are typically fast)
        const maxAttempts = 60;
        for (let i = 0; i < maxAttempts; i++) {
          await new Promise((resolve) => setTimeout(resolve, 3000));
          let allDone = true;
          for (const [, group] of groups) {
            const res = await getLoreProposals(group.repoName);
            const p = (res.proposals || []).find((pr) => pr.id === group.proposalId);
            if (p?.merge_error) throw new Error(p.merge_error);
            if (p?.status === 'merging') {
              allDone = false;
              break;
            }
            if (p?.merge_previews) {
              // Apply immediately
              const merges = p.merge_previews.map((mp) => ({
                layer: mp.layer,
                content: mp.merged_content,
              }));
              await applyLoreMerge(group.repoName, group.proposalId, merges);
            }
          }
          if (allDone) break;
        }
        toastSuccess(`${approvedCards.length} rules saved`);
        invalidateProposals();
        loadData();
      } catch (err) {
        alert('Apply Failed', getErrorMessage(err, 'Failed to apply rules'));
      } finally {
        setApplying(false);
      }
    }
  };

  const handleCommitAndPush = async (review: MergeReviewItem) => {
    setApplying(true);
    try {
      const publicPreviews = review.previews
        .filter((p) => p.layer === 'repo_public')
        .map((p) => ({
          layer: p.layer,
          content: editedPreviews[p.layer] ?? p.merged_content,
        }));
      // Apply private layers without auto-commit
      const privatePreviews = review.previews
        .filter((p) => p.layer !== 'repo_public')
        .map((p) => ({ layer: p.layer, content: p.merged_content }));

      if (privatePreviews.length > 0) {
        await applyLoreMerge(review.repoName, review.proposal.id, privatePreviews);
      }
      if (publicPreviews.length > 0) {
        await applyLoreMerge(review.repoName, review.proposal.id, publicPreviews, true);
      }

      const mode = config?.lore?.public_rule_mode || 'direct_push';
      toastSuccess(mode === 'create_pr' ? 'PR created' : 'Committed and pushed');
      invalidateProposals();
      loadData();
    } catch (err) {
      await alert('Push Failed', getErrorMessage(err, 'Failed to push'));
    } finally {
      setApplying(false);
    }
  };

  const handleDismissMergeReview = async (review: MergeReviewItem) => {
    // TODO: could clear merge_previews on the server. For now just reload.
    setMergeReviews((prev) => prev.filter((r) => r.proposal.id !== review.proposal.id));
  };

  // --- Derived state ---

  const pendingCards = cards.filter((c) =>
    c.kind === 'instruction' ? c.rule.status === 'pending' : true
  );
  const approvedCards = cards.filter(
    (c) => c.kind === 'instruction' && c.rule.status === 'approved'
  );
  const allTriaged = pendingCards.length === 0 && approvedCards.length > 0;

  // --- Render ---

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

  return (
    <div className={styles.container} data-testid="lore-page">
      <div className={styles.header}>
        <h2>Lore</h2>
        <p className={styles.headerSubtitle}>Schmux continual learning system</p>
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

      {/* Lore disabled */}
      {loreStatus && !loreStatus.enabled ? (
        <div className="empty-state">
          <div className="empty-state__icon">!</div>
          <h3 className="empty-state__title">Lore Disabled</h3>
          <p className="empty-state__description">
            The lore system is disabled. <a href="/config?tab=advanced">Enable it in config</a> to
            start capturing agent learnings.
          </p>
        </div>
      ) : (
        <>
          {/* Merge reviews ready (server-persisted — survives navigation) */}
          {mergeReviews.map((review) => (
            <div
              key={review.proposal.id}
              className={styles.proposalCard}
              style={{ marginBottom: '1rem' }}
            >
              <h3>Review Changes</h3>
              {review.previews
                .filter((p) => p.layer === 'repo_public')
                .map((preview) => (
                  <div key={preview.layer} style={{ marginBottom: '1rem' }}>
                    <div
                      style={{
                        display: 'flex',
                        alignItems: 'center',
                        gap: '0.5rem',
                        marginBottom: '0.5rem',
                      }}
                    >
                      <span className={styles.ruleLayer}>
                        {LAYER_LABELS[preview.layer] || preview.layer}
                      </span>
                      <span style={{ fontSize: '0.8rem', color: 'var(--text-secondary)' }}>
                        {preview.summary}
                      </span>
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
                  onClick={() => handleDismissMergeReview(review)}
                  disabled={applying}
                >
                  Dismiss
                </button>
                <button
                  className={styles.applyButton}
                  onClick={() => handleCommitAndPush(review)}
                  disabled={applying}
                >
                  {applying && <span className="spinner spinner--small" />}
                  {(config?.lore?.public_rule_mode || 'direct_push') === 'create_pr'
                    ? 'Create PR'
                    : 'Commit & Push'}
                </button>
              </div>
            </div>
          ))}

          {/* Merging in progress indicator */}
          {mergingCount > 0 && mergeReviews.length === 0 && (
            <div className={styles.mergingStatus} style={{ marginBottom: '1rem' }}>
              <span className="spinner spinner--small" />
              Merging rules in the background...
            </div>
          )}

          {/* Summary banner when all cards are triaged */}
          {allTriaged && (
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
                        <p>{priv.length} private (this repo) — saved immediately</p>
                      )}
                      {privAll.length > 0 && (
                        <p>{privAll.length} private (all repos) — saved immediately</p>
                      )}
                      {pub.length > 0 && <p>{pub.length} public — will be merged into CLAUDE.md</p>}
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
                  card.kind === 'instruction' ? `rule-${card.rule.id}` : `action-${card.action.id}`;
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
          ) : mergeReviews.length === 0 && mergingCount === 0 ? (
            <div className="empty-state">
              <p className="empty-state__description">
                Nothing to review. New insights will appear here as agents work.
              </p>
            </div>
          ) : null}
        </>
      )}

      {isDevMode && (
        <section style={{ marginTop: '2rem' }}>
          <button
            className={styles.toggleButton}
            onClick={() => {
              const next = !showDebug;
              setShowDebug(next);
              if (next && debugRepo) {
                getLoreEntries(debugRepo)
                  .then((res) => setDebugEntries(res.entries || []))
                  .catch(() => {});
              }
            }}
          >
            {showDebug ? '\u25BC' : '\u25B6'} Debug
          </button>
          {showDebug && (
            <div style={{ marginTop: '0.75rem' }}>
              {repos.length > 1 && (
                <select
                  className={styles.filterSelect}
                  value={debugRepo}
                  onChange={(e) => {
                    setDebugRepo(e.target.value);
                    getLoreEntries(e.target.value)
                      .then((res) => setDebugEntries(res.entries || []))
                      .catch(() => {});
                  }}
                >
                  {repos.map((r) => (
                    <option key={r.name} value={r.name}>
                      {r.name}
                    </option>
                  ))}
                </select>
              )}
              <div style={{ display: 'flex', gap: '0.5rem', margin: '0.5rem 0' }}>
                <button
                  className={styles.curateButton}
                  onClick={() => debugRepo && startCuration(debugRepo)}
                  disabled={!!activeCurations[debugRepo] || pendingCurations.has(debugRepo)}
                >
                  {activeCurations[debugRepo] || pendingCurations.has(debugRepo)
                    ? 'Curating...'
                    : 'Trigger Curation'}
                </button>
                <button
                  className={styles.deleteButton}
                  onClick={async () => {
                    if (!debugRepo) return;
                    try {
                      const result = await clearLoreEntries(debugRepo);
                      toastSuccess(`Deleted ${result.cleared} signal file(s)`);
                      setDebugEntries([]);
                    } catch (err) {
                      alert('Clear Failed', getErrorMessage(err, 'Failed to clear signals'));
                    }
                  }}
                  disabled={debugEntries.length === 0}
                >
                  Delete Signals
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
