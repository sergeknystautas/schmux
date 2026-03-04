import type { Options, SuiteResult, SuiteName, FlakyResult, EventCallback } from './types.js';
import { createProgressDisplay } from './ui.js';
import { buildLocalArtifacts, buildDashboard } from './suites/shared.js';
import { run as runBackend } from './suites/backend.js';
import { run as runFrontend } from './suites/frontend.js';
import { run as runE2E } from './suites/e2e.js';
import { run as runScenarios } from './suites/scenarios.js';
import { run as runBench } from './suites/bench.js';
import { run as runBenchMicro } from './suites/bench-micro.js';

type SuiteRunner = (opts: Options, onEvent: EventCallback) => Promise<SuiteResult>;

const runners: Record<SuiteName, SuiteRunner> = {
  backend: runBackend,
  frontend: runFrontend,
  e2e: runE2E,
  scenarios: runScenarios,
  bench: runBench,
  'bench-micro': runBenchMicro,
};

export interface RunResult {
  results: SuiteResult[];
  flakyResults: FlakyResult[];
}

export async function runSuites(opts: Options): Promise<RunResult> {
  let results: SuiteResult[];

  if (opts.suites.length > 1) {
    results = await runParallel(opts);
  } else {
    results = await runSerial(opts);
  }

  // Compute flaky results when repeat > 1 — each suite handles its own
  // repetition natively (go test -count=N, playwright --repeat-each, etc.),
  // so duplicate test names in a single run indicate flakiness.
  const flakyResults = opts.repeat > 1 ? computeFlakyResults(results, opts.repeat) : [];

  return { results, flakyResults };
}

// ─── Serial Mode ───────────────────────────────────────────────────────────

async function runSerial(opts: Options): Promise<SuiteResult[]> {
  const results: SuiteResult[] = [];

  for (const suite of opts.suites) {
    const display = createProgressDisplay([suite], false);
    const runner = runners[suite];

    const result = await runner(opts, (s, event) => {
      display.onEvent(s, event);
    });

    display.finish(result);
    display.stop();
    results.push(result);
  }

  return results;
}

// ─── Parallel Mode ─────────────────────────────────────────────────────────

async function runParallel(opts: Options): Promise<SuiteResult[]> {
  const display = createProgressDisplay(opts.suites, true);

  // Build prerequisites in parallel first
  const needsDocker = opts.suites.some((s) => s === 'e2e' || s === 'scenarios');
  const needsDashboard = opts.suites.includes('scenarios');

  const buildEvent: EventCallback = (suite, event) => {
    display.onEvent(suite, event);
  };

  const buildPromises: Promise<{ ok: boolean }>[] = [];

  if (needsDocker) {
    buildPromises.push(buildLocalArtifacts(buildEvent, opts.coverage));
  }
  if (needsDashboard) {
    buildPromises.push(buildDashboard(buildEvent, opts.coverage));
  }

  if (buildPromises.length > 0) {
    const buildResults = await Promise.all(buildPromises);
    if (buildResults.some((r) => !r.ok)) {
      display.stop();
      // Still return partial results — the suites that needed builds will fail
    }
  }

  // Run all suites concurrently
  const promises = opts.suites.map(async (suite) => {
    const runner = runners[suite];
    const result = await runner(opts, (s, event) => {
      display.onEvent(s, event);
    });
    display.finish(result);
    return result;
  });

  const settled = await Promise.allSettled(promises);
  display.stop();

  const results: SuiteResult[] = [];
  for (let i = 0; i < settled.length; i++) {
    const s = settled[i];
    if (s.status === 'fulfilled') {
      results.push(s.value);
    } else {
      results.push({
        suite: opts.suites[i],
        status: 'failed',
        durationMs: 0,
        passedTests: [],
        failedTests: [
          {
            name: 'runner_error',
            output: String(s.reason),
            rerunCommand: `./test.sh --${opts.suites[i]}`,
          },
        ],
        skippedTests: [],
        testDurations: {},
        output: String(s.reason),
      });
    }
  }

  return results;
}

// ─── Flaky Detection ──────────────────────────────────────────────────────
// With -count=N (go test) or --repeat-each=N (playwright), each test name
// appears multiple times in the results. Mixed pass/fail = flaky.

function computeFlakyResults(results: SuiteResult[], repeat: number): FlakyResult[] {
  const testHistory = new Map<
    string,
    { suite: SuiteName; passes: number; fails: number; rerunCommand: string }
  >();
  const repeatArg = ` --repeat ${repeat}`;

  for (const result of results) {
    for (const name of result.passedTests) {
      const key = `${result.suite}::${name}`;
      const entry = testHistory.get(key) ?? {
        suite: result.suite,
        passes: 0,
        fails: 0,
        rerunCommand: `./test.sh --${result.suite} --run ${name}${repeatArg}`,
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
        rerunCommand: `${ft.rerunCommand}${repeatArg}`,
      };
      entry.fails++;
      testHistory.set(key, entry);
    }
  }

  const flakyResults: FlakyResult[] = [];
  for (const [key, entry] of testHistory) {
    const testName = key.split('::').slice(1).join('::');
    const totalRuns = entry.passes + entry.fails;
    flakyResults.push({
      testName,
      suite: entry.suite,
      passCount: entry.passes,
      failCount: entry.fails,
      totalRuns,
      flakyScore: entry.fails / totalRuns,
      rerunCommand: entry.rerunCommand,
    });
  }

  return flakyResults;
}

// ─── Signal Handling ───────────────────────────────────────────────────────

export function setupSignalHandlers(): void {
  const cleanup = () => {
    process.exit(1);
  };

  process.on('SIGINT', cleanup);
  process.on('SIGTERM', cleanup);
}
