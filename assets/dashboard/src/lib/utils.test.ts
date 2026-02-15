import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { formatRelativeTime, truncateStart } from './utils';

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

  it('returns date string for >= 7 days', () => {
    const eightDaysAgo = new Date('2024-06-07T12:00:00Z');
    const result = formatRelativeTime(eightDaysAgo);
    // Should be a locale date string, not a relative time
    expect(result).not.toMatch(/ago$/);
    expect(result).not.toBe('just now');
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

  it('boundary: exactly 7 days → date string', () => {
    const exactlySevenDays = new Date('2024-06-08T12:00:00Z');
    const result = formatRelativeTime(exactlySevenDays);
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
