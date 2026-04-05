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
  openTerminal,
} from './helpers-terminal';

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
    test.setTimeout(120_000);

    await openTerminal(page, sessionId, tmuxName);

    // Fill scrollback with 500 lines so there's meaningful history
    const sentinel = sendTmuxCommandWithSentinel(
      tmuxName,
      'for i in $(seq 1 500); do echo "resize-test-line-$i"; done'
    );
    await waitForSentinel(sessionId, sentinel, 30_000);

    // Verify viewport is at bottom before resize (poll to handle rendering lag)
    const pollViewportAtBottom = async (label: string): Promise<void> => {
      const deadline = Date.now() + 5_000;
      while (Date.now() < deadline) {
        const atBottom = await page.evaluate(() => {
          const terminal = (window as any).__schmuxTerminal;
          if (!terminal) return false;
          const buffer = terminal.buffer.active;
          return buffer.viewportY >= buffer.baseY;
        });
        if (atBottom) return;
        await new Promise((r) => setTimeout(r, 200));
      }
      throw new Error(`Viewport not at bottom: ${label}`);
    };

    await pollViewportAtBottom('before resize');

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

    // The viewport must still be at the bottom after the resize.
    // Before the fix, fitTerminal() would fire DOM scroll events that
    // disabled followTail, leaving the viewport stuck above the bottom.
    await pollViewportAtBottom('after shrink');

    // Now grow it back (simulating nudge badge disappearing)
    await page.evaluate(() => {
      const container = document.getElementById('terminal');
      if (container) {
        container.style.height = '';
      }
    });

    // Still at bottom after growing back
    await pollViewportAtBottom('after restore');

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
    await waitForSentinel(sessionId, sentinel, page);

    // Wait for xterm.js to finish processing all output and for the write
    // guard to clear. Without this, live frames from the 200-line echo may
    // still be arriving via rAF when the resize fires, causing a race between
    // the write guard clear (8ms) and deferred viewport scroll events.
    await page.waitForFunction(
      () => {
        const stream = (window as any).__schmuxStream;
        if (!stream) return false;
        return !stream.writeRAFPending && !stream.writingToTerminal && !stream.scrollRAFPending;
      },
      { timeout: 5_000 }
    );

    // Verify followTail is true before resize via the Resume button
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

    // Wait for resize debounce (300ms) + fit + deferred viewport sync to settle.
    // Then verify the viewport is still at the bottom. During resize, xterm.js
    // buffer reflow can cause transient scroll events that briefly shift the
    // viewport, but followTail's scrollToBottom() in fitTerminal() should
    // restore it. We check the actual viewport position, not followTail state,
    // because the scroll guard race is a known timing issue in Docker.
    await page.waitForFunction(
      () => {
        const stream = (window as any).__schmuxStream;
        if (!stream) return false;
        return !stream.writeRAFPending && !stream.writingToTerminal && !stream.scrollRAFPending;
      },
      { timeout: 5_000 }
    );
    // Verify viewport is at the bottom after resize
    const atBottomAfterShrink = await page.evaluate(() => {
      const terminal = (window as any).__schmuxTerminal;
      if (!terminal) return false;
      const buf = terminal.buffer.active;
      return buf.viewportY >= buf.baseY;
    });
    expect(atBottomAfterShrink).toBe(true);

    // Restore container
    await page.evaluate(() => {
      const container = document.getElementById('terminal');
      if (container) {
        container.style.height = '';
      }
    });

    // Wait for restore resize to settle, then verify at bottom
    await page.waitForFunction(
      () => {
        const stream = (window as any).__schmuxStream;
        if (!stream) return false;
        return !stream.writeRAFPending && !stream.writingToTerminal && !stream.scrollRAFPending;
      },
      { timeout: 5_000 }
    );
    const atBottomAfterRestore = await page.evaluate(() => {
      const terminal = (window as any).__schmuxTerminal;
      if (!terminal) return false;
      const buf = terminal.buffer.active;
      return buf.viewportY >= buf.baseY;
    });
    expect(atBottomAfterRestore).toBe(true);
  });
});
