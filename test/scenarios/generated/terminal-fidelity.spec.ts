import { test } from '@playwright/test';
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
} from './helpers-terminal';

// ---------------------------------------------------------------------------
// Tier 1: Encoding
// ---------------------------------------------------------------------------

test.describe.serial('Terminal fidelity: encoding', () => {
  let repoPath: string;
  let sessionId: string;

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
      prompt: 'fidelity-encoding',
      targets: { 'shell-agent': 1 },
    });
    sessionId = results[0].session_id;
    await waitForSessionRunning(sessionId);
  });

  test('ascii lines', async ({ page }) => {
    test.setTimeout(30_000);

    const sentinel = sendTmuxCommandWithSentinel(
      sessionId,
      'for i in $(seq 1 10); do echo "Line $i: the quick brown fox"; done'
    );
    await waitForSentinel(sessionId, sentinel);

    await page.goto(`/sessions/${sessionId}`);
    await waitForDashboardLive(page);
    await page.waitForSelector('[data-testid="terminal-viewport"]', { timeout: 15_000 });
    await new Promise((r) => setTimeout(r, 1000));

    await assertTerminalMatchesTmux(page, sessionId);
  });

  test('utf8 box drawing', async ({ page }) => {
    test.setTimeout(30_000);

    const sentinel = sendTmuxCommandWithSentinel(
      sessionId,
      "printf '\\342\\224\\214\\342\\224\\200\\342\\224\\200\\342\\224\\200\\342\\224\\200\\342\\224\\200\\342\\224\\200\\342\\224\\200\\342\\224\\200\\342\\224\\200\\342\\224\\200\\342\\224\\220\\n\\342\\224\\202 schmux   \\342\\224\\202\\n\\342\\224\\202 terminal \\342\\224\\202\\n\\342\\224\\224\\342\\224\\200\\342\\224\\200\\342\\224\\200\\342\\224\\200\\342\\224\\200\\342\\224\\200\\342\\224\\200\\342\\224\\200\\342\\224\\200\\342\\224\\200\\342\\224\\230\\n'"
    );
    await waitForSentinel(sessionId, sentinel);

    await page.goto(`/sessions/${sessionId}`);
    await waitForDashboardLive(page);
    await page.waitForSelector('[data-testid="terminal-viewport"]', { timeout: 15_000 });
    await new Promise((r) => setTimeout(r, 1000));

    await assertTerminalMatchesTmux(page, sessionId);
  });

  test('utf8 mixed characters', async ({ page }) => {
    test.setTimeout(30_000);

    const sentinel = sendTmuxCommandWithSentinel(
      sessionId,
      "printf '\\xe2\\x9c\\x93 Pass: \\xe6\\x97\\xa5\\xe6\\x9c\\xac\\xe8\\xaa\\x9e\\xe3\\x83\\x86\\xe3\\x82\\xb9\\xe3\\x83\\x88\\n\\xe2\\x9c\\x97 Fail: \\xe2\\x94\\x80\\xe2\\x94\\x80\\xe2\\x94\\x80\\xe2\\x94\\x80\\xe2\\x94\\x80\\xe2\\x94\\x80\\n\\xe2\\x9a\\xa1 Done\\n'"
    );
    await waitForSentinel(sessionId, sentinel);

    await page.goto(`/sessions/${sessionId}`);
    await waitForDashboardLive(page);
    await page.waitForSelector('[data-testid="terminal-viewport"]', { timeout: 15_000 });
    await new Promise((r) => setTimeout(r, 1000));

    await assertTerminalMatchesTmux(page, sessionId);
  });

  test('ansi colors preserve text', async ({ page }) => {
    test.setTimeout(30_000);

    const sentinel = sendTmuxCommandWithSentinel(
      sessionId,
      "printf '\\033[31mred \\033[32mgreen \\033[34mblue \\033[0mnormal\\n'"
    );
    await waitForSentinel(sessionId, sentinel);

    await page.goto(`/sessions/${sessionId}`);
    await waitForDashboardLive(page);
    await page.waitForSelector('[data-testid="terminal-viewport"]', { timeout: 15_000 });
    await new Promise((r) => setTimeout(r, 1000));

    await assertTerminalMatchesTmux(page, sessionId);
  });

  test('long line wrapping', async ({ page }) => {
    test.setTimeout(30_000);

    const sentinel = sendTmuxCommandWithSentinel(sessionId, 'python3 -c "print(\'A\' * 200)"');
    await waitForSentinel(sessionId, sentinel);

    await page.goto(`/sessions/${sessionId}`);
    await waitForDashboardLive(page);
    await page.waitForSelector('[data-testid="terminal-viewport"]', { timeout: 15_000 });
    await new Promise((r) => setTimeout(r, 1000));

    await assertTerminalMatchesTmux(page, sessionId);
  });
});

