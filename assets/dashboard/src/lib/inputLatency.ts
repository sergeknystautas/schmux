// Keystroke round-trip latency tracker.
// Always active — samples are collected whenever a terminal session is connected.
// The LatencySparkline component reads from this to render the dev-mode overlay.
//
// Tracks two phases:
//   total: markSent → markReceived (WebSocket send → onmessage fires)
//   render: time spent in terminal.write() after receiving

const MAX_SAMPLES = 1000;

export type LatencyStats = {
  count: number;
  p25: number;
  median: number;
  p75: number;
  p95: number;
  p99: number;
  max: number;
  avg: number;
  stddev: number;
};

export type LatencyDistribution = {
  buckets: number[]; // count per 1ms bucket
  maxCount: number; // max count across buckets (for scaling)
  maxMs: number; // upper bound of the last bucket
};

// Server-side latency segments received from the Go backend via stats messages.
// Field names match the JSON keys from LatencyPercentiles in latency_collector.go.
export type ServerLatencySegments = {
  dispatchP50: number;
  dispatchP99: number;
  sendKeysP50: number;
  sendKeysP99: number;
  echoP50: number;
  echoP99: number;
  frameSendP50: number;
  frameSendP99: number;
  sampleCount: number;
  mutexWaitP50?: number;
  mutexWaitP99?: number;
  executeNetP50?: number;
  executeNetP99?: number;
  executeCountP50?: number;
  executeCountP99?: number;
  // Context fields: diagnose whether P99 correlates with backpressure
  outputChDepthP50?: number;
  outputChDepthP99?: number;
  echoDataLenP50?: number;
  echoDataLenP99?: number;
};

// Per-keystroke server-side segment tuple from the inputEcho sideband.
export type ServerSegmentTuple = {
  dispatch: number;
  sendKeys: number;
  echo: number;
  frameSend: number;
  total: number;
  mutexWait?: number;
  executeNet?: number;
  executeCount?: number;
  sessionType?: 'local' | 'remote';
  receiveLag?: number; // event loop lag at sideband processing time
};

// Full latency breakdown for a cohort (typical IQR or outlier P95+).
export type LatencyBreakdown = {
  network: number;
  jsQueue: number;
  handler: number;
  wsWrite: number;
  xterm: number;
  tmuxCmd: number;
  paneOutput: number;
  segmentSum: number;
  total: number;
};

export type WireContext = {
  framesBetweenP50: number;
  framesBetweenP99: number;
  handleOutputMsP50: number;
  handleOutputMsP99: number;
  receiveLagP50: number;
  receiveLagP99: number;
};

// Snapshot of all mutable tracker state, used for per-machine storage.
type TrackerSnapshot = {
  samples: number[];
  renderSamples: number[];
  serverSegmentSamples: ServerSegmentTuple[];
  lagSamples: number[];
  receiveLagSamples: number[];
  framesBetweenSamples: number[];
  handleOutputTimeSamples: number[];
  frameCounter: number;
  lastInputTime: number;
  serverLatency: ServerLatencySegments | null;
};

export class InputLatencyTracker {
  samples: number[] = [];
  renderSamples: number[] = [];
  serverSegmentSamples: ServerSegmentTuple[] = []; // per-keystroke paired server-side segments
  lagSamples: number[] = []; // event loop lag samples (MessageChannel-based, send-time)
  receiveLagSamples: number[] = []; // event loop lag at receive time
  framesBetweenSamples: number[] = []; // output frames between send and receive
  handleOutputTimeSamples: number[] = []; // per-frame binary handler processing time
  private _frameCounter = 0;
  private lastInputTime = 0;
  private version = 0;
  private serverLatency: ServerLatencySegments | null = null;

  // Per-machine data: "local" for all local sessions, remote host ID for remote.
  private machineKey = 'local';
  private machineSnapshots = new Map<string, TrackerSnapshot>();

