import { projectRoot } from '../exec.js';
import { parseGoTestLine, GoTestOutputAccumulator } from '../parsers.js';
import {
  isDockerAvailable,
  ensureBaseImage,
  imageExists,
  buildImage,
  runContainer,
  removeImage,
  cleanupOrphans,
} from '../docker.js';
import { buildLocalArtifacts } from './shared.js';
import type { Options, EventCallback, SuiteResult, FailedTest } from '../types.js';
import { resolve } from 'node:path';
import { rmSync, mkdirSync } from 'node:fs';
import { availableParallelism } from 'node:os';

const BASE_TAG = 'schmux-e2e-base';

export async function run(opts: Options, onEvent: EventCallback): Promise<SuiteResult> {
  const startTime = performance.now();

  onEvent('e2e', { type: 'suite_status', status: 'running', message: 'Running E2E tests...' });

  if (!(await isDockerAvailable())) {
    onEvent('e2e', {
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
  const imageTag = `schmux-e2e-${process.pid}`;

  // Clean up orphaned containers/images from interrupted previous runs
  const orphans = await cleanupOrphans('e2e');
  if (orphans > 0) {
    onEvent('e2e', {
      type: 'build_step',
      message: `Cleaned up ${orphans} orphaned container(s)/image(s) from previous runs`,
    });
  }

  // Build local artifacts
  onEvent('e2e', {
    type: 'suite_status',
    status: 'building',
    message: 'Building local artifacts...',
  });
  const buildResult = await buildLocalArtifacts(onEvent, opts.coverage);
  if (!buildResult.ok) {
    return makeResult(
      'broken',
      performance.now() - startTime,
      [],
      [],
      [],
      {},
      buildResult.errorOutput || 'Failed to build local artifacts'
    );
  }

  // Track whether the base image was reused from cache
  const baseCached = !opts.force && (await imageExists(BASE_TAG));

  // Ensure base image
  if (
    !(await ensureBaseImage({
      tag: BASE_TAG,
      dockerfile: 'Dockerfile.e2e-base',
      label: 'E2E',
      force: opts.force,
      verbose: opts.verbose,
      onEvent,
      suite: 'e2e',
    }))
  ) {
    return makeResult(
      'broken',
      performance.now() - startTime,
      [],
      [],
      [],
      {},
      'Failed to build E2E base image'
    );
  }

  // Build ephemeral image + run container (with auto-retry on stale base image)
  // Set up coverage directory if requested
  let covDataDir: string | undefined;
  if (opts.coverage) {
    covDataDir = resolve(root, 'build/covdata-e2e');
    rmSync(covDataDir, { recursive: true, force: true });
    mkdirSync(covDataDir, { recursive: true });
  }

  try {
    const result = await buildAndRun(opts, onEvent, imageTag, covDataDir);
    if (!result) {
      return makeResult(
        'broken',
        performance.now() - startTime,
        [],
        [],
        [],
        {},
        'Failed to build E2E test image'
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
      onEvent('e2e', {
        type: 'build_step',
        message: `${reason} — rebuilding base image and retrying...`,
      });

      await removeImage(BASE_TAG).catch(() => {});
      if (
        !(await ensureBaseImage({
          tag: BASE_TAG,
          dockerfile: 'Dockerfile.e2e-base',
          label: 'E2E',
          force: true,
          verbose: opts.verbose,
          onEvent,
          suite: 'e2e',
        }))
      ) {
        return makeResult(
          'broken',
          performance.now() - startTime,
          [],
          [],
          [],
          {},
          'Failed to rebuild E2E base image'
        );
      }

      if (covDataDir) {
        rmSync(covDataDir, { recursive: true, force: true });
        mkdirSync(covDataDir, { recursive: true });
      }

      const retryResult = await buildAndRun(opts, onEvent, imageTag, covDataDir);
      if (!retryResult) {
        return makeResult(
          'broken',
          performance.now() - startTime,
          [],
          [],
          [],
          {},
          'Failed to build E2E test image on retry'
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
  covDataDir?: string
): Promise<SuiteResult | null> {
  // Build ephemeral image
  onEvent('e2e', { type: 'build_step', message: 'Building E2E test image...' });
  if (
    !(await buildImage({
      dockerfile: 'Dockerfile.e2e',
      tag: imageTag,
      verbose: opts.verbose,
      onEvent,
      suite: 'e2e',
    }))
  ) {
    onEvent('e2e', {
      type: 'suite_status',
      status: 'broken',
      message: 'Failed to build E2E test image',
    });
    return null;
  }
  onEvent('e2e', { type: 'build_step', message: 'E2E test image built' });

  // When repeat > 1, run N containers in parallel instead of a single container
  // with -test.count. Each container is fully isolated (own daemon, SSH, tmux)
  // so parallelism is safe and uses idle CPUs.
  // Use --serial to force the old single-container behavior.
  if (opts.repeat > 1 && !opts.serial) {
    return runParallelContainers(opts, onEvent, imageTag, covDataDir);
  }

  return runSingleContainer(opts, onEvent, imageTag, covDataDir);
}

async function runSingleContainer(
  opts: Options,
  onEvent: EventCallback,
  imageTag: string,
  covDataDir?: string
): Promise<SuiteResult> {
  onEvent('e2e', {
    type: 'suite_status',
    status: 'running',
    message: 'Running E2E tests in container...',
  });

  const accumulator = new GoTestOutputAccumulator();
  const passedTests: string[] = [];
  const failedTests: FailedTest[] = [];
  const skippedTests: string[] = [];
  const testDurations: Record<string, number> = {};

  const env: Record<string, string> = {};
  const volumes: string[] = [];
  if (opts.runPattern) {
    env['TEST_RUN'] = opts.runPattern;
  }
  if (opts.serial && opts.repeat > 1) {
    env['TEST_COUNT'] = String(opts.repeat);
  }
  if (process.env.TEST_PARALLEL) {
    env['TEST_PARALLEL'] = process.env.TEST_PARALLEL;
  }
  if (covDataDir) {
    env['GOCOVERDIR'] = '/covdata';
    volumes.push(`${covDataDir}:/covdata`);
  }

  const containerResult = await runContainer({
    tag: imageTag,
    env: Object.keys(env).length > 0 ? env : undefined,
    volumes: volumes.length > 0 ? volumes : undefined,
    onLine: (line) => {
      accumulator.feedLine(line);

      const event = parseGoTestLine(line, 0);
      if (!event) {
        if (opts.verbose) {
          onEvent('e2e', { type: 'output_line', line });
        }
        return;
      }

      switch (event.type) {
        case 'test_pass':
          passedTests.push(event.name);
          testDurations[event.name] = Math.max(testDurations[event.name] ?? 0, event.durationMs);
          onEvent('e2e', event);
          break;
        case 'test_fail':
          event.output = accumulator.getFailureOutput(event.name);
          failedTests.push({
            name: event.name,
            output: event.output,
            rerunCommand: `./test.sh --e2e --run ${event.name}`,
          });
          testDurations[event.name] = Math.max(testDurations[event.name] ?? 0, event.durationMs);
          onEvent('e2e', event);
          break;
        case 'test_skip':
          skippedTests.push(event.name);
          break;
        default:
          onEvent('e2e', event);
      }
    },
  });

  const status = containerResult.exitCode === 0 ? 'passed' : 'failed';

  onEvent('e2e', {
    type: 'suite_status',
    status: status === 'passed' ? 'passed' : 'failed',
    message: status === 'passed' ? 'E2E tests passed' : 'E2E tests failed',
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
  covDataDir?: string
): Promise<SuiteResult> {
  const total = opts.repeat;
  // Each container runs a daemon + SSH + tmux, needing ~5 CPUs
  // to avoid resource contention that causes timing-sensitive tests to fail.
  const maxParallel = Math.min(total, Math.max(2, Math.floor(availableParallelism() / 5)));
  onEvent('e2e', {
    type: 'suite_status',
    status: 'running',
    message: `Running ${total} E2E repeats in parallel (max ${maxParallel} concurrent containers)...`,
  });

  // Prepare per-run coverage directories
  const runDirs: { covData?: string }[] = [];
  for (let i = 0; i < total; i++) {
    let runCov: string | undefined;
    if (covDataDir) {
      runCov = resolve(covDataDir, `run-${i}`);
      mkdirSync(runCov, { recursive: true });
    }
    runDirs.push({ covData: runCov });
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

  onEvent('e2e', {
    type: 'suite_status',
    status,
    message: status === 'passed' ? 'E2E tests passed' : 'E2E tests failed',
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
  dirs: { covData?: string },
  runIndex: number
): Promise<{
  passedTests: string[];
  failedTests: FailedTest[];
  skippedTests: string[];
  testDurations: Record<string, number>;
  exitCode: number;
  output: string;
}> {
  const accumulator = new GoTestOutputAccumulator();
  const passedTests: string[] = [];
  const failedTests: FailedTest[] = [];
  const skippedTests: string[] = [];
  const testDurations: Record<string, number> = {};

  const env: Record<string, string> = {};
  const volumes: string[] = [];
  if (opts.runPattern) {
    env['TEST_RUN'] = opts.runPattern;
  }
  if (dirs.covData) {
    env['GOCOVERDIR'] = '/covdata';
    volumes.push(`${dirs.covData}:/covdata`);
  }

  const containerResult = await runContainer({
    tag: imageTag,
    env: Object.keys(env).length > 0 ? env : undefined,
    volumes: volumes.length > 0 ? volumes : undefined,
    onLine: (line) => {
      accumulator.feedLine(line);

      const event = parseGoTestLine(line, 0);
      if (!event) {
        if (opts.verbose) {
          onEvent('e2e', { type: 'output_line', line: `[run ${runIndex}] ${line}` });
        }
        return;
      }

      switch (event.type) {
        case 'test_pass':
          passedTests.push(event.name);
          testDurations[event.name] = Math.max(testDurations[event.name] ?? 0, event.durationMs);
          onEvent('e2e', event);
          break;
        case 'test_fail':
          event.output = accumulator.getFailureOutput(event.name);
          failedTests.push({
            name: event.name,
            output: event.output,
            rerunCommand: `./test.sh --e2e --run ${event.name}`,
          });
          testDurations[event.name] = Math.max(testDurations[event.name] ?? 0, event.durationMs);
          onEvent('e2e', event);
          break;
        case 'test_skip':
          skippedTests.push(event.name);
          break;
        default:
          onEvent('e2e', event);
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
    suite: 'e2e',
    status,
    durationMs,
    passedTests,
    failedTests,
    skippedTests,
    testDurations,
    output,
  };
}
