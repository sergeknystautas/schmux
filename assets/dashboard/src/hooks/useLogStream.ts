import { useEffect, useRef, useState } from 'react';
import { transport } from '../lib/transport';

// useLogStream is the shared engine for every Logs-tab tailer. It opens a
// dedicated websocket to `path` (relative to the dashboard origin), decodes
// the typed paged envelope (history/history_end/append/history_error), runs a
// source-specific parser on each line, and exposes stable-keyed items plus
// paging state. A null path stays disconnected — for example, before the user
// has picked a Fence session. The stream resets and reconnects whenever the
// path changes; `parse` may change freely without forcing a reconnect.

export interface LogStreamItem<T> {
  id: number;
  value: T;
}

export interface LogStreamState<T> {
  items: LogStreamItem<T>[];
  connected: boolean;
  hasMore: boolean;
  loadingOlder: boolean;
  historyError: string | null;
  loadOlder: () => void;
}

type LogServerMessage =
  | { type: 'history'; line: string }
  | { type: 'append'; line: string }
  | { type: 'history_end'; has_more: boolean }
  | { type: 'history_error'; message: string };

interface WebSocketLike {
  onopen: ((ev?: unknown) => void) | null;
  onmessage: ((ev: { data: unknown }) => void) | null;
  onclose: ((ev: { code: number }) => void) | null;
  onerror: ((ev: unknown) => void) | null;
  send(data: string): void;
  close(): void;
}

export default function useLogStream<T>(
  path: string | null,
  parse: (data: string) => T
): LogStreamState<T> {
  const [items, setItems] = useState<LogStreamItem<T>[]>([]);
  const [connected, setConnected] = useState(false);
  const [hasMore, setHasMore] = useState(false);
  const [loadingOlder, setLoadingOlder] = useState(false);
  const [historyError, setHistoryError] = useState<string | null>(null);

  // Refs mirror request guards so two rapid loadOlder calls can't bypass the
  // React state gate. They are NOT a second source of durable state — React
  // state is what renders.
  const connectedRef = useRef(false);
  const loadingRef = useRef(false);
  const hasMoreRef = useRef(false);
  const errorRef = useRef<string | null>(null);

  // Keep parse out of the effect deps (callers pass a fresh closure each
  // render); read the latest via a ref so the socket only cycles on path.
  const parseRef = useRef(parse);
  parseRef.current = parse;

  // Monotonic id per connection.
  const nextIdRef = useRef(1);

  // Live WebSocket so loadOlder can send and the effect cleanup can close.
  const wsRef = useRef<WebSocketLike | null>(null);

  useEffect(() => {
    connectedRef.current = false;
    loadingRef.current = false;
    hasMoreRef.current = false;
    errorRef.current = null;
    setItems([]);
    setConnected(false);
    setHasMore(false);
    setLoadingOlder(true); // initial server-pushed page is active
    setHistoryError(null);
    nextIdRef.current = 1;

    if (!path) {
      return;
    }

    const protocol = window.location.protocol === 'https:' ? 'wss:' : 'ws:';
    const ws = transport.createWebSocket(
      `${protocol}//${window.location.host}${path}`
    ) as unknown as WebSocketLike;
    wsRef.current = ws;

    ws.onopen = () => {
      if (wsRef.current !== ws) return; // late event from a stale socket
      connectedRef.current = true;
      setConnected(true);
    };

    ws.onmessage = (event) => {
      if (wsRef.current !== ws) return;
      let msg: LogServerMessage;
      try {
        msg = JSON.parse(event.data as string) as LogServerMessage;
      } catch (e) {
        console.error('[useLogStream] failed to parse envelope:', e);
        return;
      }
      switch (msg.type) {
        case 'history': {
          try {
            const value = parseRef.current(msg.line);
            const item = { id: nextIdRef.current++, value };
            setItems((prev) => [...prev, item]);
          } catch (e) {
            console.error('[useLogStream] failed to parse history line:', e);
          }
          break;
        }
        case 'append': {
          try {
            const value = parseRef.current(msg.line);
            const item = { id: nextIdRef.current++, value };
            setItems((prev) => [item, ...prev]);
          } catch (e) {
            console.error('[useLogStream] failed to parse append line:', e);
          }
          break;
        }
        case 'history_end':
          loadingRef.current = false;
          errorRef.current = null;
          hasMoreRef.current = msg.has_more;
          setLoadingOlder(false);
          setHasMore(msg.has_more);
          setHistoryError(null);
          break;
        case 'history_error':
          loadingRef.current = false;
          errorRef.current = msg.message || 'Failed to load older logs';
          setLoadingOlder(false);
          setHistoryError(msg.message || 'Failed to load older logs');
          break;
        default:
          // Unknown message — ignore silently.
          break;
      }
    };

    ws.onclose = () => {
      if (wsRef.current !== ws) return;
      connectedRef.current = false;
      loadingRef.current = false;
      setConnected(false);
      setLoadingOlder(false);
    };

    return () => {
      wsRef.current = null;
      ws.close();
    };
  }, [path]);

  const loadOlder = () => {
    if (!wsRef.current) return;
    if (!connectedRef.current) return;
    if (loadingRef.current) return;
    if (!hasMoreRef.current && errorRef.current === null) return;
    // Flip the guard ref synchronously so two rapid calls collapse to one send.
    loadingRef.current = true;
    errorRef.current = null;
    setLoadingOlder(true);
    setHistoryError(null);
    try {
      wsRef.current.send(JSON.stringify({ type: 'load_older' }));
    } catch (e) {
      console.error('[useLogStream] failed to send load_older:', e);
      loadingRef.current = false;
      setLoadingOlder(false);
    }
  };

  return { items, connected, hasMore, loadingOlder, historyError, loadOlder };
}
