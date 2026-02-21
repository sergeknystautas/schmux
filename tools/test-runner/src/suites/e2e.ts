import { projectRoot } from '../exec.js';
import { parseGoTestLine, GoTestOutputAccumulator } from '../parsers.js';
import {
  isDockerAvailable,
  ensureBaseImage,
  buildImage,
  runContainer,
  removeImage,
} from '../docker.js';
import { buildLocalArtifacts } from './shared.js';
import type { Options, EventCallback, SuiteResult, FailedTest } from '../types.js';

export async function run(opts: Options, onEvent: EventCallback): Promise<SuiteResult> {
  const startTime = performance.now();

  onEvent('e2e', { type: 'suite_status', status: 'running', message: 'Running E2E tests...' });

  if (!(await isDockerAvailable())) {
    onEvent('e2e', {
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

  const imageTag = `schmux-e2e-${process.pid}`;

  // Build local artifacts
  onEvent('e2e', {
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

  // Ensure base image
  if (
    !(await ensureBaseImage({
      tag: 'schmux-e2e-base',
      dockerfile: 'Dockerfile.e2e-base',
      label: 'E2E',
      force: opts.force,
      verbose: opts.verbose,
      onEvent,
      suite: 'e2e',
    }))
  ) {
    return makeResult(
      'failed',
      performance.now() - startTime,
      [],
      [],
      [],
      {},
      'Failed to build E2E base image'
    );
  }

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
      status: 'failed',
      message: 'Failed to build E2E test image',
    });
    return makeResult(
      'failed',
      performance.now() - startTime,
      [],
      [],
      [],
      {},
      'Failed to build E2E test image'
    );
  }
  onEvent('e2e', { type: 'build_step', message: 'E2E test image built' });

  // Run container
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
  if (opts.runPattern) {
    env['TEST_RUN'] = opts.runPattern;
  }
  if (opts.repeat > 1) {
    env['TEST_COUNT'] = String(opts.repeat);
  }

  try {
    const containerResult = await runContainer({
      tag: imageTag,
      env: Object.keys(env).length > 0 ? env : undefined,
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

    const durationMs = performance.now() - startTime;
    const status = containerResult.exitCode === 0 ? 'passed' : 'failed';

    onEvent('e2e', {
      type: 'suite_status',
      status: status === 'passed' ? 'passed' : 'failed',
      message: status === 'passed' ? 'E2E tests passed' : 'E2E tests failed',
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
