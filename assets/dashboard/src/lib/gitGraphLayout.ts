import type { GitGraphResponse, GitGraphNode } from './types';

export interface LayoutNode {
  hash: string;
  lane: number;
  y: number;
  node: GitGraphNode;
  nodeType: 'normal' | 'merge' | 'fork-point' | 'label';
  /** Label text for 'label' type nodes */
  label?: string;
}

export interface LayoutEdge {
  fromHash: string;
  toHash: string;
  fromLane: number;
  toLane: number;
  fromY: number;
  toY: number;
}

export interface LaneLine {
  lane: number;
  fromY: number;
  toY: number;
}

export interface GitGraphLayout {
  nodes: LayoutNode[];
  edges: LayoutEdge[];
  branchLanes: Record<string, number>;
  laneCount: number;
  rowHeight: number;
  /** Persistent vertical lines per lane (ISL-style column state) */
  laneLines: LaneLine[];
  /** The non-main branch name (the workspace's local branch) */
  localBranch: string | null;
}

const ROW_HEIGHT = 28;

/**
 * Compute a lane-based layout from the GitGraphResponse.
 * Nodes are expected in topological order (newest first).
 *
 * Lane assignment:
 * - Lane 0 (left): main/default branch — all nodes reachable from origin/main HEAD
 *   that are NOT exclusively on the local branch.
 * - Lane 1 (right): local branch — nodes reachable only from the local branch HEAD
 *   (i.e., the "ahead" commits).
 * - Fork point: stays in lane 0 (it's a shared ancestor on main).
 */
