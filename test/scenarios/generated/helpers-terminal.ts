import { type Page } from '@playwright/test';
import { execSync } from 'child_process';
import { mkdirSync, writeFileSync } from 'fs';
import { waitForDashboardLive } from './helpers';

// Read at call time (not module load) so fixture-set env vars are picked up.
function getBaseURL(): string {
  return process.env.SCHMUX_BASE_URL || 'http://localhost:7337';
}
function getTmuxSocket(): string {
  return process.env.SCHMUX_TMUX_SOCKET || 'schmux';
}
let sentinelCounter = 0;

/**
 * Resolve the tmux session name for a given API session ID.
 * The API session ID differs from the tmux session name when nicknames are used.
 * Parses the `attach_cmd` field from GET /api/sessions.
 */
export async function getTmuxSessionName(sessionId: string): Promise<string> {
  const res = await fetch(`${getBaseURL()}/api/sessions`);
  if (!res.ok) {
    throw new Error(`Failed to get sessions: ${res.status}`);
  }
  const workspaces = (await res.json()) as Array<{
    sessions: Array<{ id: string; attach_cmd: string }>;
  }>;
  for (const ws of workspaces) {
    for (const sess of ws.sessions) {
      if (sess.id === sessionId) {
        const match = sess.attach_cmd.match(/tmux(?: -L [\w-]+)? attach -t "=(.+)"/);
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
  execSync(`tmux -L ${getTmuxSocket()} send-keys -t '${tmuxSession}' -l ${shellQuote(command)}`);
  execSync(`tmux -L ${getTmuxSocket()} send-keys -t '${tmuxSession}' Enter`);
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
  let cmd = `tmux -L ${getTmuxSocket()} capture-pane -p -t '${tmuxSession}'`;
  if (options?.scrollbackLines) {
    cmd = `tmux -L ${getTmuxSocket()} capture-pane -p -t '${tmuxSession}' -S -${options.scrollbackLines}`;
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
 * On failure, writes a detailed diagnostic file to /tmp/terminal-diagnostics/
 * with convergence data, full captures, and stream state.
 */
export async function assertTerminalMatchesTmux(
  page: Page,
  sessionId: string,
  options?: { scrollbackLines?: number; maxRetries?: number }
): Promise<void> {
  const tmuxSession = sessionId;
  const maxRetries = options?.maxRetries ?? 50;
  const retryDelayMs = 200;

  let lastMismatches: string[] = [];
  let firstMismatches: string[] | null = null;
  let firstTmuxLines: string[] = [];
  let firstXtermLines: string[] = [];
  let lastTmuxLines: string[] = [];
  let lastXtermLines: string[] = [];
  const convergenceLog: number[] = [];
  const startTime = Date.now();

  for (let attempt = 0; attempt <= maxRetries; attempt++) {
    if (attempt > 0) {
      await new Promise((r) => setTimeout(r, retryDelayMs));
    }

    const tmuxLines = capturePane(tmuxSession, options);
    const xtermLines = await readXtermBuffer(page, options);
    lastMismatches = compareTerminalContent(tmuxLines, xtermLines);
    lastTmuxLines = tmuxLines;
    lastXtermLines = xtermLines;
    convergenceLog.push(lastMismatches.length);

    if (firstMismatches === null && lastMismatches.length > 0) {
      firstMismatches = [...lastMismatches];
      firstTmuxLines = [...tmuxLines];
      firstXtermLines = [...xtermLines];
    }

    if (lastMismatches.length === 0) return;
  }

  // Collect diagnostic data before throwing
  const elapsedMs = Date.now() - startTime;

  // Get tmux pane dimensions for comparison with xterm.js
  let tmuxPaneDims = { height: -1, width: -1 };
  try {
    const dimsOutput = execSync(
      `tmux -L ${getTmuxSocket()} display-message -p -t '${tmuxSession}' '#{pane_height} #{pane_width}'`,
      { encoding: 'utf-8' }
    ).trim();
    const [h, w] = dimsOutput.split(' ').map(Number);
    tmuxPaneDims = { height: h, width: w };
  } catch {
    /* best-effort */
  }

  const streamState = await page
    .evaluate(() => {
      const stream = (window as any).__schmuxStream;
      const terminal = (window as any).__schmuxTerminal;
      const diag: Record<string, unknown> = {};
      if (stream) {
        diag.writeBuffer = (stream.writeBuffer || '').length;
        diag.writeRAFPending = stream.writeRAFPending ?? null;
        diag.writingToTerminal = stream.writingToTerminal ?? null;
        diag.scrollRAFPending = stream.scrollRAFPending ?? null;
        diag.followTail = stream.followTail ?? null;
        diag.bootstrapState = stream.bootstrapState ?? null;
      }
      if (terminal) {
        const buf = terminal.buffer.active;
        diag.baseY = buf.baseY;
        diag.cursorX = buf.cursorX;
        diag.cursorY = buf.cursorY;
        diag.rows = terminal.rows;
        diag.cols = terminal.cols;
        diag.bufferLength = buf.length;
      }
      return diag;
    })
    .catch(() => ({ error: 'failed to read stream state' }));

  // Determine convergence pattern
  const wasConverging =
    convergenceLog.length > 3 && convergenceLog[convergenceLog.length - 1] < convergenceLog[0];
  const wasStuck = convergenceLog.length > 3 && new Set(convergenceLog.slice(-5)).size === 1;

  // Build diagnostic report
  const lines: string[] = [
    `# Terminal Fidelity Diagnostic`,
    ``,
    `**Session:** ${sessionId}`,
    `**Scrollback lines:** ${options?.scrollbackLines ?? 'viewport only'}`,
    `**Retries:** ${maxRetries} (${retryDelayMs}ms delay)`,
    `**Elapsed:** ${elapsedMs}ms`,
    `**Mismatched rows:** first=${firstMismatches?.length ?? 0}, last=${lastMismatches.length}`,
    `**Pattern:** ${wasStuck ? 'STUCK (same mismatch count for last 5 retries)' : wasConverging ? 'CONVERGING (mismatch count decreased)' : 'FLUCTUATING'}`,
    `**Tmux pane:** ${tmuxPaneDims.height}x${tmuxPaneDims.width}`,
    ``,
    `## Convergence Log (mismatch count per retry)`,
    ``,
    '```',
    convergenceLog
      .map((n, i) => `  retry ${String(i).padStart(3)}: ${n} mismatched rows`)
      .join('\n'),
    '```',
    ``,
    `## Stream State at Failure`,
    ``,
    '```json',
    JSON.stringify(streamState, null, 2),
    '```',
    ``,
    `## First Mismatch (retry 0)`,
    ``,
    '```',
    (firstMismatches ?? []).join('\n') || '(no mismatch on first attempt)',
    '```',
    ``,
    `## Last Mismatch (retry ${maxRetries})`,
    ``,
    '```',
    lastMismatches.join('\n'),
    '```',
    ``,
    `## Full Captures at Last Retry`,
    ``,
    `### tmux (${lastTmuxLines.length} lines)`,
    '```',
    lastTmuxLines.map((l, i) => `${String(i).padStart(3)}| ${JSON.stringify(l)}`).join('\n'),
    '```',
    ``,
    `### xterm.js (${lastXtermLines.length} lines)`,
    '```',
    lastXtermLines.map((l, i) => `${String(i).padStart(3)}| ${JSON.stringify(l)}`).join('\n'),
    '```',
  ];

  // Also include first captures if different from last
  if (firstTmuxLines.length > 0) {
    lines.push(
      ``,
      `## Full Captures at First Retry`,
      ``,
      `### tmux (${firstTmuxLines.length} lines)`,
      '```',
      firstTmuxLines.map((l, i) => `${String(i).padStart(3)}| ${JSON.stringify(l)}`).join('\n'),
      '```',
      ``,
      `### xterm.js (${firstXtermLines.length} lines)`,
      '```',
      firstXtermLines.map((l, i) => `${String(i).padStart(3)}| ${JSON.stringify(l)}`).join('\n'),
      '```'
    );
  }

  // Write diagnostic file
  const diagDir = '/tmp/terminal-diagnostics';
  try {
    mkdirSync(diagDir, { recursive: true });
    const timestamp = new Date().toISOString().replace(/[:.]/g, '-');
    const safeName = sessionId.replace(/[^a-zA-Z0-9-]/g, '_');
    writeFileSync(`${diagDir}/${timestamp}_${safeName}.md`, lines.join('\n'));
  } catch {
    // Best-effort — don't let diagnostic writing break the test
  }

  throw new Error(
    `Terminal fidelity mismatch (${lastMismatches.length} rows differ after ${maxRetries} retries):\n` +
      lastMismatches.join('\n')
  );
}

/**
 * Wait for a sentinel string to appear in the page's xterm.js buffer.
 * This polls the actual rendered terminal content, guaranteeing end-to-end
 * delivery through the full pipeline (WebSocket → writeLiveFrame → rAF →
 * writeTerminal → xterm.js parse). Using a separate WebSocket (the old
 * approach) only confirmed backend delivery to a different subscriber,
 * leaving a race with the browser-side rendering pipeline.
 *
 * Accepts either:
 *   waitForSentinel(sessionId, sentinel, page)
 *   waitForSentinel(sessionId, sentinel, page, timeoutMs)
 *   waitForSentinel(sessionId, sentinel, timeoutMs)  — legacy fallback
 */
export async function waitForSentinel(
  _sessionId: string,
  sentinel: string,
  pageOrTimeout?: Page | number,
  timeoutOrNothing?: number
): Promise<void> {
  let timeoutMs = 15_000;
  let page: Page | undefined;
  if (typeof pageOrTimeout === 'number') {
    timeoutMs = pageOrTimeout;
  } else {
    page = pageOrTimeout;
    if (timeoutOrNothing !== undefined) timeoutMs = timeoutOrNothing;
  }

  if (!page) {
    // Fallback to WebSocket-based wait if no page is available.
    const { waitForTerminalOutput } = await import('./helpers');
    await waitForTerminalOutput(_sessionId, sentinel, timeoutMs);
    return;
  }
  const deadline = Date.now() + timeoutMs;
  while (Date.now() < deadline) {
    const found = await page.evaluate((s: string) => {
      const terminal = (window as any).__schmuxTerminal;
      if (!terminal) return false;
      const buffer = terminal.buffer.active;
      const baseY = buffer.baseY;
      const rows = terminal.rows;
      // Check visible rows + recent scrollback for the sentinel
      const start = Math.max(0, baseY - 50);
      for (let i = start; i < baseY + rows; i++) {
        const line = buffer.getLine(i);
        if (line && line.translateToString(true).includes(s)) return true;
      }
      return false;
    }, sentinel);
    if (found) return;
    await new Promise((r) => setTimeout(r, 100));
  }
  // Capture buffer state for diagnostics on timeout
  const bufferDump = await page
    .evaluate(() => {
      const terminal = (window as any).__schmuxTerminal;
      if (!terminal) return { error: 'no terminal' };
      const buffer = terminal.buffer.active;
      const lines: string[] = [];
      const start = Math.max(0, buffer.baseY - 20);
      for (let i = start; i < buffer.baseY + terminal.rows; i++) {
        const line = buffer.getLine(i);
        lines.push(line ? line.translateToString(true) : '');
      }
      const stream = (window as any).__schmuxStream;
      return {
        lines,
        baseY: buffer.baseY,
        rows: terminal.rows,
        writeBuffer: stream?.writeBuffer?.length ?? -1,
        writeRAFPending: stream?.writeRAFPending ?? null,
      };
    })
    .catch(() => ({ error: 'evaluate failed' }));

  const diagDir = '/tmp/terminal-diagnostics';
  try {
    mkdirSync(diagDir, { recursive: true });
    const timestamp = new Date().toISOString().replace(/[:.]/g, '-');
    writeFileSync(
      `${diagDir}/${timestamp}_sentinel_timeout.md`,
      [
        `# Sentinel Timeout Diagnostic`,
        ``,
        `**Sentinel:** \`${sentinel}\``,
        `**Timeout:** ${timeoutMs}ms`,
        ``,
        `## Buffer state at timeout`,
        '```json',
        JSON.stringify(bufferDump, null, 2),
        '```',
      ].join('\n')
    );
  } catch {
    /* best-effort */
  }

  throw new Error(`Sentinel "${sentinel}" not found in xterm.js buffer after ${timeoutMs}ms`);
}

/**
 * Clear the tmux scrollback history for a session.
 * Use after `clear` to sync scrollback between tmux and xterm.js,
 * since tmux ignores \033[3J (sent by `clear`) for its own buffer
 * while xterm.js honors it and clears scrollback.
 */
export function clearTmuxHistory(tmuxSession: string): void {
  execSync(`tmux -L ${getTmuxSocket()} clear-history -t '${tmuxSession}'`);
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

  // Clear xterm.js state directly — the sanitize filter strips \033[2J and
  // \033[3J from the WebSocket stream, so escape-sequence-based clearing no
  // longer reaches xterm.js. Reset it programmatically instead.
  //
  // Drain the TerminalStream writeBuffer and cancel any pending rAF BEFORE
  // resetting. Without this, a pending requestAnimationFrame callback can
  // fire after reset() and write stale data into the freshly-cleared terminal.
  await page.evaluate(() => {
    const stream = (window as any).__schmuxStream;
    if (stream) {
      stream.writeBuffer = '';
      stream.writeRAFPending = false;
      stream.pendingWriteCb = null;
    }
    const terminal = (window as any).__schmuxTerminal;
    if (terminal) terminal.reset();
  });

  // Wait for reset to take effect by polling until buffer is empty
  const resetDeadline = Date.now() + 5_000;
  while (Date.now() < resetDeadline) {
    const isEmpty = await page.evaluate(() => {
      const terminal = (window as any).__schmuxTerminal;
      if (!terminal) return false;
      const buffer = terminal.buffer.active;
      for (let i = 0; i < terminal.rows; i++) {
        const line = buffer.getLine(buffer.baseY + i);
        if (line && line.translateToString(true).trim()) return false;
      }
      return true;
    });
    if (isEmpty) break;
    await new Promise((r) => setTimeout(r, 50));
  }

  // Clear tmux visible screen using ED0 (\033[J from cursor-home), which the
  // sanitize filter allows through. The shell re-displays its prompt via the
  // live stream, and the freshly-reset xterm.js receives it cleanly.
  sendTmuxCommand(tmuxName, "printf '\\033[H\\033[J'");

  // Wait for shell prompt to re-appear in xterm.js (proves clear + redraw completed)
  const promptDeadline = Date.now() + 5_000;
  while (Date.now() < promptDeadline) {
    const hasPrompt = await page.evaluate(() => {
      const terminal = (window as any).__schmuxTerminal;
      if (!terminal) return false;
      const buffer = terminal.buffer.active;
      for (let i = 0; i < terminal.rows; i++) {
        const line = buffer.getLine(buffer.baseY + i);
        if (line && line.translateToString(true).trim()) return true;
      }
      return false;
    });
    if (hasPrompt) break;
    await new Promise((r) => setTimeout(r, 50));
  }

  // Clear tmux's scrollback history (xterm.js scrollback was cleared by reset).
  // This is a synchronous local tmux command — no wait needed.
  clearTmuxHistory(tmuxName);

  // Wait for terminal size to stabilize. The frontend debounces resize events
  // by 300ms, so a delayed fitTerminal() call can resize the tmux pane AFTER
  // openTerminal returns. This causes content reflow in tmux that diverges from
  // xterm.js, especially for tests involving scroll regions or cursor positioning.
  // Poll until the tmux pane dimensions match xterm.js for 2 consecutive checks.
  const sizeDeadline = Date.now() + 5_000;
  let consecutiveMatches = 0;
  while (Date.now() < sizeDeadline && consecutiveMatches < 2) {
    const dims = await page.evaluate(() => {
      const terminal = (window as any).__schmuxTerminal;
      if (!terminal) return null;
      return { rows: terminal.rows, cols: terminal.cols };
    });
    if (dims) {
      try {
        const tmuxDims = execSync(
          `tmux -L ${getTmuxSocket()} display-message -p -t '${tmuxName}' '#{pane_height} #{pane_width}'`,
          { encoding: 'utf-8' }
        ).trim();
        const [h, w] = tmuxDims.split(' ').map(Number);
        if (h === dims.rows && w === dims.cols) {
          consecutiveMatches++;
        } else {
          consecutiveMatches = 0;
        }
      } catch {
        consecutiveMatches = 0;
      }
    }
    if (consecutiveMatches < 2) {
      await new Promise((r) => setTimeout(r, 200));
    }
  }
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
  const output = execSync(
    `tmux -L ${getTmuxSocket()} display-message -p -t '${tmuxSession}' '#{cursor_x} #{cursor_y}'`,
    {
      encoding: 'utf-8',
    }
  ).trim();
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
 * On failure, writes diagnostics to /tmp/terminal-diagnostics/.
 */
export async function assertCursorMatchesTmux(page: Page, tmuxSession: string): Promise<void> {
  const maxRetries = 50;
  const retryDelayMs = 200;

  let lastTmux = { x: 0, y: 0 };
  let lastXterm = { x: 0, y: 0 };
  let firstTmux = { x: 0, y: 0 };
  let firstXterm = { x: 0, y: 0 };
  let firstRecorded = false;

  for (let attempt = 0; attempt <= maxRetries; attempt++) {
    if (attempt > 0) {
      await new Promise((r) => setTimeout(r, retryDelayMs));
    }

    lastTmux = getTmuxCursorPosition(tmuxSession);
    lastXterm = await getXtermCursorPosition(page);

    if (!firstRecorded) {
      firstTmux = { ...lastTmux };
      firstXterm = { ...lastXterm };
      firstRecorded = true;
    }

    if (lastTmux.x === lastXterm.x && lastTmux.y === lastXterm.y) return;
  }

  // Get tmux pane dimensions and xterm dimensions for comparison
  let tmuxPaneDims = { height: -1, width: -1 };
  try {
    const dimsOutput = execSync(
      `tmux -L ${getTmuxSocket()} display-message -p -t '${tmuxSession}' '#{pane_height} #{pane_width}'`,
      { encoding: 'utf-8' }
    ).trim();
    const [h, w] = dimsOutput.split(' ').map(Number);
    tmuxPaneDims = { height: h, width: w };
  } catch {
    /* best-effort */
  }

  const xtermDims = await page
    .evaluate(() => {
      const terminal = (window as any).__schmuxTerminal;
      if (!terminal) return { rows: -1, cols: -1, baseY: -1 };
      return { rows: terminal.rows, cols: terminal.cols, baseY: terminal.buffer.active.baseY };
    })
    .catch(() => ({ rows: -1, cols: -1, baseY: -1 }));

  const diagDir = '/tmp/terminal-diagnostics';
  try {
    mkdirSync(diagDir, { recursive: true });
    const timestamp = new Date().toISOString().replace(/[:.]/g, '-');
    writeFileSync(
      `${diagDir}/${timestamp}_cursor_${tmuxSession.replace(/[^a-zA-Z0-9-]/g, '_')}.md`,
      [
        `# Cursor Position Diagnostic`,
        ``,
        `**Session:** ${tmuxSession}`,
        `**Retries:** ${maxRetries}`,
        `**Tmux pane:** ${tmuxPaneDims.height}x${tmuxPaneDims.width}`,
        `**Xterm.js:** ${xtermDims.rows}x${xtermDims.cols} (baseY: ${xtermDims.baseY})`,
        ``,
        `## First attempt`,
        `- tmux:  (${firstTmux.x}, ${firstTmux.y})`,
        `- xterm: (${firstXterm.x}, ${firstXterm.y})`,
        ``,
        `## Last attempt`,
        `- tmux:  (${lastTmux.x}, ${lastTmux.y})`,
        `- xterm: (${lastXterm.x}, ${lastXterm.y})`,
      ].join('\n')
    );
  } catch {
    /* best-effort */
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
  const output = execSync(
    `tmux -L ${getTmuxSocket()} display-message -p -t '${tmuxSession}' '#{cursor_flag}'`,
    {
      encoding: 'utf-8',
    }
  ).trim();
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
  const maxRetries = 50;
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
