import { test, expect, type Page } from './coverage-fixture';
import {
  seedConfig,
  createTestRepo,
  spawnSession,
  waitForDashboardLive,
  waitForHealthy,
  waitForTerminalOutput,
  sleep,
  disposeAllSessions,
} from './helpers';

/**
 * Wait for the full typing pipeline to be operational:
 * xterm → WebSocket → server → tmux → cat → tmux → server → WebSocket → xterm.
 *
 * Presses warmup keys until the latency tracker records a sample, confirming
 * the WebSocket is connected and echo is flowing. Resets the tracker afterward.
 */
async function waitForEchoPipeline(page: Page, timeoutMs = 30_000): Promise<void> {
  const textarea = page.locator('.xterm-helper-textarea');
  const deadline = Date.now() + timeoutMs;

  while (Date.now() < deadline) {
    await textarea.press('.');
    await sleep(200);
    const ready = await page.evaluate(() => {
      const tracker = (window as any).__inputLatency;
      return tracker && tracker.samples.length > 0;
    });
    if (ready) {
      // Reset so warmup samples don't pollute the benchmark
      await page.evaluate(() => {
        const tracker = (window as any).__inputLatency;
        if (tracker) tracker.reset();
      });
      return;
    }
  }

  throw new Error(`Echo pipeline not ready after ${timeoutMs}ms`);
}

test.describe.serial('Typing latency benchmark', () => {
  let repoPath: string;

  test.beforeAll(async () => {
    await waitForHealthy();
    repoPath = await createTestRepo('test-repo-latency');
  });

  test.afterAll(async () => {
    // Dispose all sessions (especially flood-agent) to prevent accumulated
    // sessions from overwhelming the daemon during repeated test runs.
    await disposeAllSessions();
  });

  test('idle typing latency', async ({ page }) => {
    test.setTimeout(180_000);

    await seedConfig({
      repos: [repoPath],
      agents: [
        {
          name: 'cat-agent',
          command: "sh -c 'echo READY; exec cat'",
        },
      ],
    });

    const results = await spawnSession({
      repo: repoPath,
      branch: 'main',
      targets: { 'cat-agent': 1 },
    });
    const sessionId = results[0].session_id;

    await waitForTerminalOutput(sessionId, 'READY', 30_000);
    await page.goto(`/sessions/${sessionId}`);
    await waitForDashboardLive(page);
    await page.waitForSelector('[data-testid="terminal-viewport"]', { timeout: 15_000 });

    // Wait for the full echo pipeline (WebSocket connected + cat echoing)
    await waitForEchoPipeline(page, 60_000);

    const textarea = page.locator('.xterm-helper-textarea');
    const charCount = 30;

    for (let i = 0; i < charCount; i++) {
      const prevCount = await page.evaluate(() => {
        const tracker = (window as any).__inputLatency;
        return tracker ? tracker.samples.length : 0;
      });

      await textarea.press('x');

      // Wait for the sample to be recorded (echo round-trip)
      const deadline = Date.now() + 5_000;
      while (Date.now() < deadline) {
        const count = await page.evaluate(() => {
          const tracker = (window as any).__inputLatency;
          return tracker ? tracker.samples.length : 0;
        });
        if (count > prevCount) break;
        await sleep(10);
      }

      await sleep(50);
    }

    const stats = await page.evaluate(() => {
      const tracker = (window as any).__inputLatency;
      return tracker ? tracker.getStats() : null;
    });

    if (stats) {
      const benchResult = {
        name: 'BrowserTypingLatency',
        variant: 'idle',
        iterations: stats.count,
        p50_ms: stats.median,
        p95_ms: stats.p95,
        p99_ms: stats.p99,
        max_ms: stats.max,
        mean_ms: stats.avg,
        min_ms: 0,
        stddev_ms: 0,
        gc_pauses: 0,
        gc_pause_total_us: 0,
        timestamp: new Date().toISOString(),
      };
      console.log('BENCH_RESULT_JSON:', JSON.stringify(benchResult, null, 2));
    }

    // Sanity assertion: median should be under 1500ms (catches catastrophic regressions).
    // The threshold is generous to account for Docker container overhead and
    // accumulated sessions from prior tests sharing the same daemon.
    expect(stats).not.toBeNull();
    expect(stats!.median).toBeLessThan(1500);
  });

  test('stressed typing latency', async ({ page }) => {
    test.setTimeout(180_000);

    await seedConfig({
      repos: [repoPath],
      agents: [
        {
          name: 'flood-agent',
          command: "sh -c 'while true; do seq 1 100; sleep 0.01; done & exec cat'",
        },
      ],
    });

    const results = await spawnSession({
      repo: repoPath,
      branch: 'main',
      targets: { 'flood-agent': 1 },
    });
    const sessionId = results[0].session_id;

    // Skip waitForTerminalOutput — the flood output drowns any marker.
    // waitForEchoPipeline below is the authoritative readiness check.
    await page.goto(`/sessions/${sessionId}`);
    await waitForDashboardLive(page);
    await page.waitForSelector('[data-testid="terminal-viewport"]', { timeout: 15_000 });

    // Wait for the full echo pipeline (WebSocket connected + cat echoing)
    await waitForEchoPipeline(page, 60_000);

    const textarea = page.locator('.xterm-helper-textarea');
    const charCount = 30;

    for (let i = 0; i < charCount; i++) {
      const prevCount = await page.evaluate(() => {
        const tracker = (window as any).__inputLatency;
        return tracker ? tracker.samples.length : 0;
      });

      await textarea.press('x');

      const deadline = Date.now() + 5_000;
      while (Date.now() < deadline) {
        const count = await page.evaluate(() => {
          const tracker = (window as any).__inputLatency;
          return tracker ? tracker.samples.length : 0;
        });
        if (count > prevCount) break;
        await sleep(10);
      }

      await sleep(50);
    }

    const stats = await page.evaluate(() => {
      const tracker = (window as any).__inputLatency;
      return tracker ? tracker.getStats() : null;
    });

    if (stats) {
      const benchResult = {
        name: 'BrowserTypingLatency',
        variant: 'stressed',
        iterations: stats.count,
        p50_ms: stats.median,
        p95_ms: stats.p95,
        p99_ms: stats.p99,
        max_ms: stats.max,
        mean_ms: stats.avg,
        min_ms: 0,
        stddev_ms: 0,
        gc_pauses: 0,
        gc_pause_total_us: 0,
        timestamp: new Date().toISOString(),
      };
      console.log('BENCH_RESULT_JSON:', JSON.stringify(benchResult, null, 2));
    }

    expect(stats).not.toBeNull();
    // Stressed latency threshold is higher since a background flood process
    // competes for CPU. 3000ms is generous to account for Docker overhead.
    expect(stats!.median).toBeLessThan(3000);
  });
});
