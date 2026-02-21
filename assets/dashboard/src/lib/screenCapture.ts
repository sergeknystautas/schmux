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
