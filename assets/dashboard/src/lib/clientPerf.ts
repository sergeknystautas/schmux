// Client performance recorder. A module singleton like inputLatency; every
// method is a no-op while off. See docs/client-performance.md for the file.
import type { ChatLoadSample } from './chat/loadTelemetry';
import { saveSnapshot, loadSnapshot, clearSnapshot } from './clientPerfStore';
import { startObservers } from './clientPerfObservers';
import { inputLatency } from './inputLatency';

export const STALL_MS = 100;
const COMMIT_MS = 16;
const INTERACTION_MS = 100;
const WS_SLOW_MS = 5;
const WS_LARGE_BYTES = 50 * 1024;
export const TIMELINE_CAP = 3600;
const BROWSER_SWITCH_KEY = 'schmux:client-perf';
const BROWSER_ID_KEY = 'schmux:client-perf-browser-id';
const TAB_ID_KEY = 'schmux:client-perf-tab-id';

export interface PerfRow {
  t: number;
  loop: number;
  longTask: number;
  commit: number;
  wsBytes: number;
  wsHandler: number;
  fetches: number;
  route: string;
  hidden: boolean;
}
export interface LongTaskRecord {
  t: number;
  duration: number;
  attribution: string;
}
export interface InteractionRecord {
  t: number;
  type: string;
  target: string;
  inputDelay: number;
  processing: number;
  presentation: number;
}
export interface CommitRecord {
  t: number;
  id: string;
  phase: string;
  duration: number;
  route: string;
}
export interface WsSecondRecord {
  t: number;
  path: string;
  type: string;
  count: number;
  bytes: number;
  handlerMs: number;
}
export interface WsMessageRecord {
  t: number;
  path: string;
  type: string;
  bytes: number;
  handlerMs: number;
}
export interface FetchRecord {
  t: number;
  endpoint: string;
  duration: number;
  bytes: number;
}
export interface TerminalSecondRecord {
  t: number;
  id: string;
  frames: number;
  bytes: number;
  handleOutputP50: number;
  handleOutputP99: number;
}
export interface MemoryRecord {
  t: number;
  heapBytes: number | null;
  domNodes: number;
  terminals: number;
  sockets: number;
}
export interface NavigationRecord {
  t: number;
  kind: 'route' | 'visible' | 'hidden' | 'reload';
  route: string;
  firstCommitMs?: number;
  paintMs?: number;
}
export interface ErrorRecord {
  t: number;
  message: string;
}
export interface ChatIds {
  workspaceId: string;
  sessionId: string;
}
export interface BuildInfo {
  version: string;
  devMode: boolean;
  sourceWorkspace: string;
  viteDev: boolean;
}
export interface Environment {
  userAgent: string;
  cpus: number;
  deviceMemoryGb: number | null;
  viewport: { w: number; h: number };
  pixelRatio: number;
  host: string;
  remoteClient: boolean;
  unsupported: string[];
  clockOffsetMs: number;
  /** Worst main-thread cost of one snapshot write by the recorder itself. */
  persistMaxMs: number;
}

/** A record sourced from a PerformanceEntry: the collector stamps `t` from `startTime`. */
export type PerfEntryInput<T extends { t: number }> = Omit<T, 't'> & { startTime: number };
export interface Workload {
  workspaces: number;
  sessions: number;
  running: number;
  chats: number;
  terminals: number;
  mountedTerminals: number;
  socketsByPath: Record<string, number>;
  lastDashboardMessageBytes: number;
  panels: Record<string, boolean>;
  flags: Record<string, boolean>;
}

export interface ClientPerfFile {
  version: 1;
  browserId: string;
  startedAt: number;
  builtAt: number;
  build: BuildInfo;
  environment: Environment;
  workload: Workload;
  timeline: PerfRow[];
  stalls: number[];
  longTasks: LongTaskRecord[];
  interactions: InteractionRecord[];
  commits: CommitRecord[];
  websocket: { perSecond: WsSecondRecord[]; individual: WsMessageRecord[] };
  fetches: FetchRecord[];
  terminals: TerminalSecondRecord[];
  chatLoads: ChatLoadSample[];
  memory: MemoryRecord[];
  navigation: NavigationRecord[];
  errors: ErrorRecord[];
}

