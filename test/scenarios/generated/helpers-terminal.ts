import { type Page } from '@playwright/test';
import { execSync } from 'child_process';
import { waitForDashboardLive } from './helpers';
import {
  buildPromptMarker,
  buildMarkerPS1Assignment,
  assertSingleTerminalComparison,
  writeDiagnosticArtifact,
} from './terminalCompare';

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
 * Send a command, then replace PS1 with one containing a fresh unique marker.
 * The marker renders only when the shell draws the next prompt — strictly
 * after `command` finished — and the quote-split assignment never echoes the
 * contiguous marker, so the returned string is a true completion boundary.
 * Use with waitForSentinel() / assertTerminalMatchesTmux().
 */
export function sendTmuxCommandWithSentinel(tmuxSession: string, command: string): string {
  const sentinel = buildPromptMarker(++sentinelCounter);
  sendTmuxCommand(tmuxSession, command);
  sendTmuxCommand(tmuxSession, buildMarkerPS1Assignment(sentinel));
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
 * Assert terminal fidelity: ONE wait (marker + pipeline clean), ONE capture of
 * each side, ONE comparison. Mismatch throws immediately with a diagnostic
 * artifact — a stable mismatch and a converging one are no longer confused.
 */
export async function assertTerminalMatchesTmux(
  page: Page,
  sessionId: string,
  options: { sentinel: string; scrollbackLines?: number }
): Promise<void> {
  const waitStart = Date.now();
  const settled = await waitForRenderSettledOnPage(page, { marker: options.sentinel });

  const tmuxLines = capturePane(sessionId, options);
  const xtermLines = await readXtermBuffer(page, options);
  await assertSingleTerminalComparison(tmuxLines, xtermLines, async (mismatches) => {
    const tmuxPaneDims = getTmuxPaneDims(sessionId);
    const streamState = await snapshotStreamState(page);
    const report = [
      '# Terminal Fidelity Diagnostic',
      '',
      `**Session:** ${sessionId}`,
      `**Sentinel:** ${options.sentinel}`,
      `**Scrollback lines:** ${options.scrollbackLines ?? 'viewport only'}`,
      `**Wait started:** ${new Date(waitStart).toISOString()}`,
      ...renderSettleDiagnosticLines(settled),
      `**Compared once** at ${new Date().toISOString()} — no retries`,
      `**Mismatched rows:** ${mismatches.length}`,
      `**Tmux pane:** ${tmuxPaneDims.height}x${tmuxPaneDims.width}`,
      '',
      '## Stream State at Mismatch',
      '',
      '```json',
      JSON.stringify(streamState, null, 2),
      '```',
      '',
      '## Mismatch',
      '',
      '```',
      mismatches.join('\n'),
      '```',
      '',
      '## Full Captures',
      '',
      `### tmux (${tmuxLines.length} lines)`,
      '```',
      tmuxLines.map((l, i) => `${String(i).padStart(3)}| ${JSON.stringify(l)}`).join('\n'),
      '```',
      '',
      `### xterm.js (${xtermLines.length} lines)`,
      '```',
      xtermLines.map((l, i) => `${String(i).padStart(3)}| ${JSON.stringify(l)}`).join('\n'),
      '```',
    ].join('\n');
    writeDiagnosticArtifact(
      '/tmp/terminal-diagnostics',
      `${new Date().toISOString().replace(/[:.]/g, '-')}_${sessionId.replace(/[^a-zA-Z0-9-]/g, '_')}.md`,
      report
    );
  });
}

/**
 * Get the tmux pane dimensions as seen by tmux display-message.
 * Returns {-1, -1} on failure (best-effort).
 */
function getTmuxPaneDims(tmuxSession: string): { height: number; width: number } {
  try {
    const dimsOutput = execSync(
      `tmux -L ${getTmuxSocket()} display-message -p -t '${tmuxSession}' '#{pane_height} #{pane_width}'`,
      { encoding: 'utf-8' }
    ).trim();
    const [h, w] = dimsOutput.split(' ').map(Number);
    return { height: h, width: w };
  } catch {
    return { height: -1, width: -1 };
  }
}

/**
 * Snapshot diagnostic state from the page's exposed stream + terminal.
 * Returns {} on failure (best-effort).
 */
async function snapshotStreamState(page: Page): Promise<Record<string, unknown>> {
  return page
    .evaluate(() => {
      const stream = (window as any).__schmuxStream;
      const terminal = (window as any).__schmuxTerminal;
      const diag: Record<string, unknown> = {};
      if (stream) {
        diag.writeBuffer = (stream.writeBuffer || '').length;
        diag.writeRAFPending = stream.writeRAFPending ?? null;
        diag.pendingWriteCb = stream.pendingWriteCb !== null;
        diag.writingToTerminal = stream.writingToTerminal ?? null;
        diag.writeGuardTimer = stream.writeGuardTimer !== null;
        diag.scrollRAFPending = stream.scrollRAFPending ?? null;
        diag.viewportSyncRAFPending = stream.viewportSyncRAFPending ?? null;
        diag.followTail = stream.followTail ?? null;
        diag.gapRequestPending = stream.gapRequestPending ?? null;
        diag.resizeDebounceTimer = stream.resizeDebounceTimer !== null;
        diag.bootstrapped = stream.bootstrapped ?? null;
        diag.bootstrapComplete = stream.bootstrapComplete ?? null;
        diag.lastReceivedSeq = String(stream.lastReceivedSeq ?? 'n/a');
        diag.lastResizeAppliedAt = stream.lastResizeAppliedAt ?? null;
        diag.evaluationTrace = stream.evaluationTrace ?? [];
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
}

type PageRenderSettleResult = { markerSeenAt?: number; settledAt: number; lastSeq: string };

function renderSettleDiagnosticLines(result: PageRenderSettleResult): string[] {
  return [
    `**Marker seen (performance clock):** ${result.markerSeenAt ?? 'not recorded'}ms`,
    `**Render settled (performance clock):** ${result.settledAt}ms`,
    `**Last rendered sequence:** ${result.lastSeq}`,
  ];
}

/**
 * In-page completion wait. Throws with an artifact when the wait fails.
 * Bridges the page.evaluate boundary — the in-page API never rejects, so
 * Node-side helpers own failure semantics and artifact writing.
 */
export async function waitForRenderSettledOnPage(
  page: Page,
  condition: { marker: string } | { bootstrapComplete: true } | { resizeApplied: true },
  timeoutMs = 15_000
): Promise<{ markerSeenAt?: number; settledAt: number; lastSeq: string }> {
  const result = await page.evaluate(
    ({ cond, timeout }) =>
      (window as any).__schmuxStream.waitForRenderSettled(cond, { timeoutMs: timeout }),
    { cond: condition, timeout: timeoutMs }
  );
  if (!result || result.ok !== true) {
    writeDiagnosticArtifact(
      '/tmp/terminal-diagnostics',
      `${new Date().toISOString().replace(/[:.]/g, '-')}_settle_timeout.md`,
      [
        '# Render Settle Failure',
        '',
        `**Condition:** ${JSON.stringify(condition)}`,
        `**Timeout:** ${timeoutMs}ms`,
        '',
        '```json',
        JSON.stringify(result ?? { error: 'page.evaluate returned nothing' }, null, 2),
        '```',
      ].join('\n')
    );
    throw new Error(
      `Terminal did not settle for ${JSON.stringify(condition)} within ${timeoutMs}ms` +
        (result?.timedOut ? ' (timed out)' : ` (reason: ${result?.reason ?? 'unknown'})`)
    );
  }
  return result;
}

/**
 * Wait for a sentinel string to appear in the page's xterm.js buffer.
 * Uses the stream's event-driven waitForRenderSettled API (one deadline,
 * one capture — not a polling loop). The page argument is required; the
 * legacy no-page WebSocket fallback has been removed.
 */
export async function waitForSentinel(
  _sessionId: string,
  sentinel: string,
  page: Page,
  timeoutMs = 15_000
): Promise<void> {
  await waitForRenderSettledOnPage(page, { marker: sentinel }, timeoutMs);
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

  // Wait for the terminal WebSocket bootstrap — the real control event, not a
  // content proxy. bootstrapComplete is per-connection (connect() resets it).
  await waitForRenderSettledOnPage(page, { bootstrapComplete: true }, 15_000);

  // Clear xterm.js state via the stream's test API. The sanitize filter strips
  // \033[2J and \033[3J, so escape-sequence clearing never reaches xterm.
  // resetAndSettle drains any submitted xterm write BEFORE resetting (nothing
  // survives in xterm's write queue) and cancels the armed write-flush rAF —
  // replacing the old direct mutation of TerminalStream private fields, which
  // left the rAF armed to fire after reset.
  const reset = await page.evaluate(() =>
    (window as any).__schmuxStream.resetAndSettle({ timeoutMs: 15_000 })
  );
  if (!reset?.ok) {
    throw new Error(`resetAndSettle failed: ${JSON.stringify(reset)}`);
  }

  // Clear tmux's visible screen with ED0 (allowed by the sanitize filter) and
  // wait for the prompt redraw via a prompt-embedded marker — proves the clear
  // AND the redraw completed through the live stream.
  const clearMarker = sendTmuxCommandWithSentinel(tmuxName, "printf '\\033[H\\033[J'");
  await waitForRenderSettledOnPage(page, { marker: clearMarker }, 15_000);

  // Clear tmux's scrollback history (xterm scrollback was cleared by reset).
  clearTmuxHistory(tmuxName);

  // Wait for the frontend's 300ms resize debounce to drain and the resize to
  // be applied + sent, instead of guessing past it with a fixed sleep.
  await waitForRenderSettledOnPage(page, { resizeApplied: true }, 15_000);

  // Backend→tmux resize propagation is an opaque external-process boundary
  // with no event channel to the browser: one bounded probe (rubric rule 6).
  const sizeDeadline = Date.now() + 10_000;
  let lastObserved = 'none';
  while (Date.now() < sizeDeadline) {
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
        lastObserved = `tmux ${h}x${w} vs xterm ${dims.rows}x${dims.cols}`;
        if (h === dims.rows && w === dims.cols) return; // sizes agree — done
      } catch {
        lastObserved = `tmux query failed; xterm ${dims.rows}x${dims.cols}`;
      }
    }
    await new Promise((r) => setTimeout(r, 200));
  }
  throw new Error(`Terminal size did not stabilize within 10s (last observed: ${lastObserved})`);
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
 * Both use 0-indexed coordinates. Requires a sentinel — the same prompt
 * marker used for the content assertion synchronizes the cursor comparison
 * to the post-command quiescent state.
 */
export async function assertCursorMatchesTmux(
  page: Page,
  tmuxSession: string,
  options: { sentinel: string }
): Promise<void> {
  const waitStart = Date.now();
  const settled = await waitForRenderSettledOnPage(page, { marker: options.sentinel });
  const tmux = getTmuxCursorPosition(tmuxSession);
  const xterm = await getXtermCursorPosition(page);
  if (tmux.x === xterm.x && tmux.y === xterm.y) return;

  const streamState = await snapshotStreamState(page);

  writeDiagnosticArtifact(
    '/tmp/terminal-diagnostics',
    `${new Date().toISOString().replace(/[:.]/g, '-')}_cursor_${tmuxSession.replace(/[^a-zA-Z0-9-]/g, '_')}.md`,
    [
      '# Cursor Position Diagnostic',
      '',
      `**Session:** ${tmuxSession}`,
      `**Sentinel:** ${options.sentinel}`,
      `**Wait started:** ${new Date(waitStart).toISOString()}`,
      ...renderSettleDiagnosticLines(settled),
      `**Tmux pane:** ${JSON.stringify(getTmuxPaneDims(tmuxSession))}`,
      `**Compared once** at ${new Date().toISOString()} — no retries`,
      '',
      `- tmux:  (${tmux.x}, ${tmux.y})`,
      `- xterm: (${xterm.x}, ${xterm.y})`,
      '',
      '## Stream State at Mismatch',
      '',
      '```json',
      JSON.stringify(streamState, null, 2),
      '```',
    ].join('\n')
  );
  throw new Error(
    `Cursor position mismatch:\n  tmux:  (${tmux.x}, ${tmux.y})\n  xterm: (${xterm.x}, ${xterm.y})`
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

export async function assertCursorVisibilityMatchesTmux(
  page: Page,
  tmuxSession: string,
  options: { sentinel: string }
): Promise<void> {
  const waitStart = Date.now();
  const settled = await waitForRenderSettledOnPage(page, { marker: options.sentinel });
  const tmuxVisible = getTmuxCursorVisible(tmuxSession);
  const xtermVisible = await getXtermCursorVisible(page);
  if (tmuxVisible === xtermVisible) return;

  const streamState = await snapshotStreamState(page);
  writeDiagnosticArtifact(
    '/tmp/terminal-diagnostics',
    `${new Date().toISOString().replace(/[:.]/g, '-')}_cursor_visibility_${tmuxSession.replace(/[^a-zA-Z0-9-]/g, '_')}.md`,
    [
      '# Cursor Visibility Diagnostic',
      '',
      `**Session:** ${tmuxSession}`,
      `**Sentinel:** ${options.sentinel}`,
      `**Wait started:** ${new Date(waitStart).toISOString()}`,
      ...renderSettleDiagnosticLines(settled),
      `**Compared once** at ${new Date().toISOString()} — no retries`,
      '',
      `- tmux:  ${tmuxVisible ? 'visible' : 'hidden'}`,
      `- xterm: ${xtermVisible ? 'visible' : 'hidden'}`,
      '',
      '## Stream State at Mismatch',
      '',
      '```json',
      JSON.stringify(streamState, null, 2),
      '```',
    ].join('\n')
  );
  throw new Error(
    `Cursor visibility mismatch:\n  tmux:  ${tmuxVisible ? 'visible' : 'hidden'}\n  xterm: ${xtermVisible ? 'visible' : 'hidden'}`
  );
}
