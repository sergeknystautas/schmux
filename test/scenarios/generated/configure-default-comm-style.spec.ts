import { test, expect } from './coverage-fixture';
import {
  seedConfig,
  createTestRepo,
  apiGet,
  apiPost,
  getConfig,
  resetConfig,
  waitForHealthy,
} from './helpers';

test.describe('Configure default communication style per agent type', () => {
  let repoPath: string;
  let savedConfig: Record<string, unknown>;

  test.beforeAll(async () => {
    await waitForHealthy();
    repoPath = await createTestRepo('style-config-repo');
    savedConfig = await getConfig();

    await seedConfig({
      repos: [repoPath],
      agents: [
        {
          name: 'echo-agent',
          command: "sh -c 'echo hello; sleep 600'",
        },
      ],
    });
  });

  test.afterAll(async () => {
    await resetConfig(savedConfig);
  });

  test('set comm_styles via API', async () => {
    // Set a default style for an agent type via API
    await apiPost('/api/config', { comm_styles: { 'echo-agent': 'pirate' } });

    // Verify: GET /api/config returns the updated comm_styles
    const config = await apiGet<{ comm_styles?: Record<string, string> }>('/api/config');
    expect(config.comm_styles).toBeDefined();
    expect(config.comm_styles!['echo-agent']).toBe('pirate');
  });

  test('update comm_styles to a different style', async () => {
    await apiPost('/api/config', { comm_styles: { 'echo-agent': 'caveman' } });

    const config = await apiGet<{ comm_styles?: Record<string, string> }>('/api/config');
    expect(config.comm_styles).toBeDefined();
    expect(config.comm_styles!['echo-agent']).toBe('caveman');
  });

  test('clear comm_styles', async () => {
    await apiPost('/api/config', { comm_styles: {} });

    const config = await apiGet<{ comm_styles?: Record<string, string> }>('/api/config');
    // comm_styles should be empty or undefined
    if (config.comm_styles) {
      expect(Object.keys(config.comm_styles)).toHaveLength(0);
    }
  });

  test('set multiple agent type defaults', async () => {
    await apiPost('/api/config', {
      comm_styles: { 'echo-agent': 'pirate', claude: 'butler' },
    });

    const config = await apiGet<{ comm_styles?: Record<string, string> }>('/api/config');
    expect(config.comm_styles).toBeDefined();
    expect(config.comm_styles!['echo-agent']).toBe('pirate');
    expect(config.comm_styles!['claude']).toBe('butler');
  });
});
