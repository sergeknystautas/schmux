import { useState, useEffect, useCallback, useRef } from 'react';
import { useNavigate } from 'react-router-dom';
import { getGitGraph } from '../lib/api';
import { computeLayout, GRAPH_COLOR, HIGHLIGHT_COLOR } from '../lib/gitGraphLayout';
import type { GitGraphLayout, LayoutNode, LayoutEdge, LaneLine } from '../lib/gitGraphLayout';
import type { GitGraphResponse } from '../lib/types';
import { useSessions } from '../contexts/SessionsContext';

interface GitHistoryDAGProps {
  workspaceId: string;
}

const NODE_RADIUS = 5;
const COLUMN_WIDTH = 20;
const GRAPH_PADDING = 12;

function relativeTime(timestamp: string): string {
  const now = Date.now();
  const then = new Date(timestamp).getTime();
  const diffSec = Math.floor((now - then) / 1000);
  if (diffSec < 60) return 'just now';
  const diffMin = Math.floor(diffSec / 60);
  if (diffMin < 60) return `${diffMin}m ago`;
  const diffHr = Math.floor(diffMin / 60);
  if (diffHr < 24) return `${diffHr}h ago`;
  const diffDay = Math.floor(diffHr / 24);
  if (diffDay < 30) return `${diffDay}d ago`;
  return new Date(timestamp).toLocaleDateString();
}

