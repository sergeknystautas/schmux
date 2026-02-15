import { useState, useEffect, useCallback, useRef } from 'react';
import { useNavigate } from 'react-router-dom';
import {
  getGitGraph,
  getDiff,
  gitCommitStage,
  gitAmend,
  gitDiscard,
  gitUncommit,
  spawnCommitSession,
  pushToBranch,
  getConfig,
} from '../lib/api';
import { computeLayout, GRAPH_COLOR, HIGHLIGHT_COLOR, ROW_HEIGHT } from '../lib/gitGraphLayout';
import type { GitGraphLayout, LayoutNode, LayoutEdge, LaneLine } from '../lib/gitGraphLayout';
import type { GitGraphResponse, FileDiff } from '../lib/types';
import { useSessions } from '../contexts/SessionsContext';
import { useSync } from '../hooks/useSync';
import { useModal } from './ModalProvider';
import { usePendingNavigation } from '../lib/navigation';
import { formatRelativeTime } from '../lib/utils';
import Tooltip from './Tooltip';

interface GitHistoryDAGProps {
  workspaceId: string;
}

const NODE_RADIUS = 5;
const COLUMN_WIDTH = 20;
const GRAPH_PADDING = 12;

export default function GitHistoryDAG({ workspaceId }: GitHistoryDAGProps) {
  const navigate = useNavigate();
  const { confirm, alert } = useModal();
  const { setPendingNavigation } = usePendingNavigation();
  const [data, setData] = useState<GitGraphResponse | null>(null);
  const [diffFiles, setDiffFiles] = useState<FileDiff[]>([]);
  const [layout, setLayout] = useState<GitGraphLayout | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [loading, setLoading] = useState(true);
  const [copiedHash, setCopiedHash] = useState<string | null>(null);
  const [syncing, setSyncing] = useState(false);
  const [ffToMainSyncing, setFfToMainSyncing] = useState(false);
  const [pushToBranchSyncing, setPushToBranchSyncing] = useState(false);
  const [selectedFiles, setSelectedFiles] = useState<Set<string>>(new Set());
  const knownFilesRef = useRef<Set<string>>(new Set());
  const [isCommitting, setIsCommitting] = useState(false);
  const [isAmending, setIsAmending] = useState(false);
  const [isDiscarding, setIsDiscarding] = useState(false);
  const [isUncommitting, setIsUncommitting] = useState(false);
  const [commitMessageConfigured, setCommitMessageConfigured] = useState(false);
  const { handleSmartSync, handleLinearSyncToMain, handlePushToBranch } = useSync();
  const containerRef = useRef<HTMLDivElement>(null);
  const prevFingerprintRef = useRef('');
  const [containerHeight, setContainerHeight] = useState(0);

  // Pull workspace data early so all hooks are called before any conditional returns.
  const { workspaces } = useSessions();
  const ws = workspaces.find((w) => w.id === workspaceId);
  const gitFingerprint = ws
    ? `${ws.git_ahead}:${ws.git_behind}:${ws.git_files_changed}:${ws.git_lines_added}:${ws.git_lines_removed}`
    : '';

  // Measure container height so we can request the right number of commits
  useEffect(() => {
    const el = containerRef.current;
    if (!el) return;
    const observer = new ResizeObserver((entries) => {
      for (const entry of entries) {
        setContainerHeight(entry.contentRect.height);
      }
    });
    observer.observe(el);
    return () => observer.disconnect();
  }, []);

  // Fetch config to check if commit message target is configured
  useEffect(() => {
    getConfig()
      .then((config) => {
        setCommitMessageConfigured(!!config.commit_message?.target);
      })
      .catch(() => {
        setCommitMessageConfigured(false);
      });
  }, []);

  // Reserve rows for virtual nodes: you-are-here (1) + commit workflow (2 + file count)
  const virtualRowOverhead = 1 + (diffFiles.length > 0 ? 2 + diffFiles.length : 0);
  const maxCommits =
    containerHeight > 0
      ? Math.max(5, Math.floor(containerHeight / ROW_HEIGHT) - virtualRowOverhead)
      : 0;

  const fetchData = useCallback(async () => {
    if (maxCommits <= 0) return; // wait for container measurement
    try {
      const [graphResp, diffResp] = await Promise.all([
        getGitGraph(workspaceId, { maxTotal: maxCommits }),
        getDiff(workspaceId).catch(() => ({ files: [] as FileDiff[] })),
      ]);
      setData(graphResp);
      const files = diffResp.files || [];
      setDiffFiles(files);
      setSelectedFiles((prev) => {
        const newPaths = new Set(files.map((f) => f.new_path || f.old_path || ''));
        const known = knownFilesRef.current;
        if (known.size === 0) {
          knownFilesRef.current = newPaths;
          return newPaths;
        }
        const result = new Set<string>();
        for (const p of newPaths) {
          if (known.has(p)) {
            if (prev.has(p)) result.add(p); // preserve user's selection
          } else {
            result.add(p); // new file — auto-select
          }
        }
        knownFilesRef.current = newPaths;
        return result;
      });
      setLayout(computeLayout(graphResp, files));
      setError(null);
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to load git graph');
    } finally {
      setLoading(false);
    }
  }, [workspaceId, maxCommits]);

  useEffect(() => {
    fetchData();
  }, [fetchData]);

  // Refetch when git state changes via WebSocket session updates.
  // Track the git-relevant fields and refetch when they change.
  useEffect(() => {
    if (gitFingerprint && gitFingerprint !== prevFingerprintRef.current) {
      prevFingerprintRef.current = gitFingerprint;
      fetchData();
    }
  }, [gitFingerprint, fetchData]);

  const copyHash = useCallback((hash: string) => {
    navigator.clipboard
      .writeText(hash)
      .then(() => {
        setCopiedHash(hash);
        setTimeout(() => setCopiedHash(null), 2000);
      })
      .catch(() => {
        // Clipboard API not available (non-HTTPS)
      });
  }, []);

  if (!ws) {
    return (
      <div className="git-dag" ref={containerRef}>
        <div className="loading-state">
          <div className="spinner" /> Loading workspace...
        </div>
      </div>
    );
  }

  if (loading || maxCommits <= 0) {
    return (
      <div className="git-dag" ref={containerRef}>
        <div className="loading-state">
          <div className="spinner" /> Loading commit graph...
        </div>
      </div>
    );
  }

  if (error) {
    return (
      <div className="git-dag" ref={containerRef}>
        <div className="banner banner--error">{error}</div>
      </div>
    );
  }

  if (!data || !layout || layout.nodes.length === 0) {
    return (
      <div className="git-dag" ref={containerRef}>
        <div className="empty-state">
          <div className="empty-state__title">No commits</div>
          <div className="empty-state__description">
            No commit history found for this workspace.
          </div>
        </div>
      </div>
    );
  }

  const graphWidth = GRAPH_PADDING * 2 + layout.columnCount * COLUMN_WIDTH;
  const totalHeight = layout.nodes.length * layout.rowHeight;
  const yahCol = layout.youAreHereColumn;

  const renderNode = (ln: LayoutNode, lay: GitGraphLayout) => {
    if (ln.nodeType === 'you-are-here') {
      const defaultBranch = ws?.default_branch || 'main';
      const branchName = ws?.branch || 'current branch';
      const aheadCount = ws?.git_ahead ?? 0;
      const behindCount = ws?.git_behind ?? 0;
      const filesChanged = ws?.git_files_changed ?? 0;
      const commitsSynced = ws?.commits_synced_with_remote ?? false;
      const showPushToDefault = aheadCount > 0;
      // Show push-to-branch whenever the branch is not yet synced with origin.
      // This also covers the "0 commits ahead of default but no remote branch yet" case.
      const showPushToBranch = !commitsSynced;
      const hasLocalChanges = filesChanged > 0;
      const isBehind = behindCount > 0;
      const pushToDefaultDisabled = isBehind || hasLocalChanges;
      const pushToBranchDisabled = hasLocalChanges || commitsSynced; // Disabled if local changes or already pushed

      // Determine tooltip based on why it's disabled
      let pushToDefaultTooltip: string;
      if (pushToDefaultDisabled) {
        if (hasLocalChanges) {
          pushToDefaultTooltip = `You cannot push to ${defaultBranch} with local changes. Please commit or discard them before pushing.`;
        } else {
          pushToDefaultTooltip = `You cannot push to ${defaultBranch} until you have pulled the latest from ${defaultBranch}.`;
        }
      } else {
        pushToDefaultTooltip = `Merge fast forwards ${aheadCount} commit${aheadCount === 1 ? '' : 's'} to origin/${defaultBranch}`;
      }

      let pushToBranchTooltip: string;
      if (pushToBranchDisabled) {
        if (hasLocalChanges) {
          pushToBranchTooltip = `You cannot push with local changes. Please commit or discard them before pushing.`;
        } else {
          pushToBranchTooltip = `Branch is already synced with origin/${branchName}.`;
        }
      } else {
        pushToBranchTooltip = `Push commits to origin/${branchName}`;
      }

      const onPushToDefaultClick = async () => {
        if (!ws || pushToDefaultDisabled || ffToMainSyncing) return;
        setFfToMainSyncing(true);
        try {
          await handleLinearSyncToMain(ws.id, defaultBranch, ws.path);
        } finally {
          setFfToMainSyncing(false);
        }
      };

      const onPushToBranchClick = async () => {
        if (!ws || pushToBranchSyncing) return;
        setPushToBranchSyncing(true);
        try {
          await handlePushToBranch(ws.id, branchName);
        } finally {
          setPushToBranchSyncing(false);
        }
      };

      const pushToDefaultButton = (
        <button
          className="git-dag__ff-to-main-button"
          onClick={onPushToDefaultClick}
          disabled={pushToDefaultDisabled || ffToMainSyncing}
        >
          {ffToMainSyncing ? (
            <>
              <span className="spinner" /> Pushing to {defaultBranch}
            </>
          ) : (
            <>Push to {defaultBranch}</>
          )}
        </button>
      );

      const pushToBranchButton = (
        <button
          className="git-dag__push-to-branch-button"
          onClick={onPushToBranchClick}
          disabled={pushToBranchDisabled || pushToBranchSyncing}
        >
          {pushToBranchSyncing ? (
            <>
              <span className="spinner" /> Pushing
            </>
          ) : (
            <>Push to branch</>
          )}
        </button>
      );

      return (
        <div key={ln.hash} className="git-dag__row" style={{ height: lay.rowHeight }}>
          <span className="git-dag__you-are-here">You are here</span>
          {showPushToDefault &&
            (pushToDefaultDisabled ? (
              <Tooltip content={pushToDefaultTooltip}>
                <span style={{ display: 'inline-flex' }}>{pushToDefaultButton}</span>
              </Tooltip>
            ) : (
              pushToDefaultButton
            ))}
          {showPushToBranch &&
            (pushToBranchDisabled ? (
              <Tooltip content={pushToBranchTooltip}>
                <span style={{ display: 'inline-flex' }}>{pushToBranchButton}</span>
              </Tooltip>
            ) : (
              pushToBranchButton
            ))}
        </div>
      );
    }
    if (ln.nodeType === 'commit-actions') {
      return (
        <div
          key={ln.hash}
          className="git-dag__row git-dag__commit-row"
          style={{ height: lay.rowHeight }}
        >
          <button
            className="git-dag__btn"
            onClick={() =>
              setSelectedFiles(new Set(diffFiles.map((f) => f.new_path || f.old_path || '')))
            }
          >
            Select All
          </button>
          <button className="git-dag__btn" onClick={() => setSelectedFiles(new Set())}>
            Deselect All
          </button>
          <button
            className="git-dag__btn"
            onClick={async () => {
              const filesToDiscard = Array.from(selectedFiles);
              if (filesToDiscard.length === 0) return;
              const title = `Discard ${filesToDiscard.length} selected file${filesToDiscard.length === 1 ? '' : 's'}?`;
              const message = `This will discard changes to:\n\n${filesToDiscard.map((f) => `• ${f}`).join('\n')}`;
              const confirmed = await confirm(title, {
                danger: true,
                detailedMessage: message,
              });
              if (!confirmed) return;
              setIsDiscarding(true);
              try {
                await gitDiscard(workspaceId, filesToDiscard);
                fetchData();
              } catch (err) {
                await alert('Discard Failed', err instanceof Error ? err.message : 'Unknown error');
              } finally {
                setIsDiscarding(false);
              }
            }}
            disabled={isDiscarding || selectedFiles.size === 0}
          >
            {isDiscarding ? 'Discarding...' : 'Discard'}
          </button>
        </div>
      );
    }
    if (ln.nodeType === 'commit-file' && ln.file) {
      const filePath = ln.file.new_path || ln.file.old_path || '';
      const isSelected = selectedFiles.has(filePath);
      const status = ln.file.status || 'modified';
      const statusLabel =
        status === 'added' ? 'A' : status === 'deleted' ? 'D' : status === 'untracked' ? '??' : 'M';
      const statusClass =
        status === 'added'
          ? 'commit-workflow__status--added'
          : status === 'deleted'
            ? 'commit-workflow__status--deleted'
            : 'commit-workflow__status--modified';
      const toggleFile = () => {
        const newSet = new Set(selectedFiles);
        if (newSet.has(filePath)) newSet.delete(filePath);
        else newSet.add(filePath);
        setSelectedFiles(newSet);
      };
      return (
        <div
          key={ln.hash}
          className="git-dag__file-row"
          style={{ cursor: 'pointer' }}
          onClick={toggleFile}
          role="button"
          tabIndex={0}
          aria-label={`${isSelected ? 'Deselect' : 'Select'} file ${filePath}`}
          onKeyDown={(e) => {
            if (e.key === 'Enter' || e.key === ' ') {
              e.preventDefault();
              toggleFile();
            }
          }}
        >
          <input
            type="checkbox"
            checked={isSelected}
            onChange={toggleFile}
            onClick={(e) => e.stopPropagation()}
            style={{ marginRight: '8px' }}
            aria-label={`Select ${filePath}`}
          />
          <span className={`commit-workflow__status ${statusClass}`}>{statusLabel}</span>
          <span className="commit-workflow__filename">{filePath}</span>
        </div>
      );
    }
    if (ln.nodeType === 'commit-footer') {
      const canAmend = (ws?.git_ahead ?? 0) > 0;
      const commitDisabled = !commitMessageConfigured || selectedFiles.size === 0 || isCommitting;

      const commitButton = (
        <button
          className="git-dag__btn"
          disabled={commitDisabled}
          onClick={async () => {
            const fileList = Array.from(selectedFiles)
              .map((f) => `• ${f}`)
              .join('\n');
            const confirmed = await confirm(`Commit ${selectedFiles.size} files?`, {
              confirmText: 'Commit',
              detailedMessage: `The following files will be staged and committed:\n\n${fileList}`,
            });
            if (!confirmed) return;
            setIsCommitting(true);
            try {
              await gitCommitStage(workspaceId, Array.from(selectedFiles));
              if (ws) {
                const results = await spawnCommitSession(
                  workspaceId,
                  ws.repo,
                  ws.branch,
                  Array.from(selectedFiles)
                );
                if (results.length > 0 && results[0].session_id) {
                  setPendingNavigation({ type: 'session', id: results[0].session_id });
                }
              }
              fetchData();
            } catch (err) {
              await alert('Commit Failed', err instanceof Error ? err.message : 'Unknown error');
            } finally {
              setIsCommitting(false);
            }
          }}
        >
          {isCommitting ? 'Committing...' : 'Commit'}
        </button>
      );

      return (
        <div
          key={ln.hash}
          className="git-dag__row git-dag__commit-row"
          style={{ height: lay.rowHeight }}
        >
          {!commitMessageConfigured ? (
            <Tooltip content="Select a model in Settings > Code Review > Commit Message">
              <span style={{ display: 'inline-flex' }}>{commitButton}</span>
            </Tooltip>
          ) : (
            commitButton
          )}
          {canAmend && (
            <button
              className="git-dag__btn"
              disabled={selectedFiles.size === 0 || isAmending}
              onClick={async () => {
                const fileList = Array.from(selectedFiles)
                  .map((f) => `• ${f}`)
                  .join('\n');
                const confirmed = await confirm(`Amend commit with ${selectedFiles.size} files?`, {
                  confirmText: 'Amend',
                  detailedMessage: `The following files will be staged and amend the previous commit:\n\n${fileList}`,
                });
                if (!confirmed) return;
                setIsAmending(true);
                try {
                  await gitAmend(workspaceId, Array.from(selectedFiles));
                  fetchData();
                } catch (err) {
                  await alert('Amend Failed', err instanceof Error ? err.message : 'Unknown error');
                } finally {
                  setIsAmending(false);
                }
              }}
            >
              {isAmending ? 'Amending...' : 'Amend'}
            </button>
          )}
        </div>
      );
    }
    if (ln.nodeType === 'sync-summary' && ln.syncSummary) {
      const hasKnownConflict = ws?.conflict_on_branch && ws.conflict_on_branch === ws.branch;
      const syncIndicator = hasKnownConflict ? '🟡' : '🟢';
      const defaultBranch = ws?.default_branch || 'main';

      const onSyncClick = async () => {
        if (!ws || syncing) return;
        setSyncing(true);
        try {
          await handleSmartSync(ws);
        } finally {
          setSyncing(false);
        }
      };

      return (
        <div key={ln.hash} className="git-dag__row" style={{ height: lay.rowHeight }}>
          <Tooltip content={`Iteratively rebases this branch to origin/${defaultBranch}`}>
            <button
              className="git-dag__sync-button"
              onClick={onSyncClick}
              disabled={syncing || !ws}
            >
              {syncing ? (
                <>
                  <span className="spinner" /> Pulling
                </>
              ) : (
                <>
                  {syncIndicator} Pull from {defaultBranch}
                </>
              )}
            </button>
          </Tooltip>
          <span className="git-dag__sync-summary">
            &middot; {ln.syncSummary.count} commit
            {ln.syncSummary.count !== 1 ? 's' : ''}
          </span>
          <span style={{ flex: 1 }} />
          <span className="git-dag__time">
            {formatRelativeTime(ln.syncSummary.newestTimestamp)}
          </span>
        </div>
      );
    }
    if (ln.nodeType === 'truncation') {
      return (
        <div
          key={ln.hash}
          className="git-dag__row git-dag__truncation-row"
          style={{ height: lay.rowHeight }}
        >
          <span className="git-dag__truncation-text">⋯ older commits not shown</span>
        </div>
      );
    }
    const isHeadCommit = ln.node.is_head.includes(ws?.branch || '');
    const canUncommit = isHeadCommit && (ws?.git_ahead ?? 0) > 0;

    return (
      <div
        key={ln.hash}
        className="git-dag__row"
        style={{ height: lay.rowHeight }}
        title={ln.node.hash}
      >
        <button
          className="git-dag__hash"
          onClick={() => copyHash(ln.node.hash)}
          title={copiedHash === ln.node.hash ? 'Copied!' : 'Click to copy full hash'}
        >
          {ln.node.short_hash}
        </button>
        <span className="git-dag__message">
          {ln.node.is_head.length > 0 && (
            <span className="git-dag__head-labels">
              {ln.node.is_head.map((b) => (
                <span key={b} className="git-dag__head-label">
                  {b}
                </span>
              ))}
            </span>
          )}
          <span className="git-dag__message-text">{ln.node.message}</span>
          {canUncommit && (
            <Tooltip content="Keep these changes and make them unstaged locally">
              <button
                className="git-dag__uncommit-btn"
                disabled={isUncommitting}
                onClick={async (e) => {
                  e.stopPropagation();
                  setIsUncommitting(true);
                  try {
                    await gitUncommit(workspaceId, ln.node.hash);
                    fetchData();
                  } catch (err) {
                    await alert(
                      'Uncommit Failed',
                      err instanceof Error ? err.message : 'Unknown error'
                    );
                  } finally {
                    setIsUncommitting(false);
                  }
                }}
              >
                {isUncommitting ? 'Uncommitting...' : 'Uncommit'}
              </button>
            </Tooltip>
          )}
        </span>
        <span className="git-dag__author">{ln.node.author}</span>
        <span className="git-dag__time">{formatRelativeTime(ln.node.timestamp)}</span>
      </div>
    );
  };

  return (
    <div className="git-dag" ref={containerRef}>
      <div className="git-dag__scroll" style={{ overflow: 'auto', flex: 1 }}>
        <div
          className="git-dag__container"
          style={{ position: 'relative', minHeight: totalHeight }}
        >
          <svg
            className="git-dag__svg"
            width={graphWidth}
            height={totalHeight}
            style={{ position: 'absolute', left: 0, top: 0 }}
          >
            {/* Persistent column lines (ISL-style: dashed, background) */}
            {layout.laneLines.map((ll, i) => (
              <ColumnLine
                key={`col-${i}`}
                laneLine={ll}
                rowHeight={layout.rowHeight}
                isHighlight={ll.column === yahCol}
              />
            ))}

            {/* Edges (solid, foreground) */}
            {layout.edges.map((edge, i) => (
              <EdgePath key={i} edge={edge} rowHeight={layout.rowHeight} />
            ))}

            {/* Node glyphs (circles only — ISL style) */}
            {layout.nodes.map((ln) => (
              <NodeCircle
                key={ln.hash}
                node={ln}
                rowHeight={layout.rowHeight}
                isHighlight={ln.column === yahCol}
              />
            ))}
          </svg>

          {/* Row content — each row is absolutely positioned using the same y
              coordinates as the SVG, so the two sides stay perfectly aligned
              regardless of wrapper margins/padding. */}
          <div
            className="git-dag__rows"
            style={{ marginLeft: graphWidth, position: 'relative', minHeight: totalHeight }}
          >
            {/* Commit section background (absolutely positioned behind the rows) */}
            {(() => {
              const commitTypes = new Set(['commit-actions', 'commit-file', 'commit-footer']);
              const commitNodes = layout.nodes.filter((ln) => commitTypes.has(ln.nodeType));
              if (commitNodes.length === 0) return null;
              const topY = commitNodes[0].y;
              const bottomY = commitNodes[commitNodes.length - 1].y + layout.rowHeight;
              return (
                <div
                  className="git-dag__commit-section-bg"
                  style={{
                    position: 'absolute',
                    top: topY,
                    left: 0,
                    right: 0,
                    height: bottomY - topY,
                  }}
                />
              );
            })()}
            {layout.nodes.map((ln) => (
              <div
                key={ln.hash}
                style={{
                  position: 'absolute',
                  top: ln.y,
                  left: 0,
                  right: 0,
                  height: layout.rowHeight,
                }}
              >
                {renderNode(ln, layout)}
              </div>
            ))}
          </div>
        </div>
      </div>
    </div>
  );
}

