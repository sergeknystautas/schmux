import { describe, it, expect } from 'vitest';
import { isAttentionState } from './notificationSound';

describe('isAttentionState', () => {
  it('"Needs Authorization" → true', () => {
    expect(isAttentionState('Needs Authorization')).toBe(true);
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