  // Switch to a different machine's dataset. Saves current data, restores
  // (or creates) the target machine's data. All local sessions share "local".
  switchMachine(key: string) {
    if (key === this.machineKey) return;
    // Save current state
    this.machineSnapshots.set(this.machineKey, {
      samples: this.samples,
      renderSamples: this.renderSamples,
      serverSegmentSamples: this.serverSegmentSamples,
      lagSamples: this.lagSamples,
      receiveLagSamples: this.receiveLagSamples,
      framesBetweenSamples: this.framesBetweenSamples,
      handleOutputTimeSamples: this.handleOutputTimeSamples,
      frameCounter: this._frameCounter,
      lastInputTime: this.lastInputTime,
      serverLatency: this.serverLatency,
    });
    // Restore or create target
    this.machineKey = key;
    const snap = this.machineSnapshots.get(key);
    if (snap) {
      this.samples = snap.samples;
      this.renderSamples = snap.renderSamples;
      this.serverSegmentSamples = snap.serverSegmentSamples;
      this.lagSamples = snap.lagSamples;
      this.receiveLagSamples = snap.receiveLagSamples;
      this.framesBetweenSamples = snap.framesBetweenSamples;
      this.handleOutputTimeSamples = snap.handleOutputTimeSamples;
      this._frameCounter = snap.frameCounter;
      this.lastInputTime = snap.lastInputTime;
      this.serverLatency = snap.serverLatency;
    } else {
      this.samples = [];
      this.renderSamples = [];
      this.serverSegmentSamples = [];
      this.lagSamples = [];
      this.receiveLagSamples = [];
      this.framesBetweenSamples = [];
      this.handleOutputTimeSamples = [];
      this._frameCounter = 0;
      this.lastInputTime = 0;
      this.serverLatency = null;
    }
    this.version++;
  }

  getMachineKey(): string {
    return this.machineKey;
  }

  markSent() {
    this.lastInputTime = performance.now();
    // Event loop lag probe: post a MessageChannel message and measure
    // how long before the handler fires. This captures JS main thread
    // congestion without setTimeout's 4ms minimum delay.
    const sentTime = performance.now();
    const channel = new MessageChannel();
    channel.port1.onmessage = () => {
      const lag = performance.now() - sentTime;
      this.lagSamples.push(lag);
      if (this.lagSamples.length > MAX_SAMPLES) {
        this.lagSamples = this.lagSamples.slice(-MAX_SAMPLES);
      }
    };
    channel.port2.postMessage(null);
  }

  markReceived() {
    if (this.lastInputTime === 0) return;
    const now = performance.now();
    const rtt = now - this.lastInputTime;
    // Staleness guard: discard if >2s since keystroke (agent is thinking, not echoing)
    if (rtt > 2000) {
      this.lastInputTime = 0;
      return;
    }
    this.lastInputTime = 0;
    this.samples.push(rtt);
    if (this.samples.length > MAX_SAMPLES) {
      this.samples = this.samples.slice(-MAX_SAMPLES);
    }

    // Capture frame counter: how many output frames were processed between
    // markSent() and this echo frame arriving.
    this.framesBetweenSamples.push(this._frameCounter);
    if (this.framesBetweenSamples.length > MAX_SAMPLES) {
      this.framesBetweenSamples = this.framesBetweenSamples.slice(-MAX_SAMPLES);
    }
    this._frameCounter = 0;

    this.version++;
  }

  recordServerSegments(tuple: ServerSegmentTuple) {
    this.serverSegmentSamples.push(tuple);
    if (this.serverSegmentSamples.length > MAX_SAMPLES) {
      this.serverSegmentSamples = this.serverSegmentSamples.slice(-MAX_SAMPLES);
    }

    // Event loop lag probe: measures JS main thread congestion at the moment
    // the sideband message is processed. The callback fires in a subsequent
    // macrotask and writes directly into the just-pushed tuple.
    const probeStart = performance.now();
    const channel = new MessageChannel();
    const target = this.serverSegmentSamples[this.serverSegmentSamples.length - 1];
    channel.port1.onmessage = () => {
      target.receiveLag = performance.now() - probeStart;
    };
    channel.port2.postMessage(null);
  }

