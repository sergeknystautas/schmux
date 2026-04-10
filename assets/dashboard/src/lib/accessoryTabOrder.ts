import type { Tab } from './types.generated';

const ACCESSORY_TAB_ORDER_KEY_PREFIX = 'schmux:accessory-tab-order:';

const ACCESSORY_TAB_ORDER_CHANGED_EVENT = 'schmux:accessory-tab-order-changed';

export function sortTabsByOrder(workspaceId: string, tabs: Tab[]): Tab[] {
  if (!workspaceId || tabs.length <= 1) return tabs;

  try {
    const raw = localStorage.getItem(ACCESSORY_TAB_ORDER_KEY_PREFIX + workspaceId);
    if (!raw) return tabs;

    const order: string[] = JSON.parse(raw);
    if (!Array.isArray(order)) return tabs;

    const idx = new Map(order.map((id, i) => [id, i]));
    return [...tabs].sort((a, b) => (idx.get(a.id) ?? -1) - (idx.get(b.id) ?? -1));
  } catch {
    return tabs;
  }
}

export function saveAccessoryTabOrder(workspaceId: string, tabIds: string[]): void {
  try {
    localStorage.setItem(ACCESSORY_TAB_ORDER_KEY_PREFIX + workspaceId, JSON.stringify(tabIds));
    window.dispatchEvent(new CustomEvent(ACCESSORY_TAB_ORDER_CHANGED_EVENT));
  } catch {
    // Ignore storage errors
  }
}
