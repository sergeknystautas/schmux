import { describe, it, expect, vi, beforeEach } from 'vitest';
import { renderHook, act } from '@testing-library/react';
import useLocalStorage from './useLocalStorage';

describe('useLocalStorage', () => {
  beforeEach(() => {
    localStorage.clear();
  });

  it('returns initial value when localStorage is empty', () => {
    const { result } = renderHook(() => useLocalStorage('test-key', 'default'));
    expect(result.current[0]).toBe('default');
  });

  it('reads existing value from localStorage with schmux: prefix', () => {
    localStorage.setItem('schmux:test-key', JSON.stringify('saved-value'));
    const { result } = renderHook(() => useLocalStorage('test-key', 'default'));
    expect(result.current[0]).toBe('saved-value');
  });

  it('setValue persists to localStorage', () => {
    const { result } = renderHook(() => useLocalStorage('test-key', 'default'));

    act(() => {
      result.current[1]('new-value');
    });

    expect(result.current[0]).toBe('new-value');
    expect(localStorage.getItem('schmux:test-key')).toBe(JSON.stringify('new-value'));
  });

  it('setValue with updater function', () => {
    const { result } = renderHook(() => useLocalStorage('counter', 0));

    act(() => {
      result.current[1]((prev) => prev + 1);
    });

    expect(result.current[0]).toBe(1);
    expect(localStorage.getItem('schmux:counter')).toBe('1');
  });

  it('removeValue clears from localStorage and resets to initial', () => {
    localStorage.setItem('schmux:test-key', JSON.stringify('saved'));
    const { result } = renderHook(() => useLocalStorage('test-key', 'default'));

    expect(result.current[0]).toBe('saved');

    act(() => {
      result.current[2](); // removeValue
    });

    expect(result.current[0]).toBe('default');
    expect(localStorage.getItem('schmux:test-key')).toBeNull();
  });

  it('setting undefined removes the key', () => {
    const { result } = renderHook(() => useLocalStorage<string | undefined>('test-key', 'initial'));

    act(() => {
      result.current[1]('something');
    });
    expect(localStorage.getItem('schmux:test-key')).toBe(JSON.stringify('something'));

    act(() => {
      result.current[1](undefined);
    });
    expect(localStorage.getItem('schmux:test-key')).toBeNull();
  });

  it('falls back to initial value when localStorage has invalid JSON', () => {
    const spy = vi.spyOn(console, 'error').mockImplementation(() => {});
    localStorage.setItem('schmux:test-key', 'not valid json{{{');
    const { result } = renderHook(() => useLocalStorage('test-key', 'fallback'));
    expect(result.current[0]).toBe('fallback');
    spy.mockRestore();
  });

  it('works with complex objects', () => {
    const initial = { items: [1, 2, 3], active: true };
    const { result } = renderHook(() => useLocalStorage('complex', initial));

    expect(result.current[0]).toEqual(initial);

    const updated = { items: [4, 5], active: false };
    act(() => {
      result.current[1](updated);
    });

    expect(result.current[0]).toEqual(updated);
    expect(JSON.parse(localStorage.getItem('schmux:complex')!)).toEqual(updated);
  });
});
