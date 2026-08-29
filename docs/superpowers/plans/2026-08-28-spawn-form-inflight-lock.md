# Spawn Form In-Flight Lock Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** While a spawn is in flight, disable every editable control on that spawn form, and keep the lock (and any error) alive across SPA navigation via an app-level store.

**Architecture:** A module-level in-flight store (`lib/spawn-inflight.ts`, read via `useSyncExternalStore`) keyed per form (`workspaceId || 'fresh'`) owns the spawn sequence: optional branch suggestion → `POST /api/spawn` → `waiting` until a spawned session id appears in dashboard data. `SpawnPage` subscribes, derives one `formDisabled` flag, and threads `disabled` to every editable control. Draft persistence helpers move to `lib/spawn-draft.ts` so both the form and the store use one owner.

**Tech Stack:** React 18, TypeScript, Vitest + React Testing Library. Frontend only — no Go changes.

**Spec:** `docs/superpowers/specs/2026-08-28-spawn-form-inflight-lock-design.md`

## Global Constraints

- Run all commands from the repository root of this worktree (`/Users/sergek/dev/schmux-001`).
- Frontend tests run via `./test.sh --quick` during iteration; the full `./test.sh` gates completion. NEVER run `npx vitest` from `assets/dashboard/`.
- NEVER run `npm install` or `vite build` directly; dashboard builds go through `go run ./cmd/build-dashboard` (not needed for tests).
- Committing is the user's lever. At each commit step, stop and ask the user (project convention is the `/commit` skill); do not run `git commit` yourself unless the user has explicitly authorized commits for this plan.
- Format with `./format.sh` before any commit.
- Per-form locking: a lock for one form key must never affect another form key.
- `pendingNavigation` stays a single slot (last-completed spawn wins); do not change `SessionsContext`.
- No cancel button, no backend changes, no reload-survival — all out of scope per spec.

---

### Task 1: Extract draft helpers to `lib/spawn-draft.ts`

**Files:**

- Create: `assets/dashboard/src/lib/spawn-draft.ts`
- Create: `assets/dashboard/src/lib/spawn-draft.test.ts`
- Modify: `assets/dashboard/src/routes/SpawnPage.tsx` (delete lines ~28–78: the `SpawnDraft` interface, `getSpawnDraftKey`, `loadSpawnDraft`, `saveSpawnDraft`, `clearSpawnDraft`; import them instead)

**Interfaces:**

- Consumes: nothing new.
- Produces: `lib/spawn-draft.ts` exporting `interface SpawnDraft`, `loadSpawnDraft(workspaceId: string | null): SpawnDraft | null`, `saveSpawnDraft(workspaceId: string | null, draft: SpawnDraft): void`, `clearSpawnDraft(workspaceId: string | null): void`. Task 2's store calls `clearSpawnDraft`; Task 4's form imports all of them.

- [ ] **Step 1: Write the failing test**

Create `assets/dashboard/src/lib/spawn-draft.test.ts`:

```ts
import { describe, it, expect, beforeEach } from 'vitest';
import { loadSpawnDraft, saveSpawnDraft, clearSpawnDraft, type SpawnDraft } from './spawn-draft';

describe('spawn-draft', () => {
  beforeEach(() => {
    sessionStorage.clear();
  });

  const draft: SpawnDraft = {
    prompt: 'do the thing',
    targetCounts: { claude: 1 },
    modelSelectionMode: 'single',
  };

  it('round-trips a draft per workspace key', () => {
    saveSpawnDraft('ws-1', draft);
    expect(loadSpawnDraft('ws-1')).toEqual(draft);
    expect(loadSpawnDraft(null)).toBeNull();
  });

  it('uses a distinct key for fresh (null workspace)', () => {
    saveSpawnDraft(null, draft);
    expect(loadSpawnDraft(null)).toEqual(draft);
    expect(loadSpawnDraft('ws-1')).toBeNull();
  });

  it('clearSpawnDraft removes only the given key', () => {
    saveSpawnDraft('ws-1', draft);
    saveSpawnDraft(null, draft);
    clearSpawnDraft('ws-1');
    expect(loadSpawnDraft('ws-1')).toBeNull();
    expect(loadSpawnDraft(null)).toEqual(draft);
  });

  it('returns null on corrupt stored JSON instead of throwing', () => {
    sessionStorage.setItem('spawn-draft-ws-1', '{not json');
    expect(loadSpawnDraft('ws-1')).toBeNull();
  });
});
```

- [ ] **Step 2: Run test to verify it fails**

Run from repo root: `./test.sh --quick`
Expected: FAIL — `spawn-draft.test.ts` cannot resolve `./spawn-draft`.

- [ ] **Step 3: Create the module by moving code verbatim**

Create `assets/dashboard/src/lib/spawn-draft.ts` with the code currently at `assets/dashboard/src/routes/SpawnPage.tsx:28-78`, exported:

```ts
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
```

In `SpawnPage.tsx`: delete the moved block (the `SpawnDraft` interface through `clearSpawnDraft`, currently lines 28–78) and add to the imports:

```ts
import {
  loadSpawnDraft,
  saveSpawnDraft,
  clearSpawnDraft,
  type SpawnDraft,
} from '../lib/spawn-draft';
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `./test.sh --quick`
Expected: PASS — new tests green, all existing `SpawnPage.*.test.tsx` still green (behavior unchanged).

- [ ] **Step 5: Commit**

Ask the user to commit (project convention: `/commit`). Suggested message: `refactor(dashboard): extract spawn draft helpers to lib/spawn-draft`

---

### Task 2: The in-flight store (`lib/spawn-inflight.ts`)

**Files:**

- Create: `assets/dashboard/src/lib/spawn-inflight.ts`
- Create: `assets/dashboard/src/lib/spawn-inflight.test.ts`

**Interfaces:**

- Consumes: `spawnSessions`, `suggestBranch`, `getErrorMessage` from `lib/api`; `clearSpawnDraft` from Task 1; `useSessions` from `contexts/SessionsContext` (hook only — precedent: `lib/navigation.ts` already imports it); `WORKSPACE_EXPANDED_KEY` from `lib/constants`; types `SpawnRequest`, `SpawnResult`, `PendingNavigation` from `lib/types`.
- Produces (Task 4 relies on these exact signatures):

```ts
export type SpawnInflightPhase = 'naming' | 'spawning' | 'waiting';
export interface SpawnInflightEntry {
  phase: SpawnInflightPhase;
  error?: string;
  failedPhase?: 'naming' | 'spawning';
  navTarget?: { type: 'session' | 'workspace'; id: string };
  spawnedSessionIds?: string[];
}
export function getSpawnFormKey(workspaceId: string | null): string; // workspaceId || 'fresh'
export function useSpawnInflight(workspaceId: string | null): SpawnInflightEntry | undefined;
export function getSpawnInflightEntry(formKey: string): SpawnInflightEntry | undefined;
export function startSpawn(args: StartSpawnArgs): Promise<void>;
export function consumeSpawnError(
  formKey: string
): { error: string; failedPhase?: 'naming' | 'spawning' } | null;
export function clearIfLanded(formKey: string, sessionsById: Record<string, unknown>): void;
export function resetSpawnInflightForTests(): void;

