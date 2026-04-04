import { describe, it, expect, beforeEach } from 'vitest';
import { renderHook, act } from '@testing-library/react';
import { useTabOrder } from './useTabOrder';
import { TAB_ORDER_KEY_PREFIX } from '../lib/tabOrder';
import type { SessionResponse } from '../lib/types';

function makeSession(id: string): SessionResponse {
  return {
    id,
    target: `target-${id}`,
    branch: 'main',
    created_at: '2026-01-01T00:00:00Z',
    running: true,
    attach_cmd: `tmux -L schmux attach -t "=${id}"`,
  };
}

describe('useTabOrder', () => {
  beforeEach(() => {
    localStorage.clear();
  });

  it('returns sessions sorted by stored order', () => {
    localStorage.setItem(`${TAB_ORDER_KEY_PREFIX}ws-1`, JSON.stringify(['c', 'b', 'a']));
    const sessions = [makeSession('a'), makeSession('b'), makeSession('c')];
    const { result } = renderHook(() => useTabOrder('ws-1', sessions));
    expect(result.current.orderedSessions.map((s) => s.id)).toEqual(['c', 'b', 'a']);
  });

  it('returns original order when workspaceId is undefined', () => {
    const sessions = [makeSession('a'), makeSession('b')];
    const { result } = renderHook(() => useTabOrder(undefined, sessions));
    expect(result.current.orderedSessions.map((s) => s.id)).toEqual(['a', 'b']);
  });

  it('reorder updates localStorage and returns new order', () => {
    const sessions = [makeSession('a'), makeSession('b'), makeSession('c')];
    const { result } = renderHook(() => useTabOrder('ws-1', sessions));

    act(() => {
      result.current.reorder('a', 'c');
    });

    expect(result.current.orderedSessions.map((s) => s.id)).toEqual(['b', 'c', 'a']);
    const stored = JSON.parse(localStorage.getItem(`${TAB_ORDER_KEY_PREFIX}ws-1`)!);
    expect(stored).toEqual(['b', 'c', 'a']);
  });

  it('reorder is a no-op when workspaceId is undefined', () => {
    const sessions = [makeSession('a'), makeSession('b')];
    const { result } = renderHook(() => useTabOrder(undefined, sessions));

    act(() => {
      result.current.reorder('a', 'b');
    });

    expect(result.current.orderedSessions.map((s) => s.id)).toEqual(['a', 'b']);
  });

  it('reorder discards when activeId does not exist in sessions', () => {
    const sessions = [makeSession('a'), makeSession('b')];
    const { result } = renderHook(() => useTabOrder('ws-1', sessions));

    act(() => {
      result.current.reorder('gone', 'b');
    });

    expect(result.current.orderedSessions.map((s) => s.id)).toEqual(['a', 'b']);
    expect(localStorage.getItem(`${TAB_ORDER_KEY_PREFIX}ws-1`)).toBeNull();
  });

  it('freezes session list during active drag', () => {
    const sessions = [makeSession('a'), makeSession('b')];
    const { result, rerender } = renderHook(({ sessions }) => useTabOrder('ws-1', sessions), {
      initialProps: { sessions },
    });

    // Start drag
    act(() => {
      result.current.startDrag();
    });

    // Simulate WebSocket update: new session arrives
    const updatedSessions = [makeSession('a'), makeSession('b'), makeSession('c')];
    rerender({ sessions: updatedSessions });

    // Should still show frozen snapshot (no 'c')
    expect(result.current.orderedSessions.map((s) => s.id)).toEqual(['a', 'b']);

    // End drag — unfreezes, picks up new session
    act(() => {
      result.current.endDrag();
    });

    expect(result.current.orderedSessions.map((s) => s.id)).toEqual(['a', 'b', 'c']);
  });

  it('reorder during freeze persists correctly and unfreezes with new order', () => {
    const sessions = [makeSession('a'), makeSession('b'), makeSession('c')];
    const { result } = renderHook(() => useTabOrder('ws-1', sessions));

    act(() => {
      result.current.startDrag();
    });

    act(() => {
      result.current.reorder('a', 'c');
    });

    // After reorder, freeze is released and new order is visible
    expect(result.current.orderedSessions.map((s) => s.id)).toEqual(['b', 'c', 'a']);
  });

  it('prunes disposed sessions from localStorage on reorder', () => {
    localStorage.setItem(`${TAB_ORDER_KEY_PREFIX}ws-1`, JSON.stringify(['a', 'b', 'c']));
    const sessions = [makeSession('a'), makeSession('c')]; // 'b' disposed
    const { result } = renderHook(() => useTabOrder('ws-1', sessions));

    act(() => {
      result.current.reorder('c', 'a');
    });

    // Stored order should not include 'b'
    const stored = JSON.parse(localStorage.getItem(`${TAB_ORDER_KEY_PREFIX}ws-1`)!);
    expect(stored).toEqual(['c', 'a']);
  });
});
