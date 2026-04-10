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

  it('returns only global items when workspace is empty', () => {
    const result = getQuickLaunchItems(['charlie', 'alpha', 'bravo'], []);
    expect(result).toEqual([
      { name: 'alpha', scope: 'global' },
      { name: 'bravo', scope: 'global' },
      { name: 'charlie', scope: 'global' },
    ]);
  });

  it('returns only workspace items when global is empty', () => {
    const result = getQuickLaunchItems([], ['zulu', 'mike']);
    expect(result).toEqual([
      { name: 'mike', scope: 'workspace' },
      { name: 'zulu', scope: 'workspace' },
    ]);
  });

  it('removes duplicate within the same scope', () => {
    const result = getQuickLaunchItems(['alpha', 'alpha'], ['bravo', 'bravo']);
    expect(result).toEqual([
      { name: 'alpha', scope: 'global' },
      { name: 'bravo', scope: 'workspace' },
    ]);
  });

  it('filters whitespace-only names from both scopes', () => {
    const result = getQuickLaunchItems(['  ', '\t', ''], [' ', '', 'valid']);
    expect(result).toEqual([{ name: 'valid', scope: 'workspace' }]);
  });

  it('trims leading and trailing whitespace from names', () => {
    const result = getQuickLaunchItems(['  padded  '], ['  spaced  ']);
    expect(result).toEqual([
      { name: 'padded', scope: 'global' },
      { name: 'spaced', scope: 'workspace' },
    ]);
  });

  it('deduplicates trimmed names across scopes', () => {
    // global "  foo  " and workspace "foo" are the same after trimming
    const result = getQuickLaunchItems(['  foo  '], ['foo', 'bar']);
    expect(result).toEqual([
      { name: 'foo', scope: 'global' },
      { name: 'bar', scope: 'workspace' },
    ]);
  });
});
