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
});
