import { defineConfig } from '@playwright/test';
import { cpus } from 'os';

export default defineConfig({
  testDir: '.',
  testMatch: '*.spec.ts',
  // Benchmark specs never run in the scenario gate: machine timing must not
  // pass or fail CI (docs/testing.md rule 8). They are selected only by
  // playwright.bench.config.ts via `./test.sh --bench`.
  testIgnore: '**/*.bench.spec.ts',
  timeout: 60_000,
  retries: 1,
  workers: parseInt(process.env.TEST_WORKERS || '') || Math.max(1, Math.floor(cpus().length / 2)),
  use: {
    viewport: { width: 1280, height: 1080 },
    screenshot: 'only-on-failure',
    trace: 'retain-on-failure',
    video: process.env.RECORD_VIDEO ? 'on' : 'retain-on-failure',
  },
  reporter: [['list'], ['html', { open: 'never' }]],
});
