import { useEffect, useRef, useState } from 'react';
import { transport } from '../lib/transport';

// useLogStream is the shared engine for every Logs-tab tailer. It opens a
// dedicated websocket to `path` (relative to the dashboard origin), maps each
// message through `parse`, and accumulates the results in arrival order
// (backlog first, then live appends), while tracking connection state. Pass a
// null path to stay disconnected — e.g. before the user has picked a target.
// The stream resets and reconnects whenever `path` changes; `parse` may change
// freely without forcing a reconnect.
export default function useLogStream<T>(
  path: string | null,
  parse: (data: string) => T
): { items: T[]; connected: boolean } {
  const [items, setItems] = useState<T[]>([]);
  const [connected, setConnected] = useState(false);
  const mountedRef = useRef(true);
  // Keep parse out of the effect deps (callers pass a fresh closure each
  // render); read the latest via a ref so the socket only cycles on path.
  const parseRef = useRef(parse);
  parseRef.current = parse;

  useEffect(() => {
    mountedRef.current = true;
    setItems([]);
    if (!path) {
      setConnected(false);
      return;
    }
    const protocol = window.location.protocol === 'https:' ? 'wss:' : 'ws:';
    const ws = transport.createWebSocket(`${protocol}//${window.location.host}${path}`);

    ws.onopen = () => {
      if (mountedRef.current) setConnected(true);
    };
    ws.onmessage = (event) => {
      if (!mountedRef.current) return;
      try {
        const item = parseRef.current(event.data as string);
        setItems((prev) => [...prev, item]);
      } catch (e) {
        console.error('[useLogStream] failed to parse message:', e);
      }
    };
    ws.onclose = () => {
      if (mountedRef.current) setConnected(false);
    };

    return () => {
      mountedRef.current = false;
      ws.close();
    };
  }, [path]);

  return { items, connected };
}
