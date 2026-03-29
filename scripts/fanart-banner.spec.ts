import { test, expect } from '@playwright/test';

test('fanart banner and cache API return success JSON', async ({ request }) => {
  const banner = await request.get('/api/branding/fanart-banner');
  expect(banner.ok()).toBeTruthy();
  const bj = await banner.json();
  expect(bj.success).toBe(true);
  expect(bj.data).toBeTruthy();
  console.log('[fanart-banner] /api/branding/fanart-banner', { active: bj.data.active, reason: bj.data.reason });

  const st = await request.get('/api/fanart-banner/cache-status');
  expect(st.ok()).toBeTruthy();
  const sj = await st.json();
  expect(sj.success).toBe(true);
  expect(sj.data).toHaveProperty('entries');
  expect(sj.data).toHaveProperty('dir');
  console.log('[fanart-banner] cache-status', sj.data);

  const lg = await request.get('/api/fanart-banner/log');
  expect(lg.ok()).toBeTruthy();
  const lj = await lg.json();
  expect(lj.success).toBe(true);
  expect(Array.isArray(lj.data.entries)).toBe(true);
});

test('settings shows Fanart.tv controls', async ({ page }) => {
  await page.goto('/');
  await page.getByRole('button', { name: 'Settings' }).click();
  await expect(page.getByTestId('cfg-fanart-enabled')).toBeVisible();
  await expect(page.getByTestId('cfg-banner-art-refresh')).toBeVisible();
  await expect(page.getByTestId('cfg-banner-rotate')).toBeVisible();
  await expect(page.getByTestId('fanart-cache-status-line')).toBeVisible();
  await expect(page.getByTestId('fanart-activity-log')).toBeVisible();
});
