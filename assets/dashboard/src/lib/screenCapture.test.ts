import { describe, it, expect } from 'vitest';
import { extractScreenText, extractViewportText } from './screenCapture';

describe('extractScreenText', () => {
  it('extracts text from a mock buffer', () => {
    // Create a minimal mock of xterm.js buffer
    const mockBuffer = {
      length: 2,
      getLine: (y: number) => ({
        length: 5,
        getCell: (x: number) => {
          const chars = y === 0 ? 'hello' : 'world';
          return {
            getChars: () => chars[x] || '',
            getWidth: () => 1,
          };
        },
        translateToString: () => (y === 0 ? 'hello' : 'world'),
      }),
    };
    const text = extractScreenText(mockBuffer as any);
    expect(text).toBe('hello\nworld\n');
  });
});

describe('extractViewportText', () => {
  it('extracts only visible viewport rows', () => {
    // Simulate a buffer with scrollback: 3 scrollback lines + 2 visible rows
    const allLines = ['scrollback1', 'scrollback2', 'scrollback3', 'visible1', 'visible2'];
    const mockBuffer = {
      length: allLines.length,
      baseY: 3, // viewport starts at line 3
      getLine: (y: number) => ({
        translateToString: () => allLines[y],
      }),
    };
    const text = extractViewportText(mockBuffer as any, 2);
    expect(text).toBe('visible1\nvisible2\n');
  });

  it('handles buffer shorter than rows', () => {
    const mockBuffer = {
      length: 2,
      baseY: 0,
      getLine: (y: number) => ({
        translateToString: () => (y === 0 ? 'line1' : 'line2'),
      }),
    };
    // Request 5 rows but only 2 exist
    const text = extractViewportText(mockBuffer as any, 5);
    expect(text).toBe('line1\nline2\n');
  });
});
