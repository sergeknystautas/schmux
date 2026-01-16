import React, { createContext, useContext, useCallback } from 'react';
import { useConfig } from './ConfigContext.jsx';
import useLocalStorage from '../hooks/useLocalStorage.js';

const ViewedSessionsContext = createContext();

export function ViewedSessionsProvider({ children }) {
  const [viewedSessions, setViewedSessions] = useLocalStorage('viewedSessions', {});
  const { config } = useConfig();

  const markAsViewed = useCallback((sessionId) => {
    // Read config at call time to avoid recreating this function when config changes
    const buffer = config?.internal?.viewed_buffer_ms || 5000;
    setViewedSessions((prev) => ({ ...prev, [sessionId]: Date.now() + buffer }));
  }, []); // Empty deps - function is stable, reads config dynamically

  return (
    <ViewedSessionsContext.Provider value={{ viewedSessions, markAsViewed }}>
      {children}
    </ViewedSessionsContext.Provider>
  );
}

export function useViewedSessions() {
  return useContext(ViewedSessionsContext);
}