class Ring<T> {
  private items: T[] = [];
  constructor(readonly cap: number) {}
  push(v: T) {
    this.items.push(v);
    if (this.items.length > this.cap) this.items.shift();
  }
  toArray(): T[] {
    return this.items.slice();
  }
  clear() {
    this.items = [];
  }
  get length() {
    return this.items.length;
  }
}

interface Snapshot {
  startedAt: number;
  stalls: number;
  chat: ChatIds | null;
  buffers: Omit<
    ClientPerfFile,
    'version' | 'browserId' | 'startedAt' | 'builtAt' | 'build' | 'environment' | 'workload'
  >;
}

function readOrCreate(storage: Storage, key: string): string {
  let v = storage.getItem(key);
  if (!v) {
    v = Math.random().toString(36).slice(2, 10);
    storage.setItem(key, v);
  }
  return v;
}

function isStall(row: Pick<PerfRow, 'loop' | 'longTask'>): boolean {
  return row.loop >= STALL_MS || row.longTask >= STALL_MS;
}

function socketPath(url: string): string {
  let pathname: string;
  try {
    pathname = new URL(url, 'http://x').pathname;
  } catch {
    return url;
  }
  return pathname.replace(/^\/ws\/(terminal|chat|logs\/fence)\/[^/]+/, '/ws/$1/:id');
}
function messageBytes(data: unknown): number {
  if (typeof data === 'string') return data.length;
  if (data instanceof ArrayBuffer) return data.byteLength;
  if (ArrayBuffer.isView(data)) return data.byteLength;
  if (typeof Blob !== 'undefined' && data instanceof Blob) return data.size;
  return 0;
}
const TYPE_RE = /"type"\s*:\s*"([^"]+)"/;
function messageType(data: unknown): string {
  if (typeof data !== 'string') return 'binary';
  const m = TYPE_RE.exec(data.slice(0, 200));
  return m ? m[1] : 'untyped';
}

export class ClientPerfCollector {
  private now: () => number;
  private tabId: string;
  // A tab id read from sessionStorage is rotated on restore, because a
  // duplicated tab inherits sessionStorage and would share the snapshot key.
  private rotateTabId: boolean;
  private configEnabled = false;
  private devMode = false;
  private recording = false;
  private restoring: Promise<boolean> | null = null;
  private persistMaxMs = 0;
  private started: number | null = null;
  private stalls = 0;
  private chat: ChatIds | null = null;
  private build: BuildInfo = { version: '', devMode: false, sourceWorkspace: '', viteDev: false };
  private workload: Workload = {
    workspaces: 0,
    sessions: 0,
    running: 0,
    chats: 0,
    terminals: 0,
    mountedTerminals: 0,
    socketsByPath: {},
    lastDashboardMessageBytes: 0,
    panels: {},
    flags: {},
  };
  private clockOffsetMs = 0;
  private unsupported: string[] = [];
  private remote = false;
  private pendingRouteNav: NavigationRecord | null = null;
  private route = typeof window !== 'undefined' ? window.location.pathname : '';
  private hidden = false;
  private listeners = new Set<() => void>();
  private stopObservers: (() => void) | null = null;
  private ver = 0;
  private socketPaths = new Map<string, number>();
  private lastDashboardBytes = 0;
  private terminalSamplers = new Map<string, () => { frames: number; bytes: number }>();
  private terminalLast = new Map<string, { frames: number; bytes: number }>();

