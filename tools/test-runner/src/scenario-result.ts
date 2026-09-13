export type ScenarioRunStatus = 'passed' | 'failed' | 'broken';

/**
 * Classify one isolated Playwright run from parsed evidence plus the container
 * exit code. A parsed failure is a failure. No parsed tests, or a nonzero exit
 * after some tests were parsed, is incomplete evidence — never a pass.
 */
export function classifyScenarioRun(
  passedCount: number,
  failedCount: number,
  exitCode: number
): ScenarioRunStatus {
  if (passedCount === 0 && failedCount === 0) return 'broken';
  if (failedCount > 0) return 'failed';
  if (exitCode !== 0) return 'broken';
  return 'passed';
}

/** Human-readable reason for a `broken` classification. */
export function describeBrokenScenarioRun(
  passedCount: number,
  failedCount: number,
  exitCode: number
): string {
  if (passedCount === 0 && failedCount === 0) {
    return `Scenario runner produced no test results (exit code ${exitCode})`;
  }
  return `Scenario container exited with code ${exitCode} after ${passedCount} passed and ${failedCount} failed tests; results are incomplete`;
}
