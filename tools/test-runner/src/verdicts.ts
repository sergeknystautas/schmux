import type { FlakyResult } from './types.js';

export type FlakyVerdict = 'flaky' | 'stable' | 'failing' | 'inconsistent' | 'skipped';

// Classify one test's repeat history. Skip counting is frontend-only today
// (backend identities are bare TestXxx names with cross-package duplicates),
// so non-frontend entries carry skipCount === 0 and classify exactly as
// before this change.
export function classifyVerdict(
  r: Pick<FlakyResult, 'passCount' | 'failCount' | 'skipCount'>
): FlakyVerdict {
  if (r.passCount > 0 && r.failCount > 0) return 'flaky';
  if (r.skipCount > 0 && (r.passCount > 0 || r.failCount > 0)) return 'inconsistent';
  if (r.failCount > 0) return 'failing';
  if (r.passCount > 0) return 'stable';
  return 'skipped';
}
