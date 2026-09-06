import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { renderHook, act } from '@testing-library/react';
import { useChatSocket } from './useChatSocket';
import { setTransport } from '../lib/transport';

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

beforeEach(() => {
  MockWebSocket.instances = [];
  setTransport({
    createWebSocket: (url: string) => new MockWebSocket(url) as unknown as WebSocket,
    fetch: () => Promise.resolve(new Response()),
  });
});

afterEach(() => {
  setTransport({
    createWebSocket: (url: string) => new WebSocket(url),
    fetch: (input, init) => window.fetch(input, init),
  });
});

describe('useChatSocket', () => {
  it('builds the conversation from history then live records', async () => {
    const { result } = renderHook(() => useChatSocket('s1', true));
    const ws = lastWS();
    act(() => {
      ws.onopen?.();
      ws.onmessage?.({
        data: JSON.stringify({
          type: 'history',
          protocol: 'claude-stream-json',
          records: [{ ts: 't', type: 'user_message', id: 'u1', text: 'hello' }],
        }),
      });
    });
    expect(result.current.status).toBe('connected');
    expect(result.current.conversation.items[0]).toMatchObject({ kind: 'user', text: 'hello' });
    expect(result.current.conversation.phase).toBe('running');
    act(() => {
      ws.onmessage?.({
        data: JSON.stringify({
          type: 'record',
          record: { ts: 't', type: 'harness', line: { type: 'result', subtype: 'success' } },
        }),
      });
    });
    // Live records apply on the next animation frame; flush it.
    await act(async () => {
      await new Promise<void>((r) => requestAnimationFrame(() => r()));
    });
    expect(result.current.conversation.phase).toBe('idle');
  });

  it('reports gone when the session stops running', () => {
    const { result, rerender } = renderHook(({ running }) => useChatSocket('s1', running), {
      initialProps: { running: true },
    });
    expect(result.current.status).toBe('connecting');
    rerender({ running: false });
    expect(result.current.status).toBe('gone');
  });

  it('send writes a frame through the socket', () => {
    const { result } = renderHook(() => useChatSocket('s1', true));
    act(() => {
      result.current.send('hi', []);
    });
    expect(JSON.parse(lastWS().sent[0])).toEqual({ type: 'send', text: 'hi', images: [] });
  });

  it('batches live records into one update per animation frame', async () => {
    const { result } = renderHook(() => useChatSocket('s1', true));
    // Establish baseline: history frame triggers one update.
    const ws = lastWS();
    act(() => {
      ws.onopen?.();
      ws.onmessage?.({
        data: JSON.stringify({
          type: 'history',
          protocol: 'claude-stream-json',
          records: [{ ts: 't0', type: 'user_message', id: 'u0', text: 'hello' }],
        }),
      });
    });
    const baseline = result.current.conversation.items.length;
    // Push three live records synchronously: none should be visible yet.
    act(() => {
      ws.onmessage?.({
        data: JSON.stringify({
          type: 'record',
          record: {
            ts: 't1',
            type: 'harness',
            line: {
              type: 'stream_event',
              event: {
                type: 'content_block_start',
                index: 0,
                content_block: { type: 'text', text: '' },
              },
            },
          },
        }),
      });
      ws.onmessage?.({
        data: JSON.stringify({
          type: 'record',
          record: {
            ts: 't2',
            type: 'harness',
            line: {
              type: 'stream_event',
              event: {
                type: 'content_block_delta',
                index: 0,
                delta: { type: 'text_delta', text: 'a' },
              },
            },
          },
        }),
      });
      ws.onmessage?.({
        data: JSON.stringify({
          type: 'record',
          record: {
            ts: 't3',
            type: 'harness',
            line: {
              type: 'stream_event',
              event: {
                type: 'content_block_delta',
                index: 0,
                delta: { type: 'text_delta', text: 'b' },
              },
            },
          },
        }),
      });
    });
    // Before the frame fires, only the baseline is visible.
    expect(result.current.conversation.items.length).toBe(baseline);
    // Flush the pending animation frame.
    await act(async () => {
      await new Promise<void>((r) => requestAnimationFrame(() => r()));
    });
    // All three records landed in one update: baseline + the assistant turn
    // with the deltas combined as "ab" in a single prose segment.
    const after = result.current.conversation.items;
    expect(after.length).toBe(baseline);
    const turn = after[after.length - 1] as { segments: { kind: string; text?: string }[] };
    const prose = turn.segments.find((s) => s.kind === 'prose');
    expect(prose?.text).toBe('ab');
  });
});
