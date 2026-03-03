import { projectRoot } from '../exec.js';
import { parsePlaywrightLine } from '../parsers.js';
import {
  isDockerAvailable,
  ensureBaseImage,
  imageExists,
  buildImage,
  runContainer,
  removeImage,
  cleanupOrphans,
} from '../docker.js';
import { buildLocalArtifacts, buildDashboard } from './shared.js';
import type { Options, EventCallback, SuiteResult, FailedTest } from '../types.js';
import { resolve } from 'node:path';
import { rmSync, mkdirSync } from 'node:fs';
import { availableParallelism } from 'node:os';

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

  // Clean up orphaned containers/images from interrupted previous runs
  const orphans = await cleanupOrphans('scenarios');
  if (orphans > 0) {
    onEvent('scenarios', {
      type: 'build_step',
      message: `Cleaned up ${orphans} orphaned container(s)/image(s) from previous runs`,
    });
  }

  // Clean and create artifacts directory
  rmSync(artifactsDir, { recursive: true, force: true });
  mkdirSync(artifactsDir, { recursive: true });

  // Build local artifacts
  onEvent('scenarios', {
    type: 'suite_status',
    status: 'building',
    message: 'Building local artifacts...',
  });
  const artifactsBuild = await buildLocalArtifacts(onEvent, opts.coverage);
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
  const dashboardBuild = await buildDashboard(onEvent, opts.coverage);
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
  // Set up coverage directory if requested
  let covDataDir: string | undefined;
  if (opts.coverage) {
    covDataDir = resolve(root, 'build/covdata-scenarios');
    rmSync(covDataDir, { recursive: true, force: true });
    mkdirSync(covDataDir, { recursive: true });
  }

  try {
    const result = await buildAndRun(opts, onEvent, imageTag, artifactsDir, covDataDir);
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
      if (covDataDir) {
        rmSync(covDataDir, { recursive: true, force: true });
        mkdirSync(covDataDir, { recursive: true });
      }

      const retryResult = await buildAndRun(opts, onEvent, imageTag, artifactsDir, covDataDir);
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
  } finally {
    await removeImage(imageTag).catch(() => {});
  }
}

async function buildAndRun(
  opts: Options,
  onEvent: EventCallback,
  imageTag: string,
  artifactsDir: string,
  covDataDir?: string
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

  // When repeat > 1, run N containers in parallel instead of a single container
  // with --repeat-each. Each container is fully isolated (own daemon, Chromium,
  // Playwright) so parallelism is safe and uses idle CPUs.
  // Use --serial to force the old single-container behavior.
  if (opts.repeat > 1 && !opts.serial) {
    return runParallelContainers(opts, onEvent, imageTag, artifactsDir, covDataDir);
  }

  return runSingleContainer(opts, onEvent, imageTag, artifactsDir, covDataDir);
}

