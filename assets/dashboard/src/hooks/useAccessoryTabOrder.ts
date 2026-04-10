import { useState, useMemo, useCallback, useRef } from 'react';
import { arrayMove } from '@dnd-kit/sortable';
import type { Tab } from '../lib/types.generated';
import { sortTabsByOrder, saveAccessoryTabOrder } from '../lib/accessoryTabOrder';

interface UseAccessoryTabOrderResult {
  orderedTabs: Tab[];
  reorder: (activeId: string, overId: string) => void;
  startDrag: () => void;
  endDrag: () => void;
}

export function useAccessoryTabOrder(
  workspaceId: string | undefined,
  tabs: Tab[]
): UseAccessoryTabOrderResult {
  const [snapshot, setSnapshot] = useState<Tab[] | null>(null);
  const isDragging = useRef(false);

  const sorted = useMemo(
    () => (workspaceId ? sortTabsByOrder(workspaceId, tabs) : tabs),
    [workspaceId, tabs]
  );

  // When tabs change (WebSocket update) and we're not dragging,
  // clear any stale snapshot so we use the fresh sorted result.
  const prevTabsRef = useRef(tabs);
  if (tabs !== prevTabsRef.current) {
    prevTabsRef.current = tabs;
    if (!isDragging.current && snapshot !== null) {
      setSnapshot(null);
    }
  }

  const orderedTabs = snapshot ?? sorted;

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

      const current = snapshot ?? sorted;
      const currentIds = current.map((t) => t.id);

      const oldIndex = currentIds.indexOf(activeId);
      const newIndex = currentIds.indexOf(overId);

      if (oldIndex === -1 || newIndex === -1) {
        setSnapshot(null);
        return;
      }

      const newIds = arrayMove(currentIds, oldIndex, newIndex);
      saveAccessoryTabOrder(workspaceId, newIds);

      const tabMap = new Map(current.map((t) => [t.id, t]));
      const reordered = newIds.map((id) => tabMap.get(id)!).filter(Boolean);
      setSnapshot(reordered);
    },
    [workspaceId, snapshot, sorted]
  );

  return { orderedTabs, reorder, startDrag, endDrag };
}
