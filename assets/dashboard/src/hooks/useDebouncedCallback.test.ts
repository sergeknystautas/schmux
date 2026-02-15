import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { renderHook } from '@testing-library/react';
import useDebouncedCallback from './useDebouncedCallback';

describe('useDebouncedCallback', () => {
  beforeEach(() => {
    vi.useFakeTimers();
  });

  afterEach(() => {
    vi.useRealTimers();
  });

  it('does not call callback immediately', () => {
    const callback = vi.fn();
    const { result } = renderHook(() => useDebouncedCallback(callback, 100));

    result.current();
    expect(callback).not.toHaveBeenCalled();
  });

  it('calls callback after delay', () => {
    const callback = vi.fn();
    const { result } = renderHook(() => useDebouncedCallback(callback, 100));

    result.current();
    vi.advanceTimersByTime(100);
    expect(callback).toHaveBeenCalledTimes(1);
  });

  it('only fires last call when called multiple times rapidly', () => {
    const callback = vi.fn();
    const { result } = renderHook(() => useDebouncedCallback(callback, 100));

    result.current();
    vi.advanceTimersByTime(50);
    result.current();
    vi.advanceTimersByTime(50);
    result.current();
    vi.advanceTimersByTime(100);

    expect(callback).toHaveBeenCalledTimes(1);
  });

  it('cleans up on unmount (no late invocations)', () => {
    const callback = vi.fn();
    const { result, unmount } = renderHook(() => useDebouncedCallback(callback, 100));

    result.current();
    unmount();
    vi.advanceTimersByTime(200);

    expect(callback).not.toHaveBeenCalled();
  });

  it('uses updated callback reference', () => {
    const callback1 = vi.fn();
    const callback2 = vi.fn();

    const { result, rerender } = renderHook(({ cb }) => useDebouncedCallback(cb, 100), {
      initialProps: { cb: callback1 },
    });

    result.current();
    rerender({ cb: callback2 });
    vi.advanceTimersByTime(100);

    expect(callback1).not.toHaveBeenCalled();
    expect(callback2).toHaveBeenCalledTimes(1);
  });
});
