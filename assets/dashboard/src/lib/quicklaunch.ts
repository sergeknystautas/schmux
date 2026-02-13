export type QuickLaunchItem = {
  name: string;
  scope: 'global' | 'workspace';
};

/**
 * Merges global quicklaunch names with workspace-specific names.
 * Returns sorted alphabetically with duplicates removed.
 * @deprecated Use getQuickLaunchItems() for grouped items with scope info
 */
export function mergeQuickLaunchNames(globalNames: string[], workspaceNames: string[]): string[] {
  const result: string[] = [];
  const seen = new Set<string>();

  for (const name of globalNames || []) {
    const trimmed = name.trim();
    if (!trimmed || seen.has(trimmed)) continue;
    result.push(trimmed);
    seen.add(trimmed);
  }

  for (const name of workspaceNames || []) {
    const trimmed = name.trim();
    if (!trimmed || seen.has(trimmed)) continue;
    result.push(trimmed);
    seen.add(trimmed);
  }

  return result.sort((a, b) => a.localeCompare(b));
}

/**
 * Returns quicklaunch items grouped by scope (global vs workspace).
 * Each group is sorted alphabetically, duplicates removed (global takes precedence).
 */
export function getQuickLaunchItems(
  globalNames: string[],
  workspaceNames: string[]
): QuickLaunchItem[] {
  const result: QuickLaunchItem[] = [];
  const seen = new Set<string>();

  // Global items first
  for (const name of globalNames || []) {
    const trimmed = name.trim();
    if (!trimmed || seen.has(trimmed)) continue;
    result.push({ name: trimmed, scope: 'global' });
    seen.add(trimmed);
  }

  // Workspace items after
  for (const name of workspaceNames || []) {
    const trimmed = name.trim();
    if (!trimmed || seen.has(trimmed)) continue;
    result.push({ name: trimmed, scope: 'workspace' });
    seen.add(trimmed);
  }

  // Sort each group independently while preserving group order
  const globalItems = result
    .filter((i) => i.scope === 'global')
    .sort((a, b) => a.name.localeCompare(b.name));
  const workspaceItems = result
    .filter((i) => i.scope === 'workspace')
    .sort((a, b) => a.name.localeCompare(b.name));

  return [...globalItems, ...workspaceItems];
}
