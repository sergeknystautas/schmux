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
