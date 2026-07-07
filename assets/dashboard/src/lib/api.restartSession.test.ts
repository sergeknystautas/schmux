import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest';
import { restartSession, getRestartOptions } from './api';

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

  it('sends a JSON body when overrides are provided', async () => {
    const session = { session_id: 'sess-new' };
    mockFetch.mockResolvedValueOnce({ ok: true, json: () => Promise.resolve(session) });

    await restartSession('sess-old', { target: 'claude-opus-4-6', fence: false });

    const init = mockFetch.mock.calls[0][1] as RequestInit;
    expect(init.method).toBe('POST');
    expect(JSON.parse(init.body as string)).toEqual({ target: 'claude-opus-4-6', fence: false });
    expect((init.headers as Record<string, string>)['Content-Type']).toBe('application/json');
  });

  it('omits the body when no overrides are provided (unchanged plain restart)', async () => {
    mockFetch.mockResolvedValueOnce({ ok: true, json: () => Promise.resolve({ session_id: 'x' }) });
    await restartSession('sess-old');
    const init = mockFetch.mock.calls[0][1] as RequestInit;
    expect(init.body).toBeUndefined();
  });
});

describe('getRestartOptions', () => {
  beforeEach(() => {
    mockFetch.mockReset();
    vi.stubGlobal('fetch', mockFetch);
  });
  afterEach(() => {
    vi.restoreAllMocks();
  });

  it('GETs the restart-options endpoint and returns the parsed options', async () => {
    const options = {
      current_target: 'claude',
      targets: ['claude'],
      fence: true,
      fence_available: true,
    };
    mockFetch.mockResolvedValueOnce({ ok: true, json: () => Promise.resolve(options) });

    const result = await getRestartOptions('sess-1');

    expect(mockFetch).toHaveBeenCalledWith('/api/sessions/sess-1/restart-options', undefined);
    expect(result).toEqual(options);
  });
});
