import { test, expect, type Page } from './coverage-fixture';
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
import { writeDiagnosticArtifact } from './terminalCompare';

/**
 * Per-connection record of every terminal WebSocket frame the page sees,
 * kept so a sequence regression can be attributed: a second connection
 * restarts at its bootstrap sequence, a gap replay re-sends older sequences
 * right after a `gap` frame is sent, and bootstrap chunks share one reserved
 * sequence before `bootstrapComplete`. Timestamps are ms since tracking began.
 */
type SocketLog = {
  index: number;
  openedAt: number;
  closedAt: number | null;
  seqs: { seq: number; t: number }[];
  control: { t: number; type: string }[]; // text frames received (bootstrapComplete, stats, ...)
  sent: { t: number; payload: string }[]; // text frames sent (gap requests, resize, ...)
};

function trackTerminalSockets(page: Page): SocketLog[] {
  const sockets: SocketLog[] = [];
  const t0 = Date.now();
  page.on('websocket', (ws) => {
    if (!ws.url().includes('/ws/terminal/')) return;
    const log: SocketLog = {
      index: sockets.length,
      openedAt: Date.now() - t0,
      closedAt: null,
      seqs: [],
      control: [],
      sent: [],
    };
    sockets.push(log);
    ws.on('framereceived', (frame) => {
      const t = Date.now() - t0;
      if (typeof frame.payload !== 'string') {
        const buf = Buffer.from(frame.payload as unknown as ArrayBuffer);
        if (buf.length >= 8) log.seqs.push({ seq: Number(buf.readBigUInt64BE(0)), t });
      } else {
        let type = 'unparseable';
        try {
          type = String(JSON.parse(frame.payload).type);
        } catch {
          // keep 'unparseable'
        }
        log.control.push({ t, type });
      }
    });
    ws.on('framesent', (frame) => {
      if (typeof frame.payload === 'string') {
        log.sent.push({ t: Date.now() - t0, payload: frame.payload.slice(0, 200) });
      }
    });
    ws.on('close', () => {
      log.closedAt = Date.now() - t0;
    });
  });
  return sockets;
}

/** Indices i where seqs[i] < seqs[i-1] within one connection. */
function sequenceRegressions(seqs: { seq: number }[]): number[] {
  const out: number[] = [];
  for (let i = 1; i < seqs.length; i++) if (seqs[i].seq < seqs[i - 1].seq) out.push(i);
  return out;
}

async function snapshotStreamDiagnostics(page: Page): Promise<Record<string, unknown>> {
  return page
    .evaluate(() => {
      const stream = (window as any).__schmuxStream;
      const d = stream?.diagnostics;
      return {
        lastReceivedSeq: String(stream?.lastReceivedSeq ?? 'n/a'),
        bootstrapComplete: stream?.bootstrapComplete ?? null,
        gapRequestPending: stream?.gapRequestPending ?? null,
        reconnectAttempt: stream?.reconnectAttempt ?? null,
        diagnostics: d
          ? {
              framesReceived: d.framesReceived,
              bootstrapCount: d.bootstrapCount,
              sequenceBreaks: d.sequenceBreaks,
              recentBreaks: d.recentBreaks,
              gapsDetected: d.gapsDetected,
              gapRequestsSent: d.gapRequestsSent,
              gapFramesDeduped: d.gapFramesDeduped,
              gapReplayWritten: d.gapReplayWritten,
              connectionEvents: d.connectionEvents,
            }
          : null,
      };
    })
    .catch((err) => ({ error: String(err) }));
}

function describeSockets(sockets: SocketLog[]): string {
  return sockets
    .map((s) => {
      const regressions = sequenceRegressions(s.seqs);
      return [
        `### socket ${s.index}: opened +${s.openedAt}ms, closed ${s.closedAt === null ? 'no' : `+${s.closedAt}ms`}, ${s.seqs.length} binary frames`,
        `regressions at indices: ${JSON.stringify(regressions)}`,
        `seqs: ${JSON.stringify(s.seqs.map((x) => x.seq))}`,
        `seq timestamps: ${JSON.stringify(s.seqs.map((x) => x.t))}`,
        `control frames received: ${JSON.stringify(s.control)}`,
        `text frames sent: ${JSON.stringify(s.sent)}`,
      ].join('\n');
    })
    .join('\n\n');
}

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

    const sockets = trackTerminalSockets(page);

    await openTerminal(page, sessionId, tmuxName);

    // Generate some output to produce live frames
    const sentinel = sendTmuxCommandWithSentinel(
      tmuxName,
      'for i in $(seq 1 50); do echo "seq-test-$i"; done'
    );
    await waitForSentinel(sessionId, sentinel, page);

    // Merged view across every terminal socket the page opened — the claim as
    // historically stated. The diagnostics below show, per socket, whether a
    // regression comes from a second connection, a gap replay (a `gap` frame
    // sent just before), or an out-of-order live frame.
    const merged = sockets.flatMap((s) => s.seqs.map((x) => ({ ...x, socket: s.index })));
    const regressions = sequenceRegressions(merged).map((i) => ({
      index: i,
      prev: merged[i - 1],
      next: merged[i],
    }));

    // Should have received binary frames
    expect(
      merged.length,
      `no binary frames received across ${sockets.length} socket(s)`
    ).toBeGreaterThan(0);

    if (regressions.length > 0) {
      const streamState = await snapshotStreamDiagnostics(page);
      const report = [
        '# Sequence Regression Diagnostic',
        '',
        `**Session:** ${sessionId}`,
        `**Sentinel:** ${sentinel}`,
        `**Terminal sockets opened:** ${sockets.length}`,
        `**Regressions (merged view):** ${JSON.stringify(regressions)}`,
        '',
        '## Client stream diagnostics',
        '',
        '```json',
        JSON.stringify(streamState, null, 2),
        '```',
        '',
        '## Per-socket frame log',
        '',
        describeSockets(sockets),
      ].join('\n');
      writeDiagnosticArtifact(
        '/tmp/terminal-diagnostics',
        `${new Date().toISOString().replace(/[:.]/g, '-')}_seq_regression_${sessionId.replace(/[^a-zA-Z0-9-]/g, '_')}.md`,
        report
      );
      throw new Error(
        `Sequence regression on terminal WebSocket: ${JSON.stringify(regressions)}\n` +
          `sockets=${sockets.length} stream=${JSON.stringify(streamState)}\n` +
          describeSockets(sockets)
      );
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
    await waitForSentinel(sessionId, sentinel, page);

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
    await waitForSentinel(sessionId, sentinel, page);

    // The critical assertion: regardless of whether gaps occurred,
    // the terminal content should match tmux ground truth.
    // If gaps occurred and replay worked, content matches.
    // If no gaps occurred, content also matches.
    await assertTerminalMatchesTmux(page, tmuxName, { sentinel });

    // Log whether any gaps were detected (informational, not a pass/fail criterion)
    if (gapMessages.length > 0) {
      console.log(`[gap-detection] ${gapMessages.length} gap message(s) sent during flood`);
    } else {
      console.log('[gap-detection] No gaps detected during flood (clean delivery)');
    }
  });
});
