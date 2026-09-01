import useLogStream, { type LogStreamItem } from './useLogStream';
import type { SpawnLogRecord } from '../lib/types.generated';

// useLogsWebSocket tails the spawn log over /ws/logs/{source}: one
// SpawnLogRecord per JSON line, newest-first history with progressive paging
// plus live appends.
export default function useLogsWebSocket(source: string): {
  records: LogStreamItem<SpawnLogRecord>[];
  connected: boolean;
  hasMore: boolean;
  loadingOlder: boolean;
  historyError: string | null;
  loadOlder: () => void;
} {
  const { items, connected, hasMore, loadingOlder, historyError, loadOlder } = useLogStream(
    `/ws/logs/${source}`,
    (data) => JSON.parse(data) as SpawnLogRecord
  );
  return { records: items, connected, hasMore, loadingOlder, historyError, loadOlder };
}
