import React, {
  createContext,
  useCallback,
  useContext,
  useMemo,
  useRef,
  useEffect,
  useState,
} from 'react';
import { useNavigate } from 'react-router-dom';
import useSessionsWebSocket from '../hooks/useSessionsWebSocket';
import { useConfig } from './ConfigContext';
import { playAttentionSound, isAttentionState } from '../lib/notificationSound';
import type {
  SessionWithWorkspace,
  WorkspaceResponse,
  LinearSyncResolveConflictStatePayload,
  PendingNavigation,
} from '../lib/types';

type SessionsContextValue = {
  workspaces: WorkspaceResponse[];
  loading: boolean;
  error: string;
  connected: boolean;
  waitForSession: (
    sessionId: string,
    opts?: { timeoutMs?: number; intervalMs?: number }
  ) => Promise<boolean>;
  sessionsById: Record<string, SessionWithWorkspace>;
  linearSyncResolveConflictStates: Record<string, LinearSyncResolveConflictStatePayload>;
  clearLinearSyncResolveConflictState: (workspaceId: string) => void;
  pendingNavigation: PendingNavigation | null;
  setPendingNavigation: (nav: PendingNavigation | null) => void;
  clearPendingNavigation: () => void;
};

const SessionsContext = createContext<SessionsContextValue | null>(null);

export function SessionsProvider({ children }: { children: React.ReactNode }) {
  const navigate = useNavigate();
  const { config } = useConfig();
  const {
    workspaces,
    loading,
    connected,
    linearSyncResolveConflictStates,
    clearLinearSyncResolveConflictState,
  } = useSessionsWebSocket();
  const [pendingNavigation, setPendingNavigationState] = useState<PendingNavigation | null>(null);

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

  // Track previous nudge states to detect changes
  const prevNudgeStatesRef = useRef<Record<string, string | undefined>>({});

  // Detect nudge state changes and play notification sound
  useEffect(() => {
    const prevStates = prevNudgeStatesRef.current;
    let shouldPlaySound = false;

    Object.entries(sessionsById).forEach(([sessionId, session]) => {
      const prevState = prevStates[sessionId];
      const newState = session.nudge_state;

      // Only notify if state changed TO an attention state (not if it was already that state)
      if (newState !== prevState && isAttentionState(newState)) {
        shouldPlaySound = true;
      }
    });

    // Update ref with current states
    const newStates: Record<string, string | undefined> = {};
    Object.entries(sessionsById).forEach(([sessionId, session]) => {
      newStates[sessionId] = session.nudge_state;
    });
    prevNudgeStatesRef.current = newStates;

    // Play sound if any session transitioned to attention state (and sound is not disabled)
    if (shouldPlaySound && !config?.notifications?.sound_disabled) {
      playAttentionSound();
    }
  }, [sessionsById, config?.notifications?.sound_disabled]);

  // Keep a ref updated so waitForSession can always read current value
  const sessionsByIdRef = useRef(sessionsById);
  useEffect(() => {
    sessionsByIdRef.current = sessionsById;
  }, [sessionsById]);

  // Check for pending navigation matches whenever workspaces update
  useEffect(() => {
    if (!pendingNavigation) return;

    if (pendingNavigation.type === 'session') {
      const session = sessionsById[pendingNavigation.id];
      if (session) {
        navigate(`/sessions/${pendingNavigation.id}`);
        setPendingNavigationState(null);
      }
    } else if (pendingNavigation.type === 'workspace') {
      const workspace = workspaces.find((ws) => ws.id === pendingNavigation.id);
      if (workspace) {
        if (workspace.sessions?.length) {
          navigate(`/sessions/${workspace.sessions[0].id}`);
        } else {
          const hasChanges = workspace.git_lines_added > 0 || workspace.git_lines_removed > 0;
          if (hasChanges) {
            navigate(`/diff/${pendingNavigation.id}`);
          } else {
            navigate(`/spawn?workspace_id=${pendingNavigation.id}`);
          }
        }
        setPendingNavigationState(null);
      }
    }
  }, [workspaces, sessionsById, pendingNavigation, navigate]);

  const setPendingNavigation = useCallback((nav: PendingNavigation | null) => {
    setPendingNavigationState(nav);
  }, []);

  const clearPendingNavigation = useCallback(() => {
    setPendingNavigationState(null);
  }, []);

  const waitForSession = useCallback(
    async (sessionId: string, { timeoutMs = 8000, intervalMs = 500 } = {}) => {
      if (!sessionId) return false;
      // Check ref to get current value, not stale closure
      if (sessionsByIdRef.current[sessionId]) return true;

      // With WebSocket, we just need to wait for the next update
      // The server will broadcast when a session is created
      const deadline = Date.now() + timeoutMs;
      while (Date.now() < deadline) {
        // Check if session appeared (state updated via WebSocket)
        // Read from ref to get current value, not stale closure
        if (sessionsByIdRef.current[sessionId]) return true;
        await new Promise((resolve) => setTimeout(resolve, intervalMs));
      }
      return false;
    },
    []
  );

  const value = useMemo(
    () => ({
      workspaces,
      loading,
      error: '',
      connected,
      waitForSession,
      sessionsById,
      linearSyncResolveConflictStates,
      clearLinearSyncResolveConflictState,
      pendingNavigation,
      setPendingNavigation,
      clearPendingNavigation,
    }),
    [
      workspaces,
      loading,
      connected,
      waitForSession,
      sessionsById,
      linearSyncResolveConflictStates,
      clearLinearSyncResolveConflictState,
      pendingNavigation,
      setPendingNavigation,
      clearPendingNavigation,
    ]
  );

  return <SessionsContext.Provider value={value}>{children}</SessionsContext.Provider>;
}

export function useSessions() {
  const ctx = useContext(SessionsContext);
  if (!ctx) {
    throw new Error('useSessions must be used within a SessionsProvider');
  }
  return ctx;
}
