import { type Page } from './coverage-fixture';
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
} from './helpers-terminal';

// ---------------------------------------------------------------------------
// All tests navigate to the session page BEFORE sending commands.
// This ensures xterm.js connects via WebSocket, triggering a resize of the
// tmux pane to match the browser viewport. A `clear` command is then sent to
// re-render the prompt via the live stream (not bootstrap), which preserves
// the prompt's trailing space and ensures cursor position parity.
// ---------------------------------------------------------------------------

/**
 * Navigate to the session page, wait for the terminal to be live,
 * and clear the screen to sync cursor state between tmux and xterm.js.
 */
async function openTerminal(page: Page, sessionId: string, tmuxName: string): Promise<void> {
  await page.goto(`/sessions/${sessionId}`);
  await waitForDashboardLive(page);
  await page.waitForSelector('[data-testid="terminal-viewport"]', { timeout: 15_000 });

  // Wait for the terminal WebSocket to connect and bootstrap by checking
  // if xterm.js has any content in its buffer. This avoids the race where
  // clear escape sequences are sent before the WebSocket connects.
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

  // Clear both scrollback and visible screen to sync state:
  // - \033[3J clears xterm.js scrollback (tmux ignores it for its own buffer)
  // - \033[H\033[2J moves cursor home and clears visible screen in both
  // After this, the shell re-displays its prompt via live stream, ensuring
  // cursor position parity between tmux and xterm.js.
  sendTmuxCommand(tmuxName, "printf '\\033[3J\\033[H\\033[2J'");
  await new Promise((r) => setTimeout(r, 500));
  // Clear tmux's scrollback history separately (tmux ignores \033[3J])
  clearTmuxHistory(tmuxName);
  await new Promise((r) => setTimeout(r, 200));
}

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

    await openTerminal(page, sessionId, tmuxName);

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
    await new Promise((r) => setTimeout(r, 200));
  });
});

// ---------------------------------------------------------------------------
// Tier 3: Alternate screen
// ---------------------------------------------------------------------------

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

    // Verify after reload (retrying assertion handles bootstrap rendering lag)
    await assertTerminalMatchesTmux(page, tmuxName);
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
    await waitForSessionRunning(sessionId);
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
    await waitForSentinel(sessionId, buildSentinel);

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
    await waitForSentinel(sessionId, sentinel);

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

    // Wait mid-flood, then reload to test reconnection during output
    await new Promise((r) => setTimeout(r, 1000));
    await page.reload();
    await waitForDashboardLive(page);
    await page.waitForSelector('[data-testid="terminal-viewport"]', { timeout: 15_000 });

    // Wait for the flood to finish (wait for background job, then output sentinel)
    const floodSentinel = sendTmuxCommandWithSentinel(tmuxName, 'wait; echo "flood-done"');
    await waitForSentinel(sessionId, floodSentinel);

    // Clear screen to establish known state after the chaotic reconnection,
    // then verify the terminal pipeline is still functional.
    sendTmuxCommand(tmuxName, 'clear');
    await new Promise((r) => setTimeout(r, 500));
    clearTmuxHistory(tmuxName);
    await new Promise((r) => setTimeout(r, 300));

    const sentinel = sendTmuxCommandWithSentinel(
      tmuxName,
      'echo "reconnect-verified: terminal works after mid-stream reload"'
    );
    await waitForSentinel(sessionId, sentinel);

    await assertTerminalMatchesTmux(page, tmuxName);
  });
});
