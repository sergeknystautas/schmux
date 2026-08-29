// Per-tab active spawn draft, stored in sessionStorage.
// Keyed per form: one draft per workspace, plus one for fresh spawns.
// Cleared by the in-flight store on successful spawn.

export interface SpawnDraft {
  prompt: string;
  targetCounts: Record<string, number>;
  modelSelectionMode: 'single' | 'multiple' | 'advanced';
  // Only for fresh spawns (no workspace_id)
  repo?: string;
  newRepoName?: string;
  // Only for workspace mode
  createBranch?: boolean;
  imageAttachments?: string[]; // base64-encoded PNGs
}

function getSpawnDraftKey(workspaceId: string | null): string {
  return `spawn-draft-${workspaceId || 'fresh'}`;
}

export function loadSpawnDraft(workspaceId: string | null): SpawnDraft | null {
  try {
    const key = getSpawnDraftKey(workspaceId);
    const stored = sessionStorage.getItem(key);
    if (stored) {
      return JSON.parse(stored) as SpawnDraft;
    }
  } catch (err) {
    console.warn('Failed to load spawn draft:', err);
  }
  return null;
}

export function saveSpawnDraft(workspaceId: string | null, draft: SpawnDraft): void {
  try {
    const key = getSpawnDraftKey(workspaceId);
    sessionStorage.setItem(key, JSON.stringify(draft));
  } catch (err) {
    console.warn('Failed to save spawn draft:', err);
  }
}

export function clearSpawnDraft(workspaceId: string | null): void {
  try {
    const key = getSpawnDraftKey(workspaceId);
    sessionStorage.removeItem(key);
  } catch (err) {
    console.warn('Failed to clear spawn draft:', err);
  }
}
