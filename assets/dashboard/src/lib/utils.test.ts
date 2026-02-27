import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import {
  formatRelativeTime,
  truncateStart,
  splitPath,
  formatTimestamp,
  copyToClipboard,
  formatNudgeSummary,
  isRemoteClient,
  nudgeStateEmoji,
} from './utils';

describe('formatRelativeTime', () => {
  beforeEach(() => {
    vi.useFakeTimers();
    vi.setSystemTime(new Date('2024-06-15T12:00:00Z'));
  });

  afterEach(() => {
    vi.useRealTimers();
  });

  it('returns "just now" for < 60 seconds ago', () => {
    const thirtySecondsAgo = new Date('2024-06-15T11:59:30Z');
    expect(formatRelativeTime(thirtySecondsAgo)).toBe('just now');
  });

  it('returns minutes ago', () => {
    const fiveMinutesAgo = new Date('2024-06-15T11:55:00Z');
    expect(formatRelativeTime(fiveMinutesAgo)).toBe('5m ago');
  });

  it('returns hours ago', () => {
    const threeHoursAgo = new Date('2024-06-15T09:00:00Z');
    expect(formatRelativeTime(threeHoursAgo)).toBe('3h ago');
  });

  it('returns days ago for < 7 days', () => {
    const twoDaysAgo = new Date('2024-06-13T12:00:00Z');
    expect(formatRelativeTime(twoDaysAgo)).toBe('2d ago');
  });

  it('returns days ago for < 30 days', () => {
    const eightDaysAgo = new Date('2024-06-07T12:00:00Z');
    expect(formatRelativeTime(eightDaysAgo)).toBe('8d ago');
  });

  it('boundary: exactly 60 seconds → "1m ago"', () => {
    const exactlyOneMinute = new Date('2024-06-15T11:59:00Z');
    expect(formatRelativeTime(exactlyOneMinute)).toBe('1m ago');
  });

  it('boundary: exactly 60 minutes → "1h ago"', () => {
    const exactlyOneHour = new Date('2024-06-15T11:00:00Z');
    expect(formatRelativeTime(exactlyOneHour)).toBe('1h ago');
  });

  it('boundary: exactly 24 hours → "1d ago"', () => {
    const exactlyOneDay = new Date('2024-06-14T12:00:00Z');
    expect(formatRelativeTime(exactlyOneDay)).toBe('1d ago');
  });

  it('boundary: exactly 7 days → days ago', () => {
    const exactlySevenDays = new Date('2024-06-08T12:00:00Z');
    expect(formatRelativeTime(exactlySevenDays)).toBe('7d ago');
  });

  it('boundary: exactly 30 days → date string', () => {
    const exactlyThirtyDays = new Date('2024-05-16T12:00:00Z');
    const result = formatRelativeTime(exactlyThirtyDays);
    expect(result).not.toMatch(/ago$/);
  });

  it('accepts string timestamps', () => {
    expect(formatRelativeTime('2024-06-15T11:55:00Z')).toBe('5m ago');
  });

  it('accepts numeric timestamps', () => {
    const fiveMinAgoMs = new Date('2024-06-15T11:55:00Z').getTime();
    expect(formatRelativeTime(fiveMinAgoMs)).toBe('5m ago');
  });
});

describe('truncateStart', () => {
  it('returns short string unchanged', () => {
    expect(truncateStart('hello', 40)).toBe('hello');
  });

  it('truncates long string with ... prefix', () => {
    const long = 'a'.repeat(50);
    const result = truncateStart(long, 10);
    expect(result).toBe('...' + 'a'.repeat(7));
    expect(result.length).toBe(10);
  });

  it('returns unchanged when length equals maxLength', () => {
    const exact = 'a'.repeat(40);
    expect(truncateStart(exact, 40)).toBe(exact);
  });

  it('uses default maxLength of 40', () => {
    const short = 'hello';
    expect(truncateStart(short)).toBe(short);

    const long = 'x'.repeat(50);
    const result = truncateStart(long);
    expect(result.length).toBe(40);
    expect(result.startsWith('...')).toBe(true);
  });

  it('handles custom maxLength', () => {
    const result = truncateStart('abcdefghij', 6);
    expect(result).toBe('...hij');
    expect(result.length).toBe(6);
  });
});

describe('splitPath', () => {
  it('splits a typical Unix path into directory and filename', () => {
    const result = splitPath('/home/user/project/file.ts');
    expect(result).toEqual({ filename: 'file.ts', directory: '/home/user/project/' });
  });

  it('returns empty directory for a bare filename', () => {
    const result = splitPath('file.ts');
    expect(result).toEqual({ filename: 'file.ts', directory: '' });
  });

  it('handles root-level files', () => {
    const result = splitPath('/file.ts');
    expect(result).toEqual({ filename: 'file.ts', directory: '/' });
  });

  it('handles deeply nested paths', () => {
    const result = splitPath('/a/b/c/d/e/f.txt');
    expect(result).toEqual({ filename: 'f.txt', directory: '/a/b/c/d/e/' });
  });

  it('handles empty string', () => {
    const result = splitPath('');
    expect(result).toEqual({ filename: '', directory: '' });
  });

  it('handles trailing slash (directory path)', () => {
    const result = splitPath('/home/user/');
    expect(result).toEqual({ filename: '', directory: '/home/user/' });
  });
});

