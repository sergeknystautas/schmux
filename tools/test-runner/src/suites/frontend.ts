import { exec, projectRoot } from '../exec.js';
import { parseVitestLine } from '../parsers.js';
import { parseVitestCoverage } from '../coverage.js';
import type { Options, EventCallback, SuiteResult, FailedTest } from '../types.js';
import type { FrontendCoverageReport } from '../coverage.js';
import { existsSync } from 'node:fs';
import { resolve } from 'node:path';

export async function run(opts: Options, onEvent: EventCallback): Promise<SuiteResult> {
  onEvent('frontend', {
    type: 'suite_status',
    status: 'running',
    message: 'Running frontend tests...',
  });

  const root = projectRoot();
  const dashboardDir = resolve(root, 'assets/dashboard');

  // Ensure node_modules exists
  if (!existsSync(resolve(dashboardDir, 'node_modules'))) {
    onEvent('frontend', { type: 'build_step', message: 'Installing dashboard dependencies...' });
    const install = await exec({
      cmd: 'npm',
      args: ['ci', '--silent'],
      cwd: dashboardDir,
    });
    if (install.exitCode !== 0) {
      onEvent('frontend', { type: 'suite_status', status: 'broken', message: 'npm ci failed' });
      return makeResult('broken', install.durationMs, [], [], [], {}, install.stderr);
    }
  }

  const args = ['vitest', 'run'];
  if (opts.coverage) args.push('--coverage');

  const passedTests: string[] = [];
  const failedTests: FailedTest[] = [];
  const skippedTests: string[] = [];
  const testDurations: Record<string, number> = {};
  const outputLines: string[] = [];
  let totalTestCount = 0;

  const result = await exec({
    cmd: 'npx',
    args,
    cwd: dashboardDir,
    onLine: (line) => {
      outputLines.push(line);

      const event = parseVitestLine(line);
      if (!event) {
        if (opts.verbose) {
          onEvent('frontend', { type: 'output_line', line });
        }
        return;
      }

      switch (event.type) {
        case 'test_pass': {
          passedTests.push(event.name);
          testDurations[event.name] = Math.max(testDurations[event.name] ?? 0, event.durationMs);
          // Extract individual test count from pkg field (e.g. "7 tests")
          const countMatch = event.pkg?.match(/^(\d+)/);
          if (countMatch) {
            totalTestCount += parseInt(countMatch[1], 10);
          } else {
            totalTestCount++;
          }
          onEvent('frontend', event);
          break;
        }
        case 'test_fail':
          failedTests.push({
            name: event.name,
            output: '',
            rerunCommand: `./test.sh --frontend`,
          });
          testDurations[event.name] = Math.max(testDurations[event.name] ?? 0, event.durationMs);
          onEvent('frontend', event);
          break;
        case 'test_skip':
          skippedTests.push(event.name);
          break;
        default:
          onEvent('frontend', event);
      }
    },
  });

  const status = result.exitCode === 0 ? 'passed' : 'failed';

  // Parse coverage if enabled and tests passed
  let frontendCoverageReport: FrontendCoverageReport | undefined;
  if (opts.coverage && status === 'passed') {
    const parsed = parseVitestCoverage(outputLines.join('\n'));
    if (parsed) {
      frontendCoverageReport = parsed;
    }
  }

  onEvent('frontend', {
    type: 'suite_status',
    status: status === 'passed' ? 'passed' : 'failed',
    message: status === 'passed' ? 'Frontend tests passed' : 'Frontend tests failed',
  });

  // Use totalTestCount for passedTests if we got counts from vitest
  const expandedPassedTests =
    totalTestCount > passedTests.length
      ? Array.from({ length: totalTestCount }, (_, i) => passedTests[i] ?? `test_${i + 1}`)
      : passedTests;

  return makeResult(
    status,
    result.durationMs,
    expandedPassedTests,
    failedTests,
    skippedTests,
    testDurations,
    outputLines.join('\n'),
    frontendCoverageReport
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
  frontendCoverageReport?: FrontendCoverageReport
): SuiteResult {
  return {
    suite: 'frontend',
    status,
    durationMs,
    passedTests,
    failedTests,
    skippedTests,
    testDurations,
    output,
    frontendCoverageReport,
  };
}
