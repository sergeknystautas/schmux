import { test, expect, type Page } from './coverage-fixture';
import {
  seedConfig,
  createTestRepo,
  spawnSession,
  waitForDashboardLive,
  waitForHealthy,
  disposeAllSessions,
} from './helpers';
import { waitForRenderSettledOnPage } from './helpers-terminal';
import { writeFileSync } from 'fs';

// BENCHMARK SPEC — excluded from the scenario gate (testIgnore in
// playwright.config.ts) and selected only by playwright.bench.config.ts via
// `./test.sh --bench` (docs/testing.md rule 8). It measures keystroke
// round-trip latency through the full pipeline; latency values are reported,
// never asserted against thresholds. The only assertions are execution
// sanity: each variant produced samples.

/**
 * The benchmark agent reads one byte at a time and emits a numbered marker.
 * Flood output never contains that marker, so observing it in xterm proves the
 * measured keystroke reached the agent and its acknowledgement rendered.
 */
function benchmarkAgentCommand(stressed: boolean): string {
  const flood = stressed ? 'while true; do seq 1 20; sleep 0.05; done & ' : '';
  return (
    "bash -c 'stty -echo -icanon min 1 time 0; " +
    flood +
    'i=0; while IFS= read -r -n 1 c; do i=$((i+1)); ' +
    'printf "\\r\\n__BENCH_ACK_%d__\\r\\n" "$i"; done\''
  );
}

const SAMPLE_COUNT = 30;

/** Measure one keydown through its uniquely correlated rendered acknowledgement. */
async function measureAcknowledgedKey(page: Page, index: number): Promise<number> {
  const textarea = page.locator('.xterm-helper-textarea');
  const marker = `__BENCH_ACK_${index}__`;

  await page.evaluate(() => {
    (window as any).__benchKeydownAt = null;
    document.addEventListener(
      'keydown',
      () => {
        (window as any).__benchKeydownAt = performance.now();
      },
      { capture: true, once: true }
    );
  });
  await textarea.press('x');

  const settled = await waitForRenderSettledOnPage(page, { marker }, 5_000);
  const keydownAt = await page.evaluate(() => (window as any).__benchKeydownAt as number | null);
  if (keydownAt === null) {
    throw new Error(`No browser keydown timestamp captured for ${marker}`);
  }
  return settled.settledAt - keydownAt;
}

async function collectSamples(page: Page): Promise<number[]> {
  // Warmup also proves the full path is ready. It is not included in results.
  await measureAcknowledgedKey(page, 1);
  const samples: number[] = [];
  for (let index = 2; index < SAMPLE_COUNT + 2; index++) {
    samples.push(await measureAcknowledgedKey(page, index));
  }
  return samples;
}

function statsFor(samples: number[]) {
  const sorted = [...samples].sort((a, b) => a - b);
  const percentile = (p: number) =>
    sorted[Math.min(Math.floor(sorted.length * p), sorted.length - 1)];
  return {
    count: sorted.length,
    median: percentile(0.5),
    p95: percentile(0.95),
    p99: percentile(0.99),
    max: sorted[sorted.length - 1],
    avg: sorted.reduce((sum, value) => sum + value, 0) / sorted.length,
  };
}

const collectedResults: object[] = [];

/** Collect stats and emit the result as a BENCH_RESULT_JSON stdout line. */
async function reportVariant(
  page: Page,
  variant: 'idle' | 'stressed',
  samples: number[]
): Promise<void> {
  // Execution sanity only — a latency value never fails this spec. Missing
  // acknowledgements fail at their deadline; a partial sample set is invalid.
  expect(samples).toHaveLength(SAMPLE_COUNT);
  const stats = statsFor(samples);

  const env = await page.evaluate(() => ({
    nproc: navigator.hardwareConcurrency,
    userAgent: navigator.userAgent,
  }));
  const benchResult = {
    name: 'BrowserTypingLatency',
    variant,
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
          command: benchmarkAgentCommand(false),
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

    const samples = await collectSamples(page);
    await reportVariant(page, 'idle', samples);
  });

  test('stressed typing latency', async ({ page }) => {
    test.setTimeout(180_000);

    await seedConfig({
      repos: [repoPath],
      agents: [
        {
          name: 'flood-agent',
          command: benchmarkAgentCommand(true),
        },
      ],
    });

    const results = await spawnSession({
      repo: repoPath,
      branch: 'main',
      targets: { 'flood-agent': 1 },
    });
    const sessionId = results[0].session_id;

    await page.goto(`/sessions/${sessionId}`);
    await waitForDashboardLive(page);
    await page.waitForSelector('[data-testid="terminal-viewport"]', { timeout: 15_000 });

    const samples = await collectSamples(page);
    await reportVariant(page, 'stressed', samples);
  });
});
