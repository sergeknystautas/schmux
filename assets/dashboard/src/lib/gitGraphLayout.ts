import type { GitGraphResponse, GitGraphNode } from './types';

export interface LayoutNode {
  hash: string;
  column: number;
  y: number;
  node: GitGraphNode;
  nodeType: 'commit' | 'you-are-here' | 'view-changes' | 'sync-summary';
  /** Dirty working copy state (only on view-changes nodes) */
  dirtyState?: { filesChanged: number; linesAdded: number; linesRemoved: number };
  /** Sync summary metadata (only on sync-summary nodes) */
  syncSummary?: { count: number; newestTimestamp: string };
}

export interface LayoutEdge {
  fromHash: string;
  toHash: string;
  fromColumn: number;
  toColumn: number;
  fromY: number;
  toY: number;
}

export interface LaneLine {
  column: number;
  fromY: number;
  toY: number;
}

export interface GitGraphLayout {
  nodes: LayoutNode[];
  edges: LayoutEdge[];
  columnCount: number;
  rowHeight: number;
  laneLines: LaneLine[];
  localBranch: string | null;
  /** Column index of the you-are-here node, if present */
  youAreHereColumn: number | null;
}

const ROW_HEIGHT = 28;

/**
 * Compute a column-based layout from the GitGraphResponse.
 *
 * Column assignment is data-driven from branch info (not hardcoded):
 * - Column 0: main/default branch
 * - Column 1+: each additional branch gets the next column
 * - Nodes on a non-main branch exclusively go to that branch's column
 * - Shared nodes (fork points, main-only) stay in column 0
 *
 * One virtual node (you-are-here) represents the working directory,
 * following ISL's pattern of a single virtual working-copy commit.
 * Branch labels are rendered as badges on commit rows via is_head data.
 */
