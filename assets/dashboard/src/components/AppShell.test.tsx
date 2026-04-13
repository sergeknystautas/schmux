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
import { sortWorkspaces } from '../lib/workspaceSort';
import type { SessionResponse, WorkspaceResponse } from '../lib/types';

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

// --- Workspace backburner sorting tests ---

function makeWorkspace(
  id: string,
  branch: string,
  opts: { backburner?: boolean; repo?: string } = {}
): WorkspaceResponse {
  return {
    id,
    repo: opts.repo || `https://example.com/${branch}.git`,
    branch,
    path: `/workspaces/${id}`,
    session_count: 0,
    sessions: [],
    ahead: 0,
    behind: 0,
    lines_added: 0,
    lines_removed: 0,
    files_changed: 0,
    backburner: opts.backburner,
  };
}

const identityRepoName = (url: string) => url;

describe('backburner sorting', () => {
  it('sorts backburnered workspaces to bottom in alpha mode', () => {
    const workspaces = [
      makeWorkspace('ws-alpha', 'alpha'),
      makeWorkspace('ws-charlie', 'charlie', { backburner: true }),
      makeWorkspace('ws-bravo', 'bravo'),
    ];

    const sorted = sortWorkspaces(workspaces, 'alpha', identityRepoName, true);
    expect(sorted.map((w) => w.id)).toEqual(['ws-alpha', 'ws-bravo', 'ws-charlie']);
  });

  it('preserves alphabetical order within each group', () => {
    const workspaces = [
      makeWorkspace('ws-delta', 'delta', { backburner: true }),
      makeWorkspace('ws-alpha', 'alpha'),
      makeWorkspace('ws-charlie', 'charlie', { backburner: true }),
      makeWorkspace('ws-bravo', 'bravo'),
    ];

    const sorted = sortWorkspaces(workspaces, 'alpha', identityRepoName, true);
    expect(sorted.map((w) => w.id)).toEqual(['ws-alpha', 'ws-bravo', 'ws-charlie', 'ws-delta']);
  });

  it('does not partition when backburner is disabled', () => {
    const workspaces = [
      makeWorkspace('ws-delta', 'delta', { backburner: true }),
      makeWorkspace('ws-alpha', 'alpha'),
      makeWorkspace('ws-charlie', 'charlie', { backburner: true }),
      makeWorkspace('ws-bravo', 'bravo'),
    ];

    const sorted = sortWorkspaces(workspaces, 'alpha', identityRepoName, false);
    // Pure alphabetical, ignoring backburner flag
    expect(sorted.map((w) => w.id)).toEqual(['ws-alpha', 'ws-bravo', 'ws-charlie', 'ws-delta']);
  });

  it('sorts backburnered workspaces to bottom in time mode', () => {
    const now = Date.now();
    const workspaces = [
      makeWorkspace('ws-alpha', 'alpha'),
      makeWorkspace('ws-charlie', 'charlie', { backburner: true }),
      makeWorkspace('ws-bravo', 'bravo'),
    ];

    // Give charlie a more recent activity — it should still sort below non-bb
    workspaces[1].sessions = [
      {
        id: 's1',
        target: 't',
        branch: 'main',
        created_at: new Date(now).toISOString(),
        running: true,
        attach_cmd: '',
        last_output_at: new Date(now).toISOString(),
      },
    ];

    const sorted = sortWorkspaces(workspaces, 'time', identityRepoName, true);
    // alpha and bravo have no sessions (tied) so sorted alphabetically,
    // charlie has recent activity but is backburnered so goes last
    expect(sorted.map((w) => w.id)).toEqual(['ws-alpha', 'ws-bravo', 'ws-charlie']);
  });
});
