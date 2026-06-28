import { useEffect, useRef, useState } from 'react';
import { transport } from '../lib/transport';
import type { SpawnLogRecord } from '../lib/types.generated';

// useLogsWebSocket opens a dedicated /ws/logs/{source} connection for the Logs
// page only: connect on mount, close on unmount. Records arrive in file order
// (backlog first, then live appends) and are accumulated chronologically.
export default function useLogsWebSocket(source: string): {
  records: SpawnLogRecord[];
  connected: boolean;
} {
  const [records, setRecords] = useState<SpawnLogRecord[]>([]);
  const [connected, setConnected] = useState(false);
  const mountedRef = useRef(true);

  useEffect(() => {
    mountedRef.current = true;
    setRecords([]);
    const protocol = window.location.protocol === 'https:' ? 'wss:' : 'ws:';
    const ws = transport.createWebSocket(`${protocol}//${window.location.host}/ws/logs/${source}`);

    ws.onopen = () => {
      if (mountedRef.current) setConnected(true);
    };
    ws.onmessage = (event) => {
      if (!mountedRef.current) return;
      try {
        const rec = JSON.parse(event.data as string) as SpawnLogRecord;
        setRecords((prev) => [...prev, rec]);
      } catch (e) {
        console.error('[ws/logs] failed to parse record:', e);
      }
    };
    ws.onclose = () => {
      if (mountedRef.current) setConnected(false);
    };

    return () => {
      mountedRef.current = false;
      ws.close();
    };
  }, [source]);

  return { records, connected };
}
