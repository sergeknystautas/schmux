import { describe, it, expect } from 'vitest';
import { getQuickLaunchItems } from './quicklaunch';

describe('getQuickLaunchItems', () => {
  it('returns empty array for empty inputs', () => {
    expect(getQuickLaunchItems([], [])).toEqual([]);
  });

  it('returns global items before workspace items', () => {
    const result = getQuickLaunchItems(['global1'], ['workspace1']);
    expect(result[0].scope).toBe('global');
    expect(result[1].scope).toBe('workspace');
  });

  it('sorts alphabetically within each scope', () => {
    const result = getQuickLaunchItems(['zulu', 'alpha'], ['bravo', 'alpha2']);
    const globalItems = result.filter((i) => i.scope === 'global');
    const workspaceItems = result.filter((i) => i.scope === 'workspace');
    expect(globalItems.map((i) => i.name)).toEqual(['alpha', 'zulu']);
    expect(workspaceItems.map((i) => i.name)).toEqual(['alpha2', 'bravo']);
  });

  it('deduplicates - global wins over workspace', () => {
    const result = getQuickLaunchItems(['shared'], ['shared', 'unique']);
    expect(result).toEqual([
      { name: 'shared', scope: 'global' },
      { name: 'unique', scope: 'workspace' },
    ]);
  });

  it('trims whitespace and filters blanks', () => {
    const result = getQuickLaunchItems(['  alpha  ', '', '  '], ['  bravo  ']);
    expect(result).toEqual([
      { name: 'alpha', scope: 'global' },
      { name: 'bravo', scope: 'workspace' },
    ]);
  });

  it('handles null/undefined arrays', () => {
    expect(getQuickLaunchItems(null as any, undefined as any)).toEqual([]);
  });
});
