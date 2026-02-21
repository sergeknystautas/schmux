import { projectRoot } from '../exec.js';
import { parsePlaywrightLine } from '../parsers.js';
import {
  isDockerAvailable,
  ensureBaseImage,
  buildImage,
  runContainer,
  removeImage,
} from '../docker.js';
import { buildLocalArtifacts, buildDashboard } from './shared.js';
import type { Options, EventCallback, SuiteResult, FailedTest } from '../types.js';
import { resolve } from 'node:path';
import { rmSync, mkdirSync } from 'node:fs';

export async function run(opts: Options, onEvent: EventCallback): Promise<SuiteResult> {
  const startTime = performance.now();

  onEvent('scenarios', {
    type: 'suite_status',
    status: 'running',
    message: 'Running scenario tests...',
  });

  if (!(await isDockerAvailable())) {
    onEvent('scenarios', {
      type: 'suite_status',
      status: 'failed',
      message: 'Docker is not installed or not running',
    });
    return makeResult(
      'failed',
      performance.now() - startTime,
      [],
      [],
      [],
      {},
      'Docker is not available'
    );
  }

  const root = projectRoot();
  const imageTag = `schmux-scenarios-${process.pid}`;
  const artifactsDir = resolve(root, 'test/scenarios/artifacts');

  // Clean and create artifacts directory
  rmSync(artifactsDir, { recursive: true, force: true });
  mkdirSync(artifactsDir, { recursive: true });

  // Build local artifacts
  onEvent('scenarios', {
    type: 'suite_status',
    status: 'building',
    message: 'Building local artifacts...',
  });
  if (!(await buildLocalArtifacts(onEvent))) {
    return makeResult(
      'failed',
      performance.now() - startTime,
      [],
      [],
      [],
      {},
      'Failed to build local artifacts'
    );
  }

  // Build dashboard (needed for scenarios, not for E2E)
  if (!(await buildDashboard(onEvent))) {
    return makeResult(
      'failed',
      performance.now() - startTime,
      [],
      [],
      [],
      {},
      'Failed to build dashboard'
    );
  }

  // Ensure base image
  if (
    !(await ensureBaseImage({
      tag: 'schmux-scenarios-base',
      dockerfile: 'Dockerfile.scenarios-base',
      label: 'Scenario',
      force: opts.force,
      verbose: opts.verbose,
      onEvent,
      suite: 'scenarios',
    }))
  ) {
    return makeResult(
      'failed',
      performance.now() - startTime,
      [],
      [],
      [],
      {},
      'Failed to build Scenario base image'
    );
  }

  // Build ephemeral image
  onEvent('scenarios', { type: 'build_step', message: 'Building scenario test image...' });
  if (
    !(await buildImage({
      dockerfile: 'Dockerfile.scenarios',
      tag: imageTag,
      verbose: opts.verbose,
      onEvent,
      suite: 'scenarios',
    }))
  ) {
    onEvent('scenarios', {
      type: 'suite_status',
      status: 'failed',
      message: 'Failed to build scenario test image',
    });
    return makeResult(
      'failed',
      performance.now() - startTime,
      [],
      [],
      [],
      {},
      'Failed to build scenario test image'
    );
  }
  onEvent('scenarios', { type: 'build_step', message: 'Scenario test image built' });

  // Run container
  onEvent('scenarios', {
    type: 'suite_status',
    status: 'running',
    message: 'Running Playwright scenario tests in container...',
  });

  const passedTests: string[] = [];
  const failedTests: FailedTest[] = [];
  const skippedTests: string[] = [];
  const testDurations: Record<string, number> = {};

  const env: Record<string, string> = {};
  if (opts.runPattern) {
    env['TEST_GREP'] = opts.runPattern;
  }
  if (opts.repeat > 1) {
    env['TEST_REPEAT'] = String(opts.repeat);
  }

  try {
    const containerResult = await runContainer({
      tag: imageTag,
      env: Object.keys(env).length > 0 ? env : undefined,
      volumes: [`${artifactsDir}:/artifacts`],
      onLine: (line) => {
        const event = parsePlaywrightLine(line);
        if (!event) {
          if (opts.verbose) {
            onEvent('scenarios', { type: 'output_line', line });
          }
          return;
        }

        switch (event.type) {
          case 'test_pass':
            passedTests.push(event.name);
            testDurations[event.name] = Math.max(testDurations[event.name] ?? 0, event.durationMs);
            onEvent('scenarios', event);
            break;
          case 'test_fail':
            failedTests.push({
              name: event.name,
              output: event.output ?? '',
              rerunCommand: `./test.sh --scenarios --run '${event.name}'`,
            });
            onEvent('scenarios', event);
            break;
          case 'test_skip':
            skippedTests.push(event.name);
            break;
          default:
            onEvent('scenarios', event);
        }
      },
    });

    const durationMs = performance.now() - startTime;
    const status = containerResult.exitCode === 0 ? 'passed' : 'failed';

    if (status === 'failed') {
      onEvent('scenarios', {
        type: 'output_line',
        line: `Test artifacts saved to: test/scenarios/artifacts/`,
      });
    }

    onEvent('scenarios', {
      type: 'suite_status',
      status: status === 'passed' ? 'passed' : 'failed',
      message: status === 'passed' ? 'Scenario tests passed' : 'Scenario tests failed',
    });

    return makeResult(
      status,
      durationMs,
      passedTests,
      failedTests,
      skippedTests,
      testDurations,
      containerResult.output
    );
  } finally {
    await removeImage(imageTag).catch(() => {});
  }
}

function makeResult(
  status: 'passed' | 'failed',
  durationMs: number,
  passedTests: string[],
  failedTests: FailedTest[],
  skippedTests: string[],
  testDurations: Record<string, number>,
  output: string
): SuiteResult {
  return {
    suite: 'scenarios',
    status,
    durationMs,
    passedTests,
    failedTests,
    skippedTests,
    testDurations,
    output,
  };
}
