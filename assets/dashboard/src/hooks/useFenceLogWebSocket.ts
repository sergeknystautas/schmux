import { useEffect, useRef, useState } from 'react';
import { transport } from '../lib/transport';

// useFenceLogWebSocket tails one fenced session's Fence monitor.log over
// /ws/logs/fence/{sessionId}. Pass null to stay disconnected (no session
// picked). Lines arrive as raw text (backlog first, then live) and accumulate.
export default function useFenceLogWebSocket(sessionId: string | null): {
  lines: string[];
  connected: boolean;
} {
  const [lines, setLines] = useState<string[]>([]);
  const [connected, setConnected] = useState(false);
  const mountedRef = useRef(true);

  useEffect(() => {
    mountedRef.current = true;
    setLines([]);
    if (!sessionId) {
      setConnected(false);
      return;
    }
    const protocol = window.location.protocol === 'https:' ? 'wss:' : 'ws:';
    const ws = transport.createWebSocket(
      `${protocol}//${window.location.host}/ws/logs/fence/${sessionId}`
    );
    ws.onopen = () => {
      if (mountedRef.current) setConnected(true);
    };
    ws.onmessage = (event) => {
      if (mountedRef.current) setLines((prev) => [...prev, event.data as string]);
    };
    ws.onclose = () => {
      if (mountedRef.current) setConnected(false);
    };
    return () => {
      mountedRef.current = false;
      ws.close();
    };
  }, [sessionId]);

  return { lines, connected };
}
