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
} from './helpers-terminal';

// ---------------------------------------------------------------------------
// Helper: open a terminal and clear it for a clean test surface.
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
// Terminal scroll position survives nudge-triggered resize
// ---------------------------------------------------------------------------
test.describe.serial('Resize scroll stability', () => {
  let repoPath: string;
  let sessionId: string;
  let tmuxName: string;

  test.beforeAll(async () => {
    await waitForHealthy();
    repoPath = await createTestRepo('test-repo-resize-scroll');
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

  test('viewport stays at bottom after container resize', async ({ page }) => {
    test.setTimeout(60_000);

    await openTerminal(page, sessionId, tmuxName);

    // Fill scrollback with 500 lines so there's meaningful history
    const sentinel = sendTmuxCommandWithSentinel(
      tmuxName,
      'for i in $(seq 1 500); do echo "resize-test-line-$i"; done'
    );
    await waitForSentinel(sessionId, sentinel);

    // Wait for xterm.js to finish rendering
    await new Promise((r) => setTimeout(r, 500));

    // Verify viewport is at bottom before resize
    const atBottomBefore = await page.evaluate(() => {
      const terminal = (window as any).__schmuxTerminal;
      if (!terminal) return false;
      const buffer = terminal.buffer.active;
      return buffer.viewportY >= buffer.baseY;
    });
    expect(atBottomBefore).toBe(true);

    // Simulate the nudge-triggered resize: shrink the terminal container
    // by 40px (equivalent to a session tab growing when row2 nudge text appears).
    // This triggers ResizeObserver → fitTerminal() → terminal.resize().
    await page.evaluate(() => {
      const container = document.getElementById('terminal');
      if (container) {
        const currentHeight = container.getBoundingClientRect().height;
        container.style.height = `${currentHeight - 40}px`;
      }
    });

    // Wait for the debounced resize to fire (300ms debounce + margin)
    await new Promise((r) => setTimeout(r, 600));

    // The viewport must still be at the bottom after the resize.
    // Before the fix, fitTerminal() would fire DOM scroll events that
    // disabled followTail, leaving the viewport stuck above the bottom.
    const atBottomAfter = await page.evaluate(() => {
      const terminal = (window as any).__schmuxTerminal;
      if (!terminal) return false;
      const buffer = terminal.buffer.active;
      return buffer.viewportY >= buffer.baseY;
    });
    expect(atBottomAfter).toBe(true);

    // Now grow it back (simulating nudge badge disappearing)
    await page.evaluate(() => {
      const container = document.getElementById('terminal');
      if (container) {
        container.style.height = '';
      }
    });
    await new Promise((r) => setTimeout(r, 600));

    // Still at bottom after growing back
    const atBottomRestored = await page.evaluate(() => {
      const terminal = (window as any).__schmuxTerminal;
      if (!terminal) return false;
      const buffer = terminal.buffer.active;
      return buffer.viewportY >= buffer.baseY;
    });
    expect(atBottomRestored).toBe(true);

    // Terminal content should match tmux after all resizes settle
    await assertTerminalMatchesTmux(page, tmuxName);
  });

  test('followTail remains true through resize cycle', async ({ page }) => {
    test.setTimeout(60_000);

    await openTerminal(page, sessionId, tmuxName);

    // Generate some output
    const sentinel = sendTmuxCommandWithSentinel(
      tmuxName,
      'for i in $(seq 1 200); do echo "follow-tail-test-$i"; done'
    );
    await waitForSentinel(sessionId, sentinel);
    await new Promise((r) => setTimeout(r, 500));

    // Verify followTail is true before resize
    const followBefore = await page.evaluate(() => {
      const ts = (window as any).__schmuxTerminalStream;
      return ts ? (ts as any).followTail : null;
    });
    // If __schmuxTerminalStream is not exposed, check via the Resume button
    // (it only shows when followTail is false)
    const resumeVisibleBefore = await page.locator('.log-viewer__new-content').isVisible();
    expect(resumeVisibleBefore).toBe(false);

    // Shrink container to trigger resize
    await page.evaluate(() => {
      const container = document.getElementById('terminal');
      if (container) {
        const currentHeight = container.getBoundingClientRect().height;
        container.style.height = `${currentHeight - 40}px`;
      }
    });
    await new Promise((r) => setTimeout(r, 600));

    // Resume button should NOT appear — followTail must stay true
    const resumeVisibleAfter = await page.locator('.log-viewer__new-content').isVisible();
    expect(resumeVisibleAfter).toBe(false);

    // Restore container
    await page.evaluate(() => {
      const container = document.getElementById('terminal');
      if (container) {
        container.style.height = '';
      }
    });
    await new Promise((r) => setTimeout(r, 600));

    // Still no Resume button
    const resumeVisibleRestored = await page.locator('.log-viewer__new-content').isVisible();
    expect(resumeVisibleRestored).toBe(false);
  });
});
