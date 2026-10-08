import { describe, it, expect, vi, afterEach } from 'vitest';
import { transport, setTransport, liveTransport, type Transport } from './transport';

describe('transport', () => {
  afterEach(() => {
    setTransport(liveTransport);
  });

  it('defaults to liveTransport', () => {
    // transport is always instrumented (clientPerf wraps createWebSocket);
    // fetch keeps the live implementation's identity.
    expect(transport.fetch).toBe(liveTransport.fetch);
  });

  it('setTransport swaps the active transport and fetch uses the swapped implementation', async () => {
    const mockResponse = new Response('mock body');
    const mock: Transport = {
      createWebSocket: () => ({}) as WebSocket,
      fetch: vi.fn().mockResolvedValue(mockResponse),
    };
    setTransport(mock);
    expect(transport.fetch).toBe(mock.fetch);

    // Call fetch through the swapped transport and verify it delegates correctly
    const result = await transport.fetch('/api/test', { method: 'POST' });
    expect(result).toBe(mockResponse);
    expect(mock.fetch).toHaveBeenCalledWith('/api/test', { method: 'POST' });
  });

  it('liveTransport.fetch delegates to window.fetch', async () => {
    const fakeResponse = new Response('ok');
    const originalFetch = window.fetch;
    window.fetch = vi.fn().mockResolvedValue(fakeResponse);
    try {
      const result = await liveTransport.fetch('/api/health');
      expect(window.fetch).toHaveBeenCalledWith('/api/health', undefined);
      expect(result).toBe(fakeResponse);
    } finally {
      window.fetch = originalFetch;
    }
  });
});
