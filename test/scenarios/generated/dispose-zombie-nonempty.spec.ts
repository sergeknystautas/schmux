import { test, expect } from './coverage-fixture';
import {
  seedConfig,
  createTestRepo,
  getSessions,
  spawnSession,
  waitForHealthy,
  waitForSessionRunning,
  disposeSession,
  sleep,
} from './helpers';
import { promises as fs } from 'fs';
import * as path from 'path';

test.describe.serial('Dispose a zombie workspace whose directory still has files', () => {
  const leftoverRelPath = path.join('.schmux', 'events', 'session-transcript.jsonl');
  const leftoverContent = '{"event":"preserved"}\n';

  let sessionId: string;
  let workspaceId: string;
  let workspaceDir: string;

  test.beforeAll(async () => {
    await waitForHealthy();
    const repoPath = await createTestRepo('test-repo-zombie-nonempty');
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
      branch: 'zombie-nonempty-branch',
      targets: { 'echo-agent': 1 },
    });
    sessionId = results[0].session_id;
    workspaceId = results[0].workspace_id;
    await waitForSessionRunning(sessionId);

    // The per-worker fixture sets HOME to the worker's isolated home and
    // places workspaces under <HOME>/workspaces.
    const homeDir = process.env.HOME || '';
    workspaceDir = path.join(homeDir, 'workspaces', workspaceId);

    // Dispose the session first so dispose-workspace is allowed (no active sessions).
    await disposeSession(sessionId);

    const sessionStopDeadline = Date.now() + 10_000;
    while (Date.now() < sessionStopDeadline) {
      const wsList = await getSessions();
      const ws = wsList.find((w) => w.id === workspaceId);
      if (!ws || ws.sessions.every((s) => !s.running)) break;
      await sleep(200);
    }

    // Zombify: strip VCS metadata by removing .git, but keep the rest of the
    // directory. This matches the "directory still has files, no VCS metadata"
    // precondition in test/scenarios/dispose-zombie-nonempty.md.
    await fs.rm(path.join(workspaceDir, '.git'), { recursive: true, force: true });

    // Plant a leftover file that dispose must not destroy. Session event
    // transcripts under .schmux/events are the motivating real-world case.
    const leftoverPath = path.join(workspaceDir, leftoverRelPath);
    await fs.mkdir(path.dirname(leftoverPath), { recursive: true });
    await fs.writeFile(leftoverPath, leftoverContent);
  });

  test('dispose workspace via API returns 200 OK', async () => {
    const baseURL = process.env.SCHMUX_BASE_URL || 'http://localhost:7337';
    const res = await fetch(`${baseURL}/api/workspaces/${workspaceId}/dispose`, {
      method: 'POST',
    });
    expect(res.ok).toBe(true);
  });

  test('GET /api/sessions no longer includes the disposed workspace', async () => {
    const workspaces = await getSessions();
    const found = workspaces.find((w) => w.id === workspaceId);
    expect(found).toBeUndefined();
  });

  test('zombie directory is still present on disk', async () => {
    const stat = await fs.stat(workspaceDir);
    expect(stat.isDirectory()).toBe(true);
  });

  test('leftover files are preserved unchanged', async () => {
    const content = await fs.readFile(path.join(workspaceDir, leftoverRelPath), 'utf-8');
    expect(content).toBe(leftoverContent);
  });
});
