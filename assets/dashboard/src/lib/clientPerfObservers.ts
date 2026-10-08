import type { ClientPerfCollector } from './clientPerf';

export interface ObserverDeps {
  PerformanceObserver?: typeof PerformanceObserver;
  setInterval?: typeof setInterval;
  clearInterval?: typeof clearInterval;
  fetchHealthz?: () => Promise<Response>;
}

// Replace any path segment containing a digit with :id so the agent sees
// one row per endpoint, not one per workspace or session.
export function normalizeEndpoint(path: string): string {
  return path
    .split('?')[0]
    .split('/')
    .map((seg) => (/\d/.test(seg) ? ':id' : seg))
    .join('/');
}

export function startObservers(c: ClientPerfCollector, deps: ObserverDeps = {}): () => void {
  const PO =
    'PerformanceObserver' in deps ? deps.PerformanceObserver : globalThis.PerformanceObserver;
  const setI = deps.setInterval ?? globalThis.setInterval;
  const clearI = deps.clearInterval ?? globalThis.clearInterval;
  const stops: (() => void)[] = [];

  const observe = (type: string, cb: (entries: PerformanceEntry[]) => void) => {
    if (!PO) {
      c.noteUnsupported(type);
      return;
    }
    try {
      const po = new PO((list) => cb(list.getEntries()));
      po.observe({ type, buffered: false } as PerformanceObserverInit);
      stops.push(() => po.disconnect());
    } catch {
      c.noteUnsupported(type);
    }
  };

  observe('longtask', (entries) => {
    for (const e of entries) {
      const attr =
        (e as PerformanceEntry & { attribution?: { containerType?: string }[] }).attribution?.[0]
          ?.containerType ?? '';
      c.recordLongTask({
        startTime: e.startTime,
        duration: Math.round(e.duration),
        attribution: attr,
      });
    }
  });

  observe('event', (entries) => {
    for (const e of entries) {
      const ev = e as PerformanceEventTiming;
      const target = (ev.target as Element | null)?.tagName?.toLowerCase() ?? '';
      c.recordInteraction({
        startTime: ev.startTime,
        type: ev.name,
        target,
        inputDelay: Math.round(ev.processingStart - ev.startTime),
        processing: Math.round(ev.processingEnd - ev.processingStart),
        presentation: Math.round(ev.startTime + ev.duration - ev.processingEnd),
      });
    }
  });

  observe('resource', (entries) => {
    for (const e of entries) {
      const r = e as PerformanceResourceTiming;
      if (r.initiatorType !== 'fetch' && r.initiatorType !== 'xmlhttprequest') continue;
      let url: URL;
      try {
        url = new URL(r.name);
      } catch {
        continue;
      }
      if (url.origin !== window.location.origin || !url.pathname.startsWith('/api/')) continue;
      c.recordFetch({
        startTime: r.startTime,
        endpoint: normalizeEndpoint(url.pathname),
        duration: Math.round(r.duration),
        bytes: r.transferSize,
      });
    }
  });

  // Event loop delay: post a message each second and measure how late it runs.
  if (typeof MessageChannel !== 'undefined') {
    const ch = new MessageChannel();
    let sentAt = 0;
    ch.port1.onmessage = () => c.recordLoopDelay(Math.round(performance.now() - sentAt));
    const id = setI(() => {
      sentAt = performance.now();
      ch.port2.postMessage(null);
      c.tick();
    }, 1000);
    stops.push(() => {
      clearI(id);
      ch.port1.close();
      ch.port2.close();
    });
  } else {
    c.noteUnsupported('MessageChannel');
    const id = setI(() => c.tick(), 1000);
    stops.push(() => clearI(id));
  }

  const mem = setI(() => {
    const heap =
      (performance as Performance & { memory?: { usedJSHeapSize: number } }).memory
        ?.usedJSHeapSize ?? null;
    if (heap === null) c.noteUnsupported('performance.memory');
    c.recordMemory({
      heapBytes: heap,
      domNodes: document.getElementsByTagName('*').length,
      terminals: c.terminalCount(),
      sockets: c.socketCount(),
    });
  }, 10_000);
  stops.push(() => clearI(mem));

  const onVisibility = () => {
    const hidden = document.visibilityState === 'hidden';
    c.recordHidden(hidden);
    c.recordNavigation({ kind: hidden ? 'hidden' : 'visible', route: window.location.pathname });
  };
  document.addEventListener('visibilitychange', onVisibility);
  stops.push(() => document.removeEventListener('visibilitychange', onVisibility));

  const onError = (e: ErrorEvent) => c.recordError(e.message);
  const onRejection = (e: PromiseRejectionEvent) => c.recordError(String(e.reason));
  window.addEventListener('error', onError);
  window.addEventListener('unhandledrejection', onRejection);
  stops.push(() => {
    window.removeEventListener('error', onError);
    window.removeEventListener('unhandledrejection', onRejection);
  });

  const onPageHide = () => {
    void c.persist();
  };
  window.addEventListener('pagehide', onPageHide);
  stops.push(() => window.removeEventListener('pagehide', onPageHide));
  // The periodic snapshot clones every ring; take it in idle time so the
  // recorder is not the long task it reports. pagehide stays synchronous.
  const whenIdle: (fn: () => void) => void =
    typeof requestIdleCallback === 'function'
      ? (fn) => requestIdleCallback(fn, { timeout: 2000 })
      : (fn) => setTimeout(fn, 0);
  const persistId = setI(() => {
    whenIdle(() => {
      void c.persist();
    });
  }, 5000);
  stops.push(() => clearI(persistId));

  const healthz = deps.fetchHealthz ?? (() => fetch('/api/healthz'));
  void healthz()
    .then((res) => {
      const date = res.headers.get('Date');
      if (date) c.setClockOffset(Date.parse(date) - Date.now());
    })
    .catch(() => c.noteUnsupported('healthz-date'));

  return () => stops.forEach((s) => s());
}
