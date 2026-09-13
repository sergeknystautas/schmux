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

// Functional echo scenario: a typed line must traverse the full pipeline
// (browser xterm → WebSocket → server → tmux → agent stdin → agent stdout →
// tmux → server → WebSocket → xterm buffer) and the agent's exact
// acknowledgement of that line must come back rendered. No elapsed-time
// assertions: machine timing never passes or fails the gate (docs/testing.md
// rule 8). The 500 ms responsiveness objective is documented, not asserted,
// in test/scenarios/typing-latency.md.

// The agent acknowledges every line it reads as `ACK<line>KCA` in one write,
// so the frame lands contiguously on a single terminal line even while a
// background flood interleaves with the tty's own echo of the keystrokes.
// The frame cannot be produced by echoed input (we never type "ACK<"), and a
// dropped or altered character changes the frame, so a contiguous match
// proves the agent received exactly what was typed.
const ACK_LOOP = 'while IFS= read -r line; do printf "ACK<%s>KCA\\n" "$line"; done';

/** Random letters-only nonce, unique per use. */
function randomNonce(length = 20): string {
  const alphabet = 'abcdefghijklmnopqrstuvwxyz';
  let out = '';
  for (let i = 0; i < length; i++) {
    out += alphabet[Math.floor(Math.random() * alphabet.length)];
  }
  return out;
}

function ackFrame(nonce: string): string {
  return `ACK<${nonce}>KCA`;
}

/** The exact acknowledgement frame appears contiguously on one buffer line. */
function hasAck(lines: string[], nonce: string): boolean {
  const frame = ackFrame(nonce);
  return lines.some((l) => l.includes(frame));
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
    // The buffer spans the full viewport height, so content is followed by
    // blank rows; drop those before taking the tail or the tail is empty.
    let end = last.length;
    while (end > 0 && last[end - 1].trim() === '') end--;
    throw new Error(
      `${description} not rendered within ${timeoutMs}ms. ` +
        `Last observed buffer tail (${last.length} lines total, ${end} non-blank):\n` +
        last.slice(Math.max(0, end - 30), end).join('\n'),
      { cause: err }
    );
  }
  return last;
}

/** Type one line (nonce + Enter) into the terminal. */
async function typeLine(page: Page, nonce: string): Promise<void> {
  const textarea = page.locator('.xterm-helper-textarea');
  await textarea.type(nonce, { delay: 10 });
  await textarea.press('Enter');
}

/**
 * Prove the round-trip pipeline is operational before the measured nonce is
 * typed, without retrying input. Two semantic boundaries, each awaited once:
 *
 * 1. The agent's `READY` banner renders in xterm. Content reaches xterm only
 *    over the terminal WebSocket, so this proves the socket is open — the
 *    stream silently drops input sent before then — and the agent is running.
 * 2. One run-unique warm-up nonce is typed and its exact acknowledgement
 *    frame awaited once, with a deadline and the last observed buffer in the
 *    failure (rubric rules 6, 12).
 */
async function awaitAckReadiness(page: Page, timeoutMs = 60_000): Promise<void> {
  await awaitRenderedContent(
    page,
    (ls) => ls.some((l) => l.includes('READY')),
    'agent READY banner',
    timeoutMs
  );

  const warmup = randomNonce(8);
  await typeLine(page, warmup);
  await awaitRenderedContent(
    page,
    (ls) => hasAck(ls, warmup),
    `warm-up acknowledgement "${ackFrame(warmup)}"`,
    timeoutMs
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
          ? `sh -c 'echo READY; while true; do seq 1 20; sleep 0.05; done & ${ACK_LOOP}'`
          : `sh -c 'echo READY; ${ACK_LOOP}'`;

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

      // Readiness: the agent's READY banner and one warm-up acknowledgement
      // render — the round trip is proven operational before the claim.
      await awaitAckReadiness(page);

      // Claim: the agent receives exactly the typed line and its exact
      // acknowledgement frame renders contiguously.
      const nonce = randomNonce();
      await typeLine(page, nonce);

      const lines = await awaitRenderedContent(
        page,
        (ls) => hasAck(ls, nonce),
        `acknowledgement "${ackFrame(nonce)}"`
      );

      // Assert once, after arrival (rubric rule 7).
      expect(hasAck(lines, nonce)).toBe(true);
    });
  }
});