/** Circle glyph for all nodes (ISL-style: no diamonds) */
function NodeCircle({
  node,
  rowHeight,
  isHighlight,
}: {
  node: LayoutNode;
  rowHeight: number;
  isHighlight: boolean;
}) {
  const cx = GRAPH_PADDING + node.column * COLUMN_WIDTH;
  const cy = node.y + rowHeight / 2;

  // Commit workflow rows don't get circles - just the lane line passes through
  if (
    node.nodeType === 'commit-file' ||
    node.nodeType === 'commit-actions' ||
    node.nodeType === 'commit-footer' ||
    node.nodeType === 'truncation'
  ) {
    return null;
  }

  if (node.nodeType === 'you-are-here') {
    return (
      <circle
        cx={cx}
        cy={cy}
        r={NODE_RADIUS}
        fill={HIGHLIGHT_COLOR}
        stroke={HIGHLIGHT_COLOR}
        strokeWidth={1.5}
      />
    );
  }

  if (node.nodeType === 'sync-summary') {
    return (
      <circle
        cx={cx}
        cy={cy}
        r={NODE_RADIUS}
        fill={GRAPH_COLOR}
        stroke={GRAPH_COLOR}
        strokeWidth={1.5}
      />
    );
  }

  return (
    <circle
      cx={cx}
      cy={cy}
      r={NODE_RADIUS}
      fill="none"
      stroke={isHighlight ? HIGHLIGHT_COLOR : GRAPH_COLOR}
      strokeWidth={1.5}
    />
  );
}

