import type { BrowserBenchEnvironment, BrowserBenchReport, BrowserBenchResult } from './types.js';

export const REQUIRED_BENCH_VARIANTS = ['idle', 'stressed'] as const;
export const EXPECTED_BROWSER_BENCH_ITERATIONS = 30;

/**
 * Parse a "BENCH_RESULT_JSON: {...}" line emitted by a browser benchmark spec
 * running inside the bench container. Returns null for any line that is not a
 * complete, well-formed BrowserTypingLatency result.
 */
export function parseBenchResultLine(line: string): BrowserBenchResult | null {
  const match = line.match(/^BENCH_RESULT_JSON: (\{.*\})\s*$/);
  if (!match) return null;

  let parsed: unknown;
  try {
    parsed = JSON.parse(match[1]);
  } catch {
    return null;
  }
  if (typeof parsed !== 'object' || parsed === null) return null;

  const r = parsed as Record<string, unknown>;
  const num = (v: unknown): v is number => typeof v === 'number' && Number.isFinite(v) && v >= 0;
  const percentileFields = [
    'iterations',
    'p50_ms',
    'p95_ms',
    'p99_ms',
    'max_ms',
    'mean_ms',
  ] as const;

  if (r.name !== 'BrowserTypingLatency') return null;
  if (r.variant !== 'idle' && r.variant !== 'stressed') return null;
  const percentileValues: Record<(typeof percentileFields)[number], number> = {
    iterations: 0,
    p50_ms: 0,
    p95_ms: 0,
    p99_ms: 0,
    max_ms: 0,
    mean_ms: 0,
  };
  for (const field of percentileFields) {
    const v = r[field];
    if (!num(v)) return null;
    percentileValues[field] = v;
  }
  if (!Number.isInteger(percentileValues.iterations)) return null;
  if (typeof r.timestamp !== 'string') return null;
  if (!num(r.nproc)) return null;
  if (typeof r.userAgent !== 'string') return null;

  return {
    name: 'BrowserTypingLatency',
    variant: r.variant,
    iterations: percentileValues.iterations,
    p50_ms: percentileValues.p50_ms,
    p95_ms: percentileValues.p95_ms,
    p99_ms: percentileValues.p99_ms,
    max_ms: percentileValues.max_ms,
    mean_ms: percentileValues.mean_ms,
    timestamp: r.timestamp,
    nproc: r.nproc,
    userAgent: r.userAgent,
  };
}

/**
 * A valid sample set contains every required variant exactly once and the full
 * configured sample count. Anything else is an execution error, never a clean
 * result.
 */
export function validateBrowserBenchResults(results: BrowserBenchResult[]): {
  valid: boolean;
  problems: string[];
} {
  const problems: string[] = [];
  for (const variant of REQUIRED_BENCH_VARIANTS) {
    const matches = results.filter((r) => r.variant === variant);
    if (matches.length === 0) {
      problems.push(`missing variant "${variant}"`);
    } else if (matches.length > 1) {
      problems.push(`variant "${variant}" appears ${matches.length} times`);
    } else if (matches[0].iterations !== EXPECTED_BROWSER_BENCH_ITERATIONS) {
      problems.push(
        `variant "${variant}" recorded ${matches[0].iterations} samples; ` +
          `expected ${EXPECTED_BROWSER_BENCH_ITERATIONS}`
      );
    } else if (!(
      matches[0].p50_ms <= matches[0].p95_ms &&
      matches[0].p95_ms <= matches[0].p99_ms &&
      matches[0].p99_ms <= matches[0].max_ms
    )) {
      problems.push(`variant "${variant}" has non-monotonic percentile values`);
    }
  }
  return { valid: problems.length === 0, problems };
}

/** Merge spec-side results with host/container metadata into the canonical report. */
export function buildBrowserBenchReport(
  results: BrowserBenchResult[],
  env: BrowserBenchEnvironment
): BrowserBenchReport {
  const first = results[0];
  return {
    profile: 'docker-scenario',
    generatedAt: new Date().toISOString(),
    gitCommit: env.gitCommit,
    host: {
      os: env.hostOs,
      arch: env.hostArch,
      cpuModel: env.hostCpuModel,
      cpuCount: env.hostCpuCount,
    },
    container: {
      runtime: env.runtime,
      baseImage: env.baseImage,
      image: env.image,
      nproc: first ? first.nproc : null,
      chromiumUserAgent: first ? first.userAgent : null,
      // No --cpus pinning is applied; recorded explicitly so reports stay honest.
      cpusPinned: false,
    },
    variants: results.map(
      ({ name: _name, nproc: _nproc, userAgent: _ua, timestamp: _ts, ...rest }) => rest
    ),
  };
}
