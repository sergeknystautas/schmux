import { defineConfig } from '@playwright/test';

// Benchmark-only config, selected by entrypoint.sh when BENCH_BROWSER=1
// (set by `./test.sh --bench`). Selection and parallelism are pinned: one
// worker keeps sample counts clean, zero retries never hides a lost sample.
// TEST_WORKERS / TEST_GREP / TEST_REPEAT are deliberately not honored here —
// benchmark runs are not ambient.
export default defineConfig({
  testDir: '.',
  testMatch: '*.bench.spec.ts',
  timeout: 180_000,
  workers: 1,
  retries: 0,
  use: {
    viewport: { width: 1280, height: 1080 },
    screenshot: 'only-on-failure',
    trace: 'retain-on-failure',
  },
  reporter: [['list'], ['html', { open: 'never' }]],
});
