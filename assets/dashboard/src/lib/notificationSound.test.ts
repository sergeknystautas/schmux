import { describe, it, expect } from 'vitest';
import { isAttentionState, soundForState } from './notificationSound';

describe('isAttentionState', () => {
  it('"Needs Input" → true', () => {
    expect(isAttentionState('Needs Input')).toBe(true);
  });

  it('"Error" → true', () => {
    expect(isAttentionState('Error')).toBe(true);
  });

  it('"Running" → false', () => {
    expect(isAttentionState('Running')).toBe(false);
  });

  it('"Idle" → false', () => {
    expect(isAttentionState('Idle')).toBe(false);
  });

  it('undefined → false', () => {
    expect(isAttentionState(undefined)).toBe(false);
  });
});

describe('soundForState', () => {
  it('"Needs Input" → attention', () => {
    expect(soundForState('Needs Input')).toBe('attention');
  });

  it('"Needs Attention" → attention', () => {
    expect(soundForState('Needs Attention')).toBe('attention');
  });

  it('"Error" → attention', () => {
    expect(soundForState('Error')).toBe('attention');
  });

  it('"Completed" → completion', () => {
    expect(soundForState('Completed')).toBe('completion');
  });

  it('"Working" → null', () => {
    expect(soundForState('Working')).toBe(null);
  });

  it('"Idle" → null', () => {
    expect(soundForState('Idle')).toBe(null);
  });

  it('undefined → null', () => {
    expect(soundForState(undefined)).toBe(null);
  });
});
