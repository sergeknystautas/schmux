import { useCallback, useEffect, useRef, useState } from 'react';
import type { WorkspaceResponse, LinearSyncResolveConflictStatePayload } from '../lib/types';

const RECONNECT_DELAY_MS = 2000;
const MAX_RECONNECT_DELAY_MS = 30000;

type SessionsWebSocketState = {
  workspaces: WorkspaceResponse[];
  connected: boolean;
  loading: boolean;
  stale: boolean;
  linearSyncResolveConflictStates: Record<string, LinearSyncResolveConflictStatePayload>;
  clearLinearSyncResolveConflictState: (workspaceId: string) => void;
};

export default function useSessionsWebSocket(): SessionsWebSocketState {
  const [workspaces, setWorkspaces] = useState<WorkspaceResponse[]>([]);
  const [connected, setConnected] = useState(false);
  const [loading, setLoading] = useState(true);
  const [stale, setStale] = useState(false);
  const [linearSyncResolveConflictStates, setLinearSyncResolveConflictStates] = useState<
    Record<string, LinearSyncResolveConflictStatePayload>
  >({});
  const wsRef = useRef<WebSocket | null>(null);
  const reconnectTimeoutRef = useRef<number | null>(null);
  const reconnectDelayRef = useRef(RECONNECT_DELAY_MS);
  const mountedRef = useRef(true);
  const lastSessionsMsgRef = useRef<string>('');

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
          setLinearSyncResolveConflictStates((prev) => ({
            ...prev,
            [data.workspace_id]: data,
          }));
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

      // Schedule reconnect with exponential backoff
      reconnectTimeoutRef.current = window.setTimeout(() => {
        reconnectDelayRef.current = Math.min(reconnectDelayRef.current * 2, MAX_RECONNECT_DELAY_MS);
        connect();
      }, reconnectDelayRef.current);
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
    setLinearSyncResolveConflictStates((prev) => {
      if (!prev[workspaceId]) return prev;
      const next = { ...prev };
      delete next[workspaceId];
      return next;
    });
  }, []);

  return {
    workspaces,
    connected,
    loading,
    stale,
    linearSyncResolveConflictStates,
    clearLinearSyncResolveConflictState,
  };
}
