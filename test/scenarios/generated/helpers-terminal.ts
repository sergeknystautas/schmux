import { type Page } from '@playwright/test';
import { execSync } from 'child_process';
import { waitForDashboardLive, waitForTerminalOutput } from './helpers';

const BASE_URL = 'http://localhost:7337';
let sentinelCounter = 0;

/**
 * Resolve the tmux session name for a given API session ID.
 * The API session ID differs from the tmux session name when nicknames are used.
 * Parses the `attach_cmd` field from GET /api/sessions.
 */
export async function getTmuxSessionName(sessionId: string): Promise<string> {
  const res = await fetch(`${BASE_URL}/api/sessions`);
  if (!res.ok) {
    throw new Error(`Failed to get sessions: ${res.status}`);
  }
  const workspaces = (await res.json()) as Array<{
    sessions: Array<{ id: string; attach_cmd: string }>;
  }>;
  for (const ws of workspaces) {
    for (const sess of ws.sessions) {
      if (sess.id === sessionId) {
        const match = sess.attach_cmd.match(/tmux attach -t "=(.+)"/);
        if (!match) {
          throw new Error(`Could not parse tmux session name from attach_cmd: ${sess.attach_cmd}`);
        }
        return match[1];
      }
    }
  }
  throw new Error(`Session ${sessionId} not found in API response`);
}

/**
 * Send a command to a tmux session via `tmux send-keys`.
 * Uses -l for literal text (no escape interpretation), then sends Enter.
 * This bypasses the WebSocket input pipeline to isolate rendering tests.
 */
export function sendTmuxCommand(tmuxSession: string, command: string): void {
  execSync(`tmux send-keys -t '${tmuxSession}' -l ${shellQuote(command)}`);
  execSync(`tmux send-keys -t '${tmuxSession}' Enter`);
}

/**
 * Send a command followed by a sentinel echo. Returns the sentinel string.
 * Use with waitForSentinel() to synchronize before comparison.
 */
export function sendTmuxCommandWithSentinel(tmuxSession: string, command: string): string {
  const sentinel = `__FIDELITY_${++sentinelCounter}__`;
  sendTmuxCommand(tmuxSession, command);
  sendTmuxCommand(tmuxSession, `echo '${sentinel}'`);
  return sentinel;
}

/**
 * Capture the current tmux pane content as an array of strings (one per row).
 * This is the "ground truth" — what any terminal attached to the session would show.
 */
export function capturePane(tmuxSession: string, options?: { scrollbackLines?: number }): string[] {
  let cmd = `tmux capture-pane -p -t '${tmuxSession}'`;
  if (options?.scrollbackLines) {
    cmd = `tmux capture-pane -p -t '${tmuxSession}' -S -${options.scrollbackLines}`;
  }
  const output = execSync(cmd, { encoding: 'utf-8' });
  return output.split('\n');
}

/**
 * Read the xterm.js buffer content via Playwright page.evaluate().
 * Returns an array of strings (one per row), matching capturePane format.
 */
export async function readXtermBuffer(
  page: Page,
  options?: { scrollbackLines?: number }
): Promise<string[]> {
  return page.evaluate((opts) => {
    const terminal = (window as any).__schmuxTerminal;
    if (!terminal) {
      throw new Error('__schmuxTerminal not found on window');
    }
    const buffer = terminal.buffer.active;
    const lines: string[] = [];

    if (opts?.scrollbackLines) {
      // Match tmux's `-S -N` semantics: capture N lines of scrollback
      // plus all visible rows. tmux's `-S -N` starts N lines above
      // the top of the visible pane, so it captures N + rows lines total.
      const baseY = buffer.baseY; // scrollback line count
      const rows = terminal.rows; // visible rows
      const scrollStart = Math.max(0, baseY - opts.scrollbackLines);
      for (let i = scrollStart; i < baseY + rows; i++) {
        const line = buffer.getLine(i);
        lines.push(line ? line.translateToString(true) : '');
      }
    } else {
      const baseY = buffer.baseY;
      const rows = terminal.rows;
      for (let i = 0; i < rows; i++) {
        const line = buffer.getLine(baseY + i);
        lines.push(line ? line.translateToString(true) : '');
      }
    }
    return lines;
  }, options);
}

