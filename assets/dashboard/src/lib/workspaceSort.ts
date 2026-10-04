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

      const getTime = (ws: WorkspaceResponse): number => {
        const times =
          ws.sessions
            ?.filter((s) => s.last_output_at)
            .map((s) => {
              const timestamp = new Date(s.last_output_at!).getTime();
              return isInProgress(s) ? timestamp - IN_PROGRESS_SORT_PENALTY_MS : timestamp;
            }) || [];
        return times.length > 0 ? Math.max(...times) : 0;
      };
      const timeA = getTime(a);
      const timeB = getTime(b);
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
