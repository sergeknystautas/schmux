import { projectRoot } from '../exec.js';
import { parsePlaywrightLine } from '../parsers.js';
import {
  isDockerAvailable,
  ensureBaseImage,
  imageExists,
  buildImage,
  runContainer,
  removeImage,
} from '../docker.js';
import { buildLocalArtifacts, buildDashboard } from './shared.js';
import type { Options, EventCallback, SuiteResult, FailedTest } from '../types.js';
import { resolve } from 'node:path';
import { rmSync, mkdirSync } from 'node:fs';

const BASE_TAG = 'schmux-scenarios-base';

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
      status: 'broken',
      message: 'Docker is not installed or not running',
    });
    return makeResult(
      'broken',
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
  const artifactsBuild = await buildLocalArtifacts(onEvent);
  if (!artifactsBuild.ok) {
    return makeResult(
      'broken',
      performance.now() - startTime,
      [],
      [],
      [],
      {},
      artifactsBuild.errorOutput || 'Failed to build local artifacts'
    );
  }

  // Build dashboard (needed for scenarios, not for E2E)
  const dashboardBuild = await buildDashboard(onEvent);
  if (!dashboardBuild.ok) {
    return makeResult(
      'broken',
      performance.now() - startTime,
      [],
      [],
      [],
      {},
      dashboardBuild.errorOutput || 'Failed to build dashboard'
    );
  }

  // Track whether the base image was reused from cache
  const baseCached = !opts.force && (await imageExists(BASE_TAG));

  // Ensure base image
  if (
    !(await ensureBaseImage({
      tag: BASE_TAG,
      dockerfile: 'Dockerfile.scenarios-base',
      label: 'Scenario',
      force: opts.force,
      verbose: opts.verbose,
      onEvent,
      suite: 'scenarios',
    }))
  ) {
    return makeResult(
      'broken',
      performance.now() - startTime,
      [],
      [],
      [],
      {},
      'Failed to build Scenario base image'
    );
  }

  // Build ephemeral image + run container (with auto-retry on stale base image)
  const result = await buildAndRun(opts, onEvent, imageTag, artifactsDir);
  if (!result) {
    return makeResult(
      'broken',
      performance.now() - startTime,
      [],
      [],
      [],
      {},
      'Failed to build scenario test image'
    );
  }

  // Auto-retry: if all tests failed (or none ran) and the base image was cached,
  // rebuild base and retry once — a stale base image can cause total failure
  if (
    result.status === 'failed' &&
    result.passedTests.length === 0 &&
    baseCached &&
    !opts.runPattern
  ) {
    const reason =
      result.failedTests.length === 0
        ? '0 tests ran'
        : `all ${result.failedTests.length} tests failed`;
    onEvent('scenarios', {
      type: 'build_step',
      message: `${reason} — rebuilding base image and retrying...`,
    });

    await removeImage(BASE_TAG).catch(() => {});
    if (
      !(await ensureBaseImage({
        tag: BASE_TAG,
        dockerfile: 'Dockerfile.scenarios-base',
        label: 'Scenario',
        force: true,
        verbose: opts.verbose,
        onEvent,
        suite: 'scenarios',
      }))
    ) {
      return makeResult(
        'broken',
        performance.now() - startTime,
        [],
        [],
        [],
        {},
        'Failed to rebuild Scenario base image'
      );
    }

    // Clean artifacts for retry
    rmSync(artifactsDir, { recursive: true, force: true });
    mkdirSync(artifactsDir, { recursive: true });

    const retryResult = await buildAndRun(opts, onEvent, imageTag, artifactsDir);
    if (!retryResult) {
      return makeResult(
        'broken',
        performance.now() - startTime,
        [],
        [],
        [],
        {},
        'Failed to build scenario test image on retry'
      );
    }

    return { ...retryResult, durationMs: performance.now() - startTime };
  }

  return { ...result, durationMs: performance.now() - startTime };
}

async function buildAndRun(
  opts: Options,
  onEvent: EventCallback,
  imageTag: string,
  artifactsDir: string
): Promise<SuiteResult | null> {
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
      status: 'broken',
      message: 'Failed to build scenario test image',
    });
    return null;
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
      0, // durationMs filled by caller
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
  status: 'passed' | 'failed' | 'broken',
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
