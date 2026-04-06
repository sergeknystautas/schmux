import { test, expect } from './coverage-fixture';
import { execSync } from 'child_process';
import {
  seedConfig,
  createTestRepo,
  waitForDashboardLive,
  waitForHealthy,
  apiGet,
  apiPost,
} from './helpers';

const SCHMUX_HOME = process.env.SCHMUX_HOME || `${process.env.HOME}/.schmux`;

/** Seed a proposal JSON file directly on disk so the daemon picks it up. */
function seedProposal(repoName: string, proposal: Record<string, unknown>): void {
  const proposalDir = `${SCHMUX_HOME}/lore-proposals/${repoName}`;
  execSync(`mkdir -p ${proposalDir}`);
  const json = JSON.stringify(proposal, null, 2);
  const id = proposal.id as string;
  execSync(`cat > ${proposalDir}/${id}.json << 'PROPEOF'\n${json}\nPROPEOF`);
}

test.describe.serial('Lore card review flow', () => {
  const repoName = 'test-lore-review';

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

    // Seed a proposal with two pending rules
    seedProposal(repoName, {
      id: 'prop-review-001',
      repo: repoName,
      created_at: new Date().toISOString(),
      status: 'pending',
      rules: [
        {
          id: 'r1',
          text: 'Always run tests before committing',
          category: 'workflow',
          suggested_layer: 'repo_private',
          status: 'pending',
          source_entries: [],
        },
        {
          id: 'r2',
          text: 'Use go build ./cmd/schmux for building',
          category: 'build',
          suggested_layer: 'repo_public',
          status: 'pending',
          source_entries: [],
        },
      ],
    });
  });

  test('cards appear on lore page', async ({ page }) => {
    await page.goto('/lore');
    await waitForDashboardLive(page);

    // Both rule texts should be visible as cards
    await expect(page.locator('text=Always run tests before committing')).toBeVisible();
    await expect(page.locator('text=Use go build ./cmd/schmux for building')).toBeVisible();
  });

  test('approve changes card to collapsed approved state', async ({ page }) => {
    await page.goto('/lore');
    await waitForDashboardLive(page);

    // Wait for the first card to appear
    await expect(page.locator('[data-testid="lore-card-r1"]')).toBeVisible();

    // Click the Approve button on the first card (r1)
    const firstCard = page.locator('[data-testid="lore-card-r1"]');
    const approveButton = firstCard.locator('button', { hasText: 'Approve' });
    await approveButton.click();

    // After approval, the card should collapse and show a check mark
    const collapsedCard = page.locator('[data-testid="lore-card-r1"]');
    await expect(collapsedCard).toBeVisible();
    // The collapsed card contains the check mark character
    await expect(collapsedCard.locator('text=\u2713')).toBeVisible();
    // The rule text (truncated or full) should still be visible in collapsed form
    await expect(collapsedCard).toContainText('Always run tests');
  });

  test('dismiss removes card from view', async ({ page }) => {
    await page.goto('/lore');
    await waitForDashboardLive(page);

    // Wait for the second card (r2) to appear — r1 was approved in the previous test
    // so it shows as collapsed. r2 should still be a full pending card.
    const secondCard = page.locator('[data-testid="lore-card-r2"]');
    await expect(secondCard).toBeVisible();

    // Click Dismiss on the second card
    const dismissButton = secondCard.locator('button', { hasText: 'Dismiss' });
    await dismissButton.click();

    // Wait for the dismiss animation (200ms transition + buffer)
    await page.waitForTimeout(400);

    // The card should no longer display the rule text
    await expect(page.locator('text=Use go build ./cmd/schmux for building')).toBeHidden();
  });

  test('API confirms rule statuses persisted', async () => {
    interface Proposal {
      id: string;
      status: string;
      rules: Array<{ id: string; status: string; text: string }>;
    }

    const proposal = await apiGet<Proposal>(
      `/api/lore/${encodeURIComponent(repoName)}/proposals/prop-review-001`
    );

    expect(proposal.id).toBe('prop-review-001');

    const r1 = proposal.rules.find((r) => r.id === 'r1');
    const r2 = proposal.rules.find((r) => r.id === 'r2');

    expect(r1).toBeDefined();
    expect(r1!.status).toBe('approved');

    expect(r2).toBeDefined();
    expect(r2!.status).toBe('dismissed');
  });

  test('edit updates rule text', async ({ page }) => {
    // Seed a fresh proposal for the edit test
    seedProposal(repoName, {
      id: 'prop-review-002',
      repo: repoName,
      created_at: new Date().toISOString(),
      status: 'pending',
      rules: [
        {
          id: 'r-edit',
          text: 'Original rule text for editing',
          category: 'testing',
          suggested_layer: 'repo_private',
          status: 'pending',
          source_entries: [],
        },
      ],
    });

    await page.goto('/lore');
    await waitForDashboardLive(page);

    // Wait for the new card to appear
    const editCard = page.locator('[data-testid="lore-card-r-edit"]');
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

    // Verify via API that the text was persisted
    interface Proposal {
      rules: Array<{ id: string; text: string }>;
    }
    const proposal = await apiGet<Proposal>(
      `/api/lore/${encodeURIComponent(repoName)}/proposals/prop-review-002`
    );
    const rule = proposal.rules.find((r) => r.id === 'r-edit');
    expect(rule).toBeDefined();
    expect(rule!.text).toBe('Updated rule text after editing');
  });

  test('duplicate rules show as single card', async ({ page }) => {
    // Seed two proposals with rules that have the same normalized text
    seedProposal(repoName, {
      id: 'prop-dedup-a',
      repo: repoName,
      created_at: new Date().toISOString(),
      status: 'pending',
      rules: [
        {
          id: 'r-dup-a',
          text: 'A unique dedup test rule',
          category: 'testing',
          suggested_layer: 'repo_private',
          status: 'pending',
          source_entries: [],
        },
      ],
    });

    seedProposal(repoName, {
      id: 'prop-dedup-b',
      repo: repoName,
      created_at: new Date().toISOString(),
      status: 'pending',
      rules: [
        {
          id: 'r-dup-b',
          text: 'a unique dedup test rule',
          category: 'testing',
          suggested_layer: 'repo_private',
          status: 'pending',
          source_entries: [],
        },
      ],
    });

    await page.goto('/lore');
    await waitForDashboardLive(page);

    // Wait for data to load — at least one instance must be visible
    await expect(page.locator('text=unique dedup test rule').first()).toBeVisible();

    // The LorePage deduplicates by normalized text (case-insensitive, whitespace-collapsed).
    // Only one card should appear, not two.
    const cards = page.locator('text=unique dedup test rule');
    await expect(cards).toHaveCount(1);
  });

  test('approved rules do not appear on page reload', async ({ page }) => {
    // Approve a rule via API, then verify the card wall filters it out.
    // The card wall only shows pending rules — approved ones are "triaged" and hidden.
    await apiPost(
      `/api/lore/${encodeURIComponent(repoName)}/proposals/prop-review-002/rules/r-edit`,
      { status: 'approved' }
    );

    await page.goto('/lore');
    await waitForDashboardLive(page);

    // The r-edit card should NOT appear — loadData skips non-pending rules
    await expect(page.locator('[data-testid="lore-card-r-edit"]')).toBeHidden();
  });
});
