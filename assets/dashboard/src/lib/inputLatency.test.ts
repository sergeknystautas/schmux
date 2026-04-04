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

  it('updateServerLatency stores data and increments version', () => {
    const v0 = inputLatency.getVersion();
    const serverData = {
      dispatchP50: 0.5,
      dispatchP99: 1.2,
      sendKeysP50: 2.0,
      sendKeysP99: 5.0,
      echoP50: 3.0,
      echoP99: 8.0,
      frameSendP50: 0.1,
      frameSendP99: 0.3,
      sampleCount: 42,
    };
    inputLatency.updateServerLatency(serverData);

    expect(inputLatency.getServerLatency()).toBe(serverData);
    expect(inputLatency.getVersion()).toBe(v0 + 1);
  });

  it('getBreakdown returns null without enough paired segment samples', () => {
    // Add 4 RTT samples but no server segment tuples — below the 5 minimum
    const mockNow = vi.spyOn(performance, 'now');
    for (let i = 0; i < 4; i++) {
      mockNow.mockReturnValueOnce(100);
      inputLatency.markSent();
      mockNow.mockReturnValueOnce(130);
      inputLatency.markReceived();
    }
    mockNow.mockRestore();

    expect(inputLatency.getBreakdown('typical')).toBeNull();
  });

  it('reset clears serverLatency', () => {
    inputLatency.updateServerLatency({
      dispatchP50: 1,
      dispatchP99: 2,
      sendKeysP50: 1,
      sendKeysP99: 2,
      echoP50: 1,
      echoP99: 2,
      frameSendP50: 1,
      frameSendP99: 2,
      sampleCount: 5,
    });
    expect(inputLatency.getServerLatency()).not.toBeNull();

    inputLatency.reset();
    expect(inputLatency.getServerLatency()).toBeNull();
  });

  it('recordServerSegments stores paired server-side segment tuples', () => {
    const tuple1 = { dispatch: 1, sendKeys: 2, echo: 1.5, frameSend: 0.7, total: 5.2 };
    const tuple2 = { dispatch: 0.8, sendKeys: 1.5, echo: 0.5, frameSend: 0.3, total: 3.1 };
    inputLatency.recordServerSegments(tuple1);
    inputLatency.recordServerSegments(tuple2);
    expect(inputLatency.serverSegmentSamples).toEqual([tuple1, tuple2]);
  });

  it('recordServerSegments fires MessageChannel probe and stores receiveLag on the tuple', () => {
    const originalMC = globalThis.MessageChannel;
    class MockMessageChannel {
      port1 = { onmessage: null as ((ev: any) => void) | null };
      port2 = {
        postMessage: () => {
          if (this.port1.onmessage) {
            this.port1.onmessage({} as any);
          }
        },
      };
    }
    globalThis.MessageChannel = MockMessageChannel as any;

    const mockNow = vi.spyOn(performance, 'now');
    mockNow.mockReturnValueOnce(500); // probe start
    mockNow.mockReturnValueOnce(503); // handler fires: lag = 3ms
    const tuple = { dispatch: 1, sendKeys: 2, echo: 3, frameSend: 0.5, total: 6.5 };
    inputLatency.recordServerSegments(tuple);

    mockNow.mockRestore();
    globalThis.MessageChannel = originalMC;

    const stored = inputLatency.serverSegmentSamples[0];
    expect((stored as any).receiveLag).toBe(3);
  });

  it('reset clears serverSegmentSamples and lagSamples', () => {
    inputLatency.recordServerSegments({
      dispatch: 1,
      sendKeys: 2,
      echo: 1,
      frameSend: 0.5,
      total: 4.5,
    });
    inputLatency.reset();
    expect(inputLatency.serverSegmentSamples).toEqual([]);
    expect(inputLatency.lagSamples).toEqual([]);
    expect(inputLatency.framesBetweenSamples).toEqual([]);
    expect(inputLatency.handleOutputTimeSamples).toEqual([]);
  });

  it('getBreakdown returns segments from paired tuples (cohort median)', () => {
    const originalMC = globalThis.MessageChannel;
    class MockMC {
      port1 = { onmessage: null as ((ev: any) => void) | null };
      port2 = {
        postMessage: () => {
          if (this.port1.onmessage) this.port1.onmessage({} as any);
        },
      };
    }
    globalThis.MessageChannel = MockMC as any;

    const mockNow = vi.spyOn(performance, 'now');
    // 20 RTT samples: all 30ms, 20 render samples: all 2ms
    for (let i = 0; i < 20; i++) {
      // markSent: 3 calls
      mockNow.mockReturnValueOnce(100);
      mockNow.mockReturnValueOnce(100);
      mockNow.mockReturnValueOnce(101);
      // markReceived: 1 call
      mockNow.mockReturnValueOnce(130);
      inputLatency.markSent();
      inputLatency.markReceived();
      inputLatency.markRenderTime(2);
      // recordServerSegments: 2 calls
      mockNow.mockReturnValueOnce(200);
      mockNow.mockReturnValueOnce(201);
      inputLatency.recordServerSegments({
        dispatch: 1,
        sendKeys: 2,
        echo: 3,
        frameSend: 0.5,
        total: 6.5,
      });
    }
    mockNow.mockRestore();
    globalThis.MessageChannel = originalMC;

    const breakdown = inputLatency.getBreakdown('typical');
    expect(breakdown).not.toBeNull();
    // Cohort medians of uniform data
    expect(breakdown!.handler).toBe(1);
    expect(breakdown!.tmuxCmd).toBe(2);
    expect(breakdown!.paneOutput).toBe(3);
    expect(breakdown!.wsWrite).toBe(0.5);
    expect(breakdown!.xterm).toBe(2);
    expect(breakdown!.total).toBe(30);
    // receiveLag was set to 1ms by MockMC probe (201-200), network = 30 - 6.5 - 2 - 1 = 20.5
    expect(breakdown!.jsQueue).toBe(1);
    expect(breakdown!.network).toBe(20.5);
  });

  it('getBreakdown uses per-tuple receiveLag for jsQueue', () => {
    // Directly populate arrays to avoid MessageChannel mocking complexity.
    // 20 uniform samples at 30ms RTT, 2ms render, receiveLag=5 on each tuple.
    for (let i = 0; i < 20; i++) {
      inputLatency.samples.push(30);
      inputLatency.renderSamples.push(2);
      inputLatency.serverSegmentSamples.push({
        dispatch: 1,
        sendKeys: 2,
        echo: 3,
        frameSend: 0.5,
        total: 6.5,
        receiveLag: 5,
      });
    }

    const breakdown = inputLatency.getBreakdown('typical');
    expect(breakdown).not.toBeNull();
    // jsQueue comes from per-tuple receiveLag (5ms)
    // network = 30 - 6.5 - 2 - 5 = 16.5
    expect(breakdown!.jsQueue).toBe(5);
    expect(breakdown!.network).toBe(16.5);
  });

  it('getBreakdown discards mispaired samples where server > RTT', () => {
    // 5 samples with RTT of 5ms — server segments sum to 9ms (impossible, mispairing)
    for (let i = 0; i < 5; i++) {
      inputLatency.samples.push(5);
      inputLatency.renderSamples.push(0);
      inputLatency.serverSegmentSamples.push({
        dispatch: 2,
        sendKeys: 3,
        echo: 3,
        frameSend: 1,
        total: 9,
      });
    }

    // All samples filtered out → returns null
    expect(inputLatency.getBreakdown('typical')).toBeNull();
  });

  it('getBreakdown clamps network to zero when render exceeds infra budget', () => {
    // 20 samples: RTT=10ms, render=8ms, server=3ms
    // measured = 3 + 8 + 0 (no receiveLag) = 11 > 10 → network clamped to 0
    for (let i = 0; i < 20; i++) {
      inputLatency.samples.push(10);
      inputLatency.renderSamples.push(8);
      inputLatency.serverSegmentSamples.push({
        dispatch: 0.5,
        sendKeys: 1,
        echo: 1,
        frameSend: 0.5,
        total: 3,
      });
    }

    const breakdown = inputLatency.getBreakdown('typical');
    expect(breakdown).not.toBeNull();
    // No receiveLag → jsQueue = 0, network = max(0, 10 - 3 - 8 - 0) = 0
    expect(breakdown!.jsQueue).toBe(0);
    expect(breakdown!.network).toBe(0);
  });

  it('event loop lag tracker records samples via markSent', async () => {
    // Mock MessageChannel to synchronously fire the handler
    const originalMC = globalThis.MessageChannel;
    class MockMessageChannel {
      port1 = { onmessage: null as ((ev: any) => void) | null };
      port2 = {
        postMessage: () => {
          // Synchronously invoke the handler to simulate message delivery
          if (this.port1.onmessage) {
            this.port1.onmessage({} as any);
          }
        },
      };
    }
    globalThis.MessageChannel = MockMessageChannel as any;

    const mockNow = vi.spyOn(performance, 'now');
    // markSent reads performance.now() twice: once for lastInputTime, once for sentTime
    mockNow.mockReturnValueOnce(100); // lastInputTime
    mockNow.mockReturnValueOnce(100); // sentTime in lag probe
    mockNow.mockReturnValueOnce(102); // handler fires: lag = 2ms
    inputLatency.markSent();

    mockNow.mockRestore();
    globalThis.MessageChannel = originalMC;

    expect(inputLatency.lagSamples.length).toBe(1);
    expect(inputLatency.lagSamples[0]).toBe(2);
  });

  it('recordFrameProcessed increments counter, markReceived captures and resets', () => {
    const mockNow = vi.spyOn(performance, 'now');
    mockNow.mockReturnValueOnce(100);
    inputLatency.markSent();

    // Simulate 5 output frames processed between send and receive
    inputLatency.recordFrameProcessed();
    inputLatency.recordFrameProcessed();
    inputLatency.recordFrameProcessed();
    inputLatency.recordFrameProcessed();
    inputLatency.recordFrameProcessed();

    mockNow.mockReturnValueOnce(130);
    inputLatency.markReceived();
    mockNow.mockRestore();

    expect(inputLatency.framesBetweenSamples).toEqual([5]);

    // Counter should be reset after markReceived
    const mockNow2 = vi.spyOn(performance, 'now');
    mockNow2.mockReturnValueOnce(200);
    inputLatency.markSent();
    mockNow2.mockReturnValueOnce(210);
    inputLatency.markReceived();
    mockNow2.mockRestore();

    // Second sample should be 0 (no frames recorded between)
    expect(inputLatency.framesBetweenSamples).toEqual([5, 0]);
  });

  it('recordHandleOutputTime stores samples', () => {
    inputLatency.recordHandleOutputTime(1.5);
    inputLatency.recordHandleOutputTime(3.2);
    inputLatency.recordHandleOutputTime(0.8);
    expect(inputLatency.handleOutputTimeSamples).toEqual([1.5, 3.2, 0.8]);
  });

  it('markReceived no longer fires receive-time lag probe', () => {
    const originalMC = globalThis.MessageChannel;
    class MockMessageChannel {
      port1 = { onmessage: null as ((ev: any) => void) | null };
      port2 = {
        postMessage: () => {
          if (this.port1.onmessage) {
            this.port1.onmessage({} as any);
          }
        },
      };
    }
    globalThis.MessageChannel = MockMessageChannel as any;

    const mockNow = vi.spyOn(performance, 'now');
    // markSent: lastInputTime=100, sentTime=100, lagHandler=102
    mockNow.mockReturnValueOnce(100);
    mockNow.mockReturnValueOnce(100);
    mockNow.mockReturnValueOnce(102);
    inputLatency.markSent();

    // markReceived: only reads performance.now() once for rtt calculation
    mockNow.mockReturnValueOnce(130); // performance.now() → now
    inputLatency.markReceived();

    mockNow.mockRestore();
    globalThis.MessageChannel = originalMC;

    // Probe moved to recordServerSegments — no separate receiveLagSamples array
    // Verify no server segment samples were added by markReceived
    expect(inputLatency.serverSegmentSamples.length).toBe(0);
  });

  it('getWireContext returns correct P50/P99 values', () => {
    // No data → null
    expect(inputLatency.getWireContext()).toBeNull();

    // Add some samples
    inputLatency.framesBetweenSamples = [0, 2, 4, 6, 8, 10, 12, 14, 16, 18];
    inputLatency.handleOutputTimeSamples = [1, 1, 1, 1, 1, 2, 2, 2, 2, 10];
    const lags = [0.5, 0.5, 0.5, 0.5, 0.5, 1, 1, 1, 1, 50];
    for (let i = 0; i < lags.length; i++) {
      inputLatency.serverSegmentSamples.push({
        dispatch: 1,
        sendKeys: 1,
        echo: 1,
        frameSend: 0.5,
        total: 3.5,
        receiveLag: lags[i],
      });
    }

    const ctx = inputLatency.getWireContext();
    expect(ctx).not.toBeNull();
    // With 10 samples: P50 = index 5, P99 = index 9
    // framesBetween sorted: [0,2,4,6,8,10,12,14,16,18] → P50=10, P99=18
    expect(ctx!.framesBetweenP50).toBe(10);
    expect(ctx!.framesBetweenP99).toBe(18);
    // handleOutputMs sorted: [1,1,1,1,1,2,2,2,2,10] → P50=2, P99=10
    expect(ctx!.handleOutputMsP50).toBe(2);
    expect(ctx!.handleOutputMsP99).toBe(10);
    // receiveLag sorted: [0.5,0.5,0.5,0.5,0.5,1,1,1,1,50] → P50=1, P99=50
    expect(ctx!.receiveLagP50).toBe(1);
    expect(ctx!.receiveLagP99).toBe(50);
  });

  it('markReceived discards samples when lastInputTime is stale (>2s)', () => {
    const originalMC = globalThis.MessageChannel;
    class MockMessageChannel {
      port1 = { onmessage: null as ((ev: any) => void) | null };
      port2 = {
        postMessage: () => {
          if (this.port1.onmessage) {
            this.port1.onmessage({} as any);
          }
        },
      };
    }
    globalThis.MessageChannel = MockMessageChannel as any;

    const mockNow = vi.spyOn(performance, 'now');
    // markSent: lastInputTime, sentTime, lagHandler
    mockNow.mockReturnValueOnce(100); // lastInputTime
    mockNow.mockReturnValueOnce(100); // sentTime in lag probe
    mockNow.mockReturnValueOnce(102); // lag handler fires
    inputLatency.markSent();
    // 2500ms later — exceeds 2s staleness threshold
    // markReceived: only now is read (staleness guard returns early before lag probe)
    mockNow.mockReturnValueOnce(2600); // performance.now() → now (rtt > 2000, early return)
    inputLatency.markReceived();

    mockNow.mockRestore();
    globalThis.MessageChannel = originalMC;

    // Sample should have been discarded
    expect(inputLatency.getStats()).toBeNull();
    expect(inputLatency.samples.length).toBe(0);
  });

  it('markReceived keeps samples within staleness threshold', () => {
    const originalMC = globalThis.MessageChannel;
    class MockMessageChannel {
      port1 = { onmessage: null as ((ev: any) => void) | null };
      port2 = {
        postMessage: () => {
          if (this.port1.onmessage) {
            this.port1.onmessage({} as any);
          }
        },
      };
    }
    globalThis.MessageChannel = MockMessageChannel as any;

    const mockNow = vi.spyOn(performance, 'now');
    // markSent: lastInputTime, sentTime, lagHandler
    mockNow.mockReturnValueOnce(100); // lastInputTime
    mockNow.mockReturnValueOnce(100); // sentTime in lag probe
    mockNow.mockReturnValueOnce(102); // lag handler fires
    inputLatency.markSent();
    // 1500ms later — within 2s threshold
    // markReceived: now=1600 (rtt calculation only, no receive lag probe)
    mockNow.mockReturnValueOnce(1600); // performance.now() → now
    inputLatency.markReceived();

    mockNow.mockRestore();
    globalThis.MessageChannel = originalMC;

    expect(inputLatency.samples.length).toBe(1);
  });

  it('getBreakdown returns segmentSum field', () => {
    // 20 uniform samples to ensure IQR cohort has >= 5 members
    for (let i = 0; i < 20; i++) {
      inputLatency.samples.push(30);
      inputLatency.renderSamples.push(2);
      inputLatency.serverSegmentSamples.push({
        dispatch: 1,
        sendKeys: 2,
        echo: 3,
        frameSend: 0.5,
        total: 6.5,
      });
    }

    const breakdown = inputLatency.getBreakdown('typical');
    expect(breakdown).not.toBeNull();
    expect(breakdown!.segmentSum).toBeDefined();
    expect(breakdown!.segmentSum).toBeGreaterThan(0);
    // segmentSum should equal sum of all segments
    const sum =
      breakdown!.network +
      breakdown!.jsQueue +
      breakdown!.handler +
      breakdown!.wsWrite +
      breakdown!.xterm +
      breakdown!.tmuxCmd +
      breakdown!.paneOutput;
    expect(breakdown!.segmentSum).toBeCloseTo(sum, 5);
  });
});

