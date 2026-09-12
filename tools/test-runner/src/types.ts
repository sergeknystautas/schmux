import type { CoverageReport, FrontendCoverageReport } from './coverage.js';

export type SuiteName = 'backend' | 'frontend' | 'e2e' | 'scenarios' | 'bench' | 'microbench';
export type SuiteStatus =
  'pending' | 'building' | 'running' | 'passed' | 'failed' | 'broken' | 'skipped';

// Events emitted by suite runners as tests execute
export type TestEvent =
  | { type: 'test_pass'; name: string; durationMs: number; pkg?: string }
  | { type: 'test_fail'; name: string; durationMs: number; output: string; pkg?: string }
  | { type: 'test_skip'; name: string }
  | { type: 'suite_status'; status: SuiteStatus; message: string }
  | { type: 'build_step'; message: string }
  | { type: 'output_line'; line: string };

export interface SuiteResult {
  suite: SuiteName;
  status: 'passed' | 'failed' | 'broken' | 'skipped';
  durationMs: number;
  passedTests: string[];
  failedTests: FailedTest[];
  skippedTests: string[];
  testDurations: Record<string, number>; // all individual test durations
  output: string;
  cached?: boolean;
  cachedTimestamp?: string; // ISO timestamp of the cached run
  coverageReport?: CoverageReport;
  frontendCoverageReport?: FrontendCoverageReport;
}

export interface FailedTest {
  name: string;
  output: string;
  rerunCommand: string;
}

// Browser typing benchmark results, parsed from BENCH_RESULT_JSON lines
// emitted by test/scenarios/generated/*.bench.spec.ts inside the bench container.
export interface BrowserBenchResult {
  name: 'BrowserTypingLatency';
  variant: 'idle' | 'stressed';
  iterations: number;
  p50_ms: number;
  p95_ms: number;
  p99_ms: number;
  max_ms: number;
  mean_ms: number;
  timestamp: string;
  nproc: number;
  userAgent: string;
}

/** Host/container metadata gathered by the bench suite when the report is written. */
export interface BrowserBenchEnvironment {
  gitCommit: string;
  runtime: string;
  baseImage: string;
  image: string;
  hostOs: string;
  hostArch: string;
  hostCpuModel: string;
  hostCpuCount: number;
}

/** Canonical report written to bench-results/<date>/browser-typing-latency.json. */
export interface BrowserBenchReport {
  profile: 'docker-scenario';
  generatedAt: string;
  gitCommit: string;
  host: { os: string; arch: string; cpuModel: string; cpuCount: number };
  container: {
    runtime: string;
    baseImage: string;
    image: string;
    nproc: number | null;
    chromiumUserAgent: string | null;
    cpusPinned: false;
  };
  variants: Array<Omit<BrowserBenchResult, 'name' | 'nproc' | 'userAgent' | 'timestamp'>>;
}

// One Vitest JSON-reporter iteration, parsed to per-test results.
// Identities are file-qualified: `file > ancestors > title`.
export interface VitestRunDetail {
  passedTests: string[];
  failedTests: FailedTest[];
  skippedTests: string[];
  testDurations: Record<string, number>;
  integrityOk: boolean; // parsed assertion count === numTotalTests
  success: boolean; // top-level success flag, cross-checked vs exit code by the caller
  totals: { passed: number; failed: number; skipped: number; total: number };
}

export interface Options {
  suites: SuiteName[];
  all: boolean;
  race: boolean;
  verbose: boolean;
  coverage: boolean;
  force: boolean;
  noCache: boolean;
  quick: boolean;
  runPattern: string | null;
  repeat: number;
  serial: boolean;
  recordVideo: boolean;
  vitestShuffleSeed: number | null;
  verifyDetector: boolean;
}

// Callback for live events from a running suite
export type EventCallback = (suite: SuiteName, event: TestEvent) => void;

// Flaky detection results (populated when repeat > 1)
export interface FlakyResult {
  testName: string;
  suite: SuiteName;
  passCount: number;
  failCount: number;
  skipCount: number; // frontend only — skips are not counted for other suites
  totalRuns: number;
  flakyScore: number;
  rerunCommand: string;
}
