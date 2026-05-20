import { test, expect } from '@playwright/test';

export const suite = {
  id: "plex-dashboard",
  summary: "Plex dashboard movie library grid — full library rendering, search/filter, multi-select, and Plex thumb proxy",
  tags: ["api", "ui", "media", "search"],
} as const;

const BASE = 'http://127.0.0.1:8081';

test('movie grid loads and displays posters', async ({ page }) => {
  await page.goto(BASE);

  // Target Player card should be first
  const targetPlayerHeading = page.locator('h3', { hasText: 'Target Player' });
  await expect(targetPlayerHeading).toBeVisible();

  // Library card and Load Movies button
  const loadBtn = page.locator('#loadMoviesBtn');
  await expect(loadBtn).toBeVisible();
  await loadBtn.click();

  // Wait for movies to load (may take a few seconds for 6940 movies)
  await expect(page.locator('#movieStatus')).toHaveText('Loaded', { timeout: 30000 });

  // Check count badge shows a large number
  const countBadge = page.locator('#movieCount');
  await expect(countBadge).toBeVisible();
  const countText = await countBadge.textContent();
  const count = parseInt(countText || '0');
  expect(count).toBeGreaterThan(100);

  // Grid should have movie cards
  const cards = page.locator('.movie-card');
  await expect(cards.first()).toBeVisible();
  const cardCount = await cards.count();
  expect(cardCount).toBeGreaterThan(100);

  // First card should have a poster image
  const firstImg = cards.first().locator('img');
  await expect(firstImg).toBeVisible();

  console.log(`[movie-grid] Loaded ${count} movies, rendered ${cardCount} cards`);
});

test('movie search filters grid', async ({ page }) => {
  await page.goto(BASE);

  const loadBtn = page.locator('#loadMoviesBtn');
  await loadBtn.click();
  await expect(page.locator('#movieStatus')).toHaveText('Loaded', { timeout: 30000 });

  // Search for Tom Hanks
  const searchInput = page.locator('#movieSearch');
  await searchInput.fill('Tom Hanks');

  // Count should show filtered results
  const countBadge = page.locator('#movieCount');
  const countText = await countBadge.textContent();
  expect(countText).toContain('of 6940');

  const filtered = parseInt(countText?.match(/^(\d+)/)?.[1] || '0');
  expect(filtered).toBeGreaterThan(5);
  expect(filtered).toBeLessThan(200);

  console.log(`[movie-grid] Tom Hanks filter: ${countText}`);
});

test('select movies and enable play button', async ({ page }) => {
  await page.goto(BASE);

  const loadBtn = page.locator('#loadMoviesBtn');
  await loadBtn.click();
  await expect(page.locator('#movieStatus')).toHaveText('Loaded', { timeout: 30000 });

  // Play Selected should be disabled initially
  const playSelectedBtn = page.locator('#moviePlaySelectedBtn');
  await expect(playSelectedBtn).toBeDisabled();

  // Search to narrow down, then check first two
  await page.locator('#movieSearch').fill('Die Hard');
  await page.waitForTimeout(200);

  const checkboxes = page.locator('.movie-card input[type="checkbox"]');
  await checkboxes.nth(0).check();
  await checkboxes.nth(1).check();

  // Play Selected should now be enabled and show count
  await expect(playSelectedBtn).toBeEnabled();
  await expect(playSelectedBtn).toContainText('(2)');

  console.log('[movie-grid] Multi-select works correctly');
});

test('plex thumb proxy returns an image', async ({ request }) => {
  // Get a rating key from the movies API
  const resp = await request.get(`${BASE}/api/movies`);
  expect(resp.ok()).toBeTruthy();
  const data = await resp.json();
  const firstKey = data.data.movies[0].RatingKey;

  // Fetch the thumb proxy
  const thumbResp = await request.get(`${BASE}/api/plex/thumb?ratingKey=${firstKey}`);
  expect(thumbResp.ok()).toBeTruthy();
  const ct = thumbResp.headers()['content-type'];
  expect(ct).toMatch(/^image\//);

  console.log(`[movie-grid] Thumb proxy OK for ratingKey=${firstKey}, content-type=${ct}`);
});