/** Dashed persistent column line (ISL-style column state) */
function ColumnLine({
  laneLine,
  rowHeight,
  isHighlight,
}: {
  laneLine: LaneLine;
  rowHeight: number;
  isHighlight: boolean;
}) {
  const x = GRAPH_PADDING + laneLine.column * COLUMN_WIDTH;
  const y1 = laneLine.fromY + rowHeight / 2;
  const y2 = laneLine.toY + rowHeight / 2;

  return (
    <line
      x1={x}
      y1={y1}
      x2={x}
      y2={y2}
      stroke={isHighlight ? HIGHLIGHT_COLOR : GRAPH_COLOR}
      strokeWidth={1.5}
      strokeDasharray="3,2"
      opacity={0.4}
    />
  );
}

/** Edge line (solid, single color — ISL-style) */
function EdgePath({ edge, rowHeight }: { edge: LayoutEdge; rowHeight: number }) {
  const x1 = GRAPH_PADDING + edge.fromColumn * COLUMN_WIDTH;
  const y1 = edge.fromY + rowHeight / 2;
  const x2 = GRAPH_PADDING + edge.toColumn * COLUMN_WIDTH;
  const y2 = edge.toY + rowHeight / 2;

  if (x1 === x2) {
    return <line x1={x1} y1={y1} x2={x2} y2={y2} stroke={GRAPH_COLOR} strokeWidth={1.5} />;
  }

  // S-curve for cross-column edges
  const cp1Y = y1 + (y2 - y1) * 0.75;
  const cp2Y = y1 + (y2 - y1) * 0.25;
  const d = `M ${x1} ${y1} C ${x1} ${cp1Y}, ${x2} ${cp2Y}, ${x2} ${y2}`;
  return <path d={d} fill="none" stroke={GRAPH_COLOR} strokeWidth={1.5} />;
}
