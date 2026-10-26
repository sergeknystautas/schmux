import { describe, it, expect, vi } from 'vitest';
import { ClientPerfCollector } from './clientPerf';
import { startObservers, normalizeEndpoint, syncClock } from './clientPerfObservers';

function fakeObserverClass(entries: Record<string, unknown[]>) {
  const instances: { type: string; cb: (list: { getEntries(): unknown[] }) => void }[] = [];
  class FakePO {
    cb: (list: { getEntries(): unknown[] }) => void;
    constructor(cb: (list: { getEntries(): unknown[] }) => void) {
      this.cb = cb;
    }
    observe(opts: { type: string }) {
      if (!(opts.type in entries)) throw new Error('unsupported');
      instances.push({ type: opts.type, cb: this.cb });
    }
    disconnect() {}
  }
  return {
    FakePO,
    fire: (type: string) =>
      instances
        .filter((i) => i.type === type)
        .forEach((i) => i.cb({ getEntries: () => entries[type] })),
  };
}

async function recordingCollector() {
  const c = new ClientPerfCollector({
    now: () => 0,
    fetchHealthz: () => Promise.resolve({ headers: { get: () => null } } as unknown as Response),
  });
  c.setConfigEnabled(true);
  c.setDevMode(true);
  await c.start();
  return c;
}

describe('clientPerf observers', () => {
  it('notes unsupported observers and keeps the others', async () => {
    const c = await recordingCollector();
    const { FakePO, fire } = fakeObserverClass({
      longtask: [{ startTime: 10, duration: 120, attribution: [{ containerType: 'window' }] }],
    });
    const stop = startObservers(c, {
      PerformanceObserver: FakePO as unknown as typeof PerformanceObserver,
    });
    fire('longtask');
    const file = c.buildFile();
    expect(file.longTasks).toEqual([
      { t: c.perfToDaemon(10), duration: 120, attribution: 'window' },
    ]);
    expect(file.environment.unsupported).toEqual(expect.arrayContaining(['event', 'resource']));
    stop();
    c.stop();
  });

  it('records same-origin /api/ resources with ids replaced', async () => {
    const c = await recordingCollector();
    const { FakePO, fire } = fakeObserverClass({
      resource: [
        {
          name: window.location.origin + '/api/sessions/abc-123/output?x=1',
          initiatorType: 'fetch',
          startTime: 5,
          duration: 40,
          transferSize: 2048,
        },
        {
          name: 'https://cdn.example.com/lib.js',
          initiatorType: 'script',
          startTime: 5,
          duration: 40,
          transferSize: 1,
        },
      ],
    });
    const stop = startObservers(c, {
      PerformanceObserver: FakePO as unknown as typeof PerformanceObserver,
    });
    fire('resource');
    expect(c.buildFile().fetches).toEqual([
      { t: c.perfToDaemon(5), endpoint: '/api/sessions/:id/output', duration: 40, bytes: 2048 },
    ]);
    stop();
    c.stop();
  });

  it('registers event timing with the minimum 16ms threshold', () => {
    const c = new ClientPerfCollector({ now: () => 0 });
    const observed: unknown[] = [];
    class RecordingPO {
      observe(opts: unknown) {
        observed.push(opts);
      }
      disconnect() {}
    }
    const stop = startObservers(c, {
      PerformanceObserver: RecordingPO as unknown as typeof PerformanceObserver,
    });
    expect(observed).toContainEqual({ type: 'event', buffered: false, durationThreshold: 16 });
    stop();
  });

  it('exports input timing below 100ms without requiring a stall', async () => {
    const c = await recordingCollector();
    const { FakePO, fire } = fakeObserverClass({
      event: [16, 48, 96].map((duration) => ({
        startTime: 10,
        duration,
        name: 'keydown',
        target: document.createElement('textarea'),
        processingStart: 14,
        processingEnd: 18,
      })),
    });
    const stop = startObservers(c, {
      PerformanceObserver: FakePO as unknown as typeof PerformanceObserver,
    });
    try {
      fire('event');
      c.tick();
      const file = c.buildFile();
      expect(file.interactions).toEqual(
        [8, 40, 88].map((presentation) => ({
          t: c.perfToDaemon(10),
          type: 'keydown',
          target: 'textarea',
          inputDelay: 4,
          processing: 4,
          presentation,
        }))
      );
      expect(file.stalls).toEqual([]);
      expect(c.hasUnsent()).toBe(true);
    } finally {
      stop();
      c.stop();
    }
  });

  it('resets stale offsets and reports invalid or unavailable healthz dates', async () => {
    const response = (date: string | null) =>
      ({ headers: { get: () => date } }) as unknown as Response;
    const cases = [
      () => Promise.resolve(response('not-a-date')),
      () => Promise.resolve(response(null)),
      () => Promise.reject(new Error('offline')),
    ];
    const c = new ClientPerfCollector({ now: () => 0 });
    for (const fetchHealthz of cases) {
      c.setClockOffset(500);
      const offset = await syncClock(c, fetchHealthz);
      expect(offset).toBe(0);
      expect(c.buildFile().environment.unsupported).toContain('healthz-date');
    }
    c.stop();
  });

  it('normalizes ids in endpoints', () => {
    expect(normalizeEndpoint('/api/workspaces/schmux-004/attachments')).toBe(
      '/api/workspaces/:id/attachments'
    );
    expect(normalizeEndpoint('/api/config')).toBe('/api/config');
  });

  it('records a window error and an unhandled rejection', async () => {
    const c = await recordingCollector(); // start() wires the window error listeners
    window.dispatchEvent(new ErrorEvent('error', { message: 'boom' }));
    expect(c.buildFile().errors).toEqual([{ t: 0, message: 'boom' }]);
    c.stop();
  });

  it('records unhandled rejections without message payload expansion', async () => {
    const c = await recordingCollector();
    const reject = (reason: unknown) =>
      window.dispatchEvent(Object.assign(new Event('unhandledrejection'), { reason }));
    reject(new Error('lost'));
    reject({ payload: 'x'.repeat(10_000) });
    expect(c.buildFile().errors).toEqual([
      { t: 0, message: 'Error: lost' },
      { t: 0, message: '[object Object]' },
    ]);
    c.stop();
  });

  it('continues with missing MessageChannel and performance.memory while noting unsupported', async () => {
    const c = await recordingCollector();
    vi.stubGlobal('MessageChannel', undefined);
    const intervals = new Map<number, () => void>();
    const stop = startObservers(c, {
      PerformanceObserver: undefined,
      setInterval: ((fn: () => void, ms: number) => {
        intervals.set(ms, fn);
        return ms;
      }) as unknown as typeof setInterval,
      clearInterval: (() => {}) as typeof clearInterval,
    });
    intervals.get(1000)!();
    intervals.get(10_000)!();
    const file = c.buildFile();
    expect(file.timeline).toHaveLength(1);
    expect(file.memory).toEqual([
      expect.objectContaining({ heapBytes: null, terminals: 0, sockets: 0 }),
    ]);
    expect(file.environment.unsupported).toEqual(
      expect.arrayContaining(['MessageChannel', 'performance.memory'])
    );
    stop();
    vi.unstubAllGlobals();
    c.stop();
  });
});
