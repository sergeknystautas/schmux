import { test, expect, type Page } from './coverage-fixture';
import {
  seedConfig,
  createTestRepo,
  spawnSession,
  waitForDashboardLive,
  waitForHealthy,
  disposeAllSessions,
} from './helpers';
import { readXtermBuffer } from './helpers-terminal';

// Functional echo scenario: typed input must traverse the full pipeline
// (browser xterm → WebSocket → server → tmux → cat → back → xterm render)
// and the typed characters must come back rendered. No elapsed-time
// assertions — latency measurement lives in typing-latency.bench.spec.ts
// behind `./test.sh --bench` (docs/testing.md rule 8).

/** Random letters-only marker. The stressed agent's flood emits only digits,
 *  so letters in the buffer can only be our own echoed keystrokes. */
function randomMarker(length = 20): string {
  const alphabet = 'abcdefghijklmnopqrstuvwxyz';
  let out = '';
  for (let i = 0; i < length; i++) {
    out += alphabet[Math.floor(Math.random() * alphabet.length)];
  }
  return out;
}

/** Every character of `marker` appears in `lines`, in order. Contiguous when
 *  nothing interleaves (idle); an in-order subsequence when flood output
 *  interleaves the echoed characters (stressed). */
function containsInOrder(lines: string[], marker: string): boolean {
  const text = lines.join('\n');
  let idx = 0;
  for (const ch of marker) {
    idx = text.indexOf(ch, idx);
    if (idx === -1) return false;
    idx += 1;
  }
  return true;
}

/** Await an eventual rendered-terminal state (rubric rule 5): resolves when
 *  `predicate` holds over the full xterm buffer (scrollback + viewport).
 *  On timeout, fails with the last observed buffer (rubric rule 12). */
async function awaitRenderedContent(
  page: Page,
  predicate: (lines: string[]) => boolean,
  description: string,
  timeoutMs = 30_000
): Promise<string[]> {
  let last: string[] = [];
  try {
    await expect
      .poll(
        async () => {
          last = await readXtermBuffer(page, { scrollbackLines: 5000 });
          return predicate(last);
        },
        { timeout: timeoutMs }
      )
      .toBe(true);
  } catch (err) {
    throw new Error(
      `${description} not rendered within ${timeoutMs}ms. ` +
        `Last observed buffer tail (${last.length} lines total):\n` +
        last.slice(-30).join('\n'),
      { cause: err }
    );
  }
  return last;
}

/**
 * Press a warmup key until its echo renders — proves the full pipeline is
 * connected before the marker is typed. This retries input (readiness
 * probing), never an assertion: once the echo renders, the test moves on.
 */
async function waitForEchoReadiness(page: Page, timeoutMs = 60_000): Promise<void> {
  const textarea = page.locator('.xterm-helper-textarea');
  const deadline = Date.now() + timeoutMs;
  let lastLines: string[] = [];

  while (Date.now() < deadline) {
    await textarea.press('q');
    try {
      await expect
        .poll(
          async () => {
            lastLines = await readXtermBuffer(page, { scrollbackLines: 1000 });
            return lastLines.join('\n').includes('q');
          },
          { timeout: 2_000 }
        )
        .toBe(true);
      return;
    } catch {
      // Echo not rendered yet — press again.
    }
  }

  throw new Error(
    `Echo pipeline not ready after ${timeoutMs}ms. Last observed buffer tail:\n` +
      lastLines.slice(-30).join('\n')
  );
}

test.describe.serial('Typing echo', () => {
  let repoPath: string;

  test.beforeAll(async () => {
    await waitForHealthy();
    repoPath = await createTestRepo('test-repo-typing-echo');
  });

  test.afterAll(async () => {
    // Dispose sessions (especially the flood agent) so accumulated sessions
    // don't overwhelm the daemon during repeated runs.
    await disposeAllSessions();
  });

  for (const condition of ['idle', 'stressed'] as const) {
    test(`typing echo returns typed characters (${condition})`, async ({ page }) => {
      test.setTimeout(120_000);

      const agentCommand =
        condition === 'stressed'
          ? "sh -c 'echo READY; while true; do seq 1 20; sleep 0.05; done & exec cat'"
          : "sh -c 'echo READY; exec cat'";

      await seedConfig({
        repos: [repoPath],
        agents: [{ name: `${condition}-echo-agent`, command: agentCommand }],
      });

      const results = await spawnSession({
        repo: repoPath,
        branch: 'main',
        targets: { [`${condition}-echo-agent`]: 1 },
      });
      const sessionId = results[0].session_id;

      await page.goto(`/sessions/${sessionId}`);
      await waitForDashboardLive(page);
      await page.waitForSelector('[data-testid="terminal-viewport"]', { timeout: 15_000 });

      // Readiness: a warmup keystroke's echo renders (semantic boundary —
      // the pipeline is proven operational before the claim is tested).
      await waitForEchoReadiness(page);

      // Claim: every typed character returns, in order, rendered.
      const marker = randomMarker();
      const textarea = page.locator('.xterm-helper-textarea');
      await textarea.type(marker, { delay: 10 });

      const lines = await awaitRenderedContent(
        page,
        (ls) => containsInOrder(ls, marker),
        `all ${marker.length} marker characters in order`
      );

      // Assert once, after arrival (rubric rule 7).
      expect(containsInOrder(lines, marker)).toBe(true);
    });
  }
});
