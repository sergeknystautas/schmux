import { useCallback, useEffect, useRef, useState } from 'react';
import { transport } from '../lib/transport';
import type {
  WorkspaceResponse,
  LinearSyncResolveConflictStatePayload,
  WorkspaceLockState,
  OverlayChangeEvent,
  RemoteAccessStatus,
  WorkspaceSyncResultEvent,
  CuratorStreamEvent,
  CurationRun,
  MonitorEvent,
} from '../lib/types';

const RECONNECT_DELAY_MS = 2000;
const MAX_RECONNECT_DELAY_MS = 30000;

// --- Runtime type guards for WebSocket messages ---

function isString(v: unknown): v is string {
  return typeof v === 'string';
}

function isBoolean(v: unknown): v is boolean {
  return typeof v === 'boolean';
}

function isNumber(v: unknown): v is number {
  return typeof v === 'number';
}

function isObject(v: unknown): v is Record<string, unknown> {
  return typeof v === 'object' && v !== null && !Array.isArray(v);
}

function isSessionsMessage(
  data: Record<string, unknown>
): data is { type: 'sessions'; workspaces: WorkspaceResponse[] } {
  return data.type === 'sessions' && Array.isArray(data.workspaces);
}

function isLinearSyncMessage(
  data: Record<string, unknown>
): data is { type: 'linear_sync_resolve_conflict'; workspace_id: string; status: string } & Record<
  string,
  unknown
> {
  return data.type === 'linear_sync_resolve_conflict' && isString(data.workspace_id);
}

function isWorkspaceLockedMessage(data: Record<string, unknown>): data is {
  type: 'workspace_locked';
  workspace_id: string;
  locked: boolean;
  sync_progress?: { current: number; total: number };
  sync_result?: {
    success?: boolean;
    success_count?: number;
    conflicting_hash?: string;
    branch?: string;
    message?: string;
  };
} {
  return data.type === 'workspace_locked' && isString(data.workspace_id) && isBoolean(data.locked);
}

function isOverlayChangeMessage(
  data: Record<string, unknown>
): data is OverlayChangeEvent & Record<string, unknown> {
  return (
    data.type === 'overlay_change' && isString(data.rel_path) && isString(data.source_workspace_id)
  );
}

function isRemoteAccessMessage(
  data: Record<string, unknown>
): data is { type: 'remote_access_status'; data: RemoteAccessStatus } {
  return (
    data.type === 'remote_access_status' &&
    isObject(data.data) &&
    isString((data.data as Record<string, unknown>).state)
  );
}

function isPendingNavigationMessage(
  data: Record<string, unknown>
): data is { type: 'pending_navigation'; navType: 'preview'; id1: string; id2: string } {
  return (
    data.type === 'pending_navigation' &&
    data.navType === 'preview' &&
    isString(data.id1) &&
    isString(data.id2)
  );
}

function isCuratorEventMessage(
  data: Record<string, unknown>
): data is { type: 'curator_event'; event: CuratorStreamEvent } {
  return data.type === 'curator_event' && isObject(data.event);
}

function isCuratorStateMessage(
  data: Record<string, unknown>
): data is { type: 'curator_state'; run: CurationRun } {
  return data.type === 'curator_state' && isObject(data.run);
}

function isMonitorEventMessage(
  data: Record<string, unknown>
): data is { type: 'event'; session_id: string; event: Record<string, unknown> } {
  return data.type === 'event' && isString(data.session_id) && isObject(data.event);
}

function parseSyncProgress(v: unknown): { current: number; total: number } | undefined {
  if (!isObject(v)) return undefined;
  if (!isNumber(v.current) || !isNumber(v.total)) return undefined;
  return { current: v.current, total: v.total };
}

function parseSyncResult(v: unknown):
  | {
      success: boolean;
      success_count?: number;
      conflicting_hash?: string;
      branch?: string;
      message?: string;
    }
  | undefined {
  if (!isObject(v)) return undefined;
  if (!isBoolean(v.success)) return undefined;
  return {
    success: v.success,
    success_count: isNumber(v.success_count) ? v.success_count : undefined,
    conflicting_hash: isString(v.conflicting_hash) ? v.conflicting_hash : undefined,
    branch: isString(v.branch) ? v.branch : undefined,
    message: isString(v.message) ? v.message : undefined,
  };
}

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
  curatorEvents: Record<string, CuratorStreamEvent[]>;
  monitorEvents: MonitorEvent[];
  clearMonitorEvents: () => void;
  subredditUpdateCount: number;
  repofeedUpdateCount: number;
};

