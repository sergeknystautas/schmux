import { describe, it, expect } from 'vitest';
import { computeScreenDiff } from './screenDiff';

describe('computeScreenDiff', () => {
  it('returns empty diff for identical screens', () => {
    const diff = computeScreenDiff('hello\nworld\n', 'hello\nworld\n');
    expect(diff.differingRows).toHaveLength(0);
    expect(diff.summary).toBe('0 rows differ');
  });

  it('detects differing rows', () => {
    const diff = computeScreenDiff('line1\nline2\nline3\n', 'line1\nLINE2\nline3\n');
    expect(diff.differingRows).toHaveLength(1);
    expect(diff.differingRows[0].row).toBe(1);
    expect(diff.differingRows[0].tmux).toBe('line2');
    expect(diff.differingRows[0].xterm).toBe('LINE2');
  });

  it('generates human-readable diff text', () => {
    const diff = computeScreenDiff('aaa\nbbb\n', 'aaa\nccc\n');
    expect(diff.diffText).toContain('Row 1:');
    expect(diff.diffText).toContain('tmux:  bbb');
    expect(diff.diffText).toContain('xterm: ccc');
  });

  it('strips ANSI from tmux screen before comparing', () => {
    const tmuxScreen = '\x1b[32mhello\x1b[0m world\nline 2';
    const xtermScreen = 'hello world\nline 2';
    const result = computeScreenDiff(tmuxScreen, xtermScreen);
    expect(result.differingRows.length).toBe(0);
    expect(result.summary).toBe('0 rows differ');
  });

  it('preserves raw ANSI in differingRows tmux field', () => {
    const tmuxScreen = '\x1b[31mred\x1b[0m text';
    const xtermScreen = 'different text';
    const result = computeScreenDiff(tmuxScreen, xtermScreen);
    expect(result.differingRows.length).toBe(1);
    expect(result.differingRows[0].tmux).toBe('\x1b[31mred\x1b[0m text');
    expect(result.differingRows[0].xterm).toBe('different text');
  });

  it('trims trailing whitespace from both sides before comparing', () => {
    const tmuxScreen = 'hello   \nworld   ';
    const xtermScreen = 'hello\nworld';
    const result = computeScreenDiff(tmuxScreen, xtermScreen);
    expect(result.differingRows.length).toBe(0);
  });
});