async function runSingleContainer(
  opts: Options,
  onEvent: EventCallback,
  imageTag: string,
  artifactsDir: string,
  covDataDir?: string
): Promise<SuiteResult> {
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
  const volumes: string[] = [`${artifactsDir}:/artifacts`];
  if (opts.runPattern) {
    env['TEST_GREP'] = opts.runPattern;
  }
  if (opts.serial && opts.repeat > 1) {
    env['TEST_REPEAT'] = String(opts.repeat);
  }
  if (covDataDir) {
    env['GOCOVERDIR'] = '/covdata';
    volumes.push(`${covDataDir}:/covdata`);
  }

  const containerResult = await runContainer({
    tag: imageTag,
    env: Object.keys(env).length > 0 ? env : undefined,
    volumes,
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
}

/** Run N containers in parallel, capped at CPU count, and merge results. */
async function runParallelContainers(
  opts: Options,
  onEvent: EventCallback,
  imageTag: string,
  artifactsDir: string,
  covDataDir?: string
): Promise<SuiteResult> {
  const total = opts.repeat;
  // Each container runs a daemon + Chromium + Playwright + tmux, needing ~5 CPUs
  // to avoid resource contention that causes timing-sensitive terminal tests to fail.
  const maxParallel = Math.min(total, Math.max(2, Math.floor(availableParallelism() / 5)));
  onEvent('scenarios', {
    type: 'suite_status',
    status: 'running',
    message: `Running ${total} scenario repeats in parallel (max ${maxParallel} concurrent containers)...`,
  });

  // Prepare per-run directories
  const runDirs: { artifacts: string; covData?: string }[] = [];
  for (let i = 0; i < total; i++) {
    const runArtifacts = resolve(artifactsDir, `run-${i}`);
    mkdirSync(runArtifacts, { recursive: true });
    let runCov: string | undefined;
    if (covDataDir) {
      runCov = resolve(covDataDir, `run-${i}`);
      mkdirSync(runCov, { recursive: true });
    }
    runDirs.push({ artifacts: runArtifacts, covData: runCov });
  }

  // Run containers in batches capped at maxParallel
  const containerResults: {
    passedTests: string[];
    failedTests: FailedTest[];
    skippedTests: string[];
    testDurations: Record<string, number>;
    exitCode: number;
    output: string;
  }[] = [];
  for (let batchStart = 0; batchStart < total; batchStart += maxParallel) {
    const batchEnd = Math.min(batchStart + maxParallel, total);
    const batchPromises = [];

    for (let i = batchStart; i < batchEnd; i++) {
      batchPromises.push(runRepeatContainer(opts, onEvent, imageTag, runDirs[i], i));
    }

    const batchResults = await Promise.all(batchPromises);
    containerResults.push(...batchResults);
  }

  // Merge results from all containers
  const mergedPassed: string[] = [];
  const mergedFailed: FailedTest[] = [];
  const mergedSkipped: string[] = [];
  const mergedDurations: Record<string, number> = {};
  const outputs: string[] = [];
  let anyFailed = false;

  for (const cr of containerResults) {
    mergedPassed.push(...cr.passedTests);
    mergedFailed.push(...cr.failedTests);
    mergedSkipped.push(...cr.skippedTests);
    for (const [name, dur] of Object.entries(cr.testDurations)) {
      mergedDurations[name] = Math.max(mergedDurations[name] ?? 0, dur);
    }
    if (cr.exitCode !== 0) anyFailed = true;
    outputs.push(cr.output);
  }

  // Deduplicate skipped tests (same tests skipped in every container)
  const uniqueSkipped = [...new Set(mergedSkipped)];

  const status = anyFailed ? 'failed' : 'passed';

  if (status === 'failed') {
    onEvent('scenarios', {
      type: 'output_line',
      line: `Test artifacts saved to: test/scenarios/artifacts/run-*/`,
    });
  }

  onEvent('scenarios', {
    type: 'suite_status',
    status,
    message: status === 'passed' ? 'Scenario tests passed' : 'Scenario tests failed',
  });

  return makeResult(
    status,
    0, // durationMs filled by caller
    mergedPassed,
    mergedFailed,
    uniqueSkipped,
    mergedDurations,
    outputs.join('\n---\n')
  );
}

/** Run a single repeat container and collect its results. */
async function runRepeatContainer(
  opts: Options,
  onEvent: EventCallback,
  imageTag: string,
  dirs: { artifacts: string; covData?: string },
  runIndex: number
): Promise<{
  passedTests: string[];
  failedTests: FailedTest[];
  skippedTests: string[];
  testDurations: Record<string, number>;
  exitCode: number;
  output: string;
}> {
  const passedTests: string[] = [];
  const failedTests: FailedTest[] = [];
  const skippedTests: string[] = [];
  const testDurations: Record<string, number> = {};

  const env: Record<string, string> = {};
  const volumes: string[] = [`${dirs.artifacts}:/artifacts`];
  if (opts.runPattern) {
    env['TEST_GREP'] = opts.runPattern;
  }
  if (dirs.covData) {
    env['GOCOVERDIR'] = '/covdata';
    volumes.push(`${dirs.covData}:/covdata`);
  }

  const containerResult = await runContainer({
    tag: imageTag,
    env: Object.keys(env).length > 0 ? env : undefined,
    volumes,
    onLine: (line) => {
      const event = parsePlaywrightLine(line);
      if (!event) {
        if (opts.verbose) {
          onEvent('scenarios', { type: 'output_line', line: `[run ${runIndex}] ${line}` });
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

  return {
    passedTests,
    failedTests,
    skippedTests,
    testDurations,
    exitCode: containerResult.exitCode,
    output: containerResult.output,
  };
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
