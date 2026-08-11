import type { CommitGraphNode } from './types.generated';

/**
 * Set of commit hashes reachable from startHash (inclusive) by walking parent
 * edges, restricted to nodes present in the loaded graph. Empty set when
 * startHash is missing or not in the graph.
 */
export function reachableFrom(
  startHash: string | undefined,
  nodes: CommitGraphNode[]
): Set<string> {
  const byHash = new Map(nodes.map((n) => [n.hash, n]));
  const seen = new Set<string>();
  if (!startHash || !byHash.has(startHash)) return seen;
  const stack = [startHash];
  while (stack.length > 0) {
    const h = stack.pop()!;
    if (seen.has(h)) continue;
    seen.add(h);
    for (const p of byHash.get(h)?.parents ?? []) {
      if (byHash.has(p) && !seen.has(p)) stack.push(p);
    }
  }
  return seen;
}

/** Number of commits in hash's ancestry (inclusive) that are not in excludeSet. */
export function countUnpushed(
  hash: string,
  nodes: CommitGraphNode[],
  excludeSet: Set<string>
): number {
  let n = 0;
  for (const h of reachableFrom(hash, nodes)) {
    if (!excludeSet.has(h)) n++;
  }
  return n;
}