export interface StartSpawnArgs {
  workspaceId: string | null; // null for fresh — determines form key and which draft to clear
  request: SpawnRequest; // fully built; branch/new_branch filled by suggestion when set
  suggest?: { prompt: string; into: 'branch' | 'new_branch' };
  onSuccess?: () => void; // caller-provided persistence of last-used repo/targets/mode
  setPendingNavigation: (nav: PendingNavigation | null) => void;
}
```

- [ ] **Step 1: Write the failing tests**

Create `assets/dashboard/src/lib/spawn-inflight.test.ts`. Mock `lib/api`; the hook (which imports `SessionsContext`) is exercised in Task 4's component tests, so these tests use the non-hook API only.

```ts
import { describe, it, expect, vi, beforeEach } from 'vitest';
import type { SpawnRequest, SpawnResult } from './types';

const mockSpawnSessions = vi.fn<(req: SpawnRequest) => Promise<SpawnResult[]>>();
const mockSuggestBranch = vi.fn<(req: { prompt: string }) => Promise<{ branch: string }>>();

vi.mock('./api', () => ({
  spawnSessions: (req: SpawnRequest) => mockSpawnSessions(req),
  suggestBranch: (req: { prompt: string }) => mockSuggestBranch(req),
  getErrorMessage: (err: unknown, fallback: string) =>
    err instanceof Error ? err.message : fallback,
}));

// The store imports useSessions for its hook; stub the module so importing
// the store never pulls in the real context tree.
vi.mock('../contexts/SessionsContext', () => ({
  useSessions: () => ({ sessionsById: {} }),
}));

import {
  startSpawn,
  getSpawnInflightEntry,
  getSpawnFormKey,
  consumeSpawnError,
  clearIfLanded,
  resetSpawnInflightForTests,
} from './spawn-inflight';

const baseRequest: SpawnRequest = {
  repo: 'https://example.com/repo.git',
  branch: 'feature/x',
  prompt: 'do the thing',
  nickname: '',
  targets: { claude: 1 },
  workspace_id: '',
};

function deferred<T>() {
  let resolve!: (v: T) => void;
  let reject!: (e: unknown) => void;
  const promise = new Promise<T>((res, rej) => {
    resolve = res;
    reject = rej;
  });
  return { promise, resolve, reject };
}

