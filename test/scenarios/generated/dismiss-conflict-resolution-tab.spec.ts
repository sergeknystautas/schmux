import { test, expect } from './coverage-fixture';
import {
  seedConfig,
  spawnSession,
  waitForDashboardLive,
  waitForHealthy,
  sleep,
  waitForSessionRunning,
} from './helpers';
import WS from 'ws';

const BASE_URL = 'http://localhost:7337';

test.describe.serial('Dismiss conflict resolution tab after completion', () => {
  let sessionId: string;
  let workspaceId: string;

  test.beforeAll(async () => {
    await waitForHealthy();

    // Create a repo with divergent branches that conflict on file.txt.
    // This ensures conflict resolution will fail (no LLM configured),
    // giving us a stable "failed" state to test the dismiss behavior.
    const { execSync } = await import('child_process');
    const repoDir = '/tmp/schmux-test-repos/test-repo-cr-dismiss';
    execSync(`rm -rf ${repoDir} && mkdir -p ${repoDir}`);
    execSync(`git init -b main ${repoDir}`);
    execSync(`git -C ${repoDir} config user.email "test@schmux.dev"`);
    execSync(`git -C ${repoDir} config user.name "Schmux Test"`);
    execSync(`printf 'original content\\n' > ${repoDir}/file.txt`);
    execSync(`git -C ${repoDir} add .`);
    execSync(`git -C ${repoDir} commit -m "initial"`);

    // Create branch with a conflicting change
    execSync(`git -C ${repoDir} checkout -b test-cr-dismiss`);
    execSync(`printf 'branch content\\n' > ${repoDir}/file.txt`);
    execSync(`git -C ${repoDir} add .`);
    execSync(`git -C ${repoDir} commit -m "branch change"`);

    // Add a conflicting commit on main (same file, different content)
    execSync(`git -C ${repoDir} checkout main`);
    execSync(`printf 'main content\\n' > ${repoDir}/file.txt`);
    execSync(`git -C ${repoDir} add .`);
    execSync(`git -C ${repoDir} commit -m "main change"`);

    await seedConfig({
      repos: [repoDir],
      agents: [
        {
          name: 'echo-agent',
          command: "sh -c 'echo hello; sleep 600'",
          promptable: true,
        },
      ],
    });

    const results = await spawnSession({
      repo: repoDir,
      branch: 'test-cr-dismiss',
      prompt: 'test',
      targets: { 'echo-agent': 1 },
    });
    sessionId = results[0].session_id;
    workspaceId = results[0].workspace_id;

    await waitForSessionRunning(sessionId);

    // Trigger conflict resolution via API. Accept 202 (started) or
    // 409 (already in progress from auto-trigger during spawn).
    const resolveRes = await fetch(
      `${BASE_URL}/api/workspaces/${workspaceId}/linear-sync-resolve-conflict`,
      { method: 'POST', headers: { 'Content-Type': 'application/json' } }
    );
    if (resolveRes.status !== 202 && resolveRes.status !== 409) {
      throw new Error(`Expected 202 or 409 from resolve-conflict, got ${resolveRes.status}`);
    }

    // Wait for the resolution to reach terminal state (done or failed)
    // by listening on the dashboard WebSocket instead of using a fixed sleep
    await new Promise<void>((resolve, reject) => {
      const ws = new WS('ws://localhost:7337/ws/dashboard');
      const timer = setTimeout(() => {
        ws.close();
        reject(new Error('CR state not terminal after 30s'));
      }, 30_000);

      ws.on('message', (data: WS.Data) => {
        try {
          const msg = JSON.parse(data.toString());
          if (
            msg.type === 'linear_sync_resolve_conflict' &&
            msg.workspace_id === workspaceId &&
            (msg.status === 'done' || msg.status === 'failed')
          ) {
            clearTimeout(timer);
            ws.close();
            resolve();
          }
        } catch {
          // ignore non-JSON
        }
      });

      ws.on('error', (err: Error) => {
        clearTimeout(timer);
        reject(new Error(`Dashboard WS error: ${err.message}`));
      });
    });
  });

  test('conflict resolution tab disappears on dismiss and stays gone', async ({ page }) => {
    // Navigate directly to the session page (NOT the resolve-conflict page)
    // so the dismiss button on the conflict tab is visible.
    await page.goto(`/sessions/${sessionId}`);
    await waitForDashboardLive(page);

    // The conflict tab should show with the dismiss button (failed state).
    // Use exact: true to avoid matching the outer tab wrapper which also
    // contains "Dismiss conflict resolution" in its accessible name.
    const dismissButton = page.getByRole('button', {
      name: 'Dismiss conflict resolution',
      exact: true,
    });
    await expect(dismissButton).toBeVisible({ timeout: 15000 });

    // Click dismiss
    await dismissButton.click();

    // Tab should disappear immediately
    await expect(dismissButton).not.toBeVisible({ timeout: 5000 });

    // Wait for several WebSocket broadcast cycles to ensure stale
    // re-broadcasts don't bring the tab back (the core bug this fixes).
    await sleep(2000);

    // The tab should still be gone — this is the key verification.
    // Before the fix, stale WS broadcasts would re-add the dismissed state.
    await expect(dismissButton).not.toBeVisible();

    // Verify the session tabs still work normally
    const sessionTab = page.locator('.session-tab').first();
    await expect(sessionTab).toBeVisible();
  });

  test('DELETE API confirms state was dismissed', async () => {
    // The dismiss click in the previous test sent the DELETE request.
    // Verify by trying again — should return 404 (already gone).
    const res = await fetch(
      `${BASE_URL}/api/workspaces/${workspaceId}/linear-sync-resolve-conflict-state`,
      { method: 'DELETE' }
    );
    expect(res.status).toBe(404);
  });
});