export function computeLayout(response: GitGraphResponse): GitGraphLayout {
  const { nodes, branches } = response;

  if (nodes.length === 0) {
    return {
      nodes: [],
      edges: [],
      columnCount: 0,
      rowHeight: ROW_HEIGHT,
      laneLines: [],
      localBranch: null,
      youAreHereColumn: null,
    };
  }

  // Identify branches
  let mainBranch = 'main';
  let localBranch: string | null = null;
  for (const [name, info] of Object.entries(branches)) {
    if (info.is_main) mainBranch = name;
    else localBranch = name;
  }
  if (!localBranch) localBranch = mainBranch;

  // Build column map: main gets column 0, each additional branch gets the next
  const branchColumns = new Map<string, number>();
  branchColumns.set(mainBranch, 0);
  let nextCol = 1;
  for (const name of Object.keys(branches)) {
    if (!branchColumns.has(name)) {
      branchColumns.set(name, nextCol++);
    }
  }
  const columnCount = localBranch !== mainBranch ? nextCol : 1;

  // Column assignment: nodes on a non-main branch exclusively → that branch's column
  const nodeColumn = (node: GitGraphNode): number => {
    const onMain = node.branches.includes(mainBranch);
    for (const branchName of node.branches) {
      if (branchName !== mainBranch && branchColumns.has(branchName) && !onMain) {
        return branchColumns.get(branchName)!;
      }
    }
    return 0;
  };

  // HEAD hashes
  const mainHeadHash = branches[mainBranch]?.head ?? null;
  const localHeadHash = localBranch !== mainBranch ? (branches[localBranch]?.head ?? null) : null;
  const workingCopyParent = localHeadHash ?? mainHeadHash;
  const workingCopyColumn = localBranch !== mainBranch ? (branchColumns.get(localBranch) ?? 1) : 0;

  // Identify main-ahead commits (on main exclusively, not on local branch).
  // These get collapsed into a single "Sync" summary row per ISL pattern §10.
  const mainAheadHashes = new Set<string>();
  if (localBranch !== mainBranch) {
    for (const node of nodes) {
      const onMain = node.branches.includes(mainBranch);
      const onLocal = node.branches.includes(localBranch);
      if (onMain && !onLocal) {
        mainAheadHashes.add(node.hash);
      }
    }
  }

  // Find the newest main-ahead commit timestamp for the sync summary.
  // Compare using parsed Date objects, not strings (ISO 8601 strings aren't
  // guaranteed to be sortable when timezone offsets differ).
  let newestMainAheadTimestamp = '';
  let newestMainAheadTime = 0;
  let mainAheadCount = mainAheadHashes.size;
  if (mainAheadCount > 0) {
    for (const node of nodes) {
      if (mainAheadHashes.has(node.hash)) {
        const t = new Date(node.timestamp).getTime();
        if (t > newestMainAheadTime) {
          newestMainAheadTime = t;
          newestMainAheadTimestamp = node.timestamp;
        }
      }
    }
  }

  // Build layout nodes
  const layoutNodes: LayoutNode[] = [];
  let rowIndex = 0;
  const dirtyState = response.dirty_state;
  let youAreHereColumn: number | null = null;
  let syncSummaryInserted = false;

  // Commit nodes, with virtual nodes inserted at appropriate positions.
  for (const node of nodes) {
    // Skip individual main-ahead commits — they're collapsed into the sync summary.
    if (mainAheadHashes.has(node.hash)) {
      // Insert sync summary row on first encounter (it appears above the draft stack).
      if (!syncSummaryInserted) {
        syncSummaryInserted = true;
        layoutNodes.push({
          hash: '__sync-summary__',
          column: 0,
          y: rowIndex * ROW_HEIGHT,
          node,
          nodeType: 'sync-summary',
          syncSummary: { count: mainAheadCount, newestTimestamp: newestMainAheadTimestamp },
        });
        rowIndex++;
      }
      continue;
    }

    // Insert virtual nodes right before the working copy parent
    if (workingCopyParent && node.hash === workingCopyParent) {
      youAreHereColumn = workingCopyColumn;

      layoutNodes.push({
        hash: '__you-are-here__',
        column: workingCopyColumn,
        y: rowIndex * ROW_HEIGHT,
        node,
        nodeType: 'you-are-here',
      });
      rowIndex++;

      if (dirtyState && dirtyState.files_changed > 0) {
        layoutNodes.push({
          hash: '__view-changes__',
          column: workingCopyColumn,
          y: rowIndex * ROW_HEIGHT,
          node,
          nodeType: 'view-changes',
          dirtyState: {
            filesChanged: dirtyState.files_changed,
            linesAdded: dirtyState.lines_added,
            linesRemoved: dirtyState.lines_removed,
          },
        });
        rowIndex++;
      }
    }

    layoutNodes.push({
      hash: node.hash,
      column: nodeColumn(node),
      y: rowIndex * ROW_HEIGHT,
      node,
      nodeType: 'commit',
    });
    rowIndex++;
  }

  // Node lookup
  const nodeByHash = new Map<string, LayoutNode>();
  for (const ln of layoutNodes) {
    nodeByHash.set(ln.hash, ln);
  }

  // Build edges
  const edges: LayoutEdge[] = [];

  // you-are-here → [view-changes →] HEAD commit
  if (workingCopyParent) {
    const yahNode = nodeByHash.get('__you-are-here__');
    const vcNode = nodeByHash.get('__view-changes__');
    const headNode = nodeByHash.get(workingCopyParent);

    if (vcNode && yahNode) {
      edges.push({
        fromHash: '__you-are-here__',
        toHash: '__view-changes__',
        fromColumn: yahNode.column,
        toColumn: vcNode.column,
        fromY: yahNode.y,
        toY: vcNode.y,
      });
      if (headNode) {
        edges.push({
          fromHash: '__view-changes__',
          toHash: workingCopyParent,
          fromColumn: vcNode.column,
          toColumn: headNode.column,
          fromY: vcNode.y,
          toY: headNode.y,
        });
      }
    } else if (yahNode && headNode) {
      edges.push({
        fromHash: '__you-are-here__',
        toHash: workingCopyParent,
        fromColumn: yahNode.column,
        toColumn: headNode.column,
        fromY: yahNode.y,
        toY: headNode.y,
      });
    }
  }

  // Commit → parent edges
  for (const ln of layoutNodes) {
    if (ln.nodeType !== 'commit') continue;
    for (const parentHash of ln.node.parents) {
      const parentNode = nodeByHash.get(parentHash);
      if (parentNode) {
        edges.push({
          fromHash: ln.hash,
          toHash: parentHash,
          fromColumn: ln.column,
          toColumn: parentNode.column,
          fromY: ln.y,
          toY: parentNode.y,
        });
      }
    }
  }

  // No solid edge from the sync summary node. The column 0 dashed lane line
  // (ISL column-reservation pattern) provides visual continuity. A solid edge
  // would imply a parent/child relationship that doesn't exist.

  // Compute persistent lane lines (ISL column-reservation pattern).
  // Each column's line spans from its topmost to bottommost node.
  // Column 0 (main) always extends to the top of the graph — it's "reserved"
  // even where no main commit exists, so the main line runs alongside branch commits.
  const columnExtents = new Map<number, { minY: number; maxY: number }>();
  for (const ln of layoutNodes) {
    const ext = columnExtents.get(ln.column);
    if (ext) {
      ext.minY = Math.min(ext.minY, ln.y);
      ext.maxY = Math.max(ext.maxY, ln.y);
    } else {
      columnExtents.set(ln.column, { minY: ln.y, maxY: ln.y });
    }
  }

  // Reserve column 0 to the top of the graph
  const topY = layoutNodes.length > 0 ? layoutNodes[0].y : 0;
  const col0 = columnExtents.get(0);
  if (col0) {
    col0.minY = Math.min(col0.minY, topY);
  } else if (columnCount > 1) {
    // Column 0 has no nodes but we have multiple columns — create extent from top to bottom
    const bottomY = layoutNodes.length > 0 ? layoutNodes[layoutNodes.length - 1].y : 0;
    columnExtents.set(0, { minY: topY, maxY: bottomY });
  }

  const laneLines: LaneLine[] = [];
  for (const [col, ext] of columnExtents) {
    if (ext.minY !== ext.maxY) {
      laneLines.push({ column: col, fromY: ext.minY, toY: ext.maxY });
    }
  }

  return {
    nodes: layoutNodes,
    edges,
    columnCount,
    rowHeight: ROW_HEIGHT,
    laneLines,
    localBranch: localBranch !== mainBranch ? localBranch : null,
    youAreHereColumn,
  };
}

/** Graph foreground color (ISL-style: single color for all lines/nodes) */
export const GRAPH_COLOR = 'var(--color-text-muted)';
/** Highlight color for the working-copy column */
export const HIGHLIGHT_COLOR = 'var(--color-graph-lane-1)';
