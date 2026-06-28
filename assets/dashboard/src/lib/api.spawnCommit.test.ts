import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest';
import { spawnCommitSession } from './api';
import type { ConfigResponse } from './types.generated';
import { makeConfig, systemCapabilities } from './test-factories';

const mockFetch = vi.fn();

/**
 * Build a config response for the commit-tab tests. Pins `commit_message.target`
 * (spawnCommitSession throws when it is empty) and defaults fence to usable, then
 * applies the per-test overrides.
 */
function configWith(overrides: Partial<ConfigResponse>): ConfigResponse {
  return makeConfig({
    commit_message: { target: 'claude' },
    fence_commit: false,
    fence_mode: 'optional_off',
    system_capabilities: systemCapabilities({ fence_available: true }),
    ...overrides,
  });
}

/** Route the three fetches spawnCommitSession makes, returning the supplied config. */
function stubFetch(config: ConfigResponse): void {
  mockFetch.mockImplementation((url: string) => {
    if (url === '/api/config') {
      return Promise.resolve({ ok: true, json: () => Promise.resolve(config) });
    }
    if (url === '/api/commit/prompt') {
      return Promise.resolve({ ok: true, json: () => Promise.resolve({ prompt: 'template' }) });
    }
    if (url === '/api/spawn') {
      return Promise.resolve({ ok: true, json: () => Promise.resolve([]) });
    }
    return Promise.resolve({ ok: false, status: 404, text: () => Promise.resolve('') });
  });
}

/** The `fence` value sent on the /api/spawn POST body. */
function spawnFence(): unknown {
  const call = mockFetch.mock.calls.find(([url]) => url === '/api/spawn');
  if (!call) throw new Error('no /api/spawn call recorded');
  return JSON.parse(call[1].body).fence;
}

describe('spawnCommitSession fence wiring', () => {
  beforeEach(() => {
    mockFetch.mockReset();
    vi.stubGlobal('fetch', mockFetch);
  });

  afterEach(() => {
    vi.restoreAllMocks();
  });

  it('fences the commit when fence_commit is on and fence is usable', async () => {
    stubFetch(configWith({ fence_commit: true, fence_mode: 'optional_off' }));
    await spawnCommitSession('ws-1', 'repo', 'main', ['a.ts']);
    expect(spawnFence()).toBe(true);
  });

  it('does not fence when fence_commit is off', async () => {
    stubFetch(configWith({ fence_commit: false }));
    await spawnCommitSession('ws-1', 'repo', 'main', ['a.ts']);
    expect(spawnFence()).toBe(false);
  });

  it('does not fence when fence_mode is disabled', async () => {
    stubFetch(configWith({ fence_commit: true, fence_mode: 'disabled' }));
    await spawnCommitSession('ws-1', 'repo', 'main', ['a.ts']);
    expect(spawnFence()).toBe(false);
  });

  it('does not fence when fence is unavailable', async () => {
    stubFetch(configWith({ fence_commit: true, system_capabilities: systemCapabilities() }));
    await spawnCommitSession('ws-1', 'repo', 'main', ['a.ts']);
    expect(spawnFence()).toBe(false);
  });
});
