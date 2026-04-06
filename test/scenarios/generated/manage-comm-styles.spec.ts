import { test, expect } from './coverage-fixture';
import { apiGet, apiPost, waitForDashboardLive, waitForHealthy } from './helpers';
function getBaseURL(): string {
  return process.env.SCHMUX_BASE_URL || 'http://localhost:7337';
}

interface StyleItem {
  id: string;
  name: string;
  icon: string;
  tagline: string;
  prompt: string;
  built_in: boolean;
}

interface StyleListResponse {
  styles: StyleItem[];
}

test.describe('Manage communication styles', () => {
  const customId = 'test-custom';

  test.beforeAll(async () => {
    await waitForHealthy();

    // Clean up any leftover custom style from prior runs
    try {
      await fetch(`${getBaseURL()}/api/styles/test-custom`, {
        method: 'DELETE',
      });
    } catch {
      // ignore
    }
  });

  test('sidebar link and styles list page', async ({ page }) => {
    await page.goto('/');
    await waitForDashboardLive(page);

    // Verify: sidebar shows "Comm Styles" link
    const navLink = page.locator('.tools-section__list a', { hasText: 'Comm Styles' });
    await expect(navLink).toBeVisible();

    // Click the sidebar link
    await navLink.click();
    await page.waitForURL('/styles');
    expect(page.url()).toMatch(/\/styles$/);

    // Verify: style grid shows at least 25 built-in cards
    await page.waitForSelector('[data-testid="style-grid"]', { timeout: 10_000 });
    const cards = page.locator('[data-testid^="style-card-"]');
    const count = await cards.count();
    expect(count).toBeGreaterThanOrEqual(25);
  });

  test('create a custom style', async ({ page }) => {
    await page.goto('/styles/create');
    await waitForDashboardLive(page);

    // Fill in the form
    await page.fill('#style-name', 'Test Custom');
    await page.fill('#style-icon', '🧪');
    await page.fill('#style-tagline', 'A test style for scenarios');
    await page.fill('#style-prompt', 'Communicate in a test-friendly way.');

    // Click Create
    await page.click('button:has-text("Create")');

    // Should navigate back to /styles
    await page.waitForURL('/styles');

    // Verify via API
    const data = await apiGet<StyleListResponse>('/api/styles');
    const custom = data.styles.find((s) => s.id === customId);
    expect(custom).toBeDefined();
    expect(custom!.name).toBe('Test Custom');
    expect(custom!.icon).toBe('🧪');
  });

  test('edit a custom style', async ({ page }) => {
    await page.goto(`/styles/${customId}`);
    await waitForDashboardLive(page);

    // Wait for form to load
    await page.waitForSelector('[data-testid="style-form"]', { timeout: 10_000 });

    // Change the tagline
    await page.fill('#style-tagline', 'Updated tagline');
    await page.click('button:has-text("Save Changes")');

    // Should navigate back to /styles
    await page.waitForURL('/styles');

    // Verify via API
    const data = await apiGet<StyleListResponse>('/api/styles');
    const updated = data.styles.find((s) => s.id === customId);
    expect(updated).toBeDefined();
    expect(updated!.tagline).toBe('Updated tagline');
  });

  test('delete a custom style removes it', async ({ page }) => {
    await page.goto('/styles');
    await waitForDashboardLive(page);
    await page.waitForSelector(`[data-testid="style-card-${customId}"]`, { timeout: 10_000 });

    // Click the close/delete button on the custom style card
    const card = page.locator(`[data-testid="style-card-${customId}"]`);
    const deleteBtn = card.locator('.persona-card__close');
    await deleteBtn.click();

    // Confirm the deletion modal
    await page.click('button.btn--danger:has-text("Confirm")');

    // Verify via API: custom style is gone
    const data = await apiGet<StyleListResponse>('/api/styles');
    const deleted = data.styles.find((s) => s.id === customId);
    expect(deleted).toBeUndefined();
  });

  test('delete a built-in style resets it', async ({ page }) => {
    // Get the pirate style's original prompt
    const before = await apiGet<StyleListResponse>('/api/styles');
    const pirateBefore = before.styles.find((s) => s.id === 'pirate');
    expect(pirateBefore).toBeDefined();
    expect(pirateBefore!.built_in).toBe(true);

    await page.goto('/styles');
    await waitForDashboardLive(page);
    await page.waitForSelector('[data-testid="style-card-pirate"]', { timeout: 10_000 });

    // Click delete on pirate
    const card = page.locator('[data-testid="style-card-pirate"]');
    const deleteBtn = card.locator('.persona-card__close');
    await deleteBtn.click();

    // Confirm
    await page.click('button.btn--danger:has-text("Confirm")');

    // Verify: pirate still exists (was reset, not deleted)
    const after = await apiGet<StyleListResponse>('/api/styles');
    const pirateAfter = after.styles.find((s) => s.id === 'pirate');
    expect(pirateAfter).toBeDefined();
    expect(pirateAfter!.built_in).toBe(true);
  });
});