// ---------------------------------------------------------------------------
// Tier 2: Cursor movement
// ---------------------------------------------------------------------------

test.describe.serial('Terminal fidelity: cursor movement', () => {
  let repoPath: string;
  let sessionId: string;

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
      prompt: 'fidelity-cursor',
      targets: { 'shell-agent': 1 },
    });
    sessionId = results[0].session_id;
    await waitForSessionRunning(sessionId);
  });

  test('carriage return overwrites', async ({ page }) => {
    test.setTimeout(30_000);

    const sentinel = sendTmuxCommandWithSentinel(
      sessionId,
      "for i in 20 40 60 80 100; do printf '\\rProgress: %d%%' $i; sleep 0.1; done; echo"
    );
    await waitForSentinel(sessionId, sentinel);

    await page.goto(`/sessions/${sessionId}`);
    await waitForDashboardLive(page);
    await page.waitForSelector('[data-testid="terminal-viewport"]', { timeout: 15_000 });
    await new Promise((r) => setTimeout(r, 1000));

    await assertTerminalMatchesTmux(page, sessionId);
  });

  test('cursor positioning CSI H', async ({ page }) => {
    test.setTimeout(30_000);

    const sentinel = sendTmuxCommandWithSentinel(
      sessionId,
      "printf '\\033[2J\\033[3;5HRow3Col5\\033[7;20HRow7Col20\\033[10;1H'"
    );
    await waitForSentinel(sessionId, sentinel);

    await page.goto(`/sessions/${sessionId}`);
    await waitForDashboardLive(page);
    await page.waitForSelector('[data-testid="terminal-viewport"]', { timeout: 15_000 });
    await new Promise((r) => setTimeout(r, 1000));

    await assertTerminalMatchesTmux(page, sessionId);
  });

  test('erase in line', async ({ page }) => {
    test.setTimeout(30_000);

    const sentinel = sendTmuxCommandWithSentinel(sessionId, "printf 'AAABBBCCC\\033[6D\\033[K\\n'");
    await waitForSentinel(sessionId, sentinel);

    await page.goto(`/sessions/${sessionId}`);
    await waitForDashboardLive(page);
    await page.waitForSelector('[data-testid="terminal-viewport"]', { timeout: 15_000 });
    await new Promise((r) => setTimeout(r, 1000));

    await assertTerminalMatchesTmux(page, sessionId);
  });

  test('erase in display', async ({ page }) => {
    test.setTimeout(30_000);

    const sentinel = sendTmuxCommandWithSentinel(
      sessionId,
      'for i in $(seq 1 5); do echo "fill line $i"; done; printf \'\\033[2J\\033[HCleared\\n\''
    );
    await waitForSentinel(sessionId, sentinel);

    await page.goto(`/sessions/${sessionId}`);
    await waitForDashboardLive(page);
    await page.waitForSelector('[data-testid="terminal-viewport"]', { timeout: 15_000 });
    await new Promise((r) => setTimeout(r, 1000));

    await assertTerminalMatchesTmux(page, sessionId);
  });

  test('scroll region', async ({ page }) => {
    test.setTimeout(30_000);

    // Set scroll region to rows 3-8, output lines within it, then reset
    const sentinel = sendTmuxCommandWithSentinel(
      sessionId,
      "printf '\\033[3;8r'; for i in $(seq 1 10); do echo \"scroll-$i\"; done; printf '\\033[r'"
    );
    await waitForSentinel(sessionId, sentinel);

    await page.goto(`/sessions/${sessionId}`);
    await waitForDashboardLive(page);
    await page.waitForSelector('[data-testid="terminal-viewport"]', { timeout: 15_000 });
    await new Promise((r) => setTimeout(r, 1000));

    await assertTerminalMatchesTmux(page, sessionId);
  });
});

// ---------------------------------------------------------------------------
// Tier 3: Alternate screen
// ---------------------------------------------------------------------------

