import { exec, projectRoot } from '../exec.js';
import { parseGoTestLine, GoTestOutputAccumulator } from '../parsers.js';
import { analyzeGoCoverage } from '../coverage.js';
import type { Options, EventCallback, SuiteResult, FailedTest } from '../types.js';
import { readFileSync } from 'node:fs';
import { resolve } from 'node:path';
import type { CoverageReport } from '../coverage.js';

export async function run(opts: Options, onEvent: EventCallback): Promise<SuiteResult> {
  onEvent('backend', {
    type: 'suite_status',
    status: 'running',
    message: 'Running backend tests...',
  });

  const root = projectRoot();

  // Read module path from go.mod
  const goMod = readFileSync(resolve(root, 'go.mod'), 'utf-8');
  const moduleLine = goMod.match(/^module\s+(\S+)/m);
  if (!moduleLine) {
    onEvent('backend', {
      type: 'suite_status',
      status: 'broken',
      message: 'Could not parse module path from go.mod',
    });
    return makeResult('broken', 0, [], [], [], {}, '');
  }
  const modulePath = moduleLine[1];

  // Get package list, excluding /e2e$
  const listResult = await exec({
    cmd: 'go',
    args: ['list', `${modulePath}/...`],
    cwd: root,
  });
  if (listResult.exitCode !== 0) {
    onEvent('backend', { type: 'suite_status', status: 'broken', message: 'go list failed' });
    return makeResult('broken', listResult.durationMs, [], [], [], {}, listResult.stderr);
  }

  const packages = listResult.stdout
    .split('\n')
    .map((l) => l.trim())
    .filter((l) => l && !l.match(/\/e2e$/));

  // Untagged invocation: full backend test suite.
  const untaggedArgs = ['test', '-short', '-v', ...packages];
  if (opts.repeat > 1) {
    untaggedArgs.push(`-count=${opts.repeat}`);
  } else if (opts.noCache) {
    untaggedArgs.push('-count=1');
  }
  if (opts.race) untaggedArgs.push('-race');
  if (opts.coverage) untaggedArgs.push('-coverprofile=coverage.out', '-covermode=atomic');
  if (opts.runPattern) untaggedArgs.push('-run', opts.runPattern);

  // Vendorlocked invocation: only runs the vendorlocked-specific tests
  // (TestVendorLocked* / TestWarnVendorLocked*) in the three packages that
  // have them. The -run filter is for SPEED — without it we'd re-execute
  // ~9 seconds of pre-lock tests that all self-skip. The self-skip helpers
  // (skipUnderVendorlocked) still exist as defense-in-depth so a developer
  // running raw `go test -tags=vendorlocked ./...` gets graceful skips
  // rather than failures. All four tags are required together —
  // internal/buildflags/vendor_combo_check.go enforces this at compile time.
  const vendorlockedArgs = [
    'test',
    '-tags=nogithub notunnel nodashboardsx vendorlocked',
    '-short',
    '-v',
    '-run',
    '^(TestVendorLocked|TestWarnVendorLocked)',
    `${modulePath}/internal/buildflags/...`,
    `${modulePath}/internal/config/...`,
    `${modulePath}/internal/dashboard/...`,
  ];
  if (opts.repeat > 1) {
    vendorlockedArgs.push(`-count=${opts.repeat}`);
  } else if (opts.noCache) {
    vendorlockedArgs.push('-count=1');
  }
  if (opts.race) vendorlockedArgs.push('-race');

  const passedTests: string[] = [];
  const failedTests: FailedTest[] = [];
  const skippedTests: string[] = [];
  const testDurations: Record<string, number> = {};
  const outputLines: string[] = [];

  const runInvocation = async (args: string[], rerunPrefix: string) => {
    const accumulator = new GoTestOutputAccumulator();
    return exec({
      cmd: 'go',
      args,
      cwd: root,
      onLine: (line) => {
        outputLines.push(line);
        accumulator.feedLine(line);

        const event = parseGoTestLine(line, 0);
        if (!event) {
          if (opts.verbose) {
            onEvent('backend', { type: 'output_line', line });
          }
          return;
        }

        switch (event.type) {
          case 'test_pass':
            passedTests.push(event.name);
            testDurations[event.name] = Math.max(testDurations[event.name] ?? 0, event.durationMs);
            onEvent('backend', event);
            break;
          case 'test_fail':
            event.output = accumulator.getFailureOutput(event.name);
            failedTests.push({
              name: event.name,
              output: event.output,
              rerunCommand: `${rerunPrefix} ${event.name}`,
            });
            testDurations[event.name] = Math.max(testDurations[event.name] ?? 0, event.durationMs);
            onEvent('backend', event);
            break;
          case 'test_skip':
            skippedTests.push(event.name);
            break;
          default:
            onEvent('backend', event);
        }
      },
    });
  };

  const untaggedResult = await runInvocation(untaggedArgs, './test.sh --backend --run');
  const vendorlockedResult = await runInvocation(
    vendorlockedArgs,
    `go test -tags=vendorlocked -run`
  );

  const status =
    untaggedResult.exitCode === 0 && vendorlockedResult.exitCode === 0 ? 'passed' : 'failed';
  const totalDuration = untaggedResult.durationMs + vendorlockedResult.durationMs;

  // Coverage analysis (untagged invocation only — coverage is not requested for vendorlocked).
  let coverageReport;
  if (opts.coverage && untaggedResult.exitCode === 0) {
    coverageReport = await analyzeGoCoverage(resolve(root, 'coverage.out'), root);
  }

  onEvent('backend', {
    type: 'suite_status',
    status: status === 'passed' ? 'passed' : 'failed',
    message: status === 'passed' ? 'Backend tests passed' : 'Backend tests failed',
  });

  return makeResult(
    status,
    totalDuration,
    passedTests,
    failedTests,
    skippedTests,
    testDurations,
    outputLines.join('\n'),
    coverageReport
  );
}

function makeResult(
  status: 'passed' | 'failed' | 'broken',
  durationMs: number,
  passedTests: string[],
  failedTests: FailedTest[],
  skippedTests: string[],
  testDurations: Record<string, number>,
  output: string,
  coverageReport?: CoverageReport
): SuiteResult {
  return {
    suite: 'backend',
    status,
    durationMs,
    passedTests,
    failedTests,
    skippedTests,
    testDurations,
    output,
    coverageReport,
  };
}
