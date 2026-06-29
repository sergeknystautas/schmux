import useLogStream from './useLogStream';
import type { OneshotLogRecord } from '../lib/types.generated';

// useOneshotLogWebSocket tails the oneshot log over /ws/logs/oneshot: one
// OneshotLogRecord per JSON line, backlog first then live appends.
export default function useOneshotLogWebSocket(): {
  records: OneshotLogRecord[];
  connected: boolean;
} {
  const { items, connected } = useLogStream(
    '/ws/logs/oneshot',
    (data) => JSON.parse(data) as OneshotLogRecord
  );
  return { records: items, connected };
}