  private timeline = new Ring<PerfRow>(TIMELINE_CAP);
  private longTasks = new Ring<LongTaskRecord>(2000);
  private interactions = new Ring<InteractionRecord>(2000);
  private commits = new Ring<CommitRecord>(2000);
  private wsSeconds = new Ring<WsSecondRecord>(3600);
  private wsIndividual = new Ring<WsMessageRecord>(2000);
  private fetches = new Ring<FetchRecord>(2000);
  private terminals = new Ring<TerminalSecondRecord>(3600);
  private chatLoads = new Ring<ChatLoadSample>(200);
  private memory = new Ring<MemoryRecord>(360);
  private navigation = new Ring<NavigationRecord>(500);
  private errors = new Ring<ErrorRecord>(100);

  // The second being accumulated. Closed into `timeline` by tick().
  private cur = this.emptySecond();
  private curWs = new Map<string, WsSecondRecord>();

  constructor(opts: { now?: () => number; tabId?: string } = {}) {
    this.now = opts.now ?? (() => Date.now());
    this.rotateTabId = opts.tabId === undefined && typeof sessionStorage !== 'undefined';
    this.tabId =
      opts.tabId ??
      (typeof sessionStorage !== 'undefined' ? readOrCreate(sessionStorage, TAB_ID_KEY) : 'tab');
  }

  // Both switches the daemon controls: the config flag and dev mode. The
  // per-browser switch in localStorage is the third, written by start/stop.
  private enabled(): boolean {
    return this.configEnabled && this.devMode;
  }

  /** Daemon time for a PerformanceEntry timestamp (ms since performance.timeOrigin). */
  perfToDaemon(perfMs: number): number {
    const origin = typeof performance !== 'undefined' ? (performance.timeOrigin ?? 0) : 0;
    return Math.round(origin + perfMs) + this.clockOffsetMs;
  }

  elapsedMs(): number {
    return this.started === null ? 0 : this.t() - this.started;
  }

  private emptySecond() {
    return { loop: 0, longTask: 0, commit: 0, wsBytes: 0, wsHandler: 0, fetches: 0 };
  }
  private t(): number {
    return this.now() + this.clockOffsetMs;
  }
  private notify() {
    this.ver += 1;
    this.listeners.forEach((l) => l());
  }
  version() {
    return this.ver;
  }
  private browserSwitch(): boolean {
    return localStorage.getItem(BROWSER_SWITCH_KEY) === '1';
  }

  subscribe(listener: () => void): () => void {
    this.listeners.add(listener);
    return () => this.listeners.delete(listener);
  }

  setConfigEnabled(on: boolean) {
    this.configEnabled = on;
    this.applySwitches();
  }

  setDevMode(on: boolean) {
    this.devMode = on;
    this.applySwitches();
  }

  private applySwitches() {
    if (!this.enabled() && (this.recording || this.chat)) {
      this.stop();
      this.chat = null;
      void clearSnapshot(this.tabId);
      this.notify();
    }
  }

  start() {
    if (!this.enabled() || this.recording) return;
    localStorage.setItem(BROWSER_SWITCH_KEY, '1');
    this.recording = true;
    this.started = this.t();
    this.stopObservers = startObservers(this);
    this.notify();
  }

  stop() {
    if (!this.recording) return;
    localStorage.removeItem(BROWSER_SWITCH_KEY);
    this.stopObservers?.();
    this.stopObservers = null;
    this.recording = false;
    this.started = null;
    this.clearBuffers();
    this.stalls = 0;
    void clearSnapshot(this.tabId);
    this.notify();
  }

  isRecording() {
    return this.recording;
  }
  startedAt() {
    return this.started;
  }
  stallCount() {
    return this.stalls;
  }
  hasUnsent() {
    return this.recording && this.timeline.length > 0;
  }
  setChat(ids: ChatIds) {
    this.chat = ids;
    this.notify();
  }
  getChat() {
    return this.chat;
  }
  setBuild(b: BuildInfo) {
    this.build = b;
  }
  setWorkload(w: Workload) {
    this.workload = w;
  }
  setClockOffset(ms: number) {
    this.clockOffsetMs = ms;
  }
  setRemoteClient(v: boolean) {
    this.remote = v;
  }
  recordPaint() {
    if (this.pendingRouteNav && this.pendingRouteNav.paintMs === undefined) {
      this.pendingRouteNav.paintMs = this.t() - this.pendingRouteNav.t;
    }
  }
  noteUnsupported(api: string) {
    if (!this.unsupported.includes(api)) this.unsupported.push(api);
  }

