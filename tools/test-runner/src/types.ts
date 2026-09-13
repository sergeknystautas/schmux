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
