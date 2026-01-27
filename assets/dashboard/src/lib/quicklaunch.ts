/**
 * Merges global quicklaunch names with workspace-specific names.
 * Workspace names are appended after global names.
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

  return result;
}
