import { test } from 'node:test';
import assert from 'node:assert/strict';
import { execFileSync } from 'node:child_process';
import { mkdtempSync, readFileSync, rmSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import {
  compareTerminalContent,
  assertSingleTerminalComparison,
  comparisonCount,
  resetComparisonCount,
  buildPromptMarker,
  buildMarkerPS1Assignment,
  writeDiagnosticArtifact,
} from '../../../../test/scenarios/generated/terminalCompare.js';

test('stable mismatch reports rows and counts as exactly one comparison', async () => {
  resetComparisonCount();
  const before = comparisonCount();
  const dir = mkdtempSync(join(tmpdir(), 'terminal-mismatch-'));
  try {
    await assert.rejects(
      () =>
        assertSingleTerminalComparison(['hello', 'world'], ['hello', 'wrld'], (mismatches) => {
          writeDiagnosticArtifact(dir, 'mismatch.md', mismatches.join('\n'));
        }),
      /Terminal fidelity mismatch \(1 rows differ\)/
    );
    assert.match(readFileSync(join(dir, 'mismatch.md'), 'utf-8'), /Row 1/);
    assert.equal(comparisonCount() - before, 1);
  } finally {
    rmSync(dir, { recursive: true, force: true });
  }
});

test('identical captures match with one comparison', () => {
  resetComparisonCount();
  const mismatches = compareTerminalContent(['a', 'b'], ['a', 'b']);
  assert.equal(mismatches.length, 0);
  assert.equal(comparisonCount(), 1);
});

test('marker assignment never contains the contiguous marker, but its PS1 value does', () => {
  const marker = buildPromptMarker(5);
  assert.equal(marker, '__FIDELITY_5__');
  const assignment = buildMarkerPS1Assignment(marker);
  assert.ok(!assignment.includes(marker), 'echoed input must not contain the marker');
  // Evaluate the assignment in a real POSIX shell and inspect the PS1 value.
  const script = `${assignment}; printf %s "$PS1"`;
  const value = execFileSync('/bin/sh', ['-c', script], { encoding: 'utf-8' });
  assert.ok(
    value.includes(marker),
    `PS1 value must contain the marker, got: ${JSON.stringify(value)}`
  );
});

test('writeDiagnosticArtifact writes the report to disk', () => {
  const dir = mkdtempSync(join(tmpdir(), 'terminal-compare-'));
  try {
    writeDiagnosticArtifact(dir, 'report.md', '# hello');
    assert.equal(readFileSync(join(dir, 'report.md'), 'utf-8'), '# hello');
  } finally {
    rmSync(dir, { recursive: true, force: true });
  }
});
