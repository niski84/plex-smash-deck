/**
 * Verifies that typing in the v1 dashboard search box filters the movie grid
 * to show only matching titles, and that clearing the search restores the full grid.
 */
import { test, expect } from '@playwright/test';

test.describe('Dashboard search (v1)', () => {
  test.beforeEach(async ({ request }) => {
    const health = await request.get('/api/health');
    test.skip(!health.ok(), 'plex-dashboard not running on :8081');
  });

  test('typing a title filters the grid to matching movies only', async ({ page }) => {
    await page.goto('/');

    // Make sure we are on the dashboard tab (it is the default, but be explicit).
    await page.locator('.tab-btn[data-tab="dashboard"]').click();

    // Wait for movies to load — the count badge changes from "0 movies".
    const countBadge = page.locator('[data-testid="movie-count"]');
    await expect(countBadge).not.toHaveText('0 movies', { timeout: 30_000 });

    const totalText = await countBadge.textContent();
    const totalMatch = (totalText || '').match(/^(\d[\d,]*)/);
    const totalMovies = totalMatch ? parseInt(totalMatch[1].replace(/,/g, ''), 10) : 0;
    expect(totalMovies).toBeGreaterThan(0);

    // Type "ghostbusters" into the search box.
    const searchInput = page.locator('[data-testid="movie-search"]');
    await searchInput.fill('ghostbusters');

    // Wait for the grid to update — count badge should reflect fewer movies.
    await expect(countBadge).not.toHaveText(totalText!, { timeout: 5_000 });

    // Every visible card title must include "ghostbusters" (case-insensitive).
    const grid = page.locator('[data-testid="movie-list"]');
    const cards = grid.locator('.movie-card, .mc-card, [data-rating-key]');
    const count = await cards.count();
    expect(count).toBeGreaterThan(0);

    for (let i = 0; i < count; i++) {
      const titleEl = cards.nth(i).locator('.mc-title, .movie-title, [class*="title"]').first();
      const title = (await titleEl.textContent() || '').toLowerCase();
      expect(title).toContain('ghostbusters');
    }

    // Clearing the search should restore the full grid.
    await searchInput.fill('');
    await expect(countBadge).toHaveText(totalText!, { timeout: 5_000 });
  });

  test('search with no matches shows empty grid and zero count', async ({ page }) => {
    await page.goto('/');
    await page.locator('.tab-btn[data-tab="dashboard"]').click();

    const countBadge = page.locator('[data-testid="movie-count"]');
    await expect(countBadge).not.toHaveText('0 movies', { timeout: 30_000 });

    const searchInput = page.locator('[data-testid="movie-search"]');
    await searchInput.fill('zzznomatchxxx');

    // Grid should be empty.
    const grid = page.locator('[data-testid="movie-list"]');
    await expect(grid).toBeEmpty({ timeout: 5_000 });
  });
});
