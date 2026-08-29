import { useEffect, useSyncExternalStore } from 'react';
import { spawnSessions, suggestBranch, getErrorMessage } from './api';
import { clearSpawnDraft } from './spawn-draft';
import { useSessions } from '../contexts/SessionsContext';
import { WORKSPACE_EXPANDED_KEY } from './constants';
import type { PendingNavigation, SpawnRequest, SpawnResult } from './types';

// App-level in-flight spawn state, keyed per form (workspaceId || 'fresh').
// Owns the spawn sequence so it survives SpawnPage unmounts; see
// docs/superpowers/specs/2026-08-28-spawn-form-inflight-lock-design.md.

export interface SpawnInflightEntry {
  phase: 'naming' | 'spawning' | 'waiting';
  error?: string; // set on failure; consumed once via consumeSpawnError
  failedPhase?: 'naming' | 'spawning';
  navTarget?: { type: 'session' | 'workspace'; id: string };
  spawnedSessionIds?: string[]; // waiting clears when one of these appears
}

export interface StartSpawnArgs {
  workspaceId: string | null; // null for fresh spawns
  request: SpawnRequest;
  suggest?: { prompt: string; into: 'branch' | 'new_branch' };
  onSuccess?: () => void;
  setPendingNavigation: (nav: PendingNavigation | null) => void;
}

const entries = new Map<string, SpawnInflightEntry>();
const listeners = new Set<() => void>();
// Immutable snapshot so useSyncExternalStore sees stable references between emits.
let snapshot: Record<string, SpawnInflightEntry> = {};

function emit(): void {
  snapshot = Object.fromEntries(entries);
  listeners.forEach((l) => l());
}

function subscribe(listener: () => void): () => void {
  listeners.add(listener);
  return () => listeners.delete(listener);
}

function setEntry(formKey: string, entry: SpawnInflightEntry): void {
  entries.set(formKey, entry);
  emit();
}

function deleteEntry(formKey: string): void {
  if (entries.delete(formKey)) emit();
}

export function getSpawnFormKey(workspaceId: string | null): string {
  return workspaceId || 'fresh';
}

export function getSpawnInflightEntry(formKey: string): SpawnInflightEntry | undefined {
  return snapshot[formKey];
}

export function consumeSpawnError(
  formKey: string
): { error: string; failedPhase?: 'naming' | 'spawning' } | null {
  const entry = entries.get(formKey);
  if (!entry?.error) return null;
  deleteEntry(formKey);
  return { error: entry.error, failedPhase: entry.failedPhase };
}

export function clearIfLanded(formKey: string, sessionsById: Record<string, unknown>): void {
  const entry = entries.get(formKey);
  if (entry?.phase !== 'waiting') return;
  if (entry.spawnedSessionIds?.some((id) => id in sessionsById)) {
    deleteEntry(formKey);
  }
}

export function resetSpawnInflightForTests(): void {
  entries.clear();
  snapshot = {};
  listeners.clear();
}

/** Subscribe to the in-flight entry for one form, clearing it once its
 *  spawned session lands in dashboard data. undefined = idle. */
export function useSpawnInflight(workspaceId: string | null): SpawnInflightEntry | undefined {
  const formKey = getSpawnFormKey(workspaceId);
  const { sessionsById } = useSessions();
  const entry = useSyncExternalStore(subscribe, () => snapshot[formKey]);
  useEffect(() => {
    clearIfLanded(formKey, sessionsById);
  }, [entry, sessionsById, formKey]);
  return entry;
}

function expandWorkspacesInSidebar(results: SpawnResult[]): void {
  const workspaceIds = [...new Set(results.map((r) => r.workspace_id).filter(Boolean))] as string[];
  if (workspaceIds.length === 0) return;
  let expanded: Record<string, boolean> = {};
  try {
    expanded = JSON.parse(localStorage.getItem(WORKSPACE_EXPANDED_KEY) || '{}') as Record<
      string,
      boolean
    >;
  } catch (err) {
    console.warn('Failed to parse workspace expanded state:', err);
    expanded = {};
  }
  let changed = false;
  workspaceIds.forEach((id) => {
    if (expanded[id] !== true) {
      expanded[id] = true;
      changed = true;
    }
  });
  if (changed) {
    try {
      localStorage.setItem(WORKSPACE_EXPANDED_KEY, JSON.stringify(expanded));
    } catch (err) {
      console.warn('Failed to save workspace expanded state:', err);
    }
  }
}

export async function startSpawn(args: StartSpawnArgs): Promise<void> {
  const { workspaceId, suggest, onSuccess, setPendingNavigation } = args;
  const formKey = getSpawnFormKey(workspaceId);
  if (entries.has(formKey)) return; // this form is already in flight

  let request = args.request;

  if (suggest) {
    setEntry(formKey, { phase: 'naming' });
    try {
      const result = await suggestBranch({ prompt: suggest.prompt });
      const branchName = result.branch?.trim();
      if (!branchName) throw new Error('empty branch suggestion');
      request =
        suggest.into === 'branch'
          ? { ...request, branch: branchName }
          : { ...request, new_branch: branchName };
    } catch (err) {
      setEntry(formKey, {
        phase: 'naming',
        error: getErrorMessage(err, 'Unknown error'),
        failedPhase: 'naming',
      });
      return;
    }
  }

  setEntry(formKey, { phase: 'spawning' });
  let response: SpawnResult[];
  try {
    response = await spawnSessions(request);
  } catch (err) {
    setEntry(formKey, {
      phase: 'spawning',
      error: getErrorMessage(err, 'Unknown error'),
      failedPhase: 'spawning',
    });
    return;
  }

  const successes = response.filter((r) => !r.error);
  if (successes.length === 0) {
    const unique = [...new Set(response.map((r) => r.error).filter(Boolean))];
    setEntry(formKey, {
      phase: 'spawning',
      error: unique.join('; '),
      failedPhase: 'spawning',
    });
    return;
  }

  clearSpawnDraft(workspaceId);
  onSuccess?.();
  expandWorkspacesInSidebar(successes);

  const spawnedSessionIds = successes.map((r) => r.session_id).filter(Boolean) as string[];
  let navTarget: SpawnInflightEntry['navTarget'];
  if (successes.length === 1 && successes[0].session_id) {
    navTarget = { type: 'session', id: successes[0].session_id };
  } else if (successes[0].workspace_id) {
    navTarget = { type: 'workspace', id: successes[0].workspace_id };
  }
  if (navTarget) setPendingNavigation(navTarget);

  if (spawnedSessionIds.length === 0) {
    // Nothing to watch for — clear instead of holding the lock forever.
    deleteEntry(formKey);
    return;
  }
  setEntry(formKey, { phase: 'waiting', navTarget, spawnedSessionIds });
}
