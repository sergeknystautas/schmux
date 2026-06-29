import { describe, it, expect } from 'vitest';
import { parseFenceLine } from './fenceLog';

describe('parseFenceLine', () => {
  it('parses a blocked network CONNECT as network', () => {
    const f = parseFenceLine(
      '[fence:http]      10:52:49 ✗ CONNECT 403 www.google.com https://www.google.com:443 (0s)'
    );
    expect(f).toEqual({
      time: '10:52:49',
      kind: 'network',
      message: 'CONNECT 403 www.google.com https://www.google.com:443 (0s)',
    });
  });

  it('parses a blocked file write as file', () => {
    const f = parseFenceLine(
      '[fence:logstream] 10:52:49 ✗ file-write-create /private/etc/x (bash:1)'
    );
    expect(f.kind).toBe('file');
    expect(f.time).toBe('10:52:49');
    expect(f.message).toBe('file-write-create /private/etc/x (bash:1)');
  });

  it('parses a mach-lookup as system noise', () => {
    const f = parseFenceLine(
      '[fence:logstream] 10:52:49 ✗ mach-lookup com.apple.diagnosticd (curl:2)'
    );
    expect(f.kind).toBe('system');
    expect(f.message).toBe('mach-lookup com.apple.diagnosticd (curl:2)');
  });

  it('formats an unrecognized line as other, keeping the text', () => {
    const f = parseFenceLine('something totally unexpected');
    expect(f).toEqual({ time: '', kind: 'other', message: 'something totally unexpected' });
  });
});
