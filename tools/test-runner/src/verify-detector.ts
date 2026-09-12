import { exec, projectRoot } from './exec.js';
import {
  parseVitestJson,
  combineVitestRuns,
  classifyVitestSuiteStatus,
  type VitestIteration,
} from './parsers.js';
import { computeFlakyResults } from './runner.js';
import { classifyVerdict } from './verdicts.js';
import { existsSync, mkdtempSync, readFileSync, rmSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join, resolve } from 'node:path';
import type { SuiteResult } from './types.js';

const FIXTURE_DIR = 'test/detector-fixtures/frontend';
const ALTERNATING = 'detector contract: alternates by sample index';
const STABLE = 'detector contract: stable control';

// Judge two fixture runs with the production aggregation path: the
// alternating test must be FLAKY (1 pass / 1 fail) and the stable control
// must be stable (2 passes) with complete evidence. Anything else — a green
// suite, missing JSON, or wrong counts — is a contract failure.
export function evaluateDetectorRuns(runs: VitestIteration[]): { ok: boolean; reason: string } {
  if (runs.some((r) => r.detail === null)) {
    return { ok: false, reason: 'a fixture run produced no parseable JSON report' };
  }
  const details = runs.map((r) => r.detail!);
  const combined = combineVitestRuns(details);
  const names = [...combined.passedTests, ...combined.failedTests.map((f) => f.name)];
  const alternating = names.filter((n) => n.includes(ALTERNATING));
  const stable = names.filter((n) => n.includes(STABLE));
  if (alternating.length !== 2 || stable.length !== 2) {
    return {
      ok: false,
      reason: `expected 2 observations of each fixture test, saw alternating=${alternating.length} stable=${stable.length}`,
    };
  }
  const { status } = classifyVitestSuiteStatus(runs, null);
  const result: SuiteResult = {
    suite: 'frontend',
    status,
    durationMs: 0,
    passedTests: combined.passedTests,
    failedTests: combined.failedTests,
    skippedTests: combined.skippedTests,
    testDurations: combined.testDurations,
    output: '',
  };
  const { flakyResults, incompleteSuites } = computeFlakyResults([result], 2);
  if (incompleteSuites.length > 0) {
    return {
      ok: false,
      reason: 'fixture evidence incomplete — every fixture test must be observed exactly 2 times',
    };
  }
  const alt = flakyResults.find((r) => r.testName.includes(ALTERNATING));
  const stab = flakyResults.find((r) => r.testName.includes(STABLE));
  if (!alt || classifyVerdict(alt) !== 'flaky' || alt.passCount !== 1 || alt.failCount !== 1) {
    return {
      ok: false,
      reason: `alternating fixture must be FLAKY with 1 pass / 1 fail, got ${
        alt ? `${classifyVerdict(alt)} (${alt.passCount}/${alt.failCount})` : 'no history'
      }`,
    };
  }
  if (!stab || classifyVerdict(stab) !== 'stable' || stab.passCount !== 2) {
    return {
      ok: false,
      reason: `stable fixture must pass both runs, got ${
        stab ? `${classifyVerdict(stab)} (${stab.passCount}/${stab.failCount})` : 'no history'
      }`,
    };
  }
  return {
    ok: true,
    reason:
      'alternating fixture reported FLAKY (1 pass / 1 fail); stable control stable (2 passes)',
  };
}

export async function runDetectorVerify(): Promise<number> {
  const root = projectRoot();
  const dashboardDir = resolve(root, 'assets/dashboard');
  const fixtureRoot = resolve(root, FIXTURE_DIR);
  const tmpDir = mkdtempSync(join(tmpdir(), 'schmux-detector-'));
  const runs: VitestIteration[] = [];
  try {
    for (let i = 1; i <= 2; i++) {
      const jsonPath = join(tmpDir, `run-${i}.json`);
      const result = await exec({
        cmd: 'npx',
        args: [
          'vitest',
          'run',
          '--reporter=default',
          '--reporter=json',
          `--outputFile=${jsonPath}`,
          '--root',
          fixtureRoot,
        ],
        cwd: dashboardDir,
        env: { DETECTOR_SAMPLE_INDEX: String(i) },
      });
      if (result.exitCode !== 0 && result.exitCode !== 1) {
        console.error(`detector verify: vitest exited ${result.exitCode} on sample ${i}`);
        console.error(result.stderr);
        return 2;
      }
      let detail = null;
      if (existsSync(jsonPath)) {
        try {
          detail = parseVitestJson(JSON.parse(readFileSync(jsonPath, 'utf-8')), fixtureRoot);
        } catch {
          detail = null; // unparseable JSON — broken evidence
        }
      }
      runs.push({ exitCode: result.exitCode, detail, index: i });
    }
  } finally {
    rmSync(tmpDir, { recursive: true, force: true });
  }
  const verdict = evaluateDetectorRuns(runs);
  console.log(
    verdict.ok
      ? `detector contract verified (frontend): ${verdict.reason}`
      : `detector verify FAILED (frontend): ${verdict.reason}`
  );
  return verdict.ok ? 0 : 2;
}
