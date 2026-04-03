// Singleton store for tmux health probe data received via stats WebSocket messages.
// Updated by terminalStream, read by TmuxDiagnostic component.

export type TmuxHealthData = {
  samples: number[]; // RTT values in microseconds
  p50_us: number;
  p99_us: number;
  max_rtt_us: number;
  count: number;
  errors: number;
  last_us: number;
  uptime_s: number;
};

export type TmuxHealthDistribution = {
  buckets: number[];
  maxCount: number;
  maxUs: number; // upper bound in microseconds
  bucketUs: number; // width of each bucket in microseconds
};

// Compute histogram distribution from raw samples (microseconds).
// Uses 10μs bucket granularity, same adaptive approach as TypingPerformance.
export function computeDistribution(data: TmuxHealthData): TmuxHealthDistribution | null {
  if (data.samples.length < 3) return null;
  const sorted = [...data.samples].sort((a, b) => a - b);
  const p99 = sorted[Math.floor(sorted.length * 0.99)];
  const bucketUs = 10; // 10μs granularity
  const maxUs = Math.max(Math.ceil((p99 * 1.1) / bucketUs) * bucketUs, 100); // at least 100μs range
  const numBuckets = Math.round(maxUs / bucketUs);
  const buckets = new Array(numBuckets).fill(0);
  for (const v of data.samples) {
    const idx = Math.min(Math.floor(v / bucketUs), numBuckets - 1);
    buckets[Math.max(idx, 0)]++;
  }
  let maxCount = 0;
  for (const c of buckets) if (c > maxCount) maxCount = c;
  return { buckets, maxCount, maxUs, bucketUs };
}

let _latestHealth: TmuxHealthData | null = null;
let _version = 0;
let _machineKey = 'local';
const _machineSnapshots = new Map<string, TmuxHealthData | null>();

export function switchTmuxHealthMachine(key: string) {
  if (key === _machineKey) return;
  _machineSnapshots.set(_machineKey, _latestHealth);
  _machineKey = key;
  _latestHealth = _machineSnapshots.get(key) ?? null;
  _version++;
}

export function updateTmuxHealth(data: TmuxHealthData | null) {
  _latestHealth = data;
  _version++;
}

export function getTmuxHealth(): TmuxHealthData | null {
  return _latestHealth;
}

export function getTmuxHealthVersion(): number {
  return _version;
}
