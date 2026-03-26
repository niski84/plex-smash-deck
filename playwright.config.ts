import { defineConfig } from '@playwright/test';

export default defineConfig({
  testDir: './scripts',
  testMatch: '**/*.spec.ts',
  timeout: 120_000,
  use: {
    baseURL: 'http://127.0.0.1:8081',
    headless: true,
    viewport: { width: 1400, height: 860 },
    // Capture a screenshot for every step so we can build a GIF
    screenshot: 'on',
  },
  reporter: [['list']],
});
