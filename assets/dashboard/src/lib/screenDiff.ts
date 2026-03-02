import { stripAnsi } from './ansiStrip';

export interface ScreenDiff {
  differingRows: Array<{ row: number; tmux: string; xterm: string }>;
  summary: string;
  diffText: string;
}

export function computeScreenDiff(tmuxScreen: string, xtermScreen: string): ScreenDiff {
  const tmuxLines = tmuxScreen.split('\n');
  const xtermLines = xtermScreen.split('\n');
  const maxRows = Math.max(tmuxLines.length, xtermLines.length);
  const differingRows: ScreenDiff['differingRows'] = [];

  for (let i = 0; i < maxRows; i++) {
    const rawTmuxLine = tmuxLines[i] ?? '';
    const tmuxLine = stripAnsi(rawTmuxLine).trimEnd();
    const xtermLine = (xtermLines[i] ?? '').trimEnd();
    if (tmuxLine !== xtermLine) {
      differingRows.push({ row: i, tmux: rawTmuxLine, xterm: xtermLines[i] ?? '' });
    }
  }

  const summary = `${differingRows.length} rows differ`;
  const diffText =
    differingRows.length === 0
      ? 'Screens match.'
      : differingRows
          .map((d) => `Row ${d.row}:\n  tmux:  ${d.tmux}\n  xterm: ${d.xterm}`)
          .join('\n') + `\n---\n${summary}`;

  return { differingRows, summary, diffText };
}
