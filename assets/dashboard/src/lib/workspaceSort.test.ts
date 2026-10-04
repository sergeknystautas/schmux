import { describe, it, expect } from 'vitest';
import { isSessionInProgress, sortWorkspaces } from './workspaceSort';
import type { SessionResponse, WorkspaceResponse } from './types';

const identityRepoName = (repo: string) => repo;

function makeSession(
  id: string,
  nudgeState: string | undefined,
  lastOutputAt: string,
  opts: { running?: boolean; target?: string } = {}
): SessionResponse {
  return {
    id,
    target: opts.target ?? 'claude',
    branch: 'main',
    created_at: '2026-10-04T09:00:00Z',
    last_output_at: lastOutputAt,
    running: opts.running ?? true,
    attach_cmd: '',
    nudge_state: nudgeState,
  };
}

function makeWorkspace(
  id: string,
  sessions: SessionResponse[],
  opts: { backburner?: boolean } = {}
): WorkspaceResponse {
  return {
    id,
    repo: 'repo',
    branch: id,
    path: `/workspaces/${id}`,
    session_count: sessions.length,
    sessions,
    ahead: 0,
    behind: 0,
    lines_added: 0,
    lines_removed: 0,
    files_changed: 0,
    backburner: opts.backburner,
  };
}

// Mirrors AppShell: nudgenik enabled, "make" is the only command run target.
const commandTargets = ['make'];
const inProgress = (session: SessionResponse) =>
  isSessionInProgress(session, {
    nudgenikEnabled: true,
    isPromptable: !commandTargets.includes(session.target),
  });

const sortedIds = (workspaces: WorkspaceResponse[], mode: 'alpha' | 'time', bb = false) =>
  sortWorkspaces(workspaces, mode, identityRepoName, bb, inProgress).map((w) => w.id);

describe('Workspace time sort in-progress filter', () => {
  describe('isSessionInProgress', () => {
    const at = '2026-10-04T10:00:00Z';

    it.each(['Working', 'Background'])('treats running %s as in progress', (state) => {
      expect(inProgress(makeSession('s', state, at))).toBe(true);
    });

    it.each([
      'Needs Input',
      'Needs Attention',
      'Needs Feature Clarification',
      'Completed',
      'Error',
      'Idle',
      'Some Unknown State',
    ])('treats running %s as not in progress', (state) => {
      expect(inProgress(makeSession('s', state, at))).toBe(false);
    });

    it.each(['Working', 'Background'])('treats stopped %s as not in progress', (state) => {
      expect(inProgress(makeSession('s', state, at, { running: false }))).toBe(false);
    });

    it('treats a running promptable session with no nudge as in progress when nudgenik is enabled', () => {
      expect(
        isSessionInProgress(makeSession('s', undefined, at), {
          nudgenikEnabled: true,
          isPromptable: true,
        })
      ).toBe(true);
    });

    it('treats a running command session with no nudge as not in progress', () => {
      expect(
        isSessionInProgress(makeSession('s', undefined, at, { target: 'make' }), {
          nudgenikEnabled: true,
          isPromptable: false,
        })
      ).toBe(false);
    });

    it('treats a running promptable session with no nudge as not in progress when nudgenik is disabled', () => {
      expect(
        isSessionInProgress(makeSession('s', undefined, at), {
          nudgenikEnabled: false,
          isPromptable: true,
        })
      ).toBe(false);
    });
  });

  describe('time mode', () => {
    it.each(['Working', 'Background'])(
      'applies a 60 minute penalty to a newer all-%s workspace',
      (state) => {
        const workspaces = [
          makeWorkspace('ws-busy', [makeSession('s1', state, '2026-10-04T10:02:00Z')]),
          makeWorkspace('ws-done', [makeSession('s2', 'Completed', '2026-10-04T10:01:00Z')]),
        ];
        expect(sortedIds(workspaces, 'time')).toEqual(['ws-done', 'ws-busy']);
      }
    );

    it('keeps a busy workspace above activity more than 60 minutes older', () => {
      const workspaces = [
        makeWorkspace('ws-busy', [makeSession('s1', 'Working', '2026-10-04T12:00:00Z')]),
        makeWorkspace('ws-done', [makeSession('s2', 'Completed', '2026-10-04T10:00:00Z')]),
      ];
      expect(sortedIds(workspaces, 'time')).toEqual(['ws-busy', 'ws-done']);
    });

    it('uses the alphabetical tiebreak when the 60 minute penalty creates an exact tie', () => {
      const workspaces = [
        makeWorkspace('ws-a-busy', [makeSession('s1', 'Working', '2026-10-04T11:00:00Z')]),
        makeWorkspace('ws-z-done', [makeSession('s2', 'Completed', '2026-10-04T10:00:00Z')]),
      ];
      expect(sortedIds(workspaces, 'time')).toEqual(['ws-a-busy', 'ws-z-done']);
    });

    it('ranks a workspace by its best timestamp after penalties', () => {
      const workspaces = [
        makeWorkspace('ws-mixed', [
          makeSession('s1', 'Working', '2026-10-04T10:05:00Z'),
          makeSession('s2', 'Completed', '2026-10-04T10:01:00Z'),
        ]),
        makeWorkspace('ws-other', [makeSession('s3', 'Completed', '2026-10-04T10:02:00Z')]),
      ];
      expect(sortedIds(workspaces, 'time')).toEqual(['ws-other', 'ws-mixed']);
    });

    it('keeps an all-in-progress workspace above a no-session workspace', () => {
      const workspaces = [
        makeWorkspace('ws-c-done', [makeSession('s1', 'Completed', '2026-10-04T10:00:00Z')]),
        makeWorkspace('ws-b-busy', [
          makeSession('s2', 'Working', '2026-10-04T10:05:00Z'),
          makeSession('s3', 'Background', '2026-10-04T10:06:00Z'),
        ]),
        makeWorkspace('ws-a-empty', []),
      ];
      expect(sortedIds(workspaces, 'time')).toEqual(['ws-c-done', 'ws-b-busy', 'ws-a-empty']);
    });

    it('keeps backburner partitioning ahead of a newer eligible timestamp', () => {
      const workspaces = [
        makeWorkspace('ws-bb', [makeSession('s1', 'Completed', '2026-10-04T10:05:00Z')], {
          backburner: true,
        }),
        makeWorkspace('ws-front', [makeSession('s2', 'Completed', '2026-10-04T10:01:00Z')]),
      ];
      expect(sortedIds(workspaces, 'time', true)).toEqual(['ws-front', 'ws-bb']);
    });
  });

  describe('alpha mode', () => {
    it('ignores Working sessions', () => {
      const workspaces = [
        makeWorkspace('ws-b', [makeSession('s1', 'Completed', '2026-10-04T10:00:00Z')]),
        makeWorkspace('ws-a', [makeSession('s2', 'Working', '2026-10-04T10:05:00Z')]),
      ];
      const calls: string[] = [];
      const sorted = sortWorkspaces(workspaces, 'alpha', identityRepoName, false, (session) => {
        calls.push(session.id);
        return true;
      });
      expect(sorted.map((w) => w.id)).toEqual(['ws-a', 'ws-b']);
      expect(calls).toEqual([]);
    });
  });
});
