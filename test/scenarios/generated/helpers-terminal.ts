import { type Page } from '@playwright/test';
import { execSync } from 'child_process';
import { waitForTerminalOutput } from './helpers';

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
 * Assert that the xterm.js terminal content matches tmux's capture-pane output.
 * Compares line-by-line with trimmed trailing whitespace.
 * Throws with a detailed diff on mismatch.
 */
export async function assertTerminalMatchesTmux(
  page: Page,
  sessionId: string,
  options?: { scrollbackLines?: number }
): Promise<void> {
  const tmuxSession = sessionId;

  const tmuxLines = capturePane(tmuxSession, options);
  const xtermLines = await readXtermBuffer(page, options);

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

  if (mismatches.length > 0) {
    throw new Error(
      `Terminal fidelity mismatch (${mismatches.length} rows differ):\n` + mismatches.join('\n')
    );
  }
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
  await new Promise((r) => setTimeout(r, 200));
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

function shellQuote(s: string): string {
  return (
    '"' +
    s.replace(/\\/g, '\\\\').replace(/"/g, '\\"').replace(/\$/g, '\\$').replace(/`/g, '\\`') +
    '"'
  );
}
