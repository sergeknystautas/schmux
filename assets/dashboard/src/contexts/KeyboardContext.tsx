import React, {
  createContext,
  useCallback,
  useContext,
  useEffect,
  useMemo,
  useRef,
  useState,
} from 'react';

type KeyboardModeState = 'inactive' | 'active';

type KeyboardScope =
  | { type: 'global' }
  | { type: 'workspace'; id: string }
  | { type: 'session'; id: string };

type KeyboardContextState = {
  workspaceId: string | null;
  sessionId: string | null;
};

function scopesEqual(a?: KeyboardScope, b?: KeyboardScope) {
  if (!a && !b) return true;
  if (!a || !b) return false;
  if (a.type !== b.type) return false;
  if (a.type === 'global') return true;
  if (a.type === 'workspace' && b.type === 'workspace') {
    return a.id === b.id;
  }
  if (a.type === 'session' && b.type === 'session') {
    return a.id === b.id;
  }
  return false;
}

type KeyboardAction = {
  key: string;
  prefixKey?: string;
  shiftKey?: boolean;
  description: string;
  handler: () => void;
  scope?: KeyboardScope;
};

type KeyboardContextValue = {
  mode: KeyboardModeState;
  enterMode: () => void;
  exitMode: () => void;
  registerAction: (action: KeyboardAction) => void;
  unregisterAction: (
    key: string,
    shiftKey?: boolean,
    scope?: KeyboardScope,
    prefixKey?: string
  ) => void;
  actions: KeyboardAction[];
  context: KeyboardContextState;
  setContext: (context: KeyboardContextState) => void;
  clearContext: () => void;
};

const KeyboardContext = createContext<KeyboardContextValue | null>(null);

export function useKeyboardMode() {
  const ctx = useContext(KeyboardContext);
  if (!ctx) throw new Error('useKeyboardMode must be used within KeyboardProvider');
  return ctx;
}

