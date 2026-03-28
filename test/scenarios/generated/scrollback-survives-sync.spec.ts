import { test, expect } from './coverage-fixture';
import {
  seedConfig,
  createTestRepo,
  spawnSession,
  waitForHealthy,
  waitForSessionRunning,
} from './helpers';
import {
  sendTmuxCommandWithSentinel,
  waitForSentinel,
  assertTerminalMatchesTmux,
  getTmuxSessionName,
  readXtermBuffer,
  openTerminal,
} from './helpers-terminal';

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
    test.setTimeout(120_000);

    await openTerminal(page, sessionId, tmuxName);

    // Output 500 lines to fill scrollback well beyond the visible viewport
    const fillSentinel = sendTmuxCommandWithSentinel(
      tmuxName,
      'for i in $(seq 1 500); do echo "scrollback-line-$i"; done'
    );
    await waitForSentinel(sessionId, fillSentinel, 30_000);

    // Verify scrollback matches before corruption.
    // assertTerminalMatchesTmux retries internally, handling rendering lag.
    await assertTerminalMatchesTmux(page, tmuxName, { scrollbackLines: 500 });

    // Corrupt xterm.js viewport to force a sync correction.
    // Only corrupt the visible area — scrollback should be untouched.
    await page.evaluate(() => {
      const terminal = (window as any).__schmuxTerminal;
      if (!terminal) throw new Error('__schmuxTerminal not found');
      terminal.write('\x1b[H\x1b[JCORRUPTED_SYNC_SCROLLBACK_TEST');
    });

    // Wait for corruption to appear in xterm.js buffer (state check, not fixed delay)
    const corruptDeadline = Date.now() + 5_000;
    let hasCorruption = false;
    while (Date.now() < corruptDeadline && !hasCorruption) {
      const xtermAfterCorruption = await readXtermBuffer(page);
      hasCorruption = xtermAfterCorruption.some((line) =>
        line.includes('CORRUPTED_SYNC_SCROLLBACK_TEST')
      );
      if (!hasCorruption) await new Promise((r) => setTimeout(r, 50));
    }
    expect(hasCorruption).toBe(true);

    // Wait for the sync goroutine to detect the mismatch and correct it.
    // Initial sync fires at 5s from connection. We use a 45s deadline
    // to allow margin for Docker container overhead and the activity guard.
    const deadline = Date.now() + 45_000;
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
