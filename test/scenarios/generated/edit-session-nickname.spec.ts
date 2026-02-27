import { test, expect } from './coverage-fixture';
import {
  seedConfig,
  createTestRepo,
  getSessions,
  spawnSession,
  waitForDashboardLive,
  waitForHealthy,
  waitForSessionRunning,
} from './helpers';

test.describe.serial('Edit a session nickname', () => {
  let repoPath: string;
  let sessionId: string;
  // Use a unique nickname to avoid collisions across repeated test runs
  // (the daemon is shared and previous sessions may still exist)
  const testNickname = `test-nick-${Date.now()}`;

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
    await waitForSessionRunning(sessionId);
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
    await promptInput.fill(testNickname);

    // Click the Save button to confirm
    await page.getByRole('button', { name: 'Save' }).click();

    // Verify the nickname updates in the sidebar
    await expect(nicknameField.locator('.metadata-field__value')).toHaveText(testNickname, {
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
    expect(foundSession!.nickname).toBe(testNickname);
  });
});
