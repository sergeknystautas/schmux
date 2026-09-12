import { test } from 'node:test';
import assert from 'node:assert/strict';
import { buildRepeatReport } from '../repeat-report.js';
import type { FlakyResult, SuiteName } from '../types.js';

const hist = (
  name: string,
  passCount: number,
  failCount: number,
  suite: SuiteName = 'frontend'
): FlakyResult => ({
  testName: name,
  suite,
  passCount,
  failCount,
  skipCount: 0,
  totalRuns: passCount + failCount,
  flakyScore: failCount / (passCount + failCount),
  rerunCommand: `./test.sh --frontend --run ${name}`,
});

test('buildRepeatReport lists mixed histories as flaky findings and counts all tests', () => {
  const report = buildRepeatReport({
    suite: 'frontend',
    repeat: 2,
    shuffleSeed: 4242,
    vitestVersion: '^4.0.18',
    incompleteEvidence: false,
    flakyResults: [hist('a', 2, 0), hist('b', 1, 1), hist('c', 0, 2)],
    now: () => new Date('2026-09-11T00:00:00Z'),
  });
  assert.equal(report.findings.length, 1);
  assert.deepEqual(report.findings[0], {
    testName: 'b',
    verdict: 'flaky',
    passCount: 1,
    failCount: 1,
    skipCount: 0,
    totalRuns: 2,
    rerunCommand: './test.sh --frontend --run b',
  });
  assert.equal(report.testCount, 3);
  assert.equal(report.shuffleSeed, 4242);
  assert.equal(report.generatedAt, '2026-09-11T00:00:00.000Z');
});

test('buildRepeatReport marks incomplete evidence without inventing findings', () => {
  const report = buildRepeatReport({
    suite: 'frontend',
    repeat: 2,
    shuffleSeed: null,
    vitestVersion: null,
    incompleteEvidence: true,
    flakyResults: [hist('a', 1, 0)],
    now: () => new Date(0),
  });
  assert.equal(report.incompleteEvidence, true);
  assert.equal(report.findings.length, 0);
});

test('buildRepeatReport filters histories to the requested suite', () => {
  const report = buildRepeatReport({
    suite: 'e2e',
    repeat: 2,
    shuffleSeed: null,
    vitestVersion: null,
    incompleteEvidence: false,
    flakyResults: [hist('fe-only', 2, 0), hist('go-side', 1, 1, 'e2e')],
    now: () => new Date(0),
  });
  assert.equal(report.testCount, 1);
  assert.equal(report.findings[0]?.testName, 'go-side');
});
