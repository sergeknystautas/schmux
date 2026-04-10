import { describe, it, expect } from 'vitest';
import { computeScreenDiff } from './screenDiff';

describe('computeScreenDiff', () => {
  it('reports no differences for identical screens', () => {
    const screen = 'line one\nline two\nline three';
    const result = computeScreenDiff(screen, screen);
    expect(result.differingRows).toEqual([]);
    expect(result.summary).toBe('0 rows differ');
  });

  it('detects a single differing row', () => {
    const tmux = 'aaa\nbbb\nccc';
    const xterm = 'aaa\nBBB\nccc';
    const result = computeScreenDiff(tmux, xterm);
    expect(result.differingRows).toHaveLength(1);
    expect(result.differingRows[0].row).toBe(1);
    expect(result.differingRows[0].tmux).toBe('bbb');
    expect(result.differingRows[0].xterm).toBe('BBB');
  });

  it('handles different number of rows (tmux shorter)', () => {
    const tmux = 'aaa\nbbb';
    const xterm = 'aaa\nbbb\nccc';
    const result = computeScreenDiff(tmux, xterm);
    expect(result.differingRows).toHaveLength(1);
    expect(result.differingRows[0].row).toBe(2);
    expect(result.differingRows[0].tmux).toBe('');
    expect(result.differingRows[0].xterm).toBe('ccc');
  });

  it('handles different number of rows (xterm shorter)', () => {
    const tmux = 'aaa\nbbb\nccc';
    const xterm = 'aaa\nbbb';
    const result = computeScreenDiff(tmux, xterm);
    expect(result.differingRows).toHaveLength(1);
    expect(result.differingRows[0].row).toBe(2);
    expect(result.differingRows[0].xterm).toBe('');
  });

  it('captures multiple differing rows', () => {
    const tmux = 'aaa\nbbb\nccc\nddd';
    const xterm = 'AAA\nbbb\nCCC\nddd';
    const result = computeScreenDiff(tmux, xterm);
    expect(result.differingRows).toHaveLength(2);
    expect(result.differingRows[0].row).toBe(0);
    expect(result.differingRows[1].row).toBe(2);
    expect(result.summary).toBe('2 rows differ');
  });

  it('handles empty inputs', () => {
    const result = computeScreenDiff('', '');
    expect(result.differingRows).toEqual([]);
    expect(result.summary).toBe('0 rows differ');
  });

  it('strips ANSI codes from tmux output before comparison', () => {
    const tmux = '\x1b[32mhello\x1b[0m';
    const xterm = 'hello';
    const result = computeScreenDiff(tmux, xterm);
    expect(result.differingRows).toEqual([]);
  });

  it('preserves raw ANSI in differingRows tmux field', () => {
    const tmux = '\x1b[31mred\x1b[0m text';
    const xterm = 'different text';
    const result = computeScreenDiff(tmux, xterm);
    expect(result.differingRows).toHaveLength(1);
    expect(result.differingRows[0].tmux).toBe('\x1b[31mred\x1b[0m text');
  });

  it('trims trailing whitespace before comparing', () => {
    const tmux = 'hello   \nworld   ';
    const xterm = 'hello\nworld';
    const result = computeScreenDiff(tmux, xterm);
    expect(result.differingRows).toHaveLength(0);
  });

  it('generates human-readable diff text', () => {
    const diff = computeScreenDiff('aaa\nbbb', 'aaa\nccc');
    expect(diff.diffText).toContain('Row 1:');
    expect(diff.diffText).toContain('tmux:  bbb');
    expect(diff.diffText).toContain('xterm: ccc');
  });
});
