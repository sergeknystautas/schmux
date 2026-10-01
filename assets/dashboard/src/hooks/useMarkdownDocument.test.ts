import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { renderHook, act } from '@testing-library/react';
import useMarkdownDocument from './useMarkdownDocument';

class MockWebSocket {
  static instances: MockWebSocket[] = [];
  onopen: (() => void) | null = null;
  onmessage: ((ev: { data: string }) => void) | null = null;
  onclose: ((ev: { code: number; reason: string }) => void) | null = null;
  onerror: (() => void) | null = null;
  close = vi.fn();
  send = vi.fn();
  constructor(public url: string) {
    MockWebSocket.instances.push(this);
  }
}

beforeEach(() => {
  MockWebSocket.instances = [];
  vi.stubGlobal('WebSocket', MockWebSocket);
  vi.useFakeTimers();
});

afterEach(() => {
  vi.useRealTimers();
  vi.restoreAllMocks();
});

const lastWS = () => MockWebSocket.instances[MockWebSocket.instances.length - 1];
const open = (ws: MockWebSocket) => ws.onopen?.();
const doc = (ws: MockWebSocket, content: string, reply?: string) =>
  ws.onmessage?.({
    data: JSON.stringify({
      type: 'document',
      content,
      revision: 'sha256:x',
      ...(reply ? { reply } : {}),
    }),
  });
const sentSaves = (ws: MockWebSocket) => ws.send.mock.calls.map((c) => JSON.parse(c[0] as string));

function mount() {
  const hook = renderHook(() => useMarkdownDocument('ws-1', 'docs/notes.md'));
  const ws = lastWS();
  act(() => open(ws));
  act(() => doc(ws, '# a'));
  return { hook, ws };
}

describe('useMarkdownDocument', () => {
  it('connects to the markdown route with the encoded path', () => {
    mount();
    expect(lastWS().url).toBe(`ws://${window.location.host}/ws/markdown/ws-1/docs%2Fnotes.md`);
  });

  it('debounces 500 ms and sends one save with base and draft', () => {
    const { hook, ws } = mount();
    act(() => hook.result.current.onEdit('# ab'));
    act(() => vi.advanceTimersByTime(499));
    expect(ws.send).not.toHaveBeenCalled();
    act(() => vi.advanceTimersByTime(1));
    const saves = sentSaves(ws);
    expect(saves).toHaveLength(1);
    expect(saves[0]).toMatchObject({ type: 'save', base: '# a', draft: '# ab' });
    expect(saves[0].id).toEqual(expect.any(String));
    expect(hook.result.current.status).toBe('saving');
  });

  it('keeps one save in flight and folds later typing into the next', () => {
    const { hook, ws } = mount();
    act(() => hook.result.current.onEdit('# ab'));
    act(() => vi.advanceTimersByTime(500));
    act(() => hook.result.current.onEdit('# abc'));
    act(() => vi.advanceTimersByTime(500));
    expect(sentSaves(ws)).toHaveLength(1);
    act(() => doc(ws, '# ab', sentSaves(ws)[0].id));
    const saves = sentSaves(ws);
    expect(saves).toHaveLength(2);
    expect(saves[1]).toMatchObject({ base: '# ab', draft: '# abc' });
    expect(hook.result.current.draft).toBe('# abc');
  });

  it('adopts an external document when clean and saves at once when dirty', () => {
    const { hook, ws } = mount();
    act(() => doc(ws, '# agent'));
    expect(hook.result.current.draft).toBe('# agent');
    act(() => hook.result.current.onEdit('# agent!'));
    act(() => doc(ws, '# agent2'));
    expect(sentSaves(ws)).toHaveLength(1);
    expect(sentSaves(ws)[0]).toMatchObject({ base: '# agent', draft: '# agent!' });
  });

  it('reconnects with backoff after a non-terminal close and resends the in-flight save', () => {
    vi.spyOn(Math, 'random').mockReturnValue(0);
    const { hook, ws } = mount();
    act(() => hook.result.current.onEdit('# ab'));
    act(() => vi.advanceTimersByTime(500));
    const id = sentSaves(ws)[0].id;
    act(() => ws.onclose?.({ code: 1008, reason: 'write_failed' }));
    expect(hook.result.current.status).toBe('error');
    expect(hook.result.current.reason).toBe('write_failed');
    expect(MockWebSocket.instances).toHaveLength(1);
    act(() => vi.advanceTimersByTime(2100));
    expect(MockWebSocket.instances).toHaveLength(2);
    const ws2 = lastWS();
    act(() => open(ws2));
    act(() => doc(ws2, '# a'));
    expect(sentSaves(ws2)).toEqual([expect.objectContaining({ id, base: '# a', draft: '# ab' })]);
  });

  it('does not reconnect after a terminal close', () => {
    const { hook, ws } = mount();
    act(() => ws.onclose?.({ code: 1008, reason: 'too_large' }));
    act(() => vi.advanceTimersByTime(60_000));
    expect(MockWebSocket.instances).toHaveLength(1);
    expect(hook.result.current.reason).toBe('too_large');
  });

  it('closes the socket on unmount', () => {
    const { hook, ws } = mount();
    hook.unmount();
    expect(ws.close).toHaveBeenCalled();
  });

  it('starts from a clean state when the path changes, never saving the old draft onto the new file', () => {
    // The router reuses one page element for /diff/:ws/md/:path, so the hook
    // sees a prop change rather than a remount when navigating between files.
    const hook = renderHook(({ path }) => useMarkdownDocument('ws-1', path), {
      initialProps: { path: 'a.md' },
    });
    const wsA = lastWS();
    act(() => open(wsA));
    act(() => doc(wsA, '# a'));
    act(() => hook.result.current.onEdit('# a dirty'));

    hook.rerender({ path: 'b.md' });
    expect(wsA.close).toHaveBeenCalled();
    const wsB = lastWS();
    expect(wsB).not.toBe(wsA);
    act(() => open(wsB));
    act(() => doc(wsB, '# b'));
    act(() => vi.advanceTimersByTime(1000));

    expect(sentSaves(wsB)).toEqual([]);
    expect(hook.result.current.draft).toBe('# b');
    expect(hook.result.current.status).toBe('saved');
  });
});
