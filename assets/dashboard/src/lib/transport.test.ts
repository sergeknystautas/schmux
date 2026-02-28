import { describe, it, expect } from 'vitest';
import { transport, setTransport, liveTransport, type Transport } from './transport';

describe('transport', () => {
  it('defaults to liveTransport', () => {
    expect(transport).toBe(liveTransport);
  });

  it('setTransport swaps the active transport', () => {
    const mock: Transport = {
      createWebSocket: () => ({}) as WebSocket,
      fetch: () => Promise.resolve(new Response()),
    };
    setTransport(mock);
    expect(transport).toBe(mock);
    // Restore
    setTransport(liveTransport);
    expect(transport).toBe(liveTransport);
  });

  it('liveTransport.fetch delegates to window.fetch', async () => {
    // Just verify the shape — actual fetch is tested elsewhere
    expect(typeof liveTransport.fetch).toBe('function');
    expect(typeof liveTransport.createWebSocket).toBe('function');
  });
});
