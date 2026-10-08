import { useSyncExternalStore } from 'react';
import { clientPerf } from '../lib/clientPerf';

export function useClientPerf() {
  const version = useSyncExternalStore(
    (cb) => clientPerf.subscribe(cb),
    () => clientPerf.version(),
    () => 0
  );
  void version;
  return {
    recording: clientPerf.isRecording(),
    startedAt: clientPerf.startedAt(),
    elapsedMs: clientPerf.elapsedMs(),
    stalls: clientPerf.stallCount(),
    unsent: clientPerf.hasUnsent(),
    chat: clientPerf.getChat(),
  };
}
