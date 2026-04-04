import { describe, it, expect } from 'vitest';
import { computeLayout, ROW_HEIGHT } from './gitGraphLayout';
import type { CommitGraphResponse, FileDiff } from './types';

function makeNode(
  hash: string,
  branches: string[],
  parents: string[] = [],
  isHead: string[] = []
): CommitGraphResponse['nodes'][0] {
  return {
    hash,
    short_hash: hash.slice(0, 7),
    message: `commit ${hash}`,
    author: 'test',
    timestamp: '2024-01-01T00:00:00Z',
    parents,
    branches,
    is_head: isHead,
    workspace_ids: [],
  };
}

function makeResponse(
  nodes: CommitGraphResponse['nodes'],
  branches: CommitGraphResponse['branches'],
  overrides: Partial<CommitGraphResponse> = {}
): CommitGraphResponse {
  return { repo: 'test-repo', nodes, branches, main_ahead_count: 0, ...overrides };
}

describe('computeLayout', () => {
  it('returns empty layout for empty nodes', () => {
    const layout = computeLayout(makeResponse([], {}));
    expect(layout.nodes).toEqual([]);
    expect(layout.edges).toEqual([]);
    expect(layout.columnCount).toBe(0);
    expect(layout.laneLines).toEqual([]);
    expect(layout.localBranch).toBeNull();
    expect(layout.youAreHereColumn).toBeNull();
  });

  it('lays out single-branch (main only) with column 0', () => {
    const nodes = [makeNode('aaa', ['main'], [], ['main']), makeNode('bbb', ['main'], ['aaa'])];
    const branches = {
      main: { head: 'aaa', is_main: true, workspace_ids: [] },
    };
    const layout = computeLayout(makeResponse(nodes, branches));

    // you-are-here inserted before HEAD
    const commitNodes = layout.nodes.filter((n) => n.nodeType === 'commit');
    expect(commitNodes).toHaveLength(2);
    expect(commitNodes.every((n) => n.column === 0)).toBe(true);
    expect(layout.columnCount).toBe(1);
    expect(layout.localBranch).toBeNull();
  });

  it('assigns feature branch to column 1', () => {
    const nodes = [
      makeNode('feat1', ['feature'], [], ['feature']),
      makeNode('shared', ['main', 'feature'], ['feat1'], ['main']),
    ];
    const branches = {
      main: { head: 'shared', is_main: true, workspace_ids: [] },
      feature: { head: 'feat1', is_main: false, workspace_ids: [] },
    };
    const layout = computeLayout(makeResponse(nodes, branches));

    // feat1 is exclusively on feature branch → column 1
    const feat1Node = layout.nodes.find((n) => n.hash === 'feat1');
    expect(feat1Node?.column).toBe(1);

    // shared is on both → column 0 (main takes priority)
    const sharedNode = layout.nodes.find((n) => n.hash === 'shared');
    expect(sharedNode?.column).toBe(0);

    expect(layout.columnCount).toBe(2);
    expect(layout.localBranch).toBe('feature');
  });

  it('inserts you-are-here node before HEAD commit', () => {
    const nodes = [makeNode('head1', ['main'], [], ['main'])];
    const branches = { main: { head: 'head1', is_main: true, workspace_ids: [] } };
    const layout = computeLayout(makeResponse(nodes, branches));

    const yahNode = layout.nodes.find((n) => n.nodeType === 'you-are-here');
    expect(yahNode).toBeDefined();
    expect(yahNode!.hash).toBe('__you-are-here__');

    const headNode = layout.nodes.find((n) => n.hash === 'head1');
    expect(yahNode!.y).toBeLessThan(headNode!.y);
    expect(layout.youAreHereColumn).toBe(0);
  });

  it('inserts commit workflow nodes when files are provided', () => {
    const nodes = [makeNode('head1', ['main'], [], ['main'])];
    const branches = { main: { head: 'head1', is_main: true, workspace_ids: [] } };
    const files: FileDiff[] = [
      { lines_added: 5, lines_removed: 2, is_binary: false, new_path: 'file1.ts' },
      { lines_added: 3, lines_removed: 0, is_binary: false, new_path: 'file2.ts' },
    ];
    const layout = computeLayout(makeResponse(nodes, branches), files);

    const nodeTypes = layout.nodes.map((n) => n.nodeType);
    expect(nodeTypes).toContain('you-are-here');
    expect(nodeTypes).toContain('commit-actions');
    expect(nodeTypes).toContain('commit-file');
    expect(nodeTypes).toContain('commit-footer');

    const fileNodes = layout.nodes.filter((n) => n.nodeType === 'commit-file');
    expect(fileNodes).toHaveLength(2);
    expect(fileNodes[0].file?.new_path).toBe('file1.ts');
    expect(fileNodes[1].file?.new_path).toBe('file2.ts');
  });

  it('creates edge from you-are-here to HEAD when no files', () => {
    const nodes = [makeNode('head1', ['main'], [], ['main'])];
    const branches = { main: { head: 'head1', is_main: true, workspace_ids: [] } };
    const layout = computeLayout(makeResponse(nodes, branches));

    const yahToHead = layout.edges.find(
      (e) => e.fromHash === '__you-are-here__' && e.toHash === 'head1'
    );
    expect(yahToHead).toBeDefined();
  });

  it('creates edges through commit workflow when files present', () => {
    const nodes = [makeNode('head1', ['main'], [], ['main'])];
    const branches = { main: { head: 'head1', is_main: true, workspace_ids: [] } };
    const files: FileDiff[] = [{ lines_added: 1, lines_removed: 0, is_binary: false }];
    const layout = computeLayout(makeResponse(nodes, branches), files);

    // Should have edges: yah→actions, actions→footer, footer→head
    const yahToActions = layout.edges.find(
      (e) => e.fromHash === '__you-are-here__' && e.toHash === '__commit-actions__'
    );
    const actionsToFooter = layout.edges.find(
      (e) => e.fromHash === '__commit-actions__' && e.toHash === '__commit-footer__'
    );
    const footerToHead = layout.edges.find(
      (e) => e.fromHash === '__commit-footer__' && e.toHash === 'head1'
    );

    expect(yahToActions).toBeDefined();
    expect(actionsToFooter).toBeDefined();
    expect(footerToHead).toBeDefined();
  });

  it('creates commit-to-parent edges', () => {
    const nodes = [
      makeNode('child', ['main'], ['parent'], ['main']),
      makeNode('parent', ['main'], []),
    ];
    const branches = { main: { head: 'child', is_main: true, workspace_ids: [] } };
    const layout = computeLayout(makeResponse(nodes, branches));

    const edge = layout.edges.find((e) => e.fromHash === 'child' && e.toHash === 'parent');
    expect(edge).toBeDefined();
  });

  it('computes lane lines spanning topmost to bottommost nodes per column', () => {
    const nodes = [
      makeNode('feat1', ['feature'], [], ['feature']),
      makeNode('feat2', ['feature'], ['feat1']),
      makeNode('shared', ['main', 'feature'], ['feat2'], ['main']),
    ];
    const branches = {
      main: { head: 'shared', is_main: true, workspace_ids: [] },
      feature: { head: 'feat1', is_main: false, workspace_ids: [] },
    };
    const layout = computeLayout(makeResponse(nodes, branches));

    // Feature branch column should have a lane line
    const featureLane = layout.laneLines.find((l) => l.column === 1);
    expect(featureLane).toBeDefined();
    expect(featureLane!.fromY).toBeLessThan(featureLane!.toY);
  });

  it('reserves column 0 to top of graph when multi-column', () => {
    // Feature branch commits only — column 0 should still have a lane
    const nodes = [
      makeNode('feat1', ['feature'], [], ['feature']),
      makeNode('shared', ['main', 'feature'], ['feat1'], ['main']),
    ];
    const branches = {
      main: { head: 'shared', is_main: true, workspace_ids: [] },
      feature: { head: 'feat1', is_main: false, workspace_ids: [] },
    };
    const layout = computeLayout(makeResponse(nodes, branches));
    const col0Lane = layout.laneLines.find((l) => l.column === 0);
    expect(col0Lane).toBeDefined();
    // Column 0 lane should start at the top of the graph
    expect(col0Lane!.fromY).toBe(layout.nodes[0].y);
  });

  it('inserts sync summary node when main_ahead_count > 0 and on feature branch', () => {
    const nodes = [
      makeNode('feat1', ['feature'], [], ['feature']),
      makeNode('shared', ['main', 'feature'], ['feat1'], ['main']),
    ];
    const branches = {
      main: { head: 'shared', is_main: true, workspace_ids: [] },
      feature: { head: 'feat1', is_main: false, workspace_ids: [] },
    };
    const layout = computeLayout(makeResponse(nodes, branches, { main_ahead_count: 3 }));

    const syncNode = layout.nodes.find((n) => n.nodeType === 'sync-summary');
    expect(syncNode).toBeDefined();
    expect(syncNode!.hash).toBe('__sync-summary__');
    expect(syncNode!.column).toBe(0);
    expect(syncNode!.syncSummary?.count).toBe(3);
    // Sync summary should be the first node
    expect(layout.nodes[0]).toBe(syncNode);
  });

  it('does not insert sync summary when on main branch', () => {
    const nodes = [makeNode('head1', ['main'], [], ['main'])];
    const branches = { main: { head: 'head1', is_main: true, workspace_ids: [] } };
    const layout = computeLayout(makeResponse(nodes, branches, { main_ahead_count: 5 }));
    const syncNode = layout.nodes.find((n) => n.nodeType === 'sync-summary');
    expect(syncNode).toBeUndefined();
  });

  it('localBranch returns null when on main only', () => {
    const nodes = [makeNode('aaa', ['main'], [], ['main'])];
    const branches = { main: { head: 'aaa', is_main: true, workspace_ids: [] } };
    const layout = computeLayout(makeResponse(nodes, branches));
    expect(layout.localBranch).toBeNull();
  });

  it('localBranch returns the feature branch name', () => {
    const nodes = [
      makeNode('feat1', ['feature'], [], ['feature']),
      makeNode('shared', ['main', 'feature'], ['feat1'], ['main']),
    ];
    const branches = {
      main: { head: 'shared', is_main: true, workspace_ids: [] },
      feature: { head: 'feat1', is_main: false, workspace_ids: [] },
    };
    const layout = computeLayout(makeResponse(nodes, branches));
    expect(layout.localBranch).toBe('feature');
  });

  it('youAreHereColumn tracks the working copy column on feature branch', () => {
    const nodes = [
      makeNode('feat1', ['feature'], [], ['feature']),
      makeNode('shared', ['main', 'feature'], ['feat1'], ['main']),
    ];
    const branches = {
      main: { head: 'shared', is_main: true, workspace_ids: [] },
      feature: { head: 'feat1', is_main: false, workspace_ids: [] },
    };
    const layout = computeLayout(makeResponse(nodes, branches));
    expect(layout.youAreHereColumn).toBe(1);
  });

  it('rowHeight is always ROW_HEIGHT constant', () => {
    const nodes = [makeNode('aaa', ['main'], [], ['main'])];
    const branches = { main: { head: 'aaa', is_main: true, workspace_ids: [] } };
    const layout = computeLayout(makeResponse(nodes, branches));
    expect(layout.rowHeight).toBe(ROW_HEIGHT);
  });

  it('includes dirty state on commit-actions node', () => {
    const nodes = [makeNode('head1', ['main'], [], ['main'])];
    const branches = { main: { head: 'head1', is_main: true, workspace_ids: [] } };
    const files: FileDiff[] = [{ lines_added: 1, lines_removed: 0, is_binary: false }];
    const dirtyState = { files_changed: 3, lines_added: 10, lines_removed: 5 };
    const layout = computeLayout(makeResponse(nodes, branches, { dirty_state: dirtyState }), files);

    const actionsNode = layout.nodes.find((n) => n.nodeType === 'commit-actions');
    expect(actionsNode?.dirtyState).toEqual(dirtyState);
  });

  it('passes main_ahead_newest_timestamp to sync summary node', () => {
    const nodes = [
      makeNode('feat1', ['feature'], [], ['feature']),
      makeNode('shared', ['main', 'feature'], ['feat1'], ['main']),
    ];
    const branches = {
      main: { head: 'shared', is_main: true, workspace_ids: [] },
      feature: { head: 'feat1', is_main: false, workspace_ids: [] },
    };
    const layout = computeLayout(
      makeResponse(nodes, branches, {
        main_ahead_count: 3,
        main_ahead_newest_timestamp: '2024-06-15T10:30:00Z',
      })
    );

    const syncNode = layout.nodes.find((n) => n.nodeType === 'sync-summary');
    expect(syncNode).toBeDefined();
    expect(syncNode!.syncSummary?.newestTimestamp).toBe('2024-06-15T10:30:00Z');
  });

  it('appends truncation node when local_truncated is true', () => {
    const nodes = [
      makeNode('feat1', ['feature'], ['feat2'], ['feature']),
      makeNode('feat2', ['feature'], ['shared']),
      makeNode('shared', ['main', 'feature'], [], ['main']),
    ];
    const branches = {
      main: { head: 'shared', is_main: true, workspace_ids: [] },
      feature: { head: 'feat1', is_main: false, workspace_ids: [] },
    };
    const layout = computeLayout(makeResponse(nodes, branches, { local_truncated: true }));

    const truncNode = layout.nodes.find((n) => n.nodeType === 'truncation');
    expect(truncNode).toBeDefined();
    expect(truncNode!.hash).toBe('__truncation__');
    // Truncation node should be the last node
    expect(layout.nodes[layout.nodes.length - 1]).toBe(truncNode);
  });

  it('does not insert truncation node when local_truncated is false', () => {
    const nodes = [
      makeNode('feat1', ['feature'], [], ['feature']),
      makeNode('shared', ['main', 'feature'], ['feat1'], ['main']),
    ];
    const branches = {
      main: { head: 'shared', is_main: true, workspace_ids: [] },
      feature: { head: 'feat1', is_main: false, workspace_ids: [] },
    };
    const layout = computeLayout(makeResponse(nodes, branches));

    const truncNode = layout.nodes.find((n) => n.nodeType === 'truncation');
    expect(truncNode).toBeUndefined();
  });

  it('handles a branch with many commits ahead correctly', () => {
    // Build 20 feature-only commits in a chain
    const nodes = [];
    for (let i = 0; i < 20; i++) {
      const hash = `feat${String(i).padStart(3, '0')}`;
      const parent = i < 19 ? `feat${String(i + 1).padStart(3, '0')}` : 'shared';
      const isHead = i === 0 ? ['feature'] : [];
      nodes.push(makeNode(hash, ['feature'], [parent], isHead));
    }
    nodes.push(makeNode('shared', ['main', 'feature'], [], ['main']));

    const branches = {
      main: { head: 'shared', is_main: true, workspace_ids: [] },
      feature: { head: 'feat000', is_main: false, workspace_ids: [] },
    };
    const layout = computeLayout(makeResponse(nodes, branches));

    // All feature-only commits should be in column 1
    const featureCommits = layout.nodes.filter(
      (n) => n.nodeType === 'commit' && n.hash.startsWith('feat')
    );
    expect(featureCommits).toHaveLength(20);
    expect(featureCommits.every((n) => n.column === 1)).toBe(true);

    // Shared commit should be in column 0
    const sharedNode = layout.nodes.find((n) => n.hash === 'shared');
    expect(sharedNode?.column).toBe(0);

    // Lane lines should span the full extent
    const col1Lane = layout.laneLines.find((l) => l.column === 1);
    expect(col1Lane).toBeDefined();
    expect(col1Lane!.fromY).toBeLessThan(col1Lane!.toY);

    // Every commit should have an edge to its parent
    for (const commit of featureCommits) {
      const parentHash = commit.node.parents[0];
      const edge = layout.edges.find((e) => e.fromHash === commit.hash && e.toHash === parentHash);
      expect(edge).toBeDefined();
    }
  });

  it('creates cross-column edges for merge commits', () => {
    // feature merges from main: merge has parents on both columns
    const nodes = [
      makeNode('merge1', ['feature'], ['feat1', 'main1'], ['feature']),
      makeNode('feat1', ['feature'], ['shared']),
      makeNode('main1', ['main'], ['shared'], ['main']),
      makeNode('shared', ['main', 'feature'], []),
    ];
    const branches = {
      main: { head: 'main1', is_main: true, workspace_ids: [] },
      feature: { head: 'merge1', is_main: false, workspace_ids: [] },
    };
    const layout = computeLayout(makeResponse(nodes, branches));

    // merge1 should have edges to both parents
    const edgeToFeat = layout.edges.find((e) => e.fromHash === 'merge1' && e.toHash === 'feat1');
    const edgeToMain = layout.edges.find((e) => e.fromHash === 'merge1' && e.toHash === 'main1');
    expect(edgeToFeat).toBeDefined();
    expect(edgeToMain).toBeDefined();

    // Cross-column edge should have different from/to columns
    expect(edgeToMain!.fromColumn).toBe(1); // merge1 is on feature (col 1)
    expect(edgeToMain!.toColumn).toBe(0); // main1 is on main (col 0)
  });

  it('truncation node uses column 0 when on main branch', () => {
    const nodes = [makeNode('aaa', ['main'], ['bbb'], ['main']), makeNode('bbb', ['main'], [])];
    const branches = { main: { head: 'aaa', is_main: true, workspace_ids: [] } };
    const layout = computeLayout(makeResponse(nodes, branches, { local_truncated: true }));

    const truncNode = layout.nodes.find((n) => n.nodeType === 'truncation');
    expect(truncNode).toBeDefined();
    expect(truncNode!.column).toBe(0);
  });

  it('renders sync summary and truncation together', () => {
    const nodes = [
      makeNode('feat1', ['feature'], ['feat2'], ['feature']),
      makeNode('feat2', ['feature'], ['shared']),
      makeNode('shared', ['main', 'feature'], [], ['main']),
    ];
    const branches = {
      main: { head: 'shared', is_main: true, workspace_ids: [] },
      feature: { head: 'feat1', is_main: false, workspace_ids: [] },
    };
    const layout = computeLayout(
      makeResponse(nodes, branches, {
        main_ahead_count: 5,
        local_truncated: true,
      })
    );

    const syncNode = layout.nodes.find((n) => n.nodeType === 'sync-summary');
    const truncNode = layout.nodes.find((n) => n.nodeType === 'truncation');
    expect(syncNode).toBeDefined();
    expect(truncNode).toBeDefined();

    // Sync summary is first, truncation is last
    expect(layout.nodes[0]).toBe(syncNode);
    expect(layout.nodes[layout.nodes.length - 1]).toBe(truncNode);

    // Sync summary in column 0, truncation in column 1 (feature)
    expect(syncNode!.column).toBe(0);
    expect(truncNode!.column).toBe(1);
  });

  it('renders sync summary and commit workflow together', () => {
    const nodes = [
      makeNode('feat1', ['feature'], [], ['feature']),
      makeNode('shared', ['main', 'feature'], ['feat1'], ['main']),
    ];
    const branches = {
      main: { head: 'shared', is_main: true, workspace_ids: [] },
      feature: { head: 'feat1', is_main: false, workspace_ids: [] },
    };
    const files: FileDiff[] = [
      { lines_added: 10, lines_removed: 2, is_binary: false, new_path: 'app.ts' },
    ];
    const layout = computeLayout(makeResponse(nodes, branches, { main_ahead_count: 2 }), files);

    const nodeTypes = layout.nodes.map((n) => n.nodeType);
    expect(nodeTypes[0]).toBe('sync-summary');
    expect(nodeTypes).toContain('you-are-here');
    expect(nodeTypes).toContain('commit-actions');
    expect(nodeTypes).toContain('commit-file');
    expect(nodeTypes).toContain('commit-footer');

    // Sync summary should be before you-are-here
    const syncIdx = nodeTypes.indexOf('sync-summary');
    const yahIdx = nodeTypes.indexOf('you-are-here');
    expect(syncIdx).toBeLessThan(yahIdx);
  });

  it('places commit workflow nodes in feature branch column', () => {
    const nodes = [
      makeNode('feat1', ['feature'], [], ['feature']),
      makeNode('shared', ['main', 'feature'], ['feat1'], ['main']),
    ];
    const branches = {
      main: { head: 'shared', is_main: true, workspace_ids: [] },
      feature: { head: 'feat1', is_main: false, workspace_ids: [] },
    };
    const files: FileDiff[] = [{ lines_added: 1, lines_removed: 0, is_binary: false }];
    const layout = computeLayout(makeResponse(nodes, branches), files);

    const workflowNodes = layout.nodes.filter(
      (n) =>
        n.nodeType === 'you-are-here' ||
        n.nodeType === 'commit-actions' ||
        n.nodeType === 'commit-file' ||
        n.nodeType === 'commit-footer'
    );
    // All workflow nodes should be in column 1 (feature branch)
    expect(workflowNodes.every((n) => n.column === 1)).toBe(true);
  });

  it('handles many dirty files without breaking layout', () => {
    const nodes = [makeNode('head1', ['main'], [], ['main'])];
    const branches = { main: { head: 'head1', is_main: true, workspace_ids: [] } };
    const files: FileDiff[] = Array.from({ length: 50 }, (_, i) => ({
      lines_added: i + 1,
      lines_removed: 0,
      is_binary: false,
      new_path: `src/components/Component${i}.tsx`,
    }));
    const layout = computeLayout(makeResponse(nodes, branches), files);

    const fileNodes = layout.nodes.filter((n) => n.nodeType === 'commit-file');
    expect(fileNodes).toHaveLength(50);

    // Each file node should have its own unique y position
    const yValues = fileNodes.map((n) => n.y);
    const uniqueYs = new Set(yValues);
    expect(uniqueYs.size).toBe(50);

    // File nodes should be monotonically increasing in y
    for (let i = 1; i < fileNodes.length; i++) {
      expect(fileNodes[i].y).toBeGreaterThan(fileNodes[i - 1].y);
    }
  });

  it('y values are monotonically increasing across all nodes', () => {
    const nodes = [
      makeNode('feat1', ['feature'], ['feat2'], ['feature']),
      makeNode('feat2', ['feature'], ['shared']),
      makeNode('shared', ['main', 'feature'], [], ['main']),
    ];
    const branches = {
      main: { head: 'shared', is_main: true, workspace_ids: [] },
      feature: { head: 'feat1', is_main: false, workspace_ids: [] },
    };
    const files: FileDiff[] = [{ lines_added: 1, lines_removed: 0, is_binary: false }];
    const layout = computeLayout(makeResponse(nodes, branches, { main_ahead_count: 2 }), files);

    for (let i = 1; i < layout.nodes.length; i++) {
      expect(layout.nodes[i].y).toBeGreaterThan(layout.nodes[i - 1].y);
    }
  });

  it('does not insert commit-actions or commit-footer when no files', () => {
    const nodes = [makeNode('head1', ['main'], [], ['main'])];
    const branches = { main: { head: 'head1', is_main: true, workspace_ids: [] } };
    const layout = computeLayout(makeResponse(nodes, branches));

    const actionsNode = layout.nodes.find((n) => n.nodeType === 'commit-actions');
    const footerNode = layout.nodes.find((n) => n.nodeType === 'commit-footer');
    const fileNode = layout.nodes.find((n) => n.nodeType === 'commit-file');
    expect(actionsNode).toBeUndefined();
    expect(footerNode).toBeUndefined();
    expect(fileNode).toBeUndefined();
  });

  it('drops edges when parent hash is not in node list (truncated)', () => {
    // Simulate truncated graph — child's parent is missing
    const nodes = [makeNode('child', ['feature'], ['missing-parent'], ['feature'])];
    const branches = {
      feature: { head: 'child', is_main: false, workspace_ids: [] },
    };
    const layout = computeLayout(makeResponse(nodes, branches));

    // Edge to missing-parent should NOT exist (silently dropped)
    const danglingEdge = layout.edges.find((e) => e.toHash === 'missing-parent');
    expect(danglingEdge).toBeUndefined();

    // The node itself should still be in the layout
    const childNode = layout.nodes.find((n) => n.hash === 'child');
    expect(childNode).toBeDefined();
  });

  it('handles root commit with no parents', () => {
    const nodes = [makeNode('root', ['main'], [], ['main'])];
    const branches = { main: { head: 'root', is_main: true, workspace_ids: [] } };
    const layout = computeLayout(makeResponse(nodes, branches));

    const commitEdges = layout.edges.filter((e) => e.fromHash === 'root' || e.toHash === 'root');
    // Only edge should be you-are-here → root, no parent edges from root
    expect(commitEdges.every((e) => e.toHash === 'root')).toBe(true);
    // No parent edges originating from root
    const rootParentEdges = layout.edges.filter((e) => e.fromHash === 'root');
    expect(rootParentEdges).toHaveLength(0);
  });

  it('preserves workspace_ids on layout nodes', () => {
    const node = makeNode('head1', ['main'], [], ['main']);
    node.workspace_ids = ['ws-123', 'ws-456'];
    const branches = { main: { head: 'head1', is_main: true, workspace_ids: [] } };
    const layout = computeLayout(makeResponse([node], branches));

    const commitNode = layout.nodes.find((n) => n.hash === 'head1');
    expect(commitNode?.node.workspace_ids).toEqual(['ws-123', 'ws-456']);
  });

  it('edge coordinates match the from/to node positions exactly', () => {
    const nodes = [
      makeNode('feat1', ['feature'], ['shared'], ['feature']),
      makeNode('shared', ['main', 'feature'], [], ['main']),
    ];
    const branches = {
      main: { head: 'shared', is_main: true, workspace_ids: [] },
      feature: { head: 'feat1', is_main: false, workspace_ids: [] },
    };
    const layout = computeLayout(makeResponse(nodes, branches));

    for (const edge of layout.edges) {
      const fromNode = layout.nodes.find((n) => n.hash === edge.fromHash);
      const toNode = layout.nodes.find((n) => n.hash === edge.toHash);
      if (fromNode && toNode) {
        expect(edge.fromY).toBe(fromNode.y);
        expect(edge.toY).toBe(toNode.y);
        expect(edge.fromColumn).toBe(fromNode.column);
        expect(edge.toColumn).toBe(toNode.column);
      }
    }
  });

  it('uses non-main branch name as default when no is_main flag set', () => {
    // Both branches claim to not be main — first one alphabetically should be treated as main
    const nodes = [
      makeNode('aaa', ['alpha'], [], ['alpha']),
      makeNode('bbb', ['beta'], ['aaa'], ['beta']),
    ];
    const branches = {
      alpha: { head: 'aaa', is_main: false, workspace_ids: [] },
      beta: { head: 'bbb', is_main: false, workspace_ids: [] },
    };
    const layout = computeLayout(makeResponse(nodes, branches));

    // Should not crash, localBranch should be set
    expect(layout.nodes.length).toBeGreaterThan(0);
  });

  it('commit file nodes reference the correct file data', () => {
    const nodes = [makeNode('head1', ['main'], [], ['main'])];
    const branches = { main: { head: 'head1', is_main: true, workspace_ids: [] } };
    const files: FileDiff[] = [
      { lines_added: 10, lines_removed: 5, is_binary: false, new_path: 'src/index.ts' },
      {
        lines_added: 0,
        lines_removed: 20,
        is_binary: false,
        new_path: 'src/old.ts',
        old_path: 'src/old.ts',
      },
      { lines_added: 0, lines_removed: 0, is_binary: true, new_path: 'logo.png' },
    ];
    const layout = computeLayout(makeResponse(nodes, branches), files);

    const fileNodes = layout.nodes.filter((n) => n.nodeType === 'commit-file');
    expect(fileNodes).toHaveLength(3);
    expect(fileNodes[0].file?.new_path).toBe('src/index.ts');
    expect(fileNodes[0].file?.lines_added).toBe(10);
    expect(fileNodes[1].file?.new_path).toBe('src/old.ts');
    expect(fileNodes[1].file?.lines_removed).toBe(20);
    expect(fileNodes[2].file?.is_binary).toBe(true);
  });

  it('main_ahead_count of 0 does not insert sync summary even on feature branch', () => {
    const nodes = [
      makeNode('feat1', ['feature'], [], ['feature']),
      makeNode('shared', ['main', 'feature'], ['feat1'], ['main']),
    ];
    const branches = {
      main: { head: 'shared', is_main: true, workspace_ids: [] },
      feature: { head: 'feat1', is_main: false, workspace_ids: [] },
    };
    const layout = computeLayout(makeResponse(nodes, branches, { main_ahead_count: 0 }));

    const syncNode = layout.nodes.find((n) => n.nodeType === 'sync-summary');
    expect(syncNode).toBeUndefined();
  });

  // --- Long branch / disconnected graph tests ---

  it('reorders nodes when HEAD commit is not first in API response (disconnected graph)', () => {
    // Simulate the backend ISL-style DFS sort where context/public commits
    // appear before draft/branch commits after reversal in a disconnected graph.
    // The API returns: [context1, context2, feat1(HEAD), feat2, shared]
    // computeLayout should reorder so feat1 (HEAD) comes first.
    const nodes = [
      makeNode('context1', ['main'], ['context2']),
      makeNode('context2', ['main'], [], ['main']),
      makeNode('feat1', ['feature'], ['feat2'], ['feature']),
      makeNode('feat2', ['feature'], ['shared']),
      makeNode('shared', ['main', 'feature'], []),
    ];
    const branches = {
      main: { head: 'context2', is_main: true, workspace_ids: [] },
      feature: { head: 'feat1', is_main: false, workspace_ids: [] },
    };
    const layout = computeLayout(makeResponse(nodes, branches));

    const commitNodes = layout.nodes.filter((n) => n.nodeType === 'commit');
    const commitHashes = commitNodes.map((n) => n.hash);

    // feat1 (HEAD) should come before context commits
    expect(commitHashes.indexOf('feat1')).toBeLessThan(commitHashes.indexOf('context1'));
    expect(commitHashes.indexOf('feat1')).toBeLessThan(commitHashes.indexOf('context2'));
  });

  it('you-are-here is always the first or second node even in disconnected graphs', () => {
    // Context commits sort before HEAD in API response
    const nodes = [
      makeNode('ctx1', ['main'], []),
      makeNode('head1', ['feature'], ['ctx1'], ['feature']),
    ];
    const branches = {
      main: { head: 'ctx1', is_main: true, workspace_ids: [] },
      feature: { head: 'head1', is_main: false, workspace_ids: [] },
    };
    const layout = computeLayout(makeResponse(nodes, branches));

    const yahIdx = layout.nodes.findIndex((n) => n.nodeType === 'you-are-here');
    const firstCommitIdx = layout.nodes.findIndex((n) => n.nodeType === 'commit');
    // you-are-here should appear before all commit nodes
    expect(yahIdx).toBeLessThan(firstCommitIdx);
    expect(yahIdx).toBeLessThanOrEqual(1); // at most after sync-summary
  });

  it('you-are-here is before all commits in disconnected graph with sync summary', () => {
    // API returns context commits before HEAD, plus main_ahead_count
    const nodes = [
      makeNode('ctx1', ['main'], ['ctx2']),
      makeNode('ctx2', ['main'], [], ['main']),
      makeNode('feat1', ['feature'], ['shared'], ['feature']),
      makeNode('shared', ['main', 'feature'], []),
    ];
    const branches = {
      main: { head: 'ctx2', is_main: true, workspace_ids: [] },
      feature: { head: 'feat1', is_main: false, workspace_ids: [] },
    };
    const layout = computeLayout(makeResponse(nodes, branches, { main_ahead_count: 5 }));

    const nodeTypes = layout.nodes.map((n) => n.nodeType);
    // sync-summary first, then you-are-here, then commits
    expect(nodeTypes[0]).toBe('sync-summary');
    expect(nodeTypes[1]).toBe('you-are-here');
    // No commit should appear before you-are-here
    const yahIdx = nodeTypes.indexOf('you-are-here');
    const firstCommitIdx = nodeTypes.indexOf('commit');
    expect(yahIdx).toBeLessThan(firstCommitIdx);
  });

  it('long branch (50 commits) has correct ordering and all edges valid', () => {
    // Build a 50-commit feature branch chain
    const nodes = [];
    for (let i = 0; i < 50; i++) {
      const hash = `f${String(i).padStart(3, '0')}`;
      const parent = i < 49 ? `f${String(i + 1).padStart(3, '0')}` : 'fork';
      const isHead = i === 0 ? ['feature'] : [];
      nodes.push(makeNode(hash, ['feature'], [parent], isHead));
    }
    // Fork point (on both branches)
    nodes.push(makeNode('fork', ['main', 'feature'], ['main1'], ['main']));
    // One more main commit below fork
    nodes.push(makeNode('main1', ['main'], []));

    const branches = {
      main: { head: 'fork', is_main: true, workspace_ids: [] },
      feature: { head: 'f000', is_main: false, workspace_ids: [] },
    };
    const layout = computeLayout(makeResponse(nodes, branches));

    // 50 feature commits + fork + main1 + you-are-here = 53 nodes
    const commitNodes = layout.nodes.filter((n) => n.nodeType === 'commit');
    expect(commitNodes).toHaveLength(52);

    // you-are-here is at the top
    expect(layout.nodes[0].nodeType).toBe('you-are-here');

    // All y values are strictly increasing
    for (let i = 1; i < layout.nodes.length; i++) {
      expect(layout.nodes[i].y).toBeGreaterThan(layout.nodes[i - 1].y);
    }

    // Every commit→parent edge references nodes that exist in the layout
    const layoutHashes = new Set(layout.nodes.map((n) => n.hash));
    for (const edge of layout.edges) {
      expect(layoutHashes.has(edge.fromHash)).toBe(true);
      expect(layoutHashes.has(edge.toHash)).toBe(true);
    }

    // Every commit→parent edge has fromY < toY (parent is below child)
    const commitEdges = layout.edges.filter(
      (e) => !e.fromHash.startsWith('__') && !e.toHash.startsWith('__')
    );
    for (const edge of commitEdges) {
      expect(edge.fromY).toBeLessThan(edge.toY);
    }
  });

  it('fork point is present and in column 0 when all commits are fetched', () => {
    // 10 feature commits + fork point + 2 main-only commits
    const nodes = [];
    for (let i = 0; i < 10; i++) {
      const hash = `feat${i}`;
      const parent = i < 9 ? `feat${i + 1}` : 'fork';
      const isHead = i === 0 ? ['feature'] : [];
      nodes.push(makeNode(hash, ['feature'], [parent], isHead));
    }
    nodes.push(makeNode('fork', ['main', 'feature'], ['m1'], ['main']));
    nodes.push(makeNode('m1', ['main'], ['m2']));
    nodes.push(makeNode('m2', ['main'], []));

    const branches = {
      main: { head: 'fork', is_main: true, workspace_ids: [] },
      feature: { head: 'feat0', is_main: false, workspace_ids: [] },
    };
    const layout = computeLayout(makeResponse(nodes, branches));

    // Fork point should exist and be in column 0
    const forkNode = layout.nodes.find((n) => n.hash === 'fork');
    expect(forkNode).toBeDefined();
    expect(forkNode!.column).toBe(0);

    // There should be an edge from the last feature commit to the fork point
    const edgeToFork = layout.edges.find((e) => e.fromHash === 'feat9' && e.toHash === 'fork');
    expect(edgeToFork).toBeDefined();
    // This edge crosses columns (feature col 1 → main col 0)
    expect(edgeToFork!.fromColumn).toBe(1);
    expect(edgeToFork!.toColumn).toBe(0);
  });

  it('disconnected graph with context commits before HEAD reorders correctly', () => {
    // Simulate a graph where the backend DFS puts 5 context commits before
    // the 3 branch commits (disconnected because maxCommits < ahead)
    const nodes = [
      // Context commits (on main, not ancestors of HEAD)
      makeNode('c1', ['main'], ['c2']),
      makeNode('c2', ['main'], ['c3']),
      makeNode('c3', ['main'], ['c4']),
      makeNode('c4', ['main'], ['c5']),
      makeNode('c5', ['main'], [], ['main']),
      // Branch commits (HEAD is b1)
      makeNode('b1', ['feature'], ['b2'], ['feature']),
      makeNode('b2', ['feature'], ['b3']),
      makeNode('b3', ['feature'], []), // parent not in graph (truncated)
    ];
    const branches = {
      main: { head: 'c5', is_main: true, workspace_ids: [] },
      feature: { head: 'b1', is_main: false, workspace_ids: [] },
    };
    const layout = computeLayout(makeResponse(nodes, branches));

    const commitNodes = layout.nodes.filter((n) => n.nodeType === 'commit');
    const commitHashes = commitNodes.map((n) => n.hash);

    // Branch commits should appear before context commits
    expect(commitHashes.indexOf('b1')).toBeLessThan(commitHashes.indexOf('c1'));
    expect(commitHashes.indexOf('b2')).toBeLessThan(commitHashes.indexOf('c1'));
    expect(commitHashes.indexOf('b3')).toBeLessThan(commitHashes.indexOf('c1'));

    // Branch commits maintain their relative order
    expect(commitHashes.indexOf('b1')).toBeLessThan(commitHashes.indexOf('b2'));
    expect(commitHashes.indexOf('b2')).toBeLessThan(commitHashes.indexOf('b3'));
  });

  it('node reordering does not happen when HEAD is already first', () => {
    // Normal case: HEAD commit is first in the array
    const nodes = [
      makeNode('feat1', ['feature'], ['feat2'], ['feature']),
      makeNode('feat2', ['feature'], ['shared']),
      makeNode('shared', ['main', 'feature'], [], ['main']),
    ];
    const branches = {
      main: { head: 'shared', is_main: true, workspace_ids: [] },
      feature: { head: 'feat1', is_main: false, workspace_ids: [] },
    };
    const layout = computeLayout(makeResponse(nodes, branches));

    const commitNodes = layout.nodes.filter((n) => n.nodeType === 'commit');
    const commitHashes = commitNodes.map((n) => n.hash);
    // Order should be preserved: feat1, feat2, shared
    expect(commitHashes).toEqual(['feat1', 'feat2', 'shared']);
  });

  it('long disconnected graph preserves edge integrity after reordering', () => {
    // 3 context commits before 15 branch commits in API response
    const nodes = [];
    // Context commits first (as the backend would sort them)
    nodes.push(makeNode('ctx1', ['main'], ['ctx2']));
    nodes.push(makeNode('ctx2', ['main'], ['ctx3']));
    nodes.push(makeNode('ctx3', ['main'], [], ['main']));
    // Branch commits
    for (let i = 0; i < 15; i++) {
      const hash = `b${String(i).padStart(2, '0')}`;
      const parent = i < 14 ? `b${String(i + 1).padStart(2, '0')}` : 'fork';
      const isHead = i === 0 ? ['feature'] : [];
      nodes.push(makeNode(hash, ['feature'], [parent], isHead));
    }
    nodes.push(makeNode('fork', ['main', 'feature'], ['ctx1']));

    const branches = {
      main: { head: 'ctx3', is_main: true, workspace_ids: [] },
      feature: { head: 'b00', is_main: false, workspace_ids: [] },
    };
    const layout = computeLayout(makeResponse(nodes, branches));

    // Verify every edge's coordinates match the actual node positions
    for (const edge of layout.edges) {
      const fromNode = layout.nodes.find((n) => n.hash === edge.fromHash);
      const toNode = layout.nodes.find((n) => n.hash === edge.toHash);
      if (fromNode && toNode) {
        expect(edge.fromY).toBe(fromNode.y);
        expect(edge.toY).toBe(toNode.y);
        expect(edge.fromColumn).toBe(fromNode.column);
        expect(edge.toColumn).toBe(toNode.column);
        // Edges always go downward (from < to in y)
        expect(edge.fromY).toBeLessThan(edge.toY);
      }
    }

    // All 15 branch commits should be in column 1
    for (let i = 0; i < 15; i++) {
      const hash = `b${String(i).padStart(2, '0')}`;
      const node = layout.nodes.find((n) => n.hash === hash);
      expect(node?.column).toBe(1);
    }
  });

  it('very long branch with sync summary and files still has monotonic y values', () => {
    // 30 branch commits + fork + context, with sync summary and dirty files
    const nodes = [];
    for (let i = 0; i < 30; i++) {
      const hash = `f${String(i).padStart(3, '0')}`;
      const parent = i < 29 ? `f${String(i + 1).padStart(3, '0')}` : 'fork';
      const isHead = i === 0 ? ['feature'] : [];
      nodes.push(makeNode(hash, ['feature'], [parent], isHead));
    }
    nodes.push(makeNode('fork', ['main', 'feature'], [], ['main']));

    const branches = {
      main: { head: 'fork', is_main: true, workspace_ids: [] },
      feature: { head: 'f000', is_main: false, workspace_ids: [] },
    };
    const files: FileDiff[] = [
      { lines_added: 5, lines_removed: 2, is_binary: false, new_path: 'a.ts' },
      { lines_added: 3, lines_removed: 0, is_binary: false, new_path: 'b.ts' },
    ];
    const layout = computeLayout(makeResponse(nodes, branches, { main_ahead_count: 10 }), files);

    // Verify monotonic y values
    for (let i = 1; i < layout.nodes.length; i++) {
      expect(layout.nodes[i].y).toBeGreaterThan(layout.nodes[i - 1].y);
    }

    // Verify expected node types at boundaries
    expect(layout.nodes[0].nodeType).toBe('sync-summary');
    expect(layout.nodes[1].nodeType).toBe('you-are-here');
    expect(layout.nodes[layout.nodes.length - 1].nodeType).toBe('commit');

    // Total nodes: sync-summary + yah + actions + 2 files + footer + 30 commits + fork = 37
    expect(layout.nodes).toHaveLength(37);
  });

  it('truncation + files + sync summary produces correct total row count', () => {
    const nodes = [
      makeNode('feat1', ['feature'], [], ['feature']),
      makeNode('shared', ['main', 'feature'], ['feat1'], ['main']),
    ];
    const branches = {
      main: { head: 'shared', is_main: true, workspace_ids: [] },
      feature: { head: 'feat1', is_main: false, workspace_ids: [] },
    };
    const files: FileDiff[] = [
      { lines_added: 1, lines_removed: 0, is_binary: false, new_path: 'a.ts' },
      { lines_added: 2, lines_removed: 1, is_binary: false, new_path: 'b.ts' },
    ];
    const layout = computeLayout(
      makeResponse(nodes, branches, {
        main_ahead_count: 3,
        local_truncated: true,
      }),
      files
    );

    // Expected nodes:
    // sync-summary (1) + you-are-here (1) + commit-actions (1) + 2 files (2)
    // + commit-footer (1) + feat1 commit (1) + shared commit (1) + truncation (1)
    expect(layout.nodes).toHaveLength(9);

    // Verify all y values are unique and contiguous multiples of ROW_HEIGHT
    for (let i = 0; i < layout.nodes.length; i++) {
      expect(layout.nodes[i].y).toBe(i * ROW_HEIGHT);
    }
  });
});
