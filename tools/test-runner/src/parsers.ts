import { relative, sep } from 'node:path';
import type { TestEvent, FailedTest, VitestRunDetail } from './types.js';

// Strip ANSI escape codes from a string
function stripAnsi(str: string): string {
  return str.replace(/\x1b\[[0-9;]*m/g, '');
}

// Strip Go module prefix (e.g. "github.com/user/repo/") to show only the local package path
function stripModulePrefix(pkg: string): string {
  return pkg.replace(/^[a-zA-Z0-9.-]+\.[a-z]+\/[^/]+\/[^/]+\//, '');
}

// Parse a single line of `go test` output into a TestEvent (or null if not a test line).
// Recognizes: "--- PASS:", "--- FAIL:", "=== RUN", "ok <pkg>", "FAIL <pkg>"
export function parseGoTestLine(line: string, slowThreshold: number): TestEvent | null {
  // --- PASS: TestName (1.23s)
  const passMatch = line.match(/^--- PASS: (\S+) \((\d+\.\d+)s\)/);
  if (passMatch) {
    const durationMs = parseFloat(passMatch[2]) * 1000;
    return { type: 'test_pass', name: passMatch[1], durationMs };
  }

  // --- FAIL: TestName (1.23s)
  const failMatch = line.match(/^--- FAIL: (\S+) \((\d+\.\d+)s\)/);
  if (failMatch) {
    const durationMs = parseFloat(failMatch[2]) * 1000;
    return { type: 'test_fail', name: failMatch[1], durationMs, output: '' };
  }

  // --- SKIP: TestName (0.00s)
  const skipMatch = line.match(/^--- SKIP: (\S+)/);
  if (skipMatch) {
    return { type: 'test_skip', name: skipMatch[1] };
  }

  // ok  	github.com/foo/bar/pkg	1.234s
  const okMatch = line.match(/^ok\s+(\S+)\s+(\d+\.\d+)s/);
  if (okMatch) {
    return {
      type: 'suite_status',
      status: 'passed',
      message: `${stripModulePrefix(okMatch[1])} (${okMatch[2]}s)`,
    };
  }

  // FAIL	github.com/foo/bar/pkg	1.234s
  const failPkgMatch = line.match(/^FAIL\s+(\S+)\s+(\d+\.\d+)s/);
  if (failPkgMatch) {
    return {
      type: 'suite_status',
      status: 'failed',
      message: `${stripModulePrefix(failPkgMatch[1])} (${failPkgMatch[2]}s)`,
    };
  }

  return null;
}

// Accumulate go test failure output. Call with each line; it buffers output
// between "=== RUN" / "--- FAIL:" boundaries and attaches it to FailedTest entries.
export class GoTestOutputAccumulator {
  private currentTest: string | null = null;
  private currentOutput: string[] = [];
  private failedOutputs = new Map<string, string>();

  feedLine(line: string): void {
    // === RUN   TestName or === RUN   TestName/SubTest
    const runMatch = line.match(/^=== RUN\s+(\S+)/);
    if (runMatch) {
      this.flush();
      this.currentTest = runMatch[1];
      this.currentOutput = [];
      return;
    }

    // --- FAIL: ends the current test
    const failMatch = line.match(/^--- FAIL: (\S+)/);
    if (failMatch) {
      // Use the name from --- FAIL: as canonical (handles subtests)
      const name = failMatch[1];
      this.currentOutput.push(line);
      this.failedOutputs.set(name, this.currentOutput.join('\n'));
      this.currentTest = null;
      this.currentOutput = [];
      return;
    }

    // --- PASS: ends the current test
    if (line.match(/^--- PASS:/)) {
      this.currentTest = null;
      this.currentOutput = [];
      return;
    }

    // Accumulate lines for current test
    if (this.currentTest !== null) {
      this.currentOutput.push(line);
    }
  }

  private flush(): void {
    // Nothing to flush for pass — only care about failures
  }

  getFailureOutput(testName: string): string {
    return this.failedOutputs.get(testName) ?? '';
  }
}

// ─── Frontend (Vitest JSON) helpers ───────────────────────────────────────

// Wrap a string in single quotes for safe shell interpolation.
export function shellSingleQuote(s: string): string {
  return `'${s.replace(/'/g, `'\\''`)}'`;
}

// Build the -t pattern Vitest matches against: the ancestors and title
// space-joined, regex-escaped. Verified against Vitest 4.1.8
// (@vitest/runner chunk-artifact.js: getTaskFullName joins suite names and
// the title with single spaces, and -t is an unanchored RegExp). Must be
// built from these structured fields — a title may itself contain ' > '
// (10 dashboard titles do), which collapsing separators in a pre-joined
// identity string would silently eat.
export function vitestNamePattern(ancestors: string[], title: string): string {
  const fullName = [...ancestors, title].join(' ');
  return fullName.replace(/[.*+?^${}()|[\]\\]/g, '\\$&');
}

// Wrap an already-built -t pattern in the shell command that re-runs it.
export function frontendRunCommand(pattern: string): string {
  return `./test.sh --frontend --run ${shellSingleQuote(pattern)}`;
}

// Lossy fallback deriving a rerun command from a file-qualified identity
// (`file > ancestors > title`): strips the file segment and collapses
// ` > ` to a space. Correct only when no ancestor or title contains a
// literal ` > `. Only for verdict classes whose rerun command is never
// printed (stable, skipped); printed flaky commands come from the failed
// occurrence's structured-field command.
export function frontendRerunCommand(identity: string): string {
  const idx = identity.indexOf(' > ');
  const displayName = idx === -1 ? identity : identity.slice(idx + 3);
  const pattern = displayName.replace(/ > /g, ' ');
  const regexEscaped = pattern.replace(/[.*+?^${}()|[\]\\]/g, '\\$&');
  return `./test.sh --frontend --run ${shellSingleQuote(regexEscaped)}`;
}

// Parse a single line of Playwright output into a TestEvent (or null).
// Recognizes: "✓  N description", "✗  N description", summary lines
export function parsePlaywrightLine(line: string): TestEvent | null {
  const clean = stripAnsi(line);
  // Playwright formats vary. Common patterns:
  //   ✓  1 [chromium] › file.spec.ts:10:5 › description (5.2s)
  //   ✗  2 [chromium] › file.spec.ts:10:5 › description (5.2s)
  //   -  3 [chromium] › file.spec.ts:10:5 › description (skipped)
  const passMatch = clean.match(/\s*[✓✔]\s+\d+\s+(.+?)(?:\((\d+(?:\.\d+)?)s\))?\s*$/);
  if (passMatch) {
    const durationMs = passMatch[2] ? parseFloat(passMatch[2]) * 1000 : 0;
    return { type: 'test_pass', name: passMatch[1].trim(), durationMs };
  }

  const failMatch = clean.match(/\s*[✗×✘❌]\s+\d+\s+(.+?)(?:\((\d+(?:\.\d+)?)s\))?\s*$/);
  if (failMatch) {
    const durationMs = failMatch[2] ? parseFloat(failMatch[2]) * 1000 : 0;
    return { type: 'test_fail', name: failMatch[1].trim(), durationMs, output: '' };
  }

  const skipMatch = clean.match(/\s*-\s+\d+\s+(.+?)\(skipped\)\s*$/);
  if (skipMatch) {
    return { type: 'test_skip', name: skipMatch[1].trim() };
  }

  // Summary: "  N passed", "  N failed"
  const summaryMatch = clean.match(/^\s*(\d+) (passed|failed)/);
  if (summaryMatch) {
    return {
      type: 'suite_status',
      status: summaryMatch[2] === 'passed' ? 'passed' : 'failed',
      message: clean.trim(),
    };
  }

  return null;
}

// ─── Vitest JSON ingestion ────────────────────────────────────────────────

// Minimal structural types for Vitest's Jest-compatible JSON report.
interface VitestAssertion {
  ancestorTitles?: string[];
  title?: string;
  status?: string;
  duration?: number;
  failureMessages?: string[];
}

interface VitestFileResult {
  name?: string;
  status?: string;
  message?: string;
  assertionResults?: VitestAssertion[];
}

interface VitestJsonReport {
  numTotalTests?: number;
  success?: boolean;
  testResults?: VitestFileResult[];
}

function vitestRelativeFile(filePath: string, dashboardDir: string): string {
  return relative(dashboardDir, filePath).split(sep).join('/');
}

// Parse one Vitest JSON-reporter document into per-test results.
// Test identity: `file > ancestors > title` (file relative to the dashboard).
export function parseVitestJson(doc: unknown, dashboardDir: string): VitestRunDetail {
  const report = doc as VitestJsonReport;
  const passedTests: string[] = [];
  const failedTests: FailedTest[] = [];
  const skippedTests: string[] = [];
  const testDurations: Record<string, number> = {};
  let parsed = 0;
  let passed = 0;
  let failed = 0;
  let skipped = 0;

  for (const file of report.testResults ?? []) {
    const assertions = file.assertionResults ?? [];
    const relFile = vitestRelativeFile(file.name ?? '<unknown>', dashboardDir);

    for (const a of assertions) {
      parsed++;
      const ancestors = (a.ancestorTitles ?? []).filter((t) => t.length > 0);
      const identity = `${relFile} > ${[...ancestors, a.title ?? ''].join(' > ')}`;
      if (typeof a.duration === 'number') {
        testDurations[identity] = Math.max(testDurations[identity] ?? 0, a.duration);
      }
      if (a.status === 'passed') {
        passedTests.push(identity);
        passed++;
      } else if (a.status === 'failed') {
        failedTests.push({
          name: identity,
          output: (a.failureMessages ?? []).join('\n'),
          rerunCommand: frontendRunCommand(vitestNamePattern(ancestors, a.title ?? '')),
        });
        failed++;
      } else {
        // skipped, todo, pending
        skippedTests.push(identity);
        skipped++;
      }
    }

    // File-level load failure: the file failed with zero assertions.
    // A project test failure — not a harness failure.
    if (file.status === 'failed' && (file.message ?? '').trim() !== '' && assertions.length === 0) {
      failedTests.push({
        name: `${relFile} (failed to load)`,
        output: file.message ?? '',
        // File path, not a test name: --run passes .test.ts patterns to
        // Vitest as positional file filters (see suites/frontend.ts).
        rerunCommand: `./test.sh --frontend --run ${shellSingleQuote(relFile)}`,
      });
      failed++;
    }
  }

  return {
    passedTests,
    failedTests,
    skippedTests,
    testDurations,
    integrityOk: parsed === (report.numTotalTests ?? parsed),
    success: report.success ?? false,
    totals: { passed, failed, skipped, total: report.numTotalTests ?? parsed },
  };
}

// ─── Vitest iteration aggregation ─────────────────────────────────────────

// One executed vitest process. detail === null means the JSON output file
// was missing or unparseable — the iteration produced no trustworthy evidence.
export interface VitestIteration {
  exitCode: number;
  detail: VitestRunDetail | null;
  index: number;
}

export type { VitestRunDetail };

// Concatenate per-iteration results into suite-level lists. Each identity
// appears once per iteration, which is what the flaky occurrence-counting
// in runner.ts relies on.
export function combineVitestRuns(details: VitestRunDetail[]): {
  passedTests: string[];
  failedTests: FailedTest[];
  skippedTests: string[];
  testDurations: Record<string, number>;
} {
  const passedTests: string[] = [];
  const failedTests: FailedTest[] = [];
  const skippedTests: string[] = [];
  const testDurations: Record<string, number> = {};
  for (const d of details) {
    passedTests.push(...d.passedTests);
    failedTests.push(...d.failedTests);
    skippedTests.push(...d.skippedTests);
    for (const [name, ms] of Object.entries(d.testDurations)) {
      testDurations[name] = Math.max(testDurations[name] ?? 0, ms);
    }
  }
  return { passedTests, failedTests, skippedTests, testDurations };
}

// Decide the suite status from the iteration evidence.
// broken = harness/bootstrap failure (missing JSON, integrity mismatch,
// exit/success disagreement). failed = project tests failed. passed = green.
// namePattern is the --run pattern when one was given: Vitest exits 0 when
// test files run but the pattern selects nothing runnable (filtered-out
// tests are reported as skipped assertions), so a pattern under which no
// test executed is classified failed rather than reported green.
export function classifyVitestSuiteStatus(
  runs: VitestIteration[],
  namePattern?: string | null
): {
  status: 'passed' | 'failed' | 'broken';
  reason: string;
} {
  for (const run of runs) {
    if (run.detail === null) {
      return {
        status: 'broken',
        reason: `iteration ${run.index}: JSON output missing or unparseable`,
      };
    }
    if (!run.detail.integrityOk) {
      return {
        status: 'broken',
        reason: `iteration ${run.index}: parsed assertions do not match numTotalTests`,
      };
    }
    if (run.detail.success !== (run.exitCode === 0)) {
      return {
        status: 'broken',
        reason: `iteration ${run.index}: exit code ${run.exitCode} disagrees with JSON success flag`,
      };
    }
  }
  if (
    namePattern &&
    runs.every(
      (r) => (r.detail?.passedTests.length ?? 0) + (r.detail?.failedTests.length ?? 0) === 0
    )
  ) {
    return {
      status: 'failed',
      reason: `--run pattern '${namePattern}' selected no tests to run`,
    };
  }
  const anyFail = runs.some(
    (r) =>
      r.exitCode !== 0 ||
      (r.detail && (r.detail.totals.failed > 0 || r.detail.failedTests.length > 0))
  );
  return { status: anyFail ? 'failed' : 'passed', reason: '' };
}
