import type { GitGraphResponse, GitGraphNode } from './types';

export interface LayoutNode {
  hash: string;
  lane: number;
  y: number;
  node: GitGraphNode;
  /** 'normal' | 'merge' | 'fork-point' */
  nodeType: 'normal' | 'merge' | 'fork-point';
}

export interface LayoutEdge {
  fromHash: string;
  toHash: string;
  fromLane: number;
  toLane: number;
  fromY: number;
  toY: number;
}

export interface GitGraphLayout {
  nodes: LayoutNode[];
  edges: LayoutEdge[];
  branchLanes: Record<string, number>;
  laneCount: number;
  rowHeight: number;
}

const ROW_HEIGHT = 28;

/**
 * Compute a lane-based layout from the GitGraphResponse.
 * Nodes are expected in topological order (newest first).
 * Main branch occupies lane 0.
 */
export function computeLayout(response: GitGraphResponse): GitGraphLayout {
  const { nodes, branches } = response;

  if (nodes.length === 0) {
    return { nodes: [], edges: [], branchLanes: {}, laneCount: 0, rowHeight: ROW_HEIGHT };
  }

  // Identify the main branch
  let mainBranch = 'main';
  for (const [name, info] of Object.entries(branches)) {
    if (info.is_main) {
      mainBranch = name;
      break;
    }
  }

  // Assign lanes to branches: main = 0, others get 1, 2, 3...
  const branchLanes: Record<string, number> = {};
  branchLanes[mainBranch] = 0;
  let nextLane = 1;

  // Sort non-main branches by their first appearance in the node list
  const branchOrder: string[] = [];
  const branchSeen = new Set<string>();
  branchSeen.add(mainBranch);
  for (const node of nodes) {
    for (const b of node.branches) {
      if (!branchSeen.has(b)) {
        branchSeen.add(b);
        branchOrder.push(b);
      }
    }
  }
  for (const b of branchOrder) {
    branchLanes[b] = nextLane++;
  }
  // Assign any remaining branches from the branches map
  for (const b of Object.keys(branches)) {
    if (!(b in branchLanes)) {
      branchLanes[b] = nextLane++;
    }
  }

  const laneCount = nextLane;

  // Build hash → index map
  const hashIndex = new Map<string, number>();
  for (let i = 0; i < nodes.length; i++) {
    hashIndex.set(nodes[i].hash, i);
  }

  // Determine fork points: commits that are parents of nodes on different branches
  const forkPointSet = new Set<string>();
  for (const node of nodes) {
    if (node.parents.length >= 2) {
      // Merge commit — second parent's lane commit could be a fork point elsewhere
    }
    // A node is a fork point if it has children on multiple different lanes
  }
  // Simpler approach: a node is a fork point if it's in multiple branches and is a parent
  // of a node in a branch it doesn't share a lane with
  for (const node of nodes) {
    if (node.branches.length > 1) {
      const lanes = new Set(node.branches.map(b => branchLanes[b]));
      if (lanes.size > 1) {
        forkPointSet.add(node.hash);
      }
    }
  }

  // Compute node lane assignment: use the lowest-numbered branch's lane
  const nodeLane = (node: GitGraphNode): number => {
    if (node.branches.length === 0) return 0;
    let minLane = Infinity;
    for (const b of node.branches) {
      const lane = branchLanes[b] ?? 0;
      if (lane < minLane) minLane = lane;
    }
    // For non-main branches, prefer the non-main lane if this is a HEAD
    if (node.is_head.length > 0) {
      for (const b of node.is_head) {
        if (b !== mainBranch) {
          return branchLanes[b] ?? minLane;
        }
      }
    }
    // For nodes that belong to a non-main branch, prefer that lane
    for (const b of node.branches) {
      if (b !== mainBranch) {
        return branchLanes[b] ?? minLane;
      }
    }
    return minLane;
  };

  // Build layout nodes
  const layoutNodes: LayoutNode[] = nodes.map((node, i) => {
    const lane = nodeLane(node);
    const isMerge = node.parents.length >= 2;
    const isForkPoint = forkPointSet.has(node.hash);

    return {
      hash: node.hash,
      lane,
      y: i * ROW_HEIGHT,
      node,
      nodeType: isMerge ? 'merge' : isForkPoint ? 'fork-point' : 'normal',
    };
  });

  // Build layout node lookup
  const layoutByHash = new Map<string, LayoutNode>();
  for (const ln of layoutNodes) {
    layoutByHash.set(ln.hash, ln);
  }

  // Build edges from each node to its parents
  const edges: LayoutEdge[] = [];
  for (const ln of layoutNodes) {
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

  return {
    nodes: layoutNodes,
    edges,
    branchLanes,
    laneCount,
    rowHeight: ROW_HEIGHT,
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
