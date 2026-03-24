import type { SessionResponse } from './types';

/** localStorage key prefix for tab order. Uses schmux: convention. */
export const TAB_ORDER_KEY_PREFIX = 'schmux:tab-order:';

/** Custom DOM event dispatched when tab order changes, so other components can re-render. */
export const TAB_ORDER_CHANGED_EVENT = 'schmux:tab-order-changed';

/**
 * Sort sessions by a stored custom order from localStorage.
 * Pure function — reads localStorage but never writes to it.
 *
 * - Sessions in stored order appear in that order.
 * - New sessions (not in stored order) are appended at the end.
 * - Disposed sessions (in stored order but not in sessions array) are omitted.
 * - Falls back to original order on any error.
 */
export function sortSessionsByTabOrder(
  workspaceId: string | undefined,
  sessions: SessionResponse[]
): SessionResponse[] {
  if (!workspaceId || sessions.length === 0) return sessions;

  let storedOrder: string[];
  try {
    const raw = localStorage.getItem(`${TAB_ORDER_KEY_PREFIX}${workspaceId}`);
    if (!raw) return sessions;
    const parsed = JSON.parse(raw);
    if (!Array.isArray(parsed)) return sessions;
    storedOrder = parsed;
  } catch {
    return sessions;
  }

  const sessionMap = new Map(sessions.map((s) => [s.id, s]));
  const ordered: SessionResponse[] = [];

  // Add sessions in stored order (skip disposed)
  for (const id of storedOrder) {
    const sess = sessionMap.get(id);
    if (sess) {
      ordered.push(sess);
      sessionMap.delete(id);
    }
  }

  // Append new sessions not in stored order
  for (const sess of sessionMap.values()) {
    ordered.push(sess);
  }

  return ordered;
}

/**
 * Persist a new tab order to localStorage.
 * Silently catches quota/unavailable errors.
 */
export function saveTabOrder(workspaceId: string, sessionIds: string[]): void {
  try {
    localStorage.setItem(`${TAB_ORDER_KEY_PREFIX}${workspaceId}`, JSON.stringify(sessionIds));
    window.dispatchEvent(new Event(TAB_ORDER_CHANGED_EVENT));
  } catch {
    // Quota exceeded or unavailable — drag still works, just won't persist
  }
}
