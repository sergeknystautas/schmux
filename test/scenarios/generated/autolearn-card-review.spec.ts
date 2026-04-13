import { test, expect } from './coverage-fixture';
import { execSync } from 'child_process';
import {
  seedConfig,
  createTestRepo,
  waitForDashboardLive,
  waitForHealthy,
  apiGet,
  apiPost,
  apiPatch,
} from './helpers';

const SCHMUX_HOME = process.env.SCHMUX_HOME || `${process.env.HOME}/.schmux`;

/** Seed a batch JSON file directly on disk so the daemon picks it up. */
function seedBatch(repoName: string, batch: Record<string, unknown>): void {
  const batchDir = `${SCHMUX_HOME}/autolearn/batches/${repoName}`;
  execSync(`mkdir -p ${batchDir}`);
  const json = JSON.stringify(batch, null, 2);
  const id = batch.id as string;
  execSync(`cat > ${batchDir}/${id}.json << 'BATCHEOF'\n${json}\nBATCHEOF`);
}

test.describe.serial('Autolearn card review flow', () => {
  const repoName = 'test-autolearn-review';

  test.beforeAll(async () => {
    await waitForHealthy();
    const repoPath = await createTestRepo(repoName);
    await seedConfig({
      repos: [repoPath],
      agents: [
        {
          name: 'echo-agent',
          command: "sh -c 'echo hello; sleep 600'",
        },
      ],
    });

    // Seed a batch with two pending learnings
    seedBatch(repoName, {
      id: 'batch-review-001',
      repo: repoName,
      created_at: new Date().toISOString(),
      status: 'pending',
      learnings: [
        {
          id: 'r1',
          kind: 'rule',
          status: 'pending',
          title: 'Always run tests before committing',
          category: 'workflow',
          suggested_layer: 'repo_private',
          sources: [],
          created_at: new Date().toISOString(),
          rule: {},
        },
        {
          id: 'r2',
          kind: 'rule',
          status: 'pending',
          title: 'Use go build ./cmd/schmux for building',
          category: 'build',
          suggested_layer: 'repo_public',
          sources: [],
          created_at: new Date().toISOString(),
          rule: {},
        },
      ],
    });
  });

  test('cards appear on autolearn page', async ({ page }) => {
    await page.goto('/autolearn');
    await waitForDashboardLive(page);

    // Both rule texts should be visible as cards
    await expect(page.locator('text=Always run tests before committing')).toBeVisible();
    await expect(page.locator('text=Use go build ./cmd/schmux for building')).toBeVisible();
  });

  test('approve changes card to collapsed approved state', async ({ page }) => {
    await page.goto('/autolearn');
    await waitForDashboardLive(page);

    // Wait for the first card to appear
    await expect(page.locator('[data-testid="autolearn-card-r1"]')).toBeVisible();

    // Click the Approve button on the first card (r1)
    const firstCard = page.locator('[data-testid="autolearn-card-r1"]');
    const approveButton = firstCard.locator('button', { hasText: 'Approve' });
    await approveButton.click();

    // After approval, the card should collapse and show a check mark
    const collapsedCard = page.locator('[data-testid="autolearn-card-r1"]');
    await expect(collapsedCard).toBeVisible();
    // The collapsed card contains the check mark character
    await expect(collapsedCard.locator('text=\u2713')).toBeVisible();
    // The rule text (truncated or full) should still be visible in collapsed form
    await expect(collapsedCard).toContainText('Always run tests');
  });

  test('dismiss removes card from view', async ({ page }) => {
    await page.goto('/autolearn');
    await waitForDashboardLive(page);

    // Wait for the second card (r2) to appear — r1 was approved in the previous test
    // so it shows as collapsed. r2 should still be a full pending card.
    const secondCard = page.locator('[data-testid="autolearn-card-r2"]');
    await expect(secondCard).toBeVisible();

    // Click Dismiss on the second card
    const dismissButton = secondCard.locator('button', { hasText: 'Dismiss' });
    await dismissButton.click();

    // The card should no longer display the rule text (assertion retries handle animation delay)
    await expect(page.locator('text=Use go build ./cmd/schmux for building')).toBeHidden({
      timeout: 5000,
    });
  });

  test('API confirms learning statuses persisted', async () => {
    interface Batch {
      id: string;
      status: string;
      learnings: Array<{ id: string; status: string; title: string }>;
    }

    const batch = await apiGet<Batch>(
      `/api/autolearn/${encodeURIComponent(repoName)}/batches/batch-review-001`
    );

    expect(batch.id).toBe('batch-review-001');

    const r1 = batch.learnings.find((r) => r.id === 'r1');
    const r2 = batch.learnings.find((r) => r.id === 'r2');

    expect(r1).toBeDefined();
    expect(r1!.status).toBe('approved');

    expect(r2).toBeDefined();
    expect(r2!.status).toBe('dismissed');
  });

  test('edit updates learning title', async ({ page }) => {
    // Seed a fresh batch for the edit test
    seedBatch(repoName, {
      id: 'batch-review-002',
      repo: repoName,
      created_at: new Date().toISOString(),
      status: 'pending',
      learnings: [
        {
          id: 'r-edit',
          kind: 'rule',
          title: 'Original rule text for editing',
          category: 'testing',
          suggested_layer: 'repo_private',
          status: 'pending',
          sources: [],
          created_at: new Date().toISOString(),
          rule: {},
        },
      ],
    });

    await page.goto('/autolearn');
    await waitForDashboardLive(page);

    // Wait for the new card to appear
    const editCard = page.locator('[data-testid="autolearn-card-r-edit"]');
    await expect(editCard).toBeVisible();

    // Click Edit button
    const editButton = editCard.locator('button', { hasText: 'Edit' });
    await editButton.click();

    // The textarea should appear with the original text
    const textarea = editCard.locator('textarea');
    await expect(textarea).toBeVisible();
    await expect(textarea).toHaveValue('Original rule text for editing');

    // Clear and type new text
    await textarea.fill('Updated rule text after editing');

    // Click Save
    const saveButton = editCard.locator('button', { hasText: 'Save' });
    await saveButton.click();

    // The card should now show the updated text (no longer in edit mode)
    await expect(editCard.locator('textarea')).toBeHidden();
    await expect(editCard).toContainText('Updated rule text after editing');

    // Verify via API that the title was persisted
    interface Batch {
      learnings: Array<{ id: string; title: string }>;
    }
    const batch = await apiGet<Batch>(
      `/api/autolearn/${encodeURIComponent(repoName)}/batches/batch-review-002`
    );
    const learning = batch.learnings.find((r) => r.id === 'r-edit');
    expect(learning).toBeDefined();
    expect(learning!.title).toBe('Updated rule text after editing');
  });

  test('duplicate learnings show as single card', async ({ page }) => {
    // Seed two batches with learnings that have the same normalized title
    seedBatch(repoName, {
      id: 'batch-dedup-a',
      repo: repoName,
      created_at: new Date().toISOString(),
      status: 'pending',
      learnings: [
        {
          id: 'r-dup-a',
          kind: 'rule',
          title: 'A unique dedup test rule',
          category: 'testing',
          suggested_layer: 'repo_private',
          status: 'pending',
          sources: [],
          created_at: new Date().toISOString(),
          rule: {},
        },
      ],
    });

    seedBatch(repoName, {
      id: 'batch-dedup-b',
      repo: repoName,
      created_at: new Date().toISOString(),
      status: 'pending',
      learnings: [
        {
          id: 'r-dup-b',
          kind: 'rule',
          title: 'a unique dedup test rule',
          category: 'testing',
          suggested_layer: 'repo_private',
          status: 'pending',
          sources: [],
          created_at: new Date().toISOString(),
          rule: {},
        },
      ],
    });

    await page.goto('/autolearn');
    await waitForDashboardLive(page);

    // Wait for data to load — at least one instance must be visible
    await expect(page.locator('text=unique dedup test rule').first()).toBeVisible();

    // The AutolearnPage deduplicates by normalized title (case-insensitive, whitespace-collapsed).
    // Only one card should appear, not two.
    const cards = page.locator('text=unique dedup test rule');
    await expect(cards).toHaveCount(1);
  });

  test('approved learnings do not appear on page reload', async ({ page }) => {
    // Approve a learning via API, then verify the card wall filters it out.
    // The card wall only shows pending learnings — approved ones are "triaged" and hidden.
    await apiPatch(
      `/api/autolearn/${encodeURIComponent(repoName)}/batches/batch-review-002/learnings/r-edit`,
      { status: 'approved' }
    );

    await page.goto('/autolearn');
    await waitForDashboardLive(page);

    // The r-edit card should NOT appear — loadData skips non-pending learnings
    await expect(page.locator('[data-testid="autolearn-card-r-edit"]')).toBeHidden();
  });
});
