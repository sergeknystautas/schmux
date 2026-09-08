import { useNavigate, useLocation } from 'react-router';
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

// --- App-lifetime location tracker ---------------------------------------
//
// The dispose-redirect guard needs to know whether the user navigated
// while a dispose API call was in flight — including after the page
// that started the dispose has unmounted. A component-local ref can't
// survive that unmount (it freezes at the pre-navigation location,
// whose key still equals the captured one), so the live key lives at
// module scope and is fed by useLocationKeyTracker() from the app
// root, which mounts once and re-renders on every navigation.

let trackedLocationKey: string | null = null;

/** The live router location key; null before the tracker first ran. */
export function currentLocationKey(): string | null {
  return trackedLocationKey;
}

/** True iff no navigation has occurred since `key` was captured. */
export function locationUnchangedSince(key: string | null): boolean {
  return key !== null && trackedLocationKey === key;
}

/**
 * Feed the tracker from the app root. Call once in App.tsx.
 *
 * Assigns during render rather than in an effect: effects flush after
 * paint, and a dispose resolving inside that window would compare
 * against a stale key. Render-time assignment is idempotent, so Strict
 * Mode double-rendering is harmless.
 */
export function useLocationKeyTracker(): void {
  const location = useLocation();
  trackedLocationKey = location.key;
}
