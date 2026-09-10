import { test } from 'node:test';
import assert from 'node:assert/strict';
import { parseVitestJson } from '../parsers.js';

const DASH = '/repo/assets/dashboard';

const report = {
  numTotalTests: 4,
  numPassedTests: 2,
  numFailedTests: 1,
  numPendingTests: 1,
  success: false,
  testResults: [
    {
      name: `${DASH}/src/lib/math.test.ts`,
      status: 'failed',
      message: '',
      assertionResults: [
        {
          ancestorTitles: ['', 'math'],
          fullName: ' math 2 + 2 should equal 4',
          status: 'passed',
          title: '2 + 2 should equal 4',
          duration: 9,
          failureMessages: [],
        },
        {
          ancestorTitles: ['', 'math'],
          status: 'failed',
          title: '4 - 2 should equal 2',
          duration: 4,
          failureMessages: ['expected 5 to be 4 // Object.is equality'],
        },
        {
          ancestorTitles: ['', 'math'],
          status: 'skipped',
          title: 'sometimes skipped',
          duration: 0,
          failureMessages: [],
        },
      ],
    },
    {
      name: `${DASH}/src/lib/load-fail.test.ts`,
      status: 'failed',
      message: 'Error: Cannot find module ./missing',
      assertionResults: [],
    },
    {
      name: `${DASH}/src/lib/todo.test.ts`,
      status: 'passed',
      message: '',
      assertionResults: [
        { ancestorTitles: [''], status: 'todo', title: 'a todo', duration: 0, failureMessages: [] },
      ],
    },
  ],
};

test('parses identities as file > ancestors > title with statuses mapped', () => {
  const d = parseVitestJson(report, DASH);
  assert.deepEqual(d.passedTests, ['src/lib/math.test.ts > math > 2 + 2 should equal 4']);
  assert.deepEqual(d.skippedTests, [
    'src/lib/math.test.ts > math > sometimes skipped',
    'src/lib/todo.test.ts > a todo',
  ]);
  assert.equal(d.failedTests.length, 2);
  const assertionFailure = d.failedTests.find((f) => f.name.includes('4 - 2'));
  assert.ok(assertionFailure);
  assert.equal(assertionFailure.output, 'expected 5 to be 4 // Object.is equality');
  assert.equal(
    assertionFailure.rerunCommand,
    "./test.sh --frontend --run 'math 4 - 2 should equal 2'"
  );
});

test('file-level load failure becomes a failed test carrying the message', () => {
  const d = parseVitestJson(report, DASH);
  const loadFailure = d.failedTests.find((f) => f.name.includes('(failed to load)'));
  assert.ok(loadFailure);
  assert.equal(loadFailure.name, 'src/lib/load-fail.test.ts (failed to load)');
  assert.equal(loadFailure.output, 'Error: Cannot find module ./missing');
  assert.equal(loadFailure.rerunCommand, "./test.sh --frontend --run 'src/lib/load-fail.test.ts'");
});

test('durations are kept per identity', () => {
  const d = parseVitestJson(report, DASH);
  assert.equal(d.testDurations['src/lib/math.test.ts > math > 2 + 2 should equal 4'], 9);
});

test('integrity holds when parsed count matches numTotalTests', () => {
  const d = parseVitestJson(report, DASH);
  assert.equal(d.integrityOk, true);
  assert.equal(d.totals.total, 4);
  assert.equal(d.totals.passed, 1);
  assert.equal(d.totals.failed, 2); // 1 assertion failure + 1 file load failure
  assert.equal(d.totals.skipped, 2);
  assert.equal(d.success, false);
});

test('failed-test rerun commands preserve a literal > inside the title', () => {
  // vitest -t matches the space-joined ancestors+title as a regex; the >
  // in a comparison-operator title is literal text and must survive, while
  // the parens must be escaped.
  const arrowReport = {
    numTotalTests: 1,
    numPassedTests: 0,
    numFailedTests: 1,
    numPendingTests: 0,
    success: false,
    testResults: [
      {
        name: `${DASH}/src/components/ClipboardBanner.test.tsx`,
        status: 'failed',
        message: '',
        assertionResults: [
          {
            ancestorTitles: ['', 'ClipboardBanner'],
            status: 'failed',
            title: 'shows stripped count note when strippedControlChars > 0 (singular)',
            duration: 5,
            failureMessages: ['boom'],
          },
        ],
      },
    ],
  };
  const d = parseVitestJson(arrowReport, DASH);
  assert.equal(d.failedTests.length, 1);
  assert.equal(
    d.failedTests[0].rerunCommand,
    "./test.sh --frontend --run 'ClipboardBanner shows stripped count note when strippedControlChars > 0 \\(singular\\)'"
  );
});

test('integrity fails when parsed count diverges from numTotalTests', () => {
  const d = parseVitestJson({ ...report, numTotalTests: 5 }, DASH);
  assert.equal(d.integrityOk, false);
});
