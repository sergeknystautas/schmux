import { useState, useMemo, useCallback, useRef } from 'react';
import { arrayMove } from '@dnd-kit/sortable';
import { sortSessionsByTabOrder, saveTabOrder } from '../lib/tabOrder';
import type { SessionResponse } from '../lib/types';

interface UseTabOrderResult {
  /** Sessions sorted by custom order (frozen during drag) */
  orderedSessions: SessionResponse[];
  /** Reorder: move activeId to overId's position, persist, and unfreeze */
  reorder: (activeId: string, overId: string) => void;
  /** Call on drag start to freeze the session list */
  startDrag: () => void;
  /** Call on drag end (without reorder) to unfreeze */
  endDrag: () => void;
}

export function useTabOrder(
  workspaceId: string | undefined,
  sessions: SessionResponse[]
): UseTabOrderResult {
  // Snapshot holds the reordered session list after drag-end, or the frozen
  // list during an active drag. When null, we fall through to the memoized sort.
  const [snapshot, setSnapshot] = useState<SessionResponse[] | null>(null);
  const isDragging = useRef(false);

  const sorted = useMemo(
    () => sortSessionsByTabOrder(workspaceId, sessions),
    [workspaceId, sessions]
  );

  // When sessions change (WebSocket update) and we're not dragging,
  // clear any stale snapshot so we use the fresh sorted result.
  const prevSessionsRef = useRef(sessions);
  if (sessions !== prevSessionsRef.current) {
    prevSessionsRef.current = sessions;
    if (!isDragging.current && snapshot !== null) {
      setSnapshot(null);
    }
  }

  const orderedSessions = snapshot ?? sorted;

  const startDrag = useCallback(() => {
    isDragging.current = true;
    setSnapshot(sorted);
  }, [sorted]);

  const endDrag = useCallback(() => {
    isDragging.current = false;
    setSnapshot(null);
  }, []);

  const reorder = useCallback(
    (activeId: string, overId: string) => {
      isDragging.current = false;

      if (!workspaceId || activeId === overId) {
        setSnapshot(null);
        return;
      }

      // Use the current ordered list (frozen snapshot or sorted)
      const current = snapshot ?? sorted;
      const currentIds = current.map((s) => s.id);

      const oldIndex = currentIds.indexOf(activeId);
      const newIndex = currentIds.indexOf(overId);

      // If either ID is stale (disposed mid-drag), discard
      if (oldIndex === -1 || newIndex === -1) {
        setSnapshot(null);
        return;
      }

      const newIds = arrayMove(currentIds, oldIndex, newIndex);
      saveTabOrder(workspaceId, newIds);

      // Build the reordered session array and set it as the snapshot.
      // This ensures the UI immediately shows the new order without
      // waiting for a WebSocket update to change the sessions reference
      // (which would be needed to bust the useMemo cache).
      const sessionMap = new Map(current.map((s) => [s.id, s]));
      const reordered = newIds.map((id) => sessionMap.get(id)!).filter(Boolean);
      setSnapshot(reordered);
    },
    [workspaceId, snapshot, sorted]
  );

  return { orderedSessions, reorder, startDrag, endDrag };
}
