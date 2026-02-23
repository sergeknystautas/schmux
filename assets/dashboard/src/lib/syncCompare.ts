export type CompareResult = {
  match: boolean;
  diffRows: number[];
  skip?: boolean;
};

/**
 * Compare xterm.js visible lines against sync snapshot lines.
 * Both sides should be plain text (ANSI already stripped from sync).
 * Returns match=true if content is equivalent, or diffRows listing mismatched row indices.
 * Returns skip=true if dimensions are incompatible (resize race).
 */
export function compareScreens(xtermLines: string[], syncLines: string[]): CompareResult {
  const xtermLen = xtermLines.length;
  const syncLen = syncLines.length;

  // Dimension check: if the row counts differ significantly,
  // it's likely a resize race — skip this comparison
  if (xtermLen !== syncLen && Math.abs(xtermLen - syncLen) > 2) {
    return { match: false, diffRows: [], skip: true };
  }

  const maxRows = Math.max(xtermLen, syncLen);
  const diffRows: number[] = [];

  for (let i = 0; i < maxRows; i++) {
    const xtermLine = (xtermLines[i] ?? '').trimEnd();
    const syncLine = (syncLines[i] ?? '').trimEnd();
    if (xtermLine !== syncLine) {
      diffRows.push(i);
    }
  }

  // Trailing empty rows: if all diffs are in the trailing region where one
  // side has content and the other has empty/missing lines, treat as match
  if (diffRows.length > 0) {
    const contentEnd = Math.min(xtermLen, syncLen);
    const allTrailing = diffRows.every((row) => {
      if (row >= contentEnd) {
        const xLine = (xtermLines[row] ?? '').trimEnd();
        const sLine = (syncLines[row] ?? '').trimEnd();
        return xLine === '' && sLine === '';
      }
      return false;
    });
    if (allTrailing) {
      return { match: true, diffRows: [] };
    }
  }

  return { match: diffRows.length === 0, diffRows };
}
