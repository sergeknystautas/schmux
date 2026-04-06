import { test, expect } from './coverage-fixture';
import {
  seedConfig,
  createTestRepo,
  getSessions,
  spawnSession,
  waitForDashboardLive,
  waitForHealthy,
  waitForSessionRunning,
  apiPost,
  sleep,
} from './helpers';
import WS from 'ws';

function getBaseURL(): string {
  return process.env.SCHMUX_BASE_URL || 'http://localhost:7337';
}

test.describe.serial('Command spawn triggers immediate WebSocket broadcast', () => {
  let repoPath: string;
  let workspaceId: string;

  test.beforeAll(async () => {
    await waitForHealthy();
    repoPath = await createTestRepo('test-repo-cmd-broadcast');
    await seedConfig({
      repos: [repoPath],
      agents: [
        {
          name: 'echo-agent',
          command: "sh -c 'echo hello; sleep 600'",
        },
      ],
    });

    // Spawn a target-based session first to create a workspace
    const results = await spawnSession({
      repo: repoPath,
      branch: 'main',
      targets: { 'echo-agent': 1 },
    });
    workspaceId = results[0].workspace_id;
    await waitForSessionRunning(results[0].session_id);
  });

  test('command spawn appears in WebSocket within 3 seconds', async () => {
    // Open a WebSocket to listen for dashboard broadcasts
    const ws = new WS(`${getBaseURL().replace(/^http/, 'ws')}/ws/dashboard`);
    const receivedSessionIds: string[] = [];

    const broadcastPromise = new Promise<string>((resolve, reject) => {
      const timer = setTimeout(() => {
        ws.close();
        reject(
          new Error(
            `Command session did not appear in WebSocket broadcast within 3 seconds. ` +
              `Received session IDs: [${receivedSessionIds.join(', ')}]`
          )
        );
      }, 3000);

      ws.on('message', (data: WS.Data) => {
        try {
          const msg = JSON.parse(data.toString());
          // Dashboard broadcasts have format: { type: "sessions", workspaces: [...] }
          if (msg.type === 'sessions' && Array.isArray(msg.workspaces)) {
            for (const workspace of msg.workspaces) {
              if (workspace.sessions) {
                for (const session of workspace.sessions) {
                  receivedSessionIds.push(session.id);
                  // Look for our command session (target === "command")
                  if (session.target === 'command' && session.nickname === 'test-shell') {
                    clearTimeout(timer);
                    ws.close();
                    resolve(session.id);
                  }
                }
              }
            }
          }
        } catch {
          // Not JSON or unexpected format — ignore
        }
      });

      ws.on('error', (err: Error) => {
        clearTimeout(timer);
        reject(new Error(`WebSocket error: ${err.message}`));
      });
    });

    // Wait for WebSocket to connect (state transition, not fixed delay)
    await new Promise<void>((resolve, reject) => {
      if (ws.readyState === WS.OPEN) return resolve();
      ws.once('open', resolve);
      ws.once('error', reject);
    });

    // Spawn a command session (like a "shell" quick launch)
    const spawnRes = await apiPost<
      Array<{ session_id: string; workspace_id: string; error?: string }>
    >('/api/spawn', {
      repo: repoPath,
      branch: 'main',
      command: "sh -c 'echo command-session-started; sleep 600'",
      nickname: 'test-shell',
      workspace_id: workspaceId,
    });

    expect(spawnRes[0].error).toBeUndefined();
    const commandSessionId = spawnRes[0].session_id;
    expect(commandSessionId).toBeTruthy();

    // The session should appear in the WebSocket broadcast within 3 seconds
    // (before the bug fix, this would time out waiting for the next poll cycle)
    const broadcastedId = await broadcastPromise;
    expect(broadcastedId).toBe(commandSessionId);
  });

  test('API confirms command session exists', async () => {
    const workspaces = await getSessions();

    // Find the command session
    const allSessions = workspaces.flatMap((ws) => ws.sessions);
    const commandSession = allSessions.find(
      (s) => s.target === 'command' && s.nickname === 'test-shell'
    );

    expect(commandSession).toBeDefined();
    expect(commandSession!.running).toBe(true);
    expect(commandSession!.target).toBe('command');
  });
});
