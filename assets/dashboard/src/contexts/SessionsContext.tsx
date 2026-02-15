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
  waitForSession: (sessionId: string, opts?: { timeoutMs?: number }) => Promise<boolean>;
  sessionsById: Record<string, SessionWithWorkspace>;
  ackSession: (sessionId: string) => void;
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
  const lastProcessedNudgeRef = useRef<Record<string, number>>({});
  const lastCleanupRef = useRef(0);

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

  // Detect nudge state changes and play notification sound
  useEffect(() => {
    let shouldPlaySound = false;

    Object.entries(sessionsById).forEach(([sessionId, session]) => {
      const nudgeSeq = session.nudge_seq ?? 0;
      if (nudgeSeq === 0) return;

      // Skip if we already processed this exact seq in-memory
      if (lastProcessedNudgeRef.current[sessionId] === nudgeSeq) return;
      lastProcessedNudgeRef.current[sessionId] = nudgeSeq;

      const storageKey = `schmux:ack:${sessionId}`;
      const lastAckedSeq = parseInt(localStorage.getItem(storageKey) || '0', 10);

      if (nudgeSeq > lastAckedSeq && isAttentionState(session.nudge_state)) {
        shouldPlaySound = true;
        localStorage.setItem(storageKey, String(nudgeSeq));
      }
    });

    // Cleanup: remove localStorage entries for sessions that no longer exist.
    // Throttled to once per minute to avoid scanning all localStorage keys on every update.
    const now = Date.now();
    if (now - lastCleanupRef.current > 60_000) {
      lastCleanupRef.current = now;
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
    }

    if (shouldPlaySound && !config?.notifications?.sound_disabled) {
      playAttentionSound();
    }
  }, [sessionsById, config?.notifications?.sound_disabled]);

  // Keep a ref updated so waitForSession can always read current value
  const sessionsByIdRef = useRef(sessionsById);
  // Listeners notified whenever sessionsById updates (for event-driven waitForSession)
  const sessionListenersRef = useRef<Set<() => void>>(new Set());
  useEffect(() => {
    sessionsByIdRef.current = sessionsById;
    // Notify all waiting listeners that sessions have been updated
    for (const listener of sessionListenersRef.current) {
      listener();
    }
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

  const waitForSession = useCallback(async (sessionId: string, { timeoutMs = 8000 } = {}) => {
    if (!sessionId) return false;
    // Check ref to get current value, not stale closure
    if (sessionsByIdRef.current[sessionId]) return true;

    // Event-driven: resolve when a WebSocket update includes the target session
    return new Promise<boolean>((resolve) => {
      let settled = false;
      const listener = () => {
        if (sessionsByIdRef.current[sessionId] && !settled) {
          settled = true;
          sessionListenersRef.current.delete(listener);
          resolve(true);
        }
      };
      sessionListenersRef.current.add(listener);

      // Timeout fallback
      setTimeout(() => {
        if (!settled) {
          settled = true;
          sessionListenersRef.current.delete(listener);
          resolve(!!sessionsByIdRef.current[sessionId]);
        }
      }, timeoutMs);
    });
  }, []);

  // Acknowledge a session's current nudge_seq so the sound won't replay on reload.
  // Called when the user navigates to a session (views it).
  const ackSession = useCallback((sessionId: string) => {
    const session = sessionsByIdRef.current[sessionId];
    const nudgeSeq = session?.nudge_seq ?? 0;
    if (nudgeSeq === 0) return;
    const storageKey = `schmux:ack:${sessionId}`;
    const lastAckedSeq = parseInt(localStorage.getItem(storageKey) || '0', 10);
    if (nudgeSeq > lastAckedSeq) {
      localStorage.setItem(storageKey, String(nudgeSeq));
    }
    lastProcessedNudgeRef.current[sessionId] = nudgeSeq;
  }, []);

  const value = useMemo(
    () => ({
      workspaces,
      loading,
      error: '',
      connected,
      waitForSession,
      sessionsById,
      ackSession,
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
      ackSession,
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
