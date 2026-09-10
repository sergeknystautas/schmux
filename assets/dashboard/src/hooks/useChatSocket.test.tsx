import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { renderHook, act } from '@testing-library/react';
import { useChatSocket } from './useChatSocket';
import { capturedActivity } from '../lib/chat/__fixtures__/activity';
import { selectActivity } from '../lib/chat/activity-selector';
import type { ConversationRecord } from '../lib/chat/types';
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

describe('historyLoaded', () => {
  it('is false until the history frame arrives, then true', () => {
    const { result } = renderHook(() => useChatSocket('s1', true));
    expect(result.current.historyLoaded).toBe(false);
    const ws = lastWS();
    act(() => {
      ws.onopen?.();
    });
    expect(result.current.historyLoaded).toBe(false);
    act(() => {
      ws.onmessage?.({
        data: JSON.stringify({ type: 'history', protocol: 'claude-stream-json', records: [] }),
      });
    });
    expect(result.current.historyLoaded).toBe(true);
  });

  it('resets to false on reconnect and back to true on the new history', async () => {
    const { result } = renderHook(() => useChatSocket('s1', true));
    act(() => {
      lastWS().onopen?.();
      lastWS().onmessage?.({
        data: JSON.stringify({ type: 'history', protocol: 'claude-stream-json', records: [] }),
      });
    });
    expect(result.current.historyLoaded).toBe(true);
    act(() => {
      lastWS().onclose?.({ code: 1006 });
    });
    // Reconnect is scheduled with a 500ms backoff; wait for the new socket to
    // be created before driving its onopen/onmessage handlers.
    await act(async () => {
      await new Promise<void>((r) => setTimeout(r, 600));
    });
    expect(MockWebSocket.instances.length).toBe(2);
    expect(result.current.historyLoaded).toBe(false);
    act(() => {
      lastWS().onopen?.();
    });
    expect(result.current.historyLoaded).toBe(false);
    act(() => {
      lastWS().onmessage?.({
        data: JSON.stringify({ type: 'history', protocol: 'claude-stream-json', records: [] }),
      });
    });
    expect(result.current.historyLoaded).toBe(true);
  });
});

describe('onRequestResolved', () => {
  it('fires for resolution records in history and live traffic', () => {
    const onRequestResolved = vi.fn();
    renderHook(() => useChatSocket('s1', true, onRequestResolved));
    const ws = lastWS();
    act(() => {
      ws.onopen?.();
      ws.onmessage?.({
        data: JSON.stringify({
          type: 'history',
          protocol: 'claude-stream-json',
          records: [
            {
              ts: 't',
              type: 'control',
              line: { type: 'control_response', response: { request_id: 'r1' } },
            },
          ],
        }),
      });
    });
    expect(onRequestResolved).toHaveBeenCalledWith('r1');
    act(() => {
      ws.onmessage?.({
        data: JSON.stringify({
          type: 'record',
          record: {
            ts: 't',
            type: 'harness',
            line: { type: 'control_cancel_request', request_id: 'r2' },
          },
        }),
      });
    });
    expect(onRequestResolved).toHaveBeenCalledWith('r2');
    expect(onRequestResolved).toHaveBeenCalledTimes(2);
  });
});

describe('activity across a real socket reconnect', () => {
  it.each(['background', 'agent'] as const)(
    'rebuilds %s from durable history, discards buffered deltas and applies a late live result once',
    async (name) => {
      const records = capturedActivity(name);
      const split = records.findIndex(
        (r) => r.type === 'harness' && r.line.subtype === 'task_notification'
      );
      expect(split).toBeGreaterThan(0);
      const prefix = records.slice(0, split);
      const tail = records.slice(split);
      expect(tail.length).toBeGreaterThan(0);
      const delta: ConversationRecord = {
        type: 'harness',
        ts: records[0].ts,
        line: {
          type: 'stream_event',
          event: {
            type: 'content_block_start',
            index: 91,
            content_block: { type: 'text', text: 'transient only' },
          },
        },
      };
      const { result } = renderHook(() => useChatSocket('activity-reconnect', true));
      const original = lastWS();
      const history = (ws: MockWebSocket, records: ConversationRecord[]) =>
        ws.onmessage?.({
          data: JSON.stringify({ type: 'history', protocol: 'claude-stream-json', records }),
        });
      const live = (ws: MockWebSocket, record: ConversationRecord) =>
        ws.onmessage?.({ data: JSON.stringify({ type: 'record', record }) });
      const flush = () =>
        act(async () => {
          await new Promise<void>((resolve) => requestAnimationFrame(() => resolve()));
        });
      act(() => {
        original.onopen?.();
        history(original, []);
        live(original, prefix[0]);
        live(original, delta); // Delivered live, absent from the durable prefix.
        prefix.slice(1).forEach((r) => live(original, r));
      });
      await flush();
      act(() => tail.forEach((r) => live(original, r)));
      await flush();
      const now = Date.parse(tail[0].ts) + 1000;
      const expected = selectActivity(result.current.conversation, { kind: 'connected' }, { now });
      act(() => {
        // A buffered live update from the old socket must not be replayed after history.
        live(original, delta);
        original.onclose?.({ code: 1006 });
      });
      await act(async () => {
        await new Promise((resolve) => setTimeout(resolve, 600));
      });
      expect(result.current.historyLoaded).toBe(false);
      const replacement = lastWS();
      expect(replacement).not.toBe(original);
      act(() => {
        replacement.onopen?.();
        history(replacement, prefix);
        tail.forEach((r) => live(replacement, r));
      });
      await flush();
      const actual = selectActivity(result.current.conversation, { kind: 'connected' }, { now });
      expect(actual).toEqual(expected);
      const task = tail[0];
      if (task.type !== 'harness') throw new Error('missing notification');
      expect(
        actual.rows.filter((row) => row.key.endsWith(':' + String(task.line.task_id)))
      ).toEqual([]);
      const outcome = Object.values(result.current.conversation.activity.operations).find(
        (op) => op.id === String(task.line.task_id)
      );
      expect(outcome?.lifecycle).toBe('finished');
      expect(JSON.stringify(result.current.conversation.items)).not.toContain('transient only');
    }
  );
});
