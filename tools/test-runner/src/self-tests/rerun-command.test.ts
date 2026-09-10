import { test } from 'node:test';
import assert from 'node:assert/strict';
import {
  frontendRerunCommand,
  frontendRunCommand,
  shellSingleQuote,
  vitestNamePattern,
} from '../parsers.js';

test('vitestNamePattern space-joins ancestors and title, regex-escaped for -t', () => {
  assert.equal(vitestNamePattern(['math'], '2 + 2 should equal 4'), 'math 2 \\+ 2 should equal 4');
});

test('vitestNamePattern keeps a literal > inside a title intact', () => {
  // 10 dashboard titles contain comparison operators. vitest matches -t
  // against the space-joined ancestors+title, where that > is literal text —
  // collapsing separator arrows would eat it and the pattern would match
  // nothing.
  assert.equal(
    vitestNamePattern([], 'strippedControlChars > 0 (singular)'),
    'strippedControlChars > 0 \\(singular\\)'
  );
  assert.equal(
    vitestNamePattern(['ClipboardBanner'], 'strippedControlChars > 0 (singular)'),
    'ClipboardBanner strippedControlChars > 0 \\(singular\\)'
  );
});

test('frontendRunCommand single-quotes the pattern for the shell', () => {
  assert.equal(
    frontendRunCommand('math 2 \\+ 2 should equal 4'),
    "./test.sh --frontend --run 'math 2 \\+ 2 should equal 4'"
  );
});

test('frontendRerunCommand (identity fallback) strips the file segment and collapses > to space', () => {
  // Lossy when a title itself contains ' > ' — only used for verdict
  // classes whose rerun command is never printed; printed flaky commands
  // come from the failed occurrence's structured-field command.
  assert.equal(
    frontendRerunCommand('src/lib/math.test.ts > math > 2 + 2 should equal 4'),
    "./test.sh --frontend --run 'math 2 \\+ 2 should equal 4'"
  );
});

test('frontendRerunCommand shell-escapes embedded single quotes', () => {
  assert.equal(
    frontendRerunCommand("src/a.test.ts > it's broken"),
    `./test.sh --frontend --run 'it'\\''s broken'`
  );
});

test('frontendRerunCommand passes through an identity with no file segment', () => {
  assert.equal(
    frontendRerunCommand('plain test name'),
    "./test.sh --frontend --run 'plain test name'"
  );
});

test('shellSingleQuote wraps and escapes', () => {
  assert.equal(shellSingleQuote("it's"), `'it'\\''s'`);
  assert.equal(shellSingleQuote('abc'), `'abc'`);
});
