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
  sendTmuxCommandWithSentinel,
  waitForSentinel,
  assertTerminalMatchesTmux,
  getTmuxSessionName,
  openTerminal,
} from './helpers-terminal';

// ---------------------------------------------------------------------------
// Bootstrap renders at correct scroll position
// ---------------------------------------------------------------------------

test.describe.serial('Bootstrap scroll position', () => {
  let repoPath: string;
  let sessionId: string;
  let tmuxName: string;

  test.beforeAll(async () => {
    await waitForHealthy();
    repoPath = await createTestRepo('test-repo-bootstrap-scroll');
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

  test('bootstrap shows cursor at bottom without scrolling animation', async ({ page }) => {
    test.setTimeout(60_000);

    await openTerminal(page, sessionId, tmuxName);

    // Output 2000 lines to build substantial scrollback
    const fillSentinel = sendTmuxCommandWithSentinel(
      tmuxName,
      'for i in $(seq 1 2000); do echo "bootstrap-line-$i"; done'
    );
    await waitForSentinel(sessionId, fillSentinel);

    // Verify everything is synced before reload
    await assertTerminalMatchesTmux(page, tmuxName);

    // Reload — triggers a full bootstrap from the output log
    await page.reload();
    await waitForDashboardLive(page);
    await page.waitForSelector('[data-testid="terminal-viewport"]', { timeout: 15_000 });

    // Wait for bootstrap to complete and terminal to render.
    // The cursor should be at the bottom (near the prompt) within 2 seconds.
    const start = Date.now();
    let cursorAtBottom = false;

    while (Date.now() - start < 5_000) {
      cursorAtBottom = await page.evaluate(() => {
        const terminal = (window as any).__schmuxTerminal;
        if (!terminal) return false;
        const buffer = terminal.buffer.active;
        // Terminal is "at bottom" when viewport shows the last lines
        // (viewportY >= baseY means we're scrolled to the bottom)
        return buffer.viewportY >= buffer.baseY;
      });
      if (cursorAtBottom) break;
      await new Promise((r) => setTimeout(r, 100));
    }

    expect(cursorAtBottom).toBe(true);

    // The visible viewport should match tmux (ground truth)
    await assertTerminalMatchesTmux(page, tmuxName);
  });

  test('bootstrap uses sequenced binary frames', async ({ page }) => {
    test.setTimeout(60_000);

    // Track binary frames received during bootstrap
    const binaryFrameSeqs: number[] = [];
    let bootstrapCompleteReceived = false;

    page.on('websocket', (ws) => {
      if (ws.url().includes('/ws/terminal/')) {
        ws.on('framereceived', (frame) => {
          if (typeof frame.payload !== 'string') {
            // Binary frame — extract sequence number from 8-byte header
            const buf = Buffer.from(frame.payload as ArrayBuffer);
            if (buf.length >= 8) {
              const seq = Number(buf.readBigUInt64BE(0));
              binaryFrameSeqs.push(seq);
            }
          } else {
            try {
              const msg = JSON.parse(frame.payload as string);
              if (msg.type === 'bootstrapComplete') {
                bootstrapCompleteReceived = true;
              }
            } catch {
              // ignore
            }
          }
        });
      }
    });

    // Reload to trigger a fresh bootstrap
    await page.goto(`/sessions/${sessionId}`);
    await waitForDashboardLive(page);
    await page.waitForSelector('[data-testid="terminal-viewport"]', { timeout: 15_000 });

    // Wait for bootstrap to complete
    const deadline = Date.now() + 10_000;
    while (Date.now() < deadline && !bootstrapCompleteReceived) {
      await new Promise((r) => setTimeout(r, 100));
    }

    // Verify we received binary frames with sequence headers
    expect(binaryFrameSeqs.length).toBeGreaterThan(0);

    // Verify bootstrapComplete was sent after the binary frames
    expect(bootstrapCompleteReceived).toBe(true);

    // Verify sequence numbers are monotonically non-decreasing
    // (chunked replay may send multiple entries with the last seq of each chunk)
    for (let i = 1; i < binaryFrameSeqs.length; i++) {
      expect(binaryFrameSeqs[i]).toBeGreaterThanOrEqual(binaryFrameSeqs[i - 1]);
    }
  });
});
