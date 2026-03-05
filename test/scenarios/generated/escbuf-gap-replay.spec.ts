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
  assertCursorMatchesTmux,
  getTmuxSessionName,
  clearTmuxHistory,
} from './helpers-terminal';

// ---------------------------------------------------------------------------
// Reuse the openTerminal pattern from gap-detection.spec.ts
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

/**
 * Read the gap snapshot from __schmuxStream.diagnostics via page.evaluate().
 */
async function getGapSnapshot(page: Page): Promise<{
  gapsDetected: number;
  gapRequestsSent: number;
  gapFramesDeduped: number;
  gapReplayWritten: number;
  emptySeqFrames: number;
  lastReceivedSeq: string;
}> {
  return page.evaluate(() => {
    const stream = (window as any).__schmuxStream;
    if (!stream) throw new Error('__schmuxStream not found on window');
    if (!stream.diagnostics) throw new Error('diagnostics not enabled on __schmuxStream');
    return stream.diagnostics.gapSnapshot();
  });
}

// ---------------------------------------------------------------------------
// Escbuf holdback & gap replay scenario tests
// ---------------------------------------------------------------------------

test.describe.serial('Escbuf holdback & gap replay fixes', () => {
  let repoPath: string;
  let sessionId: string;
  let tmuxName: string;

  test.beforeAll(async () => {
    await waitForHealthy();
    repoPath = await createTestRepo('test-repo-escbuf-gap');
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

  test('ANSI-heavy output produces no phantom gaps', async ({ page }) => {
    test.setTimeout(60_000);

    await openTerminal(page, sessionId, tmuxName);

    // Generate ANSI-heavy output with CSI color sequences that are likely to
    // trigger escbuf holdback (partial escape sequences at frame boundaries).
    const sentinel = sendTmuxCommandWithSentinel(
      tmuxName,
      "for i in $(seq 1 200); do printf '\\033[38;5;196m\\033[48;5;21mColored %d\\033[0m\\n' $i; done"
    );
    await waitForSentinel(sessionId, sentinel);

    // Let rendering settle
    await new Promise((r) => setTimeout(r, 500));

    const snapshot = await getGapSnapshot(page);

    // No phantom gaps should be triggered — empty-data frames preserve seq continuity
    expect(snapshot.gapsDetected).toBe(0);

    // emptySeqFrames counter is being tracked (may be 0 if no holdback occurred)
    expect(snapshot.emptySeqFrames).toBeGreaterThanOrEqual(0);

    // Terminal content matches tmux ground truth
    await assertTerminalMatchesTmux(page, tmuxName);

    console.log(
      `[escbuf] emptySeqFrames=${snapshot.emptySeqFrames}, gapsDetected=${snapshot.gapsDetected}`
    );
  });

  test('sequence numbers are strictly contiguous during ANSI output', async ({ page }) => {
    test.setTimeout(60_000);

    // Collect sequence numbers from binary WebSocket frames
    const seqs: number[] = [];
    let bootstrapCompleteReceived = false;
    let liveStartIndex = -1;

    page.on('websocket', (ws) => {
      if (ws.url().includes('/ws/terminal/')) {
        ws.on('framereceived', (frame) => {
          if (typeof frame.payload !== 'string') {
            const buf = Buffer.from(frame.payload as ArrayBuffer);
            if (buf.length >= 8) {
              seqs.push(Number(buf.readBigUInt64BE(0)));
              if (bootstrapCompleteReceived && liveStartIndex === -1) {
                liveStartIndex = seqs.length - 1;
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

    await openTerminal(page, sessionId, tmuxName);

    // Generate the same ANSI-heavy output
    const sentinel = sendTmuxCommandWithSentinel(
      tmuxName,
      "for i in $(seq 1 200); do printf '\\033[38;5;196m\\033[48;5;21mColored %d\\033[0m\\n' $i; done"
    );
    await waitForSentinel(sessionId, sentinel);
    await new Promise((r) => setTimeout(r, 500));

    // Should have received binary frames
    expect(seqs.length).toBeGreaterThan(0);

    // After bootstrapComplete, live frames must be strictly contiguous
    if (liveStartIndex >= 0 && liveStartIndex < seqs.length - 1) {
      for (let i = liveStartIndex; i < seqs.length - 1; i++) {
        const gap = seqs[i + 1] - seqs[i];
        expect(gap).toBeLessThanOrEqual(1);
        // Strictly: next seq should be exactly current + 1 for live frames
        if (gap !== 0) {
          // Allow gap of 0 (dedup) or 1 (next seq), but not > 1
          expect(gap).toBe(1);
        }
      }
      console.log(`[escbuf] Verified ${seqs.length - liveStartIndex} live frames are contiguous`);
    } else {
      console.log('[escbuf] No live frames received after bootstrap (bootstrap-only session)');
    }
  });

  test('flood with gap recovery produces correct terminal state', async ({ page }) => {
    test.setTimeout(90_000);

    await openTerminal(page, sessionId, tmuxName);

    // Generate massive flood with ANSI coloring to increase escbuf holdback likelihood
    const sentinel = sendTmuxCommandWithSentinel(
      tmuxName,
      "for i in $(seq 1 5000); do printf '\\033[38;5;%dm flood-line-%d-padding-AAAA\\033[0m\\n' $((i % 256)) $i; done"
    );
    await waitForSentinel(sessionId, sentinel, 45_000);

    // Settling time for any gap replay to complete
    await new Promise((r) => setTimeout(r, 1000));

    const snapshot = await getGapSnapshot(page);

    // Critical regression detector: no replay frames should bypass dedup.
    // If this is non-zero, the per-entry replay fix has regressed.
    expect(snapshot.gapReplayWritten).toBe(0);

    // Terminal content matches tmux ground truth
    await assertTerminalMatchesTmux(page, tmuxName);

    // Log diagnostics
    console.log(
      `[escbuf] gapsDetected=${snapshot.gapsDetected}, gapFramesDeduped=${snapshot.gapFramesDeduped}, ` +
        `gapReplayWritten=${snapshot.gapReplayWritten}, emptySeqFrames=${snapshot.emptySeqFrames}`
    );
  });

  test('rapid cursor-positioning output during flood (regression test for original desync)', async ({
    page,
  }) => {
    test.setTimeout(90_000);

    await openTerminal(page, sessionId, tmuxName);

    // Start a background flood while simultaneously sending rapid CSI
    // cursor-positioning + colored text. This simulates a TUI status bar
    // updating during heavy output — the exact scenario that caused the
    // original cursor-jump bug.
    const sentinel = sendTmuxCommandWithSentinel(
      tmuxName,
      [
        // Background flood
        'for i in $(seq 1 500); do echo "flood-line-$i"; done &',
        // Rapid cursor-positioning over the flood (simulates TUI status bar)
        "for i in $(seq 1 50); do printf '\\033[1;1H\\033[2K\\033[32mStatus: %d\\033[0m' $i; sleep 0.02; done",
        // Wait for background flood to finish
        'wait',
      ].join(' && ')
    );
    await waitForSentinel(sessionId, sentinel);

    // Settling time
    await new Promise((r) => setTimeout(r, 1000));

    const snapshot = await getGapSnapshot(page);

    // No replay frames should bypass dedup
    expect(snapshot.gapReplayWritten).toBe(0);

    // Visible screen matches tmux
    await assertTerminalMatchesTmux(page, tmuxName);

    // Cursor position matches tmux
    await assertCursorMatchesTmux(page, tmuxName);

    console.log(
      `[escbuf] desync regression: gapsDetected=${snapshot.gapsDetected}, ` +
        `gapReplayWritten=${snapshot.gapReplayWritten}, emptySeqFrames=${snapshot.emptySeqFrames}`
    );
  });
});
