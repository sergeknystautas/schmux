import { test, expect } from './coverage-fixture';
import {
  seedConfig,
  createTestRepo,
  waitForDashboardLive,
  waitForHealthy,
  apiGet,
  apiPost,
} from './helpers';

test.describe.serial('Persist lore curator model selection', () => {
  let repoPath: string;
  const repoName = 'test-lore-persist';
  const agentName = 'echo-agent';

  test.beforeAll(async () => {
    await waitForHealthy();
    repoPath = await createTestRepo(repoName);
    await seedConfig({
      repos: [repoPath],
      agents: [
        {
          name: agentName,
          command: "sh -c 'echo hello; sleep 600'",
        },
      ],
    });
  });

  test('API round-trip persists lore llm_target', async () => {
    interface ConfigResp {
      lore: {
        enabled: boolean;
        llm_target: string;
        curate_on_dispose: string;
        auto_pr: boolean;
      };
    }

    // POST lore config
    await apiPost('/api/config', {
      lore: {
        enabled: true,
        llm_target: agentName,
        curate_on_dispose: 'workspace',
        auto_pr: false,
      },
    });

    // GET and verify all fields round-trip
    const config = await apiGet<ConfigResp>('/api/config');
    expect(config.lore.enabled).toBe(true);
    expect(config.lore.llm_target).toBe(agentName);
    expect(config.lore.curate_on_dispose).toBe('workspace');
    expect(config.lore.auto_pr).toBe(false);
  });

  test('clearing llm_target via API returns empty string, not fallback', async () => {
    interface ConfigResp {
      lore: { llm_target: string };
    }

    // Clear the target
    await apiPost('/api/config', {
      lore: { llm_target: '' },
    });

    // GET should return empty, not a compound-target fallback
    const config = await apiGet<ConfigResp>('/api/config');
    expect(config.lore.llm_target).toBe('');
  });

  test('UI curate-on-dispose persists across page reload', async ({ page }) => {
    // Set llm_target via API (TargetSelect only shows models from the catalog,
    // which are unavailable in the test environment) and reset curate_on_dispose
    await apiPost('/api/config', {
      lore: { llm_target: agentName, curate_on_dispose: 'session' },
    });

    await page.goto('/config');
    await waitForDashboardLive(page);

    // Navigate to Advanced tab
    const advancedTab = page.locator('[data-testid="config-tab-advanced"]');
    await advancedTab.click();

    // Change curate on dispose via UI
    const curateSelect = page
      .locator('.form-group', {
        has: page.locator('.form-group__label', { hasText: 'Curate On Dispose' }),
      })
      .locator('select');
    await curateSelect.selectOption('workspace');

    // Save
    const saveButton = page.locator('[data-testid="config-save"]');
    await expect(saveButton).toBeEnabled();
    await saveButton.click();
    await expect(saveButton).toBeDisabled({ timeout: 10000 });

    // Reload the page completely
    await page.reload();
    await waitForDashboardLive(page);

    // Navigate back to Advanced tab
    const advancedTabAfterReload = page.locator('[data-testid="config-tab-advanced"]');
    await advancedTabAfterReload.click();

    // Verify curate on dispose is still workspace
    const curateSelectAfterReload = page
      .locator('.form-group', {
        has: page.locator('.form-group__label', { hasText: 'Curate On Dispose' }),
      })
      .locator('select');
    await expect(curateSelectAfterReload).toHaveValue('workspace');

    // Verify llm_target was persisted via API
    interface ConfigResp {
      lore: { llm_target: string; curate_on_dispose: string };
    }
    const config = await apiGet<ConfigResp>('/api/config');
    expect(config.lore.llm_target).toBe(agentName);
    expect(config.lore.curate_on_dispose).toBe('workspace');
  });
});
