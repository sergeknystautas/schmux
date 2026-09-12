// Synthetic detector-contract fixture for `./test.sh --verify-detector`.
// Structurally excluded from normal discovery: the dashboard's vitest runs
// are rooted at assets/dashboard/ and never scan test/. The runner reaches
// this file only through --verify-detector, which sets DETECTOR_SAMPLE_INDEX
// per run. Odd samples pass, even samples fail — never randomness.
import { test, expect } from 'vitest';

const sampleIndex = Number(process.env.DETECTOR_SAMPLE_INDEX ?? '0');

test('detector contract: alternates by sample index', () => {
  expect(sampleIndex % 2).toBe(1);
});

test('detector contract: stable control', () => {
  expect(sampleIndex).toBeGreaterThan(0);
});
