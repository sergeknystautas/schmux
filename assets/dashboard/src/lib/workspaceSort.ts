/**
 * Workspace sorting utilities for the sidebar workspace list.
 *
 * Extracted from AppShell so the sort logic can be tested independently
 * (same pattern as sortSessionsByTabOrder in tabOrder.ts).
 */
import type { SessionResponse, WorkspaceResponse } from './types';

type SortMode = 'alpha' | 'time';
type GetRepoName = (repoUrl: string) => string;
const IN_PROGRESS_SORT_PENALTY_MS = 60 * 60 * 1000;

export interface SessionInProgressOptions {
  nudgenikEnabled: boolean;
  isPromptable: boolean;
}

export type SessionInProgressPredicate = (session: SessionResponse) => boolean;

/**
 * The adjusted timestamp for one session: `last_output_at`, minus the in-progress
 * penalty when the session is busy, null when no timestamp is recorded.
 *
 * Both the sidebar workspace time sort and the flights list activity line use
 * the same rule, so they sit in one place.
 */
export function sessionActivityTime(
  session: SessionResponse,
  isInProgress: SessionInProgressPredicate
): number | null {
  if (!session.last_output_at) return null;
  const timestamp = new Date(session.last_output_at).getTime();
  return isInProgress(session) ? timestamp - IN_PROGRESS_SORT_PENALTY_MS : timestamp;
}

/**
 * The greatest adjusted timestamp across a session list, or 0 when none of the
 * sessions has a timestamp. Use this to rank workspaces and started flights by
 * activity.
 */
export function latestActivityTime(
  sessions: SessionResponse[],
  isInProgress: SessionInProgressPredicate
): number {
  let best = 0;
  for (const s of sessions ?? []) {
    const t = sessionActivityTime(s, isInProgress);
    if (t !== null && t > best) best = t;
  }
  return best;
}

/**
 * Whether a session's agent is still busy (an active turn or background tasks).
 * Stopped sessions are never in progress, even with a stale persisted nudge.
 */
export function isSessionInProgress(
  session: SessionResponse,
  options: SessionInProgressOptions
): boolean {
  return (
    session.running &&
    (['Working', 'Background'].includes(session.nudge_state || '') ||
      (options.nudgenikEnabled && !session.nudge_state && options.isPromptable))
  );
}

/**
 * Sort workspaces for the sidebar, accounting for backburner partitioning
 * when the feature is enabled.
 *
 * When `backburnerEnabled` is true, backburnered workspaces are pushed
 * below non-backburnered ones, with the existing sort order preserved
 * within each group.
 *
 * In time mode, timestamps from sessions for which `isInProgress` returns true
 * are ranked as one hour older; alpha mode ignores the predicate.
 */
export function sortWorkspaces(
  workspaces: WorkspaceResponse[],
  mode: SortMode,
  getRepoName: GetRepoName,
  backburnerEnabled: boolean,
  isInProgress: SessionInProgressPredicate
): WorkspaceResponse[] {
  const sorted = [...workspaces];

  if (mode === 'alpha') {
    sorted.sort((a, b) => {
      if (backburnerEnabled) {
        const aBB = a.backburner ? 1 : 0;
        const bBB = b.backburner ? 1 : 0;
        if (aBB !== bBB) return aBB - bBB;
      }
      const repoA = a.repo_name || getRepoName(a.repo);
      const repoB = b.repo_name || getRepoName(b.repo);
      if (repoA !== repoB) return repoA.localeCompare(repoB);
      return a.branch.localeCompare(b.branch);
    });
  } else {
    // Time sort: most recent activity first, with busy sessions aged one hour
    sorted.sort((a, b) => {
      if (backburnerEnabled) {
        const aBB = a.backburner ? 1 : 0;
        const bBB = b.backburner ? 1 : 0;
        if (aBB !== bBB) return aBB - bBB;
      }

      const timeA = latestActivityTime(a.sessions ?? [], isInProgress);
      const timeB = latestActivityTime(b.sessions ?? [], isInProgress);
      // Most recent first; workspaces with no timestamp go to bottom
      if (timeA === 0 && timeB === 0) {
        const repoA = a.repo_name || getRepoName(a.repo);
        const repoB = b.repo_name || getRepoName(b.repo);
        if (repoA !== repoB) return repoA.localeCompare(repoB);
        return a.branch.localeCompare(b.branch);
      }
      if (timeA === 0) return 1;
      if (timeB === 0) return -1;
      if (timeA !== timeB) return timeB - timeA;
      // Equal timestamps: secondary sort alphabetically
      const repoA = a.repo_name || getRepoName(a.repo);
      const repoB = b.repo_name || getRepoName(b.repo);
      if (repoA !== repoB) return repoA.localeCompare(repoB);
      return a.branch.localeCompare(b.branch);
    });
  }

  return sorted;
}
