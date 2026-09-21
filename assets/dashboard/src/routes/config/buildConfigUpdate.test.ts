import { describe, expect, it } from 'vitest';
import { buildConfigUpdate } from './buildConfigUpdate';
import { initialState } from './useConfigForm';

describe('buildConfigUpdate targets', () => {
  it('emits trimmed, non-empty targets arrays', () => {
    const state = {
      ...initialState,
      branchSuggestTargets: [' MiniMax-M3::api ', '', 'GLM-5.3::api'],
      nudgenikTargets: ['MiniMax-M3::api'],
    };
    const update = buildConfigUpdate(state);
    expect(update.branch_suggest?.targets).toEqual(['MiniMax-M3::api', 'GLM-5.3::api']);
    expect(update.nudgenik?.targets).toEqual(['MiniMax-M3::api']);
  });

  it('emits an empty array (disable) when the chain is empty', () => {
    const state = { ...initialState, branchSuggestTargets: [] };
    const update = buildConfigUpdate(state);
    expect(update.branch_suggest?.targets).toEqual([]);
  });

  it('emits the workspace navigation preference', () => {
    const state = { ...initialState, skipEmptyWorkspaceNavigation: true };
    expect(buildConfigUpdate(state).ui?.skip_empty_workspaces).toBe(true);
  });
});

describe('buildConfigUpdate min_free_disk_space_mib', () => {
  it('sends 0 by default', () => {
    const update = buildConfigUpdate({ ...initialState, minFreeDiskSpaceMiB: 0 });
    expect(update.min_free_disk_space_mib).toBe(0);
  });

  it('sends the edited integer value', () => {
    const update = buildConfigUpdate({ ...initialState, minFreeDiskSpaceMiB: 5120 });
    expect(update.min_free_disk_space_mib).toBe(5120);
  });
});
