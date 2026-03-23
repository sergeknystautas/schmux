import { test, expect } from './coverage-fixture';
import { seedConfig, createSaplingTestRepo, waitForHealthy, sleep } from './helpers';

test.describe.serial('Sapling workspace VCS support', () => {
  let workspaceId: string;
  let workspacePath: string;

  test.beforeAll(async () => {
    await waitForHealthy();

    // Verify sapling is installed
    const { execSync } = await import('child_process');
    execSync('sl version', { stdio: 'pipe' });

    // Create a local sapling repo
    const repoPath = await createSaplingTestRepo('test-sapling-repo');

    // Configure the daemon with the sapling repo
    await seedConfig({
      repoConfigs: [{ name: 'test-sapling-repo', url: repoPath, vcs: 'sapling' }],
      agents: [{ name: 'sleep-agent', command: "sh -c 'sleep 600'" }],
      saplingCommands: {
        create_workspace: 'cp -r {{.RepoBasePath}} {{.DestPath}}',
        remove_workspace: 'rm -rf {{.WorkspacePath}}',
        create_repo_base: 'cp -r {{.RepoIdentifier}} {{.BasePath}}',
      },
    });

    // Spawn a session — this creates the sapling workspace
    const spawnRes = await fetch('http://localhost:7337/api/spawn', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({
        repo: repoPath,
        branch: 'main',
        targets: { 'sleep-agent': 1 },
      }),
    });

    if (!spawnRes.ok) {
      const errText = await spawnRes.text();
      throw new Error(`Spawn failed (${spawnRes.status}): ${errText}`);
    }

    const results = (await spawnRes.json()) as Array<{
      workspace_id: string;
      error?: string;
    }>;

    if (results[0]?.error) {
      throw new Error(`Spawn returned error: ${results[0].error}`);
    }

    workspaceId = results[0].workspace_id;

    // Wait for status polling
    await sleep(12000);

    // Get workspace path from the sessions API
    const sessRes = await fetch('http://localhost:7337/api/sessions');
    const workspaces = (await sessRes.json()) as Array<{ id: string; path: string }>;
    const ws = workspaces.find((w) => w.id === workspaceId);
    if (!ws?.path) {
      throw new Error(`Could not find workspace path for ${workspaceId}`);
    }
    workspacePath = ws.path;
  });

  test('sessions API includes sapling workspace', async () => {
    const res = await fetch('http://localhost:7337/api/sessions');
    expect(res.ok).toBe(true);
    const workspaces = (await res.json()) as Array<{ id: string; vcs?: string }>;
    const ws = workspaces.find((w) => w.id === workspaceId);
    expect(ws).toBeDefined();
    expect(ws?.vcs).toBe('sapling');
  });

  test('diff API returns 200 with files for sapling workspace', async () => {
    // Create a file in the workspace so diff has something to show
    const fs = await import('fs');
    const path = await import('path');
    fs.writeFileSync(path.join(workspacePath, 'newfile.txt'), 'hello from scenario\n');

    const res = await fetch(`http://localhost:7337/api/diff/${workspaceId}`);
    expect(res.status).toBe(200);
    const body = (await res.json()) as { files: Array<{ new_path?: string; status?: string }> };
    const found = body.files.some((f) => f.new_path === 'newfile.txt');
    expect(found).toBe(true);
  });

  test('stage API succeeds for sapling workspace', async () => {
    // Create a file to stage
    const fs = await import('fs');
    const path = await import('path');
    fs.writeFileSync(path.join(workspacePath, 'staged.txt'), 'to be staged\n');

    const res = await fetch(
      `http://localhost:7337/api/workspaces/${workspaceId}/git-commit-stage`,
      {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ files: ['staged.txt'] }),
      }
    );
    expect(res.status).toBe(200);
    const body = (await res.json()) as { success: boolean };
    expect(body.success).toBe(true);
  });

  test('discard API removes untracked file in sapling workspace', async () => {
    const fs = await import('fs');
    const path = await import('path');
    const throwawayPath = path.join(workspacePath, 'throwaway.txt');
    fs.writeFileSync(throwawayPath, 'to be discarded\n');

    const res = await fetch(`http://localhost:7337/api/workspaces/${workspaceId}/git-discard`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ files: ['throwaway.txt'] }),
    });
    expect(res.status).toBe(200);

    // File should be removed
    expect(fs.existsSync(throwawayPath)).toBe(false);
  });

  test('git-graph API returns 400 for sapling workspace', async () => {
    const res = await fetch(`http://localhost:7337/api/workspaces/${workspaceId}/git-graph`);
    expect(res.status).toBe(400);
    const body = await res.text();
    expect(body.toLowerCase()).toContain('not available');
  });
});
