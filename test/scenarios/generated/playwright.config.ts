import { defineConfig } from '@playwright/test';

export default defineConfig({
  testDir: '.',
  testMatch: '*.spec.ts',
  timeout: 60_000,
  retries: 1,
  workers: 1,
  globalSetup: './global-setup.ts',
  use: {
    baseURL: 'http://localhost:7337',
    screenshot: 'only-on-failure',
    trace: 'retain-on-failure',
    video: process.env.RECORD_VIDEO ? 'on' : 'retain-on-failure',
  },
  reporter: [['list'], ['html', { open: 'never' }]],
});
