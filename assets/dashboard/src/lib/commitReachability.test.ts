import { describe, it, expect } from 'vitest';
import { reachableFrom, countUnpushed } from './commitReachability';
import type { CommitGraphNode } from './types.generated';

// Linear history: c3 -> c2 -> c1 -> root
function node(hash: string, parents: string[]): CommitGraphNode {
  return {
    hash,
    short_hash: hash.slice(0, 7),
    message: `commit ${hash}`,
    author: 'test',
    timestamp: '2026-01-01T00:00:00Z',
    parents,
    branches: [],
    is_head: [],
    workspace_ids: [],
  };
}

const nodes = [node('c3', ['c2']), node('c2', ['c1']), node('c1', ['root']), node('root', [])];

describe('reachableFrom', () => {
  it('walks ancestors inclusively', () => {
    expect(reachableFrom('c2', nodes)).toEqual(new Set(['c2', 'c1', 'root']));
  });

  it('returns empty for missing or absent start', () => {
    expect(reachableFrom(undefined, nodes).size).toBe(0);
    expect(reachableFrom('nope', nodes).size).toBe(0);
  });

  it('ignores parents outside the loaded graph (truncation)', () => {
    const truncated = [node('c3', ['c2']), node('c2', ['c1'])]; // c1 not loaded
    expect(reachableFrom('c3', truncated)).toEqual(new Set(['c3', 'c2']));
  });

  it('handles merge parents', () => {
    const merged = [
      node('m', ['a', 'b']),
      node('a', ['root']),
      node('b', ['root']),
      node('root', []),
    ];
    expect(reachableFrom('m', merged)).toEqual(new Set(['m', 'a', 'b', 'root']));
  });
});

describe('countUnpushed', () => {
  it('counts ancestors-or-self not in the exclude set', () => {
    const onOrigin = reachableFrom('c1', nodes); // c1, root already pushed
    expect(countUnpushed('c3', nodes, onOrigin)).toBe(2); // c3, c2
    expect(countUnpushed('c2', nodes, onOrigin)).toBe(1); // c2
    expect(countUnpushed('c1', nodes, onOrigin)).toBe(0);
  });
});
