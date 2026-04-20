import { describe, it, expect } from 'vitest';
import { workspaceDisplayLabel } from './workspace-display';
import type { WorkspaceResponseItem } from './types.generated';

const baseWs: WorkspaceResponseItem = {
  id: 'myrepo-007',
  repo: 'r',
  branch: '',
  path: '/p',
  status: 'running',
  commits_synced_with_remote: false,
  default_branch_orphaned: false,
} as WorkspaceResponseItem;

describe('workspaceDisplayLabel', () => {
  it('returns label when set', () => {
    expect(workspaceDisplayLabel({ ...baseWs, label: 'My label', branch: 'main' })).toBe(
      'My label'
    );
  });

  it('returns computedBranch when label is empty', () => {
    expect(workspaceDisplayLabel({ ...baseWs, branch: 'main' }, 'feature/x')).toBe('feature/x');
  });

  it('returns branch when label and computedBranch are empty', () => {
    expect(workspaceDisplayLabel({ ...baseWs, branch: 'main' })).toBe('main');
  });

  it('returns id when label, computedBranch, and branch are all empty (sapling fallback)', () => {
    expect(workspaceDisplayLabel({ ...baseWs, branch: '' })).toBe('myrepo-007');
  });

  it('treats whitespace-only label as empty', () => {
    expect(workspaceDisplayLabel({ ...baseWs, label: '   ', branch: 'main' })).toBe('main');
  });
});
