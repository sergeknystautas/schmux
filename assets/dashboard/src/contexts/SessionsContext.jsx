import React, { createContext, useCallback, useContext, useEffect, useMemo, useState } from 'react';
import { getSessions } from '../lib/api.js';
import { useConfig } from './ConfigContext.jsx';

const SessionsContext = createContext(null);

export function SessionsProvider({ children }) {
  const { config } = useConfig();
  const [workspaces, setWorkspaces] = useState([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState('');

  const loadSessions = useCallback(async (silent = false) => {
    if (!silent) {
      setLoading(true);
    }
    setError('');
    try {
      const data = await getSessions();
      setWorkspaces(data);
      return data;
    } catch (err) {
      if (!silent) {
        setError(err.message || 'Failed to load sessions');
      }
      return null;
    } finally {
      if (!silent) {
        setLoading(false);
      }
    }
  }, []);

  useEffect(() => {
    loadSessions();
  }, [loadSessions]);

  useEffect(() => {
    const pollInterval = config.internal?.sessions_poll_interval_ms || 5000;
    const interval = setInterval(() => {
      loadSessions(true);
    }, pollInterval);
    return () => clearInterval(interval);
  }, [loadSessions, config]);

  const sessionsById = useMemo(() => {
    const map = {};
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

  const waitForSession = useCallback(async (sessionId, { timeoutMs = 8000, intervalMs = 500 } = {}) => {
    if (!sessionId) return false;
    if (sessionsById[sessionId]) return true;

    const deadline = Date.now() + timeoutMs;
    while (Date.now() < deadline) {
      const data = await loadSessions(true);
      if (data) {
        for (const ws of data) {
          if ((ws.sessions || []).some((sess) => sess.id === sessionId)) {
            return true;
          }
        }
      }
      await new Promise((resolve) => setTimeout(resolve, intervalMs));
    }
    return false;
  }, [loadSessions, sessionsById]);

  const value = useMemo(() => ({
    workspaces,
    loading,
    error,
    refresh: loadSessions,
    waitForSession,
    sessionsById,
  }), [workspaces, loading, error, loadSessions, waitForSession, sessionsById]);

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
