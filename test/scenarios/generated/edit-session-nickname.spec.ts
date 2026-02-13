import { test, expect } from '@playwright/test';
import {
  seedConfig,
  createTestRepo,
  getSessions,
  spawnSession,
  waitForDashboardLive,
  waitForHealthy,
  sleep,
} from './helpers';

test.describe.serial('Edit a session nickname', () => {
  let repoPath: string;
  let sessionId: string;

  test.beforeAll(async () => {
    await waitForHealthy();
    repoPath = await createTestRepo('test-repo-nickname');
    await seedConfig({
      repos: [repoPath],
      agents: [
        {
          name: 'echo-agent',
          command: "sh -c 'echo hello from agent; sleep 600'",
          promptable: true,
        },
      ],
    });

    // Spawn a session via API
    const results = await spawnSession({
      repo: repoPath,
      branch: 'test-branch',
      prompt: 'test',
      targets: { 'echo-agent': 1 },
    });
    sessionId = results[0].session_id;

    // Wait for the session to be fully running
    await sleep(2000);
  });

  test('edit session nickname via the UI', async ({ page }) => {
    // Navigate to the session detail page
    await page.goto(`/sessions/${sessionId}`);
    await waitForDashboardLive(page);

    // Wait for the session detail page to fully render
    await page.waitForSelector('[data-testid="terminal-viewport"]', { timeout: 15000 });
    await expect(page.locator('[data-testid="session-sidebar"]')).toBeVisible();

    // Verify the nickname area is visible in the sidebar
    const nicknameField = page.locator('[data-testid="session-nickname"]');
    await expect(nicknameField).toBeVisible();

    // Click the edit/add button (pencil or plus icon) next to the nickname field
    const editButton = page.locator('[data-testid="session-nickname"] button').first();
    await editButton.click();

    // Wait for the modal prompt dialog to appear with the input field
    const promptInput = page.locator('#modal-prompt-input');
    await expect(promptInput).toBeVisible({ timeout: 5000 });

    // Type the new nickname
    await promptInput.fill('my-test-session');

    // Click the Save button to confirm
    await page.getByRole('button', { name: 'Save' }).click();

    // Verify the nickname updates in the sidebar
    await expect(nicknameField.locator('.metadata-field__value')).toHaveText('my-test-session', {
      timeout: 5000,
    });
  });

  test('API confirms nickname was updated', async () => {
    const workspaces = await getSessions();

    // Find the session we edited
    let foundSession: { id: string; nickname: string; target: string } | undefined;
    for (const ws of workspaces) {
      const session = ws.sessions.find((s) => s.id === sessionId);
      if (session) {
        foundSession = session;
        break;
      }
    }

    expect(foundSession).toBeDefined();
    expect(foundSession!.nickname).toBe('my-test-session');
  });
});
