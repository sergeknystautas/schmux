import { useCallback, useEffect, useRef } from 'react';

/**
 * React hook for debouncing a callback function.
 *
 * Delays executing the callback until after delay milliseconds have elapsed
 * since the last time the debounced function was called.
 *
 * @param callback - Function to debounce
 * @param delay - Milliseconds to wait before executing
 * @returns Debounced function
 */
export default function useDebouncedCallback<T extends (...args: unknown[]) => void>(
  callback: T,
  delay: number
): T {
  const timeoutRef = useRef<number | null>(null);
  const callbackRef = useRef(callback);

  // Keep callback ref in sync
  useEffect(() => {
    callbackRef.current = callback;
  }, [callback]);

  // Cleanup timeout on unmount
  useEffect(() => {
    return () => {
      if (timeoutRef.current !== null) {
        clearTimeout(timeoutRef.current);
      }
    };
  }, []);

  return useCallback(
    (...args: Parameters<T>) => {
      if (timeoutRef.current !== null) {
        clearTimeout(timeoutRef.current);
      }
      timeoutRef.current = window.setTimeout(() => {
        callbackRef.current(...args);
      }, delay);
    },
    [delay]
  ) as T;
}
