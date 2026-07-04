import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest';
import { analyzeFence } from './api';

const mockFetch = vi.fn();

describe('analyzeFence', () => {
  beforeEach(() => {
    mockFetch.mockReset();
    vi.stubGlobal('fetch', mockFetch);
  });
  afterEach(() => {
    vi.restoreAllMocks();
  });

  it('POSTs to the fence-analyze endpoint and returns the spawned session', async () => {
    const session = {
      session_id: 'sess-new',
      workspace_id: 'ws-1',
      nickname: 'fence-analyze',
    };
    mockFetch.mockResolvedValueOnce({
      ok: true,
      json: () => Promise.resolve(session),
    });

    const result = await analyzeFence('sess-tgt');

    expect(mockFetch).toHaveBeenCalledWith(
      '/api/sessions/sess-tgt/fence-analyze',
      expect.objectContaining({ method: 'POST' })
    );
    // The client sends only the session id — no prompt, preset names, or paths.
    const init = mockFetch.mock.calls[0][1] as RequestInit;
    expect(init.body).toBeUndefined();
    expect(result).toEqual(session);
  });

  it('throws when the endpoint returns an error', async () => {
    mockFetch.mockResolvedValueOnce({
      ok: false,
      status: 400,
      json: () => Promise.resolve({ error: 'no analysis target configured' }),
      text: () => Promise.resolve('no analysis target configured'),
    });
    await expect(analyzeFence('sess-tgt')).rejects.toThrow();
  });
});
