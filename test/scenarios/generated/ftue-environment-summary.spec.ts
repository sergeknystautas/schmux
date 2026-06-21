import { test, expect } from './coverage-fixture';
import { apiGet, seedConfig, waitForHealthy, waitForDashboardLive } from './helpers';

interface Dependency {
  id: string;
  detected: boolean;
}

interface DependencyGroup {
  id: string;
  dependencies: Dependency[];
}

interface DependenciesResponse {
  groups: DependencyGroup[];
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
    await waitForDashboardLive(page);

    // Wait for the environment summary to appear. This implicitly waits for
    // WebSocket connection + config/session loading + the dependencies fetch, so
    // use a generous timeout to avoid flaking under parallel load.
    const envSummary = page.locator('[data-testid="env-summary"]');
    await expect(envSummary).toBeVisible({ timeout: 30_000 });

    // Verify: no redirect to /config — still on /
    expect(page.url()).toMatch(/\/$/);

    // Verify: the git badge is shown (git is available in the test env)
    const gitBadge = page.locator('[data-testid="env-badge-git"]');
    await expect(gitBadge.first()).toBeVisible();

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

  test('dependencies API returns the vcs group with git detected', async () => {
    const deps = await apiGet<DependenciesResponse>('/api/dependencies');

    const vcs = deps.groups.find((g) => g.id === 'vcs');
    expect(vcs).toBeDefined();
    // git must be available (required for workspace operations)
    const git = vcs!.dependencies.find((d) => d.id === 'git');
    expect(git).toBeDefined();
    expect(git!.detected).toBe(true);
  });
});
