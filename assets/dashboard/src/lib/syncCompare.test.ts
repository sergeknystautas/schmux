import { describe, it, expect } from 'vitest';
import { compareScreens } from './syncCompare';

describe('compareScreens', () => {
  it('returns match for identical content', () => {
    const lines = ['hello', 'world'];
    expect(compareScreens(lines, lines)).toEqual({ match: true, diffRows: [] });
  });

  it('returns match when trailing whitespace differs', () => {
    const xterm = ['hello   ', 'world  '];
    const sync = ['hello', 'world'];
    expect(compareScreens(xterm, sync)).toEqual({ match: true, diffRows: [] });
  });

  it('returns mismatch when a row differs', () => {
    const xterm = ['hello', 'world'];
    const sync = ['hello', 'earth'];
    const result = compareScreens(xterm, sync);
    expect(result.match).toBe(false);
    expect(result.diffRows).toEqual([1]);
  });

  it('returns match when trailing empty rows differ', () => {
    const xterm = ['hello', 'world', '', ''];
    const sync = ['hello', 'world'];
    expect(compareScreens(xterm, sync)).toEqual({ match: true, diffRows: [] });
  });

  it('returns mismatch for shifted content', () => {
    const xterm = ['', 'hello', 'world'];
    const sync = ['hello', 'world', ''];
    const result = compareScreens(xterm, sync);
    expect(result.match).toBe(false);
    expect(result.diffRows).toContain(0);
    expect(result.diffRows).toContain(1);
  });

  it('returns skip when row counts differ significantly', () => {
    const xterm = Array(53).fill('');
    const sync = Array(40).fill('');
    const result = compareScreens(xterm, sync);
    expect(result.match).toBe(false);
    expect(result.skip).toBe(true);
  });

  it('does not skip when dimension diff is exactly 2 (within tolerance)', () => {
    const xterm = Array(24)
      .fill('')
      .map((_, i) => `line ${i}`);
    const sync = Array(26)
      .fill('')
      .map((_, i) => `line ${i}`);
    const result = compareScreens(xterm, sync);
    // diff=2, within ±2 tolerance → should NOT skip
    expect(result.skip).toBeUndefined();
  });

  it('skips when dimension diff is exactly 3 (exceeds tolerance)', () => {
    const xterm = Array(24)
      .fill('')
      .map((_, i) => `line ${i}`);
    const sync = Array(27)
      .fill('')
      .map((_, i) => `line ${i}`);
    const result = compareScreens(xterm, sync);
    // diff=3, exceeds ±2 tolerance → should skip
    expect(result.skip).toBe(true);
  });

  it('handles the ghost cursor-forward case', () => {
    const xterm = ['⏺      1 file'];
    const sync = [''];
    const result = compareScreens(xterm, sync);
    expect(result.match).toBe(false);
    expect(result.diffRows).toEqual([0]);
  });
});