test.describe.serial('Terminal fidelity: alternate screen', () => {
  let repoPath: string;
  let sessionId: string;

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
      prompt: 'fidelity-altscreen',
      targets: { 'shell-agent': 1 },
    });
    sessionId = results[0].session_id;
    await waitForSessionRunning(sessionId);
  });

  test('alternate screen roundtrip', async ({ page }) => {
    test.setTimeout(30_000);

    // Write content to the normal screen
    sendTmuxCommand(sessionId, 'echo "normal-screen-content"');
    // Enter alt screen, draw something, exit alt screen
    const sentinel = sendTmuxCommandWithSentinel(
      sessionId,
      "printf '\\033[?1049h\\033[2J\\033[1;1HAlt screen content\\033[?1049l'"
    );
    await waitForSentinel(sessionId, sentinel);

    await page.goto(`/sessions/${sessionId}`);
    await waitForDashboardLive(page);
    await page.waitForSelector('[data-testid="terminal-viewport"]', { timeout: 15_000 });
    await new Promise((r) => setTimeout(r, 1000));

    // After exiting alt screen, normal screen content should be restored
    await assertTerminalMatchesTmux(page, sessionId);
  });

  test('ncurses-style bordered panel', async ({ page }) => {
    test.setTimeout(30_000);

    // Enter alt screen and draw a bordered box with content
    const sentinel = sendTmuxCommandWithSentinel(
      sessionId,
      "printf '\\033[?1049h\\033[2J" +
        '\\033[1;1H\\xe2\\x94\\x8c\\xe2\\x94\\x80\\xe2\\x94\\x80\\xe2\\x94\\x80\\xe2\\x94\\x80\\xe2\\x94\\x80\\xe2\\x94\\x80\\xe2\\x94\\x80\\xe2\\x94\\x80\\xe2\\x94\\x80\\xe2\\x94\\x80\\xe2\\x94\\x80\\xe2\\x94\\x80\\xe2\\x94\\x90' +
        '\\033[2;1H\\xe2\\x94\\x82 Dashboard  \\xe2\\x94\\x82' +
        '\\033[3;1H\\xe2\\x94\\x82  Status   \\xe2\\x94\\x82' +
        '\\033[4;1H\\xe2\\x94\\x94\\xe2\\x94\\x80\\xe2\\x94\\x80\\xe2\\x94\\x80\\xe2\\x94\\x80\\xe2\\x94\\x80\\xe2\\x94\\x80\\xe2\\x94\\x80\\xe2\\x94\\x80\\xe2\\x94\\x80\\xe2\\x94\\x80\\xe2\\x94\\x80\\xe2\\x94\\x80\\xe2\\x94\\x98' +
        "\\033[5;1H'"
    );
    await waitForSentinel(sessionId, sentinel);

    await page.goto(`/sessions/${sessionId}`);
    await waitForDashboardLive(page);
    await page.waitForSelector('[data-testid="terminal-viewport"]', { timeout: 15_000 });
    await new Promise((r) => setTimeout(r, 1000));

    // Compare while still in alt screen
    await assertTerminalMatchesTmux(page, sessionId);

    // Clean up: exit alt screen
    sendTmuxCommand(sessionId, "printf '\\033[?1049l'");
  });
});

// ---------------------------------------------------------------------------
// Tier 4: Scrollback
// ---------------------------------------------------------------------------

