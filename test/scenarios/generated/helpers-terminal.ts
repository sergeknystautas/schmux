import { type Page } from '@playwright/test';
import { execSync } from 'child_process';
import { waitForTerminalOutput } from './helpers';

let sentinelCounter = 0;

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
      const totalLines = buffer.length;
      const startLine = Math.max(0, totalLines - opts.scrollbackLines);
      for (let i = startLine; i < totalLines; i++) {
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

function shellQuote(s: string): string {
  return "$'" + s.replace(/\\/g, '\\\\').replace(/'/g, "\\'") + "'";
}
