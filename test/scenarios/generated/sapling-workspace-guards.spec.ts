import { test, expect } from './coverage-fixture';
import { seedConfig, createSaplingTestRepo, waitForHealthy, sleep } from './helpers';

test.describe.serial('Sapling workspace VCS guards', () => {
  let workspaceId: string;

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
  });

  test('diff API returns 400 for sapling workspace', async () => {
    const res = await fetch(`http://localhost:7337/api/diff/${workspaceId}`);
    expect(res.status).toBe(400);
    const body = await res.text();
    expect(body.toLowerCase()).toContain('not available');
  });

  test('git-graph API returns 400 for sapling workspace', async () => {
    const res = await fetch(`http://localhost:7337/api/workspaces/${workspaceId}/git-graph`);
    expect(res.status).toBe(400);
    const body = await res.text();
    expect(body.toLowerCase()).toContain('not available');
  });

  test('git-commit-stage API returns 400 for sapling workspace', async () => {
    const res = await fetch(
      `http://localhost:7337/api/workspaces/${workspaceId}/git-commit-stage`,
      {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ files: ['README.md'] }),
      }
    );
    expect(res.status).toBe(400);
    const body = await res.text();
    expect(body.toLowerCase()).toContain('not available');
  });

  test('sessions API includes sapling workspace', async () => {
    const res = await fetch('http://localhost:7337/api/sessions');
    expect(res.ok).toBe(true);
    // Sessions API returns a flat array of workspaces (not { workspaces: [...] })
    const workspaces = (await res.json()) as Array<{ id: string; vcs?: string }>;
    const ws = workspaces.find((w) => w.id === workspaceId);
    expect(ws).toBeDefined();
    expect(ws?.vcs).toBe('sapling');
  });
});