describe('spawn-inflight store', () => {
  const setPendingNavigation = vi.fn();

  beforeEach(() => {
    vi.clearAllMocks();
    sessionStorage.clear();
    localStorage.clear();
    resetSpawnInflightForTests();
  });

  it('getSpawnFormKey maps null to fresh', () => {
    expect(getSpawnFormKey(null)).toBe('fresh');
    expect(getSpawnFormKey('ws-1')).toBe('ws-1');
  });

  it('runs idle → spawning → waiting on success and records session ids', async () => {
    const d = deferred<SpawnResult[]>();
    mockSpawnSessions.mockReturnValue(d.promise);

    const done = startSpawn({
      workspaceId: 'ws-1',
      request: baseRequest,
      setPendingNavigation,
    });
    expect(getSpawnInflightEntry('ws-1')?.phase).toBe('spawning');

    d.resolve([{ session_id: 'sess-9', workspace_id: 'ws-1' }]);
    await done;

    const entry = getSpawnInflightEntry('ws-1');
    expect(entry?.phase).toBe('waiting');
    expect(entry?.spawnedSessionIds).toEqual(['sess-9']);
    expect(entry?.navTarget).toEqual({ type: 'session', id: 'sess-9' });
    expect(setPendingNavigation).toHaveBeenCalledWith({ type: 'session', id: 'sess-9' });
  });

  it('runs the naming phase first when a suggestion is requested', async () => {
    const naming = deferred<{ branch: string }>();
    mockSuggestBranch.mockReturnValue(naming.promise);
    mockSpawnSessions.mockResolvedValue([{ session_id: 's1', workspace_id: 'w1' }]);

    const done = startSpawn({
      workspaceId: null,
      request: { ...baseRequest, branch: '' },
      suggest: { prompt: 'do the thing', into: 'branch' },
      setPendingNavigation,
    });
    expect(getSpawnInflightEntry('fresh')?.phase).toBe('naming');

    naming.resolve({ branch: 'feature/suggested' });
    await done;

    expect(mockSpawnSessions).toHaveBeenCalledWith(
      expect.objectContaining({ branch: 'feature/suggested' })
    );
    expect(getSpawnInflightEntry('fresh')?.phase).toBe('waiting');
  });

  it('routes a suggestion into new_branch when requested', async () => {
    mockSuggestBranch.mockResolvedValue({ branch: 'feature/split' });
    mockSpawnSessions.mockResolvedValue([{ session_id: 's1', workspace_id: 'w1' }]);

    await startSpawn({
      workspaceId: 'ws-1',
      request: baseRequest,
      suggest: { prompt: 'do the thing', into: 'new_branch' },
      setPendingNavigation,
    });

    expect(mockSpawnSessions).toHaveBeenCalledWith(
      expect.objectContaining({ new_branch: 'feature/split' })
    );
  });

  it('stores failedPhase naming when suggestion fails; consume resets to idle', async () => {
    mockSuggestBranch.mockRejectedValue(new Error('llm down'));

    await startSpawn({
      workspaceId: null,
      request: { ...baseRequest, branch: '' },
      suggest: { prompt: 'x', into: 'branch' },
      setPendingNavigation,
    });

    const entry = getSpawnInflightEntry('fresh');
    expect(entry?.error).toBe('llm down');
    expect(entry?.failedPhase).toBe('naming');
    expect(mockSpawnSessions).not.toHaveBeenCalled();

    expect(consumeSpawnError('fresh')).toEqual({ error: 'llm down', failedPhase: 'naming' });
    expect(getSpawnInflightEntry('fresh')).toBeUndefined();
    expect(consumeSpawnError('fresh')).toBeNull(); // consumed exactly once
  });

  it('an empty suggested branch is a naming failure', async () => {
    mockSuggestBranch.mockResolvedValue({ branch: '  ' });

    await startSpawn({
      workspaceId: null,
      request: { ...baseRequest, branch: '' },
      suggest: { prompt: 'x', into: 'branch' },
      setPendingNavigation,
    });

    expect(getSpawnInflightEntry('fresh')?.failedPhase).toBe('naming');
    expect(mockSpawnSessions).not.toHaveBeenCalled();
  });

  it('stores failedPhase spawning when the request rejects', async () => {
    mockSpawnSessions.mockRejectedValue(new Error('tmux is required'));

    await startSpawn({ workspaceId: 'ws-1', request: baseRequest, setPendingNavigation });

    const entry = getSpawnInflightEntry('ws-1');
    expect(entry?.error).toBe('tmux is required');
    expect(entry?.failedPhase).toBe('spawning');
  });

  it('all-error results become a joined, de-duplicated error', async () => {
    mockSpawnSessions.mockResolvedValue([{ error: 'boom' }, { error: 'boom' }, { error: 'bang' }]);

    await startSpawn({ workspaceId: 'ws-1', request: baseRequest, setPendingNavigation });

    expect(getSpawnInflightEntry('ws-1')?.error).toBe('boom; bang');
    expect(setPendingNavigation).not.toHaveBeenCalled();
  });

  it('multi-session success targets the workspace and keeps all session ids', async () => {
    mockSpawnSessions.mockResolvedValue([
      { session_id: 's1', workspace_id: 'w1' },
      { session_id: 's2', workspace_id: 'w1' },
    ]);

    await startSpawn({ workspaceId: null, request: baseRequest, setPendingNavigation });

    const entry = getSpawnInflightEntry('fresh');
    expect(entry?.navTarget).toEqual({ type: 'workspace', id: 'w1' });
    expect(entry?.spawnedSessionIds).toEqual(['s1', 's2']);
    expect(setPendingNavigation).toHaveBeenCalledWith({ type: 'workspace', id: 'w1' });
  });

  it('success clears the draft for its own key only and runs onSuccess', async () => {
    sessionStorage.setItem('spawn-draft-ws-1', '{"prompt":"a"}');
    sessionStorage.setItem('spawn-draft-fresh', '{"prompt":"b"}');
    mockSpawnSessions.mockResolvedValue([{ session_id: 's1', workspace_id: 'w1' }]);
    const onSuccess = vi.fn();

    await startSpawn({
      workspaceId: 'ws-1',
      request: baseRequest,
      onSuccess,
      setPendingNavigation,
    });

    expect(sessionStorage.getItem('spawn-draft-ws-1')).toBeNull();
    expect(sessionStorage.getItem('spawn-draft-fresh')).toBe('{"prompt":"b"}');
    expect(onSuccess).toHaveBeenCalledTimes(1);
  });

  it('failure keeps the draft and skips onSuccess', async () => {
    sessionStorage.setItem('spawn-draft-ws-1', '{"prompt":"a"}');
    mockSpawnSessions.mockRejectedValue(new Error('nope'));
    const onSuccess = vi.fn();

    await startSpawn({
      workspaceId: 'ws-1',
      request: baseRequest,
      onSuccess,
      setPendingNavigation,
    });

    expect(sessionStorage.getItem('spawn-draft-ws-1')).toBe('{"prompt":"a"}');
    expect(onSuccess).not.toHaveBeenCalled();
  });

  it('expands successful workspaces in the sidebar localStorage state', async () => {
    localStorage.setItem('schmux:workspace-expanded', JSON.stringify({ other: false }));
    mockSpawnSessions.mockResolvedValue([{ session_id: 's1', workspace_id: 'w1' }]);

    await startSpawn({ workspaceId: null, request: baseRequest, setPendingNavigation });

    const expanded = JSON.parse(localStorage.getItem('schmux:workspace-expanded')!);
    expect(expanded).toEqual({ other: false, w1: true });
  });

  it('refuses to start while an entry exists for the same key, but not other keys', async () => {
    const d = deferred<SpawnResult[]>();
    mockSpawnSessions.mockReturnValue(d.promise);

    const first = startSpawn({ workspaceId: 'ws-1', request: baseRequest, setPendingNavigation });
    await startSpawn({ workspaceId: 'ws-1', request: baseRequest, setPendingNavigation });
    expect(mockSpawnSessions).toHaveBeenCalledTimes(1); // second refused

    mockSpawnSessions.mockResolvedValue([{ session_id: 's2', workspace_id: 'w2' }]);
    await startSpawn({ workspaceId: 'ws-2', request: baseRequest, setPendingNavigation });
    expect(getSpawnInflightEntry('ws-2')?.phase).toBe('waiting'); // other key unaffected

    d.resolve([{ session_id: 's1', workspace_id: 'w1' }]);
    await first;
  });

  it('clearIfLanded clears only when a spawned session id appears', async () => {
    mockSpawnSessions.mockResolvedValue([{ session_id: 'new-sess', workspace_id: 'w1' }]);
    await startSpawn({ workspaceId: 'ws-1', request: baseRequest, setPendingNavigation });

    // Pre-existing sessions in the workspace must NOT clear the lock.
    clearIfLanded('ws-1', { 'old-sess': {} });
    expect(getSpawnInflightEntry('ws-1')?.phase).toBe('waiting');

    clearIfLanded('ws-1', { 'old-sess': {}, 'new-sess': {} });
    expect(getSpawnInflightEntry('ws-1')).toBeUndefined();
  });

  it('clearIfLanded ignores entries that are not waiting', async () => {
    mockSpawnSessions.mockRejectedValue(new Error('nope'));
    await startSpawn({ workspaceId: 'ws-1', request: baseRequest, setPendingNavigation });

    clearIfLanded('ws-1', { anything: {} });
    expect(getSpawnInflightEntry('ws-1')?.error).toBe('nope'); // error still consumable
  });

  it('success with no session ids clears immediately instead of waiting forever', async () => {
    mockSpawnSessions.mockResolvedValue([{ workspace_id: 'w1' }]);

    await startSpawn({ workspaceId: 'ws-1', request: baseRequest, setPendingNavigation });

    expect(getSpawnInflightEntry('ws-1')).toBeUndefined();
    expect(setPendingNavigation).toHaveBeenCalledWith({ type: 'workspace', id: 'w1' });
  });
});
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `./test.sh --quick`
Expected: FAIL — cannot resolve `./spawn-inflight`.

