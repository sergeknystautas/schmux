import { test, expect } from './coverage-fixture';
import {
  seedConfig,
  createTestRepo,
  apiGet,
  apiPost,
  getConfig,
  resetConfig,
  waitForHealthy,
  waitForSessionRunning,
  disposeSession,
} from './helpers';

interface SessionWithStyle {
  id: string;
  target: string;
  running: boolean;
  style_id?: string;
}

interface WorkspaceWithStyle {
  id: string;
  sessions: SessionWithStyle[];
}

interface SpawnResult {
  session_id: string;
  workspace_id: string;
  error?: string;
}

test.describe('Spawn applies default communication style from config', () => {
  let repoPath: string;
  let savedConfig: Record<string, unknown>;

  test.beforeAll(async () => {
    await waitForHealthy();
    repoPath = await createTestRepo('style-spawn-repo');
    savedConfig = await getConfig();

    await seedConfig({
      repos: [repoPath],
      agents: [
        {
          name: 'echo-agent',
          command: "sh -c 'echo style-test; sleep 600'",
        },
      ],
    });
  });

  test.afterAll(async () => {
    await resetConfig(savedConfig);
  });

  test('spawn without style_id uses configured default', async () => {
    // Set a default comm style for the echo-agent tool
    await apiPost('/api/config', { comm_styles: { 'echo-agent': 'pirate' } });

    // Verify config was saved
    const config = await apiGet<{ comm_styles?: Record<string, string> }>('/api/config');
    expect(config.comm_styles).toBeDefined();
    expect(config.comm_styles!['echo-agent']).toBe('pirate');

    // Spawn a session WITHOUT specifying style_id
    const results = await apiPost<SpawnResult[]>('/api/spawn', {
      repo: repoPath,
      branch: 'default-style-branch',
      targets: { 'echo-agent': 1 },
    });
    expect(results).toHaveLength(1);
    expect(results[0].error).toBeUndefined();
    const sessionId = results[0].session_id;
    await waitForSessionRunning(sessionId);

    // Verify the session picked up the default style
    const workspaces = await apiGet<WorkspaceWithStyle[]>('/api/sessions');
    const session = workspaces.flatMap((ws) => ws.sessions).find((s) => s.id === sessionId);
    expect(session).toBeDefined();
    expect(session!.style_id).toBe('pirate');

    await disposeSession(sessionId);
  });

  test('spawn with explicit style_id overrides default', async () => {
    // Default is still 'pirate' from previous test
    // Spawn with explicit style_id 'caveman'
    const results = await apiPost<SpawnResult[]>('/api/spawn', {
      repo: repoPath,
      branch: 'explicit-style-branch',
      targets: { 'echo-agent': 1 },
      style_id: 'caveman',
    });
    expect(results).toHaveLength(1);
    expect(results[0].error).toBeUndefined();
    const sessionId = results[0].session_id;
    await waitForSessionRunning(sessionId);

    // Verify the session uses the explicit style, not the default
    const workspaces = await apiGet<WorkspaceWithStyle[]>('/api/sessions');
    const session = workspaces.flatMap((ws) => ws.sessions).find((s) => s.id === sessionId);
    expect(session).toBeDefined();
    expect(session!.style_id).toBe('caveman');

    await disposeSession(sessionId);
  });

  test('spawn with style_id "none" suppresses default', async () => {
    // Default is still 'pirate'
    // Spawn with style_id 'none' to explicitly suppress it
    const results = await apiPost<SpawnResult[]>('/api/spawn', {
      repo: repoPath,
      branch: 'none-style-branch',
      targets: { 'echo-agent': 1 },
      style_id: 'none',
    });
    expect(results).toHaveLength(1);
    expect(results[0].error).toBeUndefined();
    const sessionId = results[0].session_id;
    await waitForSessionRunning(sessionId);

    // Verify no style was applied
    const workspaces = await apiGet<WorkspaceWithStyle[]>('/api/sessions');
    const session = workspaces.flatMap((ws) => ws.sessions).find((s) => s.id === sessionId);
    expect(session).toBeDefined();
    expect(session!.style_id).toBeFalsy();

    await disposeSession(sessionId);
  });

  test('no default configured means no style applied', async () => {
    // Clear all comm_styles defaults
    await apiPost('/api/config', { comm_styles: {} });

    const results = await apiPost<SpawnResult[]>('/api/spawn', {
      repo: repoPath,
      branch: 'no-default-branch',
      targets: { 'echo-agent': 1 },
    });
    expect(results).toHaveLength(1);
    expect(results[0].error).toBeUndefined();
    const sessionId = results[0].session_id;
    await waitForSessionRunning(sessionId);

    // Verify no style was applied
    const workspaces = await apiGet<WorkspaceWithStyle[]>('/api/sessions');
    const session = workspaces.flatMap((ws) => ws.sessions).find((s) => s.id === sessionId);
    expect(session).toBeDefined();
    expect(session!.style_id).toBeFalsy();

    await disposeSession(sessionId);
  });
});