  markRenderTime(ms: number) {
    this.renderSamples.push(ms);
    if (this.renderSamples.length > MAX_SAMPLES) {
      this.renderSamples = this.renderSamples.slice(-MAX_SAMPLES);
    }
  }

  recordFrameProcessed() {
    this._frameCounter++;
  }

  recordHandleOutputTime(ms: number) {
    this.handleOutputTimeSamples.push(ms);
    if (this.handleOutputTimeSamples.length > MAX_SAMPLES) {
      this.handleOutputTimeSamples = this.handleOutputTimeSamples.slice(-MAX_SAMPLES);
    }
  }

  getWireContext(): WireContext | null {
    if (
      this.framesBetweenSamples.length === 0 &&
      this.handleOutputTimeSamples.length === 0 &&
      this.receiveLagSamples.length === 0
    ) {
      return null;
    }
    const fbStats = this.computeStats(this.framesBetweenSamples);
    const hoStats = this.computeStats(this.handleOutputTimeSamples);
    const rlStats = this.computeStats(this.receiveLagSamples);
    return {
      framesBetweenP50: fbStats?.median ?? 0,
      framesBetweenP99: fbStats?.p99 ?? 0,
      handleOutputMsP50: hoStats?.median ?? 0,
      handleOutputMsP99: hoStats?.p99 ?? 0,
      receiveLagP50: rlStats?.median ?? 0,
      receiveLagP99: rlStats?.p99 ?? 0,
    };
  }

  getVersion() {
    return this.version;
  }

  private computeStats(arr: number[]): LatencyStats | null {
    if (arr.length === 0) return null;
    const sorted = [...arr].sort((a, b) => a - b);
    const sum = sorted.reduce((a, b) => a + b, 0);
    const avg = sum / sorted.length;
    let variance = 0;
    for (const v of sorted) {
      const d = v - avg;
      variance += d * d;
    }
    variance /= sorted.length;
    return {
      count: sorted.length,
      p25: sorted[Math.floor(sorted.length * 0.25)],
      median: sorted[Math.floor(sorted.length / 2)],
      p75: sorted[Math.floor(sorted.length * 0.75)],
      p95: sorted[Math.floor(sorted.length * 0.95)],
      p99: sorted[Math.floor(sorted.length * 0.99)],
      max: sorted[sorted.length - 1],
      avg,
      stddev: Math.sqrt(variance),
    };
  }

  getStats(): LatencyStats | null {
    return this.computeStats(this.samples);
  }

  getRenderStats(): LatencyStats | null {
    return this.computeStats(this.renderSamples);
  }

  getDistribution(): LatencyDistribution | null {
    if (this.samples.length === 0) return null;
    // Determine range: cap at p99 to avoid a single outlier stretching the chart
    const sorted = [...this.samples].sort((a, b) => a - b);
    const p99 = sorted[Math.floor(sorted.length * 0.99)];
    const maxMs = Math.max(Math.ceil(p99), 10); // at least 10ms range
    const numBuckets = maxMs; // one bucket per ms
    const buckets = new Array(numBuckets).fill(0);
    for (const v of this.samples) {
      const idx = Math.min(Math.floor(v), numBuckets - 1);
      buckets[idx]++;
    }
    let maxCount = 0;
    for (const c of buckets) if (c > maxCount) maxCount = c;
    return { buckets, maxCount, maxMs };
  }

  reset() {
    this.samples = [];
    this.renderSamples = [];
    this.serverSegmentSamples = [];
    this.lagSamples = [];
    this.receiveLagSamples = [];
    this.framesBetweenSamples = [];
    this.handleOutputTimeSamples = [];
    this._frameCounter = 0;
    this.lastInputTime = 0;
    this.serverLatency = null;
    this.version++;
  }

