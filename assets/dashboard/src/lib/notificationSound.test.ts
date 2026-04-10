import { describe, it, expect } from 'vitest';
import { soundForState, isAttentionState } from './notificationSound';

describe('soundForState', () => {
  it('returns attention for Needs Input', () => {
    expect(soundForState('Needs Input')).toBe('attention');
  });

  it('returns attention for Needs Attention', () => {
    expect(soundForState('Needs Attention')).toBe('attention');
  });

  it('returns attention for Error', () => {
    expect(soundForState('Error')).toBe('attention');
  });

  it('returns completion for Completed', () => {
    expect(soundForState('Completed')).toBe('completion');
  });

  it('returns null for Running', () => {
    expect(soundForState('Running')).toBeNull();
  });

  it('returns null for undefined', () => {
    expect(soundForState(undefined)).toBeNull();
  });

  it('returns null for empty string', () => {
    expect(soundForState('')).toBeNull();
  });
});

describe('isAttentionState', () => {
  it('returns true for Needs Input', () => {
    expect(isAttentionState('Needs Input')).toBe(true);
  });

  it('returns false for Completed', () => {
    expect(isAttentionState('Completed')).toBe(false);
  });

  it('returns false for undefined', () => {
    expect(isAttentionState(undefined)).toBe(false);
  });
});
