import { describe, it, expect } from 'vitest';
import { extractScreenText } from './screenCapture';

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
