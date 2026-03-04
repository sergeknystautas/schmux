export interface CursorState {
  row: number;
  col: number;
  visible: boolean;
}

/**
 * Build an ANSI escape sequence string that surgically overwrites specific
 * viewport rows without affecting scrollback.
 *
 * Uses DECSC/DECRC to save and restore cursor position, and CSI sequences
 * to move to each row, clear it, reset attributes, and write the correct content.
 */
export function buildSurgicalCorrection(
  diffRows: number[],
  rowContents: string[],
  cursor: CursorState
): string {
  if (diffRows.length === 0) return '';

  let seq = '';
  seq += '\x1b7'; // DECSC: save cursor position + attributes

  for (let i = 0; i < diffRows.length; i++) {
    const row = diffRows[i];
    const content = rowContents[i] ?? '';
    // CSI row;col H (1-indexed)
    seq += `\x1b[${row + 1};1H`;
    // EL 2: clear entire line
    seq += '\x1b[2K';
    // SGR 0: reset attributes to prevent bleed from previous content
    seq += '\x1b[0m';
    // Write the correct content
    seq += content;
  }

  seq += '\x1b8'; // DECRC: restore cursor position + attributes

  // Restore cursor position explicitly (DECRC might not be supported everywhere)
  seq += `\x1b[${cursor.row + 1};${cursor.col + 1}H`;

  // Restore cursor visibility
  seq += cursor.visible ? '\x1b[?25h' : '\x1b[?25l';

  return seq;
}

/**
 * Build a minimal ANSI escape sequence to correct cursor position only,
 * without touching screen content. Used when sync detects that content
 * matches but cursor is at the wrong position.
 */
export function buildCursorCorrection(
  cursor: CursorState,
  xtermCursor: { row: number; col: number }
): string {
  if (cursor.row === xtermCursor.row && cursor.col === xtermCursor.col) {
    return '';
  }

  let seq = '';
  // CUP: move cursor to correct position (1-indexed)
  seq += `\x1b[${cursor.row + 1};${cursor.col + 1}H`;
  // Restore cursor visibility
  seq += cursor.visible ? '\x1b[?25h' : '\x1b[?25l';
  return seq;
}
