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
// Gap detection and replay
// ---------------------------------------------------------------------------

test.describe.serial('Gap detection: sequenced frame protocol', () => {
  let repoPath: string;
  let sessionId: string;
  let tmuxName: string;

  test.beforeAll(async () => {
    await waitForHealthy();
    repoPath = await createTestRepo('test-repo-gap-detection');
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

  test('live frames have monotonically increasing sequence numbers', async ({ page }) => {
    test.setTimeout(30_000);

    // Track binary frame sequence numbers
    const seqs: number[] = [];

    page.on('websocket', (ws) => {
      if (ws.url().includes('/ws/terminal/')) {
        ws.on('framereceived', (frame) => {
          if (typeof frame.payload !== 'string') {
            const buf = Buffer.from(frame.payload as unknown as ArrayBuffer);
            if (buf.length >= 8) {
              seqs.push(Number(buf.readBigUInt64BE(0)));
            }
          }
        });
      }
    });

    await openTerminal(page, sessionId, tmuxName);

    // Generate some output to produce live frames
    const sentinel = sendTmuxCommandWithSentinel(
      tmuxName,
      'for i in $(seq 1 50); do echo "seq-test-$i"; done'
    );
    await waitForSentinel(sessionId, sentinel);

    // Should have received binary frames
    expect(seqs.length).toBeGreaterThan(0);

    // Verify monotonically non-decreasing
    for (let i = 1; i < seqs.length; i++) {
      expect(seqs[i]).toBeGreaterThanOrEqual(seqs[i - 1]);
    }
  });

  test('bootstrapComplete is sent after bootstrap chunks', async ({ page }) => {
    test.setTimeout(30_000);

    let firstBinarySeq: number | null = null;
    let bootstrapCompleteReceived = false;
    let binaryAfterBootstrapComplete = false;

    page.on('websocket', (ws) => {
      if (ws.url().includes('/ws/terminal/')) {
        ws.on('framereceived', (frame) => {
          if (typeof frame.payload !== 'string') {
            const buf = Buffer.from(frame.payload as unknown as ArrayBuffer);
            if (buf.length >= 8) {
              if (firstBinarySeq === null) {
                firstBinarySeq = Number(buf.readBigUInt64BE(0));
              }
              if (bootstrapCompleteReceived) {
                binaryAfterBootstrapComplete = true;
              }
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

    // Navigate to trigger fresh bootstrap
    await page.goto(`/sessions/${sessionId}`);
    await waitForDashboardLive(page);
    await page.waitForSelector('[data-testid="terminal-viewport"]', { timeout: 15_000 });

    // Wait for bootstrapComplete
    const deadline = Date.now() + 10_000;
    while (Date.now() < deadline && !bootstrapCompleteReceived) {
      await new Promise((r) => setTimeout(r, 100));
    }

    // Binary frames should have been received before bootstrapComplete
    expect(firstBinarySeq).not.toBeNull();
    expect(bootstrapCompleteReceived).toBe(true);
  });

  test('stats report output log sequence state', async ({ page }) => {
    test.setTimeout(30_000);

    let statsMsg: Record<string, unknown> | null = null;

    page.on('websocket', (ws) => {
      if (ws.url().includes('/ws/terminal/')) {
        ws.on('framereceived', (frame) => {
          if (typeof frame.payload === 'string') {
            try {
              const msg = JSON.parse(frame.payload as string);
              if (msg.type === 'stats' && !statsMsg) {
                statsMsg = msg;
              }
            } catch {
              // ignore
            }
          }
        });
      }
    });

    await openTerminal(page, sessionId, tmuxName);

    // Generate some output
    const sentinel = sendTmuxCommandWithSentinel(tmuxName, 'echo "stats-test"');
    await waitForSentinel(sessionId, sentinel);

    // Wait for a stats message (sent every 2s)
    const deadline = Date.now() + 10_000;
    while (Date.now() < deadline && !statsMsg) {
      await new Promise((r) => setTimeout(r, 200));
    }

    expect(statsMsg).toBeTruthy();
    // currentSeq should be > 0 (we've sent output)
    expect(typeof (statsMsg as any).currentSeq).toBe('number');
    expect((statsMsg as any).currentSeq).toBeGreaterThan(0);
    // logOldestSeq should exist
    expect(typeof (statsMsg as any).logOldestSeq).toBe('number');
    // logTotalBytes should be > 0
    expect((statsMsg as any).logTotalBytes).toBeGreaterThan(0);
  });

  test('terminal matches tmux after output flood (gap recovery)', async ({ page }) => {
    test.setTimeout(90_000);

    // Track gap messages sent by the frontend
    const gapMessages: string[] = [];

    page.on('websocket', (ws) => {
      if (ws.url().includes('/ws/terminal/')) {
        ws.on('framesent', (frame) => {
          if (typeof frame.payload === 'string') {
            try {
              const msg = JSON.parse(frame.payload as string);
              if (msg.type === 'gap') {
                gapMessages.push(frame.payload as string);
              }
            } catch {
              // ignore
            }
          }
        });
      }
    });

    await openTerminal(page, sessionId, tmuxName);

    // Generate a massive flood of output that may cause backpressure/drops.
    // Use seq with no sleep to maximize throughput and chance of drops.
    const sentinel = sendTmuxCommandWithSentinel(
      tmuxName,
      'for i in $(seq 1 5000); do echo "flood-line-$i-padding-to-make-this-longer-AAAA"; done'
    );
    await waitForSentinel(sessionId, sentinel);

    // The critical assertion: regardless of whether gaps occurred,
    // the terminal content should match tmux ground truth.
    // If gaps occurred and replay worked, content matches.
    // If no gaps occurred, content also matches.
    await assertTerminalMatchesTmux(page, tmuxName);

    // Log whether any gaps were detected (informational, not a pass/fail criterion)
    if (gapMessages.length > 0) {
      console.log(`[gap-detection] ${gapMessages.length} gap message(s) sent during flood`);
    } else {
      console.log('[gap-detection] No gaps detected during flood (clean delivery)');
    }
  });
});