/**
 * Compare tmux and xterm.js content, returning mismatches (empty array = match).
 */
function compareTerminalContent(tmuxLines: string[], xtermLines: string[]): string[] {
  const trimTrailingEmpty = (lines: string[]) => {
    const result = [...lines];
    while (result.length > 0 && result[result.length - 1].trim() === '') {
      result.pop();
    }
    return result;
  };

  const expected = trimTrailingEmpty(tmuxLines);
  const actual = trimTrailingEmpty(xtermLines);

  const maxLines = Math.max(expected.length, actual.length);
  const mismatches: string[] = [];

  for (let i = 0; i < maxLines; i++) {
    const exp = (expected[i] || '').trimEnd();
    const act = (actual[i] || '').trimEnd();
    if (exp !== act) {
      mismatches.push(
        `  Row ${i}:\n` +
          `    tmux:  ${JSON.stringify(exp)}\n` +
          `    xterm: ${JSON.stringify(act)}`
      );
    }
  }

  return mismatches;
}

/**
 * Assert that the xterm.js terminal content matches tmux's capture-pane output.
 * Compares line-by-line with trimmed trailing whitespace.
 * Retries up to maxRetries times to handle xterm.js rendering lag.
 * Throws with a detailed diff on mismatch after all retries are exhausted.
 */
export async function assertTerminalMatchesTmux(
  page: Page,
  sessionId: string,
  options?: { scrollbackLines?: number }
): Promise<void> {
  const tmuxSession = sessionId;
  const maxRetries = 25;
  const retryDelayMs = 200;

  let lastMismatches: string[] = [];

  for (let attempt = 0; attempt <= maxRetries; attempt++) {
    if (attempt > 0) {
      await new Promise((r) => setTimeout(r, retryDelayMs));
    }

    const tmuxLines = capturePane(tmuxSession, options);
    const xtermLines = await readXtermBuffer(page, options);
    lastMismatches = compareTerminalContent(tmuxLines, xtermLines);

    if (lastMismatches.length === 0) return;
  }

  throw new Error(
    `Terminal fidelity mismatch (${lastMismatches.length} rows differ after ${maxRetries} retries):\n` +
      lastMismatches.join('\n')
  );
}

/**
 * Wait for a sentinel string to appear in the terminal via WebSocket,
 * then wait a short time for xterm.js to finish rendering.
 */
export async function waitForSentinel(
  sessionId: string,
  sentinel: string,
  timeoutMs = 15_000
): Promise<void> {
  await waitForTerminalOutput(sessionId, sentinel, timeoutMs);
}

/**
 * Clear the tmux scrollback history for a session.
 * Use after `clear` to sync scrollback between tmux and xterm.js,
 * since tmux ignores \033[3J (sent by `clear`) for its own buffer
 * while xterm.js honors it and clears scrollback.
 */
export function clearTmuxHistory(tmuxSession: string): void {
  execSync(`tmux clear-history -t '${tmuxSession}'`);
}

/**
 * Navigate to the session page, wait for the terminal to be live,
 * and clear the screen to sync cursor state between tmux and xterm.js.
 *
 * All tests navigate to the session page BEFORE sending commands.
 * This ensures xterm.js connects via WebSocket, triggering a resize of the
 * tmux pane to match the browser viewport. A `clear` command is then sent to
 * re-render the prompt via the live stream (not bootstrap), which preserves
 * the prompt's trailing space and ensures cursor position parity.
 */
