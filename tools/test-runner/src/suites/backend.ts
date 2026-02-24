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

  // Build go test args — always use -v to get individual test results for live streaming
  const args = ['test', '-short', '-v', ...packages];

  if (opts.repeat > 1) {
    args.push(`-count=${opts.repeat}`);
  } else if (opts.noCache) {
    args.push('-count=1');
  }
  if (opts.race) args.push('-race');
  if (opts.coverage) args.push('-coverprofile=coverage.out', '-covermode=atomic');
  if (opts.runPattern) args.push('-run', opts.runPattern);

  const accumulator = new GoTestOutputAccumulator();
  const passedTests: string[] = [];
  const failedTests: FailedTest[] = [];
  const skippedTests: string[] = [];
  const testDurations: Record<string, number> = {};
  const outputLines: string[] = [];

  const result = await exec({
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
          // Attach accumulated output to the failure
          event.output = accumulator.getFailureOutput(event.name);
          failedTests.push({
            name: event.name,
            output: event.output,
            rerunCommand: `./test.sh --backend --run ${event.name}`,
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

  const status = result.exitCode === 0 ? 'passed' : 'failed';

  // Coverage analysis
  let coverageReport;
  if (opts.coverage && status === 'passed') {
    coverageReport = await analyzeGoCoverage(resolve(root, 'coverage.out'), root);
  }

  onEvent('backend', {
    type: 'suite_status',
    status: status === 'passed' ? 'passed' : 'failed',
    message: status === 'passed' ? 'Backend tests passed' : 'Backend tests failed',
  });

  return makeResult(
    status,
    result.durationMs,
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
