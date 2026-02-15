import React, { createContext, useContext, useCallback, useEffect, useMemo, useRef } from 'react';
import { useConfig } from './ConfigContext';
import useLocalStorage, { VIEWED_SESSIONS_KEY } from '../hooks/useLocalStorage';

type ViewedSessionsContextValue = {
  viewedSessions: Record<string, number>;
  markAsViewed: (sessionId: string) => void;
};

const ViewedSessionsContext = createContext<ViewedSessionsContextValue | null>(null);

export function ViewedSessionsProvider({ children }: { children: React.ReactNode }) {
  const [viewedSessions, setViewedSessions] = useLocalStorage<Record<string, number>>(
    VIEWED_SESSIONS_KEY,
    {}
  );
  const { config } = useConfig();
  const configRef = useRef(config);
  useEffect(() => {
    configRef.current = config;
  }, [config]);

  const markAsViewed = useCallback((sessionId: string) => {
    // Read config via ref at call time to avoid recreating this function when config changes
    const buffer = configRef.current?.nudgenik?.viewed_buffer_ms || 5000;
    setViewedSessions((prev) => ({ ...prev, [sessionId]: Date.now() + buffer }));
  }, []); // Empty deps - function is stable, reads config via ref

  const value = useMemo(() => ({ viewedSessions, markAsViewed }), [viewedSessions, markAsViewed]);

  return <ViewedSessionsContext.Provider value={value}>{children}</ViewedSessionsContext.Provider>;
}

export function useViewedSessions() {
  const ctx = useContext(ViewedSessionsContext);
  if (!ctx) {
    throw new Error('useViewedSessions must be used within a ViewedSessionsProvider');
  }
  return ctx;
}
