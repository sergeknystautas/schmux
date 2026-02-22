import type { IBuffer } from '@xterm/xterm';

export function extractScreenText(buffer: IBuffer): string {
  const lines: string[] = [];
  for (let y = 0; y < buffer.length; y++) {
    const line = buffer.getLine(y);
    if (line) {
      lines.push(line.translateToString());
    }
  }
  return lines.join('\n') + '\n';
}

/**
 * Extract only the visible viewport text from the buffer.
 * This matches what tmux's `capture-pane` returns (visible screen only),
 * making diagnostic diffs meaningful instead of always differing due to
 * xterm.js having full scrollback while tmux captures one screen.
 */
export function extractViewportText(buffer: IBuffer, rows: number): string {
  const lines: string[] = [];
  const start = buffer.baseY;
  for (let y = start; y < start + rows && y < buffer.length; y++) {
    const line = buffer.getLine(y);
    if (line) {
      lines.push(line.translateToString());
    }
  }
  return lines.join('\n') + '\n';
}
