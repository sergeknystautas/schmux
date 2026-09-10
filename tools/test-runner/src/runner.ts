import type { Options, SuiteResult, SuiteName, FlakyResult, EventCallback } from './types.js';
import { createProgressDisplay } from './ui.js';
import { run as runBackend } from './suites/backend.js';
import { run as runFrontend } from './suites/frontend.js';
import { run as runE2E } from './suites/e2e.js';
import { run as runScenarios } from './suites/scenarios.js';
import { run as runBench } from './suites/bench.js';
import { run as runBenchMicro } from './suites/microbench.js';
import { frontendRerunCommand } from './parsers.js';
import {
  isCacheable,
  isCacheDisabled,
  computeCacheKey,
  checkCache,
  saveCache,
  deleteCache,
} from './cache.js';

type SuiteRunner = (opts: Options, onEvent: EventCallback) => Promise<SuiteResult>;

const runners: Record<SuiteName, SuiteRunner> = {
  backend: runBackend,
  frontend: runFrontend,
  e2e: runE2E,
  scenarios: runScenarios,
  bench: runBench,
  microbench: runBenchMicro,
};

async function runWithCache(
  suite: SuiteName,
  opts: Options,
  runner: SuiteRunner,
  onEvent: EventCallback
): Promise<SuiteResult> {
  if (isCacheable(suite) && !isCacheDisabled(opts)) {
    const cacheStart = Date.now();
    const key = await computeCacheKey(suite, opts);
    const { hit, entry, missReason } = checkCache(suite, key);

    if (hit && entry) {
      onEvent(suite, {
        type: 'suite_status',
        status: 'passed',
        message: `${suite} tests cached (${entry.testCount} passed)`,
      });
      return {
        suite,
        status: 'passed',
        durationMs: Date.now() - cacheStart,
        passedTests: entry.passedTests,
        failedTests: [],
        skippedTests: entry.skippedTests,
        testDurations: {},
        output: '',
        cached: true,
        cachedTimestamp: entry.timestamp,
      };
    }

    onEvent(suite, { type: 'build_step', message: `cache miss (${missReason})` });

    const result = await runner(opts, onEvent);

    if (result.status === 'passed') {
      saveCache(suite, key, result);
    } else {
      deleteCache(suite);
    }
    return result;
  }

  return runner(opts, onEvent);
}

export interface RunResult {
  results: SuiteResult[];
  flakyResults: FlakyResult[];
  incompleteSuites: SuiteName[];
}

export async function runSuites(opts: Options): Promise<RunResult> {
  let results: SuiteResult[];

  results = await runSerial(opts);

  // Compute flaky results when repeat > 1 — each suite handles its own
  // repetition natively (go test -count=N, N vitest processes, etc.),
  // so duplicate test names in a single run indicate flakiness.
  const { flakyResults, incompleteSuites } =
    opts.repeat > 1
      ? computeFlakyResults(results, opts.repeat)
      : { flakyResults: [], incompleteSuites: [] };

  return { results, flakyResults, incompleteSuites };
}

// ─── Serial Mode ───────────────────────────────────────────────────────────

async function runSerial(opts: Options): Promise<SuiteResult[]> {
  const results: SuiteResult[] = [];

  for (const suite of opts.suites) {
    const display = createProgressDisplay([suite], false);
    const runner = runners[suite];

    const result = await runWithCache(suite, opts, runner, (s, event) => {
      display.onEvent(s, event);
    });

    display.finish(result);
    display.stop();
    results.push(result);
  }

  return results;
}

// ─── Flaky Detection ──────────────────────────────────────────────────────
// With -count=N (go test) or N vitest processes, each test name appears
// multiple times in the results. Mixed pass/fail = flaky.
//
// Completeness and skip counting are FRONTEND-ONLY: backend identities are
// bare TestXxx names with 26 cross-package duplicates in this repo, so
// enforcing either there could produce false verdicts. Qualifying Go test
// identities is separate work.

export function computeFlakyResults(
  results: SuiteResult[],
  repeat: number
): { flakyResults: FlakyResult[]; incompleteSuites: SuiteName[] } {
  const testHistory = new Map<
    string,
    { suite: SuiteName; passes: number; fails: number; skips: number; rerunCommand: string }
  >();
  const repeatArg = ` --repeat ${repeat}`;

  for (const result of results) {
    const countSkips = result.suite === 'frontend';
    for (const name of result.passedTests) {
      const key = `${result.suite}::${name}`;
      const entry = testHistory.get(key) ?? {
        suite: result.suite,
        passes: 0,
        fails: 0,
        skips: 0,
        rerunCommand:
          result.suite === 'frontend'
            ? `${frontendRerunCommand(name)}${repeatArg}`
            : `./test.sh --${result.suite} --run ${name}${repeatArg}`,
      };
      entry.passes++;
      testHistory.set(key, entry);
    }
    for (const ft of result.failedTests) {
      const key = `${result.suite}::${ft.name}`;
      const entry = testHistory.get(key) ?? {
        suite: result.suite,
        passes: 0,
        fails: 0,
        skips: 0,
        rerunCommand: '',
      };
      // The failed occurrence's command was built from structured fields at
      // parse time (correct even when a title contains a literal ' > ');
      // it wins over the identity-derived fallback a passed entry may have set.
      entry.rerunCommand = `${ft.rerunCommand}${repeatArg}`;
      entry.fails++;
      testHistory.set(key, entry);
    }
    if (countSkips) {
      for (const name of result.skippedTests) {
        const key = `${result.suite}::${name}`;
        const entry = testHistory.get(key) ?? {
          suite: result.suite,
          passes: 0,
          fails: 0,
          skips: 0,
          rerunCommand: `${frontendRerunCommand(name)}${repeatArg}`,
        };
        entry.skips++;
        testHistory.set(key, entry);
      }
    }
  }

  // Completeness: every frontend test observed in a passed/failed suite must
  // have >= repeat observations (passes + fails + skips all count — a skip
  // is an observation, not missing evidence). Otherwise the suite is broken
  // and the flaky verdict is withheld.
  const incompleteSuites: SuiteName[] = [];
  for (const result of results) {
    if (result.suite !== 'frontend') continue;
    if (result.status !== 'passed' && result.status !== 'failed') continue;
    const observations = new Map<string, number>();
    const count = (name: string) => observations.set(name, (observations.get(name) ?? 0) + 1);
    for (const n of result.passedTests) count(n);
    for (const f of result.failedTests) count(f.name);
    for (const n of result.skippedTests) count(n);
    let incomplete = false;
    for (const n of observations.values()) {
      if (n < repeat) {
        incomplete = true;
        break;
      }
    }
    if (incomplete) {
      result.status = 'broken';
      if (!incompleteSuites.includes(result.suite)) incompleteSuites.push(result.suite);
    }
  }

  const flakyResults: FlakyResult[] = [];
  for (const [key, entry] of testHistory) {
    const testName = key.split('::').slice(1).join('::');
    const totalRuns = entry.passes + entry.fails + entry.skips;
    flakyResults.push({
      testName,
      suite: entry.suite,
      passCount: entry.passes,
      failCount: entry.fails,
      skipCount: entry.skips,
      totalRuns,
      flakyScore: entry.fails / totalRuns,
      rerunCommand: entry.rerunCommand,
    });
  }

  return { flakyResults, incompleteSuites };
}

// ─── Signal Handling ───────────────────────────────────────────────────────

export function setupSignalHandlers(): void {
  const cleanup = () => {
    process.exit(1);
  };

  process.on('SIGINT', cleanup);
  process.on('SIGTERM', cleanup);
}
