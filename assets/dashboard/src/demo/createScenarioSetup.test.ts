import { describe, it, expect } from 'vitest';
import { createScenarioSetup } from '../../website/src/demo/DemoShell';

describe('createScenarioSetup', () => {
  it('returns a setup with transport and scenario for "workspaces"', () => {
    const setup = createScenarioSetup('workspaces');
    expect(setup).toBeDefined();
    expect(setup.transport).toBeDefined();
    expect(setup.transport.fetch).toBeInstanceOf(Function);
    expect(setup.transport.createWebSocket).toBeInstanceOf(Function);
    expect(setup.scenario).toBeDefined();
    expect(setup.scenario.steps.length).toBeGreaterThan(0);
  });

  it('returns a setup with transport and scenario for "spawn"', () => {
    const setup = createScenarioSetup('spawn');
    expect(setup).toBeDefined();
    expect(setup.transport).toBeDefined();
    expect(setup.scenario).toBeDefined();
    expect(setup.scenario.steps.length).toBeGreaterThan(0);
  });

  it('falls back to "workspaces" for unknown scenario IDs', () => {
    const setup = createScenarioSetup('nonexistent');
    const workspacesSetup = createScenarioSetup('workspaces');
    // Same scenario structure (same step count and titles)
    expect(setup.scenario.steps.length).toBe(workspacesSetup.scenario.steps.length);
  });
});
