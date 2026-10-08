import { describe, it, expect, beforeEach, vi } from 'vitest';
import { ClientPerfCollector, STALL_MS, TIMELINE_CAP } from './clientPerf';
import { clearSnapshot } from './clientPerfStore';

function collector(opts: { tabId?: string } = {}) {
  let now = 1_000_000;
  const c = new ClientPerfCollector({ now: () => now, tabId: opts.tabId ?? 'tab-a' });
  c.setConfigEnabled(true);
  c.setDevMode(true);
  created.push(c);
  return {
    c,
    advance: (ms: number) => {
      now += ms;
    },
  };
}

const created: ClientPerfCollector[] = [];

describe('ClientPerfCollector', () => {
  beforeEach(async () => {
    vi.useFakeTimers({ toFake: ['setInterval', 'clearInterval'] });
    localStorage.clear();
    await clearSnapshot('tab-a');
    await clearSnapshot('tab-b');
  });

  afterEach(() => {
    for (const c of created) c.stop();
    created.length = 0;
    vi.useRealTimers();
  });

  it('does not record until both switches are on', () => {
    const { c } = collector();
    c.setConfigEnabled(false);
    c.start();
    expect(c.isRecording()).toBe(false);
    c.setConfigEnabled(true);
    expect(c.isRecording()).toBe(false); // start() was a no-op while config was off
    c.start();
    expect(c.isRecording()).toBe(true);
    expect(localStorage.getItem('schmux:client-perf')).toBe('1');
  });

  it('closes one timeline row per tick and marks stalls by the 100ms rule', () => {
    const { c, advance } = collector();
    c.start();
    c.recordLoopDelay(STALL_MS);
    advance(1000);
    c.tick();
    c.recordLoopDelay(5);
    c.recordLongTask({ startTime: 0, duration: STALL_MS, attribution: 'script' });
    advance(1000);
    c.tick();
    c.recordLoopDelay(5);
    advance(1000);
    c.tick();
    const file = c.buildFile();
    expect(file.timeline).toHaveLength(3);
    expect(file.stalls).toEqual([0, 1]);
    expect(c.stallCount()).toBe(2);
  });

  it('drops the oldest timeline rows past the cap but keeps counting stalls', () => {
    const { c, advance } = collector();
    c.start();
    for (let i = 0; i < TIMELINE_CAP + 10; i++) {
      c.recordLoopDelay(STALL_MS);
      advance(1000);
      c.tick();
    }
    const file = c.buildFile();
    expect(file.timeline).toHaveLength(TIMELINE_CAP);
    expect(c.stallCount()).toBe(TIMELINE_CAP + 10);
  });

  it('persists and restores across a reload under the same tab id', async () => {
    const { c, advance } = collector();
    c.start();
    c.recordLoopDelay(STALL_MS);
    advance(1000);
    c.tick();
    c.setChat({ workspaceId: 'ws-1', sessionId: 'sess-1' });
    await c.persist();

    const { c: again } = collector();
    expect(await again.restore()).toBe(true);
    expect(again.isRecording()).toBe(true);
    expect(again.stallCount()).toBe(1);
    expect(again.getChat()).toEqual({ workspaceId: 'ws-1', sessionId: 'sess-1' });
    const nav = again.buildFile().navigation;
    expect(nav[nav.length - 1]?.kind).toBe('reload');
  });

  it('keeps two tabs apart', async () => {
    const a = collector({ tabId: 'tab-a' });
    const b = collector({ tabId: 'tab-b' });
    a.c.start();
    b.c.start();
    a.c.recordLoopDelay(STALL_MS);
    a.advance(1000);
    a.c.tick();
    await a.c.persist();
    await b.c.persist();
    const { c: aAgain } = collector({ tabId: 'tab-a' });
    const { c: bAgain } = collector({ tabId: 'tab-b' });
    await aAgain.restore();
    await bAgain.restore();
    expect(aAgain.stallCount()).toBe(1);
    expect(bAgain.stallCount()).toBe(0);
  });

  it('empties buffers on markSent and keeps the chat ids', () => {
    const { c, advance } = collector();
    c.start();
    c.setChat({ workspaceId: 'ws-1', sessionId: 'sess-1' });
    c.recordLoopDelay(STALL_MS);
    advance(1000);
    c.tick();
    expect(c.hasUnsent()).toBe(true);
    c.markSent();
    expect(c.hasUnsent()).toBe(false);
    expect(c.isRecording()).toBe(true);
    expect(c.getChat()).toEqual({ workspaceId: 'ws-1', sessionId: 'sess-1' });
  });

  it('stops, clears the switch, and empties everything when config goes off', async () => {
    const { c, advance } = collector();
    c.start();
    c.setChat({ workspaceId: 'ws-1', sessionId: 'sess-1' });
    c.recordLoopDelay(5);
    advance(1000);
    c.tick();
    c.setConfigEnabled(false);
    expect(c.isRecording()).toBe(false);
    expect(localStorage.getItem('schmux:client-perf')).toBeNull();
    expect(c.getChat()).toBeNull();
    const { c: again } = collector();
    expect(await again.restore()).toBe(false);
  });

  it('notifies subscribers on start, tick, and stop', () => {
    const { c, advance } = collector();
    const listener = vi.fn();
    c.subscribe(listener);
    c.start();
    advance(1000);
    c.tick();
    c.stop();
    expect(listener).toHaveBeenCalledTimes(3);
  });
});

