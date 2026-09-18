import { describe, expect, it } from 'vitest';
import { emptySidebarPanels, resolveSidebarPanels } from './sidebarPanels';

describe('emptySidebarPanels', () => {
  it('starts every panel disabled', () => {
    expect(emptySidebarPanels()).toEqual({
      planUsage: false,
      serverLoad: false,
      eventMonitor: false,
      tmuxDiagnostic: false,
      typingPerformance: false,
      curation: false,
    });
  });
});

describe('resolveSidebarPanels', () => {
  it('returns defaults without config', () => {
    expect(resolveSidebarPanels(undefined)).toEqual(emptySidebarPanels());
  });

  it('explicit overrides win over defaults', () => {
    const r = resolveSidebarPanels({ ui: { panels: { eventMonitor: true } } });
    expect(r.eventMonitor).toBe(true);
  });

  it('unknown panel keys are ignored', () => {
    const r = resolveSidebarPanels({
      ui: { panels: { nonsense: true } as Record<string, boolean> },
    });
    expect(r.planUsage).toBe(false);
  });

  it('missing panel keys stay disabled', () => {
    const r = resolveSidebarPanels({ ui: { panels: { eventMonitor: true } } });
    expect(r.planUsage).toBe(false);
    expect(r.serverLoad).toBe(false);
  });
});
