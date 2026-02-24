import chalk from 'chalk';
import type {
  SuiteName,
  SuiteStatus,
  TestEvent,
  SuiteResult,
  FlakyResult,
  FailedTest,
} from './types.js';
import type { CoverageReport, FrontendCoverageReport } from './coverage.js';

const isTTY = process.stdout.isTTY ?? false;

// ─── Formatting helpers ────────────────────────────────────────────────────

function formatDuration(ms: number): string {
  if (ms < 1000) return `${Math.round(ms)}ms`;
  if (ms < 60000) return `${(ms / 1000).toFixed(1)}s`;
  const mins = Math.floor(ms / 60000);
  const secs = Math.round((ms % 60000) / 1000);
  return `${mins}m ${secs.toString().padStart(2, '0')}s`;
}

function statusIcon(status: SuiteStatus): string {
  switch (status) {
    case 'pending':
      return chalk.dim('◌');
    case 'building':
      return chalk.yellow('◔');
    case 'running':
      return chalk.blue('●');
    case 'passed':
      return chalk.green('✓');
    case 'failed':
      return chalk.red('✗');
    case 'broken':
      return chalk.red('⚠');
    case 'skipped':
      return chalk.dim('○');
  }
}

// ─── Table Renderer ────────────────────────────────────────────────────────
// Computes column widths from data, handles ANSI-safe padding.

