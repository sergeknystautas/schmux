import { test } from 'node:test';
import assert from 'node:assert/strict';
import { combineVitestRuns, classifyVitestSuiteStatus } from '../parsers.js';
import type { VitestRunDetail } from '../types.js';

function detail(overrides: Partial<VitestRunDetail> = {}): VitestRunDetail {
  return {
    passedTests: [],
    failedTests: [],
    skippedTests: [],
    testDurations: {},
    integrityOk: true,
    success: true,
    totals: { passed: 0, failed: 0, skipped: 0, total: 0 },
    ...overrides,
  };
}

test('combineVitestRuns concatenates lists and max-merges durations', () => {
  const combined = combineVitestRuns([
    detail({
      passedTests: ['f > a', 'f > b'],
      testDurations: { 'f > a': 5, 'f > b': 3 },
    }),
    detail({
      passedTests: ['f > a'],
      failedTests: [{ name: 'f > c', output: 'boom', rerunCommand: './test.sh --frontend' }],
      testDurations: { 'f > a': 9, 'f > c': 2 },
    }),
  ]);
  assert.deepEqual(combined.passedTests, ['f > a', 'f > b', 'f > a']);
  assert.equal(combined.failedTests.length, 1);
  assert.equal(combined.testDurations['f > a'], 9);
  assert.equal(combined.testDurations['f > b'], 3);
});

test('missing JSON detail classifies broken with the iteration named', () => {
  const { status, reason } = classifyVitestSuiteStatus([
    { exitCode: 0, detail: detail(), index: 1 },
    { exitCode: 0, detail: null, index: 2 },
  ]);
  assert.equal(status, 'broken');
  assert.match(reason, /iteration 2/);
});

test('integrity failure classifies broken', () => {
  const { status, reason } = classifyVitestSuiteStatus([
    { exitCode: 1, detail: detail({ integrityOk: false }), index: 1 },
  ]);
  assert.equal(status, 'broken');
  assert.match(reason, /iteration 1/);
});

test('exit code disagreeing with the JSON success flag classifies broken', () => {
  const { status } = classifyVitestSuiteStatus([
    { exitCode: 1, detail: detail({ success: true }), index: 1 },
  ]);
  assert.equal(status, 'broken');
});

test('a name pattern selecting no runnable tests classifies failed, not passed', () => {
  // Vitest exits 0 when test files run but -t filters out every test, and
  // the filtered-out tests arrive as *skipped* assertions (a zero-match
  // run reports ~1645 skipped, 0 passed/failed) — so the signal is "no
  // test executed", not zero totals. Without this guard a typo'd --run
  // pattern reports green.
  const { status, reason } = classifyVitestSuiteStatus(
    [
      {
        exitCode: 0,
        detail: detail({
          skippedTests: ['a', 'b'],
          totals: { passed: 0, failed: 0, skipped: 2, total: 2 },
        }),
        index: 1,
      },
    ],
    'no-such-test-name'
  );
  assert.equal(status, 'failed');
  assert.match(reason, /selected no tests to run/);
});

test('a zero-test run without a name pattern stays passed', () => {
  const { status } = classifyVitestSuiteStatus([{ exitCode: 0, detail: detail(), index: 1 }]);
  assert.equal(status, 'passed');
});

test('all iterations green classifies passed', () => {
  const { status, reason } = classifyVitestSuiteStatus([
    { exitCode: 0, detail: detail(), index: 1 },
    { exitCode: 0, detail: detail(), index: 2 },
  ]);
  assert.equal(status, 'passed');
  assert.equal(reason, '');
});

test('nonzero exit or failed tests classify failed (not broken)', () => {
  assert.equal(
    classifyVitestSuiteStatus([{ exitCode: 1, detail: detail({ success: false }), index: 1 }])
      .status,
    'failed'
  );
  assert.equal(
    classifyVitestSuiteStatus([
      {
        exitCode: 0,
        detail: detail({ success: true, totals: { passed: 1, failed: 1, skipped: 0, total: 2 } }),
        index: 1,
      },
    ]).status,
    'failed'
  );
});