test.describe.serial('Terminal fidelity: scrollback', () => {
  let repoPath: string;
  let sessionId: string;

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
      prompt: 'fidelity-scrollback',
      targets: { 'shell-agent': 1 },
    });
    sessionId = results[0].session_id;
    await waitForSessionRunning(sessionId);
  });

  test('scrollback after bulk output', async ({ page }) => {
    test.setTimeout(60_000);

    const sentinel = sendTmuxCommandWithSentinel(
      sessionId,
      'for i in $(seq 1 200); do echo "bulk-line-$i"; done'
    );
    await waitForSentinel(sessionId, sentinel);

    await page.goto(`/sessions/${sessionId}`);
    await waitForDashboardLive(page);
    await page.waitForSelector('[data-testid="terminal-viewport"]', { timeout: 15_000 });
    await new Promise((r) => setTimeout(r, 2000));

    // Compare visible screen
    await assertTerminalMatchesTmux(page, sessionId);

    // Compare scrollback (200 lines)
    await assertTerminalMatchesTmux(page, sessionId, { scrollbackLines: 200 });
  });

  test('scrollback preserved after alt screen exit', async ({ page }) => {
    test.setTimeout(60_000);

    // Output 30 lines to build scrollback
    sendTmuxCommand(sessionId, 'for i in $(seq 1 30); do echo "scrollback-line-$i"; done');
    // Enter and exit alt screen
    const sentinel = sendTmuxCommandWithSentinel(
      sessionId,
      "printf '\\033[?1049h\\033[2J\\033[1;1HAlt content\\033[?1049l'"
    );
    await waitForSentinel(sessionId, sentinel);

    await page.goto(`/sessions/${sessionId}`);
    await waitForDashboardLive(page);
    await page.waitForSelector('[data-testid="terminal-viewport"]', { timeout: 15_000 });
    await new Promise((r) => setTimeout(r, 2000));

    // Scrollback from before alt screen should still be intact
    await assertTerminalMatchesTmux(page, sessionId, { scrollbackLines: 200 });
  });

  test('bootstrap matches scrollback after reconnect', async ({ page }) => {
    test.setTimeout(60_000);

    // Output some content
    const sentinel = sendTmuxCommandWithSentinel(
      sessionId,
      'for i in $(seq 1 20); do echo "reconnect-line-$i"; done'
    );
    await waitForSentinel(sessionId, sentinel);

    // First load
    await page.goto(`/sessions/${sessionId}`);
    await waitForDashboardLive(page);
    await page.waitForSelector('[data-testid="terminal-viewport"]', { timeout: 15_000 });
    await new Promise((r) => setTimeout(r, 1500));

    // Verify first load
    await assertTerminalMatchesTmux(page, sessionId);

    // Reload (new bootstrap)
    await page.reload();
    await waitForDashboardLive(page);
    await page.waitForSelector('[data-testid="terminal-viewport"]', { timeout: 15_000 });
    await new Promise((r) => setTimeout(r, 1500));

    // Verify after reload
    await assertTerminalMatchesTmux(page, sessionId);
  });
});

// ---------------------------------------------------------------------------
// Tier 5: Compounding
// ---------------------------------------------------------------------------

