/**
 * AppShell sidebar session ordering tests.
 *
 * AppShell is a very large component with many context providers (Router,
 * SessionsContext, ConfigContext, SyncContext, KeyboardContext, WebSocket hooks,
 * etc.) that make full render testing impractical for isolated unit tests.
 *
 * The task spec explicitly allows: "if mocking is too complex, create a simpler
 * integration test: just call sortSessionsByTabOrder with the same inputs and
 * verify the result."
 *
 * These tests verify that the function AppShell uses for sidebar ordering
 * (sortSessionsByTabOrder from ../lib/tabOrder) produces the expected custom
 * order when a tab-order key is stored in localStorage — matching the behavior
 * wired up in AppShell.tsx line 890.
 */
import { describe, it, expect, beforeEach } from 'vitest';
import { sortSessionsByTabOrder, TAB_ORDER_KEY_PREFIX } from '../lib/tabOrder';
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

describe('AppShell sidebar — session ordering via sortSessionsByTabOrder', () => {
  const workspaceId = 'ws-sidebar-1';

  beforeEach(() => {
    localStorage.clear();
  });

  it('respects stored custom order [c, b, a] when sessions are [a, b, c]', () => {
    localStorage.setItem(`${TAB_ORDER_KEY_PREFIX}${workspaceId}`, JSON.stringify(['c', 'b', 'a']));

    const sessions = [makeSession('a'), makeSession('b'), makeSession('c')];
    const ordered = sortSessionsByTabOrder(workspaceId, sessions);

    expect(ordered.map((s) => s.id)).toEqual(['c', 'b', 'a']);
  });

  it('appends new sessions after stored order', () => {
    localStorage.setItem(`${TAB_ORDER_KEY_PREFIX}${workspaceId}`, JSON.stringify(['b', 'a']));

    // 'c' is a new session not in stored order
    const sessions = [makeSession('a'), makeSession('b'), makeSession('c')];
    const ordered = sortSessionsByTabOrder(workspaceId, sessions);

    expect(ordered.map((s) => s.id)).toEqual(['b', 'a', 'c']);
  });

  it('falls back to original order when no stored order exists', () => {
    const sessions = [makeSession('a'), makeSession('b'), makeSession('c')];
    const ordered = sortSessionsByTabOrder(workspaceId, sessions);

    expect(ordered.map((s) => s.id)).toEqual(['a', 'b', 'c']);
  });

  it('omits disposed sessions (present in stored order but not in sessions array)', () => {
    localStorage.setItem(`${TAB_ORDER_KEY_PREFIX}${workspaceId}`, JSON.stringify(['a', 'b', 'c']));

    // 'b' was disposed
    const sessions = [makeSession('a'), makeSession('c')];
    const ordered = sortSessionsByTabOrder(workspaceId, sessions);

    expect(ordered.map((s) => s.id)).toEqual(['a', 'c']);
  });
});
