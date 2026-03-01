import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest';
import {
  remoteAccessOn,
  remoteAccessOff,
  setRemoteAccessPassword,
  testRemoteAccessNotification,
} from './api';

// Mock fetch to inspect headers
const mockFetch = vi.fn();

/** Extract a header value from the most recent fetch call's init argument. */
function getHeader(name: string): string | null {
  const [, init] = mockFetch.mock.calls[0];
  if (!init.headers) return null;
  return init.headers instanceof Headers ? init.headers.get(name) : (init.headers[name] ?? null);
}

describe('Remote Access API CSRF headers', () => {
  beforeEach(() => {
    mockFetch.mockReset();
    vi.stubGlobal('fetch', mockFetch);
    // Set a CSRF cookie
    document.cookie = 'schmux_csrf=test-csrf-token';
    mockFetch.mockResolvedValue({
      ok: true,
      json: () => Promise.resolve({}),
      text: () => Promise.resolve(''),
    });
  });

  afterEach(() => {
    vi.restoreAllMocks();
    document.cookie = 'schmux_csrf=; Max-Age=0';
  });

  it('remoteAccessOn sends X-CSRF-Token header', async () => {
    await remoteAccessOn();
    expect(getHeader('X-CSRF-Token')).toBe('test-csrf-token');
  });

  it('remoteAccessOff sends X-CSRF-Token header', async () => {
    await remoteAccessOff();
    expect(getHeader('X-CSRF-Token')).toBe('test-csrf-token');
  });

  it('setRemoteAccessPassword sends X-CSRF-Token header', async () => {
    await setRemoteAccessPassword('new-password');
    expect(getHeader('X-CSRF-Token')).toBe('test-csrf-token');
  });

  it('testRemoteAccessNotification sends X-CSRF-Token header', async () => {
    await testRemoteAccessNotification();
    expect(getHeader('X-CSRF-Token')).toBe('test-csrf-token');
  });

  it('remoteAccessOn still includes correct method', async () => {
    await remoteAccessOn();
    const [url, init] = mockFetch.mock.calls[0];
    expect(url).toBe('/api/remote-access/on');
    expect(init.method).toBe('POST');
  });

  it('setRemoteAccessPassword includes Content-Type and body', async () => {
    await setRemoteAccessPassword('my-pass');
    const [url, init] = mockFetch.mock.calls[0];
    expect(url).toBe('/api/remote-access/set-password');
    expect(getHeader('Content-Type')).toBe('application/json');
    expect(JSON.parse(init.body)).toEqual({ password: 'my-pass' });
  });
});
