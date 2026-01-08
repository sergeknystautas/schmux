import React, { createContext, useState, useContext } from 'react';

const ViewedSessionsContext = createContext();

export function ViewedSessionsProvider({ children }) {
  const [viewedSessions, setViewedSessions] = useState({}); // sessionId -> timestamp

  const markAsViewed = (sessionId) => {
    setViewedSessions((prev) => ({ ...prev, [sessionId]: Date.now() }));
  };

  return (
    <ViewedSessionsContext.Provider value={{ viewedSessions, markAsViewed }}>
      {children}
    </ViewedSessionsContext.Provider>
  );
}

export function useViewedSessions() {
  return useContext(ViewedSessionsContext);
}

