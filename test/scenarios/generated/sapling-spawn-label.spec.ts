import { test, expect } from './coverage-fixture';
import {
  seedConfig,
  createSaplingTestRepo,
  waitForDashboardLive,
  waitForHealthy,
  waitForSessionRunning,
  apiGet,
  sleep,
} from './helpers';

function getBaseURL(): string {
  return process.env.SCHMUX_BASE_URL || 'http://localhost:7337';
}

const REPO_NAME = 'test-sapling-label';

interface WorkspaceItem {
  id: string;
  repo: string;
  branch: string;
  label?: string;
  vcs?: string;
}

interface ConfigResponse {
  models?: Array<{ id: string; configured?: boolean }>;
}

async function findWorkspaceByLabel(label: string): Promise<WorkspaceItem | undefined> {
  const workspaces = await apiGet<WorkspaceItem[]>('/api/sessions');
  return workspaces.find((w) => w.label === label);
}

async function findWorkspaceById(id: string): Promise<WorkspaceItem | undefined> {
  const workspaces = await apiGet<WorkspaceItem[]>('/api/sessions');
  return workspaces.find((w) => w.id === id);
}

async function spawnSaplingWorkspace(
  repoPath: string,
  workspaceLabel: string
): Promise<WorkspaceItem> {
  const spawnRes = await fetch(`${getBaseURL()}/api/spawn`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({
      repo: repoPath,
      branch: '',
      workspace_label: workspaceLabel,
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
  const workspaceId = results[0].workspace_id;

  // Poll until the workspace is visible in /api/sessions.
  const deadline = Date.now() + 30_000;
  while (Date.now() < deadline) {
    const ws = await findWorkspaceById(workspaceId);
    if (ws) return ws;
    await sleep(250);
  }
  throw new Error(`Workspace ${workspaceId} did not appear in /api/sessions within 30s`);
}

test.describe.serial('Sapling workspace spawn with optional Label', () => {
  let repoPath: string;
  let hasModelsInCatalog = false;

  test.beforeAll(async () => {
    await waitForHealthy();

    // Verify sapling is installed (hard-error if missing — matches the
    // existing sapling-workspace-guards convention).
    const { execSync } = await import('child_process');
    execSync('sl version', { stdio: 'pipe' });

    repoPath = await createSaplingTestRepo(REPO_NAME);

    await seedConfig({
      repoConfigs: [{ name: REPO_NAME, url: repoPath, vcs: 'sapling' }],
      agents: [{ name: 'sleep-agent', command: "sh -c 'sleep 600'" }],
      saplingCommands: {
        create_workspace: ['cp', '-r', '{{.RepoBasePath}}', '{{.DestPath}}'],
        remove_workspace: ['rm', '-rf', '{{.WorkspacePath}}'],
        create_repo_base: ['cp', '-r', '{{.RepoIdentifier}}', '{{.BasePath}}'],
      },
    });

    // The spawn page UI gates the agent/repo selectors on the detected model
    // catalog being non-empty. The scenario container does not install
    // claude/codex/gemini, so the catalog is empty and those selectors do not
    // render. Detect this and skip the UI-only assertions in that case; the
    // sidebar/API assertions still run unconditionally.
    const cfg = await apiGet<ConfigResponse>('/api/config');
    hasModelsInCatalog = (cfg.models || []).length > 0;
  });

  test('branch input is hidden and a label input appears when sapling repo is selected', async ({
    page,
  }) => {
    test.skip(
      !hasModelsInCatalog,
      'Spawn page repo/agent selectors require detected models; none available in this environment.'
    );

    await page.goto('/spawn');
    await waitForDashboardLive(page);

    const repoSelect = page.locator('[data-testid="spawn-repo-select"]').first();
    await expect(repoSelect).toBeVisible({ timeout: 15_000 });
    await repoSelect.selectOption(repoPath);

    // Branch input must NOT be visible for sapling repos.
    await expect(page.locator('input#branch')).toHaveCount(0);

    // Label input must be visible with a placeholder matching the
    // prospective workspace ID (e.g. `test-sapling-label-001`).
    const labelInput = page.locator('[data-testid="workspace-label-input"]').first();
    await expect(labelInput).toBeVisible();
    const placeholder = await labelInput.getAttribute('placeholder');
    expect(placeholder).toMatch(new RegExp(`^${REPO_NAME}-\\d{3}$`));
  });

  test('spawning without a label shows the workspace ID in the sidebar', async ({ page }) => {
    // Spawn via API with empty label. The sidebar receives the workspace via
    // WebSocket regardless of how the spawn was triggered, so the rendering
    // assertion exercises the production label resolution path.
    const ws = await spawnSaplingWorkspace(repoPath, '');
    expect(ws.label || '').toBe('');
    expect(ws.branch).toBe('');
    expect(ws.vcs).toBe('sapling');

    await waitForSessionRunning();

    await page.goto('/');
    await waitForDashboardLive(page);

    // The sidebar entry for this workspace renders the workspace ID, since
    // there is no label and no branch.
    const sidebarEntry = page.locator('.nav-workspace__name').filter({ hasText: ws.id });
    await expect(sidebarEntry.first()).toBeVisible({ timeout: 15_000 });
  });

  test('spawning with a typed label shows the label in the sidebar', async ({ page }) => {
    const TYPED_LABEL = 'Login bug fix';

    // Spawn via API with a label. The UI rendering of the sidebar still
    // verifies the workspaceDisplayLabel chain (label > branch > id).
    const ws = await spawnSaplingWorkspace(repoPath, TYPED_LABEL);
    expect(ws.label).toBe(TYPED_LABEL);
    expect(ws.branch).toBe('');

    await waitForSessionRunning();

    await page.goto('/');
    await waitForDashboardLive(page);

    // The sidebar shows the typed label, NOT the workspace ID.
    const labelledEntry = page.locator('.nav-workspace__name').filter({ hasText: TYPED_LABEL });
    await expect(labelledEntry.first()).toBeVisible({ timeout: 15_000 });

    // And the workspace ID does NOT appear as a label for this workspace —
    // the labelled entry should only contain the label text.
    const labelledText = await labelledEntry.first().innerText();
    expect(labelledText).toContain(TYPED_LABEL);
    expect(labelledText).not.toContain(ws.id);
  });

  test('workspace API reflects empty branch and the typed label', async () => {
    const TYPED_LABEL = 'Login bug fix';

    const ws = await findWorkspaceByLabel(TYPED_LABEL);
    expect(ws, `no workspace with label "${TYPED_LABEL}" found`).toBeDefined();
    expect(ws!.branch).toBe('');
    expect(ws!.label).toBe(TYPED_LABEL);
    expect(ws!.vcs).toBe('sapling');
  });
});