export async function openTerminal(page: Page, sessionId: string, tmuxName: string): Promise<void> {
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

function shellQuote(s: string): string {
  return (
    '"' +
    s.replace(/\\/g, '\\\\').replace(/"/g, '\\"').replace(/\$/g, '\\$').replace(/`/g, '\\`') +
    '"'
  );
}

/**
 * Get the tmux cursor position for a session.
 * Returns { x, y } matching tmux's #{cursor_x} #{cursor_y} (0-indexed).
 */
export function getTmuxCursorPosition(tmuxSession: string): { x: number; y: number } {
  const output = execSync(`tmux display-message -p -t '${tmuxSession}' '#{cursor_x} #{cursor_y}'`, {
    encoding: 'utf-8',
  }).trim();
  const [x, y] = output.split(' ').map(Number);
  return { x, y };
}

/**
 * Get the xterm.js cursor position via Playwright page.evaluate().
 * Returns { x, y } matching the active buffer's cursorX/cursorY (0-indexed).
 */
export async function getXtermCursorPosition(page: Page): Promise<{ x: number; y: number }> {
  return page.evaluate(() => {
    const terminal = (window as any).__schmuxTerminal;
    if (!terminal) {
      throw new Error('__schmuxTerminal not found on window');
    }
    const buffer = terminal.buffer.active;
    return { x: buffer.cursorX, y: buffer.cursorY };
  });
}

/**
 * Assert that the xterm.js cursor position matches tmux's cursor position.
 * Both use 0-indexed coordinates.
 * Retries to handle rendering lag.
 */
export async function assertCursorMatchesTmux(page: Page, tmuxSession: string): Promise<void> {
  const maxRetries = 25;
  const retryDelayMs = 200;

  let lastTmux = { x: 0, y: 0 };
  let lastXterm = { x: 0, y: 0 };

  for (let attempt = 0; attempt <= maxRetries; attempt++) {
    if (attempt > 0) {
      await new Promise((r) => setTimeout(r, retryDelayMs));
    }

    lastTmux = getTmuxCursorPosition(tmuxSession);
    lastXterm = await getXtermCursorPosition(page);

    if (lastTmux.x === lastXterm.x && lastTmux.y === lastXterm.y) return;
  }

  throw new Error(
    `Cursor position mismatch (after ${maxRetries} retries):\n` +
      `  tmux:  (${lastTmux.x}, ${lastTmux.y})\n` +
      `  xterm: (${lastXterm.x}, ${lastXterm.y})`
  );
}

/**
 * Get the tmux cursor visibility for a session.
 * Returns true if cursor is visible (cursor_flag=1), false if hidden (cursor_flag=0).
 */
export function getTmuxCursorVisible(tmuxSession: string): boolean {
  const output = execSync(`tmux display-message -p -t '${tmuxSession}' '#{cursor_flag}'`, {
    encoding: 'utf-8',
  }).trim();
  return output === '1';
}

/**
 * Get the xterm.js cursor visibility via Playwright page.evaluate().
 * Accesses _core.coreService.isCursorHidden (internal API, matches codebase pattern).
 * Returns true if cursor is visible, false if hidden.
 */
export async function getXtermCursorVisible(page: Page): Promise<boolean> {
  return page.evaluate(() => {
    const terminal = (window as any).__schmuxTerminal;
    if (!terminal) {
      throw new Error('__schmuxTerminal not found on window');
    }
    const core = (terminal as any)._core;
    if (!core?.coreService) {
      throw new Error('xterm.js _core.coreService not available');
    }
    return !core.coreService.isCursorHidden;
  });
}

/**
 * Assert that the xterm.js cursor visibility matches tmux's cursor_flag.
 * Retries to handle rendering lag.
 */
export async function assertCursorVisibilityMatchesTmux(
  page: Page,
  tmuxSession: string
): Promise<void> {
  const maxRetries = 25;
  const retryDelayMs = 200;

  let lastTmuxVisible = true;
  let lastXtermVisible = true;

  for (let attempt = 0; attempt <= maxRetries; attempt++) {
    if (attempt > 0) {
      await new Promise((r) => setTimeout(r, retryDelayMs));
    }

    lastTmuxVisible = getTmuxCursorVisible(tmuxSession);
    lastXtermVisible = await getXtermCursorVisible(page);

    if (lastTmuxVisible === lastXtermVisible) return;
  }

  throw new Error(
    `Cursor visibility mismatch (after ${maxRetries} retries):\n` +
      `  tmux:  ${lastTmuxVisible ? 'visible' : 'hidden'}\n` +
      `  xterm: ${lastXtermVisible ? 'visible' : 'hidden'}`
  );
}
