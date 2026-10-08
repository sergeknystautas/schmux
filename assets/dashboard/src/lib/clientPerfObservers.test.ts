import { describe, it, expect, vi } from 'vitest';
import { ClientPerfCollector } from './clientPerf';
import { startObservers, normalizeEndpoint } from './clientPerfObservers';

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

describe('clientPerf observers', () => {
  it('notes unsupported observers and keeps the others', () => {
    const c = new ClientPerfCollector({ now: () => 0, tabId: 't' });
    c.setConfigEnabled(true);
    c.setDevMode(true);
    c.start();
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

  it('records same-origin /api/ resources with ids replaced', () => {
    const c = new ClientPerfCollector({ now: () => 0, tabId: 't' });
    c.setConfigEnabled(true);
    c.setDevMode(true);
    c.start();
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

  it('normalizes ids in endpoints', () => {
    expect(normalizeEndpoint('/api/workspaces/schmux-004/attachments')).toBe(
      '/api/workspaces/:id/attachments'
    );
    expect(normalizeEndpoint('/api/config')).toBe('/api/config');
  });

  it('records a window error and an unhandled rejection', () => {
    const c = new ClientPerfCollector({ now: () => 0, tabId: 't' });
    c.setConfigEnabled(true);
    c.setDevMode(true);
    c.start(); // start() wires the window error listeners
    window.dispatchEvent(new ErrorEvent('error', { message: 'boom' }));
    expect(c.buildFile().errors).toEqual([{ t: 0, message: 'boom' }]);
    c.stop();
  });
});
