import { test, expect } from './coverage-fixture';
import {
  seedConfig,
  createTestRepo,
  spawnSession,
  waitForHealthy,
  apiGet,
  apiPost,
  sleep,
} from './helpers';

const BASE_URL = 'http://localhost:7337';

test.describe.serial('Git stage and discard operations', () => {
  let repoPath: string;
  let workspaceId: string;

  test.beforeAll(async () => {
    await waitForHealthy();
    repoPath = await createTestRepo('test-repo-git-ops');

    await seedConfig({
      repos: [repoPath],
      agents: [
        {
          name: 'file-modifier',
          command:
            "sh -c 'echo new-line >> README.md && echo new-file-content > extra.txt; sleep 600'",
          promptable: true,
        },
      ],
    });

    // Spawn a session so the agent modifies files in the workspace
    const results = await spawnSession({
      repo: repoPath,
      branch: 'git-ops-branch',
      prompt: 'test',
      targets: { 'file-modifier': 1 },
    });
    workspaceId = results[0].workspace_id;

    // Wait for the agent to modify files and git status to detect changes
    for (let i = 0; i < 25; i++) {
      await sleep(1000);
      try {
        const resp = await apiGet<{ files?: Array<{ new_path?: string }> }>(
          `/api/diff/${workspaceId}`
        );
        if (resp.files && resp.files.length > 0) break;
      } catch {
        // diff endpoint may not be ready yet
      }
    }
  });

  test('diff API shows modified files before any git operation', async () => {
    const resp = await apiGet<{ files?: Array<{ new_path?: string }> }>(`/api/diff/${workspaceId}`);

    expect(resp.files).toBeDefined();
    expect(resp.files!.length).toBeGreaterThanOrEqual(1);

    // README.md should be in the diff (modified by the agent)
    const readmeFile = resp.files!.find((f) => f.new_path === 'README.md');
    expect(readmeFile).toBeDefined();
  });

  test('stage files via git-commit-stage API', async () => {
    const resp = await apiPost<{ success: boolean; message: string }>(
      `/api/workspaces/${workspaceId}/git-commit-stage`,
      { files: ['README.md'] }
    );
    expect(resp.success).toBe(true);
    expect(resp.message).toBe('Files staged');
  });

  test('discard specific file via git-discard API', async () => {
    const resp = await apiPost<{ success: boolean; message: string }>(
      `/api/workspaces/${workspaceId}/git-discard`,
      { files: ['extra.txt'] }
    );
    expect(resp.success).toBe(true);
    expect(resp.message).toBe('Changes discarded');
  });

  test('diff API reflects state after stage and discard', async () => {
    // Give git status a moment to update after the operations
    await sleep(2000);

    const resp = await apiGet<{ files?: Array<{ new_path?: string }> }>(`/api/diff/${workspaceId}`);

    // README.md should still appear (staged but not committed — diff vs HEAD still shows it)
    const readmeFile = resp.files?.find((f) => f.new_path === 'README.md');
    expect(readmeFile).toBeDefined();

    // extra.txt should be gone (discarded)
    const extraFile = resp.files?.find((f) => f.new_path === 'extra.txt');
    expect(extraFile).toBeUndefined();
  });
});

test.describe.serial('Git discard all changes', () => {
  let repoPath: string;
  let workspaceId: string;

  test.beforeAll(async () => {
    await waitForHealthy();
    repoPath = await createTestRepo('test-repo-git-discard-all');

    await seedConfig({
      repos: [repoPath],
      agents: [
        {
          name: 'modifier',
          command: "sh -c 'echo change >> README.md && echo newfile > added.txt; sleep 600'",
          promptable: true,
        },
      ],
    });

    const results = await spawnSession({
      repo: repoPath,
      branch: 'discard-all-branch',
      prompt: 'test',
      targets: { modifier: 1 },
    });
    workspaceId = results[0].workspace_id;

    // Wait for changes to appear
    for (let i = 0; i < 25; i++) {
      await sleep(1000);
      try {
        const resp = await apiGet<{ files?: Array<{ new_path?: string }> }>(
          `/api/diff/${workspaceId}`
        );
        if (resp.files && resp.files.length > 0) break;
      } catch {
        // not ready
      }
    }
  });

  test('discard all changes via git-discard API with empty body', async () => {
    // Discard all (empty body = discard everything)
    const resp = await apiPost<{ success: boolean; message: string }>(
      `/api/workspaces/${workspaceId}/git-discard`,
      {}
    );
    expect(resp.success).toBe(true);
    expect(resp.message).toBe('Changes discarded');
  });

  test('diff API shows no changes after full discard', async () => {
    await sleep(2000);

    const resp = await apiGet<{ files?: Array<{ new_path?: string }> }>(`/api/diff/${workspaceId}`);

    // All changes should be gone — git clean + checkout restores to HEAD
    expect(resp.files?.length ?? 0).toBe(0);
  });
});

