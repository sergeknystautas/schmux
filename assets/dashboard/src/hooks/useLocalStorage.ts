import { useEffect, useState, useCallback } from 'react';

/**
 * localStorage keys used across the app.
 * These are unprefixed - useLocalStorage auto-adds 'schmux:' prefix.
 * For direct localStorage access, add the 'schmux:' prefix yourself.
 */
// TODO: Add sessions filter UI to ConfigPage to restore this functionality
// export const SESSIONS_FILTERS_KEY = 'sessions-filters';
export const WORKSPACE_EXPANDED_KEY = 'workspace-expanded';
export const VIEWED_SESSIONS_KEY = 'viewedSessions';
export const SESSION_SIDEBAR_COLLAPSED_KEY = 'sessionSidebarCollapsed';

/**
 * React hook for syncing state to localStorage with cross-tab support.
 *
 * The storage event only fires in OTHER tabs/windows, not the one that made
 * the change. This gives us cross-tab sync without real-time complexity.
 *
 * @param {string} key - localStorage key (will be prefixed with 'schmux:')
 * @param {any} initialValue - Default value if nothing in storage
 * @returns {[any, Function, Function]} - [value, setValue, removeValue]
 */
export default function useLocalStorage<T>(
  key: string,
  initialValue: T
): [T, (value: T | ((prev: T) => T)) => void, () => void] {
  const storageKey = `schmux:${key}`;

  // Initialize from localStorage or use initialValue
  const [storedValue, setStoredValue] = useState<T>(() => {
    try {
      const item = window.localStorage.getItem(storageKey);
      return item ? (JSON.parse(item) as T) : initialValue;
    } catch (error) {
      console.error(`Error reading localStorage key "${storageKey}":`, error);
      return initialValue;
    }
  });

  // Return a wrapped version of useState's setter function that
  // persists the new value to localStorage
  const setValue = useCallback(
    (value: T | ((prev: T) => T)) => {
      try {
        // Allow value to be a function so we have same API as useState
        const valueToStore = value instanceof Function ? value(storedValue) : value;

        setStoredValue(valueToStore);

        // Save to localStorage
        if (valueToStore === undefined) {
          window.localStorage.removeItem(storageKey);
        } else {
          window.localStorage.setItem(storageKey, JSON.stringify(valueToStore));
        }
      } catch (error) {
        console.error(`Error setting localStorage key "${storageKey}":`, error);
      }
    },
    [storageKey, storedValue]
  );

  // Remove value from localStorage and reset to initialValue
  const removeValue = useCallback(() => {
    try {
      window.localStorage.removeItem(storageKey);
      setStoredValue(initialValue);
    } catch (error) {
      console.error(`Error removing localStorage key "${storageKey}":`, error);
    }
  }, [storageKey, initialValue]);

  // Listen for changes in other tabs/windows
  useEffect(() => {
    const handleStorageChange = (event: StorageEvent) => {
      if (event.key === storageKey && event.newValue !== null) {
        try {
          setStoredValue(JSON.parse(event.newValue) as T);
        } catch (error) {
          console.error(`Error parsing localStorage value for "${storageKey}":`, error);
        }
      } else if (event.key === storageKey && event.newValue === null) {
        // Key was removed in another tab
        setStoredValue(initialValue);
      }
    };

    window.addEventListener('storage', handleStorageChange);
    return () => window.removeEventListener('storage', handleStorageChange);
  }, [storageKey, initialValue]);

  return [storedValue, setValue, removeValue];
}
