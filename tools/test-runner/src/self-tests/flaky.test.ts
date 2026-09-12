import { test } from 'node:test';
import assert from 'node:assert/strict';
import { computeFlakyResults } from '../runner.js';
import { classifyVerdict, hasFlakyFindings } from '../verdicts.js';
import type { FlakyResult, SuiteResult } from '../types.js';

function frontendResult(
  opts: {
    passed?: string[];
    failed?: string[];
    skipped?: string[];
    status?: 'passed' | 'failed';
  } = {}
): SuiteResult {
  return {
    suite: 'frontend',
    status: opts.status ?? 'passed',
    durationMs: 1,
    passedTests: opts.passed ?? [],
    failedTests: (opts.failed ?? []).map((name) => ({
      name,
      output: '',
      rerunCommand: `./test.sh --frontend --run '${name}'`,
    })),
    skippedTests: opts.skipped ?? [],
    testDurations: {},
    output: '',
  };
}

function backendResult(passed: string[], skipped: string[] = []): SuiteResult {
  return {
    suite: 'backend',
    status: 'passed',
    durationMs: 1,
    passedTests: passed,
    failedTests: [],
    skippedTests: skipped,
    testDurations: {},
    output: '',
  };
}

test('pass/pass is stable, evidence complete', () => {
  const { flakyResults, incompleteSuites } = computeFlakyResults(
    [frontendResult({ passed: ['a', 'a'] })],
    2
  );
  assert.deepEqual(incompleteSuites, []);
  const a = flakyResults.find((r) => r.testName === 'a');
  assert.ok(a);
  assert.equal(a.passCount, 2);
  assert.equal(a.failCount, 0);
  assert.equal(a.totalRuns, 2);
  assert.equal(classifyVerdict(a), 'stable');
});

test('pass/fail is flaky', () => {
  const { flakyResults } = computeFlakyResults(
    [frontendResult({ passed: ['a'], failed: ['a'] })],
    2
  );
  const a = flakyResults.find((r) => r.testName === 'a');
  assert.ok(a);
  assert.equal(classifyVerdict(a), 'flaky');
  assert.equal(a.flakyScore, 0.5);
});

test('fail/fail is a deterministic failure, not flaky', () => {
  const { flakyResults } = computeFlakyResults(
    [frontendResult({ failed: ['a', 'a'], status: 'failed' })],
    2
  );
  const a = flakyResults.find((r) => r.testName === 'a');
  assert.ok(a);
  assert.equal(a.failCount, 2);
  assert.equal(classifyVerdict(a), 'failing');
});

test('EXACT REGRESSION: one observation with repeat=2 is broken evidence', () => {
  const result = frontendResult({ passed: ['a'] });
  const { flakyResults, incompleteSuites } = computeFlakyResults([result], 2);
  assert.equal(result.status, 'broken');
  assert.deepEqual(incompleteSuites, ['frontend']);
  // The entry exists but the verdict is withheld by the report, not claimed clean.
  const a = flakyResults.find((r) => r.testName === 'a');
  assert.ok(a);
  assert.equal(a.totalRuns, 1);
});

test('duplicate display names in two files are distinct identities', () => {
  const result = frontendResult({ passed: ['f1.test.ts > a', 'f2.test.ts > a'] });
  const { incompleteSuites } = computeFlakyResults([result], 2);
  assert.equal(result.status, 'broken');
  assert.deepEqual(incompleteSuites, ['frontend']);
});

test('skip/skip is complete evidence, counted as observations', () => {
  const result = frontendResult({ skipped: ['a', 'a'] });
  const { flakyResults, incompleteSuites } = computeFlakyResults([result], 2);
  assert.equal(result.status, 'passed');
  assert.deepEqual(incompleteSuites, []);
  const a = flakyResults.find((r) => r.testName === 'a');
  assert.ok(a);
  assert.equal(a.skipCount, 2);
  assert.equal(classifyVerdict(a), 'skipped');
});

test('pass/skip is inconsistent — never stable', () => {
  const { flakyResults } = computeFlakyResults(
    [frontendResult({ passed: ['a'], skipped: ['a'] })],
    2
  );
  const a = flakyResults.find((r) => r.testName === 'a');
  assert.ok(a);
  assert.equal(a.skipCount, 1);
  assert.equal(classifyVerdict(a), 'inconsistent');
});

test('fail/skip is inconsistent — neither stable nor flaky', () => {
  const { flakyResults } = computeFlakyResults(
    [frontendResult({ failed: ['a'], skipped: ['a'], status: 'failed' })],
    2
  );
  const a = flakyResults.find((r) => r.testName === 'a');
  assert.ok(a);
  assert.equal(classifyVerdict(a), 'inconsistent');
});

test('flaky entries take the rerun command from the failed occurrence', () => {
  // The failed occurrence's command was built from structured fields at
  // parse time (correct even when the title contains ' > '); the
  // identity-derived passed form is only a fallback and must not win.
  const { flakyResults } = computeFlakyResults(
    [frontendResult({ passed: ['f.test.ts > a'], failed: ['f.test.ts > a'] })],
    2
  );
  const a = flakyResults.find((r) => r.testName === 'f.test.ts > a');
  assert.ok(a);
  assert.equal(a.rerunCommand, "./test.sh --frontend --run 'f.test.ts > a' --repeat 2");
});

test('frontend passed-test rerun commands strip the file segment and take --repeat', () => {
  const { flakyResults } = computeFlakyResults(
    [frontendResult({ passed: ['f.test.ts > math > adds'] })],
    2
  );
  const a = flakyResults.find((r) => r.testName === 'f.test.ts > math > adds');
  assert.ok(a);
  assert.equal(a.rerunCommand, "./test.sh --frontend --run 'math adds' --repeat 2");
});

test('backend is exempt: missing observations do not break it, skips are not counted', () => {
  const result = backendResult(['TestX'], ['TestY']);
  const { flakyResults, incompleteSuites } = computeFlakyResults([result], 2);
  assert.equal(result.status, 'passed');
  assert.deepEqual(incompleteSuites, []);
  const x = flakyResults.find((r) => r.testName === 'TestX');
  assert.ok(x);
  assert.equal(x.skipCount, 0);
  assert.equal(
    flakyResults.find((r) => r.testName === 'TestY'),
    undefined
  );
});

test('hasFlakyFindings is false when every history is stable or failing', () => {
  const hist = (passCount: number, failCount: number): FlakyResult => ({
    testName: 'TestX',
    suite: 'frontend',
    passCount,
    failCount,
    skipCount: 0,
    totalRuns: passCount + failCount,
    flakyScore: failCount / (passCount + failCount),
    rerunCommand: './test.sh --frontend --run TestX',
  });
  assert.equal(hasFlakyFindings([hist(3, 0), hist(0, 3)]), false);
});

test('hasFlakyFindings is true when any history is mixed pass/fail', () => {
  const hist = (passCount: number, failCount: number): FlakyResult => ({
    testName: 'TestX',
    suite: 'frontend',
    passCount,
    failCount,
    skipCount: 0,
    totalRuns: passCount + failCount,
    flakyScore: failCount / (passCount + failCount),
    rerunCommand: './test.sh --frontend --run TestX',
  });
  assert.equal(hasFlakyFindings([hist(3, 0), hist(1, 2)]), true);
});

test('hasFlakyFindings is false for an empty set (repeat=1 runs pass no histories)', () => {
  assert.equal(hasFlakyFindings([]), false);
});
