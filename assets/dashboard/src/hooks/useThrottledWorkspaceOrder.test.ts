import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { act, renderHook } from '@testing-library/react';
import useThrottledWorkspaceOrder from './useThrottledWorkspaceOrder';

type Item = { id: string; label: string };
type Props = {
  sorted: Item[] | undefined;
  enabled: boolean;
  intervalMs: number;
  resetKey: unknown;
};

const item = (id: string, label = id): Item => ({ id, label });
const list = (...ids: string[]) => ids.map((id) => item(id));
const ids = (items: Item[] | undefined) => items?.map((i) => i.id);

const base: Props = {
  sorted: list('a', 'b', 'c'),
  enabled: true,
  intervalMs: 2000,
  resetKey: false,
};

function setup(initial: Partial<Props> = {}) {
  return renderHook(
    ({ sorted, enabled, intervalMs, resetKey }: Props) =>
      useThrottledWorkspaceOrder(sorted, { enabled, intervalMs, resetKey }),
    { initialProps: { ...base, ...initial } }
  );
}

function advance(ms: number) {
  act(() => {
    vi.advanceTimersByTime(ms);
  });
}

describe('useThrottledWorkspaceOrder', () => {
  beforeEach(() => {
    vi.useFakeTimers();
  });

  afterEach(() => {
    vi.useRealTimers();
  });

  it('returns the sorted order on mount', () => {
    const { result } = setup();
    expect(ids(result.current)).toEqual(['a', 'b', 'c']);
  });

  it('returns undefined when sorted is undefined', () => {
    const { result } = setup({ sorted: undefined });
    expect(result.current).toBeUndefined();
  });

  it('holds changes inside the window and applies the latest order at window end', () => {
    const { result, rerender } = setup();

    // Mount adopted a,b,c at t=0; a change at t=2000 is outside the window.
    advance(2000);
    rerender({ ...base, sorted: list('b', 'a', 'c') });
    expect(ids(result.current)).toEqual(['b', 'a', 'c']);

    // Two changes inside the window (adopted at t=2000) are held.
    advance(500);
    rerender({ ...base, sorted: list('c', 'b', 'a') });
    expect(ids(result.current)).toEqual(['b', 'a', 'c']);
    advance(500);
    rerender({ ...base, sorted: list('a', 'c', 'b') });
    expect(ids(result.current)).toEqual(['b', 'a', 'c']);

    // The deadline is anchored to the last adoption (t=4000), not pushed out
    // by later changes; the latest order wins.
    advance(999);
    expect(ids(result.current)).toEqual(['b', 'a', 'c']);
    advance(1);
    expect(ids(result.current)).toEqual(['a', 'c', 'b']);
  });

  it('keeps item data live while the order is held', () => {
    const { result, rerender } = setup();

    rerender({ ...base, sorted: [item('c'), item('a', 'a-updated'), item('b')] });

    expect(ids(result.current)).toEqual(['a', 'b', 'c']);
    expect(result.current?.[0].label).toBe('a-updated');
  });

  it('adopts a new id set immediately', () => {
    const { result, rerender } = setup();

    rerender({ ...base, sorted: list('d', 'a', 'b', 'c') });

    expect(ids(result.current)).toEqual(['d', 'a', 'b', 'c']);
  });

  it('drops a disposed workspace immediately and does not restore a stale pending order', () => {
    const { result, rerender } = setup();

    rerender({ ...base, sorted: list('c', 'b', 'a') }); // held: a,b,c
    rerender({ ...base, sorted: list('b', 'a') }); // c disposed

    expect(ids(result.current)).toEqual(['b', 'a']);
    advance(5000);
    expect(ids(result.current)).toEqual(['b', 'a']);
  });

  it('adopts immediately when resetKey changes', () => {
    const { result, rerender } = setup();

    rerender({ ...base, resetKey: true, sorted: list('c', 'a', 'b') });

    expect(ids(result.current)).toEqual(['c', 'a', 'b']);
  });

  it('passes through when disabled and starts fresh when re-enabled', () => {
    const { result, rerender } = setup();

    rerender({ ...base, enabled: false, sorted: list('c', 'b', 'a') });
    expect(ids(result.current)).toEqual(['c', 'b', 'a']);

    rerender({ ...base, enabled: true, sorted: list('b', 'c', 'a') });
    expect(ids(result.current)).toEqual(['b', 'c', 'a']);
  });

  it('uses a changed interval for a pending reorder', () => {
    const { result, rerender } = setup();

    rerender({ ...base, sorted: list('b', 'a', 'c') }); // held until t=2000
    rerender({ ...base, sorted: list('b', 'a', 'c'), intervalMs: 500 });

    advance(499);
    expect(ids(result.current)).toEqual(['a', 'b', 'c']);
    advance(1);
    expect(ids(result.current)).toEqual(['b', 'a', 'c']);
  });

  it('clears its pending timer on unmount', () => {
    const { rerender, unmount } = setup();

    const idle = vi.getTimerCount();
    rerender({ ...base, sorted: list('b', 'a', 'c') });
    expect(vi.getTimerCount()).toBe(idle + 1);

    unmount();
    expect(vi.getTimerCount()).toBe(idle);
  });
});