// Strip ANSI escape codes to get visible character length
function stripAnsi(str: string): string {
  return str.replace(/\x1b\[[0-9;]*m/g, '');
}

function visibleLength(str: string): number {
  return stripAnsi(str).length;
}

// Pad a string that may contain ANSI codes to a target visible width
function padEnd(str: string, width: number): string {
  const visible = visibleLength(str);
  if (visible >= width) return str;
  return str + ' '.repeat(width - visible);
}

function padStart(str: string, width: number): string {
  const visible = visibleLength(str);
  if (visible >= width) return str;
  return ' '.repeat(width - visible) + str;
}

type Align = 'left' | 'right' | 'center';

interface Column {
  header: string;
  align: Align;
  minWidth?: number;
}

interface TableOptions {
  indent?: string;
  columns: Column[];
  rows: string[][];
  separatorAfter?: number[]; // row indices after which to draw a separator
}

function renderTable(opts: TableOptions): string[] {
  const indent = opts.indent ?? '  ';

  // Compute column widths: max of header and all row cells
  const widths = opts.columns.map((col, i) => {
    const headerLen = visibleLength(col.header);
    const maxCell = opts.rows.reduce((max, row) => Math.max(max, visibleLength(row[i] ?? '')), 0);
    const minW = col.minWidth ?? 0;
    return Math.max(headerLen, maxCell, minW) + 2; // +2 for padding on each side
  });

  function formatCell(text: string, colIdx: number): string {
    const w = widths[colIdx];
    const align = opts.columns[colIdx].align;
    // Content gets 1 space padding on each side, so inner width is w-2
    const innerWidth = w - 2;
    switch (align) {
      case 'right':
        return ' ' + padStart(text, innerWidth) + ' ';
      case 'center': {
        const vis = visibleLength(text);
        const totalPad = Math.max(0, innerWidth - vis);
        const left = Math.floor(totalPad / 2);
        const right = totalPad - left;
        return ' ' + ' '.repeat(left) + text + ' '.repeat(right) + ' ';
      }
      case 'left':
      default:
        return ' ' + padEnd(text, innerWidth) + ' ';
    }
  }

  const lines: string[] = [];

  // Top border
  lines.push(indent + '┌' + widths.map((w) => '─'.repeat(w)).join('┬') + '┐');

  // Header row
  const headerCells = opts.columns.map((col, i) => formatCell(col.header, i));
  lines.push(indent + '│' + headerCells.join('│') + '│');

  // Header separator
  lines.push(indent + '├' + widths.map((w) => '─'.repeat(w)).join('┼') + '┤');

  // Data rows
  const separatorSet = new Set(opts.separatorAfter ?? []);
  for (let r = 0; r < opts.rows.length; r++) {
    const row = opts.rows[r];
    const cells = row.map((cell, i) => formatCell(cell, i));
    lines.push(indent + '│' + cells.join('│') + '│');

    if (separatorSet.has(r) && r < opts.rows.length - 1) {
      lines.push(indent + '├' + widths.map((w) => '─'.repeat(w)).join('┼') + '┤');
    }
  }

  // Bottom border
  lines.push(indent + '└' + widths.map((w) => '─'.repeat(w)).join('┴') + '┘');

  return lines;
}

// ─── Progress Display Interface ────────────────────────────────────────────

export interface ProgressDisplay {
  onEvent(suite: SuiteName, event: TestEvent): void;
  finish(result: SuiteResult): void;
  stop(): void;
}

// ─── Serial Progress Display ───────────────────────────────────────────────

class SerialDisplay implements ProgressDisplay {
  private passed = 0;
  private failed = 0;
  private startTime = Date.now();

  onEvent(_suite: SuiteName, event: TestEvent): void {
    switch (event.type) {
      case 'test_pass':
        this.passed++;
        this.writeCounter();
        break;
      case 'test_fail':
        this.failed++;
        // Print failure immediately with full detail
        this.clearCounter();
        console.log('');
        console.log(
          chalk.red(`  ✗ FAIL: ${event.name}`) + chalk.dim(` (${formatDuration(event.durationMs)})`)
        );
        if (event.output) {
          for (const line of event.output.split('\n').slice(0, 30)) {
            console.log(chalk.dim(`    ${line}`));
          }
        }
        console.log('');
        this.writeCounter();
        break;
      case 'build_step':
        this.clearCounter();
        console.log(chalk.blue(`  ▸ ${event.message}`));
        break;
      case 'suite_status':
        this.clearCounter();
        if (event.status === 'passed') {
          console.log(chalk.green(`  ✓ ${event.message}`));
        } else if (event.status === 'failed' || event.status === 'broken') {
          console.log(chalk.red(`  ✗ ${event.message}`));
        } else {
          console.log(chalk.dim(`  ${statusIcon(event.status)} ${event.message}`));
        }
        break;
      case 'output_line':
        console.log(chalk.dim(`    ${event.line}`));
        break;
    }
  }

  finish(_result: SuiteResult): void {
    this.clearCounter();
  }

  stop(): void {
    this.clearCounter();
  }

  private writeCounter(): void {
    if (!isTTY) return;
    const elapsed = formatDuration(Date.now() - this.startTime);
    const msg = `  ✓ ${this.passed} passed${this.failed > 0 ? chalk.red(`, ${this.failed} failed`) : ''}  (${elapsed})`;
    process.stdout.write(`\r${msg}\x1b[K`);
  }

  private clearCounter(): void {
    if (!isTTY) return;
    process.stdout.write('\r\x1b[K');
  }
}

// ─── Parallel Progress Display ─────────────────────────────────────────────

interface SuiteState {
  status: SuiteStatus;
  passed: number;
  failed: number;
  elapsed: number;
  lastActivity: string;
}

class ParallelDisplay implements ProgressDisplay {
  private suiteStates: Map<SuiteName, SuiteState>;
  private tickInterval: ReturnType<typeof setInterval> | null = null;
  private startTimes = new Map<SuiteName, number>();
  private dashboardLines = 0;
  private started = false;

  constructor(suites: SuiteName[]) {
    this.suiteStates = new Map();
    const now = Date.now();
    for (const s of suites) {
      this.suiteStates.set(s, {
        status: 'pending',
        passed: 0,
        failed: 0,
        elapsed: 0,
        lastActivity: '',
      });
      this.startTimes.set(s, now);
    }

    if (isTTY) {
      this.tickInterval = setInterval(() => this.render(), 500);
    }
  }

  onEvent(suite: SuiteName, event: TestEvent): void {
    const state = this.suiteStates.get(suite);
    if (!state) return;

    state.elapsed = Date.now() - (this.startTimes.get(suite) ?? Date.now());

    switch (event.type) {
      case 'test_pass':
        state.passed++;
        state.status = 'running';
        state.lastActivity = `${state.passed}/${state.passed + state.failed} passed`;
        break;
      case 'test_fail':
        state.failed++;
        state.status = 'running';
        state.lastActivity = event.name;
        // Print failure immediately below dashboard
        if (isTTY) this.eraseDashboard();
        console.log('');
        console.log(
          chalk.red(`  ✗ [${suite}] FAIL: ${event.name}`) +
            chalk.dim(` (${formatDuration(event.durationMs)})`)
        );
        if (event.output) {
          for (const line of event.output.split('\n').slice(0, 20)) {
            console.log(chalk.dim(`    ${line}`));
          }
        }
        console.log('');
        if (isTTY) this.render();
        break;
      case 'build_step':
        state.status = 'building';
        state.lastActivity = event.message;
        break;
      case 'suite_status':
        if (
          event.status === 'running' ||
          event.status === 'building' ||
          event.status === 'broken'
        ) {
          state.status = event.status;
        }
        state.lastActivity = event.message;
        break;
      case 'output_line':
        // Suppress in parallel mode unless it's a failure
        break;
    }

    if (!isTTY) {
      // Non-TTY fallback: one line per significant event
      if (event.type === 'test_fail') {
        // Already printed above
      } else if (event.type === 'build_step') {
        console.log(`  [${suite}] ${event.message}`);
      }
    } else if (this.started) {
      this.render();
    }
  }

  finish(result: SuiteResult): void {
    const state = this.suiteStates.get(result.suite);
    if (!state) return;

    state.status =
      result.status === 'passed' ? 'passed' : result.status === 'broken' ? 'broken' : 'failed';
    state.elapsed = result.durationMs;
    state.passed = result.passedTests.length;
    state.failed = result.failedTests.length;

    const counts = `${state.passed} passed${state.failed > 0 ? `, ${state.failed} failed` : ''}`;
    state.lastActivity = counts;

    if (isTTY) {
      this.render();
    } else {
      const icon = result.status === 'passed' ? '✓' : result.status === 'broken' ? '⚠' : '✗';
      console.log(
        `  ${icon} [${result.suite}] ${result.status} — ${counts} (${formatDuration(result.durationMs)})`
      );
    }
  }

  stop(): void {
    if (this.tickInterval) {
      clearInterval(this.tickInterval);
      this.tickInterval = null;
    }
    this.eraseDashboard();
  }

  private render(): void {
    this.started = true;
    if (!isTTY) return;

    this.eraseDashboard();

    const rows: string[][] = [];
    for (const [name, state] of this.suiteStates) {
      if (state.status === 'running' || state.status === 'building') {
        state.elapsed = Date.now() - (this.startTimes.get(name) ?? Date.now());
      }

      const statusStr = `${statusIcon(state.status)} ${state.status}`;
      const activity = state.lastActivity.slice(0, 70);

      rows.push([capitalize(name), statusStr, formatDuration(state.elapsed), activity]);
    }

    const lines = renderTable({
      columns: [
        { header: 'Suite', align: 'left' },
        { header: 'Status', align: 'left' },
        { header: 'Duration', align: 'right' },
        { header: 'Activity', align: 'left', minWidth: 20 },
      ],
      rows,
    });

    for (const line of lines) {
      console.log(line);
    }
    this.dashboardLines = lines.length;
  }

  private eraseDashboard(): void {
    if (!isTTY || this.dashboardLines === 0) return;
    // Move up and clear each line
    for (let i = 0; i < this.dashboardLines; i++) {
      process.stdout.write('\x1b[A\x1b[2K');
    }
    this.dashboardLines = 0;
  }
}

// ─── Factory ───────────────────────────────────────────────────────────────

export function createProgressDisplay(suites: SuiteName[], parallel: boolean): ProgressDisplay {
  if (parallel && suites.length > 1) {
    return new ParallelDisplay(suites);
  }
  return new SerialDisplay();
}

// ─── Summary Table ─────────────────────────────────────────────────────────

// Deduplicate test counts for repeat mode. With -count=N, each test name
// appears N times. A test is "passed" if it only appears in passedTests,
// "failed" if only in failedTests, "flaky" if in both.
function deduplicateTestCounts(r: SuiteResult): { passed: number; failed: number; flaky: number } {
  const passedNames = new Set(r.passedTests);
  const failedNames = new Set(r.failedTests.map((f) => f.name));
  const flakyNames = [...passedNames].filter((n) => failedNames.has(n));

  return {
    passed: passedNames.size - flakyNames.length,
    failed: failedNames.size - flakyNames.length,
    flaky: flakyNames.length,
  };
}

export function printSummary(results: SuiteResult[], parallel = false, repeat = 1): void {
  console.log('');

  // Sort: broken/failures first
  const statusPriority = (s: string) => (s === 'broken' ? 0 : s === 'failed' ? 1 : 2);
  const sorted = [...results].sort((a, b) => statusPriority(a.status) - statusPriority(b.status));

  const isRepeat = repeat > 1;

  // Build table rows
  let totalPassed = 0;
  let totalFailed = 0;
  let totalFlaky = 0;
  let totalDurationMax = 0;
  let totalDurationSum = 0;

  const rows: string[][] = [];
  for (const r of sorted) {
    const icon =
      r.status === 'passed'
        ? chalk.green('✓')
        : r.status === 'broken'
          ? chalk.red('⚠')
          : r.status === 'failed'
            ? chalk.red('✗')
            : chalk.dim('○');

    totalDurationSum += r.durationMs;
    totalDurationMax = Math.max(totalDurationMax, r.durationMs);

    let tests: string;
    if (r.status === 'broken') {
      tests = chalk.red('broken');
    } else if (isRepeat) {
      const counts = deduplicateTestCounts(r);
      totalPassed += counts.passed;
      totalFailed += counts.failed;
      totalFlaky += counts.flaky;

      tests = `${counts.passed} passed`;
      if (counts.failed > 0) tests += chalk.red(`, ${counts.failed} failed`);
      if (counts.flaky > 0) tests += chalk.yellow(`, ${counts.flaky} flaky`);
    } else {
      const p = r.passedTests.length;
      const f = r.failedTests.length;
      totalPassed += p;
      totalFailed += f;

      tests = `${p} passed`;
      if (f > 0) tests += chalk.red(`, ${f} failed`);
    }

    rows.push([capitalize(r.suite), icon, formatDuration(r.durationMs), tests]);
  }

  // Total row
  const totalDuration = parallel ? totalDurationMax : totalDurationSum;
  let totalTests = `${totalPassed} passed`;
  if (totalFailed > 0) totalTests += chalk.red(`, ${totalFailed} failed`);
  if (totalFlaky > 0) totalTests += chalk.yellow(`, ${totalFlaky} flaky`);
  rows.push(['Total', '', formatDuration(totalDuration), totalTests]);

  const lines = renderTable({
    columns: [
      { header: 'Suite', align: 'left' },
      { header: 'Status', align: 'center' },
      { header: 'Duration', align: 'right' },
      { header: 'Tests', align: 'left' },
    ],
    rows,
    separatorAfter: [rows.length - 2], // separator before total row
  });

  for (const line of lines) {
    console.log(line);
  }

  // Print build errors for suites that are broken (build failed, no tests ran)
  const buildFailures = sorted.filter((r) => r.status === 'broken' && r.output);
  for (const r of buildFailures) {
    console.log('');
    console.log(chalk.red(`  ${capitalize(r.suite)} build broken:`));
    const lines = r.output.trim().split('\n').slice(-20);
    for (const line of lines) {
      console.log(chalk.dim(`    ${line}`));
    }
  }

  // Print failed test rerun commands (skip in repeat mode — flaky report covers it)
  const allFailed: (FailedTest & { suite: SuiteName })[] = [];
  if (!isRepeat) {
    const seenFailed = new Set<string>();
    for (const r of sorted) {
      for (const f of r.failedTests) {
        const key = `${r.suite}::${f.name}`;
        if (!seenFailed.has(key)) {
          seenFailed.add(key);
          allFailed.push({ ...f, suite: r.suite });
        }
      }
    }

    if (allFailed.length > 0) {
      console.log('');
      console.log(chalk.red('  Failed tests:'));
      for (const f of allFailed) {
        console.log(chalk.dim(`    ${f.suite} > `) + f.name);
        console.log(chalk.dim(`      rerun: ${f.rerunCommand}`));
      }
    }
  }

  // Print slowest 20 tests across all suites (only when all passed)
  const hasFailures = sorted.some((r) => r.failedTests.length > 0);
  if (!hasFailures) {
    const allTests: { suite: string; name: string; durationMs: number }[] = [];
    for (const r of sorted) {
      for (const [name, durationMs] of Object.entries(r.testDurations)) {
        allTests.push({ suite: r.suite, name, durationMs });
      }
    }
    allTests.sort((a, b) => b.durationMs - a.durationMs);
    const slowest = allTests.slice(0, 20);

    if (slowest.length > 0) {
      console.log('');
      console.log(chalk.yellow('  Slowest tests:'));

      const slowRows = slowest.map((s) => [
        s.suite,
        s.name,
        chalk.yellow(formatDuration(s.durationMs)),
      ]);

      const slowLines = renderTable({
        columns: [
          { header: 'Suite', align: 'left' },
          { header: 'Test', align: 'left' },
          { header: 'Duration', align: 'right' },
        ],
        rows: slowRows,
        indent: '    ',
      });

      for (const line of slowLines) {
        console.log(line);
      }
    }
  }

  console.log('');
}

// ─── Flaky Report ──────────────────────────────────────────────────────────

export function printFlakyReport(flakyResults: FlakyResult[], totalRuns: number): void {
  // Only show tests with mixed results
  const mixed = flakyResults
    .filter((r) => r.passCount > 0 && r.failCount > 0)
    .sort((a, b) => b.flakyScore - a.flakyScore);

  const stable = flakyResults.filter((r) => r.failCount === 0);

  console.log(chalk.yellow(`  Flaky Test Report (${totalRuns} runs)`));

  if (mixed.length === 0) {
    console.log('  No flaky tests detected. All tests were consistent.');
    console.log(`  Stable: ${stable.length} tests passed all ${totalRuns} runs`);
  } else {
    const rows = mixed.map((r) => {
      // Build visual pass/fail history
      let history = '';
      for (let i = 0; i < r.totalRuns; i++) {
        if (i < r.failCount) {
          history += chalk.red('✗');
        } else {
          history += chalk.green('✓');
        }
      }

      return [`${r.suite} > ${r.testName}`, `${Math.round(r.flakyScore * 100)}%`, history];
    });

    const lines = renderTable({
      columns: [
        { header: 'Test', align: 'left' },
        { header: 'Score', align: 'right' },
        { header: 'Results', align: 'left' },
      ],
      rows,
    });

    for (const line of lines) {
      console.log(line);
    }

    console.log('');
    console.log(chalk.yellow('  Reproduce:'));
    for (const r of mixed) {
      console.log(`    ${r.rerunCommand}`);
    }

    console.log('');
    console.log(`  Stable: ${stable.length} tests passed all ${totalRuns} runs`);
  }

  console.log('');
}

// ─── Header ────────────────────────────────────────────────────────────────

export function printHeader(): void {
  console.log('');
  console.log(chalk.white(' Schmux Test Runner'));
  console.log('');
}

// ─── Final Banner ──────────────────────────────────────────────────────────

export function printFinalBanner(allPassed: boolean, hasBroken = false): void {
  if (allPassed) {
    console.log(chalk.green(' All tests passed!'));
  } else if (hasBroken) {
    console.log(chalk.red(' Build broken — tests could not run!'));
  } else {
    console.log(chalk.red(' Some tests failed!'));
  }
}

// ─── Coverage Report ────────────────────────────────────────────────────────

function coverageColor(pct: number): (s: string) => string {
  if (pct < 30) return chalk.red;
  if (pct < 60) return chalk.yellow;
  return chalk.green;
}

function formatNumber(n: number): string {
  return n.toLocaleString('en-US');
}

export function printCoverageReport(report: CoverageReport, label: string): void {
  console.log('');
  console.log(chalk.white(`  Coverage Report (${capitalize(label)})`));
  console.log('');

  const colorFn = coverageColor(report.totalCoverage);
  console.log(`  Total: ${colorFn(`${report.totalCoverage.toFixed(1)}%`)} statements`);
  console.log('');

  // Filter out packages with 0 funcs
  const pkgs = report.packages.filter((p) => p.funcCount > 0);
  if (pkgs.length === 0) return;

  // Build rows
  const rows: string[][] = pkgs.map((p) => [
    p.name,
    coverageColor(p.avgCoverage)(`${p.avgCoverage.toFixed(1)}%`),
    formatNumber(p.funcCount),
    formatNumber(p.uncoveredCount),
    formatNumber(p.loc),
  ]);

  // Total row
  const totalFuncs = pkgs.reduce((s, p) => s + p.funcCount, 0);
  const totalUncovered = pkgs.reduce((s, p) => s + p.uncoveredCount, 0);
  const totalLoc = pkgs.reduce((s, p) => s + p.loc, 0);
  rows.push([
    `Total (${pkgs.length} packages)`,
    colorFn(`${report.totalCoverage.toFixed(1)}%`),
    formatNumber(totalFuncs),
    formatNumber(totalUncovered),
    formatNumber(totalLoc),
  ]);

  const lines = renderTable({
    columns: [
      { header: 'Package', align: 'left' },
      { header: 'Coverage', align: 'right' },
      { header: 'Funcs', align: 'right' },
      { header: 'Uncovered', align: 'right' },
      { header: 'LoC', align: 'right' },
    ],
    rows,
    separatorAfter: [rows.length - 2],
  });

  for (const line of lines) {
    console.log(line);
  }

  // Weakest areas: packages under 40% with >200 LoC
  const weakPkgs = pkgs
    .filter((p) => p.avgCoverage < 40 && p.loc > 200)
    .sort((a, b) => a.avgCoverage - b.avgCoverage)
    .slice(0, 3);

  if (weakPkgs.length > 0 && report.uncoveredFunctions.length > 0) {
    console.log('');
    console.log(chalk.dim('  Weakest areas (0% coverage, most impactful):'));

    for (const pkg of weakPkgs) {
      const funcs = report.uncoveredFunctions.filter((f) => f.pkg === pkg.name).slice(0, 5);
      if (funcs.length === 0) continue;

      console.log(chalk.yellow(`    ${pkg.name}`));
      for (const f of funcs) {
        console.log(chalk.dim(`      ${f.file}:${f.line}`) + `         ${f.funcName}`);
      }
    }
  }
}

export function printFrontendCoverageReport(report: FrontendCoverageReport): void {
  console.log('');
  console.log(chalk.white('  Coverage Report (Frontend)'));
  console.log('');

  const colorFn = coverageColor(report.totalCoverage);
  console.log(`  Total: ${colorFn(`${report.totalCoverage.toFixed(1)}%`)} statements`);
  console.log('');

  if (report.directories.length === 0) return;

  const rows: string[][] = report.directories.map((d) => [
    d.name,
    coverageColor(d.stmtsCoverage)(`${d.stmtsCoverage.toFixed(1)}%`),
    coverageColor(d.funcsCoverage)(`${d.funcsCoverage.toFixed(1)}%`),
  ]);

  const lines = renderTable({
    columns: [
      { header: 'Directory', align: 'left' },
      { header: 'Stmts', align: 'right' },
      { header: 'Funcs', align: 'right' },
    ],
    rows,
  });

  for (const line of lines) {
    console.log(line);
  }
}

// ─── Helpers ───────────────────────────────────────────────────────────────

function capitalize(s: string): string {
  return s.charAt(0).toUpperCase() + s.slice(1);
}