  terminalCount() {
    return this.terminalSamplers.size;
  }
  socketCount() {
    let n = 0;
    for (const v of this.socketPaths.values()) n += v;
    return n;
  }
  socketsByPath(): Record<string, number> {
    return Object.fromEntries(this.socketPaths);
  }
  lastDashboardMessageBytes() {
    return this.lastDashboardBytes;
  }

  registerTerminal(id: string, sample: () => { frames: number; bytes: number }): () => void {
    this.terminalSamplers.set(id, sample);
    this.terminalLast.set(id, sample());
    return () => {
      this.terminalSamplers.delete(id);
      this.terminalLast.delete(id);
    };
  }

  // Called from tick() before the row closes: one terminal row per mounted terminal.
  private sampleTerminals(p50: number, p99: number) {
    for (const [id, sample] of this.terminalSamplers) {
      const cur = sample();
      const prev = this.terminalLast.get(id) ?? cur;
      this.terminalLast.set(id, cur);
      this.terminals.push({
        t: this.t(),
        id,
        frames: cur.frames - prev.frames,
        bytes: cur.bytes - prev.bytes,
        handleOutputP50: p50,
        handleOutputP99: p99,
      });
    }
  }

  private openSocket(path: string) {
    this.socketPaths.set(path, (this.socketPaths.get(path) ?? 0) + 1);
  }
  private closeSocket(path: string) {
    const n = (this.socketPaths.get(path) ?? 0) - 1;
    if (n <= 0) this.socketPaths.delete(path);
    else this.socketPaths.set(path, n);
  }

  wrapSocket(ws: WebSocket, url: string): WebSocket {
    const path = socketPath(url);
    let handler: ((ev: MessageEvent) => void) | null = null;
    let lastEvent: MessageEvent | null = null;
    const wrapped = (ev: MessageEvent) => {
      if (ev === lastEvent) return; // real sockets reach us by addEventListener and by the getter; count once
      lastEvent = ev;
      const t0 = performance.now();
      try {
        handler?.(ev);
      } finally {
        const bytes = messageBytes(ev.data);
        if (path === '/ws/dashboard') this.lastDashboardBytes = bytes;
        this.recordWsMessage(path, messageType(ev.data), bytes, performance.now() - t0);
      }
    };
    Object.defineProperty(ws, 'onmessage', {
      configurable: true,
      get: () => (handler ? wrapped : null),
      set: (fn) => {
        handler = fn;
      },
    });
    if (typeof ws.addEventListener === 'function') ws.addEventListener('message', wrapped);
    let closeHandler: ((ev: CloseEvent) => void) | null = null;
    let lastClose: CloseEvent | null = null;
    const wrappedClose = (ev: CloseEvent) => {
      if (ev === lastClose) return; // same dedupe as message events
      lastClose = ev;
      this.closeSocket(path);
      closeHandler?.(ev);
    };
    Object.defineProperty(ws, 'onclose', {
      configurable: true,
      get: () => wrappedClose,
      set: (fn) => {
        closeHandler = fn;
      },
    });
    if (typeof ws.addEventListener === 'function') ws.addEventListener('close', wrappedClose);
    this.openSocket(path);
    return ws;
  }