- [ ] **Step 3: Implement the store**

Create `assets/dashboard/src/lib/spawn-inflight.ts`:

```ts
import { useEffect, useSyncExternalStore } from 'react';
import { spawnSessions, suggestBranch, getErrorMessage } from './api';
import { clearSpawnDraft } from './spawn-draft';
import { useSessions } from '../contexts/SessionsContext';
import { WORKSPACE_EXPANDED_KEY } from './constants';
import type { PendingNavigation, SpawnRequest, SpawnResult } from './types';

// App-level in-flight spawn state, keyed per form (workspaceId || 'fresh').
// Owns the spawn sequence so it survives SpawnPage unmounts; see
// docs/superpowers/specs/2026-08-28-spawn-form-inflight-lock-design.md.

export type SpawnInflightPhase = 'naming' | 'spawning' | 'waiting';

export interface SpawnInflightEntry {
  phase: SpawnInflightPhase;
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
```

Note: `getErrorMessage` in `lib/api` may not currently unwrap `Error` instances the way the test mock does — the mock defines the behavior the store needs, and the store only passes through whatever string `getErrorMessage` returns. Do not modify `lib/api`.

- [ ] **Step 4: Run tests to verify they pass**

Run: `./test.sh --quick`
Expected: PASS.

- [ ] **Step 5: Commit**

Ask the user to commit. Suggested message: `feat(dashboard): add spawn in-flight store with per-form phase tracking`

---

### Task 3: `disabled` prop on PromptTextarea

**Files:**

- Modify: `assets/dashboard/src/components/PromptTextarea.tsx`
- Create: `assets/dashboard/src/components/PromptTextarea.test.tsx`

**Interfaces:**

- Consumes: nothing new.
- Produces: `PromptTextareaProps` gains `disabled?: boolean`. When true: the textarea is disabled, and the slash-command menu and autocomplete never render. Task 4 passes `disabled={formDisabled}`.

- [ ] **Step 1: Write the failing test**

Create `assets/dashboard/src/components/PromptTextarea.test.tsx`:

```tsx
import { describe, it, expect, vi } from 'vitest';
import { render, screen, fireEvent } from '@testing-library/react';
import PromptTextarea from './PromptTextarea';

function renderTextarea(props: Partial<React.ComponentProps<typeof PromptTextarea>> = {}) {
  return render(
    <PromptTextarea
      value={props.value ?? ''}
      onChange={props.onChange ?? vi.fn()}
      commands={props.commands ?? ['/resume']}
      onSelectCommand={props.onSelectCommand ?? vi.fn()}
      data-testid="prompt"
      {...props}
    />
  );
}

describe('PromptTextarea disabled', () => {
  it('disables the textarea when disabled is set', () => {
    renderTextarea({ disabled: true });
    expect(screen.getByTestId('prompt')).toBeDisabled();
  });

  it('stays enabled by default', () => {
    renderTextarea();
    expect(screen.getByTestId('prompt')).not.toBeDisabled();
  });

  it('does not render the slash menu while disabled, even with a slash value', () => {
    // Type "/" while enabled to activate the menu, then re-render disabled.
    const { rerender } = renderTextarea({ value: '/' });
    const textarea = screen.getByTestId('prompt');
    fireEvent.change(textarea, { target: { value: '/', selectionStart: 1 } });

    rerender(
      <PromptTextarea
        value="/"
        onChange={vi.fn()}
        commands={['/resume']}
        onSelectCommand={vi.fn()}
        data-testid="prompt"
        disabled
      />
    );
    expect(screen.queryByText('/resume')).not.toBeInTheDocument();
  });
});
```

- [ ] **Step 2: Run test to verify it fails**

Run: `./test.sh --quick`
Expected: FAIL — `toBeDisabled` assertion fails (no `disabled` prop exists yet), and the slash-menu test finds `/resume`.

- [ ] **Step 3: Implement the prop**

In `assets/dashboard/src/components/PromptTextarea.tsx`:

Add to `PromptTextareaProps` (after `onSubmit?`):

```ts
  disabled?: boolean;
```

Destructure it in the component signature (default `false`):

```ts
  onSubmit,
  disabled = false,
```

Gate the two popovers. Change the `slashActive` derivation:

```ts
const slashActive =
  !disabled &&
  !dismissed &&
  !!slashMatch &&
  (slashMatch.index === 0 || /\s/.test(beforeCursor[slashMatch.index! - 1]));
```

and the autocomplete condition:

```ts
const showAutocomplete =
  !disabled && hasAutocomplete && !acDismissed && acQuery.length >= 3 && !acQuery.startsWith('/');
```

Pass it to the textarea element:

```tsx
      <textarea
        ref={textareaRef}
        value={value}
        onChange={handleChange}
        onKeyDown={handleKeyDown}
        placeholder={placeholder}
        rows={expanded ? 20 : 5}
        className="textarea"
        autoFocus
        disabled={disabled}
        data-testid={dataTestId}
```

