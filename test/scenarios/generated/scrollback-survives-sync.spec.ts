import { type Page } from './coverage-fixture';
import { test, expect } from './coverage-fixture';
import {
  seedConfig,
  createTestRepo,
  spawnSession,
  waitForDashboardLive,
  waitForHealthy,
  waitForSessionRunning,
} from './helpers';
import {
  sendTmuxCommand,
  sendTmuxCommandWithSentinel,
  waitForSentinel,
  assertTerminalMatchesTmux,
  getTmuxSessionName,
  clearTmuxHistory,
  readXtermBuffer,
} from './helpers-terminal';

// ---------------------------------------------------------------------------
// Reuse the openTerminal pattern from terminal-fidelity.spec.ts.
// ---------------------------------------------------------------------------

async function openTerminal(page: Page, sessionId: string, tmuxName: string): Promise<void> {
  await page.goto(`/sessions/${sessionId}`);
  await waitForDashboardLive(page);
  await page.waitForSelector('[data-testid="terminal-viewport"]', { timeout: 15_000 });

  const wsDeadline = Date.now() + 10_000;
  while (Date.now() < wsDeadline) {
    const hasContent = await page.evaluate(() => {
      const terminal = (window as any).__schmuxTerminal;
      if (!terminal) return false;
      const buffer = terminal.buffer.active;
      for (let i = 0; i < terminal.rows; i++) {
        const line = buffer.getLine(buffer.baseY + i);
        if (line && line.translateToString(true).trim()) return true;
      }
      return false;
    });
    if (hasContent) break;
    await new Promise((r) => setTimeout(r, 100));
  }

  sendTmuxCommand(tmuxName, "printf '\\033[3J\\033[H\\033[2J'");
  await new Promise((r) => setTimeout(r, 500));
  clearTmuxHistory(tmuxName);
  await new Promise((r) => setTimeout(r, 200));
}

// ---------------------------------------------------------------------------
// Scrollback survives sync correction
// ---------------------------------------------------------------------------

test.describe.serial('Scrollback survives sync correction', () => {
  let repoPath: string;
  let sessionId: string;
  let tmuxName: string;

  test.beforeAll(async () => {
    await waitForHealthy();
    repoPath = await createTestRepo('test-repo-scrollback-sync');
    await seedConfig({
      repos: [repoPath],
      agents: [
        {
          name: 'shell-agent',
          command: "sh -c 'exec bash'",
          promptable: false,
        },
      ],
    });

    const results = await spawnSession({
      repo: repoPath,
      branch: 'main',
      targets: { 'shell-agent': 1 },
    });
    sessionId = results[0].session_id;
    await waitForSessionRunning(sessionId);
    tmuxName = await getTmuxSessionName(sessionId);
  });

  test('scrollback remains intact after sync correction', async ({ page }) => {
    test.setTimeout(90_000);

    await openTerminal(page, sessionId, tmuxName);

    // Output 500 lines to fill scrollback well beyond the visible viewport
    const fillSentinel = sendTmuxCommandWithSentinel(
      tmuxName,
      'for i in $(seq 1 500); do echo "scrollback-line-$i"; done'
    );
    await waitForSentinel(sessionId, fillSentinel);

    // Verify scrollback matches before corruption
    await assertTerminalMatchesTmux(page, tmuxName, { scrollbackLines: 500 });

    // Corrupt xterm.js viewport to force a sync correction.
    // Only corrupt the visible area — scrollback should be untouched.
    await page.evaluate(() => {
      const terminal = (window as any).__schmuxTerminal;
      if (!terminal) throw new Error('__schmuxTerminal not found');
      terminal.write('\x1b[H\x1b[2JCORRUPTED_SYNC_SCROLLBACK_TEST');
    });
    await new Promise((r) => setTimeout(r, 200));

    // Verify corruption actually happened
    const xtermAfterCorruption = await readXtermBuffer(page);
    const hasCorruption = xtermAfterCorruption.some((line) =>
      line.includes('CORRUPTED_SYNC_SCROLLBACK_TEST')
    );
    expect(hasCorruption).toBe(true);

    // Wait for the sync goroutine to detect the mismatch and correct it.
    // Initial sync fires at 5s from connection. We use a 20s deadline.
    const deadline = Date.now() + 20_000;
    let corrected = false;

    while (Date.now() < deadline && !corrected) {
      try {
        // Check visible viewport only — sync should fix this
        await assertTerminalMatchesTmux(page, tmuxName);
        corrected = true;
      } catch {
        await new Promise((r) => setTimeout(r, 1000));
      }
    }

    expect(corrected).toBe(true);

    // CRITICAL: After sync correction, scrollback must still be intact.
    // This is the whole point — sync uses surgical correction, not reset().
    await assertTerminalMatchesTmux(page, tmuxName, { scrollbackLines: 500 });
  });
});