describe('ClientPerfCollector commits', () => {
  beforeEach(() => {
    vi.useFakeTimers({ toFake: ['setInterval', 'clearInterval'] });
    localStorage.clear();
  });
  afterEach(() => {
    for (const c of created) c.stop();
    created.length = 0;
    vi.useRealTimers();
  });

  it('keeps commits over 16ms and sums all commit time into the row', () => {
    const { c, advance } = collector();
    c.start();
    c.recordRoute('/sessions/x');
    c.recordCommit('sidebar', 'update', 5);
    c.recordCommit('main', 'update', 40);
    advance(1000);
    c.tick();
    const file = c.buildFile();
    expect(file.commits).toEqual([
      { t: 1_000_000, id: 'main', phase: 'update', duration: 40, route: '/sessions/x' },
    ]);
    expect(file.timeline[0].commit).toBe(45);
  });
});

describe('ClientPerfCollector restore idempotence', () => {
  beforeEach(() => {
    vi.useFakeTimers({ toFake: ['setInterval', 'clearInterval'] });
    localStorage.clear();
  });
  afterEach(() => {
    for (const c of created) c.stop();
    created.length = 0;
    vi.useRealTimers();
  });

  it('a second restore while recording does not duplicate buffers', async () => {
    const { c, advance } = collector();
    c.start();
    c.recordLoopDelay(STALL_MS);
    advance(1000);
    c.tick();
    await c.persist();

    const { c: again } = collector();
    expect(await again.restore()).toBe(true);
    expect(again.buildFile().timeline).toHaveLength(1);
    // A config reload fires restore again while the tab is already recording.
    expect(await again.restore()).toBe(true);
    expect(again.buildFile().timeline).toHaveLength(1);
    expect(again.buildFile().navigation.filter((n) => n.kind === 'reload')).toHaveLength(1);
  });
});

describe('ClientPerfCollector switches, clocks, and tabs', () => {
  beforeEach(async () => {
    vi.useFakeTimers({ toFake: ['setInterval', 'clearInterval'] });
    localStorage.clear();
    sessionStorage.clear();
    await clearSnapshot('tab-a');
  });
  afterEach(() => {
    for (const c of created) c.stop();
    created.length = 0;
    vi.useRealTimers();
  });

  it('does not record unless the daemon reports dev mode', () => {
    const { c } = collector();
    c.setDevMode(false);
    c.start();
    expect(c.isRecording()).toBe(false);
    c.setDevMode(true);
    c.start();
    expect(c.isRecording()).toBe(true);
    c.setDevMode(false);
    expect(c.isRecording()).toBe(false);
    expect(localStorage.getItem('schmux:client-perf')).toBeNull();
  });

  it('stamps performance entries, navigation, and timeline rows in daemon time', () => {
    const { c, advance } = collector();
    c.setClockOffset(500);
    c.start();
    c.recordLongTask({ startTime: 10, duration: 120, attribution: 'window' });
    c.recordFetch({ startTime: 20, endpoint: '/api/config', duration: 5, bytes: 1 });
    c.recordNavigation({ kind: 'route', route: '/x' });
    advance(1000);
    c.tick();
    const file = c.buildFile();
    const origin = Math.round(performance.timeOrigin ?? 0);
    expect(file.longTasks[0].t).toBe(origin + 10 + 500);
    expect(file.fetches[0].t).toBe(origin + 20 + 500);
    expect(file.navigation[0].t).toBe(1_000_000 + 500);
    expect(file.timeline[0].t).toBe(1_001_000 + 500);
  });

  it('measures first commit and paint after a route change on one clock', () => {
    const { c, advance } = collector();
    c.setClockOffset(500);
    c.start();
    c.recordNavigation({ kind: 'route', route: '/x' });
    advance(30);
    c.recordCommit('main', 'mount', 3);
    advance(20);
    c.recordPaint();
    const nav = c.buildFile().navigation[0];
    expect(nav.firstCommitMs).toBe(30);
    expect(nav.paintMs).toBe(50);
  });

  it('reports elapsed time from its own clock', () => {
    const { c, advance } = collector();
    c.start();
    advance(90_000);
    expect(c.elapsedMs()).toBe(90_000);
  });

  it('applies a snapshot once when two restores race', async () => {
    const { c, advance } = collector();
    c.start();
    c.recordLoopDelay(STALL_MS);
    advance(1000);
    c.tick();
    await c.persist();
    const { c: again } = collector();
    const [a, b] = await Promise.all([again.restore(), again.restore()]);
    expect(a && b).toBe(true);
    expect(again.buildFile().timeline).toHaveLength(1);
    expect(again.buildFile().navigation.filter((n) => n.kind === 'reload')).toHaveLength(1);
  });

  it('moves a restored recording to a fresh tab id so a duplicated tab cannot clobber it', async () => {
    const enable = (c: ClientPerfCollector) => {
      created.push(c);
      c.setConfigEnabled(true);
      c.setDevMode(true);
      return c;
    };
    const first = enable(new ClientPerfCollector({ now: () => 5 }));
    first.start();
    first.recordLoopDelay(STALL_MS);
    first.tick();
    await first.persist();
    const oldId = sessionStorage.getItem('schmux:client-perf-tab-id');
    expect(oldId).not.toBeNull();

    // A duplicated tab inherits sessionStorage, so it restores from the same key.
    const dup = enable(new ClientPerfCollector({ now: () => 5 }));
    expect(await dup.restore()).toBe(true);
    expect(dup.stallCount()).toBe(1);
    expect(sessionStorage.getItem('schmux:client-perf-tab-id')).not.toBe(oldId);

    // The original tab keeps writing under its own key and restores from it.
    await first.persist();
    const original = enable(new ClientPerfCollector({ now: () => 5, tabId: oldId! }));
    expect(await original.restore()).toBe(true);
    expect(original.stallCount()).toBe(1);
  });
});
