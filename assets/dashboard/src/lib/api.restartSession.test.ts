import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest';
import { restartSession } from './api';

const mockFetch = vi.fn();

describe('restartSession', () => {
  beforeEach(() => {
    mockFetch.mockReset();
    vi.stubGlobal('fetch', mockFetch);
  });
  afterEach(() => {
    vi.restoreAllMocks();
  });

  it('POSTs to the restart endpoint and returns the resumed session', async () => {
    const session = {
      session_id: 'sess-new',
      workspace_id: 'ws-1',
      target: 'claude',
      nickname: 'agent',
    };
    mockFetch.mockResolvedValueOnce({
      ok: true,
      json: () => Promise.resolve(session),
    });

    const result = await restartSession('sess-old');

    expect(mockFetch).toHaveBeenCalledWith(
      '/api/sessions/sess-old/restart',
      expect.objectContaining({ method: 'POST' })
    );
    const init = mockFetch.mock.calls[0][1] as RequestInit;
    expect(init.body).toBeUndefined();
    expect(result).toEqual(session);
  });

  it('throws when the endpoint returns an error', async () => {
    mockFetch.mockResolvedValueOnce({
      ok: false,
      status: 400,
      json: () => Promise.resolve({ error: 'session has no resume id' }),
      text: () => Promise.resolve('session has no resume id'),
    });
    await expect(restartSession('sess-old')).rejects.toThrow();
  });
});
