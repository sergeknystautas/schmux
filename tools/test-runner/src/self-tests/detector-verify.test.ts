import { test } from 'node:test';
import assert from 'node:assert/strict';
import { evaluateDetectorRuns } from '../verify-detector.js';
import type { VitestIteration, VitestRunDetail } from '../parsers.js';

const ALT = 'detector-verify.test.ts > detector contract: alternates by sample index';
const STABLE = 'detector-verify.test.ts > detector contract: stable control';

const detail = (altFails: boolean): VitestRunDetail => ({
  passedTests: altFails ? [STABLE] : [ALT, STABLE],
  failedTests: altFails ? [{ name: ALT, output: '', rerunCommand: '' }] : [],
  skippedTests: [],
  testDurations: {},
  integrityOk: true,
  success: !altFails,
  totals: { passed: altFails ? 1 : 2, failed: altFails ? 1 : 0, skipped: 0, total: 2 },
});

const iteration = (i: number, d: VitestRunDetail | null): VitestIteration => ({
  exitCode: d ? (d.success ? 0 : 1) : 1,
  detail: d,
  index: i,
});

test('evaluateDetectorRuns verifies pass-then-fail as FLAKY with a stable control', () => {
  const verdict = evaluateDetectorRuns([iteration(1, detail(false)), iteration(2, detail(true))]);
  assert.equal(verdict.ok, true);
});

test('evaluateDetectorRuns rejects an always-failing fixture (not FLAKY)', () => {
  const verdict = evaluateDetectorRuns([iteration(1, detail(true)), iteration(2, detail(true))]);
  assert.equal(verdict.ok, false);
});

test('evaluateDetectorRuns rejects an always-passing fixture (no variation found)', () => {
  const verdict = evaluateDetectorRuns([iteration(1, detail(false)), iteration(2, detail(false))]);
  assert.equal(verdict.ok, false);
});

test('evaluateDetectorRuns rejects unparseable evidence', () => {
  const verdict = evaluateDetectorRuns([iteration(1, detail(false)), iteration(2, null)]);
  assert.equal(verdict.ok, false);
  assert.match(verdict.reason, /no parseable JSON/);
});
