import React, { createContext, useCallback, useContext, useMemo } from 'react';
import useSessionsWebSocket from '../hooks/useSessionsWebSocket';
import type { SessionWithWorkspace, WorkspaceResponse } from '../lib/types';

type SessionsContextValue = {
  workspaces: WorkspaceResponse[];
  loading: boolean;
  error: string;
  connected: boolean;
  refresh: () => void;
  waitForSession: (sessionId: string, opts?: { timeoutMs?: number; intervalMs?: number }) => Promise<boolean>;
  sessionsById: Record<string, SessionWithWorkspace>;
};

const SessionsContext = createContext<SessionsContextValue | null>(null);

export function SessionsProvider({ children }: { children: React.ReactNode }) {
  const { workspaces, loading, connected, refresh } = useSessionsWebSocket();

  const sessionsById = useMemo(() => {
    const map: Record<string, SessionWithWorkspace> = {};
    workspaces.forEach((ws) => {
      (ws.sessions || []).forEach((sess) => {
        map[sess.id] = {
          ...sess,
          workspace_id: ws.id,
          workspace_path: ws.path,
          repo: ws.repo,
          branch: ws.branch,
        };
      });
    });
    return map;
  }, [workspaces]);

  const waitForSession = useCallback(async (sessionId: string, { timeoutMs = 8000, intervalMs = 500 } = {}) => {
    if (!sessionId) return false;
    if (sessionsById[sessionId]) return true;

    // With WebSocket, we just need to wait for the next update
    // The server will broadcast when a session is created
    const deadline = Date.now() + timeoutMs;
    while (Date.now() < deadline) {
      // Check if session appeared (state updated via WebSocket)
      if (sessionsById[sessionId]) return true;
      await new Promise((resolve) => setTimeout(resolve, intervalMs));
    }
    return false;
  }, [sessionsById]);

  const value = useMemo(() => ({
    workspaces,
    loading,
    error: '', // No error state with WebSocket - connected/disconnected handles it
    connected,
    refresh,
    waitForSession,
    sessionsById,
  }), [workspaces, loading, connected, refresh, waitForSession, sessionsById]);

  return (
    <SessionsContext.Provider value={value}>
      {children}
    </SessionsContext.Provider>
  );
}

export function useSessions() {
  const ctx = useContext(SessionsContext);
  if (!ctx) {
    throw new Error('useSessions must be used within a SessionsProvider');
  }
  return ctx;
}