export default function GitHistoryDAG({ workspaceId }: GitHistoryDAGProps) {
  const navigate = useNavigate();
  const [data, setData] = useState<GitGraphResponse | null>(null);
  const [layout, setLayout] = useState<GitGraphLayout | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [loading, setLoading] = useState(true);
  const [copiedHash, setCopiedHash] = useState<string | null>(null);

  const fetchData = useCallback(async () => {
    try {
      const resp = await getGitGraph(workspaceId);
      setData(resp);
      setLayout(computeLayout(resp));
      setError(null);
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to load git graph');
    } finally {
      setLoading(false);
    }
  }, [workspaceId]);

  useEffect(() => {
    fetchData();
  }, [fetchData]);

  // Refetch when git state changes via WebSocket session updates.
  // Track the git-relevant fields and refetch when they change.
  const { workspaces } = useSessions();
  const ws = workspaces.find(w => w.id === workspaceId);
  const gitFingerprint = ws
    ? `${ws.git_ahead}:${ws.git_behind}:${ws.git_files_changed}:${ws.git_lines_added}:${ws.git_lines_removed}`
    : '';
  const prevFingerprintRef = useRef(gitFingerprint);

  useEffect(() => {
    if (gitFingerprint && gitFingerprint !== prevFingerprintRef.current) {
      prevFingerprintRef.current = gitFingerprint;
      fetchData();
    }
  }, [gitFingerprint, fetchData]);

  const copyHash = useCallback((hash: string) => {
    navigator.clipboard.writeText(hash).then(() => {
      setCopiedHash(hash);
      setTimeout(() => setCopiedHash(null), 2000);
    });
  }, []);

  if (loading) {
    return <div className="loading-state"><div className="spinner" /> Loading commit graph...</div>;
  }

  if (error) {
    return <div className="banner banner--error">{error}</div>;
  }

  if (!data || !layout || layout.nodes.length === 0) {
    return (
      <div className="empty-state">
        <div className="empty-state__title">No commits</div>
        <div className="empty-state__description">No commit history found for this workspace.</div>
      </div>
    );
  }

  const graphWidth = GRAPH_PADDING * 2 + layout.columnCount * COLUMN_WIDTH;
  const totalHeight = layout.nodes.length * layout.rowHeight;
  const yahCol = layout.youAreHereColumn;

  return (
    <div className="git-dag">
      <div className="git-dag__scroll" style={{ overflow: 'auto', maxHeight: 'calc(100vh - 200px)' }}>
        <div className="git-dag__container" style={{ position: 'relative', minHeight: totalHeight }}>
          <svg
            className="git-dag__svg"
            width={graphWidth}
            height={totalHeight}
            style={{ position: 'absolute', left: 0, top: 0 }}
          >
            {/* Persistent column lines (ISL-style: dashed, background) */}
            {layout.laneLines.map((ll, i) => (
              <ColumnLine key={`col-${i}`} laneLine={ll} rowHeight={layout.rowHeight} isHighlight={ll.column === yahCol} />
            ))}

            {/* Edges (solid, foreground) */}
            {layout.edges.map((edge, i) => (
              <EdgePath key={i} edge={edge} rowHeight={layout.rowHeight} />
            ))}

            {/* Node glyphs (circles only — ISL style) */}
            {layout.nodes.map((ln) => (
              <NodeCircle key={ln.hash} node={ln} rowHeight={layout.rowHeight} isHighlight={ln.column === yahCol} />
            ))}
          </svg>

          {/* Row content */}
          <div className="git-dag__rows" style={{ marginLeft: graphWidth }}>
            {layout.nodes.map((ln) => {
              if (ln.nodeType === 'you-are-here') {
                return (
                  <div key={ln.hash} className="git-dag__row" style={{ height: layout.rowHeight }}>
                    <span className="git-dag__you-are-here">You are here</span>
                  </div>
                );
              }
              if (ln.nodeType === 'view-changes' && ln.dirtyState) {
                return (
                  <div key={ln.hash} className="git-dag__row" style={{ height: layout.rowHeight }}>
                    <button
                      className="git-dag__view-changes"
                      onClick={() => navigate(`/diff/${workspaceId}`)}
                      title="View uncommitted changes"
                    >
                      {ln.dirtyState.filesChanged} file{ln.dirtyState.filesChanged !== 1 ? 's' : ''}, +{ln.dirtyState.linesAdded} −{ln.dirtyState.linesRemoved}
                    </button>
                  </div>
                );
              }
              if (ln.nodeType === 'sync-summary' && ln.syncSummary) {
                return (
                  <div key={ln.hash} className="git-dag__row" style={{ height: layout.rowHeight }}>
                    <span className="git-dag__sync-summary">
                      Sync &middot; {ln.syncSummary.count} commit{ln.syncSummary.count !== 1 ? 's' : ''}
                    </span>
                    <span className="git-dag__time">{relativeTime(ln.syncSummary.newestTimestamp)}</span>
                  </div>
                );
              }
              return (
                <div
                  key={ln.hash}
                  className="git-dag__row"
                  style={{ height: layout.rowHeight }}
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
                          <span key={b} className="git-dag__head-label">{b}</span>
                        ))}
                      </span>
                    )}
                    {ln.node.message}
                  </span>
                  <span className="git-dag__author">{ln.node.author}</span>
                  <span className="git-dag__time">{relativeTime(ln.node.timestamp)}</span>
                </div>
              );
            })}
          </div>
        </div>
      </div>
    </div>
  );
}

/** Circle glyph for all nodes (ISL-style: no diamonds) */
function NodeCircle({ node, rowHeight, isHighlight }: { node: LayoutNode; rowHeight: number; isHighlight: boolean }) {
  const cx = GRAPH_PADDING + node.column * COLUMN_WIDTH;
  const cy = node.y + rowHeight / 2;

  if (node.nodeType === 'you-are-here' || node.nodeType === 'view-changes') {
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
function ColumnLine({ laneLine, rowHeight, isHighlight }: { laneLine: LaneLine; rowHeight: number; isHighlight: boolean }) {
  const x = GRAPH_PADDING + laneLine.column * COLUMN_WIDTH;
  const y1 = laneLine.fromY + rowHeight / 2;
  const y2 = laneLine.toY + rowHeight / 2;

  return (
    <line
      x1={x} y1={y1} x2={x} y2={y2}
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
