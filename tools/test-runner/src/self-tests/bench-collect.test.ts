import { test } from 'node:test';
import assert from 'node:assert/strict';
import {
  parseBenchResultLine,
  validateBrowserBenchResults,
  buildBrowserBenchReport,
} from '../bench-collect.js';
import type { BrowserBenchResult } from '../types.js';

function resultLine(variant: 'idle' | 'stressed', iterations = 30): string {
  const r = {
    name: 'BrowserTypingLatency',
    variant,
    iterations,
    p50_ms: 42.5,
    p95_ms: 80.1,
    p99_ms: 95.2,
    max_ms: 120.7,
    mean_ms: 50.3,
    min_ms: 0,
    stddev_ms: 0,
    gc_pauses: 0,
    gc_pause_total_us: 0,
    timestamp: '2026-09-11T10:00:00.000Z',
    nproc: 8,
    userAgent: 'Mozilla/5.0 ... HeadlessChrome/140.0.0.0',
  };
  return `BENCH_RESULT_JSON: ${JSON.stringify(r)}`;
}

test('parseBenchResultLine accepts idle and stressed lines', () => {
  for (const variant of ['idle', 'stressed'] as const) {
    const parsed = parseBenchResultLine(resultLine(variant));
    assert.ok(parsed, `expected a result for ${variant}`);
    assert.equal(parsed.name, 'BrowserTypingLatency');
    assert.equal(parsed.variant, variant);
    assert.equal(parsed.iterations, 30);
    assert.equal(parsed.p50_ms, 42.5);
    assert.equal(parsed.nproc, 8);
    assert.equal(parsed.userAgent, 'Mozilla/5.0 ... HeadlessChrome/140.0.0.0');
  }
});

test('parseBenchResultLine rejects non-bench, malformed, and incomplete lines', () => {
  assert.equal(parseBenchResultLine('  ✓ idle typing latency (12.3s)'), null);
  assert.equal(parseBenchResultLine('some random output'), null);
  assert.equal(parseBenchResultLine('BENCH_RESULT_JSON: {not json'), null);
  // Wrong benchmark name
  assert.equal(
    parseBenchResultLine(resultLine('idle').replace('BrowserTypingLatency', 'Other')),
    null
  );
  // Missing percentile field
  assert.equal(parseBenchResultLine(resultLine('idle').replace('"p99_ms":95.2,', '')), null);
  // Non-numeric iterations
  assert.equal(
    parseBenchResultLine(resultLine('idle').replace('"iterations":30', '"iterations":"30"')),
    null
  );
  // Unknown variant
  assert.equal(parseBenchResultLine(resultLine('idle').replace('"idle"', '"warmup"')), null);
});

test('validateBrowserBenchResults requires every variant once with samples', () => {
  const idle = parseBenchResultLine(resultLine('idle'))!;
  const stressed = parseBenchResultLine(resultLine('stressed'))!;

  assert.deepEqual(validateBrowserBenchResults([idle, stressed]), { valid: true, problems: [] });

  const missingVariant = validateBrowserBenchResults([idle]);
  assert.equal(missingVariant.valid, false);
  assert.match(missingVariant.problems.join('; '), /missing variant "stressed"/);

  const zeroSamples = validateBrowserBenchResults([
    idle,
    parseBenchResultLine(resultLine('stressed', 0))!,
  ]);
  assert.equal(zeroSamples.valid, false);
  assert.match(zeroSamples.problems.join('; '), /variant "stressed" recorded 0 samples/);

  const duplicated = validateBrowserBenchResults([idle, idle, stressed]);
  assert.equal(duplicated.valid, false);
  assert.match(duplicated.problems.join('; '), /variant "idle" appears 2 times/);
});

test('buildBrowserBenchReport merges variants with host and container metadata', () => {
  const results: BrowserBenchResult[] = [
    parseBenchResultLine(resultLine('idle'))!,
    parseBenchResultLine(resultLine('stressed'))!,
  ];
  const report = buildBrowserBenchReport(results, {
    gitCommit: 'abc1234',
    runtime: 'docker',
    baseImage: 'schmux-scenarios-base',
    image: 'schmux-bench-4242',
    hostOs: 'Darwin 25.6.0',
    hostArch: 'arm64',
    hostCpuModel: 'Apple M3 Pro',
    hostCpuCount: 12,
  });

  assert.equal(report.profile, 'docker-scenario');
  assert.equal(report.gitCommit, 'abc1234');
  assert.equal(report.host.cpuCount, 12);
  assert.equal(report.container.runtime, 'docker');
  assert.equal(report.container.image, 'schmux-bench-4242');
  assert.equal(report.container.nproc, 8);
  assert.equal(report.container.chromiumUserAgent, 'Mozilla/5.0 ... HeadlessChrome/140.0.0.0');
  assert.equal(report.container.cpusPinned, false);
  assert.deepEqual(
    report.variants.map((v) => v.variant),
    ['idle', 'stressed']
  );
  assert.equal(report.variants[0].p50_ms, 42.5);
  assert.ok(report.generatedAt);
});
