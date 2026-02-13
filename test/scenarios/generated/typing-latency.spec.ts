import { test, expect } from '@playwright/test';
import {
  seedConfig,
  createTestRepo,
  spawnSession,
  waitForDashboardLive,
  waitForHealthy,
  waitForTerminalOutput,
  sleep,
} from './helpers';

test.describe.serial('Typing latency benchmark', () => {
  let repoPath: string;

  test.beforeAll(async () => {
    await waitForHealthy();
    repoPath = await createTestRepo('test-repo-latency');
  });

  test('idle typing latency', async ({ page }) => {
    await seedConfig({
      repos: [repoPath],
      agents: [
        {
          name: 'cat-agent',
          command: "sh -c 'echo READY; exec cat'",
          promptable: true,
        },
      ],
    });

    const results = await spawnSession({
      repo: repoPath,
      branch: 'main',
      prompt: 'bench',
      targets: { 'cat-agent': 1 },
    });
    const sessionId = results[0].session_id;

    await waitForTerminalOutput(sessionId, 'READY', 15_000);
    await page.goto(`/sessions/${sessionId}`);
    await waitForDashboardLive(page);
    await page.waitForSelector('[data-testid="terminal-viewport"]', { timeout: 15_000 });

    // Reset the latency tracker
    await page.evaluate(() => {
      const tracker = (window as any).__inputLatency;
      if (tracker) tracker.reset();
    });

    const textarea = page.locator('.xterm-helper-textarea');
    const charCount = 100;

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

    // Sanity assertion: median should be under 500ms (catches catastrophic regressions)
    expect(stats).not.toBeNull();
    expect(stats!.median).toBeLessThan(500);
  });

  test('stressed typing latency', async ({ page }) => {
    await seedConfig({
      repos: [repoPath],
      agents: [
        {
          name: 'flood-agent',
          command: "sh -c 'while true; do seq 1 100; sleep 0.01; done & echo READY; exec cat'",
          promptable: true,
        },
      ],
    });

    const results = await spawnSession({
      repo: repoPath,
      branch: 'main',
      prompt: 'bench-stressed',
      targets: { 'flood-agent': 1 },
    });
    const sessionId = results[0].session_id;

    await waitForTerminalOutput(sessionId, 'READY', 15_000);
    await page.goto(`/sessions/${sessionId}`);
    await waitForDashboardLive(page);
    await page.waitForSelector('[data-testid="terminal-viewport"]', { timeout: 15_000 });

    // Reset the latency tracker
    await page.evaluate(() => {
      const tracker = (window as any).__inputLatency;
      if (tracker) tracker.reset();
    });

    const textarea = page.locator('.xterm-helper-textarea');
    const charCount = 100;

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
    expect(stats!.median).toBeLessThan(500);
  });
});
