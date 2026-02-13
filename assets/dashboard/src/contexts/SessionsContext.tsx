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
import { playAttentionSound, isAttentionState, warmupAudioContext } from '../lib/notificationSound';
import { removePreviewIframe } from '../lib/previewKeepAlive';
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

  useEffect(() => {
    warmupAudioContext();
  }, []);

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

  // Detect nudge state changes and play notification sound (localStorage-based deduplication)
  useEffect(() => {
    let shouldPlaySound = false;

    Object.entries(sessionsById).forEach(([sessionId, session]) => {
      const nudgeSeq = session.nudge_seq ?? 0;
      if (nudgeSeq === 0) return;

      const storageKey = `schmux:ack:${sessionId}`;
      const lastAckedSeq = parseInt(localStorage.getItem(storageKey) || '0', 10);

      if (nudgeSeq > lastAckedSeq && isAttentionState(session.nudge_state)) {
        shouldPlaySound = true;
        localStorage.setItem(storageKey, String(nudgeSeq));
      }
    });

    // Cleanup: remove localStorage entries for sessions that no longer exist
    // Iterate backwards to avoid skipping entries when removeItem shifts indices.
    const currentSessionIds = new Set(Object.keys(sessionsById));
    for (let i = localStorage.length - 1; i >= 0; i--) {
      const key = localStorage.key(i);
      if (key?.startsWith('schmux:ack:')) {
        const sessionId = key.slice('schmux:ack:'.length);
        if (!currentSessionIds.has(sessionId)) {
          localStorage.removeItem(key);
        }
      }
    }

    if (shouldPlaySound && !config?.notifications?.sound_disabled) {
      playAttentionSound();
    }
  }, [sessionsById, config?.notifications?.sound_disabled]);

  // Keep a ref updated so waitForSession can always read current value
  const sessionsByIdRef = useRef(sessionsById);
  useEffect(() => {
    sessionsByIdRef.current = sessionsById;
  }, [sessionsById]);

  // Clean up preview iframes when previews disappear
  const prevPreviewIdsRef = useRef<Set<string>>(new Set());
  useEffect(() => {
    const currentPreviewIds = new Set<string>();
    workspaces.forEach((ws) => {
      (ws.previews || []).forEach((p) => currentPreviewIds.add(p.id));
    });

    // Remove iframes for previews that no longer exist
    for (const id of prevPreviewIdsRef.current) {
      if (!currentPreviewIds.has(id)) {
        removePreviewIframe(id);
      }
    }

    prevPreviewIdsRef.current = currentPreviewIds;
  }, [workspaces]);

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
