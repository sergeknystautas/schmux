import { useCallback, useEffect, useRef, useState } from 'react';
import type { WorkspaceResponse } from '../lib/types';

const RECONNECT_DELAY_MS = 2000;
const MAX_RECONNECT_DELAY_MS = 30000;

type SessionsWebSocketState = {
  workspaces: WorkspaceResponse[];
  connected: boolean;
  loading: boolean;
};

export default function useSessionsWebSocket(): SessionsWebSocketState & {
  refresh: () => void;
} {
  const [workspaces, setWorkspaces] = useState<WorkspaceResponse[]>([]);
  const [connected, setConnected] = useState(false);
  const [loading, setLoading] = useState(true);
  const wsRef = useRef<WebSocket | null>(null);
  const reconnectTimeoutRef = useRef<number | null>(null);
  const reconnectDelayRef = useRef(RECONNECT_DELAY_MS);
  const mountedRef = useRef(true);

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
      // Reset reconnect delay on successful connection
      reconnectDelayRef.current = RECONNECT_DELAY_MS;
    };

    ws.onmessage = (event) => {
      if (!mountedRef.current) return;
      try {
        const data = JSON.parse(event.data);
        // Handle different message types
        if (data.type === 'sessions' && data.workspaces) {
          setWorkspaces(data.workspaces);
          setLoading(false);
        }
        // Future: handle data.type === 'config' here
      } catch (e) {
        console.error('[ws/dashboard] failed to parse message:', e);
      }
    };

    ws.onclose = () => {
      if (!mountedRef.current) return;
      setConnected(false);
      wsRef.current = null;

      // Schedule reconnect with exponential backoff
      reconnectTimeoutRef.current = window.setTimeout(() => {
        reconnectDelayRef.current = Math.min(
          reconnectDelayRef.current * 2,
          MAX_RECONNECT_DELAY_MS
        );
        connect();
      }, reconnectDelayRef.current);
    };

    ws.onerror = () => {
      if (!mountedRef.current) return;
      // onclose will be called after onerror, so we don't need to do anything here
    };
  }, []);

  // Manual refresh - just reconnect to get fresh state
  const refresh = useCallback(() => {
    connect();
  }, [connect]);

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

  return { workspaces, connected, loading, refresh };
}