test.describe.serial('Git operations — path validation', () => {
  let workspaceId: string;
  let repoPath: string;

  test.beforeAll(async () => {
    await waitForHealthy();
    repoPath = await createTestRepo('test-repo-git-validate');

    await seedConfig({
      repos: [repoPath],
      agents: [
        {
          name: 'idle-agent',
          command: "sh -c 'sleep 600'",
          promptable: true,
        },
      ],
    });

    const results = await spawnSession({
      repo: repoPath,
      branch: 'validate-branch',
      prompt: 'test',
      targets: { 'idle-agent': 1 },
    });
    workspaceId = results[0].workspace_id;
  });

  test('rejects absolute path in git-commit-stage', async () => {
    const res = await fetch(`${BASE_URL}/api/workspaces/${workspaceId}/git-commit-stage`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ files: ['/etc/passwd'] }),
    });
    expect(res.status).toBe(400);
    const body = await res.json();
    expect(body.error).toContain('invalid file path');
  });

  test('rejects path traversal in git-commit-stage', async () => {
    const res = await fetch(`${BASE_URL}/api/workspaces/${workspaceId}/git-commit-stage`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ files: ['../../../etc/shadow'] }),
    });
    expect(res.status).toBe(400);
    const body = await res.json();
    expect(body.error).toContain('invalid file path');
  });

  test('rejects path traversal in git-discard', async () => {
    const res = await fetch(`${BASE_URL}/api/workspaces/${workspaceId}/git-discard`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ files: ['../../secret'] }),
    });
    expect(res.status).toBe(400);
    const body = await res.json();
    expect(body.error).toContain('invalid file path');
  });

  test('rejects invalid JSON in git-commit-stage', async () => {
    const res = await fetch(`${BASE_URL}/api/workspaces/${workspaceId}/git-commit-stage`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: 'not json at all',
    });
    expect(res.status).toBe(400);
  });

  test('git-uncommit rejects when no commits ahead', async () => {
    // GitAhead is 0 for a freshly created workspace — handler checks this first
    const res = await fetch(`${BASE_URL}/api/workspaces/${workspaceId}/git-uncommit`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ hash: 'abc123' }),
    });
    expect(res.status).toBe(400);
    const body = await res.json();
    expect(body.error).toContain('No commits to uncommit');
  });

  test('git-amend rejects when no commits ahead', async () => {
    // GitAhead is 0 for a freshly created workspace — handler checks this first
    const res = await fetch(`${BASE_URL}/api/workspaces/${workspaceId}/git-amend`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ files: ['README.md'] }),
    });
    expect(res.status).toBe(400);
    const body = await res.json();
    expect(body.error).toContain('No commits to amend');
  });
});

test.describe.serial('Git operations — nonexistent workspace', () => {
  test.beforeAll(async () => {
    await waitForHealthy();
  });

  test('git-commit-stage returns 404 for unknown workspace', async () => {
    const res = await fetch(`${BASE_URL}/api/workspaces/nonexistent-ws-id/git-commit-stage`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ files: ['README.md'] }),
    });
    expect(res.status).toBe(404);
  });

  test('git-discard returns 404 for unknown workspace', async () => {
    const res = await fetch(`${BASE_URL}/api/workspaces/nonexistent-ws-id/git-discard`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ files: ['README.md'] }),
    });
    expect(res.status).toBe(404);
  });

  test('git-uncommit returns 404 for unknown workspace', async () => {
    const res = await fetch(`${BASE_URL}/api/workspaces/nonexistent-ws-id/git-uncommit`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ hash: 'abc123' }),
    });
    expect(res.status).toBe(404);
  });

  test('git-amend returns 404 for unknown workspace', async () => {
    const res = await fetch(`${BASE_URL}/api/workspaces/nonexistent-ws-id/git-amend`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ files: ['README.md'] }),
    });
    expect(res.status).toBe(404);
  });
});