(The `.textarea` class carries the design system's disabled styling; no CSS changes.)

- [ ] **Step 4: Run tests to verify they pass**

Run: `./test.sh --quick`
Expected: PASS.

- [ ] **Step 5: Commit**

Ask the user to commit. Suggested message: `feat(dashboard): add disabled prop to PromptTextarea`

---

### Task 4: Rewire SpawnPage onto the store and disable the full form

This is the large task. It replaces `engagePhase` with the store, moves the spawn sequences out of the component, and threads `disabled` everywhere.

**Files:**

- Modify: `assets/dashboard/src/routes/SpawnPage.tsx`
- Create: `assets/dashboard/src/routes/SpawnPage.inflight.test.tsx`

**Interfaces:**

- Consumes: everything Task 2 produces (`useSpawnInflight`, `startSpawn`, `consumeSpawnError`, `getSpawnFormKey`, `SpawnInflightEntry`, `StartSpawnArgs`); Task 3's `disabled` prop; Task 1's draft helpers (already imported).
- Produces: no new exports. Behavioral contract: any in-flight entry for this form's key renders every editable control disabled; existing `data-testid`s are unchanged.

- [ ] **Step 1: Write the failing tests**

Create `assets/dashboard/src/routes/SpawnPage.inflight.test.tsx`. Copy the mock preamble from `SpawnPage.fence.test.tsx:1-98` verbatim, with two changes: (a) the `PromptTextarea` mock must forward `disabled`, and (b) the `SessionsContext` mock must expose a mutable `sessionsByIdValue`. Then add the tests. Full file:

```tsx
import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen, waitFor, fireEvent, act } from '@testing-library/react';
import { MemoryRouter } from 'react-router';
import type { ConfigResponse, SpawnRequest, SpawnResult, WorkspaceResponse } from '../lib/types';
import { makeConfig } from '../lib/test-factories';
import { resetSpawnInflightForTests } from '../lib/spawn-inflight';

// --- Mocks (mirrors SpawnPage.fence.test.tsx) ---

const mockGetConfig = vi.fn<() => Promise<ConfigResponse>>();
const mockSpawnSessions = vi.fn<(req: SpawnRequest) => Promise<SpawnResult[]>>();
const mockSuggestBranch = vi.fn();
const mockGetPersonas = vi.fn<() => Promise<{ personas: unknown[] }>>();
const mockGetStyles = vi.fn<() => Promise<{ styles: unknown[] }>>();
const mockAlert = vi.fn();

vi.mock('../lib/api', () => ({
  getConfig: (...args: unknown[]) => mockGetConfig(...(args as [])),
  spawnSessions: (req: SpawnRequest) => mockSpawnSessions(req),
  getErrorMessage: (err: unknown, fallback: string) =>
    err instanceof Error ? err.message : fallback,
  suggestBranch: (...args: unknown[]) => mockSuggestBranch(...args),
  getPersonas: (...args: unknown[]) => mockGetPersonas(...(args as [])),
  getStyles: (...args: unknown[]) => mockGetStyles(...(args as [])),
}));

vi.mock('../lib/spawn-api', () => ({
  getSpawnEntries: vi.fn().mockResolvedValue([]),
  getPromptHistory: vi.fn().mockResolvedValue([]),
}));

vi.mock('../lib/quicklaunch', () => ({
  getQuickLaunchItems: () => [],
}));

vi.mock('../components/ToastProvider', () => ({
  useToast: () => ({ show: vi.fn(), success: vi.fn(), error: vi.fn() }),
}));

vi.mock('../components/ModalProvider', () => ({
  useModal: () => ({
    alert: mockAlert,
    confirm: vi.fn().mockResolvedValue(true),
    prompt: vi.fn(),
  }),
}));

let configContextValue: ConfigResponse | null = null;
let workspacesContextValue: WorkspaceResponse[] = [];
let sessionsByIdValue: Record<string, unknown> = {};
vi.mock('../contexts/ConfigContext', () => ({
  useConfig: () => ({
    config: configContextValue,
    loading: false,
    error: null,
    reloadConfig: vi.fn(),
    getRepoName: (url: string) => url,
  }),
}));

vi.mock('../contexts/SessionsContext', () => ({
  useSessions: () => ({
    workspaces: workspacesContextValue,
    loading: false,
    error: '',
    connected: true,
    waitForSession: vi.fn().mockResolvedValue(true),
    sessionsById: sessionsByIdValue,
    ackSession: vi.fn(),
    pendingNavigation: null,
    setPendingNavigation: vi.fn(),
    clearPendingNavigation: vi.fn(),
    curatorEvents: {},
  }),
}));

vi.mock('../lib/navigation', () => ({
  usePendingNavigation: () => ({
    pendingNavigation: null,
    setPendingNavigation: vi.fn(),
    clearPendingNavigation: vi.fn(),
  }),
}));

vi.mock('../components/WorkspaceHeader', () => ({
  default: () => <div data-testid="workspace-header" />,
}));
vi.mock('../components/SessionTabs', () => ({
  default: () => <div data-testid="session-tabs" />,
}));
vi.mock('../components/PromptTextarea', () => ({
  default: (props: { value: string; onChange: (v: string) => void; disabled?: boolean }) => (
    <textarea
      data-testid="spawn-prompt"
      value={props.value}
      disabled={props.disabled}
      onChange={(e) => props.onChange(e.target.value)}
    />
  ),
}));
vi.mock('../components/Tooltip', () => ({
  default: ({ children }: { children: React.ReactNode }) => <>{children}</>,
}));
vi.mock('../components/RemoteHostSelector', () => ({
  default: (props: { disabled?: boolean }) => (
    <div data-testid="remote-host-selector" data-disabled={props.disabled ? 'true' : 'false'} />
  ),
}));

import SpawnPage from './SpawnPage';

function renderSpawnPage(initialEntry = '/spawn') {
  return render(
    <MemoryRouter initialEntries={[initialEntry]}>
      <SpawnPage />
    </MemoryRouter>
  );
}

function deferred<T>() {
  let resolve!: (v: T) => void;
  const promise = new Promise<T>((res) => {
    resolve = res;
  });
  return { promise, resolve };
}

// Fill the form enough to pass validateForm and click Engage.
// makeConfig() defaults: repo url https://github.com/user/gitrepo.git, model id
// "claude", branch_suggest targets empty — so no naming phase, the branch input
// is visible, and validateForm requires a branch value.
async function fillAndEngage() {
  await waitFor(() => expect(screen.getByTestId('spawn-repo-select')).toBeInTheDocument());
  fireEvent.change(screen.getByTestId('spawn-repo-select'), {
    target: { value: 'https://github.com/user/gitrepo.git' },
  });
  fireEvent.change(screen.getByTestId('agent-select'), { target: { value: 'claude' } });
  fireEvent.change(screen.getByPlaceholderText('e.g. feature/my-branch'), {
    target: { value: 'feature/test-branch' },
  });
  fireEvent.change(screen.getByTestId('spawn-prompt'), { target: { value: 'do the thing' } });
  fireEvent.click(screen.getByTestId('spawn-submit'));
}

describe('SpawnPage in-flight lock', () => {
  beforeEach(() => {
    vi.clearAllMocks();
    localStorage.clear();
    sessionStorage.clear();
    resetSpawnInflightForTests();
    sessionsByIdValue = {};
    const cfg = makeConfig();
    configContextValue = cfg;
    workspacesContextValue = [];
    mockGetConfig.mockResolvedValue(cfg);
    mockGetPersonas.mockResolvedValue({ personas: [] });
    mockGetStyles.mockResolvedValue({ styles: [] });
    mockSpawnSessions.mockResolvedValue([{ session_id: 'sess-1', workspace_id: 'ws-1' }]);
  });

  it('disables every editable control while spawning', async () => {
    const d = deferred<SpawnResult[]>();
    mockSpawnSessions.mockReturnValue(d.promise);

    renderSpawnPage();
    await fillAndEngage();

    await waitFor(() => expect(screen.getByTestId('spawn-submit')).toBeDisabled());
    expect(screen.getByTestId('spawn-prompt')).toBeDisabled();
    expect(screen.getByTestId('spawn-repo-select')).toBeDisabled();
    expect(screen.getByTestId('agent-select')).toBeDisabled();
    expect(screen.getByTestId('remote-host-selector')).toHaveAttribute('data-disabled', 'true');
    expect(screen.getByText('Spawning...')).toBeInTheDocument();

    await act(async () => {
      d.resolve([{ session_id: 'sess-1', workspace_id: 'ws-1' }]);
    });
  });

  it('a remounted form is still locked while the spawn is in flight', async () => {
    const d = deferred<SpawnResult[]>();
    mockSpawnSessions.mockReturnValue(d.promise);

    const first = renderSpawnPage();
    await fillAndEngage();
    await waitFor(() => expect(screen.getByTestId('spawn-submit')).toBeDisabled());
    first.unmount();

    renderSpawnPage();
    await waitFor(() => expect(screen.getByTestId('spawn-submit')).toBeDisabled());
    expect(screen.getByTestId('spawn-prompt')).toBeDisabled();
    expect(screen.getByText('Spawning...')).toBeInTheDocument();

    await act(async () => {
      d.resolve([{ session_id: 'sess-1', workspace_id: 'ws-1' }]);
    });
  });

  it('surfaces an error that happened while unmounted, once, and unlocks', async () => {
    const d = deferred<SpawnResult[]>();
    mockSpawnSessions.mockReturnValue(d.promise);

    const first = renderSpawnPage();
    await fillAndEngage();
    await waitFor(() => expect(screen.getByTestId('spawn-submit')).toBeDisabled());
    first.unmount();

    await act(async () => {
      d.resolve([{ error: 'boom' }]);
    });

    renderSpawnPage();
    await waitFor(() =>
      expect(mockAlert).toHaveBeenCalledWith('Spawn Failed', 'Failed to spawn: boom')
    );
    await waitFor(() => expect(screen.getByTestId('spawn-submit')).not.toBeDisabled());
    // Draft survived the failure.
    expect(sessionStorage.getItem('spawn-draft-fresh')).toContain('do the thing');
    expect(mockAlert).toHaveBeenCalledTimes(1);
  });

  it('a tmux error shows the tmux banner instead of the alert', async () => {
    mockSpawnSessions.mockRejectedValue(new Error('tmux is required to spawn sessions'));

    renderSpawnPage();
    await fillAndEngage();

    await waitFor(() => expect(screen.getByTestId('tmux-error')).toBeInTheDocument());
    expect(mockAlert).not.toHaveBeenCalled();
    expect(screen.getByTestId('spawn-submit')).not.toBeDisabled();
  });

  it('unlocks when a spawned session id appears in dashboard data', async () => {
    renderSpawnPage();
    await fillAndEngage();

    await waitFor(() => expect(screen.getByText('Downloading session...')).toBeInTheDocument());

    sessionsByIdValue = { 'sess-1': {} };
    // Any state update re-renders the page; the inflight hook then observes
    // sessionsById and clears the entry.
    fireEvent.click(document.body);
    await waitFor(() => expect(screen.getByTestId('spawn-submit')).not.toBeDisabled());
  });

  it('an in-flight fresh spawn does not lock a workspace form', async () => {
    const d = deferred<SpawnResult[]>();
    mockSpawnSessions.mockReturnValue(d.promise);

    const first = renderSpawnPage();
    await fillAndEngage();
    await waitFor(() => expect(screen.getByTestId('spawn-submit')).toBeDisabled());
    first.unmount();

    workspacesContextValue = [
      {
        id: 'ws-other',
        repo: 'https://github.com/user/gitrepo.git',
        branch: 'main',
        sessions: [],
      } as unknown as WorkspaceResponse,
    ];
    renderSpawnPage('/spawn?workspace_id=ws-other');
    await waitFor(() => expect(screen.getByTestId('spawn-submit')).not.toBeDisabled());

    await act(async () => {
      d.resolve([{ session_id: 'sess-1', workspace_id: 'ws-1' }]);
    });
  });
});
```

The `makeConfig()` defaults in `assets/dashboard/src/lib/test-factories.ts` already satisfy every assumption above; pass overrides only if a default has drifted when you run this.

- [ ] **Step 2: Run tests to verify they fail**

Run: `./test.sh --quick`
Expected: FAIL — controls are not disabled, remount renders idle, `resetSpawnInflightForTests` unresolvable until Task 2 landed (it did), etc. The pre-existing `SpawnPage.*.test.tsx` suites must still pass at this point.

- [ ] **Step 3: Rewire the component**

All changes in `assets/dashboard/src/routes/SpawnPage.tsx`.

**3a. Imports.** Add:

```ts
import {
  useSpawnInflight,
  startSpawn,
  consumeSpawnError,
  getSpawnFormKey,
} from '../lib/spawn-inflight';
import type { SpawnRequest } from '../lib/types';
```

**3b. Replace `engagePhase` state.** Delete the `engagePhase`/`setEngagePhase` `useState` (line ~165). After `urlWorkspaceId` is derived (line ~228), add:

```ts
const inflight = useSpawnInflight(urlWorkspaceId);
const engagePhase = inflight?.phase ?? 'idle';
const formDisabled = inflight !== undefined;
```

Keeping the `engagePhase` name (now derived) minimizes render-side churn: the button's phase labels and existing `disabled={engagePhase !== 'idle'}` sites keep working. Delete every `setEngagePhase(...)` call as the handlers are rewritten below. The `isMounted` ref uses tied to the naming flow (lines ~896, ~930) go away with those blocks; keep the ref itself only if other uses remain (there are none — delete the ref and its effect at lines ~235, ~304-308).

**3c. Error-consuming effect.** Add after the `inflight` derivation:

```ts
// Surface a stored spawn failure exactly once — whether it happened while
// this form was mounted or while the user was elsewhere.
useEffect(() => {
  if (!inflight?.error) return;
  const consumed = consumeSpawnError(getSpawnFormKey(urlWorkspaceId));
  if (!consumed) return;
  if (consumed.error.includes('tmux is required')) {
    setTmuxError(consumed.error);
  } else if (consumed.failedPhase === 'naming') {
    if (mode === 'fresh') setShowBranchInput(true);
    alert(
      'Branch Suggestion Failed',
      `Branch suggestion failed: ${consumed.error}. Please enter a branch name.`
    );
  } else {
    alert('Spawn Failed', `Failed to spawn: ${consumed.error}`);
  }
}, [inflight, urlWorkspaceId, mode, alert]);
```

**3d. Rewrite `handleEngage`.** Replace the body of `handleEngage` (lines ~862-999) — the branch-decision logic survives, the sequencing moves to the store:

```ts
const handleEngage = useCallback(() => {
  if (formDisabled) return;
  if (!validateForm()) return;
  setTmuxError('');

  const selectedTargets: Record<string, number> = {};
  Object.entries(targetCounts).forEach(([name, count]) => {
    if (count > 0) selectedTargets[name] = count;
  });

  const actualRepo =
    repo === '__new__'
      ? /^(https?:\/\/|git@|ssh:\/\/|git:\/\/)/.test(newRepoName.trim())
        ? newRepoName.trim()
        : `local:${newRepoName.trim()}`
      : repo;

  let actualBranch = branch;
  let actualNickname = nickname;
  let suggest: { prompt: string; into: 'branch' | 'new_branch' } | undefined;

  if (mode === 'fresh') {
    if (isSapling) {
      actualBranch = '';
    } else if (branch.trim()) {
      actualBranch = branch.trim();
    } else if (branchSuggestEnabled) {
      actualBranch = '';
      suggest = { prompt, into: 'branch' };
    } else {
      actualBranch = getDefaultBranch(actualRepo);
      actualNickname = '';
    }
  }

  if (
    mode === 'workspace' &&
    createBranch &&
    prompt.trim() &&
    branchSuggestEnabled &&
    !isSaplingWorkspace
  ) {
    suggest = { prompt, into: 'new_branch' };
  }

  const request: SpawnRequest = {
    repo: actualRepo,
    branch: actualBranch,
    prompt,
    nickname: actualNickname.trim(),
    targets: selectedTargets,
    workspace_id: prefillWorkspaceId || '',
    remote_profile_id: environment.type === 'remote' ? environment.profileId : undefined,
    remote_flavor: environment.type === 'remote' ? environment.flavor : undefined,
    remote_host_id: environment.type === 'remote' ? environment.hostId : undefined,
    persona_id: selectedPersonaId || undefined,
    style_id: selectedStyleId || undefined,
    image_attachments: imageAttachments.length > 0 ? imageAttachments : undefined,
    workspace_label: isSapling ? workspaceLabel.trim() : undefined,
    fence: fenceForRequest,
  };

  void startSpawn({
    workspaceId: urlWorkspaceId,
    request,
    suggest,
    onSuccess: () => {
      saveLastRepo(actualRepo);
      saveLastTargetCounts(selectedTargets);
      saveLastModelSelectionMode(modelSelectionMode);
      setImageAttachments([]);
    },
    setPendingNavigation,
  });
}, [
  formDisabled,
  validateForm,
  targetCounts,
  repo,
  newRepoName,
  branch,
  nickname,
  mode,
  createBranch,
  prompt,
  branchSuggestEnabled,
  environment,
  prefillWorkspaceId,
  modelSelectionMode,
  getDefaultBranch,
  selectedPersonaId,
  selectedStyleId,
  imageAttachments,
  isSapling,
  isSaplingWorkspace,
  workspaceLabel,
  fenceForRequest,
  urlWorkspaceId,
  setPendingNavigation,
]);
```

Note `setImageAttachments([])` runs in `onSuccess` even if the form unmounted — a no-op setter call on an unmounted component is safe, and the draft (which carries the images) is cleared by the store.

**3e. Rewrite `handleSlashCommandSelect`.** Same transformation for its three branches (lines ~713-860). The `/resume` branch:

```ts
if (command === '/resume') {
  const selectedTargets: Record<string, number> = {};
  Object.entries(targetCounts).forEach(([name, count]) => {
    if (count > 0) selectedTargets[name] = count;
  });
  if (Object.keys(selectedTargets).length === 0) {
    toastError('Select an agent first');
    return;
  }
  const actualRepo =
    repo === '__new__'
      ? /^(https?:\/\/|git@|ssh:\/\/|git:\/\/)/.test(newRepoName.trim())
        ? newRepoName.trim()
        : `local:${newRepoName.trim()}`
      : repo;
  const actualBranch =
    mode === 'fresh' ? (isSapling ? '' : branch.trim() || getDefaultBranch(actualRepo)) : '';
  void startSpawn({
    workspaceId: urlWorkspaceId,
    request: {
      repo: mode === 'fresh' ? actualRepo : '',
      branch: actualBranch,
      prompt: '',
      nickname: '',
      targets: selectedTargets,
      workspace_id: prefillWorkspaceId || '',
      resume: true,
      fence: fenceForRequest,
      remote_profile_id: environment.type === 'remote' ? environment.profileId : undefined,
      remote_flavor: environment.type === 'remote' ? environment.flavor : undefined,
      remote_host_id: environment.type === 'remote' ? environment.hostId : undefined,
      persona_id: selectedPersonaId || undefined,
      style_id: selectedStyleId || undefined,
      intent_shared: shareIntent || undefined,
    },
    onSuccess: () => {
      saveLastRepo(actualRepo);
      saveLastTargetCounts(selectedTargets);
    },
    setPendingNavigation,
  });
  return;
}
```

The `/quick` branch:

```ts
if (command.startsWith('/quick ')) {
  const quickName = command.slice('/quick '.length);
  void startSpawn({
    workspaceId: urlWorkspaceId,
    request: {
      repo: '',
      branch: '',
      prompt: '',
      nickname: '',
      targets: {},
      workspace_id: prefillWorkspaceId || '',
      quick_launch_name: quickName,
      fence: fenceForRequest,
    },
    setPendingNavigation,
  });
  return;
}
```

The command-target fallthrough:

```ts
const actualRepo =
  repo === '__new__'
    ? /^(https?:\/\/|git@|ssh:\/\/|git:\/\/)/.test(newRepoName.trim())
      ? newRepoName.trim()
      : `local:${newRepoName.trim()}`
    : repo;
const actualBranch =
  mode === 'fresh' ? (isSapling ? '' : branch.trim() || getDefaultBranch(actualRepo)) : branch;
void startSpawn({
  workspaceId: urlWorkspaceId,
  request: {
    repo: actualRepo,
    branch: actualBranch,
    prompt: '',
    nickname: '',
    targets: { [command]: 1 },
    workspace_id: prefillWorkspaceId || '',
    fence: fenceForRequest,
    remote_profile_id: environment.type === 'remote' ? environment.profileId : undefined,
    remote_flavor: environment.type === 'remote' ? environment.flavor : undefined,
    remote_host_id: environment.type === 'remote' ? environment.hostId : undefined,
    persona_id: selectedPersonaId || undefined,
    style_id: selectedStyleId || undefined,
  },
  onSuccess: () => {
    saveLastRepo(actualRepo);
  },
  setPendingNavigation,
});
```

The handler's opening guard becomes `if (formDisabled) return;` (was `engagePhase !== 'idle'`). Update the dependency array: drop `handleSpawnResult`, add `formDisabled`, `urlWorkspaceId`, `setPendingNavigation`, `fenceForRequest`, `shareIntent`.

**3f. Delete dead code.** Remove `handleSpawnResult` (lines ~648-710), `generateBranchName` (lines ~587-599), the `isMounted` ref and its cleanup effect, the now-unused imports `spawnSessions`, `suggestBranch`, and `getErrorMessage` from `../lib/api`, the now-unused `SuggestBranchResponse` type import, and the `WORKSPACE_EXPANDED_KEY` import from `../lib/constants` (its only use moved into the store). Let `tsc` (via the full `./test.sh`) confirm nothing else references them.

**3g. Draft-persist guard.** In the persist effect (line ~494-531), replace the `engagePhase === 'waiting'` early-return with:

```ts
// Never persist while a spawn is in flight — nothing can legitimately change.
if (inflight) return;
```

and swap `engagePhase` for `inflight` in its dependency array.

**3h. Image-paste guard.** In the paste effect (line ~1016-1052), add `if (formDisabled) return;` at the top of `handlePaste`, and add `formDisabled` to the effect's dependency array. Also disable each image "✕" remove button (line ~1155): add `disabled={formDisabled}` to the `<button>`.

**3i. Thread `disabled` through the render.** Using `formDisabled`:

- `RemoteHostSelector` (line ~1100): `disabled={formDisabled}` (replaces `engagePhase !== 'idle'`).
- `PromptTextarea` (line ~1109): add `disabled={formDisabled}`.
- Single-mode fresh block: `agent-select` (~1199), `persona-select` (~1236), `style-select` (~1255), `spawn-repo-select` (~1273), branch `input#branch` (~1299), sapling label input (~1312), `newRepoName` input (~1324) — add `disabled={formDisabled}` to each.
- Single-mode workspace/remote block: `agent-select` (~1346), `persona-select` (~1383), `style-select` (~1401) — same.
- Multi/advanced block: repo `select#repo` (~1434), `newRepoName` input (~1458), "Single agent" mode button (~1483), each agent toggle button (~1502), each advanced "−" button (~1561, change `disabled={count === 0}` to `disabled={formDisabled || count === 0}`), each advanced "+" button (~1591), `persona-select` (~1629), `style-select` (~1644) — add `disabled={formDisabled}`.
- Multi/advanced branch input (~1674) and sapling label mirror (~1687): `disabled={formDisabled}`.
- Checkboxes: `createBranch` (~1746) and fence (~1772) already use `engagePhase !== 'idle'` — normalize to `formDisabled`; `shareIntent` (~1757) gains `disabled={formDisabled}`.
- Engage button (~1783): normalize to `disabled={formDisabled}`; the phase-label ternary chain is unchanged (it reads the derived `engagePhase`).

Line numbers are pre-edit references; locate controls by their `data-testid`/`id` when offsets have shifted.

- [ ] **Step 4: Run tests to verify they pass**

Run: `./test.sh --quick`
Expected: PASS — the new `SpawnPage.inflight.test.tsx` suite and all pre-existing suites (`fence`, `agent-select`, `sapling`). If a pre-existing test fails, the rewiring changed behavior it pinned — fix the code, not the old test, unless the old test asserted `engagePhase` mechanics that no longer exist.

- [ ] **Step 5: Style-guide check**

Run the `dashboard-style-check` skill (checklist at `.claude/skills/schmux-dashboard-style-check/SKILL.md`) over the changed markup — disabled states must come from design-system classes, no hardcoded palette colors introduced.

- [ ] **Step 6: Commit**

Ask the user to commit. Suggested message: `feat(dashboard): lock the whole spawn form while a spawn is in flight`

---

### Task 5: Full verification

**Files:** none (verification only).

- [ ] **Step 1: Format**

Run: `./format.sh`
Expected: exits 0; re-stage anything it touched.

- [ ] **Step 2: Full test suite**

Run: `./test.sh`
Expected: PASS — this is the definition-of-done gate; `--quick` does not count.

- [ ] **Step 3: Static analysis**

Run: `./badcode.sh`
Expected: exits 0 (knip may flag exports used only by tests — `resetSpawnInflightForTests` is exported deliberately for tests; if knip complains, add it to the knip ignore per that tool's existing configuration pattern rather than deleting it).

- [ ] **Step 4: Docs check**

No Go API packages changed, so `docs/api.md` is untouched by design. Confirm: `git diff --stat main...HEAD -- internal/` is empty.

- [ ] **Step 5: Report**

Report results to the user with the test output. Remind them the spec and this plan are deleted at `/finalize` time per project convention.
