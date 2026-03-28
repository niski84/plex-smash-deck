/**
 * Dashboard genre bar preferences (localStorage + Settings UI).
 */
import { test, expect } from '@playwright/test';

test.describe('Genre bar preferences', () => {
  test('Settings textareas reflect localStorage prefs', async ({ page, request }) => {
    const health = await request.get('/api/health');
    test.skip(!health.ok(), 'plex-dashboard not running — start server on :8081');

    await page.addInitScript(() => {
      localStorage.setItem(
        'plexdash.genreBar.prefs.v1',
        JSON.stringify({ pinned: ['horror', 'drama'], hidden: ['comedy'] })
      );
    });

    await page.goto('/');
    await page.locator('.tab-btn[data-tab="settings"]').click();
    await expect(page.locator('#settingsGenrePinned')).toHaveValue('Horror\nDrama');
    await expect(page.locator('#settingsGenreHidden')).toHaveValue('Comedy');
  });
});