export default function KeyboardProvider({ children }: { children: React.ReactNode }) {
  const [mode, setMode] = useState<KeyboardModeState>('inactive');
  const previousFocusRef = useRef<HTMLElement | null>(null);
  const [actions, setActions] = useState<KeyboardAction[]>([]);
  const [context, setContextState] = useState<KeyboardContextState>({
    workspaceId: null,
    sessionId: null,
  });
  const modifierKeys = useMemo(() => new Set(['Shift', 'Control', 'Alt', 'Meta']), []);
  const pendingPrefixRef = useRef<string | null>(null);

  const setContext = useCallback((next: KeyboardContextState) => {
    setContextState((current) => {
      if (current.workspaceId === next.workspaceId && current.sessionId === next.sessionId) {
        return current;
      }
      return next;
    });
  }, []);

  const clearContext = useCallback(() => {
    setContextState((current) => {
      if (current.workspaceId === null && current.sessionId === null) {
        return current;
      }
      return { workspaceId: null, sessionId: null };
    });
  }, []);

  // Register an action
  const registerAction = useCallback((action: KeyboardAction) => {
    setActions((current) => {
      const existingIndex = current.findIndex(
        (a) =>
          a.key === action.key &&
          a.shiftKey === action.shiftKey &&
          a.prefixKey === action.prefixKey &&
          scopesEqual(a.scope, action.scope)
      );

      if (existingIndex === -1) {
        return [...current, action];
      }

      const existing = current[existingIndex];
      if (existing.description === action.description && existing.handler === action.handler) {
        return current;
      }

      const next = current.slice();
      next[existingIndex] = action;
      return next;
    });
  }, []);

  // Unregister an action
  const unregisterAction = useCallback(
    (key: string, shiftKey = false, scope?: KeyboardScope, prefixKey?: string) => {
      setActions((current) => {
        const next = current.filter((a) => {
          if (a.key !== key) return true;
          if (a.shiftKey !== shiftKey) return true;
          if (a.prefixKey !== prefixKey) return true;
          if (scope && !scopesEqual(a.scope, scope)) return true;
          return false;
        });
        return next.length === current.length ? current : next;
      });
    },
    []
  );

  // Enter keyboard mode
  const enterMode = useCallback(() => {
    // Save current focus
    const activeElement = document.activeElement as HTMLElement;
    previousFocusRef.current = activeElement;

    // Remove focus from any input/terminal
    if (activeElement instanceof HTMLElement) {
      activeElement.blur();
    }

    setMode('active');
  }, []);

  // Exit keyboard mode and restore focus
  const exitMode = useCallback(() => {
    setMode('inactive');
    pendingPrefixRef.current = null;
    if (previousFocusRef.current && previousFocusRef.current.focus) {
      previousFocusRef.current.focus();
    }
  }, []);

  // Handle keyboard events when mode is active
  useEffect(() => {
    if (mode !== 'active') return;

    const handleKeyDown = (e: KeyboardEvent) => {
      // Exit on Escape
      if (e.key === 'Escape') {
        e.preventDefault();
        exitMode();
        return;
      }

      // Keep keyboard mode active while users press modifier keys for chords
      if (modifierKeys.has(e.key)) {
        return;
      }

      // Normalize the pressed key.
      // For letters, uppercase means Shift was held - normalize to lowercase.
      // For other keys (?, /, etc.), use as-is and don't require shiftKey flag.
      const pressedKey = e.key.length === 1 ? e.key.toLowerCase() : e.key;

      // For letters (a-z), we need to check if Shift was used to produce uppercase
      const isLetter = pressedKey >= 'a' && pressedKey <= 'z';

      const matchesAction = (action: KeyboardAction) => {
        if (action.key.toLowerCase() !== pressedKey) return false;
        if (isLetter) {
          return !!action.shiftKey === e.shiftKey;
        }
        return true;
      };

      if (pendingPrefixRef.current) {
        const prefixedAction = actions.find(
          (a) => a.prefixKey === pendingPrefixRef.current && matchesAction(a)
        );
        pendingPrefixRef.current = null;
        if (prefixedAction) {
          e.preventDefault();
          prefixedAction.handler();
          exitMode();
          return;
        }
      }

      const hasPrefix = actions.some((a) => a.prefixKey === pressedKey);
      if (hasPrefix) {
        e.preventDefault();
        pendingPrefixRef.current = pressedKey;
        return;
      }

      const action = actions.find((a) => !a.prefixKey && matchesAction(a));

      if (action) {
        e.preventDefault();
        action.handler();
        exitMode();
      } else {
        // Unrecognized key - exit mode
        exitMode();
      }
    };

    window.addEventListener('keydown', handleKeyDown);

    // Exit mode if browser loses focus
    const handleBlur = () => {
      exitMode();
    };

    window.addEventListener('blur', handleBlur);

    return () => {
      window.removeEventListener('keydown', handleKeyDown);
      window.removeEventListener('blur', handleBlur);
    };
  }, [mode, actions, exitMode, modifierKeys]);

  // Prune actions that no longer match the active context
  useEffect(() => {
    setActions((current) => {
      const next = current.filter((action) => {
        const scope = action.scope;
        if (!scope || scope.type === 'global') {
          return true;
        }
        if (scope.type === 'workspace') {
          return context.workspaceId === scope.id;
        }
        if (scope.type === 'session') {
          return context.sessionId === scope.id;
        }
        return false;
      });
      return next.length === current.length ? current : next;
    });
  }, [context.workspaceId, context.sessionId]);

  // Global Cmd+K listener to enter mode
  useEffect(() => {
    const handleKeyDown = (e: KeyboardEvent) => {
      // Cmd+K or Ctrl+K (for Windows/Linux)
      if ((e.metaKey || e.ctrlKey) && e.key === 'k' && !e.shiftKey) {
        e.preventDefault();
        if (mode === 'inactive') {
          enterMode();
        } else {
          exitMode();
        }
      }
    };

    window.addEventListener('keydown', handleKeyDown);
    return () => window.removeEventListener('keydown', handleKeyDown);
  }, [mode, enterMode, exitMode]);

  const value = useMemo(
    () => ({
      mode,
      enterMode,
      exitMode,
      registerAction,
      unregisterAction,
      actions,
      context,
      setContext,
      clearContext,
    }),
    [
      mode,
      enterMode,
      exitMode,
      registerAction,
      unregisterAction,
      actions,
      context,
      setContext,
      clearContext,
    ]
  );

  return <KeyboardContext.Provider value={value}>{children}</KeyboardContext.Provider>;
}
