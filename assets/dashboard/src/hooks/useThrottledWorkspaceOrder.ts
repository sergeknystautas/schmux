import { useEffect, useMemo, useState } from 'react';

interface ThrottledOrderOptions {
  /** When false, `sorted` passes through unchanged and the held order is dropped. */
  enabled: boolean;
  /** Minimum time between adopted reorders. */
  intervalMs: number;
  /** Any change forces the next order to be adopted immediately. */
  resetKey: unknown;
}

interface HeldOrder {
  ids: string[];
  resetKey: unknown;
  adoptedAt: number;
}

function sameIdSet(a: string[], b: string[]): boolean {
  if (a.length !== b.length) return false;
  const set = new Set(a);
  return b.every((id) => set.has(id));
}

/**
 * Throttle reordering of an already-sorted list so positions change at most
 * once per `intervalMs`. Item data always comes from the latest `sorted`;
 * only the order is held.
 *
 * An order change applies immediately when the last adoption is at least
 * `intervalMs` old; otherwise one trailing timer applies the latest order when
 * the window ends. A change in the set of ids or in `resetKey` is adopted
 * immediately.
 */
export default function useThrottledWorkspaceOrder<T extends { id: string }>(
  sorted: T[] | undefined,
  { enabled, intervalMs, resetKey }: ThrottledOrderOptions
): T[] | undefined {
  const [held, setHeld] = useState<HeldOrder | null>(null);
  const sortedIds = useMemo(() => sorted?.map((item) => item.id) ?? [], [sorted]);

  const heldUsable =
    enabled && held !== null && held.resetKey === resetKey && sameIdSet(held.ids, sortedIds);

  useEffect(() => {
    if (!enabled) {
      setHeld(null);
      return;
    }
    const adopt = () => setHeld({ ids: sortedIds, resetKey, adoptedAt: Date.now() });
    if (!heldUsable || !held) {
      adopt();
      return;
    }
    if (held.ids.every((id, i) => id === sortedIds[i])) return;
    const wait = held.adoptedAt + intervalMs - Date.now();
    if (wait <= 0) {
      adopt();
      return;
    }
    const timer = setTimeout(adopt, wait);
    return () => clearTimeout(timer);
  }, [enabled, heldUsable, held, sortedIds, resetKey, intervalMs]);

  return useMemo(() => {
    if (!sorted || !heldUsable || !held) return sorted;
    const rank = new Map<string, number>(held.ids.map((id, i) => [id, i]));
    return [...sorted].sort((a, b) => (rank.get(a.id) ?? 0) - (rank.get(b.id) ?? 0));
  }, [sorted, heldUsable, held]);
}
