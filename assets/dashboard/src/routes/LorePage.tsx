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
  LoreRule,
  LoreLayer,
  LoreMergePreview,
  LoreStatusResponse,
} from '../lib/types';
import styles from '../styles/lore.module.css';

type CardItem =
  | { kind: 'instruction'; rule: LoreRule; repoName: string; proposalId: string; createdAt: string }
  | { kind: 'action'; action: SpawnEntry; repoName: string; createdAt: string };

type Phase = 'triage' | 'summary' | 'applying' | 'mergeReview' | 'done';

const LAYER_LABELS: Record<string, string> = {
  repo_public: 'Public',
  repo_private: 'Private',
  cross_repo_private: 'Cross-Repo Private',
};

interface ProposalGroup {
  repoName: string;
  proposalId: string;
  rules: LoreRule[];
}

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
  const [phase, setPhase] = useState<Phase>('triage');
  const [mergePreviews, setMergePreviews] = useState<LoreMergePreview[]>([]);
  const [editedPreviews, setEditedPreviews] = useState<Record<string, string>>({});
  const [applying, setApplying] = useState(false);
  const [showDebug, setShowDebug] = useState(false);
  const [debugRepo, setDebugRepo] = useState(repos[0]?.name || '');
  const [debugEntries, setDebugEntries] = useState<LoreEntry[]>([]);

  const loadData = useCallback(async () => {
    setLoading(true);
    setError('');

    const statusPromise = getLoreStatus()
      .then(setLoreStatus)
      .catch(() => {});

    // Fan out across all repos
    const allCards: CardItem[] = [];
    await Promise.allSettled(
      repos.map(async (repo) => {
        const [proposalRes, actionRes] = await Promise.all([
          getLoreProposals(repo.name),
          getAllSpawnEntries(repo.name),
        ]);

        // Flatten pending rules from pending/merging proposals
        for (const proposal of proposalRes.proposals || []) {
          if (proposal.status !== 'pending' && proposal.status !== 'merging') continue;
          for (const rule of proposal.rules || []) {
            if (rule.status !== 'pending') continue;
            // Deduplicate by normalized text — later proposals win
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

        // Add proposed actions
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

    // Sort newest first
    allCards.sort((a, b) => b.createdAt.localeCompare(a.createdAt));

    await statusPromise;
    setCards(allCards);
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

  const checkTriageComplete = useCallback((updatedCards: CardItem[]) => {
    const pendingInstructions = updatedCards.filter(
      (c) => c.kind === 'instruction' && c.rule.status === 'pending'
    );
    const pendingActions = updatedCards.filter((c) => c.kind === 'action');
    const approvedInstructions = updatedCards.filter(
      (c) => c.kind === 'instruction' && c.rule.status === 'approved'
    );

    if (
      pendingInstructions.length === 0 &&
      pendingActions.length === 0 &&
      approvedInstructions.length > 0
    ) {
      setPhase('summary');
    }
  }, []);

  const handleApprove = async (card: CardItem) => {
    if (card.kind === 'instruction') {
      try {
        const updated = await updateLoreRule(card.repoName, card.proposalId, card.rule.id, {
          status: 'approved',
        });
        setCards((prev) => {
          const next = prev.map((c) => {
            if (c.kind === 'instruction' && c.rule.id === card.rule.id) {
              const updatedRule = updated.rules.find((r: LoreRule) => r.id === card.rule.id);
              return updatedRule ? { ...c, rule: updatedRule } : c;
            }
            return c;
          });
          checkTriageComplete(next);
          return next;
        });
        invalidateProposals();
      } catch (err) {
        alert('Update Failed', getErrorMessage(err, 'Failed to approve rule'));
      }
    } else {
      try {
        await pinSpawnEntry(card.repoName, card.action.id);
        setCards((prev) => {
          const next = prev.filter((c) => !(c.kind === 'action' && c.action.id === card.action.id));
          checkTriageComplete(next);
          return next;
        });
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
      setPhase('triage');
      invalidateProposals();
    } catch (err) {
      alert('Update Failed', getErrorMessage(err, 'Failed to undo approval'));
    }
  };

  const handleDismiss = async (card: CardItem) => {
    if (card.kind === 'instruction') {
      try {
        await updateLoreRule(card.repoName, card.proposalId, card.rule.id, {
          status: 'dismissed',
        });
        setCards((prev) => {
          const next = prev.filter(
            (c) => !(c.kind === 'instruction' && c.rule.id === card.rule.id)
          );
          checkTriageComplete(next);
          return next;
        });
        invalidateProposals();
      } catch (err) {
        alert('Dismiss Failed', getErrorMessage(err, 'Failed to dismiss rule'));
      }
    } else {
      try {
        await dismissSpawnEntry(card.repoName, card.action.id);
        setCards((prev) => {
          const next = prev.filter((c) => !(c.kind === 'action' && c.action.id === card.action.id));
          checkTriageComplete(next);
          return next;
        });
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
    const pendingCards = cards.filter((c) =>
      c.kind === 'instruction' ? c.rule.status === 'pending' : true
    );
    for (const card of pendingCards) {
      await handleApprove(card);
    }
  };

  // Group approved instruction cards by (repoName, proposalId)
  const buildProposalGroups = (approvedCards: CardItem[]): Map<string, ProposalGroup> => {
    const groups = new Map<string, ProposalGroup>();
    for (const card of approvedCards) {
      if (card.kind !== 'instruction') continue;
      const key = `${card.repoName}::${card.proposalId}`;
      if (!groups.has(key)) {
        groups.set(key, { repoName: card.repoName, proposalId: card.proposalId, rules: [] });
      }
      groups.get(key)!.rules.push(card.rule);
    }
    return groups;
  };

  const effectiveLayer = (rule: LoreRule): LoreLayer => {
    return rule.chosen_layer || rule.suggested_layer;
  };

  const pollForMergeCompletion = async (
    groups: Map<string, ProposalGroup>
  ): Promise<LoreMergePreview[]> => {
    const groupArr = Array.from(groups.values());
    const allPreviews: LoreMergePreview[] = [];
    // eslint-disable-next-line no-constant-condition
    while (true) {
      await new Promise((resolve) => setTimeout(resolve, 3000));
      let allDone = true;
      for (const group of groupArr) {
        const proposalRes = await getLoreProposals(group.repoName);
        const proposal = (proposalRes.proposals || []).find((p) => p.id === group.proposalId);
        if (proposal && proposal.status === 'merging') {
          allDone = false;
        } else if (proposal?.merge_error) {
          throw new Error(proposal.merge_error);
        } else if (proposal?.merge_previews) {
          // Collect previews not yet added
          for (const preview of proposal.merge_previews) {
            if (!allPreviews.some((p) => p.layer === preview.layer)) {
              allPreviews.push(preview);
            }
          }
        }
      }
      if (allDone) break;
    }
    return allPreviews;
  };

  const handleApply = async () => {
    setPhase('applying');
    setMergePreviews([]);
    setEditedPreviews({});

    const approvedCards = cards.filter(
      (c) => c.kind === 'instruction' && c.rule.status === 'approved'
    );
    const groups = buildProposalGroups(approvedCards);

    try {
      // Always start merge for each proposal group (handles both public and private layers)
      for (const [, group] of groups) {
        await startLoreMerge(group.repoName, group.proposalId);
      }

      // Poll until merge completes and previews are available
      const previews = await pollForMergeCompletion(groups);
      setMergePreviews(previews);

      // Check if any previews target a public layer
      const hasPublicPreviews = previews.some((p) => p.layer === 'repo_public');

      if (hasPublicPreviews) {
        // Apply private layers immediately, show diff for public
        const privatePreviews = previews.filter((p) => p.layer !== 'repo_public');
        if (privatePreviews.length > 0) {
          for (const [, group] of groups) {
            const merges = privatePreviews
              .filter((p) => group.rules.some((r) => effectiveLayer(r) === p.layer))
              .map((p) => ({ layer: p.layer, content: p.merged_content }));
            if (merges.length > 0) {
              await applyLoreMerge(group.repoName, group.proposalId, merges);
            }
          }
        }
        setPhase('mergeReview');
      } else {
        // No public layers — apply everything directly
        for (const [, group] of groups) {
          const merges = previews
            .filter((p) => group.rules.some((r) => effectiveLayer(r) === p.layer))
            .map((p) => ({ layer: p.layer, content: p.merged_content }));
          if (merges.length > 0) {
            await applyLoreMerge(group.repoName, group.proposalId, merges);
          }
        }
        toastSuccess(`${approvedCards.length} rules saved`);
        setPhase('done');
        invalidateProposals();
        setTimeout(() => {
          setPhase('triage');
          loadData();
        }, 3000);
      }
    } catch (err) {
      await alert('Merge Failed', getErrorMessage(err, 'Failed to merge rules'));
      setPhase('summary');
    }
  };

  const handleCommitAndPush = async () => {
    setApplying(true);
    try {
      const approvedCards = cards.filter(
        (c) => c.kind === 'instruction' && c.rule.status === 'approved'
      );
      const groups = buildProposalGroups(approvedCards);

      for (const [, group] of groups) {
        const publicPreviews = mergePreviews
          .filter(
            (p) =>
              p.layer === 'repo_public' && group.rules.some((r) => effectiveLayer(r) === p.layer)
          )
          .map((p) => ({
            layer: p.layer,
            content: editedPreviews[p.layer] ?? p.merged_content,
          }));
        if (publicPreviews.length > 0) {
          await applyLoreMerge(group.repoName, group.proposalId, publicPreviews, true);
        }
      }

      const mode = config?.lore?.public_rule_mode || 'direct_push';
      toastSuccess(mode === 'create_pr' ? 'PR created' : 'Committed and pushed');
      setPhase('done');
      invalidateProposals();
      setTimeout(() => {
        setPhase('triage');
        loadData();
      }, 3000);
    } catch (err) {
      await alert('Push Failed', getErrorMessage(err, 'Failed to push'));
    } finally {
      setApplying(false);
    }
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

  const pendingCards = cards.filter((c) =>
    c.kind === 'instruction' ? c.rule.status === 'pending' : true
  );

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
      ) : phase === 'done' ? (
        <div className="empty-state">
          <p className="empty-state__description">Done. All learnings have been saved.</p>
        </div>
      ) : phase === 'applying' ? (
        <div className={styles.mergingStatus}>
          <span className="spinner spinner--small" />
          Merging rules...
        </div>
      ) : phase === 'summary' ? (
        (() => {
          const approvedCards = cards.filter(
            (c) => c.kind === 'instruction' && c.rule.status === 'approved'
          );
          const privateThisRepo = approvedCards.filter(
            (c) => c.kind === 'instruction' && effectiveLayer(c.rule) === 'repo_private'
          );
          const privateAllRepos = approvedCards.filter(
            (c) => c.kind === 'instruction' && effectiveLayer(c.rule) === 'cross_repo_private'
          );
          const publicRules = approvedCards.filter(
            (c) => c.kind === 'instruction' && effectiveLayer(c.rule) === 'repo_public'
          );

          return (
            <div className={styles.proposalCard}>
              <h3>{approvedCards.length} learnings approved</h3>
              <div style={{ margin: '1rem 0', fontSize: '0.875rem' }}>
                {privateThisRepo.length > 0 && (
                  <p>{privateThisRepo.length} private (this repo) — saved immediately</p>
                )}
                {privateAllRepos.length > 0 && (
                  <p>{privateAllRepos.length} private (all repos) — saved immediately</p>
                )}
                {publicRules.length > 0 && (
                  <p>{publicRules.length} public — will be merged into CLAUDE.md</p>
                )}
              </div>
              <div className={styles.actions}>
                <button className={styles.dismissButton} onClick={() => setPhase('triage')}>
                  Back
                </button>
                <button className={styles.applyButton} onClick={handleApply}>
                  Apply
                </button>
              </div>
            </div>
          );
        })()
      ) : phase === 'mergeReview' ? (
        <div className={styles.proposalCard}>
          <h3>Review Changes</h3>
          {mergePreviews
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
                    compareMethod={DiffMethod.DIFF_TRIMMED_LINES}
                    disableWordDiff={true}
                    extraLinesSurroundingDiff={3}
                  />
                </div>
              </div>
            ))}
          <div className={styles.actions}>
            <button
              className={styles.dismissButton}
              onClick={() => setPhase('summary')}
              disabled={applying}
            >
              Back
            </button>
            <button
              className={styles.applyButton}
              onClick={handleCommitAndPush}
              disabled={applying}
            >
              {applying && <span className="spinner spinner--small" />}
              {(config?.lore?.public_rule_mode || 'direct_push') === 'create_pr'
                ? 'Create PR'
                : 'Commit & Push'}
            </button>
          </div>
        </div>
      ) : (
        <>
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
          ) : (
            <div className="empty-state">
              <p className="empty-state__description">
                Nothing to review. New insights will appear here as agents work.
              </p>
            </div>
          )}
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
