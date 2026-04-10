import { describe, it, expect } from 'vitest';
import { computeDistribution, TmuxHealthData } from './tmuxHealth';

function makeHealthData(samples: number[]): TmuxHealthData {
  return {
    samples,
    p50_us: 0,
    p99_us: 0,
    max_rtt_us: 0,
    count: samples.length,
    errors: 0,
    last_us: 0,
    uptime_s: 0,
  };
}

describe('computeDistribution', () => {
  it('returns null for fewer than 3 samples', () => {
    expect(computeDistribution(makeHealthData([]))).toBeNull();
    expect(computeDistribution(makeHealthData([10]))).toBeNull();
    expect(computeDistribution(makeHealthData([10, 20]))).toBeNull();
  });

  it('returns valid distribution for exactly 3 samples', () => {
    const result = computeDistribution(makeHealthData([10, 20, 30]));
    expect(result).not.toBeNull();
    expect(result!.buckets.length).toBeGreaterThan(0);
    expect(result!.maxCount).toBeGreaterThan(0);
    expect(result!.bucketUs).toBe(10);
    // bucket counts must sum to sample count
    const sum = result!.buckets.reduce((a, b) => a + b, 0);
    expect(sum).toBe(3);
  });

  it('bucket counts sum to sample count for many samples', () => {
    const samples = Array.from({ length: 100 }, (_, i) => i * 5);
    const result = computeDistribution(makeHealthData(samples));
    expect(result).not.toBeNull();
    const sum = result!.buckets.reduce((a, b) => a + b, 0);
    expect(sum).toBe(100);
  });

  it('handles all same value', () => {
    const samples = [50, 50, 50, 50, 50];
    const result = computeDistribution(makeHealthData(samples));
    expect(result).not.toBeNull();
    // All samples land in one bucket, so maxCount equals sample count
    expect(result!.maxCount).toBe(5);
    const nonZeroBuckets = result!.buckets.filter((c) => c > 0);
    expect(nonZeroBuckets.length).toBe(1);
  });

  it('handles wide range of values', () => {
    const samples = [10, 500, 1000, 5000, 10000];
    const result = computeDistribution(makeHealthData(samples));
    expect(result).not.toBeNull();
    expect(result!.maxUs).toBeGreaterThanOrEqual(100);
    expect(result!.buckets.length).toBe(Math.round(result!.maxUs / result!.bucketUs));
    const sum = result!.buckets.reduce((a, b) => a + b, 0);
    expect(sum).toBe(5);
  });
});
