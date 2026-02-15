import { describe, it, expect, beforeEach, vi } from 'vitest';
import { inputLatency } from './inputLatency';

describe('InputLatencyTracker', () => {
  beforeEach(() => {
    inputLatency.reset();
  });

  it('getStats returns null when no samples', () => {
    expect(inputLatency.getStats()).toBeNull();
  });

  it('computes correct stats after adding samples', () => {
    // Simulate a few round-trips
    const mockNow = vi.spyOn(performance, 'now');

    // Sample 1: 10ms
    mockNow.mockReturnValueOnce(100);
    inputLatency.markSent();
    mockNow.mockReturnValueOnce(110);
    inputLatency.markReceived();

    // Sample 2: 20ms
    mockNow.mockReturnValueOnce(200);
    inputLatency.markSent();
    mockNow.mockReturnValueOnce(220);
    inputLatency.markReceived();

    // Sample 3: 30ms
    mockNow.mockReturnValueOnce(300);
    inputLatency.markSent();
    mockNow.mockReturnValueOnce(330);
    inputLatency.markReceived();

    const stats = inputLatency.getStats();
    expect(stats).not.toBeNull();
    expect(stats!.count).toBe(3);
    expect(stats!.avg).toBe(20);
    expect(stats!.max).toBe(30);
    // median of [10, 20, 30] at index floor(3/2)=1 → 20
    expect(stats!.median).toBe(20);

    mockNow.mockRestore();
  });

  it('markReceived without markSent adds no sample', () => {
    inputLatency.markReceived();
    expect(inputLatency.getStats()).toBeNull();
  });

  it('markRenderTime adds to renderSamples', () => {
    inputLatency.markRenderTime(5);
    inputLatency.markRenderTime(10);

    const renderStats = inputLatency.getRenderStats();
    expect(renderStats).not.toBeNull();
    expect(renderStats!.count).toBe(2);
  });

  it('reset clears all samples', () => {
    const mockNow = vi.spyOn(performance, 'now');
    // Use non-zero start value since 0 is the sentinel for "no pending send"
    mockNow.mockReturnValueOnce(100);
    inputLatency.markSent();
    mockNow.mockReturnValueOnce(110);
    inputLatency.markReceived();
    inputLatency.markRenderTime(5);
    mockNow.mockRestore();

    expect(inputLatency.getStats()).not.toBeNull();
    expect(inputLatency.getRenderStats()).not.toBeNull();

    inputLatency.reset();
    expect(inputLatency.getStats()).toBeNull();
    expect(inputLatency.getRenderStats()).toBeNull();
  });

  it('version increments on markReceived and reset', () => {
    const v0 = inputLatency.getVersion();

    const mockNow = vi.spyOn(performance, 'now');
    mockNow.mockReturnValueOnce(100);
    inputLatency.markSent();
    mockNow.mockReturnValueOnce(110);
    inputLatency.markReceived();
    mockNow.mockRestore();

    expect(inputLatency.getVersion()).toBe(v0 + 1);

    inputLatency.reset();
    expect(inputLatency.getVersion()).toBe(v0 + 2);
  });

  it('caps samples at MAX_SAMPLES (1000)', () => {
    const mockNow = vi.spyOn(performance, 'now');
    for (let i = 0; i < 1100; i++) {
      mockNow.mockReturnValueOnce(i * 100);
      inputLatency.markSent();
      mockNow.mockReturnValueOnce(i * 100 + 5);
      inputLatency.markReceived();
    }
    mockNow.mockRestore();

    expect(inputLatency.samples.length).toBe(1000);
  });

  it('getDistribution returns buckets', () => {
    const mockNow = vi.spyOn(performance, 'now');

    // Add samples at 5ms, 10ms, 15ms (use non-zero start to avoid sentinel)
    for (const rtt of [5, 10, 15]) {
      mockNow.mockReturnValueOnce(100);
      inputLatency.markSent();
      mockNow.mockReturnValueOnce(100 + rtt);
      inputLatency.markReceived();
    }
    mockNow.mockRestore();

    const dist = inputLatency.getDistribution();
    expect(dist).not.toBeNull();
    expect(dist!.buckets).toBeDefined();
    expect(dist!.maxCount).toBeGreaterThan(0);
    expect(dist!.maxMs).toBeGreaterThanOrEqual(10);
  });

  it('getDistribution returns null when no samples', () => {
    expect(inputLatency.getDistribution()).toBeNull();
  });
});
