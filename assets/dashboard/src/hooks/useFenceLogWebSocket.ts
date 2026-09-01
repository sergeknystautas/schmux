import useLogStream, { type LogStreamItem } from './useLogStream';

// useFenceLogWebSocket tails one fenced session's Fence monitor.log over
// /ws/logs/fence/{sessionId}: raw text lines, newest-first history with
// progressive paging plus live appends. Pass null to stay disconnected (no
// session picked).
export default function useFenceLogWebSocket(sessionId: string | null): {
  lines: LogStreamItem<string>[];
  connected: boolean;
  hasMore: boolean;
  loadingOlder: boolean;
  historyError: string | null;
  loadOlder: () => void;
} {
  const { items, connected, hasMore, loadingOlder, historyError, loadOlder } = useLogStream(
    sessionId ? `/ws/logs/fence/${sessionId}` : null,
    (data) => data
  );
  return { lines: items, connected, hasMore, loadingOlder, historyError, loadOlder };
}
