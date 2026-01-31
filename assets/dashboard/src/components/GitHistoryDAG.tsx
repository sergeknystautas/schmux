import { useState, useEffect, useCallback } from 'react';
import { useNavigate } from 'react-router-dom';
import { getGitGraph } from '../lib/api';
import { computeLayout, laneColorVar } from '../lib/gitGraphLayout';
import type { GitGraphLayout, LayoutNode, LayoutEdge, LaneLine } from '../lib/gitGraphLayout';
import type { GitGraphResponse } from '../lib/types';

interface GitHistoryDAGProps {
  workspaceId: string;
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
        <div className="empty-state__description">No commit history found for this workspace.</div>
      </div>
    );
  }

  const graphWidth = GRAPH_PADDING * 2 + layout.laneCount * LANE_WIDTH;
  const totalHeight = layout.nodes.length * layout.rowHeight;

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
            {/* Persistent lane lines (ISL-style column state) */}
            {layout.laneLines.map((ll, i) => (
              <LaneLinePath key={`lane-${i}`} laneLine={ll} rowHeight={layout.rowHeight} />
            ))}

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
            {layout.nodes.map((ln) => {
              if (ln.nodeType === 'label') {
                if (ln.hash === '__remote-main__') {
                  return (
                    <div key={ln.hash} className="git-dag__row" style={{ height: layout.rowHeight }}>
                      <span
                        className="git-dag__head-label"
                        style={{ borderColor: laneColorVar(0) }}
                      >
                        {ln.label}
                      </span>
                    </div>
                  );
                }
                if (ln.hash === '__view-changes__') {
                  return (
                    <div key={ln.hash} className="git-dag__row" style={{ height: layout.rowHeight }}>
                      <button
                        className="git-dag__view-changes"
                        onClick={() => navigate(`/diff/${workspaceId}`)}
                        title="View uncommitted changes"
                      >
                        {ln.label}
                      </button>
                    </div>
                  );
                }
                return (
                  <div key={ln.hash} className="git-dag__row" style={{ height: layout.rowHeight }}>
                    <span className="git-dag__you-are-here">{ln.label}</span>
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
              );
            })}
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

  // Filled circle for branch commits, open circle for main
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

function LaneLinePath({ laneLine, rowHeight }: { laneLine: LaneLine; rowHeight: number }) {
  const x = GRAPH_PADDING + laneLine.lane * LANE_WIDTH;
  const y1 = laneLine.fromY + rowHeight / 2;
  const y2 = laneLine.toY + rowHeight / 2;
  const color = laneColorVar(laneLine.lane);

  return <line x1={x} y1={y1} x2={x} y2={y2} stroke={color} strokeWidth={1.5} opacity={0.3} />;
}

function EdgePath({ edge, rowHeight }: { edge: LayoutEdge; rowHeight: number }) {
  const x1 = GRAPH_PADDING + edge.fromLane * LANE_WIDTH;
  const y1 = edge.fromY + rowHeight / 2;
  const x2 = GRAPH_PADDING + edge.toLane * LANE_WIDTH;
  const y2 = edge.toY + rowHeight / 2;

  // Use the parent's lane color for cross-lane edges (fork lines come FROM main)
  // This matches ISL convention: the fork line is colored like main
  const color = x1 === x2 ? laneColorVar(edge.fromLane) : laneColorVar(edge.toLane);

  if (x1 === x2) {
    // Straight vertical line
    return <line x1={x1} y1={y1} x2={x2} y2={y2} stroke={color} strokeWidth={1.5} />;
  }

  // Curved connector between lanes.
  // ISL convention: the curve goes from the fork point (bottom, lane 0)
  // diagonally up to the branch (top, lane 1).
  // Since edges go from child → parent (top → bottom), and the fork point
  // is the parent (bottom, lane 0) while the first branch commit is the child
  // (top, lane 1), we draw: start at child (x1,y1), curve down to parent (x2,y2).
  // S-curve: leave child vertically, curve through midpoint, arrive at parent vertically.
  const cp1Y = y1 + (y2 - y1) * 0.75;
  const cp2Y = y1 + (y2 - y1) * 0.25;
  const d = `M ${x1} ${y1} C ${x1} ${cp1Y}, ${x2} ${cp2Y}, ${x2} ${y2}`;
  return <path d={d} fill="none" stroke={color} strokeWidth={1.5} />;
}
