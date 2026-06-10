import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';

const mockFetch = vi.fn();
vi.mock('./transport', () => ({
  transport: { fetch: (...args: unknown[]) => mockFetch(...args) },
}));

import { getAuthMe } from './api';

function res(body: unknown, status: number): Response {
  return new Response(status === 204 ? null : JSON.stringify(body), { status });
}

beforeEach(() => {
  vi.clearAllMocks();
});
afterEach(() => {
  vi.restoreAllMocks();
});

describe('getAuthMe', () => {
  it('maps 200 to authenticated with the user', async () => {
    mockFetch.mockResolvedValue(
      res({ github_id: 1, login: 'octocat', name: 'Mona', avatar_url: 'https://x/y.png' }, 200)
    );
    const result = await getAuthMe();
    expect(result).toEqual({
      status: 'authenticated',
      user: { login: 'octocat', name: 'Mona', avatar_url: 'https://x/y.png' },
    });
  });

  it('maps 401 to unauthenticated and dispatches schmux:auth-expired', async () => {
    mockFetch.mockResolvedValue(res({ error: 'Unauthorized' }, 401));
    const spy = vi.fn();
    window.addEventListener('schmux:auth-expired', spy);
    const result = await getAuthMe();
    window.removeEventListener('schmux:auth-expired', spy);
    expect(result).toEqual({ status: 'unauthenticated' });
    expect(spy).toHaveBeenCalledOnce();
  });

  it('maps 404 to disabled', async () => {
    mockFetch.mockResolvedValue(res({ error: 'Auth disabled' }, 404));
    const result = await getAuthMe();
    expect(result).toEqual({ status: 'disabled' });
  });
});
