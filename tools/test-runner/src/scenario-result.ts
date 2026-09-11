export type ScenarioRunStatus = 'passed' | 'failed' | 'broken';

/** Classify one isolated Playwright run from parsed evidence. */
export function classifyScenarioRun(passedCount: number, failedCount: number): ScenarioRunStatus {
  if (passedCount === 0 && failedCount === 0) return 'broken';
  if (failedCount > 0) return 'failed';
  return 'passed';
}
