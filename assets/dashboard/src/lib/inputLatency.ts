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
  median: number;
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
};

// Full latency breakdown for a single percentile level (P50 or P99).
export type LatencyBreakdown = {
  dispatch: number;
  sendKeys: number;
  echo: number;
  frameSend: number;
  eventLoopLag: number;
  wireResidual: number;
  render: number;
  total: number;
  mutexWait?: number;
  executeNet?: number;
};

export type WireContext = {
  framesBetweenP50: number;
  framesBetweenP99: number;
  handleOutputMsP50: number;
  handleOutputMsP99: number;
  receiveLagP50: number;
  receiveLagP99: number;
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
    const rtt = performance.now() - this.lastInputTime;
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

    // Receive-time event loop lag probe: measures JS main thread congestion
    // at the moment the echo frame is being processed — captures congestion
    // that the send-time probe misses (e.g., burst output processing).
    const recvTime = performance.now();
    const channel = new MessageChannel();
    channel.port1.onmessage = () => {
      const lag = performance.now() - recvTime;
      this.receiveLagSamples.push(lag);
      if (this.receiveLagSamples.length > MAX_SAMPLES) {
        this.receiveLagSamples = this.receiveLagSamples.slice(-MAX_SAMPLES);
      }
    };
    channel.port2.postMessage(null);

    this.version++;
  }

  recordServerSegments(tuple: ServerSegmentTuple) {
    this.serverSegmentSamples.push(tuple);
    if (this.serverSegmentSamples.length > MAX_SAMPLES) {
      this.serverSegmentSamples = this.serverSegmentSamples.slice(-MAX_SAMPLES);
    }
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
      median: sorted[Math.floor(sorted.length / 2)],
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

  getBreakdown(level: 'p50' | 'p99'): LatencyBreakdown | null {
    // Need at least 3 paired samples (client RTT, server segments, render all
    // aligned by index) to compute a meaningful breakdown.
    if (this.serverSegmentSamples.length < 3) return null;
    const pairedCount = Math.min(
      this.samples.length,
      this.serverSegmentSamples.length,
      this.renderSamples.length
    );
    if (pairedCount < 3) return null;

    // Use the most recent paired samples (aligned by index)
    const sOff = this.samples.length - pairedCount;
    const segOff = this.serverSegmentSamples.length - pairedCount;
    const rOff = this.renderSamples.length - pairedCount;

    // Compute event loop lag percentile from receiveLagSamples (or fallback to lagSamples)
    let eventLoopLag = 0;
    if (this.receiveLagSamples.length > 0) {
      const lagStats = this.computeStats(this.receiveLagSamples);
      if (lagStats) {
        eventLoopLag = level === 'p50' ? lagStats.median : lagStats.p99;
      }
    } else if (this.lagSamples.length > 0) {
      const lagStats = this.computeStats(this.lagSamples);
      if (lagStats) {
        eventLoopLag = level === 'p50' ? lagStats.median : lagStats.p99;
      }
    }

    // Build per-keystroke full breakdown tuples and sort by clientRTT
    type FullTuple = {
      clientRTT: number;
      dispatch: number;
      sendKeys: number;
      echo: number;
      frameSend: number;
      render: number;
      eventLoopLag: number;
      wireResidual: number;
      mutexWait?: number;
      executeNet?: number;
    };
    const tuples: FullTuple[] = [];
    for (let i = 0; i < pairedCount; i++) {
      const clientRTT = this.samples[sOff + i];
      const seg = this.serverSegmentSamples[segOff + i];
      const render = this.renderSamples[rOff + i];
      const serverTotal = seg.dispatch + seg.sendKeys + seg.echo + seg.frameSend;
      // Invariant: server processing can't exceed the round trip. If it does,
      // the FIFO queue paired this input with the wrong output event — discard.
      if (serverTotal > clientRTT) continue;
      const infra = Math.max(0, clientRTT - serverTotal - render);
      const wireResidual = Math.max(0, infra - eventLoopLag);
      tuples.push({
        clientRTT,
        dispatch: seg.dispatch,
        sendKeys: seg.sendKeys,
        echo: seg.echo,
        frameSend: seg.frameSend,
        render,
        eventLoopLag: Math.min(eventLoopLag, infra),
        wireResidual,
        mutexWait: seg.mutexWait,
        executeNet: seg.executeNet,
      });
    }
    if (tuples.length < 3) return null;

    // Use the histogram's percentile (from ALL samples) as the target RTT,
    // then find the closest paired tuple. This keeps the breakdown total
    // consistent with the histogram P50/P99 labels.
    const stats = this.getStats();
    if (!stats) return null;
    const targetRTT = level === 'p50' ? stats.median : stats.p99;

    let picked = tuples[0];
    let bestDist = Math.abs(tuples[0].clientRTT - targetRTT);
    for (let i = 1; i < tuples.length; i++) {
      const dist = Math.abs(tuples[i].clientRTT - targetRTT);
      if (dist < bestDist) {
        bestDist = dist;
        picked = tuples[i];
      }
    }

    return {
      dispatch: picked.dispatch,
      sendKeys: picked.sendKeys,
      echo: picked.echo,
      frameSend: picked.frameSend,
      eventLoopLag: picked.eventLoopLag,
      wireResidual: picked.wireResidual,
      render: picked.render,
      total: picked.clientRTT,
      mutexWait: picked.mutexWait,
      executeNet: picked.executeNet,
    };
  }
}

export const inputLatency = new InputLatencyTracker();

// Expose for Playwright benchmarks (scenario tests run against production builds)
if (typeof window !== 'undefined') {
  window.__inputLatency = inputLatency;
}
