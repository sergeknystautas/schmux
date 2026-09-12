import { test } from 'node:test';
import assert from 'node:assert/strict';
import { vitestRunArgs } from '../suites/frontend.js';
import type { Options } from '../types.js';

const base: Options = {
  suites: ['frontend'],
  all: false,
  race: false,
  verbose: false,
  coverage: false,
  force: false,
  noCache: false,
  quick: false,
  runPattern: null,
  repeat: 2,
  serial: false,
  recordVideo: false,
  vitestShuffleSeed: null,
  verifyDetector: false,
};

test('vitestRunArgs includes reporters and output file', () => {
  const args = vitestRunArgs(base, '/tmp/run-1.json');
  assert.deepEqual(args, [
    'vitest',
    'run',
    '--reporter=default',
    '--reporter=json',
    '--outputFile=/tmp/run-1.json',
  ]);
});

test('vitestRunArgs adds shuffle flags only when a seed is set', () => {
  assert.equal(vitestRunArgs(base, '/tmp/run-1.json').includes('--sequence.shuffle'), false);
  const shuffled = vitestRunArgs({ ...base, vitestShuffleSeed: 4242 }, '/tmp/run-1.json');
  assert.equal(shuffled.includes('--sequence.shuffle'), true);
  assert.equal(shuffled.includes('--sequence.seed=4242'), true);
});

test('vitestRunArgs passes .test.ts run patterns positionally and others via -t', () => {
  assert.equal(
    vitestRunArgs({ ...base, runPattern: 'foo.test.ts' }, '/tmp/run-1.json').includes(
      'foo.test.ts'
    ),
    true
  );
  const named = vitestRunArgs({ ...base, runPattern: 'foo' }, '/tmp/run-1.json');
  assert.equal(named.includes('-t'), true);
  assert.equal(named.includes('foo'), true);
});
