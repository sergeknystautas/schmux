import useLogStream from './useLogStream';
import type { SpawnLogRecord } from '../lib/types.generated';

// useLogsWebSocket tails the spawn log over /ws/logs/{source}: one
// SpawnLogRecord per JSON line, backlog first then live appends.
export default function useLogsWebSocket(source: string): {
  records: SpawnLogRecord[];
  connected: boolean;
} {
  const { items, connected } = useLogStream(
    `/ws/logs/${source}`,
    (data) => JSON.parse(data) as SpawnLogRecord
  );
  return { records: items, connected };
}