export default function useSessionsWebSocket(opts?: {
  onPreviewDetected?: (workspaceId: string, previewId: string) => void;
  onConfigUpdated?: () => void;
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
  const [curatorEvents, setCuratorEvents] = useState<Record<string, CuratorStreamEvent[]>>({});
  const [monitorEvents, setMonitorEvents] = useState<MonitorEvent[]>([]);
  const [subredditUpdateCount, setSubredditUpdateCount] = useState(0);
  const [repofeedUpdateCount, setRepofeedUpdateCount] = useState(0);
  const onPreviewDetectedRef = useRef(opts?.onPreviewDetected);
  onPreviewDetectedRef.current = opts?.onPreviewDetected;
  const onConfigUpdatedRef = useRef(opts?.onConfigUpdated);
  onConfigUpdatedRef.current = opts?.onConfigUpdated;
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
    const ws = transport.createWebSocket(`${protocol}//${window.location.host}/ws/dashboard`);
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
        const data = JSON.parse(raw) as Record<string, unknown>;
        if (!isObject(data) || !isString(data.type)) return;

        if (isSessionsMessage(data)) {
          if (raw !== lastSessionsMsgRef.current) {
            lastSessionsMsgRef.current = raw;
            setWorkspaces(data.workspaces);
          }
          setLoading(false);
        } else if (isLinearSyncMessage(data)) {
          const wsId = data.workspace_id;
          if (dismissedCrStatesRef.current.has(wsId)) {
            if (data.status === 'in_progress') {
              dismissedCrStatesRef.current.delete(wsId);
            } else {
              return;
            }
          }
          setLinearSyncResolveConflictStates((prev) => ({
            ...prev,
            [wsId]: data as unknown as LinearSyncResolveConflictStatePayload,
          }));
        } else if (isWorkspaceLockedMessage(data)) {
          const wsId = data.workspace_id;
          const locked = data.locked;
          if (locked) {
            const syncProgress = parseSyncProgress(data.sync_progress);
            setWorkspaceLockStates((prev) => ({
              ...prev,
              [wsId]: { locked: true, syncProgress: syncProgress ?? prev[wsId]?.syncProgress },
            }));
            if (syncProgress) {
              const remaining = syncProgress.total - syncProgress.current;
              setWorkspaces((prevWs) =>
                prevWs.map((w) => (w.id === wsId ? { ...w, behind: remaining } : w))
              );
            }
          } else {
            setWorkspaceLockStates((prev) => {
              const prevLock = prev[wsId];
              if (!prevLock) return prev;
              if (prevLock.syncProgress) {
                const remaining = prevLock.syncProgress.total - prevLock.syncProgress.current;
                setWorkspaces((prevWs) =>
                  prevWs.map((w) => (w.id === wsId ? { ...w, behind: remaining } : w))
                );
              }
              const next = { ...prev };
              delete next[wsId];
              return next;
            });

            const rawSyncResult = parseSyncResult(data.sync_result);
            if (rawSyncResult) {
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
        } else if (isOverlayChangeMessage(data)) {
          setOverlayEvents((prev) => [data as OverlayChangeEvent, ...prev]);
        } else if (isRemoteAccessMessage(data)) {
          setRemoteAccessStatus(data.data);
        } else if (isPendingNavigationMessage(data)) {
          onPreviewDetectedRef.current?.(data.id1, data.id2);
        } else if (isCuratorEventMessage(data)) {
          const ev = data.event as CuratorStreamEvent;
          setCuratorEvents((prev) => {
            const existing = prev[ev.repo] || [];
            const lastExisting = existing[existing.length - 1];
            // If the last event was terminal, this is a new run — start fresh
            if (
              lastExisting &&
              (lastExisting.event_type === 'curator_done' ||
                lastExisting.event_type === 'curator_error')
            ) {
              return { ...prev, [ev.repo]: [ev] };
            }
            return { ...prev, [ev.repo]: [...existing, ev] };
          });
        } else if (isCuratorStateMessage(data)) {
          const run = data.run as CurationRun;
          setCuratorEvents((prev) => ({
            ...prev,
            [run.repo]: run.events,
          }));
        } else if (isMonitorEventMessage(data)) {
          setMonitorEvents((prev) => {
            const next = [
              ...prev,
              { session_id: data.session_id, event: data.event as MonitorEvent['event'] },
            ];
            // Ring buffer: keep last 200
            return next.length > 200 ? next.slice(next.length - 200) : next;
          });
        } else if (data.type === 'subreddit_updated') {
          setSubredditUpdateCount((prev) => prev + 1);
        } else if (data.type === 'repofeed_updated') {
          setRepofeedUpdateCount((prev) => prev + 1);
        } else if (data.type === 'config_updated') {
          onConfigUpdatedRef.current?.();
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

  const clearMonitorEvents = useCallback(() => {
    setMonitorEvents([]);
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
    curatorEvents,
    monitorEvents,
    clearMonitorEvents,
    subredditUpdateCount,
    repofeedUpdateCount,
  };
}
