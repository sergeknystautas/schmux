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
  assertCursorMatchesTmux,
  getTmuxSessionName,
  getTmuxCursorPosition,
  getXtermCursorPosition,
  readXtermBuffer,
  openTerminal,
} from './helpers-terminal';

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

    // Wait for sync message (initial fires at 5s, may be delayed by activity guard)
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

    // Corrupt xterm.js buffer with content that doesn't match tmux.
    // We corrupt immediately after baseline verification so that the
    // corruption exists when the first sync fires (initial sync at 5s
    // from connection). The activity guard (2s since last binary data)
    // will have cleared by then since sentinel data arrived ~3-4s earlier.
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
    // The initial sync fires at 5s from connection, then every 60s.
    // Since we corrupted early, the first sync should catch it.
    // Use a 25s deadline to allow margin for Docker container overhead
    // and the activity guard (2s since last binary data).
    const deadline = Date.now() + 25_000;
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
    // The initial sync fires at 5s. The frontend should send a syncResult
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

  test('sync corrects cursor-only desync', async ({ page }) => {
    test.setTimeout(60_000);

    await openTerminal(page, sessionId, tmuxName);

    // Output known content and wait for it to settle
    const sentinel = sendTmuxCommandWithSentinel(tmuxName, 'echo "cursor-only-desync-test"');
    await waitForSentinel(sessionId, sentinel);

    // Verify baseline: both content and cursor match
    await assertTerminalMatchesTmux(page, tmuxName);
    await assertCursorMatchesTmux(page, tmuxName);

    // Corrupt ONLY the cursor position in xterm.js (move cursor to row 1, col 0)
    // without changing any screen content. This simulates the exact bug:
    // a dropped output event contained a cursor repositioning sequence.
    await page.evaluate(() => {
      const terminal = (window as any).__schmuxTerminal;
      if (!terminal) throw new Error('__schmuxTerminal not found');
      // CUP to row 1, col 1 (1-indexed) — moves cursor to top-left
      terminal.write('\x1b[1;1H');
    });

    // Wait for xterm.js to process the write
    await new Promise((r) => setTimeout(r, 200));

    // Verify the cursor IS desynced now
    const xtermCursor = await getXtermCursorPosition(page);
    const tmuxCursor = getTmuxCursorPosition(tmuxName);
    // The xterm cursor should be at (0,0) after our corruption
    expect(xtermCursor.x).toBe(0);
    expect(xtermCursor.y).toBe(0);
    // The tmux cursor should be somewhere else (after the echo command)
    expect(tmuxCursor.x !== 0 || tmuxCursor.y !== 0).toBe(true);

    // Wait for the sync goroutine to detect and correct the cursor-only desync.
    // Initial sync fires at 5s, so we give it 15s total.
    const deadline = Date.now() + 15_000;
    let corrected = false;

    while (Date.now() < deadline && !corrected) {
      try {
        await assertCursorMatchesTmux(page, tmuxName);
        corrected = true;
      } catch {
        await new Promise((r) => setTimeout(r, 1000));
      }
    }

    expect(corrected).toBe(true);
  });
});
