import { describe, it, expect } from 'vitest';
import { buildSurgicalCorrection, buildCursorCorrection } from './surgicalCorrection';

describe('buildSurgicalCorrection', () => {
  it('generates escape sequences for a single differing row', () => {
    const correction = buildSurgicalCorrection(
      [3], // row 3 differs
      ['corrected line content'], // ANSI content for row 3
      { row: 10, col: 5, visible: true } // cursor to restore
    );

    // Should contain: save cursor, move to row 4 (1-indexed), clear line, content, restore cursor
    expect(correction).toContain('\x1b7'); // DECSC save
    expect(correction).toContain('\x1b[4;1H'); // move to row 4, col 1
    expect(correction).toContain('\x1b[2K'); // clear line
    expect(correction).toContain('corrected line content');
    expect(correction).toContain('\x1b8'); // DECRC restore
  });

  it('generates corrections for multiple rows', () => {
    const correction = buildSurgicalCorrection([1, 5], ['row 1 content', 'row 5 content'], {
      row: 0,
      col: 0,
      visible: true,
    });

    expect(correction).toContain('\x1b[2;1H'); // row 1 (1-indexed = 2)
    expect(correction).toContain('\x1b[6;1H'); // row 5 (1-indexed = 6)
    expect(correction).toContain('row 1 content');
    expect(correction).toContain('row 5 content');
  });

  it('resets SGR before each row to prevent attribute bleed', () => {
    const correction = buildSurgicalCorrection([0], ['\x1b[32mgreen text\x1b[0m'], {
      row: 0,
      col: 0,
      visible: true,
    });

    // Should reset attributes before writing content
    expect(correction).toContain('\x1b[0m');
  });

  it('restores cursor visibility', () => {
    const hidden = buildSurgicalCorrection([0], ['x'], { row: 0, col: 0, visible: false });
    expect(hidden).toContain('\x1b[?25l'); // cursor hidden

    const visible = buildSurgicalCorrection([0], ['x'], { row: 0, col: 0, visible: true });
    expect(visible).toContain('\x1b[?25h'); // cursor visible
  });

  it('returns empty string when no rows differ', () => {
    const correction = buildSurgicalCorrection([], [], { row: 0, col: 0, visible: true });
    expect(correction).toBe('');
  });

  it('generates correct 1-indexed position for row 0', () => {
    const correction = buildSurgicalCorrection([0], ['hello'], {
      row: 5,
      col: 3,
      visible: true,
    });

    // Row 0 → 1-indexed = row 1, col 1
    expect(correction).toContain('\x1b[1;1H');
    // Should contain save/restore cursor
    expect(correction).toContain('\x1b7');
    expect(correction).toContain('\x1b8');
    // Should restore cursor to row 6, col 4 (1-indexed)
    expect(correction).toContain('\x1b[6;4H');
    // Content
    expect(correction).toContain('hello');
  });
});

describe('buildCursorCorrection', () => {
  it('generates CUP sequence for cursor repositioning', () => {
    const correction = buildCursorCorrection(
      { row: 5, col: 10, visible: true },
      { row: 3, col: 7 }
    );
    // Should move cursor to row 6, col 11 (1-indexed from sync cursor)
    expect(correction).toContain('\x1b[6;11H');
    // Should NOT contain DECSC/DECRC or line clearing
    expect(correction).not.toContain('\x1b7');
    expect(correction).not.toContain('\x1b[2K');
  });

  it('restores cursor visibility when visible', () => {
    const correction = buildCursorCorrection(
      { row: 0, col: 0, visible: true },
      { row: 5, col: 5 }
    );
    expect(correction).toContain('\x1b[?25h');
  });

  it('restores cursor visibility when hidden', () => {
    const correction = buildCursorCorrection(
      { row: 0, col: 0, visible: false },
      { row: 5, col: 5 }
    );
    expect(correction).toContain('\x1b[?25l');
  });

  it('returns empty string when cursor already matches', () => {
    const correction = buildCursorCorrection(
      { row: 3, col: 7, visible: true },
      { row: 3, col: 7 }
    );
    expect(correction).toBe('');
  });
});
