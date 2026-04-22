import { test, expect } from './coverage-fixture';
import {
  seedConfig,
  createTestRepo,
  spawnSession,
  waitForHealthy,
  waitForSessionRunning,
  waitForDashboardLive,
} from './helpers';
import { getTmuxSessionName } from './helpers-terminal';
import { execSync } from 'child_process';
import { writeFileSync, mkdirSync } from 'fs';
import WS from 'ws';

function getBaseURL(): string {
  return process.env.SCHMUX_BASE_URL || 'http://localhost:7337';
}

function getTmuxSocket(): string {
  return process.env.SCHMUX_TMUX_SOCKET || 'schmux';
}

/**
 * Open a `/ws/terminal/{id}` connection and resolve once it's open. The
 * caller drives the pane via sendWSInput() and is responsible for closing.
 *
 * We keep this distinct from waitForTerminalOutput() (helpers.ts) because
 * suppression tests need a long-lived input-side WebSocket, not a one-shot
 * "wait for substring" reader.
 */
async function openTerminalWS(sessionId: string): Promise<WS> {
  const url = `${getBaseURL().replace(/^http/, 'ws')}/ws/terminal/${sessionId}`;
  const ws = new WS(url);
  await new Promise<void>((resolve, reject) => {
    if (ws.readyState === WS.OPEN) {
      resolve();
      return;
    }
    const timer = setTimeout(() => reject(new Error(`WS open timeout: ${url}`)), 10_000);
    ws.once('open', () => {
      clearTimeout(timer);
      resolve();
    });
    ws.once('error', (err: Error) => {
      clearTimeout(timer);
      reject(new Error(`WS error: ${err.message}`));
    });
  });
  return ws;
}

/**
 * Send a JSON `{type:"input", data:<text>}` frame on a `/ws/terminal/{id}`
 * connection. This is the production path the dashboard's xterm.js uses, so
 * the bytes flow through SessionRuntime.SendInput → echo.appendInput → tmux
 * send-keys. Critically, this records the bytes in the per-session echo
 * buffer that drives the input-echo suppression heuristic — the whole point
 * of the suppression-positive case below.
 *
 * tmux send-keys (used by helpers-terminal.sendTmuxCommand) bypasses this
 * pipeline entirely, so it's the wrong tool for any test that depends on
 * the echo buffer being populated.
 */
function sendWSInput(ws: WS, data: string): void {
  ws.send(JSON.stringify({ type: 'input', data }));
}

/**
 * Send raw OSC 52 bytes from inside a tmux pane by typing a `printf`
 * command at the shell prompt and pressing Enter. The shell evaluates the
 * `\033]...\007` octal escapes and writes the bytes to its stdout — at
 * which point tmux (with `set-clipboard external` + `terminal-features
 * '*:clipboard'`) forwards them through control mode to the daemon.
 */
function emitOSC52(tmuxSession: string, payload: string): void {
  const b64 = Buffer.from(payload, 'utf-8').toString('base64');
  const cmd = `printf '\\033]52;c;%s\\007' '${b64}'`;
  const socket = getTmuxSocket();
  // -l: literal mode (no key-name interpretation). Then send Enter.
  execSync(`tmux -L ${socket} send-keys -t '${tmuxSession}' -l ${shellQuote(cmd)}`);
  execSync(`tmux -L ${socket} send-keys -t '${tmuxSession}' Enter`);
}

/**
 * Drive `tmux set-buffer` directly against the daemon's tmux socket. This
 * exercises the load-buffer / set-buffer fallback path: tmux fires
 * `%paste-buffer-changed` to all connected control-mode clients without
 * the OSC 52 byte sequence ever appearing in the pane's stdout. The
 * daemon's per-source listener fetches the buffer with `show-buffer`,
 * defangs it via the shared helper, and pushes a SourcePasteBuffer event
 * through the same clipboardCh pipeline as OSC 52.
 *
 * No pane interaction here — the inner shell never sees these bytes.
 */
function setTmuxBuffer(bufferName: string, payload: string): void {
  const socket = getTmuxSocket();
  execSync(`tmux -L ${socket} set-buffer -b ${shellQuote(bufferName)} ${shellQuote(payload)}`);
}

