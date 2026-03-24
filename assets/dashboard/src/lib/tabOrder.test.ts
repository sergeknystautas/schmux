import { describe, it, expect, beforeEach, vi } from 'vitest';
import {
  sortSessionsByTabOrder,
  saveTabOrder,
  TAB_ORDER_KEY_PREFIX,
  TAB_ORDER_CHANGED_EVENT,
} from './tabOrder';
import type { SessionResponse } from './types';

// Minimal session factory
function makeSession(id: string): SessionResponse {
  return {
    id,
    target: `target-${id}`,
    branch: 'main',
    created_at: '2026-01-01T00:00:00Z',
    running: true,
    attach_cmd: `tmux attach -t ${id}`,
  };
}

describe('sortSessionsByTabOrder', () => {
  beforeEach(() => {
    localStorage.clear();
  });

  it('returns original order when no stored order exists', () => {
    const sessions = [makeSession('a'), makeSession('b'), makeSession('c')];
    const result = sortSessionsByTabOrder('ws-1', sessions);
    expect(result.map((s) => s.id)).toEqual(['a', 'b', 'c']);
  });

  it('sorts sessions by stored order', () => {
    localStorage.setItem(`${TAB_ORDER_KEY_PREFIX}ws-1`, JSON.stringify(['c', 'a', 'b']));
    const sessions = [makeSession('a'), makeSession('b'), makeSession('c')];
    const result = sortSessionsByTabOrder('ws-1', sessions);
    expect(result.map((s) => s.id)).toEqual(['c', 'a', 'b']);
  });

  it('appends new sessions not in stored order at the end', () => {
    localStorage.setItem(`${TAB_ORDER_KEY_PREFIX}ws-1`, JSON.stringify(['b', 'a']));
    const sessions = [makeSession('a'), makeSession('b'), makeSession('c')];
    const result = sortSessionsByTabOrder('ws-1', sessions);
    expect(result.map((s) => s.id)).toEqual(['b', 'a', 'c']);
  });

  it('omits disposed sessions from result (does not write to localStorage)', () => {
    localStorage.setItem(`${TAB_ORDER_KEY_PREFIX}ws-1`, JSON.stringify(['a', 'b', 'c']));
    const sessions = [makeSession('a'), makeSession('c')]; // 'b' disposed
    const result = sortSessionsByTabOrder('ws-1', sessions);
    expect(result.map((s) => s.id)).toEqual(['a', 'c']);
    // localStorage should NOT be modified
    expect(localStorage.getItem(`${TAB_ORDER_KEY_PREFIX}ws-1`)).toBe(
      JSON.stringify(['a', 'b', 'c'])
    );
  });

  it('returns original order when workspaceId is undefined', () => {
    const sessions = [makeSession('a'), makeSession('b')];
    const result = sortSessionsByTabOrder(undefined, sessions);
    expect(result.map((s) => s.id)).toEqual(['a', 'b']);
  });

  it('returns original order when localStorage has invalid JSON', () => {
    localStorage.setItem(`${TAB_ORDER_KEY_PREFIX}ws-1`, 'not-json');
    const sessions = [makeSession('a'), makeSession('b')];
    const result = sortSessionsByTabOrder('ws-1', sessions);
    expect(result.map((s) => s.id)).toEqual(['a', 'b']);
  });

  it('returns original order when stored value is not an array', () => {
    localStorage.setItem(`${TAB_ORDER_KEY_PREFIX}ws-1`, JSON.stringify({ order: ['a'] }));
    const sessions = [makeSession('a'), makeSession('b')];
    const result = sortSessionsByTabOrder('ws-1', sessions);
    expect(result.map((s) => s.id)).toEqual(['a', 'b']);
  });

  it('handles empty sessions array', () => {
    localStorage.setItem(`${TAB_ORDER_KEY_PREFIX}ws-1`, JSON.stringify(['a', 'b']));
    const result = sortSessionsByTabOrder('ws-1', []);
    expect(result).toEqual([]);
  });
});

describe('saveTabOrder', () => {
  beforeEach(() => {
    localStorage.clear();
  });

  it('persists order to localStorage and dispatches change event', () => {
    const listener = vi.fn();
    window.addEventListener(TAB_ORDER_CHANGED_EVENT, listener);

    saveTabOrder('ws-1', ['c', 'b', 'a']);

    expect(localStorage.getItem(`${TAB_ORDER_KEY_PREFIX}ws-1`)).toBe(
      JSON.stringify(['c', 'b', 'a'])
    );
    expect(listener).toHaveBeenCalledTimes(1);

    window.removeEventListener(TAB_ORDER_CHANGED_EVENT, listener);
  });
});
