import { describe, it, expect, beforeEach, vi, afterEach } from 'vitest';
import {
  getUncheckedDiffFilesKey,
  loadUncheckedDiffFiles,
  saveUncheckedDiffFiles,
} from './diff-unchecked-files';

const KEY = getUncheckedDiffFilesKey('ws-1');

describe('diff-unchecked-files', () => {
  beforeEach(() => {
    localStorage.clear();
  });
  afterEach(() => {
    vi.restoreAllMocks();
  });

  it('uses a workspace-scoped key under the schmux prefix', () => {
    expect(KEY).toBe('schmux:diff-unchecked-files:ws-1');
  });

  it('returns an empty set when no key is stored', () => {
    expect(loadUncheckedDiffFiles('ws-1')).toEqual(new Set());
  });

  it('round-trips a set of paths per workspace', () => {
    saveUncheckedDiffFiles('ws-1', new Set(['a.txt', 'b.txt']));
    expect(loadUncheckedDiffFiles('ws-1')).toEqual(new Set(['a.txt', 'b.txt']));
    expect(loadUncheckedDiffFiles('ws-2')).toEqual(new Set());
  });

  it('removes the key when saving an empty set', () => {
    saveUncheckedDiffFiles('ws-1', new Set(['a.txt']));
    saveUncheckedDiffFiles('ws-1', new Set());
    expect(localStorage.getItem(KEY)).toBeNull();
  });

  it('never stores the empty-string path', () => {
    saveUncheckedDiffFiles('ws-1', new Set(['', 'a.txt']));
    expect(JSON.parse(localStorage.getItem(KEY)!)).toEqual(['a.txt']);
  });

  it('drops empty-string entries on load', () => {
    localStorage.setItem(KEY, JSON.stringify(['', 'a.txt']));
    expect(loadUncheckedDiffFiles('ws-1')).toEqual(new Set(['a.txt']));
  });

  it('treats non-JSON as no key and removes it', () => {
    vi.spyOn(console, 'warn').mockImplementation(() => {});
    localStorage.setItem(KEY, '{not json');
    expect(loadUncheckedDiffFiles('ws-1')).toEqual(new Set());
    expect(localStorage.getItem(KEY)).toBeNull();
  });

  it('treats a non-array or a mixed-type array as no key and removes it', () => {
    vi.spyOn(console, 'warn').mockImplementation(() => {});
    localStorage.setItem(KEY, JSON.stringify({ a: 1 }));
    expect(loadUncheckedDiffFiles('ws-1')).toEqual(new Set());
    expect(localStorage.getItem(KEY)).toBeNull();

    localStorage.setItem(KEY, JSON.stringify(['a.txt', 7]));
    expect(loadUncheckedDiffFiles('ws-1')).toEqual(new Set());
    expect(localStorage.getItem(KEY)).toBeNull();
  });

  // setupTests.ts replaces window.localStorage with a plain object, not a
  // Storage instance, so spy on the object itself rather than Storage.prototype.
  it('warns and returns an empty set when storage read throws', () => {
    const warn = vi.spyOn(console, 'warn').mockImplementation(() => {});
    vi.spyOn(localStorage, 'getItem').mockImplementation(() => {
      throw new Error('quota');
    });
    expect(loadUncheckedDiffFiles('ws-1')).toEqual(new Set());
    expect(warn).toHaveBeenCalled();
  });

  it('warns and does not throw when storage write throws', () => {
    const warn = vi.spyOn(console, 'warn').mockImplementation(() => {});
    vi.spyOn(localStorage, 'setItem').mockImplementation(() => {
      throw new Error('quota');
    });
    expect(() => saveUncheckedDiffFiles('ws-1', new Set(['a.txt']))).not.toThrow();
    expect(warn).toHaveBeenCalled();
  });
});
