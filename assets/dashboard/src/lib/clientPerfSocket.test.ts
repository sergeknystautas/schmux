import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest';
import { clientPerf } from './clientPerf';
import { setTransport, transport, liveTransport } from './transport';

class FakeSocket {
  onmessage: ((ev: MessageEvent) => void) | null = null;
  onclose: (() => void) | null = null;
  readyState = 1;
  constructor(public url: string) {}
  emit(data: unknown) {
    this.onmessage?.({ data } as MessageEvent);
  }
  close() {
    this.readyState = 3;
    this.onclose?.();
  }
}

describe('clientPerf socket wrapper', () => {
  const sockets: FakeSocket[] = [];
  beforeEach(() => {
    vi.useFakeTimers({ toFake: ['setInterval', 'clearInterval'] });
    localStorage.clear();
    clientPerf.setConfigEnabled(true);
    clientPerf.setDevMode(true);
    clientPerf.start();
  });
  afterEach(() => {
    for (const s of sockets) s.readyState !== 3 && s.close();
    sockets.length = 0;
    clientPerf.stop();
    setTransport(liveTransport);
    vi.useRealTimers();
  });

  it('sums messages per second by path and type and keeps large ones individually', () => {
    setTransport({
      ...liveTransport,
      createWebSocket: (url) => {
        const s = new FakeSocket(url);
        sockets.push(s);
        return s as unknown as WebSocket;
      },
    });
    const ws = transport.createWebSocket('ws://localhost/ws/terminal/abc123');
    const seen: string[] = [];
    ws.onmessage = (ev) => seen.push(String(ev.data).length.toString());
    sockets[0].emit(JSON.stringify({ type: 'output', data: 'x' }));
    sockets[0].emit(JSON.stringify({ type: 'output', data: 'y'.repeat(60 * 1024) }));
    sockets[0].emit(new ArrayBuffer(16));
    vi.advanceTimersByTime(1000);
    const file = clientPerf.buildFile();
    expect(seen).toHaveLength(3);
    expect(file.websocket.perSecond).toEqual([
      expect.objectContaining({ path: '/ws/terminal/:id', type: 'output', count: 2 }),
      expect.objectContaining({ path: '/ws/terminal/:id', type: 'binary', count: 1, bytes: 16 }),
    ]);
    expect(file.websocket.individual).toEqual([
      expect.objectContaining({ type: 'output', bytes: 60 * 1024 + 27 }),
    ]);
  });

  it('counts open sockets and records the last /ws/dashboard message size', () => {
    setTransport({
      ...liveTransport,
      createWebSocket: (url) => {
        const s = new FakeSocket(url);
        sockets.push(s);
        return s as unknown as WebSocket;
      },
    });
    const ws = transport.createWebSocket('ws://localhost/ws/dashboard');
    ws.onmessage = () => {};
    sockets[0].emit('{"type":"sessions"}');
    expect(clientPerf.socketCount()).toBe(1);
    expect(clientPerf.lastDashboardMessageBytes()).toBe(19);
    sockets[0].close();
    expect(clientPerf.socketCount()).toBe(0);
  });

  it('does not instrument twice when the active transport is set again', () => {
    setTransport({
      ...liveTransport,
      createWebSocket: (url) => {
        const s = new FakeSocket(url);
        sockets.push(s);
        return s as unknown as WebSocket;
      },
    });
    setTransport(transport);
    const ws = transport.createWebSocket('ws://localhost/ws/dashboard');
    ws.onmessage = () => {};
    sockets[0].emit('{"type":"sessions"}');
    vi.advanceTimersByTime(1000);
    expect(clientPerf.socketCount()).toBe(1);
    expect(clientPerf.buildFile().websocket.perSecond).toEqual([
      expect.objectContaining({ path: '/ws/dashboard', type: 'sessions', count: 1 }),
    ]);
  });
});
