import { test, expect } from '@playwright/test';
import {
  seedConfig,
  createTestRepo,
  spawnSession,
  waitForDashboardLive,
  waitForHealthy,
  apiGet,
  sleep,
} from './helpers';

test.describe.serial('View code changes in a workspace', () => {
  let repoPath: string;
  let workspaceId: string;

  test.beforeAll(async () => {
    await waitForHealthy();
    repoPath = await createTestRepo('test-repo-diff');

    await seedConfig({
      repos: [repoPath],
      agents: [
        {
          name: 'diff-agent',
          command: "sh -c 'echo new-line >> README.md; sleep 600'",
          promptable: true,
        },
      ],
    });

    // Spawn a session via API so the agent modifies README.md
    const results = await spawnSession({
      repo: repoPath,
      branch: 'test-branch',
      prompt: 'test',
      targets: { 'diff-agent': 1 },
    });
    workspaceId = results[0].workspace_id;

    // Wait for the agent to start and modify the file
    await sleep(2000);
  });

  test('diff page shows file list and viewer', async ({ page }) => {
    await page.goto(`/diff/${workspaceId}`);
    await waitForDashboardLive(page);

    // Wait for the file list to render with diff data
    await page.waitForSelector('[data-testid="diff-file-list"]', { timeout: 15000 });

    // Verify: file list sidebar is visible
    await expect(page.locator('[data-testid="diff-file-list"]')).toBeVisible();

    // Verify: at least one changed file appears in the file list
    const fileRows = page.locator('[data-testid^="diff-file-"]');
    await expect(fileRows.first()).toBeVisible({ timeout: 10000 });
    const count = await fileRows.count();
    expect(count).toBeGreaterThanOrEqual(1);

    // Verify: diff viewer is visible with content
    await expect(page.locator('[data-testid="diff-viewer"]')).toBeVisible();
  });

  test('API returns diff data', async () => {
    interface FileDiff {
      old_path?: string;
      new_path?: string;
      old_content?: string;
      new_content?: string;
      status?: string;
      lines_added?: number;
      lines_removed?: number;
    }

    interface DiffResponse {
      workspace_id: string;
      repo: string;
      branch: string;
      files: FileDiff[];
    }

    const response = await apiGet<DiffResponse>(`/api/diff/${workspaceId}`);

    // Verify: response contains files array with at least one entry
    expect(response.files).toBeDefined();
    expect(Array.isArray(response.files)).toBe(true);
    expect(response.files.length).toBeGreaterThanOrEqual(1);

    // Verify: at least one file is README.md
    const readmeFile = response.files.find(
      (f) => f.new_path === 'README.md' || f.old_path === 'README.md'
    );
    expect(readmeFile).toBeDefined();
  });
});
