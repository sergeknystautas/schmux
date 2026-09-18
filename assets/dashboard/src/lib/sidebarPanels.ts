// Sidebar panel preferences. Panels are off unless explicitly enabled.

export type SidebarPanelID =
  'planUsage' | 'serverLoad' | 'eventMonitor' | 'tmuxDiagnostic' | 'typingPerformance' | 'curation';

export const SIDEBAR_PANEL_META: { id: SidebarPanelID; label: string }[] = [
  { id: 'planUsage', label: 'Plan Usage' },
  { id: 'serverLoad', label: 'Server Load' },
  { id: 'eventMonitor', label: 'Event Monitor' },
  { id: 'tmuxDiagnostic', label: 'Tmux Diagnostics' },
  { id: 'typingPerformance', label: 'Typing Performance' },
  { id: 'curation', label: 'Autolearn Curation' },
];

export function emptySidebarPanels(): Record<SidebarPanelID, boolean> {
  return {
    planUsage: false,
    serverLoad: false,
    eventMonitor: false,
    tmuxDiagnostic: false,
    typingPerformance: false,
    curation: false,
  };
}

export function resolveSidebarPanels(
  config?: { ui?: { panels?: Record<string, boolean> } } | null
): Record<SidebarPanelID, boolean> {
  const resolved = emptySidebarPanels();
  const overrides = config?.ui?.panels;
  if (overrides) {
    for (const { id } of SIDEBAR_PANEL_META) {
      const v = overrides[id];
      if (typeof v === 'boolean') resolved[id] = v;
    }
  }
  return resolved;
}
