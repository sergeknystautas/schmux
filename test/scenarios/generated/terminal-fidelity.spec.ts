import { test } from './coverage-fixture';
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
  assertCursorVisibilityMatchesTmux,
  getTmuxSessionName,
  clearTmuxHistory,
  openTerminal,
} from './helpers-terminal';
import { waitForTerminalOutput } from './helpers';

// ---------------------------------------------------------------------------
// Tier 1: Encoding
// ---------------------------------------------------------------------------

test.describe.serial('Terminal fidelity: encoding', () => {
  let repoPath: string;
  let sessionId: string;
  let tmuxName: string;

  test.beforeAll(async () => {
    await waitForHealthy();
    repoPath = await createTestRepo('test-repo-fidelity');
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

  test('ascii lines', async ({ page }) => {
    test.setTimeout(30_000);

    await openTerminal(page, sessionId, tmuxName);

    const sentinel = sendTmuxCommandWithSentinel(
      tmuxName,
      'for i in $(seq 1 10); do echo "Line $i: the quick brown fox"; done'
    );
    await waitForSentinel(sessionId, sentinel);

    await assertTerminalMatchesTmux(page, tmuxName);
  });

  test('utf8 box drawing', async ({ page }) => {
    test.setTimeout(30_000);

    await openTerminal(page, sessionId, tmuxName);

    const sentinel = sendTmuxCommandWithSentinel(
      tmuxName,
      "printf '\\342\\224\\214\\342\\224\\200\\342\\224\\200\\342\\224\\200\\342\\224\\200\\342\\224\\200\\342\\224\\200\\342\\224\\200\\342\\224\\200\\342\\224\\200\\342\\224\\200\\342\\224\\220\\n\\342\\224\\202 schmux   \\342\\224\\202\\n\\342\\224\\202 terminal \\342\\224\\202\\n\\342\\224\\224\\342\\224\\200\\342\\224\\200\\342\\224\\200\\342\\224\\200\\342\\224\\200\\342\\224\\200\\342\\224\\200\\342\\224\\200\\342\\224\\200\\342\\224\\200\\342\\224\\230\\n'"
    );
    await waitForSentinel(sessionId, sentinel);

    await assertTerminalMatchesTmux(page, tmuxName);
  });

  test('utf8 mixed characters', async ({ page }) => {
    test.setTimeout(30_000);

    await openTerminal(page, sessionId, tmuxName);

    const sentinel = sendTmuxCommandWithSentinel(
      tmuxName,
      "printf '\\xe2\\x9c\\x93 Pass: \\xe6\\x97\\xa5\\xe6\\x9c\\xac\\xe8\\xaa\\x9e\\xe3\\x83\\x86\\xe3\\x82\\xb9\\xe3\\x83\\x88\\n\\xe2\\x9c\\x97 Fail: \\xe2\\x94\\x80\\xe2\\x94\\x80\\xe2\\x94\\x80\\xe2\\x94\\x80\\xe2\\x94\\x80\\xe2\\x94\\x80\\n\\xe2\\x9a\\xa1 Done\\n'"
    );
    await waitForSentinel(sessionId, sentinel);

    await assertTerminalMatchesTmux(page, tmuxName);
  });

  test('ansi colors preserve text', async ({ page }) => {
    test.setTimeout(30_000);

    await openTerminal(page, sessionId, tmuxName);

    const sentinel = sendTmuxCommandWithSentinel(
      tmuxName,
      "printf '\\033[31mred \\033[32mgreen \\033[34mblue \\033[0mnormal\\n'"
    );
    await waitForSentinel(sessionId, sentinel);

    await assertTerminalMatchesTmux(page, tmuxName);
  });

  test('long line wrapping', async ({ page }) => {
    test.setTimeout(30_000);

    await openTerminal(page, sessionId, tmuxName);

    const sentinel = sendTmuxCommandWithSentinel(tmuxName, 'python3 -c "print(\'A\' * 200)"');
    await waitForSentinel(sessionId, sentinel);

    await assertTerminalMatchesTmux(page, tmuxName);
  });
});

// ---------------------------------------------------------------------------
// Tier 2: Cursor movement
// ---------------------------------------------------------------------------

test.describe.serial('Terminal fidelity: cursor movement', () => {
  let repoPath: string;
  let sessionId: string;
  let tmuxName: string;

  test.beforeAll(async () => {
    await waitForHealthy();
    repoPath = await createTestRepo('test-repo-fidelity-cursor');
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

  test('carriage return overwrites', async ({ page }) => {
    test.setTimeout(30_000);

    await openTerminal(page, sessionId, tmuxName);

    const sentinel = sendTmuxCommandWithSentinel(
      tmuxName,
      "for i in 20 40 60 80 100; do printf '\\rProgress: %d%%' $i; sleep 0.1; done; echo"
    );
    await waitForSentinel(sessionId, sentinel);

    await assertTerminalMatchesTmux(page, tmuxName);
  });

  test('cursor positioning CSI H', async ({ page }) => {
    test.setTimeout(30_000);

    await openTerminal(page, sessionId, tmuxName);

    const sentinel = sendTmuxCommandWithSentinel(
      tmuxName,
      "printf '\\033[2J\\033[3;5HRow3Col5\\033[7;20HRow7Col20\\033[10;1H'"
    );
    await waitForSentinel(sessionId, sentinel);

    await assertTerminalMatchesTmux(page, tmuxName);
  });

  test('erase in line', async ({ page }) => {
    test.setTimeout(30_000);

    await openTerminal(page, sessionId, tmuxName);

    const sentinel = sendTmuxCommandWithSentinel(tmuxName, "printf 'AAABBBCCC\\033[6D\\033[K\\n'");
    await waitForSentinel(sessionId, sentinel);

    await assertTerminalMatchesTmux(page, tmuxName);
  });

  test('erase in display', async ({ page }) => {
    test.setTimeout(30_000);

    await openTerminal(page, sessionId, tmuxName);

    const sentinel = sendTmuxCommandWithSentinel(
      tmuxName,
      'for i in $(seq 1 5); do echo "fill line $i"; done; printf \'\\033[2J\\033[HCleared\\n\''
    );
    await waitForSentinel(sessionId, sentinel);

    await assertTerminalMatchesTmux(page, tmuxName);
  });

  test('scroll region', async ({ page }) => {
    test.setTimeout(30_000);
    // Scroll region CSI 3;8r needs enough visible rows to avoid overflow
    // artifacts. Set a tall viewport so the terminal has ≥20 rows regardless
    // of how many workspaces other specs added to the sidebar.
    await page.setViewportSize({ width: 1280, height: 1080 });

    await openTerminal(page, sessionId, tmuxName);

    // Verify tmux and xterm.js are synced before scroll region test.
    // openTerminal can race under load — if the clear hasn't fully propagated
    // through the control-mode pipeline, the scroll region output starts at
    // the wrong cursor position, producing a row offset.
    await assertTerminalMatchesTmux(page, tmuxName);

    // Set scroll region to rows 3-8, output lines within it, then reset
    const sentinel = sendTmuxCommandWithSentinel(
      tmuxName,
      "printf '\\033[3;8r'; for i in $(seq 1 10); do echo \"scroll-$i\"; done; printf '\\033[r'"
    );
    await waitForSentinel(sessionId, sentinel);

    await assertTerminalMatchesTmux(page, tmuxName);
  });

  test('cursor hiding preserved after bootstrap', async ({ page }) => {
    test.setTimeout(60_000);

    await openTerminal(page, sessionId, tmuxName);

    // Hide the cursor via DECTCEM reset (same escape Claude Code uses)
    const sentinel = sendTmuxCommandWithSentinel(tmuxName, "printf '\\033[?25l'");
    await waitForSentinel(sessionId, sentinel);

    // Verify cursor is hidden in both tmux and xterm.js (live stream)
    await assertCursorVisibilityMatchesTmux(page, tmuxName);

    // Reload triggers a new WebSocket bootstrap with capture-pane + DECTCEM
    await page.reload();
    await waitForDashboardLive(page);
    await page.waitForSelector('[data-testid="terminal-viewport"]', { timeout: 15_000 });

    // Verify cursor remains hidden after bootstrap (retrying assertion handles rendering lag)
    await assertCursorVisibilityMatchesTmux(page, tmuxName);

    // Clean up: show cursor again so subsequent tests aren't affected
    sendTmuxCommand(tmuxName, "printf '\\033[?25h'");
    // Wait for cursor to become visible (state check instead of fixed delay)
    await assertCursorVisibilityMatchesTmux(page, tmuxName);
  });

  test('dense cursor repositioning: rapid CUP + content painting', async ({ page }) => {
    await openTerminal(page, sessionId, tmuxName);

    // Simulate TUI repaint: paint 10 different rows with positioned content.
    // This mimics how Claude Code repaints status bars, input fields, and
    // output regions using dense sequences of CSI H + content.
    const escapeSequence = [
      '\\033[1;1H\\033[2KStatus: running',
      '\\033[3;1H\\033[2K> Input line here',
      '\\033[5;1H\\033[2KOutput line 1: hello world',
      '\\033[6;1H\\033[2KOutput line 2: foo bar',
      '\\033[7;1H\\033[2KOutput line 3: baz qux',
      '\\033[10;1H\\033[2K[Progress: 50%]',
      '\\033[12;1H\\033[2K───────────────────',
      '\\033[15;1H\\033[2KFooter: 3 items',
      '\\033[3;17H', // Move cursor back to end of input line
    ].join('');
    const sentinel = sendTmuxCommandWithSentinel(tmuxName, `printf '${escapeSequence}'`);
    await waitForSentinel(sessionId, sentinel);

    await assertTerminalMatchesTmux(page, tmuxName);
    await assertCursorMatchesTmux(page, tmuxName);
  });

  test('rapid partial screen overwrite: update one region while others stay', async ({ page }) => {
    await openTerminal(page, sessionId, tmuxName);

    // Paint a stable frame first
    const frame1 = [
      '\\033[1;1H\\033[2KHeader: stable',
      '\\033[3;1H\\033[2KLine A',
      '\\033[4;1H\\033[2KLine B',
      '\\033[5;1H\\033[2KLine C',
      '\\033[7;1H\\033[2KFooter: stable',
    ].join('');
    let sentinel = sendTmuxCommandWithSentinel(tmuxName, `printf '${frame1}'`);
    await waitForSentinel(sessionId, sentinel);
    await assertTerminalMatchesTmux(page, tmuxName);

    // Now repaint ONLY the middle region (lines 3-5), leaving header and footer
    const frame2 = [
      '\\033[3;1H\\033[2KLine A updated!',
      '\\033[4;1H\\033[2KLine B updated!',
      '\\033[5;1H\\033[2KLine C updated!',
      '\\033[5;16H', // Cursor at end of last updated line
    ].join('');
    sentinel = sendTmuxCommandWithSentinel(tmuxName, `printf '${frame2}'`);
    await waitForSentinel(sessionId, sentinel);

    await assertTerminalMatchesTmux(page, tmuxName);
    await assertCursorMatchesTmux(page, tmuxName);
  });

  test('insert line (CSI L) shifts content down', async ({ page }) => {
    await openTerminal(page, sessionId, tmuxName);

    // Fill rows 1-5 with content, then insert a line at row 3
    const sequence = [
      '\\033[1;1HLine 1',
      '\\033[2;1HLine 2',
      '\\033[3;1HLine 3',
      '\\033[4;1HLine 4',
      '\\033[5;1HLine 5',
      '\\033[3;1H\\033[L', // Insert line at row 3 (pushes Line 3-5 down)
      '\\033[3;1HInserted!',
    ].join('');
    const sentinel = sendTmuxCommandWithSentinel(tmuxName, `printf '${sequence}'`);
    await waitForSentinel(sessionId, sentinel);

    await assertTerminalMatchesTmux(page, tmuxName);
  });

  test('delete line (CSI M) shifts content up', async ({ page }) => {
    await openTerminal(page, sessionId, tmuxName);

    // Fill rows 1-5, then delete row 2
    const sequence = [
      '\\033[1;1HLine 1',
      '\\033[2;1HLine 2',
      '\\033[3;1HLine 3',
      '\\033[4;1HLine 4',
      '\\033[5;1HLine 5',
      '\\033[2;1H\\033[M', // Delete line at row 2 (Line 3-5 shift up)
    ].join('');
    const sentinel = sendTmuxCommandWithSentinel(tmuxName, `printf '${sequence}'`);
    await waitForSentinel(sessionId, sentinel);

    await assertTerminalMatchesTmux(page, tmuxName);
  });
});

test.describe.serial('Terminal fidelity: alternate screen', () => {
  let repoPath: string;
  let sessionId: string;
  let tmuxName: string;

  test.beforeAll(async () => {
    await waitForHealthy();
    repoPath = await createTestRepo('test-repo-fidelity-altscreen');
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

  test('alternate screen roundtrip', async ({ page }) => {
    test.setTimeout(30_000);

    await openTerminal(page, sessionId, tmuxName);

    // Write content to the normal screen
    sendTmuxCommand(tmuxName, 'echo "normal-screen-content"');
    // Enter alt screen, draw something, exit alt screen
    const sentinel = sendTmuxCommandWithSentinel(
      tmuxName,
      "printf '\\033[?1049h\\033[2J\\033[1;1HAlt screen content\\033[?1049l'"
    );
    await waitForSentinel(sessionId, sentinel);

    // After exiting alt screen, normal screen content should be restored
    await assertTerminalMatchesTmux(page, tmuxName);
  });

  test('ncurses-style bordered panel', async ({ page }) => {
    test.setTimeout(30_000);

    await openTerminal(page, sessionId, tmuxName);

    // Enter alt screen and draw a bordered box with content
    const sentinel = sendTmuxCommandWithSentinel(
      tmuxName,
      "printf '\\033[?1049h\\033[2J" +
        '\\033[1;1H\\xe2\\x94\\x8c\\xe2\\x94\\x80\\xe2\\x94\\x80\\xe2\\x94\\x80\\xe2\\x94\\x80\\xe2\\x94\\x80\\xe2\\x94\\x80\\xe2\\x94\\x80\\xe2\\x94\\x80\\xe2\\x94\\x80\\xe2\\x94\\x80\\xe2\\x94\\x80\\xe2\\x94\\x80\\xe2\\x94\\x90' +
        '\\033[2;1H\\xe2\\x94\\x82 Dashboard  \\xe2\\x94\\x82' +
        '\\033[3;1H\\xe2\\x94\\x82  Status   \\xe2\\x94\\x82' +
        '\\033[4;1H\\xe2\\x94\\x94\\xe2\\x94\\x80\\xe2\\x94\\x80\\xe2\\x94\\x80\\xe2\\x94\\x80\\xe2\\x94\\x80\\xe2\\x94\\x80\\xe2\\x94\\x80\\xe2\\x94\\x80\\xe2\\x94\\x80\\xe2\\x94\\x80\\xe2\\x94\\x80\\xe2\\x94\\x80\\xe2\\x94\\x98' +
        "\\033[5;1H'"
    );
    await waitForSentinel(sessionId, sentinel);

    // Compare while still in alt screen
    await assertTerminalMatchesTmux(page, tmuxName);

    // Clean up: exit alt screen
    sendTmuxCommand(tmuxName, "printf '\\033[?1049l'");
  });
});

// ---------------------------------------------------------------------------
// Tier 4: Scrollback
// ---------------------------------------------------------------------------

test.describe.serial('Terminal fidelity: scrollback', () => {
  let repoPath: string;
  let sessionId: string;
  let tmuxName: string;

  test.beforeAll(async () => {
    await waitForHealthy();
    repoPath = await createTestRepo('test-repo-fidelity-scrollback');
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

  test('scrollback after bulk output', async ({ page }) => {
    test.setTimeout(60_000);

    await openTerminal(page, sessionId, tmuxName);

    const sentinel = sendTmuxCommandWithSentinel(
      tmuxName,
      'for i in $(seq 1 200); do echo "bulk-line-$i"; done'
    );
    await waitForSentinel(sessionId, sentinel);

    // Compare visible screen
    await assertTerminalMatchesTmux(page, tmuxName);

    // Compare scrollback (200 lines)
    await assertTerminalMatchesTmux(page, tmuxName, { scrollbackLines: 200 });
  });

  test('scrollback preserved after alt screen exit', async ({ page }) => {
    test.setTimeout(60_000);

    await openTerminal(page, sessionId, tmuxName);

    // Output 30 lines to build scrollback
    sendTmuxCommand(tmuxName, 'for i in $(seq 1 30); do echo "scrollback-line-$i"; done');
    // Enter and exit alt screen
    const sentinel = sendTmuxCommandWithSentinel(
      tmuxName,
      "printf '\\033[?1049h\\033[2J\\033[1;1HAlt content\\033[?1049l'"
    );
    await waitForSentinel(sessionId, sentinel);

    // Scrollback from before alt screen should still be intact
    await assertTerminalMatchesTmux(page, tmuxName, { scrollbackLines: 200 });
  });

  test('bootstrap matches scrollback after reconnect', async ({ page }) => {
    test.setTimeout(60_000);

    await openTerminal(page, sessionId, tmuxName);

    // Output some content
    const sentinel = sendTmuxCommandWithSentinel(
      tmuxName,
      'for i in $(seq 1 20); do echo "reconnect-line-$i"; done'
    );
    await waitForSentinel(sessionId, sentinel);

    // Verify first load
    await assertTerminalMatchesTmux(page, tmuxName);

    // Reload (new bootstrap)
    await page.reload();
    await waitForDashboardLive(page);
    await page.waitForSelector('[data-testid="terminal-viewport"]', { timeout: 15_000 });

    // Wait for bootstrap content to appear in xterm.js before comparing.
    // Under load (multiple sessions from prior tiers), the WebSocket bootstrap
    // can take several seconds to deliver capture-pane data.
    const bootstrapDeadline = Date.now() + 15_000;
    while (Date.now() < bootstrapDeadline) {
      const hasExpectedContent = await page.evaluate(() => {
        const terminal = (window as any).__schmuxTerminal;
        if (!terminal) return false;
        const buffer = terminal.buffer.active;
        for (let i = 0; i < buffer.baseY + terminal.rows; i++) {
          const line = buffer.getLine(i);
          if (line && line.translateToString(true).includes('reconnect-line')) return true;
        }
        return false;
      });
      if (hasExpectedContent) break;
      await new Promise((r) => setTimeout(r, 100));
    }

    // Verify after reload — use extra retries because bootstrap delivery under
    // load (4 concurrent sessions from prior tiers) can take 10+ seconds.
    await assertTerminalMatchesTmux(page, tmuxName, { maxRetries: 150 });
  });

  test('cursor position correct after bootstrap', async ({ page }) => {
    test.setTimeout(60_000);

    await openTerminal(page, sessionId, tmuxName);

    // Output some content so the prompt ends up partway down the screen
    // with the cursor after "$ " (or wherever the shell leaves it)
    const sentinel = sendTmuxCommandWithSentinel(
      tmuxName,
      'for i in $(seq 1 5); do echo "cursor-test-line-$i"; done'
    );
    await waitForSentinel(sessionId, sentinel);

    // Verify cursor matches before reload (live stream)
    await assertCursorMatchesTmux(page, tmuxName);

    // Reload triggers a new WebSocket bootstrap with capture-pane + CSI H
    await page.reload();
    await waitForDashboardLive(page);
    await page.waitForSelector('[data-testid="terminal-viewport"]', { timeout: 15_000 });

    // Verify cursor position matches after bootstrap (retrying assertion handles lag)
    await assertCursorMatchesTmux(page, tmuxName);
  });
});

// ---------------------------------------------------------------------------
// Tier 5: Compounding
// ---------------------------------------------------------------------------

test.describe.serial('Terminal fidelity: compounding', () => {
  let repoPath: string;
  let sessionId: string;
  let tmuxName: string;

  test.beforeAll(async () => {
    await waitForHealthy();
    repoPath = await createTestRepo('test-repo-fidelity-compound');
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
    // Longer timeout: this beforeAll runs after several other serial blocks
    // have already created sessions, so the daemon may be under heavier load.
    await waitForSessionRunning(sessionId, 30_000);
    tmuxName = await getTmuxSessionName(sessionId);
  });

  test('build log then TUI then exit', async ({ page }) => {
    test.setTimeout(90_000);

    // Navigate once; all stages use the live connection
    await openTerminal(page, sessionId, tmuxName);

    // Stage 1: 100-line colored build log
    const buildSentinel = sendTmuxCommandWithSentinel(
      tmuxName,
      "for i in $(seq 1 100); do printf '\\033[32m[BUILD]\\033[0m Step %d: compiling module_%d\\n' $i $i; done"
    );
    await waitForSentinel(sessionId, buildSentinel, 30_000);

    // Verify build log stage
    await assertTerminalMatchesTmux(page, tmuxName);

    // Stage 2: Enter alt screen with TUI
    const tuiSentinel = sendTmuxCommandWithSentinel(
      tmuxName,
      "printf '\\033[?1049h\\033[2J\\033[1;1H=== TUI Dashboard ===\\033[3;1HStatus: Running\\033[5;1HProgress: 100%%'"
    );
    await waitForSentinel(sessionId, tuiSentinel);

    // Verify TUI stage (alt screen)
    await assertTerminalMatchesTmux(page, tmuxName);

    // Stage 3: Exit alt screen
    const exitSentinel = sendTmuxCommandWithSentinel(tmuxName, "printf '\\033[?1049l'");
    await waitForSentinel(sessionId, exitSentinel);

    // Should be back to normal screen with build log in scrollback
    await assertTerminalMatchesTmux(page, tmuxName);
  });

  test('utf8 in output flood', async ({ page }) => {
    test.setTimeout(60_000);

    await openTerminal(page, sessionId, tmuxName);

    const sentinel = sendTmuxCommandWithSentinel(
      tmuxName,
      "for i in $(seq 1 100); do printf '\\xe2\\x9c\\x93 Line %d: \\xe4\\xb8\\xad\\xe6\\x96\\x87\\xe6\\xb5\\x8b\\xe8\\xaf\\x95 test-%d\\n' $i $i; done"
    );
    await waitForSentinel(sessionId, sentinel, 30_000);

    // assertTerminalMatchesTmux retries internally, handling rendering lag
    await assertTerminalMatchesTmux(page, tmuxName);
  });

  test('rapid alt screen toggles', async ({ page }) => {
    test.setTimeout(60_000);

    await openTerminal(page, sessionId, tmuxName);

    // Toggle alt screen 5 times with content between each
    for (let i = 1; i <= 5; i++) {
      sendTmuxCommand(
        tmuxName,
        `printf '\\033[?1049h\\033[2J\\033[1;1HAlt screen pass ${i}\\033[?1049l'`
      );
      sendTmuxCommand(tmuxName, `echo "Normal screen after toggle ${i}"`);
    }

    const sentinel = sendTmuxCommandWithSentinel(tmuxName, 'echo "toggles-complete"');
    await waitForSentinel(sessionId, sentinel);

    await assertTerminalMatchesTmux(page, tmuxName);
  });

  test('overwrite after scrollback', async ({ page }) => {
    test.setTimeout(60_000);

    await openTerminal(page, sessionId, tmuxName);

    // Output 300 lines to build scrollback
    sendTmuxCommand(tmuxName, 'for i in $(seq 1 300); do echo "scrollback-$i"; done');

    // Overwrite last line 20 times with carriage return
    const sentinel = sendTmuxCommandWithSentinel(
      tmuxName,
      "for i in $(seq 1 20); do printf '\\rOverwrite pass %02d' $i; sleep 0.05; done; echo"
    );
    await waitForSentinel(sessionId, sentinel);

    // Check visible screen
    await assertTerminalMatchesTmux(page, tmuxName);

    // Check scrollback
    await assertTerminalMatchesTmux(page, tmuxName, { scrollbackLines: 300 });
  });

  test('reconnect mid-stream', async ({ page }) => {
    test.setTimeout(90_000);

    // Navigate and clear to ensure tmux is at correct width
    await openTerminal(page, sessionId, tmuxName);

    // Start a background flood
    sendTmuxCommand(tmuxName, 'for i in $(seq 1 200); do echo "flood-$i"; sleep 0.02; done &');

    // Wait until flood output appears in terminal (state check, not fixed delay)
    await waitForTerminalOutput(sessionId, 'flood-', 10_000);
    await page.reload();
    await waitForDashboardLive(page);
    await page.waitForSelector('[data-testid="terminal-viewport"]', { timeout: 15_000 });

    // Wait for the flood to finish (wait for background job, then output sentinel)
    const floodSentinel = sendTmuxCommandWithSentinel(tmuxName, 'wait; echo "flood-done"');
    await waitForSentinel(sessionId, floodSentinel);

    // Clear screen to establish known state after the chaotic reconnection,
    // then verify the terminal pipeline is still functional.
    sendTmuxCommand(tmuxName, 'clear');
    clearTmuxHistory(tmuxName);

    const sentinel = sendTmuxCommandWithSentinel(
      tmuxName,
      'echo "reconnect-verified: terminal works after mid-stream reload"'
    );
    await waitForSentinel(sessionId, sentinel);

    await assertTerminalMatchesTmux(page, tmuxName);
  });

  test('rapid TUI repaint: 20 full-screen redraws', async ({ page }) => {
    await openTerminal(page, sessionId, tmuxName);

    // Simulate a TUI that redraws its entire visible area 20 times rapidly.
    // Each frame paints 10 rows with frame-specific content, using a script
    // to avoid hitting tmux command rate limits.
    const script = `for i in $(seq 1 20); do printf '\\033[H'; for r in $(seq 1 10); do printf '\\033[%d;1H\\033[2KFrame %02d Row %02d: data-%d-%d' "$r" "$i" "$r" "$i" "$r"; done; done`;
    const sentinel = sendTmuxCommandWithSentinel(tmuxName, script);
    await waitForSentinel(sessionId, sentinel);

    // After all redraws, the visible content should match the final frame (frame 20)
    await assertTerminalMatchesTmux(page, tmuxName);
    await assertCursorMatchesTmux(page, tmuxName);
  });

  test('cursor position survives background output flood', async ({ page }) => {
    await openTerminal(page, sessionId, tmuxName);

    // Start a background flood that generates rapid output
    sendTmuxCommand(tmuxName, 'for i in $(seq 1 500); do echo "flood-line-$i"; done &');

    // Wait for flood to start producing output, then send cursor commands
    // (simulating a TUI updating its status bar during heavy output)
    await waitForTerminalOutput(sessionId, 'flood-line-', 10_000);
    const sequence = [
      '\\033[1;1H\\033[2KStatus: processing...',
      '\\033[1;22H', // cursor after "processing..."
    ].join('');
    sendTmuxCommand(tmuxName, `printf '${sequence}'`);

    // Wait for the flood to finish
    const sentinel = sendTmuxCommandWithSentinel(tmuxName, 'wait; echo flood-done');
    await waitForSentinel(sessionId, sentinel);

    // After everything settles, content and cursor must match tmux
    await assertTerminalMatchesTmux(page, tmuxName);
    await assertCursorMatchesTmux(page, tmuxName);
  });
});
