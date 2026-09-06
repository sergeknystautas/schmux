import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { ChatSocket } from './socket';
import type { ChatSocketStatus } from './socket';
import { setTransport } from '../transport';
import type { ConversationRecord } from './types';

class MockWebSocket {
  static instances: MockWebSocket[] = [];
  onopen: (() => void) | null = null;
  onmessage: ((ev: { data: string }) => void) | null = null;
  onclose: ((ev: { code: number }) => void) | null = null;
  onerror: (() => void) | null = null;
  close = vi.fn();
  sent: string[] = [];
  constructor(public url: string) {
    MockWebSocket.instances.push(this);
  }
  send(data: string) {
    this.sent.push(data);
  }
}

function lastWS(): MockWebSocket {
  return MockWebSocket.instances[MockWebSocket.instances.length - 1];
}

function openWS(ws: MockWebSocket) {
  ws.onopen?.();
}

function msg(ws: MockWebSocket, data: unknown) {
  ws.onmessage?.({ data: JSON.stringify(data) });
}

const records: ConversationRecord[] = [
  { ts: 't', type: 'user_message', id: 'u1', text: 'earlier' },
];

let statuses: ChatSocketStatus[] = [];
let history: ConversationRecord[][] = [];
let live: ConversationRecord[] = [];

beforeEach(() => {
  MockWebSocket.instances = [];
  statuses = [];
  history = [];
  live = [];
  vi.useFakeTimers();
  setTransport({
    createWebSocket: (url: string) => new MockWebSocket(url) as unknown as WebSocket,
    fetch: () => Promise.resolve(new Response()),
  });
});

afterEach(() => {
  vi.useRealTimers();
  setTransport({
    createWebSocket: (url: string) => new WebSocket(url),
    fetch: (input, init) => window.fetch(input, init),
  });
});

function newSocket(): ChatSocket {
  return new ChatSocket('s1', {
    onStatus: (s) => statuses.push(s),
    onHistory: (r) => history.push(r),
    onRecord: (r) => live.push(r),
  });
}

describe('ChatSocket', () => {
  it('connects to /ws/chat/{id} and reports connecting then connected', () => {
    const s = newSocket();
    s.connect();
    expect(MockWebSocket.instances).toHaveLength(1);
    expect(lastWS().url).toMatch(/ws:\/\/localhost.*\/ws\/chat\/s1$/);
    expect(statuses).toEqual(['connecting']);
    openWS(lastWS());
    expect(statuses).toEqual(['connecting', 'connected']);
  });

  it('dispatches history and record frames', () => {
    const s = newSocket();
    s.connect();
    const ws = lastWS();
    msg(ws, { type: 'history', records });
    expect(history).toEqual([records]);
    const rec: ConversationRecord = { ts: 't', type: 'user_message', id: 'u2', text: 'live' };
    msg(ws, { type: 'record', record: rec });
    expect(live).toEqual([rec]);
  });

  it('writes client frames', () => {
    const s = newSocket();
    s.connect();
    const ws = lastWS();
    s.send('hi', []);
    s.interrupt();
    s.permission('r', false, undefined, 'no');
    s.answer('r2', { q: 'A' }, { questions: [] });
    expect(JSON.parse(ws.sent[0])).toEqual({ type: 'send', text: 'hi', images: [] });
    expect(JSON.parse(ws.sent[1])).toEqual({ type: 'interrupt' });
    expect(JSON.parse(ws.sent[2])).toEqual({
      type: 'permission',
      request_id: 'r',
      allow: false,
      message: 'no',
    });
    expect(JSON.parse(ws.sent[3])).toEqual({
      type: 'answer',
      request_id: 'r2',
      answers: { q: 'A' },
      input: { questions: [] },
    });
  });

  it('reconnects with backoff after an unexpected close', () => {
    const s = newSocket();
    s.connect();
    openWS(lastWS());
    lastWS().onclose?.({ code: 1006 });
    expect(statuses).toEqual(['connecting', 'connected', 'disconnected']);
    expect(MockWebSocket.instances).toHaveLength(1);
    vi.advanceTimersByTime(500);
    expect(MockWebSocket.instances).toHaveLength(2);
    expect(statuses[3]).toBe('connecting');
  });

  it('does not reconnect after close()', () => {
    const s = newSocket();
    s.connect();
    s.close();
    lastWS().onclose?.({ code: 1000 });
    vi.advanceTimersByTime(5000);
    expect(MockWebSocket.instances).toHaveLength(1);
  });
});