  updateServerLatency(data: ServerLatencySegments) {
    this.serverLatency = data;
    this.version++;
  }

  getServerLatency(): ServerLatencySegments | null {
    return this.serverLatency;
  }

  getBreakdown(level: 'typical' | 'outlier'): LatencyBreakdown | null {
    const pairedCount = Math.min(
      this.samples.length,
      this.serverSegmentSamples.length,
      this.renderSamples.length
    );
    if (pairedCount < 5) return null;

    const sOff = this.samples.length - pairedCount;
    const segOff = this.serverSegmentSamples.length - pairedCount;
    const rOff = this.renderSamples.length - pairedCount;

    // Build per-tuple full breakdowns
    type FullTuple = {
      clientRTT: number;
      handler: number;
      tmuxCmd: number;
      paneOutput: number;
      wsWrite: number;
      xterm: number;
      jsQueue: number;
      network: number; // residual, clamped to 0
    };
    const tuples: FullTuple[] = [];
    for (let i = 0; i < pairedCount; i++) {
      const clientRTT = this.samples[sOff + i];
      const seg = this.serverSegmentSamples[segOff + i];
      const xterm = this.renderSamples[rOff + i];
      const serverTotal = seg.dispatch + seg.sendKeys + seg.echo + seg.frameSend;
      if (serverTotal > clientRTT) continue;

      // Per design: receiveLag === undefined means 0 ("we don't know")
      const jsQueue = seg.receiveLag ?? 0;
      const measured = serverTotal + xterm + jsQueue;
      const network = Math.max(0, clientRTT - measured);
      tuples.push({
        clientRTT,
        handler: seg.dispatch,
        tmuxCmd: seg.sendKeys,
        paneOutput: seg.echo,
        wsWrite: seg.frameSend,
        xterm,
        jsQueue,
        network,
      });
    }
    if (tuples.length < 5) return null;

    // Compute percentile boundaries from valid paired tuple RTTs
    const sortedRTTs = tuples.map(t => t.clientRTT).sort((a, b) => a - b);
    const p25 = sortedRTTs[Math.floor(sortedRTTs.length * 0.25)];
    const p75 = sortedRTTs[Math.floor(sortedRTTs.length * 0.75)];
    const p95 = sortedRTTs[Math.floor(sortedRTTs.length * 0.95)];

    // Select cohort
    let cohort: FullTuple[];
    if (level === 'typical') {
      cohort = tuples.filter(t => t.clientRTT >= p25 && t.clientRTT <= p75);
    } else {
      cohort = tuples.filter(t => t.clientRTT > p95);
    }
    if (cohort.length < 5) return null;

    // Compute median of each segment within the cohort
    const median = (arr: number[]) => {
      const s = [...arr].sort((a, b) => a - b);
      return s[Math.floor(s.length / 2)];
    };

    const handler = median(cohort.map(t => t.handler));
    const tmuxCmd = median(cohort.map(t => t.tmuxCmd));
    const paneOutput = median(cohort.map(t => t.paneOutput));
    const wsWrite = median(cohort.map(t => t.wsWrite));
    const xtermMedian = median(cohort.map(t => t.xterm));
    const jsQueue = median(cohort.map(t => t.jsQueue));
    const network = median(cohort.map(t => t.network));
    const total = median(cohort.map(t => t.clientRTT));
    const segmentSum = network + jsQueue + handler + wsWrite + xtermMedian + tmuxCmd + paneOutput;

    return {
      network,
      jsQueue,
      handler,
      wsWrite,
      xterm: xtermMedian,
      tmuxCmd,
      paneOutput,
      total,
      segmentSum,
    };
  }
}

export const inputLatency = new InputLatencyTracker();

// Expose for Playwright benchmarks (scenario tests run against production builds)
if (typeof window !== 'undefined') {
  window.__inputLatency = inputLatency;
}
