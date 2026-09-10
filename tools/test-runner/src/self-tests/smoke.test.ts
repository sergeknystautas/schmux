import { test } from 'node:test';
import assert from 'node:assert/strict';
import { parseGoTestLine } from '../parsers.js';

test('self-test harness executes TS via tsx', () => {
  const pass = parseGoTestLine('--- PASS: TestFoo (1.23s)', 0);
  assert.ok(pass && pass.type === 'test_pass');
  if (pass.type === 'test_pass') {
    assert.equal(pass.name, 'TestFoo');
    assert.equal(pass.durationMs, 1230);
  }
});

test('parseGoTestLine recognizes failure lines', () => {
  const fail = parseGoTestLine('--- FAIL: TestBar (0.10s)', 0);
  assert.ok(fail && fail.type === 'test_fail');
  if (fail.type === 'test_fail') {
    assert.equal(fail.name, 'TestBar');
    assert.equal(fail.durationMs, 100);
  }
});
