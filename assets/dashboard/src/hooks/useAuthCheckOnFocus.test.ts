import { describe, expect, it, vi, beforeEach, afterEach } from 'vitest';
import { act, renderHook } from '@testing-library/react';
import { useAuthCheckOnFocus } from './useAuthCheckOnFocus';

vi.mock('../lib/api', () => ({
  authCheck: vi.fn().mockResolvedValue(undefined),
}));

import { authCheck } from '../lib/api';

describe('useAuthCheckOnFocus', () => {
  beforeEach(() => {
    vi.clearAllMocks();
    vi.mocked(authCheck).mockResolvedValue(undefined);
  });
  afterEach(() => vi.restoreAllMocks());

  it('fires on mount', () => {
    renderHook(() => useAuthCheckOnFocus('s1', false));
    expect(authCheck).toHaveBeenCalledWith('s1');
  });

  it('fires on window focus and on becoming visible', () => {
    renderHook(() => useAuthCheckOnFocus('s1', false));
    expect(authCheck).toHaveBeenCalledTimes(1);
    act(() => {
      window.dispatchEvent(new Event('focus'));
    });
    expect(authCheck).toHaveBeenCalledTimes(2);
    act(() => {
      Object.defineProperty(document, 'visibilityState', { value: 'visible', configurable: true });
      document.dispatchEvent(new Event('visibilitychange'));
    });
    expect(authCheck).toHaveBeenCalledTimes(3);
  });

  it('never fires while skip is true (remote sessions)', () => {
    renderHook(() => useAuthCheckOnFocus('s1', true));
    act(() => {
      window.dispatchEvent(new Event('focus'));
    });
    expect(authCheck).not.toHaveBeenCalled();
  });

  it('does not fire on hidden visibility changes', () => {
    renderHook(() => useAuthCheckOnFocus('s1', false));
    const before = vi.mocked(authCheck).mock.calls.length;
    act(() => {
      Object.defineProperty(document, 'visibilityState', { value: 'hidden', configurable: true });
      document.dispatchEvent(new Event('visibilitychange'));
    });
    expect(vi.mocked(authCheck).mock.calls.length).toBe(before);
  });

  it('swallows failures (focus checks are best-effort)', async () => {
    vi.mocked(authCheck).mockRejectedValue(new Error('net'));
    renderHook(() => useAuthCheckOnFocus('s1', false));
    await act(async () => {});
    // No unhandled rejection: the hook caught it.
  });
});
