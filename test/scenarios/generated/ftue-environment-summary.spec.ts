import { test, expect } from './coverage-fixture';
import { apiGet, seedConfig, sleep, waitForHealthy } from './helpers';

interface DetectionSummary {
  status: string;
  agents: Array<{ name: string; command: string; source: string }>;
  vcs: Array<{ name: string; path: string }>;
  tmux: { available: boolean; path?: string };
}

/** Wait for tool detection to complete before opening the page. */
async function waitForDetectionReady(timeoutMs = 30_000): Promise<void> {
  const start = Date.now();
  while (Date.now() - start < timeoutMs) {
    try {
      const summary = await apiGet<DetectionSummary>('/api/detection-summary');
      if (summary.status === 'ready') return;
    } catch {
      // endpoint not ready yet
    }
    await sleep(500);
  }
  throw new Error(`Detection not ready after ${timeoutMs}ms`);
}

/** Dispose all workspaces (sessions + directories) so the home page is clean. */
async function disposeAllWorkspaces(): Promise<void> {
  const baseURL = process.env.SCHMUX_BASE_URL || 'http://localhost:7337';
  try {
    interface WS {
      id: string;
      sessions: Array<{ id: string; running: boolean }>;
    }
    const workspaces = await apiGet<WS[]>('/api/sessions');
    for (const ws of workspaces) {
      try {
        await fetch(`${baseURL}/api/workspaces/${ws.id}/dispose-all`, { method: 'POST' });
      } catch {
        // Best-effort
      }
    }
  } catch {
    // API not ready or no workspaces
  }
}

test.describe('First-time home page shows detected environment', () => {
  test.beforeAll(async () => {
    await waitForHealthy();
    await waitForDetectionReady();

    // Seed an empty config: no repos, no run_targets, no workspaces.
    // seedConfig disposes sessions but workspaces created by tests sharing
    // this worker may linger. Dispose them so the home page shows the FTUE.
    await seedConfig({
      repos: [],
      agents: [],
    });
    await disposeAllWorkspaces();
  });

  test('home page renders environment summary without redirect', async ({ page }) => {
    await page.goto('/');

    // Wait for the environment summary to appear. This implicitly waits for
    // WebSocket connection + config/session loading + detection fetch, so use
    // a generous timeout to avoid flaking under parallel load.
    const envSummary = page.locator('[data-testid="env-summary"]');
    await expect(envSummary).toBeVisible({ timeout: 30_000 });

    // Verify: no redirect to /config — still on /
    expect(page.url()).toMatch(/\/$/);

    // Verify: VCS badge is shown (git should be available in test env)
    const vcsBadges = page.locator('[data-testid="env-badge-vcs"]');
    await expect(vcsBadges.first()).toBeVisible();

    // Verify: "+ Add Workspace" CTA is visible
    const addWorkspaceCta = page.locator('[data-testid="add-workspace-cta"]');
    await expect(addWorkspaceCta).toBeVisible();

    // Verify: branches section is NOT shown
    const branches = page.locator('[data-testid="recent-branches"]');
    await expect(branches).not.toBeVisible();

    // Verify: tmux tip is NOT shown (no workspaces = no sessions to attach to)
    const tmuxTip = page.getByText('tmux -L schmux attach');
    await expect(tmuxTip).not.toBeVisible();
  });

  test('detection summary API returns ready with VCS', async () => {
    const summary = await apiGet<DetectionSummary>('/api/detection-summary');

    expect(summary.status).toBe('ready');
    // git must be available (required for workspace operations)
    expect(summary.vcs.length).toBeGreaterThan(0);
    // agents may or may not be in PATH depending on the environment
    expect(summary.agents).toBeDefined();
  });
});
