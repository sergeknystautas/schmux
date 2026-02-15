import { describe, it, expect } from 'vitest';
import { computeLayout, ROW_HEIGHT } from './gitGraphLayout';
import type { GitGraphResponse, FileDiff } from './types';

function makeNode(
  hash: string,
  branches: string[],
  parents: string[] = [],
  isHead: string[] = []
): GitGraphResponse['nodes'][0] {
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
  nodes: GitGraphResponse['nodes'],
  branches: GitGraphResponse['branches'],
  overrides: Partial<GitGraphResponse> = {}
): GitGraphResponse {
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
});