test.describe.serial('Terminal fidelity: compounding', () => {
  let repoPath: string;
  let sessionId: string;

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
      prompt: 'fidelity-compound',
      targets: { 'shell-agent': 1 },
    });
    sessionId = results[0].session_id;
    await waitForSessionRunning(sessionId);
  });

  test('build log then TUI then exit', async ({ page }) => {
    test.setTimeout(90_000);

    // Stage 1: 100-line colored build log
    const buildSentinel = sendTmuxCommandWithSentinel(
      sessionId,
      "for i in $(seq 1 100); do printf '\\033[32m[BUILD]\\033[0m Step %d: compiling module_%d\\n' $i $i; done"
    );
    await waitForSentinel(sessionId, buildSentinel);

    await page.goto(`/sessions/${sessionId}`);
    await waitForDashboardLive(page);
    await page.waitForSelector('[data-testid="terminal-viewport"]', { timeout: 15_000 });
    await new Promise((r) => setTimeout(r, 1500));

    // Verify build log stage
    await assertTerminalMatchesTmux(page, sessionId);

    // Stage 2: Enter alt screen with TUI
    const tuiSentinel = sendTmuxCommandWithSentinel(
      sessionId,
      "printf '\\033[?1049h\\033[2J\\033[1;1H=== TUI Dashboard ===\\033[3;1HStatus: Running\\033[5;1HProgress: 100%%'"
    );
    await waitForSentinel(sessionId, tuiSentinel);

    await page.goto(`/sessions/${sessionId}`);
    await waitForDashboardLive(page);
    await page.waitForSelector('[data-testid="terminal-viewport"]', { timeout: 15_000 });
    await new Promise((r) => setTimeout(r, 1500));

    // Verify TUI stage (alt screen)
    await assertTerminalMatchesTmux(page, sessionId);

    // Stage 3: Exit alt screen
    const exitSentinel = sendTmuxCommandWithSentinel(sessionId, "printf '\\033[?1049l'");
    await waitForSentinel(sessionId, exitSentinel);

    await page.goto(`/sessions/${sessionId}`);
    await waitForDashboardLive(page);
    await page.waitForSelector('[data-testid="terminal-viewport"]', { timeout: 15_000 });
    await new Promise((r) => setTimeout(r, 1500));

    // Should be back to normal screen with build log in scrollback
    await assertTerminalMatchesTmux(page, sessionId);
  });

  test('utf8 in output flood', async ({ page }) => {
    test.setTimeout(60_000);

    const sentinel = sendTmuxCommandWithSentinel(
      sessionId,
      "for i in $(seq 1 100); do printf '\\xe2\\x9c\\x93 Line %d: \\xe4\\xb8\\xad\\xe6\\x96\\x87\\xe6\\xb5\\x8b\\xe8\\xaf\\x95 test-%d\\n' $i $i; done"
    );
    await waitForSentinel(sessionId, sentinel);

    await page.goto(`/sessions/${sessionId}`);
    await waitForDashboardLive(page);
    await page.waitForSelector('[data-testid="terminal-viewport"]', { timeout: 15_000 });
    await new Promise((r) => setTimeout(r, 2000));

    await assertTerminalMatchesTmux(page, sessionId);
  });

  test('rapid alt screen toggles', async ({ page }) => {
    test.setTimeout(60_000);

    // Toggle alt screen 5 times with content between each
    for (let i = 1; i <= 5; i++) {
      sendTmuxCommand(
        sessionId,
        `printf '\\033[?1049h\\033[2J\\033[1;1HAlt screen pass ${i}\\033[?1049l'`
      );
      sendTmuxCommand(sessionId, `echo "Normal screen after toggle ${i}"`);
    }

    const sentinel = sendTmuxCommandWithSentinel(sessionId, 'echo "toggles-complete"');
    await waitForSentinel(sessionId, sentinel);

    await page.goto(`/sessions/${sessionId}`);
    await waitForDashboardLive(page);
    await page.waitForSelector('[data-testid="terminal-viewport"]', { timeout: 15_000 });
    await new Promise((r) => setTimeout(r, 1500));

    await assertTerminalMatchesTmux(page, sessionId);
  });

  test('overwrite after scrollback', async ({ page }) => {
    test.setTimeout(60_000);

    // Output 300 lines to build scrollback
    sendTmuxCommand(sessionId, 'for i in $(seq 1 300); do echo "scrollback-$i"; done');

    // Overwrite last line 20 times with carriage return
    const sentinel = sendTmuxCommandWithSentinel(
      sessionId,
      "for i in $(seq 1 20); do printf '\\rOverwrite pass %02d' $i; sleep 0.05; done; echo"
    );
    await waitForSentinel(sessionId, sentinel);

    await page.goto(`/sessions/${sessionId}`);
    await waitForDashboardLive(page);
    await page.waitForSelector('[data-testid="terminal-viewport"]', { timeout: 15_000 });
    await new Promise((r) => setTimeout(r, 2000));

    // Check visible screen
    await assertTerminalMatchesTmux(page, sessionId);

    // Check scrollback
    await assertTerminalMatchesTmux(page, sessionId, { scrollbackLines: 300 });
  });

  test('reconnect mid-stream', async ({ page }) => {
    test.setTimeout(90_000);

    // Start a background flood
    sendTmuxCommand(sessionId, 'for i in $(seq 1 200); do echo "flood-$i"; sleep 0.02; done &');

    // Navigate to the page mid-flood (don't wait for flood to finish)
    await new Promise((r) => setTimeout(r, 500));
    await page.goto(`/sessions/${sessionId}`);
    await waitForDashboardLive(page);
    await page.waitForSelector('[data-testid="terminal-viewport"]', { timeout: 15_000 });

    // Wait for the flood to finish
    const sentinel = sendTmuxCommandWithSentinel(sessionId, 'wait; echo "flood-done"');
    await waitForSentinel(sessionId, sentinel);
    await new Promise((r) => setTimeout(r, 2000));

    // Verify after flood completes
    await assertTerminalMatchesTmux(page, sessionId);

    // Reload (new bootstrap) and verify again
    await page.reload();
    await waitForDashboardLive(page);
    await page.waitForSelector('[data-testid="terminal-viewport"]', { timeout: 15_000 });
    await new Promise((r) => setTimeout(r, 2000));

    await assertTerminalMatchesTmux(page, sessionId);
  });
});
