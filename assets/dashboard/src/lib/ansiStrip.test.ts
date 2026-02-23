import { describe, it, expect } from 'vitest';
import { stripAnsi } from './ansiStrip';

describe('stripAnsi', () => {
  it('returns plain text unchanged', () => {
    expect(stripAnsi('hello world')).toBe('hello world');
  });

  it('strips SGR codes (bold, color, reset)', () => {
    expect(stripAnsi('\x1b[1mhello\x1b[0m')).toBe('hello');
  });

  it('strips 24-bit color codes', () => {
    expect(stripAnsi('\x1b[38;2;255;100;50mcolored\x1b[0m')).toBe('colored');
  });

  it('strips cursor movement sequences', () => {
    expect(stripAnsi('\x1b[6Ahello\x1b[2K')).toBe('hello');
  });

  it('strips OSC sequences (hyperlinks)', () => {
    expect(stripAnsi('\x1b]8;;https://example.com\x1b\\link\x1b]8;;\x1b\\')).toBe('link');
  });

  it('handles empty string', () => {
    expect(stripAnsi('')).toBe('');
  });

  it('handles mixed content', () => {
    expect(stripAnsi('\x1b[32m✶\x1b[0m Shimmying…')).toBe('✶ Shimmying…');
  });
});
