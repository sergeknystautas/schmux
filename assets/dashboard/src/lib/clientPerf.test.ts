import { describe, it, expect, beforeEach, vi } from 'vitest';
import { ClientPerfCollector, STALL_MS, TIMELINE_CAP } from './clientPerf';
import type { ClientPerfFile } from './clientPerf';
import { inputLatency } from './inputLatency';

// A healthz response the test resolves by hand; `called` settles once the
// collector has asked for it.
function deferredHealthz() {
  let markCalled!: () => void;
  const called = new Promise<void>((resolve) => {
    markCalled = resolve;
  });
  let respond!: (date: string | null) => void;
  const fetchHealthz = () => {
    markCalled();
    return new Promise<Response>((resolve) => {
      respond = (date) =>
        resolve({ headers: { get: (k: string) => (k === 'Date' ? date : null) } } as Response);
    });
  };
  return { fetchHealthz, called, respond: (date: string | null) => respond(date) };
}

const noDateHealthz = () =>
  Promise.resolve({ headers: { get: () => null } } as unknown as Response);

const healthzAt = (daemonNow: number) => () =>
  Promise.resolve({
    headers: { get: () => new Date(daemonNow).toUTCString() },
  } as unknown as Response);

function collector(opts: { fetchHealthz?: () => Promise<Response> } = {}) {
  let now = 1_000_000;
  const c = new ClientPerfCollector({
    now: () => now,
    fetchHealthz: opts.fetchHealthz ?? noDateHealthz,
  });
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
  beforeEach(() => {
    vi.useFakeTimers({ toFake: ['setInterval', 'clearInterval'] });
    localStorage.clear();
  });

  afterEach(() => {
    for (const c of created) c.stop();
    created.length = 0;
    vi.useRealTimers();
  });

  it('does not record until both switches are on', async () => {
    const { c } = collector();
    c.setConfigEnabled(false);
    await c.start();
    expect(c.isRecording()).toBe(false);
    c.setConfigEnabled(true);
    expect(c.isRecording()).toBe(false); // start() was a no-op while config was off
    await c.start();
    expect(c.isRecording()).toBe(true);
  });

  it('closes one timeline row per tick and marks stalls by the 100ms rule', async () => {
    const { c, advance } = collector();
    await c.start();
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

  it('exports the broader measurements when no second qualifies as a stall', async () => {
    const { c, advance } = collector();
    let frames = 0;
    c.registerTerminal('term-1', () => ({ frames, bytes: frames * 10 }));
    await c.start();
    c.recordLoopDelay(5);
    c.recordLongTask({ startTime: 0, duration: 60, attribution: 'window' });
    c.recordCommit('sidebar', 'update', 20);
    c.recordWsMessage('/ws/dashboard', 'sessions', 100, 3);
    c.recordFetch({ startTime: 0, endpoint: '/api/config', duration: 4, bytes: 200 });
    const chatLoad = {
      sessionId: 'chat-1',
      at: '2026-10-24T12:00:00.000Z',
      start: 'view' as const,
      frameChars: 100,
      records: 1,
      routeToSocketMs: 1,
      socketOpenMs: 2,
      historyWaitMs: 3,
      parseMs: 4,
      reduceMs: 5,
      commitMs: 6,
      afterPaintMs: 7,
      totalMs: 28,
    };
    c.recordChatLoad(chatLoad);
    const memory = { heapBytes: 1000, domNodes: 10, terminals: 1, sockets: 1 };
    c.recordMemory(memory);
    c.recordNavigation({ kind: 'route', route: '/sessions/chat-1' });
    c.recordError('test error');
    frames = 2;
    advance(1000);
    c.tick();

    const file = c.buildFile();
    expect(file.stalls).toEqual([]);
    expect(c.stallCount()).toBe(0);
    expect(file.timeline).toEqual([
      expect.objectContaining({ loop: 5, longTask: 60, commit: 20, wsHandler: 3, fetches: 1 }),
    ]);
    expect(file.longTasks).toEqual([expect.objectContaining({ duration: 60 })]);
    expect(file.commits).toEqual([expect.objectContaining({ id: 'sidebar', duration: 20 })]);
    expect(file.websocket.perSecond).toEqual([
      expect.objectContaining({ type: 'sessions', count: 1, bytes: 100, handlerMs: 3 }),
    ]);
    expect(file.fetches).toEqual([expect.objectContaining({ duration: 4, bytes: 200 })]);
    expect(file.terminals).toEqual([
      expect.objectContaining({ id: 'term-1', frames: 2, bytes: 20 }),
    ]);
    expect(file.chatLoads).toEqual([chatLoad]);
    expect(file.memory).toEqual([{ t: 1_000_000, ...memory }]);
    expect(file.navigation).toEqual([
      expect.objectContaining({ kind: 'route', route: '/sessions/chat-1' }),
    ]);
    expect(file.errors).toEqual([{ t: 1_000_000, message: 'test error' }]);
    expect(c.hasUnsent()).toBe(true);
  });

  it('drops the oldest timeline rows past the cap but keeps counting stalls', async () => {
    const { c, advance } = collector();
    await c.start();
    for (let i = 0; i < TIMELINE_CAP + 10; i++) {
      c.recordLoopDelay(STALL_MS);
      advance(1000);
      c.tick();
    }
    const file = c.buildFile();
    expect(file.timeline).toHaveLength(TIMELINE_CAP);
    expect(c.stallCount()).toBe(TIMELINE_CAP + 10);
  });

  it('starts terminal deltas at recording start', async () => {
    const { c, advance } = collector();
    let frames = 100;
    let bytes = 1000;
    c.registerTerminal('sess-1', () => ({ frames, bytes }));
    await c.start();
    frames = 103;
    bytes = 1030;
    advance(1000);
    c.tick();
    expect(c.buildFile().terminals[0]).toMatchObject({ frames: 3, bytes: 30 });

    frames = 108;
    bytes = 1080;
    advance(1000);
    c.tick();
    expect(c.buildFile().terminals[1]).toMatchObject({ frames: 5, bytes: 50 });
  });

  it('exports the unfinished second without changing the live recording', async () => {
    const { c } = collector();
    await c.start();
    c.recordLoopDelay(150);
    c.recordWsMessage('/ws/dashboard', 'sessions', 1234, 3);
    const file = c.buildFile();
    expect(file.timeline).toEqual([
      expect.objectContaining({ loop: 150, wsBytes: 1234, wsHandler: 3 }),
    ]);
    expect(file.stalls).toEqual([0]);
    expect(file.websocket.perSecond).toEqual([
      expect.objectContaining({ path: '/ws/dashboard', type: 'sessions', count: 1, bytes: 1234 }),
    ]);
    expect(c.buildFile().timeline).toEqual(file.timeline);
    c.tick();
    expect(c.buildFile().timeline).toEqual(file.timeline);
    expect(c.stallCount()).toBe(1);
  });

  it('notifies subscribers on start, tick, and stop', async () => {
    const { c, advance } = collector();
    const listener = vi.fn();
    c.subscribe(listener);
    await c.start();
    advance(1000);
    c.tick();
    c.stop();
    expect(listener).toHaveBeenCalledTimes(3);
  });

  it('starts a new page with no recording or remembered chat', async () => {
    const { c } = collector();
    await c.start();
    c.setChat({ workspaceId: 'ws-1', sessionId: 'chat-1' });
    c.recordError('current page only');
    c.recordLoopDelay(150);
    c.tick();

    const { c: newPage } = collector();
    expect(newPage.isRecording()).toBe(false);
    expect(newPage.startedAt()).toBeNull();
    expect(newPage.getChat()).toBeNull();
    expect(newPage.buildFile().timeline).toEqual([]);
    expect(newPage.buildFile().errors).toEqual([]);
    expect(c.buildFile().errors).toHaveLength(1);
  });

  it('finishes capture without losing the recording and freezes its duration', async () => {
    const { c, advance } = collector();
    await c.start();
    c.recordError('keep');
    advance(1000);
    c.finish();
    const file = c.buildFile();
    const elapsed = c.elapsedMs();
    expect(c.isRecording()).toBe(false);
    expect(c.hasUnsent()).toBe(true);
    advance(5000);
    c.recordError('off');
    c.setDevMode(false);
    c.setDevMode(true);
    await c.start();
    expect(c.isRecording()).toBe(false);
    expect(c.elapsedMs()).toBe(elapsed);
    expect(c.buildFile().errors).toEqual(file.errors);
    c.stop();
    await c.start();
    expect(c.isRecording()).toBe(true);
    expect(c.buildFile().errors).toEqual([]);
  });

  it('config-off clears the current recording and chat without restarting on config-on', async () => {
    const { c } = collector();
    await c.start();
    c.setChat({ workspaceId: 'ws-1', sessionId: 'chat-1' });
    c.recordError('discard me');
    c.setConfigEnabled(false);
    c.setConfigEnabled(true);
    expect(c.isRecording()).toBe(false);
    expect(c.startedAt()).toBeNull();
    expect(c.getChat()).toBeNull();
    expect(c.buildFile().errors).toEqual([]);
  });
});

describe('ClientPerfCollector lifecycle', () => {
  const ids = { workspaceId: 'ws-1', sessionId: 'sess-1' };
  beforeEach(() => {
    vi.useFakeTimers({ toFake: ['setInterval', 'clearInterval'] });
    localStorage.clear();
  });
  afterEach(() => {
    vi.unstubAllGlobals();
    for (const c of created) c.stop();
    created.length = 0;
    vi.useRealTimers();
  });

  it('suspends and resumes only the current page recording when dev mode changes', async () => {
    const { c, advance } = collector();
    let frames = 0;
    c.registerTerminal('terminal', () => ({ frames, bytes: frames * 10 }));
    await c.start();
    c.setChat(ids);
    c.recordError('keep in memory');
    c.setDevMode(false);
    expect(c.isRecording()).toBe(false);
    frames = 10;
    c.setDevMode(true);
    await c.start();
    frames = 13;
    advance(1000);
    c.tick();
    expect(c.isRecording()).toBe(true);
    expect(c.getChat()).toEqual(ids);
    expect(c.buildFile().errors[0].message).toBe('keep in memory');
    expect(c.buildFile().terminals[0]).toMatchObject({ frames: 3, bytes: 30 });
  });

  it('Stop keeps chat IDs in memory but clears the recording', async () => {
    const { c } = collector();
    await c.start();
    c.setChat(ids);
    c.recordError('discard me');
    c.stop();
    expect(c.isRecording()).toBe(false);
    expect(c.getChat()).toEqual(ids);
    expect(c.buildFile().errors).toEqual([]);
  });

  it('Start preserves startedAt and buffers when resuming dev-mode suspension', async () => {
    const { c, advance } = collector();
    await c.start();
    const startedAt = c.startedAt();
    c.recordLoopDelay(5);
    advance(1000);
    c.tick();
    const timeline = c.buildFile().timeline;
    c.setDevMode(false);

    advance(5000);
    c.setDevMode(true);
    await c.start();
    expect(c.isRecording()).toBe(true);
    expect(c.startedAt()).toBe(startedAt);
    expect(c.buildFile().timeline).toEqual(timeline);
  });

  it('excludes terminal traffic received between Stop and Start', async () => {
    const { c, advance } = collector();
    let frames = 0;
    let bytes = 0;
    c.registerTerminal('sess-1', () => ({ frames, bytes }));
    await c.start();
    advance(1000);
    c.tick();
    c.stop();

    frames = 10;
    bytes = 100;
    await c.start();
    frames = 14;
    bytes = 140;
    advance(1000);
    c.tick();
    expect(c.buildFile().terminals[0]).toMatchObject({ frames: 4, bytes: 40 });
  });

  it('marks detail data unsent before the first timeline tick', async () => {
    const { c } = collector();
    await c.start();
    expect(c.hasUnsent()).toBe(false);
    c.recordLongTask({ startTime: 0, duration: 120, attribution: 'window' });
    expect(c.hasUnsent()).toBe(true);
    c.stop();
    expect(c.hasUnsent()).toBe(false);
  });
});

describe('ClientPerfCollector file format', () => {
  beforeEach(() => {
    vi.useFakeTimers({ toFake: ['setInterval', 'clearInterval'] });
    localStorage.clear();
  });
  afterEach(() => {
    for (const c of created) c.stop();
    created.length = 0;
    vi.useRealTimers();
  });

  const chatLoad = (id: string) => ({
    sessionId: id,
    at: '2026-10-24T12:00:00.000Z',
    start: 'view' as const,
    frameChars: 0,
    records: 0,
    routeToSocketMs: 0,
    socketOpenMs: 0,
    historyWaitMs: 0,
    parseMs: 0,
    reduceMs: 0,
    commitMs: 0,
    afterPaintMs: 0,
    totalMs: 0,
  });

  // Each push records entry i; ids reads the identities back in order.
  const rings: {
    name: string;
    cap: number;
    push: (c: ClientPerfCollector, i: number) => void;
    ids: (f: ClientPerfFile) => string[];
  }[] = [
    {
      name: 'timeline',
      cap: 3600,
      push: (c, i) => {
        c.recordLoopDelay(i);
        c.tick();
      },
      ids: (f) => f.timeline.map((r) => String(r.loop)),
    },
    {
      name: 'longTasks',
      cap: 2000,
      push: (c, i) => c.recordLongTask({ startTime: 0, duration: 1, attribution: String(i) }),
      ids: (f) => f.longTasks.map((r) => r.attribution),
    },
    {
      name: 'interactions',
      cap: 2000,
      push: (c, i) =>
        c.recordInteraction({
          startTime: 0,
          type: String(i),
          target: 'button',
          inputDelay: 0,
          processing: 100,
          presentation: 0,
        }),
      ids: (f) => f.interactions.map((r) => r.type),
    },
    {
      name: 'commits',
      cap: 2000,
      push: (c, i) => c.recordCommit(String(i), 'update', 20),
      ids: (f) => f.commits.map((r) => r.id),
    },
    {
      name: 'websocket.individual',
      cap: 2000,
      push: (c, i) => c.recordWsMessage('/ws/dashboard', String(i), 60 * 1024, 0),
      ids: (f) => f.websocket.individual.map((r) => r.type),
    },
    {
      name: 'fetches',
      cap: 2000,
      push: (c, i) => c.recordFetch({ startTime: 0, endpoint: String(i), duration: 1, bytes: 1 }),
      ids: (f) => f.fetches.map((r) => r.endpoint),
    },
    {
      name: 'websocket.perSecond',
      cap: 3600,
      push: (c, i) => {
        c.recordWsMessage('/ws/dashboard', String(i), 1, 0);
        c.tick();
      },
      ids: (f) => f.websocket.perSecond.map((r) => r.type),
    },
    {
      name: 'terminals',
      cap: 3600,
      push: (c, i) =>
        c.recordTerminalSecond({
          t: 0,
          id: String(i),
          frames: 0,
          bytes: 0,
          handleOutputP50: 0,
          handleOutputP99: 0,
        }),
      ids: (f) => f.terminals.map((r) => r.id),
    },
    {
      name: 'chatLoads',
      cap: 200,
      push: (c, i) => c.recordChatLoad(chatLoad(String(i))),
      ids: (f) => f.chatLoads.map((r) => r.sessionId),
    },
    {
      name: 'memory',
      cap: 360,
      push: (c, i) => c.recordMemory({ heapBytes: i, domNodes: 0, terminals: 0, sockets: 0 }),
      ids: (f) => f.memory.map((r) => String(r.heapBytes)),
    },
    {
      name: 'navigation',
      cap: 500,
      push: (c, i) => c.recordNavigation({ kind: 'visible', route: String(i) }),
      ids: (f) => f.navigation.map((r) => r.route),
    },
    {
      name: 'errors',
      cap: 100,
      push: (c, i) => c.recordError(String(i)),
      ids: (f) => f.errors.map((r) => r.message),
    },
  ];

  it.each(rings)('drops the oldest entry at the documented $name cap', async (ring) => {
    const { c } = collector();
    await c.start();
    for (let i = 0; i <= ring.cap; i++) ring.push(c, i);
    const got = ring.ids(c.buildFile());
    expect(got).toHaveLength(ring.cap);
    expect(got[0]).toBe('1');
    expect(got[ring.cap - 1]).toBe(String(ring.cap));
  });

  it('emits the complete file envelope build environment and workload', async () => {
    const { c } = collector();
    await c.start();
    c.setBuild({ version: '1.2.3', devMode: true, sourceWorkspace: '/ws/x', viteDev: false });
    c.setWorkload({
      workspaces: 1,
      sessions: 1,
      running: 1,
      chats: 0,
      terminals: 1,
      panels: {},
      flags: {},
    });
    const file = c.buildFile();
    expect(Object.keys(file).sort()).toEqual(
      [
        'version',
        'browserId',
        'startedAt',
        'builtAt',
        'build',
        'environment',
        'workload',
        'timeline',
        'stalls',
        'longTasks',
        'interactions',
        'commits',
        'websocket',
        'fetches',
        'terminals',
        'chatLoads',
        'memory',
        'navigation',
        'errors',
      ].sort()
    );
    expect(file.version).toBe(1);
    expect(file.browserId).toBe(localStorage.getItem('schmux:client-perf-browser-id'));
    expect(file.startedAt).toBe(1_000_000);
    expect(file.builtAt).toBe(1_000_000);
    expect(file.build).toEqual({
      version: '1.2.3',
      devMode: true,
      sourceWorkspace: '/ws/x',
      viteDev: false,
    });
    expect(Object.keys(file.environment).sort()).toEqual(
      [
        'userAgent',
        'cpus',
        'deviceMemoryGb',
        'viewport',
        'pixelRatio',
        'host',
        'remoteClient',
        'unsupported',
        'clockOffsetMs',
      ].sort()
    );
    expect(Object.keys(file.workload).sort()).toEqual(
      [
        'workspaces',
        'sessions',
        'running',
        'chats',
        'terminals',
        'mountedTerminals',
        'socketsByPath',
        'lastDashboardMessageBytes',
        'panels',
        'flags',
      ].sort()
    );
    expect(Object.keys(file.websocket).sort()).toEqual(['individual', 'perSecond']);
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

  it('keeps commits over 16ms and sums all commit time into the row', async () => {
    const { c, advance } = collector();
    await c.start();
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

describe('ClientPerfCollector switches and clocks', () => {
  beforeEach(() => {
    vi.useFakeTimers({ toFake: ['setInterval', 'clearInterval'] });
    localStorage.clear();
    sessionStorage.clear();
  });
  afterEach(() => {
    for (const c of created) c.stop();
    created.length = 0;
    vi.useRealTimers();
  });

  it('does not record unless the daemon reports dev mode', async () => {
    const { c } = collector();
    c.setDevMode(false);
    await c.start();
    expect(c.isRecording()).toBe(false);
    c.setDevMode(true);
    await c.start();
    expect(c.isRecording()).toBe(true);
    c.setDevMode(false);
    expect(c.isRecording()).toBe(false);
  });

  it('stamps performance entries, navigation, and timeline rows in daemon time', async () => {
    const daemonNow = Date.UTC(2026, 9, 24, 12, 0, 0);
    const wallNow = vi.spyOn(Date, 'now').mockReturnValue(daemonNow - 500);
    const { c, advance } = collector({ fetchHealthz: healthzAt(daemonNow) });
    await c.start();
    wallNow.mockRestore();
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

  it('measures first commit and paint after a route change on one clock', async () => {
    const daemonNow = Date.UTC(2026, 9, 24, 12, 0, 0);
    const wallNow = vi.spyOn(Date, 'now').mockReturnValue(daemonNow - 500);
    const { c, advance } = collector({ fetchHealthz: healthzAt(daemonNow) });
    await c.start();
    wallNow.mockRestore();
    c.recordNavigation({ kind: 'route', route: '/x' });
    advance(30);
    c.recordCommit('main', 'mount', 3);
    advance(20);
    c.recordPaint();
    const nav = c.buildFile().navigation[0];
    expect(nav.firstCommitMs).toBe(30);
    expect(nav.paintMs).toBe(50);
  });

  it('waits for the daemon clock before starting', async () => {
    const daemonNow = Date.UTC(2026, 9, 24, 12, 0, 0);
    const wallNow = vi.spyOn(Date, 'now').mockReturnValue(daemonNow - 500);
    const healthz = deferredHealthz();
    const { c } = collector({ fetchHealthz: healthz.fetchHealthz });
    const started = c.start();
    await healthz.called;
    expect(c.isRecording()).toBe(false);
    healthz.respond(new Date(daemonNow).toUTCString());
    await started;
    wallNow.mockRestore();
    expect(c.isRecording()).toBe(true);
    expect(c.startedAt()).toBe(1_000_000 + 500);
    c.recordNavigation({ kind: 'route', route: '/x' });
    expect(c.buildFile().navigation[0].t).toBe(1_000_000 + 500);
    expect(c.buildFile().environment.clockOffsetMs).toBe(500);
  });

  it('does not start if config is disabled while clock synchronization is pending', async () => {
    const healthz = deferredHealthz();
    const { c } = collector({ fetchHealthz: healthz.fetchHealthz });
    const started = c.start();
    await healthz.called;
    c.setConfigEnabled(false);
    healthz.respond(null);
    await started;
    expect(c.isRecording()).toBe(false);
    expect(c.startedAt()).toBeNull();
  });

  it('converts chat load at to daemon time', async () => {
    const daemonNow = Date.UTC(2026, 9, 24, 12, 0, 0);
    const wallNow = vi.spyOn(Date, 'now').mockReturnValue(daemonNow - 500);
    const { c } = collector({ fetchHealthz: healthzAt(daemonNow) });
    await c.start();
    wallNow.mockRestore();
    const sample = {
      sessionId: 'sess-1',
      at: '2026-10-24T12:00:00.000Z',
      start: 'click' as const,
      frameChars: 10,
      records: 1,
      routeToSocketMs: 1,
      socketOpenMs: 1,
      historyWaitMs: 1,
      parseMs: 1,
      reduceMs: 1,
      commitMs: 1,
      afterPaintMs: 1,
      totalMs: 7,
    };
    c.recordChatLoad(sample);
    expect(c.buildFile().chatLoads[0].at).toBe(new Date(Date.parse(sample.at) + 500).toISOString());
    expect(sample.at).toBe('2026-10-24T12:00:00.000Z');
  });

  it('uses handle-output rather than render percentiles', async () => {
    vi.spyOn(inputLatency, 'getRenderStats').mockReturnValue({
      median: 50,
      p99: 90,
    } as ReturnType<typeof inputLatency.getRenderStats>);
    vi.spyOn(inputLatency, 'getWireContext').mockReturnValue({
      framesBetweenP50: 0,
      framesBetweenP99: 0,
      handleOutputMsP50: 3,
      handleOutputMsP99: 9,
      receiveLagP50: 0,
      receiveLagP99: 0,
    });
    const { c, advance } = collector();
    await c.start();
    let counts = { frames: 0, bytes: 0 };
    c.registerTerminal('term-1', () => counts);
    counts = { frames: 2, bytes: 40 };
    advance(1000);
    c.tick();
    vi.restoreAllMocks();
    expect(c.buildFile().terminals).toEqual([
      {
        t: 1_001_000,
        id: 'term-1',
        frames: 2,
        bytes: 40,
        handleOutputP50: 3,
        handleOutputP99: 9,
      },
    ]);
  });

  it('refreshes dynamic workload when the file is built', async () => {
    const { c } = collector();
    await c.start();
    const fixed = {
      workspaces: 1,
      sessions: 2,
      running: 1,
      chats: 1,
      terminals: 1,
      panels: { git: true },
      flags: { desync: false },
    };
    c.setWorkload(fixed);
    const unregister = c.registerTerminal('term-1', () => ({ frames: 0, bytes: 0 }));
    const ws = { readyState: 1 } as unknown as WebSocket;
    c.wrapSocket(ws, 'ws://localhost/ws/dashboard');
    ws.onmessage = () => {};
    ws.onmessage!({ data: '{"type":"sessions"}' } as MessageEvent);
    expect(c.buildFile().workload).toEqual({
      ...fixed,
      mountedTerminals: 1,
      socketsByPath: { '/ws/dashboard': 1 },
      lastDashboardMessageBytes: 19,
    });
    unregister();
  });

  it('reports elapsed time from its own clock', async () => {
    const { c, advance } = collector();
    await c.start();
    advance(90_000);
    expect(c.elapsedMs()).toBe(90_000);
  });
});