  recordLoopDelay(ms: number) {
    if (this.recording) this.cur.loop = Math.max(this.cur.loop, ms);
  }
  recordLongTask(r: PerfEntryInput<LongTaskRecord>) {
    if (!this.recording) return;
    const { startTime, ...rest } = r;
    this.cur.longTask += rest.duration;
    this.longTasks.push({ t: this.perfToDaemon(startTime), ...rest });
  }
  recordInteraction(r: PerfEntryInput<InteractionRecord>) {
    if (!this.recording) return;
    const { startTime, ...rest } = r;
    if (rest.inputDelay + rest.processing + rest.presentation >= INTERACTION_MS)
      this.interactions.push({ t: this.perfToDaemon(startTime), ...rest });
  }
  recordCommit(id: string, phase: string, durationMs: number) {
    if (!this.recording) return;
    this.cur.commit += durationMs;
    if (durationMs > COMMIT_MS)
      this.commits.push({
        t: this.t(),
        id,
        phase,
        duration: Math.round(durationMs),
        route: this.route,
      });
    if (this.pendingRouteNav && this.pendingRouteNav.firstCommitMs === undefined) {
      this.pendingRouteNav.firstCommitMs = this.t() - this.pendingRouteNav.t;
    }
  }
  recordWsMessage(path: string, type: string, bytes: number, handlerMs: number) {
    if (!this.recording) return;
    this.cur.wsBytes += bytes;
    this.cur.wsHandler += handlerMs;
    const key = `${path}\n${type}`;
    const rec = this.curWs.get(key) ?? {
      t: this.t(),
      path,
      type,
      count: 0,
      bytes: 0,
      handlerMs: 0,
    };
    rec.count += 1;
    rec.bytes += bytes;
    rec.handlerMs += handlerMs;
    this.curWs.set(key, rec);
    if (handlerMs > WS_SLOW_MS || bytes > WS_LARGE_BYTES)
      this.wsIndividual.push({ t: this.t(), path, type, bytes, handlerMs });
  }
  recordFetch(r: PerfEntryInput<FetchRecord>) {
    if (!this.recording) return;
    const { startTime, ...rest } = r;
    this.cur.fetches += 1;
    this.fetches.push({ t: this.perfToDaemon(startTime), ...rest });
  }
  recordTerminalSecond(r: TerminalSecondRecord) {
    if (this.recording) this.terminals.push(r);
  }
  recordChatLoad(s: ChatLoadSample) {
    if (this.recording) this.chatLoads.push(s);
  }
  recordMemory(r: Omit<MemoryRecord, 't'>) {
    if (this.recording) this.memory.push({ t: this.t(), ...r });
  }
  recordNavigation(r: Omit<NavigationRecord, 't'>) {
    if (!this.recording) return;
    const rec: NavigationRecord = { t: this.t(), ...r };
    this.navigation.push(rec);
    if (rec.kind === 'route') this.pendingRouteNav = rec;
  }
  recordError(message: string) {
    if (this.recording) this.errors.push({ t: this.t(), message });
  }
  recordRoute(route: string) {
    this.route = route;
  }
  recordHidden(hidden: boolean) {
    this.hidden = hidden;
  }

  tick() {
    if (!this.recording) return;
    const render = inputLatency.getRenderStats();
    this.sampleTerminals(render?.median ?? 0, render?.p99 ?? 0);
    const row: PerfRow = { t: this.t(), ...this.cur, route: this.route, hidden: this.hidden };
    this.timeline.push(row);
    if (isStall(row)) this.stalls += 1;
    for (const rec of this.curWs.values()) this.wsSeconds.push(rec);
    this.curWs.clear();
    this.cur = this.emptySecond();
    this.notify();
  }

  private buffers() {
    const timeline = this.timeline.toArray();
    return {
      timeline,
      stalls: timeline.map((r, i) => (isStall(r) ? i : -1)).filter((i) => i >= 0),
      longTasks: this.longTasks.toArray(),
      interactions: this.interactions.toArray(),
      commits: this.commits.toArray(),
      websocket: { perSecond: this.wsSeconds.toArray(), individual: this.wsIndividual.toArray() },
      fetches: this.fetches.toArray(),
      terminals: this.terminals.toArray(),
      chatLoads: this.chatLoads.toArray(),
      memory: this.memory.toArray(),
      navigation: this.navigation.toArray(),
      errors: this.errors.toArray(),
    };
  }

  private clearBuffers() {
    for (const r of [
      this.timeline,
      this.longTasks,
      this.interactions,
      this.commits,
      this.wsSeconds,
      this.wsIndividual,
      this.fetches,
      this.terminals,
      this.chatLoads,
      this.memory,
      this.navigation,
      this.errors,
    ])
      r.clear();
    this.curWs.clear();
    this.cur = this.emptySecond();
  }

