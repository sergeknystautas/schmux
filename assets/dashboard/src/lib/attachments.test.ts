import { describe, it, expect } from 'vitest';
import { withFileAttachments } from './attachments';

// The first literal is pinned identically in internal/attachment
// TestAppendFileList so the Go and TypeScript writers cannot drift.
describe('withFileAttachments', () => {
  it('appends the block after the text', () => {
    expect(withFileAttachments('do it', ['/a/b.csv', '/c/d.txt'])).toBe(
      'do it\n\nFile attachments:\n/a/b.csv\n/c/d.txt'
    );
  });
  it('omits the blank lines when there is no text', () => {
    expect(withFileAttachments('', ['/a/b.csv'])).toBe('File attachments:\n/a/b.csv');
  });
  it('returns the text unchanged without paths', () => {
    expect(withFileAttachments('do it', [])).toBe('do it');
  });
});
