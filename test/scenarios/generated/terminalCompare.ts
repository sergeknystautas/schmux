// Pure terminal-comparison and marker-encoding logic for scenario helpers.
// NO PROJECT IMPORTS: this module is compiled by two different tsconfig
// programs (test/scenarios/generated, tools/test-runner) and executed by
// tsx (runner self-tests). Node builtins only.

import { mkdirSync, writeFileSync } from 'fs';

let comparisons = 0;

export function comparisonCount(): number {
  return comparisons;
}

export function resetComparisonCount(): void {
  comparisons = 0;
}

/** Unique prompt marker per wait. Appears in the rendered prompt only. */
export function buildPromptMarker(n: number): string {
  return `__FIDELITY_${n}__`;
}

/**
 * PS1 assignment whose VALUE contains the contiguous marker but whose SOURCE
 * TEXT never does. The tty echoes the source on input receipt (potentially
 * while the previous command is still producing output), so the marker must
 * only be matchable in the rendered prompt — which the shell draws strictly
 * after the previous command finished. The quote-split concatenates
 * '<partA>' and "<partB> $ " into `<marker> $ `.
 */
export function buildMarkerPS1Assignment(marker: string): string {
  const split = Math.ceil(marker.length / 2);
  const a = marker.slice(0, split);
  const b = marker.slice(split);
  return `PS1='${a}'"${b} $ "`;
}

/** Compare tmux and xterm.js content; empty result = match. Counts once per call. */
export function compareTerminalContent(tmuxLines: string[], xtermLines: string[]): string[] {
  comparisons++;
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

/** Best-effort diagnostic artifact write — never throws into the test path. */
export function writeDiagnosticArtifact(diagDir: string, filename: string, report: string): void {
  try {
    mkdirSync(diagDir, { recursive: true });
    writeFileSync(`${diagDir}/${filename}`, report);
  } catch {
    /* best-effort — don't let diagnostic writing break the test */
  }
}