describe('formatTimestamp', () => {
  it('formats a Date object to locale string', () => {
    const date = new Date('2024-06-15T12:30:00Z');
    const result = formatTimestamp(date);
    expect(typeof result).toBe('string');
    expect(result.length).toBeGreaterThan(0);
  });

  it('accepts a string timestamp', () => {
    const result = formatTimestamp('2024-06-15T12:30:00Z');
    expect(typeof result).toBe('string');
    expect(result.length).toBeGreaterThan(0);
  });

  it('accepts a numeric timestamp', () => {
    const ms = new Date('2024-06-15T12:30:00Z').getTime();
    const result = formatTimestamp(ms);
    expect(typeof result).toBe('string');
    expect(result.length).toBeGreaterThan(0);
  });

  it('returns consistent results for equivalent inputs', () => {
    const dateStr = '2024-06-15T12:30:00Z';
    const dateObj = new Date(dateStr);
    const dateMs = dateObj.getTime();
    expect(formatTimestamp(dateStr)).toBe(formatTimestamp(dateObj));
    expect(formatTimestamp(dateStr)).toBe(formatTimestamp(dateMs));
  });
});

describe('copyToClipboard', () => {
  const originalClipboard = navigator.clipboard;

  afterEach(() => {
    Object.defineProperty(navigator, 'clipboard', {
      value: originalClipboard,
      writable: true,
      configurable: true,
    });
  });

  it('returns true when clipboard write succeeds', async () => {
    const writeText = vi.fn().mockResolvedValue(undefined);
    Object.defineProperty(navigator, 'clipboard', {
      value: { writeText },
      writable: true,
      configurable: true,
    });
    const result = await copyToClipboard('hello');
    expect(result).toBe(true);
    expect(writeText).toHaveBeenCalledWith('hello');
  });

  it('returns false when clipboard write fails', async () => {
    const writeText = vi.fn().mockRejectedValue(new Error('denied'));
    Object.defineProperty(navigator, 'clipboard', {
      value: { writeText },
      writable: true,
      configurable: true,
    });
    const result = await copyToClipboard('hello');
    expect(result).toBe(false);
  });

  it('passes the exact text to the clipboard API', async () => {
    const writeText = vi.fn().mockResolvedValue(undefined);
    Object.defineProperty(navigator, 'clipboard', {
      value: { writeText },
      writable: true,
      configurable: true,
    });
    await copyToClipboard('multi\nline\ntext');
    expect(writeText).toHaveBeenCalledWith('multi\nline\ntext');
  });
});

describe('formatNudgeSummary', () => {
  it('returns null for undefined input', () => {
    expect(formatNudgeSummary(undefined)).toBeNull();
  });

  it('returns null for empty string', () => {
    expect(formatNudgeSummary('')).toBeNull();
  });

  it('returns trimmed text when under max length', () => {
    expect(formatNudgeSummary('  hello world  ')).toBe('hello world');
  });

  it('returns text unchanged when at default max length', () => {
    const exact = 'a'.repeat(100);
    expect(formatNudgeSummary(exact)).toBe(exact);
  });

  it('truncates text longer than default max length with ellipsis', () => {
    const long = 'a'.repeat(110);
    const result = formatNudgeSummary(long);
    expect(result).not.toBeNull();
    expect(result!.length).toBe(100);
    expect(result!.endsWith('...')).toBe(true);
    expect(result!).toBe('a'.repeat(97) + '...');
  });

  it('uses custom max length', () => {
    const text = 'a'.repeat(20);
    const result = formatNudgeSummary(text, 10);
    expect(result).not.toBeNull();
    expect(result!.length).toBe(10);
    expect(result!.endsWith('...')).toBe(true);
  });

  it('returns empty string for whitespace-only input (trims but does not nullify)', () => {
    expect(formatNudgeSummary('   ')).toBe('');
  });

  it('trims before checking length', () => {
    const padded = '  ' + 'a'.repeat(98) + '  ';
    const result = formatNudgeSummary(padded);
    // After trim: 98 chars, under 100 limit
    expect(result).toBe('a'.repeat(98));
  });
});

describe('isRemoteClient', () => {
  const originalLocation = window.location;

  afterEach(() => {
    Object.defineProperty(window, 'location', {
      value: originalLocation,
      writable: true,
      configurable: true,
    });
  });

  it('returns false for localhost', () => {
    Object.defineProperty(window, 'location', {
      value: { hostname: 'localhost' },
      writable: true,
      configurable: true,
    });
    expect(isRemoteClient()).toBe(false);
  });

  it('returns false for 127.0.0.1', () => {
    Object.defineProperty(window, 'location', {
      value: { hostname: '127.0.0.1' },
      writable: true,
      configurable: true,
    });
    expect(isRemoteClient()).toBe(false);
  });

  it('returns true for an external hostname', () => {
    Object.defineProperty(window, 'location', {
      value: { hostname: 'my-server.example.com' },
      writable: true,
      configurable: true,
    });
    expect(isRemoteClient()).toBe(true);
  });

  it('returns true for an IP address that is not 127.0.0.1', () => {
    Object.defineProperty(window, 'location', {
      value: { hostname: '192.168.1.100' },
      writable: true,
      configurable: true,
    });
    expect(isRemoteClient()).toBe(true);
  });
});

describe('nudgeStateEmoji', () => {
  it('maps all expected states', () => {
    expect(nudgeStateEmoji['Needs Input']).toBeDefined();
    expect(nudgeStateEmoji['Needs Feature Clarification']).toBeDefined();
    expect(nudgeStateEmoji['Needs Attention']).toBeDefined();
    expect(nudgeStateEmoji['Completed']).toBeDefined();
    expect(nudgeStateEmoji['Error']).toBeDefined();
  });

  it('returns undefined for unknown state', () => {
    expect(nudgeStateEmoji['NonExistent']).toBeUndefined();
  });

  it('has exactly 5 entries', () => {
    expect(Object.keys(nudgeStateEmoji)).toHaveLength(5);
  });
});
