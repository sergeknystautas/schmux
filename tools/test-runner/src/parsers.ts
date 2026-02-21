import type { TestEvent } from './types.js';

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

// Parse a single line of vitest output into a TestEvent (or null).
// Vitest output contains ANSI escape codes, so we strip them first.
// Actual format: " ✓ src/lib/csrf.test.ts (7 tests) 6ms"
export function parseVitestLine(line: string): TestEvent | null {
  const clean = stripAnsi(line);

  // ✓ src/components/Foo.test.tsx (N tests) 123ms
  const passMatch = clean.match(/\s*✓\s+(.+?)(?:\s+\((\d+)\s+tests?\))?\s*(\d+)ms\s*$/);
  if (passMatch) {
    const durationMs = passMatch[3] ? parseInt(passMatch[3], 10) : 0;
    const testCount = passMatch[2] ? parseInt(passMatch[2], 10) : 1;
    return { type: 'test_pass', name: passMatch[1].trim(), durationMs, pkg: `${testCount} tests` };
  }

  // × src/components/Foo.test.tsx (N tests) 123ms
  const failMatch = clean.match(/\s*[×✗]\s+(.+?)(?:\s+\((\d+)\s+tests?\))?\s*(\d+)ms\s*$/);
  if (failMatch) {
    const durationMs = failMatch[3] ? parseInt(failMatch[3], 10) : 0;
    return { type: 'test_fail', name: failMatch[1].trim(), durationMs, output: '' };
  }

  // ↓ src/components/Foo.test.tsx > skipped test [skipped]
  const skipMatch = clean.match(/\s*[↓⊘]\s+(.+?)(?:\s+\[skipped\])?\s*$/);
  if (skipMatch) {
    return { type: 'test_skip', name: skipMatch[1].trim() };
  }

  // Test Files  20 passed (20)
  const testFilesMatch = clean.match(/Test Files\s+(\d+)\s+(passed|failed)/);
  if (testFilesMatch) {
    return {
      type: 'suite_status',
      status: testFilesMatch[2] === 'passed' ? 'passed' : 'failed',
      message: clean.trim(),
    };
  }

  // Tests  203 passed (203)  — extract total test count
  const testsMatch = clean.match(/^\s*Tests\s+(\d+)\s+(passed|failed)/);
  if (testsMatch) {
    return {
      type: 'suite_status',
      status: testsMatch[2] === 'passed' ? 'passed' : 'failed',
      message: clean.trim(),
    };
  }

  return null;
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
