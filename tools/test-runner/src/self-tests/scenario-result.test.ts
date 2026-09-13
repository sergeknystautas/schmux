import { test } from 'node:test';
import assert from 'node:assert/strict';
import { classifyScenarioRun, describeBrokenScenarioRun } from '../scenario-result.js';

test('complete pass: parsed passes and a zero exit are passing evidence', () => {
  assert.equal(classifyScenarioRun(2, 0, 0), 'passed');
});

test('asserted failure: any parsed failure is failed regardless of exit code', () => {
  assert.equal(classifyScenarioRun(1, 1, 1), 'failed');
  assert.equal(classifyScenarioRun(0, 1, 1), 'failed');
  assert.equal(classifyScenarioRun(1, 1, 0), 'failed');
});

test('zero evidence: no parsed tests is broken whatever the exit code', () => {
  assert.equal(classifyScenarioRun(0, 0, 0), 'broken');
  assert.equal(classifyScenarioRun(0, 0, 1), 'broken');
});

test('partial pass then nonzero exit: parsed passes cannot outvote a crashed container', () => {
  assert.equal(classifyScenarioRun(3, 0, 1), 'broken');
  assert.equal(classifyScenarioRun(3, 0, 137), 'broken');
});

test('describeBrokenScenarioRun names the missing evidence', () => {
  assert.equal(
    describeBrokenScenarioRun(0, 0, 0),
    'Scenario runner produced no test results (exit code 0)'
  );
  assert.equal(
    describeBrokenScenarioRun(3, 0, 137),
    'Scenario container exited with code 137 after 3 passed and 0 failed tests; results are incomplete'
  );
});