export function computeLayout(response: GitGraphResponse): GitGraphLayout {
  const { nodes, branches } = response;

  if (nodes.length === 0) {
    return { nodes: [], edges: [], branchLanes: {}, laneCount: 0, rowHeight: ROW_HEIGHT, laneLines: [], localBranch: null };
  }

  // Identify the main branch and local branch
  let mainBranch = 'main';
  let localBranch: string | null = null;
  for (const [name, info] of Object.entries(branches)) {
    if (info.is_main) {
      mainBranch = name;
    } else {
      localBranch = name;
    }
  }

  // If the local branch IS main (no divergence), there's only one lane
  if (!localBranch) {
    localBranch = mainBranch;
  }

  const branchLanes: Record<string, number> = {};
  branchLanes[mainBranch] = 0;
  if (localBranch !== mainBranch) {
    branchLanes[localBranch] = 1;
  }
  const laneCount = localBranch !== mainBranch ? 2 : 1;

  // Determine which lane each node belongs to.
  // Rule: if a node is on the local branch but NOT on main, it's lane 1.
  // Everything else (main-only, shared/fork-point) is lane 0.
  const nodeLane = (node: GitGraphNode): number => {
    const onMain = node.branches.includes(mainBranch);
    const onLocal = localBranch !== mainBranch && node.branches.includes(localBranch);

    if (onLocal && !onMain) {
      // Branch-only commit (ahead of main) → lane 1
      return 1;
    }
    // Main commit, shared commit, or fork point → lane 0
    return 0;
  };

  // Determine fork points: nodes that are on both branches
  const forkPointSet = new Set<string>();
  if (localBranch !== mainBranch) {
    for (const node of nodes) {
      if (node.branches.includes(mainBranch) && node.branches.includes(localBranch)) {
        forkPointSet.add(node.hash);
      }
    }
  }

  // Find the local branch HEAD hash
  const localHeadHash = localBranch !== mainBranch
    ? branches[localBranch]?.head ?? null
    : null;

  // Find the main branch HEAD hash
  const mainHeadHash = branches[mainBranch]?.head ?? null;

  // Build layout nodes, inserting label rows
  const layoutNodes: LayoutNode[] = [];
  let rowIndex = 0;
  const dirtyState = response.dirty_state;

  // Insert "remote/main" label at the top of lane 0
  if (mainHeadHash) {
    const remoteMainNode: GitGraphNode = {
      hash: '__remote-main__',
      short_hash: '',
      message: '',
      author: '',
      timestamp: '',
      parents: [],
      branches: [],
      is_head: [],
      workspace_ids: [],
    };
    layoutNodes.push({
      hash: '__remote-main__',
      lane: 0,
      y: rowIndex * ROW_HEIGHT,
      node: remoteMainNode,
      nodeType: 'label',
      label: mainBranch,
    });
    rowIndex++;
  }

  for (const node of nodes) {
    // Insert label rows before the local branch HEAD
    if (localHeadHash && node.hash === localHeadHash) {
      // "You are here" row
      const labelNode: GitGraphNode = {
        hash: '__you-are-here__',
        short_hash: '',
        message: '',
        author: '',
        timestamp: '',
        parents: [],
        branches: [],
        is_head: [],
        workspace_ids: [],
      };
      layoutNodes.push({
        hash: '__you-are-here__',
        lane: 1,
        y: rowIndex * ROW_HEIGHT,
        node: labelNode,
        nodeType: 'label',
        label: 'You are here',
      });
      rowIndex++;

      // "View changes" row (only if dirty)
      if (dirtyState && dirtyState.files_changed > 0) {
        const viewChangesNode: GitGraphNode = {
          hash: '__view-changes__',
          short_hash: '',
          message: '',
          author: '',
          timestamp: '',
          parents: [],
          branches: [],
          is_head: [],
          workspace_ids: [],
        };
        const f = dirtyState.files_changed;
        const label = `${f} file${f !== 1 ? 's' : ''}, +${dirtyState.lines_added} −${dirtyState.lines_removed} lines`;
        layoutNodes.push({
          hash: '__view-changes__',
          lane: 1,
          y: rowIndex * ROW_HEIGHT,
          node: viewChangesNode,
          nodeType: 'label',
          label,
        });
        rowIndex++;
      }
    }

    const lane = nodeLane(node);
    const isMerge = node.parents.length >= 2;
    const isForkPoint = forkPointSet.has(node.hash);

    layoutNodes.push({
      hash: node.hash,
      lane,
      y: rowIndex * ROW_HEIGHT,
      node,
      nodeType: isMerge ? 'merge' : isForkPoint ? 'fork-point' : 'normal',
    });
    rowIndex++;
  }

  // Build layout node lookup
  const layoutByHash = new Map<string, LayoutNode>();
  for (const ln of layoutNodes) {
    layoutByHash.set(ln.hash, ln);
  }

  // Build edges from each node to its parents
  const edges: LayoutEdge[] = [];

  // Synthetic edge: remote-main label → main HEAD commit
  if (mainHeadHash) {
    const remoteMainLn = layoutByHash.get('__remote-main__');
    const mainHeadLn = layoutByHash.get(mainHeadHash);
    if (remoteMainLn && mainHeadLn) {
      edges.push({
        fromHash: '__remote-main__',
        toHash: mainHeadHash,
        fromLane: remoteMainLn.lane,
        toLane: mainHeadLn.lane,
        fromY: remoteMainLn.y,
        toY: mainHeadLn.y,
      });
    }
  }

  // Synthetic edges: you-are-here → [view-changes →] HEAD commit
  if (localHeadHash) {
    const labelLn = layoutByHash.get('__you-are-here__');
    const viewChangesLn = layoutByHash.get('__view-changes__');
    const headLn = layoutByHash.get(localHeadHash);

    if (viewChangesLn && labelLn) {
      // you-are-here → view-changes → HEAD
      edges.push({
        fromHash: '__you-are-here__',
        toHash: '__view-changes__',
        fromLane: labelLn.lane,
        toLane: viewChangesLn.lane,
        fromY: labelLn.y,
        toY: viewChangesLn.y,
      });
      if (headLn) {
        edges.push({
          fromHash: '__view-changes__',
          toHash: localHeadHash,
          fromLane: viewChangesLn.lane,
          toLane: headLn.lane,
          fromY: viewChangesLn.y,
          toY: headLn.y,
        });
      }
    } else if (labelLn && headLn) {
      // No dirty state: you-are-here → HEAD
      edges.push({
        fromHash: '__you-are-here__',
        toHash: localHeadHash,
        fromLane: labelLn.lane,
        toLane: headLn.lane,
        fromY: labelLn.y,
        toY: headLn.y,
      });
    }
  }

  for (const ln of layoutNodes) {
    if (ln.nodeType === 'label') continue;
    for (const parentHash of ln.node.parents) {
      const parentLn = layoutByHash.get(parentHash);
      if (parentLn) {
        edges.push({
          fromHash: ln.hash,
          toHash: parentHash,
          fromLane: ln.lane,
          toLane: parentLn.lane,
          fromY: ln.y,
          toY: parentLn.y,
        });
      }
    }
  }

  // Compute persistent lane lines (ISL-style column state).
  // Each lane's line spans from its topmost node to its bottommost node.
  // If two lanes exist and lane 0's top is below lane 1's top, extend lane 0
  // upward to match — this keeps the main line visible alongside branch commits.
  const laneExtents = new Map<number, { minY: number; maxY: number }>();
  for (const ln of layoutNodes) {
    const ext = laneExtents.get(ln.lane);
    const cy = ln.y;
    if (ext) {
      ext.minY = Math.min(ext.minY, cy);
      ext.maxY = Math.max(ext.maxY, cy);
    } else {
      laneExtents.set(ln.lane, { minY: cy, maxY: cy });
    }
  }

  // Extend lane 0 upward to the top of the graph if lane 1 starts higher
  if (laneCount === 2) {
    const lane0 = laneExtents.get(0);
    const lane1 = laneExtents.get(1);
    if (lane0 && lane1 && lane1.minY < lane0.minY) {
      lane0.minY = lane1.minY;
    }
  }

  const laneLines: LaneLine[] = [];
  for (const [lane, ext] of laneExtents) {
    laneLines.push({ lane, fromY: ext.minY, toY: ext.maxY });
  }

  return {
    nodes: layoutNodes,
    edges,
    branchLanes,
    laneCount,
    rowHeight: ROW_HEIGHT,
    laneLines,
    localBranch: localBranch !== mainBranch ? localBranch : null,
  };
}

/**
 * Returns a CSS variable name for a lane's color.
 * Lane 0 (main) uses muted text color; others cycle through 8 lane colors.
 */
export function laneColorVar(lane: number): string {
  if (lane === 0) return 'var(--color-text-muted)';
  const index = ((lane - 1) % 8) + 1;
  return `var(--color-graph-lane-${index})`;
}