describe('getBreakdown cohort-median', () => {
  const originalMC = globalThis.MessageChannel;
  class MockMC {
    port1 = { onmessage: null as ((ev: any) => void) | null };
    port2 = {
      postMessage: () => {
        if (this.port1.onmessage) {
          this.port1.onmessage({} as any);
        }
      },
    };
  }

  beforeEach(() => {
    inputLatency.reset();
    globalThis.MessageChannel = MockMC as any;
  });
  afterEach(() => {
    globalThis.MessageChannel = originalMC;
  });

  function addSamples(count: number, rttBase: number, rttJitter: number) {
    const mockNow = vi.spyOn(performance, 'now');
    for (let i = 0; i < count; i++) {
      const rtt = rttBase + (i % 2 === 0 ? rttJitter : -rttJitter);
      // markSent: 3 calls
      mockNow.mockReturnValueOnce(100);
      mockNow.mockReturnValueOnce(100);
      mockNow.mockReturnValueOnce(101);
      // markReceived: 1 call
      mockNow.mockReturnValueOnce(100 + rtt);
      inputLatency.markSent();
      inputLatency.markReceived();
      inputLatency.markRenderTime(2);
      const serverTotal = rtt * 0.3;
      // recordServerSegments: 2 calls
      mockNow.mockReturnValueOnce(200);
      mockNow.mockReturnValueOnce(201);
      inputLatency.recordServerSegments({
        dispatch: serverTotal * 0.1,
        sendKeys: serverTotal * 0.4,
        echo: serverTotal * 0.4,
        frameSend: serverTotal * 0.1,
        total: serverTotal,
      });
    }
    mockNow.mockRestore();
  }

  it('typical breakdown uses bottom 75% of valid tuples', () => {
    addSamples(20, 30, 2);
    const breakdown = inputLatency.getBreakdown('typical');
    expect(breakdown).not.toBeNull();
    // All RTTs are near 30ms, so typical total should be close to 30
    expect(breakdown!.total).toBeGreaterThanOrEqual(25);
    expect(breakdown!.total).toBeLessThanOrEqual(35);
  });

  it('outlier breakdown uses top 25% of valid tuples', () => {
    // 16 samples at 30ms, 4 at 100ms. Top 25% = last 5 tuples (sorted).
    // The 4 high-RTT tuples land in the top quarter.
    addSamples(16, 30, 2);
    addSamples(4, 100, 2);
    const breakdown = inputLatency.getBreakdown('outlier');
    expect(breakdown).not.toBeNull();
    // Outlier total should be in the high range, not 30ms
    expect(breakdown!.total).toBeGreaterThan(50);
  });

  it('returns null for outlier when fewer than 3 valid tuples total', () => {
    // With 8 valid tuples, top 25% = 2 tuples (below minimum of 3).
    addSamples(8, 30, 2);
    const breakdown = inputLatency.getBreakdown('outlier');
    expect(breakdown).toBeNull();
  });

  it('segmentSum equals sum of all segment medians', () => {
    addSamples(20, 30, 2);
    const breakdown = inputLatency.getBreakdown('typical');
    expect(breakdown).not.toBeNull();
    const expectedSum =
      breakdown!.network +
      breakdown!.jsQueue +
      breakdown!.handler +
      breakdown!.wsWrite +
      breakdown!.xterm +
      breakdown!.tmuxCmd +
      breakdown!.paneOutput;
    expect(breakdown!.segmentSum).toBeCloseTo(expectedSum, 5);
  });

  it('residual (network) is per-tuple clamped to zero then medianed', () => {
    addSamples(20, 30, 2);
    const breakdown = inputLatency.getBreakdown('typical');
    expect(breakdown).not.toBeNull();
    expect(breakdown!.network).toBeGreaterThanOrEqual(0);
  });

  it('discards mispaired tuples where serverTotal > clientRTT', () => {
    // Directly populate: all 5 tuples have serverTotal (10) > clientRTT (5)
    for (let i = 0; i < 5; i++) {
      inputLatency.samples.push(5);
      inputLatency.renderSamples.push(1);
      inputLatency.serverSegmentSamples.push({
        dispatch: 2.5,
        sendKeys: 2.5,
        echo: 2.5,
        frameSend: 2.5,
        total: 10,
      });
    }
    expect(inputLatency.getBreakdown('typical')).toBeNull();
  });

  it('uses per-tuple receiveLag for jsQueue when available', () => {
    // 20 samples with explicit receiveLag=5 on each tuple
    for (let i = 0; i < 20; i++) {
      inputLatency.samples.push(30);
      inputLatency.renderSamples.push(2);
      inputLatency.serverSegmentSamples.push({
        dispatch: 1,
        sendKeys: 2,
        echo: 3,
        frameSend: 0.5,
        total: 6.5,
        receiveLag: 5,
      });
    }
    const breakdown = inputLatency.getBreakdown('typical');
    expect(breakdown).not.toBeNull();
    expect(breakdown!.jsQueue).toBe(5);
    // network = 30 - 6.5 - 2 - 5 = 16.5
    expect(breakdown!.network).toBe(16.5);
  });
});
