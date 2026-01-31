import { useState, useEffect, useCallback } from 'react';
import { getGitGraph } from '../lib/api';
import { computeLayout, laneColorVar } from '../lib/gitGraphLayout';
import type { GitGraphLayout, LayoutNode, LayoutEdge } from '../lib/gitGraphLayout';
import type { GitGraphResponse } from '../lib/types';

interface GitHistoryDAGProps {
  repoName: string;
}

const NODE_RADIUS = 5;
const LANE_WIDTH = 20;
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

export default function GitHistoryDAG({ repoName }: GitHistoryDAGProps) {
  const [data, setData] = useState<GitGraphResponse | null>(null);
  const [layout, setLayout] = useState<GitGraphLayout | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [loading, setLoading] = useState(true);
  const [copiedHash, setCopiedHash] = useState<string | null>(null);

  const fetchData = useCallback(async () => {
    try {
      const resp = await getGitGraph(repoName);
      setData(resp);
      setLayout(computeLayout(resp));
      setError(null);
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to load git graph');
    } finally {
      setLoading(false);
    }
  }, [repoName]);

  useEffect(() => {
    fetchData();
  }, [fetchData]);

  const copyHash = useCallback((hash: string) => {
    navigator.clipboard.writeText(hash).then(() => {
      setCopiedHash(hash);
      setTimeout(() => setCopiedHash(null), 2000);
    });
  }, []);

  if (loading) {
    return <div className="loading-state"><div className="spinner" /> Loading git graph...</div>;
  }

  if (error) {
    return <div className="banner banner--error">{error}</div>;
  }

  if (!data || !layout || layout.nodes.length === 0) {
    return (
      <div className="empty-state">
        <div className="empty-state__title">No commits</div>
        <div className="empty-state__description">No active workspace branches found for this repo.</div>
      </div>
    );
  }

  const graphWidth = GRAPH_PADDING * 2 + layout.laneCount * LANE_WIDTH;
  const totalHeight = layout.nodes.length * layout.rowHeight;

  return (
    <div className="git-dag">
      <div className="git-dag__branches">
        {Object.entries(data.branches).map(([name, info]) => (
          <span
            key={name}
            className="git-dag__branch-label"
            style={{ color: laneColorVar(layout.branchLanes[name] ?? 0) }}
          >
            {name}
            {info.is_main && <span className="git-dag__branch-main"> (main)</span>}
            {info.workspace_ids.length > 0 && (
              <span className="git-dag__branch-ws"> [{info.workspace_ids.join(', ')}]</span>
            )}
          </span>
        ))}
      </div>

      <div className="git-dag__scroll" style={{ overflow: 'auto', maxHeight: 'calc(100vh - 200px)' }}>
        <div className="git-dag__container" style={{ position: 'relative', minHeight: totalHeight }}>
          <svg
            className="git-dag__svg"
            width={graphWidth}
            height={totalHeight}
            style={{ position: 'absolute', left: 0, top: 0 }}
          >
            {/* Edges */}
            {layout.edges.map((edge, i) => (
              <EdgePath key={i} edge={edge} rowHeight={layout.rowHeight} />
            ))}

            {/* Nodes */}
            {layout.nodes.map((ln) => (
              <NodeCircle key={ln.hash} node={ln} rowHeight={layout.rowHeight} />
            ))}
          </svg>

          {/* Commit rows */}
          <div className="git-dag__rows" style={{ marginLeft: graphWidth }}>
            {layout.nodes.map((ln) => (
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
                        <span
                          key={b}
                          className="git-dag__head-label"
                          style={{ borderColor: laneColorVar(layout.branchLanes[b] ?? 0) }}
                        >
                          {b}
                        </span>
                      ))}
                    </span>
                  )}
                  {ln.node.message}
                </span>
                <span className="git-dag__author">{ln.node.author}</span>
                <span className="git-dag__time">{relativeTime(ln.node.timestamp)}</span>
              </div>
            ))}
          </div>
        </div>
      </div>
    </div>
  );
}

function NodeCircle({ node, rowHeight }: { node: LayoutNode; rowHeight: number }) {
  const cx = GRAPH_PADDING + node.lane * LANE_WIDTH;
  const cy = node.y + rowHeight / 2;
  const color = laneColorVar(node.lane);

  if (node.nodeType === 'merge') {
    // Diamond for merge/fork-point
    const s = NODE_RADIUS + 1;
    return (
      <polygon
        points={`${cx},${cy - s} ${cx + s},${cy} ${cx},${cy + s} ${cx - s},${cy}`}
        fill={color}
        stroke={color}
        strokeWidth={1}
      />
    );
  }

  if (node.nodeType === 'fork-point') {
    const s = NODE_RADIUS + 1;
    return (
      <polygon
        points={`${cx},${cy - s} ${cx + s},${cy} ${cx},${cy + s} ${cx - s},${cy}`}
        fill="none"
        stroke={color}
        strokeWidth={1.5}
      />
    );
  }

  // Normal: filled circle for branch commits, open circle for main
  const isMainOnly = node.node.branches.length === 1 && node.node.branches[0] === Object.keys({}).length.toString();
  const fill = node.lane === 0 ? 'none' : color;
  const strokeWidth = node.lane === 0 ? 1.5 : 1;

  return (
    <circle
      cx={cx}
      cy={cy}
      r={NODE_RADIUS}
      fill={fill}
      stroke={color}
      strokeWidth={strokeWidth}
    />
  );
}

function EdgePath({ edge, rowHeight }: { edge: LayoutEdge; rowHeight: number }) {
  const x1 = GRAPH_PADDING + edge.fromLane * LANE_WIDTH;
  const y1 = edge.fromY + rowHeight / 2;
  const x2 = GRAPH_PADDING + edge.toLane * LANE_WIDTH;
  const y2 = edge.toY + rowHeight / 2;

  const color = laneColorVar(edge.fromLane);

  if (x1 === x2) {
    // Straight vertical line
    return <line x1={x1} y1={y1} x2={x2} y2={y2} stroke={color} strokeWidth={1.5} />;
  }

  // Curved connector between lanes
  const midY = (y1 + y2) / 2;
  const d = `M ${x1} ${y1} C ${x1} ${midY}, ${x2} ${midY}, ${x2} ${y2}`;
  return <path d={d} fill="none" stroke={color} strokeWidth={1.5} />;
}
