/**
 * Coverage-aware Playwright test fixture.
 *
 * Extends the default `test` to extract Istanbul coverage data
 * (window.__coverage__) after each test when COVERAGE_DIR is set.
 * The collected per-test JSON files are merged by nyc in the entrypoint.
 */
import { test as base, expect } from './fixtures';
import { writeFileSync, mkdirSync } from 'fs';
import { join } from 'path';

export type { Page, Locator, BrowserContext } from '@playwright/test';
export { expect };

let coverageIndex = 0;

export const test = base.extend({
  page: async ({ page }, use, workerInfo) => {
    await use(page);

    const coverageDir = process.env.COVERAGE_DIR;
    if (!coverageDir) return;

    try {
      const coverage = await page.evaluate(
        () => (window as unknown as { __coverage__?: unknown }).__coverage__
      );
      if (coverage) {
        mkdirSync(coverageDir, { recursive: true });
        writeFileSync(
          join(coverageDir, `coverage-w${workerInfo.workerIndex}-${coverageIndex++}.json`),
          JSON.stringify(coverage)
        );
      }
    } catch {
      // Page may have already closed — that's OK, we just miss this test's coverage
    }
  },
});
