import { test, expect } from './coverage-fixture';
import {
  seedConfig,
  createTestRepo,
  getSessions,
  spawnSession,
  waitForHealthy,
  waitForSessionRunning,
} from './helpers';
import WS from 'ws';

function getBaseURL(): string {
  return process.env.SCHMUX_BASE_URL || 'http://localhost:7337';
}

test.describe.serial('Xterm title updates propagate to dashboard tabs', () => {
  let sessionId: string;

  test.beforeAll(async () => {
    await waitForHealthy();
    const repoPath = await createTestRepo('test-repo-xterm-title');
    await seedConfig({
      repos: [repoPath],
      agents: [
        {
          name: 'echo-agent',
          command: "sh -c 'echo hello; sleep 600'",
        },
      ],
    });

    const results = await spawnSession({
      repo: repoPath,
      branch: 'main',
      targets: { 'echo-agent': 1 },
    });
    sessionId = results[0].session_id;
    await waitForSessionRunning(sessionId);
  });

  test('PUT sets xterm_title and it appears in GET /api/sessions', async () => {
    const res = await fetch(`${getBaseURL()}/api/sessions-xterm-title/${sessionId}`, {
      method: 'PUT',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ title: 'Working on feature X' }),
    });
    expect(res.status).toBe(200);

    const workspaces = await getSessions();
    const session = workspaces.flatMap((ws) => ws.sessions).find((s) => s.id === sessionId);
    expect(session).toBeDefined();
    expect((session as Record<string, unknown>).xterm_title).toBe('Working on feature X');
  });

  test('same title is idempotent (200)', async () => {
    const res = await fetch(`${getBaseURL()}/api/sessions-xterm-title/${sessionId}`, {
      method: 'PUT',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ title: 'Working on feature X' }),
    });
    expect(res.status).toBe(200);
  });

  test('title change triggers WebSocket broadcast', async () => {
    const ws = new WS(`${getBaseURL().replace(/^http/, 'ws')}/ws/dashboard`);

    const titlePromise = new Promise<string>((resolve, reject) => {
      const timer = setTimeout(() => {
        ws.close();
        reject(new Error('Title did not appear in WebSocket broadcast within 5 seconds'));
      }, 5000);

      ws.on('message', (data: WS.Data) => {
        try {
          const msg = JSON.parse(data.toString());
          if (msg.type === 'sessions' && Array.isArray(msg.workspaces)) {
            for (const workspace of msg.workspaces) {
              for (const session of workspace.sessions || []) {
                if (session.id === sessionId && session.xterm_title === 'Broadcast test') {
                  clearTimeout(timer);
                  ws.close();
                  resolve(session.xterm_title);
                }
              }
            }
          }
        } catch {
          // ignore non-JSON
        }
      });
    });

    await new Promise<void>((resolve) => ws.on('open', resolve));

    await fetch(`${getBaseURL()}/api/sessions-xterm-title/${sessionId}`, {
      method: 'PUT',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ title: 'Broadcast test' }),
    });

    const title = await titlePromise;
    expect(title).toBe('Broadcast test');
  });

  test('empty title clears xterm_title', async () => {
    const res = await fetch(`${getBaseURL()}/api/sessions-xterm-title/${sessionId}`, {
      method: 'PUT',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ title: '' }),
    });
    expect(res.status).toBe(200);

    const workspaces = await getSessions();
    const session = workspaces.flatMap((ws) => ws.sessions).find((s) => s.id === sessionId);
    expect((session as Record<string, unknown>).xterm_title).toBeFalsy();
  });
});
