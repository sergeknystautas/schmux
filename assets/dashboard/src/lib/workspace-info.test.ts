import { describe, it, expect } from 'vitest';
import { buildWorkspaceInfoRows } from './workspace-info';
import type { WorkspaceResponse } from './types';

function makeWorkspace(overrides: Partial<WorkspaceResponse> = {}): WorkspaceResponse {
  return {
    id: 'ws-1',
    repo: 'https://github.com/acme/repo.git',
    branch: 'feature/foo',
    path: '/Users/me/code/workspaces/ws-1',
    session_count: 0,
    sessions: [],
    ahead: 0,
    behind: 0,
    lines_added: 0,
    lines_removed: 0,
    files_changed: 0,
    ...overrides,
  };
}

describe('buildWorkspaceInfoRows', () => {
  it('returns branch, name, repo (small), and commits rows in order for a minimal git workspace', () => {
    const rows = buildWorkspaceInfoRows(makeWorkspace());
    expect(rows).toEqual([
      { kind: 'text', value: 'feature/foo' },
      { kind: 'text', value: 'ws-1' },
      { kind: 'text', value: 'https://github.com/acme/repo.git', small: true },
      { kind: 'commits', behind: 0, ahead: 0 },
    ]);
  });

  it('uses workspace.label for the name row when non-empty', () => {
    const rows = buildWorkspaceInfoRows(makeWorkspace({ label: 'My Label' }));
    expect(rows[1]).toEqual({ kind: 'text', value: 'My Label' });
  });

  it('falls back to workspace.id when label is empty string', () => {
    const rows = buildWorkspaceInfoRows(makeWorkspace({ label: '' }));
    expect(rows[1]).toEqual({ kind: 'text', value: 'ws-1' });
  });

  it('carries behind and ahead verbatim on the commits row', () => {
    const rows = buildWorkspaceInfoRows(makeWorkspace({ ahead: 3, behind: 1 }));
    expect(rows.find((r) => r.kind === 'commits')).toEqual({
      kind: 'commits',
      behind: 1,
      ahead: 3,
    });
  });

  it('shows 0/0 on the commits row when synced', () => {
    const rows = buildWorkspaceInfoRows(makeWorkspace());
    expect(rows.find((r) => r.kind === 'commits')).toEqual({
      kind: 'commits',
      behind: 0,
      ahead: 0,
    });
  });

  it('omits the commits row for sapling workspaces', () => {
    const rows = buildWorkspaceInfoRows(makeWorkspace({ vcs: 'sapling', ahead: 5, behind: 5 }));
    expect(rows.find((r) => r.kind === 'commits')).toBeUndefined();
    expect(rows).toHaveLength(3);
  });

  it('treats undefined vcs as git', () => {
    const rows = buildWorkspaceInfoRows(makeWorkspace({ ahead: 1 }));
    expect(rows.find((r) => r.kind === 'commits')).toEqual({
      kind: 'commits',
      behind: 0,
      ahead: 1,
    });
  });

  it('does not emit path or Remote rows', () => {
    const rows = buildWorkspaceInfoRows(
      makeWorkspace({
        remote_branch_exists: true,
        commits_synced_with_remote: false,
        local_unique_commits: 2,
        remote_unique_commits: 5,
      })
    );
    expect(
      rows.find((r) => r.kind === 'text' && r.value === '/Users/me/code/workspaces/ws-1')
    ).toBeUndefined();
    expect(rows.find((r) => r.kind === 'text' && r.value.startsWith('Remote'))).toBeUndefined();
  });

  it('marks the repo row as small', () => {
    const rows = buildWorkspaceInfoRows(makeWorkspace());
    const repoRow = rows.find(
      (r) => r.kind === 'text' && r.value === 'https://github.com/acme/repo.git'
    );
    expect(repoRow).toEqual({
      kind: 'text',
      value: 'https://github.com/acme/repo.git',
      small: true,
    });
  });
});
