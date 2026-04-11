import { test, expect } from './coverage-fixture';
import {
  seedConfig,
  createTestRepo,
  getSessions,
  spawnSession,
  waitForHealthy,
  waitForSessionRunning,
  apiPost,
} from './helpers';

test.describe.serial('Dispose a zombie workspace', () => {
  let repoPath: string;
  let sessionId: string;
  let workspaceId: string;

  test.beforeAll(async () => {
    await waitForHealthy();
    repoPath = await createTestRepo('test-repo-zombie');
    await seedConfig({
      repos: [repoPath],
      agents: [
        {
          name: 'echo-agent',
          command: "sh -c 'echo hello; sleep 600'",
        },
      ],
    });

    // Spawn a session to create a workspace
    const results = await spawnSession({
      repo: repoPath,
      branch: 'test-branch',
      targets: { 'echo-agent': 1 },
    });
    sessionId = results[0].session_id;
    workspaceId = results[0].workspace_id;
    await waitForSessionRunning(sessionId);

    // Dispose the session first (workspace persists with no sessions)
    await apiPost(`/api/sessions/${sessionId}/dispose`);

    // Wait for session to stop
    const deadline = Date.now() + 10_000;
    while (Date.now() < deadline) {
      const wsList = await getSessions();
      const ws = wsList.find((w) => w.id === workspaceId);
      if (!ws || ws.sessions.every((s) => !s.running)) break;
      await new Promise((r) => setTimeout(r, 200));
    }
  });

  test('dispose workspace via API succeeds', async () => {
    const baseURL = process.env.SCHMUX_BASE_URL || 'http://localhost:7337';
    const res = await fetch(`${baseURL}/api/workspaces/${workspaceId}/dispose`, {
      method: 'POST',
      headers: {},
    });
    expect(res.ok).toBe(true);
    const body = await res.json();
    expect(body.status).toBe('ok');
  });

  test('workspace is removed from state', async () => {
    const workspaces = await getSessions();
    const found = workspaces.find((w) => w.id === workspaceId);
    expect(found).toBeUndefined();
  });
});
