import { useEffect, useInsertionEffect, useSyncExternalStore } from 'react';
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

/** Records a route navigation and times its first commit and paint. */
export function useClientPerfRoute(route: string) {
  // Insertion effects run before Profiler callbacks in the same commit, so the
  // navigation exists when the route's first commit is reported, including the
  // initial mount. A plain effect would only see the next commit.
  useInsertionEffect(() => {
    clientPerf.recordRoute(route);
    clientPerf.recordNavigation({ kind: 'route', route });
  }, [route]);

  useEffect(() => {
    let second: number | undefined;
    const first = requestAnimationFrame(() => {
      second = requestAnimationFrame(() => {
        clientPerf.recordPaint();
      });
    });
    return () => {
      cancelAnimationFrame(first);
      if (second !== undefined) cancelAnimationFrame(second);
    };
  }, [route]);
}
