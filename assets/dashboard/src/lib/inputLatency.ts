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
};

export type LatencyDistribution = {
  buckets: number[]; // count per 1ms bucket
  maxCount: number; // max count across buckets (for scaling)
  maxMs: number; // upper bound of the last bucket
};

class InputLatencyTracker {
  samples: number[] = [];
  renderSamples: number[] = [];
  private lastInputTime = 0;
  private version = 0;

  markSent() {
    this.lastInputTime = performance.now();
  }

  markReceived() {
    if (this.lastInputTime === 0) return;
    const rtt = performance.now() - this.lastInputTime;
    this.lastInputTime = 0;
    this.samples.push(rtt);
    if (this.samples.length > MAX_SAMPLES) {
      this.samples = this.samples.slice(-MAX_SAMPLES);
    }
    this.version++;
  }

  markRenderTime(ms: number) {
    this.renderSamples.push(ms);
    if (this.renderSamples.length > MAX_SAMPLES) {
      this.renderSamples = this.renderSamples.slice(-MAX_SAMPLES);
    }
  }

  getVersion() {
    return this.version;
  }

  private computeStats(arr: number[]): LatencyStats | null {
    if (arr.length === 0) return null;
    const sorted = [...arr].sort((a, b) => a - b);
    const sum = sorted.reduce((a, b) => a + b, 0);
    return {
      count: sorted.length,
      median: sorted[Math.floor(sorted.length / 2)],
      p95: sorted[Math.floor(sorted.length * 0.95)],
      p99: sorted[Math.floor(sorted.length * 0.99)],
      max: sorted[sorted.length - 1],
      avg: sum / sorted.length,
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
    return { buckets, maxCount: Math.max(...buckets), maxMs };
  }

  reset() {
    this.samples = [];
    this.renderSamples = [];
    this.lastInputTime = 0;
    this.version++;
  }
}

export const inputLatency = new InputLatencyTracker();

// Expose for Playwright benchmarks (harmless in production)
if (typeof window !== 'undefined') {
  (window as any).__inputLatency = inputLatency;
}
