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
    // Add RTT samples but no server segment tuples
    const mockNow = vi.spyOn(performance, 'now');
    mockNow.mockReturnValueOnce(100);
    inputLatency.markSent();
    mockNow.mockReturnValueOnce(130);
    inputLatency.markReceived();
    mockNow.mockRestore();

    expect(inputLatency.getBreakdown('p50')).toBeNull();
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
    expect(inputLatency.receiveLagSamples).toEqual([]);
    expect(inputLatency.framesBetweenSamples).toEqual([]);
    expect(inputLatency.handleOutputTimeSamples).toEqual([]);
  });

  it('getBreakdown returns segments from the same keystroke (paired)', () => {
    const mockNow = vi.spyOn(performance, 'now');
    // 3 RTT samples: 30ms each
    for (let i = 0; i < 3; i++) {
      mockNow.mockReturnValueOnce(100);
      inputLatency.markSent();
      mockNow.mockReturnValueOnce(130);
      inputLatency.markReceived();
    }
    // 3 render samples: 2ms each
    inputLatency.markRenderTime(2);
    inputLatency.markRenderTime(2);
    inputLatency.markRenderTime(2);
    mockNow.mockRestore();

    // 3 paired server segment tuples: dispatch=1, sendKeys=2, echo=3, frameSend=0.5
    const tuple = { dispatch: 1, sendKeys: 2, echo: 3, frameSend: 0.5, total: 6.5 };
    inputLatency.recordServerSegments(tuple);
    inputLatency.recordServerSegments(tuple);
    inputLatency.recordServerSegments(tuple);

    const breakdown = inputLatency.getBreakdown('p50');
    expect(breakdown).not.toBeNull();
    // Segments come from the actual keystroke tuple
    expect(breakdown!.dispatch).toBe(1);
    expect(breakdown!.sendKeys).toBe(2);
    expect(breakdown!.echo).toBe(3);
    expect(breakdown!.frameSend).toBe(0.5);
    expect(breakdown!.render).toBe(2);
    expect(breakdown!.total).toBe(30);
    // infra = 30 - 6.5 - 2 = 21.5, no lag → wireResidual=21.5
    expect(breakdown!.eventLoopLag).toBe(0);
    expect(breakdown!.wireResidual).toBe(21.5);
  });

  it('getBreakdown with lag samples subtracts eventLoopLag from wireResidual', () => {
    const mockNow = vi.spyOn(performance, 'now');
    for (let i = 0; i < 3; i++) {
      mockNow.mockReturnValueOnce(100);
      inputLatency.markSent();
      mockNow.mockReturnValueOnce(130);
      inputLatency.markReceived();
    }
    inputLatency.markRenderTime(2);
    inputLatency.markRenderTime(2);
    inputLatency.markRenderTime(2);
    mockNow.mockRestore();

    // Inject receiveLagSamples (preferred over lagSamples)
    inputLatency.receiveLagSamples = [5, 5, 5];

    const tuple = { dispatch: 1, sendKeys: 2, echo: 3, frameSend: 0.5, total: 6.5 };
    inputLatency.recordServerSegments(tuple);
    inputLatency.recordServerSegments(tuple);
    inputLatency.recordServerSegments(tuple);

    const breakdown = inputLatency.getBreakdown('p50');
    expect(breakdown).not.toBeNull();
    // infra = 30 - 6.5 - 2 = 21.5
    // eventLoopLag P50 of [5,5,5] = 5
    // wireResidual = 21.5 - 5 = 16.5
    expect(breakdown!.eventLoopLag).toBe(5);
    expect(breakdown!.wireResidual).toBe(16.5);
  });

  it('getBreakdown picks the keystroke at the P50 rank', () => {
    const mockNow = vi.spyOn(performance, 'now');
    // 5 RTT samples with varying latencies: 10, 20, 30, 40, 50
    const rtts = [10, 20, 30, 40, 50];
    for (const rtt of rtts) {
      mockNow.mockReturnValueOnce(100);
      inputLatency.markSent();
      mockNow.mockReturnValueOnce(100 + rtt);
      inputLatency.markReceived();
    }
    for (let i = 0; i < 5; i++) inputLatency.markRenderTime(1);
    mockNow.mockRestore();

    // Each keystroke has different server segments proportional to its RTT
    inputLatency.recordServerSegments({
      dispatch: 0.5,
      sendKeys: 1,
      echo: 1,
      frameSend: 0.5,
      total: 3,
    });
    inputLatency.recordServerSegments({
      dispatch: 1,
      sendKeys: 2,
      echo: 2,
      frameSend: 1,
      total: 6,
    });
    inputLatency.recordServerSegments({
      dispatch: 1.5,
      sendKeys: 3,
      echo: 3,
      frameSend: 1.5,
      total: 9,
    });
    inputLatency.recordServerSegments({
      dispatch: 2,
      sendKeys: 4,
      echo: 4,
      frameSend: 2,
      total: 12,
    });
    inputLatency.recordServerSegments({
      dispatch: 2.5,
      sendKeys: 5,
      echo: 5,
      frameSend: 2.5,
      total: 15,
    });

    const breakdown = inputLatency.getBreakdown('p50');
    expect(breakdown).not.toBeNull();
    // P50 index = floor(5/2) = 2, which after sorting by RTT is the 30ms keystroke
    expect(breakdown!.total).toBe(30);
    expect(breakdown!.dispatch).toBe(1.5);
    expect(breakdown!.sendKeys).toBe(3);
    expect(breakdown!.echo).toBe(3);
    expect(breakdown!.frameSend).toBe(1.5);
    expect(breakdown!.render).toBe(1);
  });

  it('getBreakdown discards mispaired samples where server > RTT', () => {
    const mockNow = vi.spyOn(performance, 'now');
    // RTT of 5ms — server segments sum to 9ms (impossible, mispairing)
    for (let i = 0; i < 3; i++) {
      mockNow.mockReturnValueOnce(100);
      inputLatency.markSent();
      mockNow.mockReturnValueOnce(105);
      inputLatency.markReceived();
    }
    for (let i = 0; i < 3; i++) inputLatency.markRenderTime(0);
    mockNow.mockRestore();

    const tuple = { dispatch: 2, sendKeys: 3, echo: 3, frameSend: 1, total: 9 };
    inputLatency.recordServerSegments(tuple);
    inputLatency.recordServerSegments(tuple);
    inputLatency.recordServerSegments(tuple);

    // All samples filtered out → returns null
    expect(inputLatency.getBreakdown('p50')).toBeNull();
  });

  it('getBreakdown clamps wireResidual to zero when render exceeds infra budget', () => {
    const mockNow = vi.spyOn(performance, 'now');
    // RTT of 10ms
    for (let i = 0; i < 3; i++) {
      mockNow.mockReturnValueOnce(100);
      inputLatency.markSent();
      mockNow.mockReturnValueOnce(110);
      inputLatency.markReceived();
    }
    // Large render (8ms) — exceeds remaining budget after server (3ms)
    for (let i = 0; i < 3; i++) inputLatency.markRenderTime(8);
    mockNow.mockRestore();

    // Server segments sum to 3ms (< 10ms RTT, passes filter)
    const tuple = { dispatch: 0.5, sendKeys: 1, echo: 1, frameSend: 0.5, total: 3 };
    inputLatency.recordServerSegments(tuple);
    inputLatency.recordServerSegments(tuple);
    inputLatency.recordServerSegments(tuple);

    const breakdown = inputLatency.getBreakdown('p50');
    expect(breakdown).not.toBeNull();
    // infra = max(0, 10 - 3 - 8) = 0 → no room for wire or lag
    expect(breakdown!.eventLoopLag).toBe(0);
    expect(breakdown!.wireResidual).toBe(0);
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
    // markReceived also fires a MessageChannel probe, so mock the time for it
    mockNow.mockReturnValueOnce(130);
    inputLatency.markReceived();
    mockNow.mockRestore();

    expect(inputLatency.framesBetweenSamples).toEqual([5]);

    // Counter should be reset after markReceived
    const mockNow2 = vi.spyOn(performance, 'now');
    mockNow2.mockReturnValueOnce(200);
    inputLatency.markSent();
    mockNow2.mockReturnValueOnce(210);
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

  it('receive-time lag probe fires and records in receiveLagSamples', () => {
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

    // markReceived: rtt measured at 130, recvTime=130, recvLagHandler=133
    mockNow.mockReturnValueOnce(130); // performance.now() for rtt
    mockNow.mockReturnValueOnce(130); // recvTime in receive lag probe
    mockNow.mockReturnValueOnce(133); // handler fires: receive lag = 3ms
    inputLatency.markReceived();

    mockNow.mockRestore();
    globalThis.MessageChannel = originalMC;

    expect(inputLatency.receiveLagSamples.length).toBe(1);
    expect(inputLatency.receiveLagSamples[0]).toBe(3);
  });

  it('getWireContext returns correct P50/P99 values', () => {
    // No data → null
    expect(inputLatency.getWireContext()).toBeNull();

    // Add some samples
    inputLatency.framesBetweenSamples = [0, 2, 4, 6, 8, 10, 12, 14, 16, 18];
    inputLatency.handleOutputTimeSamples = [1, 1, 1, 1, 1, 2, 2, 2, 2, 10];
    inputLatency.receiveLagSamples = [0.5, 0.5, 0.5, 0.5, 0.5, 1, 1, 1, 1, 50];

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

  it('getBreakdown uses receiveLagSamples for eventLoopLag when available', () => {
    const mockNow = vi.spyOn(performance, 'now');
    for (let i = 0; i < 3; i++) {
      mockNow.mockReturnValueOnce(100);
      inputLatency.markSent();
      mockNow.mockReturnValueOnce(130);
      inputLatency.markReceived();
    }
    inputLatency.markRenderTime(2);
    inputLatency.markRenderTime(2);
    inputLatency.markRenderTime(2);
    mockNow.mockRestore();

    // Both lag arrays present — receiveLagSamples should take priority
    inputLatency.lagSamples = [1, 1, 1]; // send-time: 1ms
    inputLatency.receiveLagSamples = [8, 8, 8]; // receive-time: 8ms

    const tuple = { dispatch: 1, sendKeys: 2, echo: 3, frameSend: 0.5, total: 6.5 };
    inputLatency.recordServerSegments(tuple);
    inputLatency.recordServerSegments(tuple);
    inputLatency.recordServerSegments(tuple);

    const breakdown = inputLatency.getBreakdown('p50');
    expect(breakdown).not.toBeNull();
    // Should use receiveLagSamples (8), not lagSamples (1)
    expect(breakdown!.eventLoopLag).toBe(8);
  });

  it('getBreakdown falls back to lagSamples when receiveLagSamples is empty', () => {
    const mockNow = vi.spyOn(performance, 'now');
    for (let i = 0; i < 3; i++) {
      mockNow.mockReturnValueOnce(100);
      inputLatency.markSent();
      mockNow.mockReturnValueOnce(130);
      inputLatency.markReceived();
    }
    inputLatency.markRenderTime(2);
    inputLatency.markRenderTime(2);
    inputLatency.markRenderTime(2);
    mockNow.mockRestore();

    // Only send-time lag available
    inputLatency.lagSamples = [3, 3, 3];
    inputLatency.receiveLagSamples = [];

    const tuple = { dispatch: 1, sendKeys: 2, echo: 3, frameSend: 0.5, total: 6.5 };
    inputLatency.recordServerSegments(tuple);
    inputLatency.recordServerSegments(tuple);
    inputLatency.recordServerSegments(tuple);

    const breakdown = inputLatency.getBreakdown('p50');
    expect(breakdown).not.toBeNull();
    expect(breakdown!.eventLoopLag).toBe(3);
  });
});
