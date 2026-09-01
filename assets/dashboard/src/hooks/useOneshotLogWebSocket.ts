import useLogStream, { type LogStreamItem } from './useLogStream';
import type { OneshotLogRecord } from '../lib/types.generated';

// useOneshotLogWebSocket tails the oneshot log over /ws/logs/oneshot: one
// OneshotLogRecord per JSON line, newest-first history with progressive
// paging plus live appends.
export default function useOneshotLogWebSocket(): {
  records: LogStreamItem<OneshotLogRecord>[];
  connected: boolean;
  hasMore: boolean;
  loadingOlder: boolean;
  historyError: string | null;
  loadOlder: () => void;
} {
  const { items, connected, hasMore, loadingOlder, historyError, loadOlder } = useLogStream(
    '/ws/logs/oneshot',
    (data) => JSON.parse(data) as OneshotLogRecord
  );
  return { records: items, connected, hasMore, loadingOlder, historyError, loadOlder };
}
