/**
 * Settings → Connectivity history chart (localStorage).
 */
import { test, expect } from '@playwright/test';

test.describe('Connectivity history', () => {
  test('chart renders when local samples exist', async ({ page, request }) => {
    const health = await request.get('/api/health');
    test.skip(!health.ok(), 'plex-dashboard not running — start server on :8081');

    const t0 = new Date(2026, 2, 15, 14, 0, 0).getTime();
    await page.addInitScript((ts) => {
      const samples = [
        {
          t: ts,
          u: '2026-03-15T14:00:00Z',
          o: 'ok',
          c: {
            internet: { l: 'ok', ms: 10 },
            plex: { l: 'ok', ms: 20 },
            tmdb: { l: 'ok', ms: 30 },
            lgtv: { l: 'skip', ms: null },
          },
          smb: 50,
          sl: 'ok',
          stm: 1000,
        },
        {
          t: ts + 3600000,
          u: '2026-03-15T15:00:00Z',
          o: 'ok',
          c: {
            internet: { l: 'ok', ms: 12 },
            plex: { l: 'ok', ms: 22 },
            tmdb: { l: 'ok', ms: 32 },
            lgtv: { l: 'skip', ms: null },
          },
          smb: 80,
          sl: 'ok',
          stm: 900,
        },
      ];
      localStorage.setItem('plexdash.connectivity.history.v1', JSON.stringify({ v: 1, samples }));
    }, t0);

    await page.goto('/');
    await page.locator('.tab-btn[data-tab="settings"]').click();
    await page.locator('#connHistDate').fill('2026-03-15');
    await page.locator('#connHistRange').selectOption('hour');
    await page.locator('#connHistMetric').selectOption('plexMbps');
    await page.locator('#connHistRedrawBtn').click();

    const chart = page.locator('[data-testid="conn-hist-chart"]');
    await expect(chart.locator('.conn-hist-bar')).toHaveCount(24);
    const nonempty = chart.locator('.conn-hist-bar:not(.conn-hist-bar--empty)');
    await expect(nonempty).not.toHaveCount(0);
    await expect(page.locator('[data-testid="conn-hist-summary"]')).toContainText(/samples in view/i);
  });
});
