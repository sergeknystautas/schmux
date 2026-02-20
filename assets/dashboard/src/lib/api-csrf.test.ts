import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest';
import {
  remoteAccessOn,
  remoteAccessOff,
  setRemoteAccessPassword,
  testRemoteAccessNotification,
} from './api';

// Mock fetch to inspect headers
const mockFetch = vi.fn();

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
    const [, init] = mockFetch.mock.calls[0];
    expect(init.headers).toBeDefined();
    // Headers could be a plain object or Headers instance
    const token =
      init.headers instanceof Headers
        ? init.headers.get('X-CSRF-Token')
        : init.headers['X-CSRF-Token'];
    expect(token).toBe('test-csrf-token');
  });

  it('remoteAccessOff sends X-CSRF-Token header', async () => {
    await remoteAccessOff();
    const [, init] = mockFetch.mock.calls[0];
    expect(init.headers).toBeDefined();
    const token =
      init.headers instanceof Headers
        ? init.headers.get('X-CSRF-Token')
        : init.headers['X-CSRF-Token'];
    expect(token).toBe('test-csrf-token');
  });

  it('setRemoteAccessPassword sends X-CSRF-Token header', async () => {
    await setRemoteAccessPassword('new-password');
    const [, init] = mockFetch.mock.calls[0];
    expect(init.headers).toBeDefined();
    const token =
      init.headers instanceof Headers
        ? init.headers.get('X-CSRF-Token')
        : init.headers['X-CSRF-Token'];
    expect(token).toBe('test-csrf-token');
  });

  it('testRemoteAccessNotification sends X-CSRF-Token header', async () => {
    await testRemoteAccessNotification();
    const [, init] = mockFetch.mock.calls[0];
    expect(init.headers).toBeDefined();
    const token =
      init.headers instanceof Headers
        ? init.headers.get('X-CSRF-Token')
        : init.headers['X-CSRF-Token'];
    expect(token).toBe('test-csrf-token');
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
    const token =
      init.headers instanceof Headers
        ? init.headers.get('Content-Type')
        : init.headers['Content-Type'];
    expect(token).toBe('application/json');
    expect(JSON.parse(init.body)).toEqual({ password: 'my-pass' });
  });
});
