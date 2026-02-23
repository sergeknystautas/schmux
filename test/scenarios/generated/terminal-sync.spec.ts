import { type Page } from '@playwright/test';
import { test, expect } from '@playwright/test';
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
// Navigates to the session page, waits for bootstrap, clears screen to sync.
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
// Terminal sync round-trip tests
// ---------------------------------------------------------------------------

test.describe.serial('Terminal sync: round-trip', () => {
  let repoPath: string;
  let sessionId: string;
  let tmuxName: string;

  test.beforeAll(async () => {
    await waitForHealthy();
    repoPath = await createTestRepo('test-repo-sync');
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

  test('server sends sync message with screen and cursor', async ({ page }) => {
    test.setTimeout(30_000);

    // Set up WebSocket listener BEFORE navigating so we capture the connection
    let syncMessage: Record<string, unknown> | null = null;
    const syncReceived = new Promise<void>((resolve) => {
      page.on('websocket', (ws) => {
        if (ws.url().includes('/ws/terminal/')) {
          ws.on('framereceived', (frame) => {
            if (typeof frame.payload === 'string') {
              try {
                const msg = JSON.parse(frame.payload as string);
                if (msg.type === 'sync' && !syncMessage) {
                  syncMessage = msg;
                  resolve();
                }
              } catch {
                // Not JSON, ignore
              }
            }
          });
        }
      });
    });

    await openTerminal(page, sessionId, tmuxName);

    // Output content so there's something to sync
    const sentinel = sendTmuxCommandWithSentinel(tmuxName, 'echo "sync-test-line"');
    await waitForSentinel(sessionId, sentinel);

    // Wait for sync message (initial fires at 500ms, may be delayed by activity guard)
    await Promise.race([
      syncReceived,
      new Promise((_, reject) =>
        setTimeout(() => reject(new Error('No sync message received within 15s')), 15_000)
      ),
    ]);

    // Verify sync message structure
    expect(syncMessage).toBeTruthy();
    expect((syncMessage as any).type).toBe('sync');
    expect(syncMessage).toHaveProperty('screen');
    expect(typeof (syncMessage as any).screen).toBe('string');
    expect(syncMessage).toHaveProperty('cursor');

    const cursor = (syncMessage as any).cursor;
    expect(typeof cursor.row).toBe('number');
    expect(typeof cursor.col).toBe('number');
    expect(typeof cursor.visible).toBe('boolean');
  });

  test('sync corrects desynced xterm.js buffer', async ({ page }) => {
    test.setTimeout(60_000);

    await openTerminal(page, sessionId, tmuxName);

    // Output known content
    const sentinel = sendTmuxCommandWithSentinel(tmuxName, 'echo "stable-baseline-content"');
    await waitForSentinel(sessionId, sentinel);

    // Verify baseline match
    await assertTerminalMatchesTmux(page, tmuxName);

    // Wait for activity guard to clear (>500ms since last binary data)
    await new Promise((r) => setTimeout(r, 700));

    // Corrupt xterm.js buffer with content that doesn't match tmux.
    // This simulates a bootstrap desync where xterm.js shows wrong content.
    await page.evaluate(() => {
      const terminal = (window as any).__schmuxTerminal;
      if (!terminal) throw new Error('__schmuxTerminal not found');
      terminal.write('\x1b[H\x1b[2JCORRUPTED_BUFFER_CONTENT_12345');
    });

    // Wait for xterm.js to process the write
    await new Promise((r) => setTimeout(r, 200));

    // Verify the buffer is actually corrupted (doesn't match tmux)
    const xtermAfterCorruption = await readXtermBuffer(page);
    const hasCorruption = xtermAfterCorruption.some((line) =>
      line.includes('CORRUPTED_BUFFER_CONTENT_12345')
    );
    expect(hasCorruption).toBe(true);

    // Wait for the sync goroutine to detect the desync and correct it.
    // The sync fires every 10s after the initial 500ms check, so we need
    // patience. Poll assertTerminalMatchesTmux (which retries internally
    // for ~3s) in a loop with a 15s outer deadline.
    const deadline = Date.now() + 15_000;
    let corrected = false;

    while (Date.now() < deadline && !corrected) {
      try {
        await assertTerminalMatchesTmux(page, tmuxName);
        corrected = true;
      } catch {
        // Sync hasn't corrected yet, wait before retrying
        await new Promise((r) => setTimeout(r, 1000));
      }
    }

    expect(corrected).toBe(true);
  });

  test('syncResult message sent back to server', async ({ page }) => {
    test.setTimeout(60_000);

    // Track syncResult messages sent by the frontend
    const syncResults: Array<{ corrected: boolean; diffRows: number[] }> = [];
    page.on('websocket', (ws) => {
      if (ws.url().includes('/ws/terminal/')) {
        ws.on('framesent', (frame) => {
          if (typeof frame.payload === 'string') {
            try {
              const msg = JSON.parse(frame.payload as string);
              if (msg.type === 'syncResult') {
                syncResults.push(JSON.parse(msg.data));
              }
            } catch {
              // Not JSON, ignore
            }
          }
        });
      }
    });

    await openTerminal(page, sessionId, tmuxName);

    // Output content and wait for it to settle
    const sentinel = sendTmuxCommandWithSentinel(tmuxName, 'echo "syncresult-test"');
    await waitForSentinel(sessionId, sentinel);

    // Wait for the sync goroutine to fire and the frontend to respond.
    // The initial sync fires at 500ms. The frontend should send a syncResult
    // regardless of whether a correction was needed.
    const resultDeadline = Date.now() + 15_000;
    while (Date.now() < resultDeadline && syncResults.length === 0) {
      await new Promise((r) => setTimeout(r, 200));
    }

    expect(syncResults.length).toBeGreaterThan(0);

    // Verify syncResult structure
    const result = syncResults[0];
    expect(typeof result.corrected).toBe('boolean');
    expect(Array.isArray(result.diffRows)).toBe(true);
  });
});