function shellQuote(s: string): string {
  return (
    '"' +
    s.replace(/\\/g, '\\\\').replace(/"/g, '\\"').replace(/\$/g, '\\$').replace(/`/g, '\\`') +
    '"'
  );
}

test.describe('TUI clipboard write (OSC 52)', () => {
  let repoPath: string;
  let sessionId: string;
  let tmuxName: string;

  test.beforeAll(async () => {
    await waitForHealthy();
    repoPath = await createTestRepo('test-repo-tui-clipboard');
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
    expect(sessionId).toBeTruthy();
    await waitForSessionRunning(sessionId);
    tmuxName = await getTmuxSessionName(sessionId);
  });

  test('approve writes payload to navigator.clipboard', async ({ page, context }) => {
    test.setTimeout(60_000);

    // Grant clipboard read+write so navigator.clipboard.{readText,writeText}
    // resolve in the headless Chromium context.
    await context.grantPermissions(['clipboard-read', 'clipboard-write']);

    await page.goto(`/sessions/${sessionId}`);
    await waitForDashboardLive(page);
    await page.waitForSelector('[data-testid="terminal-viewport"]', { timeout: 15_000 });

    // Give the shell prompt a moment to settle (so our printf doesn't
    // arrive while bash is still initialising).
    await page.waitForTimeout(500);

    const payload = 'hello-clipboard-test';
    emitOSC52(tmuxName, payload);

    // Banner appears with the payload.
    const banner = page.getByRole('alert').filter({ hasText: payload });
    await expect(banner).toBeVisible({ timeout: 10_000 });

    // Click Approve.
    await banner.getByRole('button', { name: 'Approve' }).click();

    // Banner disappears (cleared by clipboardCleared broadcast).
    await expect(banner).toBeHidden({ timeout: 10_000 });

    // Browser clipboard now contains the payload.
    const clipboard = await page.evaluate(() => navigator.clipboard.readText());
    expect(clipboard).toBe(payload);
  });

  test('reject does not modify navigator.clipboard', async ({ page, context }) => {
    test.setTimeout(60_000);

    await context.grantPermissions(['clipboard-read', 'clipboard-write']);

    await page.goto(`/sessions/${sessionId}`);
    await waitForDashboardLive(page);
    await page.waitForSelector('[data-testid="terminal-viewport"]', { timeout: 15_000 });

    // Seed the clipboard with a sentinel so we can prove the reject path
    // didn't write to it.
    const sentinel = 'sentinel-clipboard-value';
    await page.evaluate((s) => navigator.clipboard.writeText(s), sentinel);
    expect(await page.evaluate(() => navigator.clipboard.readText())).toBe(sentinel);

    await page.waitForTimeout(500);

    const payload = 'rejected-clipboard-payload';
    emitOSC52(tmuxName, payload);

    const banner = page.getByRole('alert').filter({ hasText: payload });
    await expect(banner).toBeVisible({ timeout: 10_000 });

    await banner.getByRole('button', { name: 'Reject' }).click();

    await expect(banner).toBeHidden({ timeout: 10_000 });

    // Clipboard is unchanged — reject did NOT call writeText.
    const clipboard = await page.evaluate(() => navigator.clipboard.readText());
    expect(clipboard).toBe(sentinel);
  });

  test('tmux set-buffer (paste-buffer-changed fallback) surfaces banner', async ({
    page,
    context,
  }) => {
    test.setTimeout(60_000);

    await context.grantPermissions(['clipboard-read', 'clipboard-write']);

    await page.goto(`/sessions/${sessionId}`);
    await waitForDashboardLive(page);
    await page.waitForSelector('[data-testid="terminal-viewport"]', { timeout: 15_000 });

    await page.waitForTimeout(500);

    // Drive tmux directly — no pane interaction, no OSC 52 bytes. This
    // exercises the %paste-buffer-changed path that catches TUIs which
    // detect tmux control mode and bypass OSC 52.
    const payload = 'hello-from-load-buffer';
    setTmuxBuffer('clip-test-set-buffer', payload);

    // Banner appears with the payload — sourced via show-buffer + defang
    // + the shared clipboardCh pipeline.
    const banner = page.getByRole('alert').filter({ hasText: payload });
    await expect(banner).toBeVisible({ timeout: 10_000 });

    // Approve writes the payload to the browser clipboard.
    await banner.getByRole('button', { name: 'Approve' }).click();
    await expect(banner).toBeHidden({ timeout: 10_000 });

    const clipboard = await page.evaluate(() => navigator.clipboard.readText());
    expect(clipboard).toBe(payload);
  });

  // -------------------------------------------------------------------------
  // Input-echo suppression (commit deb2812ab).
  //
  // Premise: when a process inside the pane emits OSC 52 with content that
  // matches bytes recently sent into the pane via the dashboard WebSocket,
  // the daemon must suppress the clipboard banner. The motivating case is
  // Claude Code's argv-prompt round-trip: the user types
  // `claude "MARKER"` in the dashboard, schmux send-keys those bytes into
  // the pane, Claude reads its own argv and emits OSC 52 with the same
  // payload. That OSC 52 is internal plumbing, not a real user-intent copy,
  // and the banner would be noise.
  //
  // Suppression rules: 5 s window, 8-byte minimum, substring match against
  // any recorded SendInput chunk. See internal/session/inputecho.go.
  // -------------------------------------------------------------------------

  test('input-echo suppression: banner does NOT fire when OSC 52 echoes typed input', async ({
    page,
    context,
  }) => {
    test.setTimeout(60_000);

    await context.grantPermissions(['clipboard-read', 'clipboard-write']);

    await page.goto(`/sessions/${sessionId}`);
    await waitForDashboardLive(page);
    await page.waitForSelector('[data-testid="terminal-viewport"]', { timeout: 15_000 });

    // Wait past the prior test's 5 s suppression window so the echo buffer
    // is empty before we start. Otherwise a marker from an earlier test
    // could leak into this test's substring match. The existing test suite
    // doesn't use WS input, so this is belt-and-suspenders, but cheap.
    await page.waitForTimeout(500);

    // Use a long, unique marker (>= 16 chars) — well above the 8-byte
    // minLen guard in matchesRecent(). The same string appears (a) in the
    // WS-sent printf command line bytes, and (b) as the OSC 52 payload
    // after the shell evaluates the printf. Substring match → suppress.
    const marker = `MARKER-suppression-positive-${Date.now()}`;
    expect(marker.length).toBeGreaterThanOrEqual(16);

    const ws = await openTerminalWS(sessionId);
    try {
      // Type a printf line that emits OSC 52 with `marker` as the payload.
      // The literal `marker` string is in the WS-sent bytes (inside the
      // base64 invocation's argument) AND will be the OSC 52 payload after
      // the shell base64-encodes-and-prints. Both halves of the suppression
      // condition are satisfied by a single typed line.
      const printfCmd = `printf '\\033]52;c;%s\\007' "$(printf '${marker}' | base64)"\n`;
      sendWSInput(ws, printfCmd);

      // Wait long enough for the round-trip to complete so we'd SEE a
      // banner if one was going to appear: WS → SendInput → tmux → shell
      // printf → control-mode output → OSC 52 extractor → suppression
      // check → (would-be debounce 200 ms) → would-be dashboard broadcast.
      // 1.5 s clears the 200 ms debounce and the typical end-to-end is
      // < 200 ms in CI. If a banner is going to appear it will appear in
      // this window — we explicitly sleep rather than poll-for-zero so an
      // early would-be banner can't transiently meet a `count==0` check
      // before the broadcast lands.
      await page.waitForTimeout(1500);

      // No banner with our marker (suppression positive).
      const banner = page.getByRole('alert').filter({ hasText: marker });
      await expect(banner).toHaveCount(0);

      // Belt-and-suspenders: NO banner at all. If suppression silently
      // broke and the daemon broadcast a request with mangled text (e.g.
      // base64-encoded form leaked), the marker filter above would miss
      // it but this catches it.
      const anyBanner = page.getByRole('alert');
      await expect(anyBanner).toHaveCount(0);
    } finally {
      ws.close();
    }
  });

  test('input-echo suppression: banner DOES fire when OSC 52 payload is not typed input', async ({
    page,
    context,
  }) => {
    test.setTimeout(60_000);

    await context.grantPermissions(['clipboard-read', 'clipboard-write']);

    await page.goto(`/sessions/${sessionId}`);
    await waitForDashboardLive(page);
    await page.waitForSelector('[data-testid="terminal-viewport"]', { timeout: 15_000 });

    await page.waitForTimeout(500);

    // Negative control: prove suppression is targeted, not a blanket
    // "swallow every OSC 52 from inside the pane". The OSC 52 payload
    // string must NEVER appear in any WS-typed input bytes — otherwise the
    // substring match would fire and we'd be testing the positive case
    // again by accident.
    //
    // Strategy: stage a script on disk via Node's fs (NOT via the pane —
    // tmux send-keys would write the script content to the pane's input
    // stream, but it goes through tmux directly, not through the daemon's
    // SendInput/echo buffer; even so, we keep it filesystem-only to remove
    // any temptation for the daemon to ever see the payload as input).
    // Then via WS, type only `bash <path>`. The recorded echo bytes are
    // `bash /tmp/...sh\n` — the OSC 52 payload string is read from disk
    // and emitted by the script's printf, never typed.
    const payload = `NOT-TYPED-payload-${Date.now()}`;
    expect(payload.length).toBeGreaterThanOrEqual(8);

    const scriptDir = '/tmp/schmux-clip-scripts';
    mkdirSync(scriptDir, { recursive: true });
    const scriptPath = `${scriptDir}/emit-${Date.now()}.sh`;
    // The script base64-encodes `payload` and emits OSC 52. Using base64
    // here keeps the literal payload bytes out of the script invocation
    // and out of any incidental shell history echoing.
    const b64 = Buffer.from(payload, 'utf-8').toString('base64');
    const scriptBody = `#!/bin/sh\nprintf '\\033]52;c;%s\\007' '${b64}'\n`;
    writeFileSync(scriptPath, scriptBody, { mode: 0o755 });

    const ws = await openTerminalWS(sessionId);
    try {
      // Type only the bash invocation. The recorded WS-input bytes are
      // `bash <path>\n` — the payload string itself is on disk, never in
      // the echo buffer.
      sendWSInput(ws, `bash ${scriptPath}\n`);

      // Banner SHOULD appear with the payload. If it doesn't, suppression
      // is over-firing (matching on `bash` or the script path), which would
      // be a regression.
      const banner = page.getByRole('alert').filter({ hasText: payload });
      await expect(banner).toBeVisible({ timeout: 10_000 });

      // Clean up the banner so the next describe-block tests don't see it.
      await banner.getByRole('button', { name: 'Reject' }).click();
      await expect(banner).toBeHidden({ timeout: 10_000 });
    } finally {
      ws.close();
    }
  });

  // Note: a third case for the time-window expiry (content older than 5 s
  // is NOT suppressed) is covered by the unit test
  // TestFanOut_DoesNotSuppressWhenInputIsTooOld in
  // internal/session/tracker_test.go. We don't duplicate it at the
  // scenario level because the only honest way to exercise the real
  // 5 s timeout is to sleep 6 s, which adds CI cost without adding
  // coverage the unit test doesn't already provide.

  // -------------------------------------------------------------------------
  // Spawn-prompt suppression (workspace-scoped exact-match against
  // req.Prompt — registered by handleSpawnPost into clipboardState).
  //
  // Covered by unit tests in internal/dashboard/clipboard_state_test.go:
  //   - TestClipboardState_RegisterSpawnPrompt_SuppressesMatching
  //   - TestClipboardState_RegisterSpawnPrompt_SuppressesAcrossSessions
  //   - TestClipboardState_RegisterSpawnPrompt_ExpiresAfterTTL
  //   - TestClipboardState_RegisterSpawnPrompt_DoesNotSuppressNonMatching
  //   - TestClipboardState_RegisterSpawnPrompt_EmptyIsNoop
  //   - TestClipboardState_RegisterSpawnPrompt_IdempotentRefreshesTTL
  //
  // Not duplicated here: the existing scenario fixture uses `shell-agent`
  // (a non-promptable run target), and the spawn API explicitly rejects a
  // prompt on such targets ("prompt is not allowed for command targets",
  // handlers_spawn.go). Exercising the real spawn flow would require
  // either standing up a promptable model (real agent binary on the test
  // host) or adding a test-only backdoor endpoint to inject the prompt —
  // neither adds coverage the unit tests don't already provide.
});
