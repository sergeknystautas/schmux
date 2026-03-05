import { describe, it, expect } from 'vitest';
import { createScenarioSetup } from '../../website/src/demo/DemoShell';

describe('createScenarioSetup', () => {
  it('returns a setup with transport and scenario for "workspaces"', () => {
    const setup = createScenarioSetup('workspaces');
    expect(setup).toMatchObject({
      scenario: {
        id: 'workspaces',
        title: expect.any(String),
        initialRoute: '/',
        steps: expect.arrayContaining([
          expect.objectContaining({ target: expect.any(String), title: expect.any(String) }),
        ]),
      },
    });
    expect(setup.scenario.steps.length).toBeGreaterThan(0);
    expect(setup.transport.fetch).toBeInstanceOf(Function);
    expect(setup.transport.createWebSocket).toBeInstanceOf(Function);
  });

  it('returns a setup with transport and scenario for "spawn"', () => {
    const setup = createScenarioSetup('spawn');
    expect(setup).toMatchObject({
      scenario: {
        id: 'spawn',
        title: expect.any(String),
        initialRoute: '/',
        steps: expect.arrayContaining([
          expect.objectContaining({ target: expect.any(String), title: expect.any(String) }),
        ]),
      },
    });
    expect(setup.scenario.steps.length).toBeGreaterThan(0);
  });

  it('falls back to "workspaces" for unknown scenario IDs', () => {
    const setup = createScenarioSetup('nonexistent');
    expect(setup.scenario.id).toBe('workspaces');
  });
});
