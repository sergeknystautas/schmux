import useLogStream from './useLogStream';

// useFenceLogWebSocket tails one fenced session's Fence monitor.log over
// /ws/logs/fence/{sessionId}: raw text lines, backlog first then live. Pass
// null to stay disconnected (no session picked).
export default function useFenceLogWebSocket(sessionId: string | null): {
  lines: string[];
  connected: boolean;
} {
  const { items, connected } = useLogStream(
    sessionId ? `/ws/logs/fence/${sessionId}` : null,
    (data) => data
  );
  return { lines: items, connected };
}
