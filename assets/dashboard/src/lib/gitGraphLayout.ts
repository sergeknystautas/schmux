import type { GitGraphResponse, GitGraphNode, FileDiff } from './types';

export interface LayoutNode {
  hash: string;
  column: number;
  y: number;
  node: GitGraphNode;
  nodeType:
    | 'commit'
    | 'you-are-here'
    | 'commit-actions'
    | 'commit-file'
    | 'commit-footer'
    | 'sync-summary';
  /** Dirty working copy state (only on commit-actions nodes) */
  dirtyState?: {
    files_changed: number;
    lines_added: number;
    lines_removed: number;
  };
  /** File info (only on commit-file nodes) */
  file?: FileDiff;
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

export const ROW_HEIGHT = 28;

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
export function computeLayout(response: GitGraphResponse, files: FileDiff[] = []): GitGraphLayout {
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

  // Main-ahead count comes from the API response (commits on main ahead of HEAD).
  // These are not included in the nodes array - they're just counted.
  const mainAheadCount = response.main_ahead_count ?? 0;

  // Build layout nodes
  const layoutNodes: LayoutNode[] = [];
  let rowIndex = 0;
  const dirtyState = response.dirty_state;
  let youAreHereColumn: number | null = null;
  let syncSummaryInserted = false;

  // Commit nodes, with virtual nodes inserted at appropriate positions.
  for (const node of nodes) {
    // Insert sync summary at the top if there are main-ahead commits.
    // This appears above the local commits.
    if (!syncSummaryInserted && mainAheadCount > 0 && localBranch !== mainBranch) {
      syncSummaryInserted = true;
      // Use the first node as a reference for the sync summary's dummy node
      const refNode = nodes[0];
      layoutNodes.push({
        hash: '__sync-summary__',
        column: 0,
        y: rowIndex * ROW_HEIGHT,
        node: refNode,
        nodeType: 'sync-summary',
        syncSummary: { count: mainAheadCount, newestTimestamp: '' },
      });
      rowIndex++;
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

      // Insert commit workflow rows: actions, files, footer
      if (files.length > 0) {
        // Actions row (Select All, Deselect All, Discard)
        layoutNodes.push({
          hash: '__commit-actions__',
          column: workingCopyColumn,
          y: rowIndex * ROW_HEIGHT,
          node,
          nodeType: 'commit-actions',
          dirtyState: dirtyState
            ? {
                files_changed: dirtyState.files_changed,
                lines_added: dirtyState.lines_added,
                lines_removed: dirtyState.lines_removed,
              }
            : undefined,
        });
        rowIndex++;

        // One row per file
        for (let i = 0; i < files.length; i++) {
          layoutNodes.push({
            hash: `__commit-file-${i}__`,
            column: workingCopyColumn,
            y: rowIndex * ROW_HEIGHT,
            node,
            nodeType: 'commit-file',
            file: files[i],
          });
          rowIndex++;
        }

        // Footer row (Commit, Amend buttons)
        layoutNodes.push({
          hash: '__commit-footer__',
          column: workingCopyColumn,
          y: rowIndex * ROW_HEIGHT,
          node,
          nodeType: 'commit-footer',
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

  // you-are-here → [commit workflow rows →] HEAD commit
  if (workingCopyParent) {
    const yahNode = nodeByHash.get('__you-are-here__');
    const footerNode = nodeByHash.get('__commit-footer__');
    const headNode = nodeByHash.get(workingCopyParent);

    if (footerNode && yahNode) {
      // Edge from you-are-here to commit-actions (first workflow row)
      const actionsNode = nodeByHash.get('__commit-actions__');
      if (actionsNode) {
        edges.push({
          fromHash: '__you-are-here__',
          toHash: '__commit-actions__',
          fromColumn: yahNode.column,
          toColumn: actionsNode.column,
          fromY: yahNode.y,
          toY: actionsNode.y,
        });
        // Edge from commit-actions to commit-footer (solid line through files)
        edges.push({
          fromHash: '__commit-actions__',
          toHash: '__commit-footer__',
          fromColumn: actionsNode.column,
          toColumn: footerNode.column,
          fromY: actionsNode.y,
          toY: footerNode.y,
        });
      }
      // Edge from commit-footer to HEAD commit
      if (headNode) {
        edges.push({
          fromHash: '__commit-footer__',
          toHash: workingCopyParent,
          fromColumn: footerNode.column,
          toColumn: headNode.column,
          fromY: footerNode.y,
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
