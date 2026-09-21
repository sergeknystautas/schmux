// Working-tree file checkboxes on the git tab, stored in localStorage.
// Keyed per workspace. Only UNCHECKED paths are stored, so a path the store
// has never seen (including a file that appears in a later diff) is checked.
// No key and an empty array mean the same thing: everything is checked.
// Advisory UI state only: it never changes what git does on its own.

export function getUncheckedDiffFilesKey(workspaceId: string): string {
  return `schmux:diff-unchecked-files:${workspaceId}`;
}

export function loadUncheckedDiffFiles(workspaceId: string): Set<string> {
  const key = getUncheckedDiffFilesKey(workspaceId);
  let raw: string | null;
  try {
    raw = localStorage.getItem(key);
  } catch (err) {
    console.warn('Failed to read unchecked diff files:', err);
    return new Set();
  }
  if (raw === null) return new Set();

  let parsed: unknown;
  try {
    parsed = JSON.parse(raw);
  } catch {
    parsed = undefined;
  }
  if (!Array.isArray(parsed) || !parsed.every((p) => typeof p === 'string')) {
    console.warn('Discarding malformed unchecked diff files for workspace', workspaceId);
    saveUncheckedDiffFiles(workspaceId, new Set());
    return new Set();
  }
  return new Set((parsed as string[]).filter((p) => p !== ''));
}

export function saveUncheckedDiffFiles(workspaceId: string, unchecked: ReadonlySet<string>): void {
  const key = getUncheckedDiffFilesKey(workspaceId);
  const paths = Array.from(unchecked).filter((p) => p !== '');
  try {
    if (paths.length === 0) {
      localStorage.removeItem(key);
    } else {
      localStorage.setItem(key, JSON.stringify(paths));
    }
  } catch (err) {
    console.warn('Failed to save unchecked diff files:', err);
  }
}
