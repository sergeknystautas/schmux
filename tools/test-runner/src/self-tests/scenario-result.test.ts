import { test } from 'node:test';
import assert from 'node:assert/strict';
import { classifyScenarioRun } from '../scenario-result.js';

test('classifyScenarioRun distinguishes failures from missing test evidence', () => {
  assert.equal(classifyScenarioRun(2, 0), 'passed');
  assert.equal(classifyScenarioRun(1, 1), 'failed');
  assert.equal(classifyScenarioRun(0, 1), 'failed');
  assert.equal(classifyScenarioRun(0, 0), 'broken');
});
