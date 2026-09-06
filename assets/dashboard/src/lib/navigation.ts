import { useNavigate } from 'react-router';
import { useSessions } from '../contexts/SessionsContext';
import type { WorkspaceResponse, PendingNavigation } from './types';
import { sortSessionsByTabOrder } from './tabOrder';

/**
 * Navigate to the appropriate page for a workspace based on its state:
 * - If workspace has sessions -> navigate to first session
 * - If no sessions but has git changes -> navigate to diff page
 * - Otherwise -> navigate to spawn page with workspace_id
 */
export function navigateToWorkspace(
  navigate: ReturnType<typeof useNavigate>,
  workspaces: WorkspaceResponse[],
  workspaceId: string
): void {
  const workspace = workspaces.find((ws) => ws.id === workspaceId);

  // An in-progress sync conflict takes priority: route to its id-named tab.
  const activeConflict = workspace?.resolve_conflicts?.find(
    (cr) => cr.status === 'in_progress' && cr.hash
  );
  if (activeConflict) {
    const conflictTab = workspace?.tabs?.find(
      (tab) => tab.kind === 'resolve-conflict' && tab.meta?.hash === activeConflict.hash
    );
    if (conflictTab) {
      navigate(conflictTab.route);
      return;
    }
  }

  if (workspace?.sessions?.length) {
    // Navigate to first session in custom tab order (or server order if no custom order)
    const ordered = sortSessionsByTabOrder(workspace.id, workspace.sessions);
    navigate(`/sessions/${ordered[0].id}`);
  } else {
    // No sessions - check for git changes
    const linesAdded = workspace?.lines_added ?? 0;
    const linesRemoved = workspace?.lines_removed ?? 0;
    const hasChanges = linesAdded > 0 || linesRemoved > 0;
    if (hasChanges) {
      navigate(`/diff/${workspaceId}`);
    } else {
      navigate(`/spawn?workspace_id=${workspaceId}`);
    }
  }
}

/**
 * Resolve the workspace a session ID belongs to by prefix match.
 * Local session IDs embed their workspace ID (`{workspaceID}-{uuid8}`),
 * so a dead session's workspace can be recovered from the ID alone.
 * Longest match wins, guarding against nested workspace IDs
 * (e.g. `schmux-003` vs `schmux-003-a`).
 */
export function findWorkspaceBySessionPrefix(
  workspaces: WorkspaceResponse[],
  sessionId: string
): WorkspaceResponse | undefined {
  let match: WorkspaceResponse | undefined;
  for (const ws of workspaces) {
    if (sessionId.startsWith(ws.id + '-') && (!match || ws.id.length > match.id.length)) {
      match = ws;
    }
  }
  return match;
}

/**
 * Find the next workspace with sessions in a given direction, skipping sessionless ones.
 * Returns the index of the found workspace, or -1 if none found.
 */
export function findNextWorkspaceWithSessions(
  workspaces: WorkspaceResponse[],
  currentIndex: number,
  direction: 1 | -1
): number {
  for (let i = currentIndex + direction; i >= 0 && i < workspaces.length; i += direction) {
    if (workspaces[i].sessions?.length && workspaces[i].status !== 'disposing') return i;
  }
  return -1;
}

/**
 * Hook to manage pending navigation - wait for a session or workspace to appear
 * in dashboard data and automatically navigate to it.
 *
 * Example usage after spawning a session:
 *   const { setPendingNavigation } = usePendingNavigation();
 *   setPendingNavigation({ type: 'session', id: newSessionId });
 *   // Dashboard will auto-navigate when session appears via WebSocket
 */
export function usePendingNavigation(): {
  pendingNavigation: PendingNavigation | null;
  setPendingNavigation: (nav: PendingNavigation | null) => void;
  clearPendingNavigation: () => void;
} {
  const { pendingNavigation, setPendingNavigation, clearPendingNavigation } = useSessions();
  return { pendingNavigation, setPendingNavigation, clearPendingNavigation };
}
