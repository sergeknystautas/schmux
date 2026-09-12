import { mkdirSync, readFileSync, writeFileSync } from 'node:fs';
import { resolve } from 'node:path';
import { projectRoot } from './exec.js';
import { classifyVerdict } from './verdicts.js';
import type { FlakyResult, SuiteName } from './types.js';

export interface RepeatReportFinding {
  testName: string;
  verdict: 'flaky' | 'inconsistent';
  passCount: number;
  failCount: number;
  skipCount: number;
  totalRuns: number;
  rerunCommand: string;
}

export interface RepeatReport {
  suite: SuiteName;
  repeat: number;
  generatedAt: string;
  shuffleSeed: number | null;
  vitestVersion: string | null;
  incompleteEvidence: boolean;
  findings: RepeatReportFinding[];
  testCount: number;
}

export function buildRepeatReport(args: {
  suite: SuiteName;
  repeat: number;
  shuffleSeed: number | null;
  vitestVersion: string | null;
  incompleteEvidence: boolean;
  flakyResults: FlakyResult[];
  now?: () => Date;
}): RepeatReport {
  const mine = args.flakyResults.filter((r) => r.suite === args.suite);
  return {
    suite: args.suite,
    repeat: args.repeat,
    generatedAt: (args.now ?? (() => new Date()))().toISOString(),
    shuffleSeed: args.shuffleSeed,
    vitestVersion: args.vitestVersion,
    incompleteEvidence: args.incompleteEvidence,
    findings: mine
      .filter((r) => {
        const verdict = classifyVerdict(r);
        return verdict === 'flaky' || verdict === 'inconsistent';
      })
      .map((r) => {
        const verdict = classifyVerdict(r);
        return {
          testName: r.testName,
          verdict: verdict as 'flaky' | 'inconsistent',
          passCount: r.passCount,
          failCount: r.failCount,
          skipCount: r.skipCount,
          totalRuns: r.totalRuns,
          rerunCommand: r.rerunCommand,
        };
      }),
    testCount: mine.length,
  };
}

function readVitestVersion(): string | null {
  try {
    const pkg = JSON.parse(
      readFileSync(resolve(projectRoot(), 'assets/dashboard/package.json'), 'utf-8')
    );
    return pkg?.devDependencies?.vitest ?? pkg?.dependencies?.vitest ?? null;
  } catch {
    return null;
  }
}

// Write one <suite>-repeat.json per suite that observed tests. Returns the
// written paths (printed by main.ts so local runs can find the report).
export function writeRepeatReports(args: {
  flakyResults: FlakyResult[];
  incompleteSuites: SuiteName[];
  repeat: number;
  shuffleSeed: number | null;
}): string[] {
  const dir = resolve(projectRoot(), '.schmux/test-runner');
  const suites = [...new Set(args.flakyResults.map((r) => r.suite))];
  const written: string[] = [];
  for (const suite of suites) {
    const report = buildRepeatReport({
      suite,
      repeat: args.repeat,
      shuffleSeed: suite === 'frontend' ? args.shuffleSeed : null,
      vitestVersion: suite === 'frontend' ? readVitestVersion() : null,
      incompleteEvidence: args.incompleteSuites.includes(suite),
      flakyResults: args.flakyResults,
    });
    mkdirSync(dir, { recursive: true });
    const path = resolve(dir, `${suite}-repeat.json`);
    writeFileSync(path, `${JSON.stringify(report, null, 2)}\n`);
    written.push(path);
  }
  return written;
}
