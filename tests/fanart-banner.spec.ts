import { test, expect } from '@playwright/test';

export const suite = {
  id: "plex-dashboard",
  summary: "Plex dashboard Fanart.tv integration — banner fetch API, cache status, settings panel controls, and rotation config",
  tags: ["api", "ui", "media"],
} as const;

// Run from plex-dashboard repo: npx playwright test scripts/fanart-banner.spec.ts
const BASE = 'http://127.0.0.1:8081';

test('fanart banner and cache API return success JSON', async ({ request }) => {
  const banner = await request.get(`${BASE}/api/branding/fanart-banner`);
  expect(banner.ok()).toBeTruthy();
  const bj = await banner.json();
  expect(bj.success).toBe(true);
  expect(bj.data).toBeTruthy();
  console.log('[fanart-banner] /api/branding/fanart-banner', { active: bj.data.active, reason: bj.data.reason });

  const st = await request.get(`${BASE}/api/fanart-banner/cache-status`);
  expect(st.ok()).toBeTruthy();
  const sj = await st.json();
  expect(sj.success).toBe(true);
  expect(sj.data).toHaveProperty('entries');
  expect(sj.data).toHaveProperty('dir');
});

test('settings shows Fanart.tv controls', async ({ page }) => {
  await page.goto(BASE);
  await page.getByRole('button', { name: 'Settings' }).click();
  await expect(page.getByTestId('cfg-fanart-enabled')).toBeVisible();
  await expect(page.getByTestId('cfg-banner-art-refresh')).toBeVisible();
  await expect(page.getByTestId('cfg-banner-rotate')).toBeVisible();
  await expect(page.getByTestId('fanart-cache-status-line')).toBeVisible();
});