  environment(): Environment {
    const nav = typeof navigator !== 'undefined' ? navigator : ({} as Navigator);
    return {
      userAgent: nav.userAgent ?? '',
      cpus: nav.hardwareConcurrency ?? 0,
      deviceMemoryGb: (nav as Navigator & { deviceMemory?: number }).deviceMemory ?? null,
      viewport: {
        w: typeof window !== 'undefined' ? window.innerWidth : 0,
        h: typeof window !== 'undefined' ? window.innerHeight : 0,
      },
      pixelRatio: typeof window !== 'undefined' ? window.devicePixelRatio : 1,
      host: typeof window !== 'undefined' ? window.location.host : '',
      remoteClient: this.remote,
      unsupported: this.unsupported.slice(),
      clockOffsetMs: this.clockOffsetMs,
      persistMaxMs: this.persistMaxMs,
    };
  }

  buildFile(): ClientPerfFile {
    return {
      version: 1,
      browserId: readOrCreate(localStorage, BROWSER_ID_KEY),
      startedAt: this.started ?? this.t(),
      builtAt: this.t(),
      build: this.build,
      environment: this.environment(),
      workload: this.workload,
      ...this.buffers(),
    };
  }

  markSent() {
    this.clearBuffers();
    void this.persist();
    this.notify();
  }

  async persist() {
    if (!this.recording) return;
    const t0 = performance.now();
    const snap: Snapshot = {
      startedAt: this.started ?? this.t(),
      stalls: this.stalls,
      chat: this.chat,
      buffers: this.buffers(),
    };
    const buildMs = performance.now() - t0;
    const putMs = await saveSnapshot(this.tabId, snap);
    this.persistMaxMs = Math.max(this.persistMaxMs, Math.round(buildMs + putMs));
  }

  // Single-flight: the config response and the healthz response both call
  // restore() on startup, and two concurrent loads would apply the snapshot twice.
  restore(): Promise<boolean> {
    if (!this.enabled() || !this.browserSwitch()) return Promise.resolve(false);
    if (this.recording) return Promise.resolve(true);
    if (!this.restoring) {
      this.restoring = this.doRestore().finally(() => {
        this.restoring = null;
      });
    }
    return this.restoring;
  }

  private async doRestore(): Promise<boolean> {
    const snap = await loadSnapshot<Snapshot>(this.tabId);
    if (!snap) return false;
    if (this.recording) return true;
    this.recording = true;
    this.started = snap.startedAt;
    this.stalls = snap.stalls;
    this.chat = snap.chat;
    const b = snap.buffers;
    b.timeline.forEach((r) => this.timeline.push(r));
    b.longTasks.forEach((r) => this.longTasks.push(r));
    b.interactions.forEach((r) => this.interactions.push(r));
    b.commits.forEach((r) => this.commits.push(r));
    b.websocket.perSecond.forEach((r) => this.wsSeconds.push(r));
    b.websocket.individual.forEach((r) => this.wsIndividual.push(r));
    b.fetches.forEach((r) => this.fetches.push(r));
    b.terminals.forEach((r) => this.terminals.push(r));
    b.chatLoads.forEach((r) => this.chatLoads.push(r));
    b.memory.forEach((r) => this.memory.push(r));
    b.navigation.forEach((r) => this.navigation.push(r));
    b.errors.forEach((r) => this.errors.push(r));
    this.recordNavigation({ kind: 'reload', route: this.route });
    this.stopObservers = startObservers(this);
    if (this.rotateTabId) {
      const old = this.tabId;
      this.tabId = Math.random().toString(36).slice(2, 10);
      sessionStorage.setItem(TAB_ID_KEY, this.tabId);
      await clearSnapshot(old);
      await this.persist();
    }
    this.notify();
    return true;
  }
}

export const clientPerf = new ClientPerfCollector();
