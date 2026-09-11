import { test, expect, type Page } from './coverage-fixture';
import {
  seedConfig,
  createTestRepo,
  spawnSession,
  waitForDashboardLive,
  waitForHealthy,
  sleep,
  disposeAllSessions,
} from './helpers';
import { writeFileSync } from 'fs';

// BENCHMARK SPEC — excluded from the scenario gate (testIgnore in
// playwright.config.ts) and selected only by playwright.bench.config.ts via
// `./test.sh --bench` (docs/testing.md rule 8). It measures keystroke
// round-trip latency through the full pipeline; latency values are reported,
// never asserted against thresholds. The only assertions are execution
// sanity: each variant produced samples.

/**
 * Wait for the full typing pipeline to be operational:
 * xterm → WebSocket → server → tmux → cat → tmux → server → WebSocket → xterm.
 *
 * Presses warmup keys until the latency tracker records a sample (the tracker
 * resolves a sample exactly when an echo round-trip completes — a product-owned
 * semantic boundary), then resets it so warmup samples don't pollute the
 * measurement.
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
      await page.evaluate(() => {
        const tracker = (window as any).__inputLatency;
        if (tracker) tracker.reset();
      });
      return;
    }
  }

  throw new Error(`Echo pipeline not ready after ${timeoutMs}ms`);
}

/** Type `charCount` keys, letting each echo round-trip record a sample. */
async function typeMeasuredKeys(page: Page, charCount: number): Promise<void> {
  const textarea = page.locator('.xterm-helper-textarea');

  for (let i = 0; i < charCount; i++) {
    const prevCount = await page.evaluate(() => {
      const tracker = (window as any).__inputLatency;
      return tracker ? tracker.samples.length : 0;
    });

    await textarea.press('x');

    // Wait for the sample to be recorded (echo round-trip). This pacing is
    // measurement code, not synchronization: the sample IS the observation.
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
}

const collectedResults: object[] = [];

/** Collect stats and emit the result as a BENCH_RESULT_JSON stdout line. */
async function reportVariant(page: Page, variant: 'idle' | 'stressed'): Promise<void> {
  const stats = await page.evaluate(() => {
    const tracker = (window as any).__inputLatency;
    return tracker ? tracker.getStats() : null;
  });

  // Execution sanity only — a latency value never fails this spec.
  expect(stats).not.toBeNull();
  expect(stats!.count).toBeGreaterThan(0);

  const env = await page.evaluate(() => ({
    nproc: navigator.hardwareConcurrency,
    userAgent: navigator.userAgent,
  }));
  const benchResult = {
    name: 'BrowserTypingLatency',
    variant,
    iterations: stats!.count,
    p50_ms: stats!.median,
    p95_ms: stats!.p95,
    p99_ms: stats!.p99,
    max_ms: stats!.max,
    mean_ms: stats!.avg,
    min_ms: 0,
    stddev_ms: 0,
    gc_pauses: 0,
    gc_pause_total_us: 0,
    timestamp: new Date().toISOString(),
    nproc: env.nproc,
    userAgent: env.userAgent,
  };
  console.log('BENCH_RESULT_JSON:', JSON.stringify(benchResult));
  collectedResults.push(benchResult);
}

test.describe.serial('Browser typing latency benchmark', () => {
  let repoPath: string;

  test.beforeAll(async () => {
    await waitForHealthy();
    repoPath = await createTestRepo('test-repo-latency');
  });

  test.afterAll(async () => {
    // Raw artifact alongside the container's test artifacts, for diagnosis.
    // (The canonical merged report is written host-side by the bench suite.)
    if (collectedResults.length > 0) {
      try {
        writeFileSync(
          '/artifacts/browser-typing-latency.json',
          JSON.stringify(collectedResults, null, 2)
        );
      } catch {
        // /artifacts not mounted (e.g. local run) — stdout lines still carry the data.
      }
    }
    // Dispose all sessions (especially flood-agent) to prevent accumulated
    // sessions from overwhelming the daemon.
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

    await page.goto(`/sessions/${sessionId}`);
    await waitForDashboardLive(page);
    await page.waitForSelector('[data-testid="terminal-viewport"]', { timeout: 15_000 });

    await waitForEchoPipeline(page, 60_000);
    await typeMeasuredKeys(page, 30);
    await reportVariant(page, 'idle');
  });

  test('stressed typing latency', async ({ page }) => {
    test.setTimeout(180_000);

    await seedConfig({
      repos: [repoPath],
      agents: [
        {
          name: 'flood-agent',
          command: "sh -c 'while true; do seq 1 20; sleep 0.05; done & exec cat'",
        },
      ],
    });

    const results = await spawnSession({
      repo: repoPath,
      branch: 'main',
      targets: { 'flood-agent': 1 },
    });
    const sessionId = results[0].session_id;

    // Skip a READY marker wait — flood output drowns any marker.
    // waitForEchoPipeline below is the authoritative readiness check.
    await page.goto(`/sessions/${sessionId}`);
    await waitForDashboardLive(page);
    await page.waitForSelector('[data-testid="terminal-viewport"]', { timeout: 15_000 });

    await waitForEchoPipeline(page, 60_000);
    await typeMeasuredKeys(page, 30);
    await reportVariant(page, 'stressed');
  });
});
