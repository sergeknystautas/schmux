import { exec, projectRoot } from '../exec.js';
import {
  parseVitestJson,
  combineVitestRuns,
  classifyVitestSuiteStatus,
  type VitestIteration,
} from '../parsers.js';
import { parseVitestCoverage } from '../coverage.js';
import type { Options, EventCallback, SuiteResult } from '../types.js';
import type { FrontendCoverageReport } from '../coverage.js';
import { existsSync, readFileSync, mkdtempSync, rmSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join, resolve } from 'node:path';

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

  const iterations = Math.max(1, opts.repeat);
  const tmpDir = mkdtempSync(join(tmpdir(), 'schmux-frontend-'));
  const outputLines: string[] = [];
  const runs: VitestIteration[] = [];
  let totalDurationMs = 0;

  try {
    for (let i = 1; i <= iterations; i++) {
      if (iterations > 1) {
        onEvent('frontend', { type: 'build_step', message: `Repeat ${i}/${iterations}` });
      }

      const jsonPath = join(tmpDir, `run-${i}.json`);
      const args = [
        'vitest',
        'run',
        // Explicit reporters: JSON goes to the file for machine ingestion,
        // default keeps human/coverage-table output. This also overrides
        // Vitest's AI-agent minimal-reporter auto-detection, which
        // suppresses everything a parser could read.
        '--reporter=default',
        '--reporter=json',
        `--outputFile=${jsonPath}`,
      ];
      if (opts.coverage) args.push('--coverage');
      if (opts.runPattern) {
        // File-ish patterns select the file (vitest positional filter);
        // anything else filters by test name (-t).
        if (opts.runPattern.endsWith('.test.ts') || opts.runPattern.endsWith('.test.tsx')) {
          args.push(opts.runPattern);
        } else {
          args.push('-t', opts.runPattern);
        }
      }

      const result = await exec({
        cmd: 'npx',
        args,
        cwd: dashboardDir,
        onLine: (line) => {
          outputLines.push(line);
          if (opts.verbose) {
            onEvent('frontend', { type: 'output_line', line });
          }
        },
      });
      totalDurationMs += result.durationMs;

      let detail = null;
      if (existsSync(jsonPath)) {
        try {
          detail = parseVitestJson(JSON.parse(readFileSync(jsonPath, 'utf-8')), dashboardDir);
        } catch {
          detail = null; // unparseable JSON — broken evidence
        }
      }
      runs.push({ exitCode: result.exitCode, detail, index: i });

      // Emit per-test events after the iteration's JSON is parsed —
      // progress arrives per-iteration, and every event is a real test.
      if (detail) {
        for (const name of detail.passedTests) {
          onEvent('frontend', {
            type: 'test_pass',
            name,
            durationMs: detail.testDurations[name] ?? 0,
          });
        }
        for (const ft of detail.failedTests) {
          onEvent('frontend', {
            type: 'test_fail',
            name: ft.name,
            durationMs: detail.testDurations[ft.name] ?? 0,
            output: ft.output,
          });
        }
      }
    }
  } finally {
    rmSync(tmpDir, { recursive: true, force: true });
  }

  const { status, reason } = classifyVitestSuiteStatus(runs, opts.runPattern);
  const combined = combineVitestRuns(
    runs.map((r) => r.detail).filter((d): d is NonNullable<typeof d> => d !== null)
  );

  onEvent('frontend', {
    type: 'suite_status',
    status,
    message:
      status === 'broken'
        ? `Frontend tests broken: ${reason}`
        : status === 'passed'
          ? 'Frontend tests passed'
          : reason
            ? `Frontend tests failed: ${reason}`
            : 'Frontend tests failed',
  });

  // Parse coverage if enabled and tests passed
  let frontendCoverageReport: FrontendCoverageReport | undefined;
  if (opts.coverage && status === 'passed') {
    const parsed = parseVitestCoverage(outputLines.join('\n'));
    if (parsed) {
      frontendCoverageReport = parsed;
    }
  }

  return makeResult(
    status,
    totalDurationMs,
    combined.passedTests,
    combined.failedTests,
    combined.skippedTests,
    combined.testDurations,
    reason ? `${reason}\n${outputLines.join('\n')}` : outputLines.join('\n'),
    frontendCoverageReport
  );
}

function makeResult(
  status: 'passed' | 'failed' | 'broken',
  durationMs: number,
  passedTests: string[],
  failedTests: SuiteResult['failedTests'],
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
