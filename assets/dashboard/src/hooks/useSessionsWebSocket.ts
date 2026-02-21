import { useCallback, useEffect, useRef, useState } from 'react';
import type {
  WorkspaceResponse,
  LinearSyncResolveConflictStatePayload,
  WorkspaceLockState,
  OverlayChangeEvent,
  RemoteAccessStatus,
  WorkspaceSyncResultEvent,
} from '../lib/types';

const RECONNECT_DELAY_MS = 2000;
const MAX_RECONNECT_DELAY_MS = 30000;

type SessionsWebSocketState = {
  workspaces: WorkspaceResponse[];
  connected: boolean;
  loading: boolean;
  stale: boolean;
  linearSyncResolveConflictStates: Record<string, LinearSyncResolveConflictStatePayload>;
  clearLinearSyncResolveConflictState: (workspaceId: string) => void;
  workspaceLockStates: Record<string, WorkspaceLockState>;
  syncResultEvents: WorkspaceSyncResultEvent[];
  clearSyncResultEvents: () => void;
  overlayEvents: OverlayChangeEvent[];
  clearOverlayEvents: () => void;
  remoteAccessStatus: RemoteAccessStatus;
};

export default function useSessionsWebSocket(opts?: {
  onPreviewDetected?: (workspaceId: string, previewId: string) => void;
}): SessionsWebSocketState {
  const [workspaces, setWorkspaces] = useState<WorkspaceResponse[]>([]);
  const [connected, setConnected] = useState(false);
  const [loading, setLoading] = useState(true);
  const [stale, setStale] = useState(false);
  const [linearSyncResolveConflictStates, setLinearSyncResolveConflictStates] = useState<
    Record<string, LinearSyncResolveConflictStatePayload>
  >({});
  const [workspaceLockStates, setWorkspaceLockStates] = useState<
    Record<string, WorkspaceLockState>
  >({});
  const [syncResultEvents, setSyncResultEvents] = useState<WorkspaceSyncResultEvent[]>([]);
  const [overlayEvents, setOverlayEvents] = useState<OverlayChangeEvent[]>([]);
  const [remoteAccessStatus, setRemoteAccessStatus] = useState<RemoteAccessStatus>({
    state: 'off',
  });
  const onPreviewDetectedRef = useRef(opts?.onPreviewDetected);
  onPreviewDetectedRef.current = opts?.onPreviewDetected;
  const wsRef = useRef<WebSocket | null>(null);
  const reconnectTimeoutRef = useRef<number | null>(null);
  const reconnectDelayRef = useRef(RECONNECT_DELAY_MS);
  const mountedRef = useRef(true);
  const lastSessionsMsgRef = useRef<string>('');
  // Track workspace IDs whose conflict state has been locally dismissed.
  // Prevents WS broadcasts from re-adding stale completed/failed states
  // before the DELETE request is processed by the backend.
  const dismissedCrStatesRef = useRef<Set<string>>(new Set());

  const connect = useCallback(() => {
    if (!mountedRef.current) return;

    // Clear any pending reconnect
    if (reconnectTimeoutRef.current) {
      window.clearTimeout(reconnectTimeoutRef.current);
      reconnectTimeoutRef.current = null;
    }

    // Close existing connection if any
    if (wsRef.current) {
      wsRef.current.close();
      wsRef.current = null;
    }

    const protocol = window.location.protocol === 'https:' ? 'wss:' : 'ws:';
    const ws = new WebSocket(`${protocol}//${window.location.host}/ws/dashboard`);
    wsRef.current = ws;

    ws.onopen = () => {
      if (!mountedRef.current) return;
      setConnected(true);
      setStale(false);
      // Reset reconnect delay on successful connection
      reconnectDelayRef.current = RECONNECT_DELAY_MS;
    };

    ws.onmessage = (event) => {
      if (!mountedRef.current) return;
      try {
        const raw = event.data as string;
        const data = JSON.parse(raw);
        // Handle different message types
        if (data.type === 'sessions' && data.workspaces) {
          // Structural sharing: skip update if data hasn't changed.
          // Raw string comparison avoids React re-render cascade when
          // the WebSocket broadcasts identical state.
          if (raw !== lastSessionsMsgRef.current) {
            lastSessionsMsgRef.current = raw;
            setWorkspaces(data.workspaces);
          }
          setLoading(false);
        } else if (data.type === 'linear_sync_resolve_conflict' && data.workspace_id) {
          const wsId = data.workspace_id as string;
          if (dismissedCrStatesRef.current.has(wsId)) {
            // A new in_progress state means a genuinely new conflict resolution —
            // clear the dismissal so the tab reappears.
            if (data.status === 'in_progress') {
              dismissedCrStatesRef.current.delete(wsId);
            } else {
              // Stale completed/failed state re-broadcast; ignore it.
              return;
            }
          }
          setLinearSyncResolveConflictStates((prev) => ({
            ...prev,
            [wsId]: data,
          }));
        } else if (data.type === 'workspace_locked' && data.workspace_id) {
          const wsId = data.workspace_id as string;
          const locked = data.locked as boolean;
          if (locked) {
            const syncProgress = data.sync_progress
              ? {
                  current: data.sync_progress.current as number,
                  total: data.sync_progress.total as number,
                }
              : undefined;
            setWorkspaceLockStates((prev) => ({
              ...prev,
              [wsId]: { locked: true, syncProgress: syncProgress ?? prev[wsId]?.syncProgress },
            }));
            // Optimistically decrement git_behind as each rebase step completes
            if (syncProgress) {
              const remaining = syncProgress.total - syncProgress.current;
              setWorkspaces((prevWs) =>
                prevWs.map((w) => (w.id === wsId ? { ...w, git_behind: remaining } : w))
              );
            }
          } else {
            setWorkspaceLockStates((prev) => {
              const prevLock = prev[wsId];
              if (!prevLock) return prev;
              // Final optimistic update from last known progress
              if (prevLock.syncProgress) {
                const remaining = prevLock.syncProgress.total - prevLock.syncProgress.current;
                setWorkspaces((prevWs) =>
                  prevWs.map((w) => (w.id === wsId ? { ...w, git_behind: remaining } : w))
                );
              }
              const next = { ...prev };
              delete next[wsId];
              return next;
            });

            const rawSyncResult = data.sync_result as
              | {
                  success?: boolean;
                  success_count?: number;
                  conflicting_hash?: string;
                  branch?: string;
                  message?: string;
                }
              | undefined;
            if (rawSyncResult && typeof rawSyncResult.success === 'boolean') {
              setSyncResultEvents((prev) => [
                ...prev,
                {
                  id: `${wsId}:${Date.now()}:${Math.random().toString(36).slice(2)}`,
                  workspace_id: wsId,
                  success: rawSyncResult.success,
                  success_count: rawSyncResult.success_count,
                  conflicting_hash: rawSyncResult.conflicting_hash,
                  branch: rawSyncResult.branch,
                  message: rawSyncResult.message,
                },
              ]);
            }
          }
        } else if (data.type === 'overlay_change') {
          setOverlayEvents((prev) => [data as OverlayChangeEvent, ...prev].slice(0, 100));
        } else if (data.type === 'remote_access_status' && data.data) {
          setRemoteAccessStatus(data.data as RemoteAccessStatus);
        } else if (data.type === 'pending_navigation' && data.navType === 'preview') {
          onPreviewDetectedRef.current?.(data.id1 as string, data.id2 as string);
        }
      } catch (e) {
        console.error('[ws/dashboard] failed to parse message:', e);
      }
    };

    ws.onclose = () => {
      if (!mountedRef.current) return;
      setConnected(false);
      setStale(true);
      wsRef.current = null;

      // Schedule reconnect with exponential backoff and jitter
      const jitter = reconnectDelayRef.current * (0.5 + Math.random());
      reconnectTimeoutRef.current = window.setTimeout(() => {
        reconnectDelayRef.current = Math.min(reconnectDelayRef.current * 2, MAX_RECONNECT_DELAY_MS);
        connect();
      }, jitter);
    };

    ws.onerror = () => {
      if (!mountedRef.current) return;
      // onclose will be called after onerror, so we don't need to do anything here
    };
  }, []);

  useEffect(() => {
    mountedRef.current = true;
    connect();

    return () => {
      mountedRef.current = false;
      if (reconnectTimeoutRef.current) {
        window.clearTimeout(reconnectTimeoutRef.current);
      }
      if (wsRef.current) {
        wsRef.current.close();
      }
    };
  }, [connect]);

  const clearLinearSyncResolveConflictState = useCallback((workspaceId: string) => {
    dismissedCrStatesRef.current.add(workspaceId);
    setLinearSyncResolveConflictStates((prev) => {
      if (!prev[workspaceId]) return prev;
      const next = { ...prev };
      delete next[workspaceId];
      return next;
    });
  }, []);

  const clearOverlayEvents = useCallback(() => {
    setOverlayEvents([]);
  }, []);

  const clearSyncResultEvents = useCallback(() => {
    setSyncResultEvents([]);
  }, []);

  return {
    workspaces,
    connected,
    loading,
    stale,
    linearSyncResolveConflictStates,
    clearLinearSyncResolveConflictState,
    workspaceLockStates,
    syncResultEvents,
    clearSyncResultEvents,
    overlayEvents,
    clearOverlayEvents,
    remoteAccessStatus,
  };
}
