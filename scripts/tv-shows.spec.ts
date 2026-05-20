/**
 * tv-shows.spec.ts
 * Diagnostic + regression test: TV shows must load in both v1 (/) and beta (/beta).
 *
 * Run:  npx playwright test scripts/tv-shows.spec.ts --config=playwright.config.ts
 */
import { test, expect } from '@playwright/test';

const BASE = 'http://127.0.0.1:8081';

// ── Helpers ──────────────────────────────────────────────────────────────────

async function checkApiShows(page: any) {
  const res = await page.request.get(`${BASE}/api/shows`);
  const body = await res.json();
  const data = body.data ?? {};
  return {
    tvEnabled: !!data.tvEnabled,
    count: (data.shows ?? []).length,
    raw: body,
  };
}

async function checkApiSettings(page: any) {
  const res = await page.request.get(`${BASE}/api/settings`);
  const body = await res.json();
  return {
    tvLibraryKey: body.data?.TVLibraryKey ?? '',
    plexBaseURL: body.data?.PlexBaseURL ?? '',
  };
}

// ── API sanity check ──────────────────────────────────────────────────────────

test('API: /api/shows returns tvEnabled=true and at least 1 show', async ({ page }) => {
  const { tvEnabled, count, raw } = await checkApiShows(page);
  const settings = await checkApiSettings(page);

  console.log('Settings TVLibraryKey:', settings.tvLibraryKey || '(empty)');
  console.log('Settings PlexBaseURL:', settings.plexBaseURL || '(empty)');
  console.log('API response:', JSON.stringify(raw).slice(0, 300));
  console.log('tvEnabled:', tvEnabled, '— show count:', count);

  if (!settings.tvLibraryKey) {
    test.skip(true, 'TVLibraryKey not configured — set it in Settings to enable TV tab');
    return;
  }

  expect(tvEnabled, `tvEnabled is false — TVLibraryKey="${settings.tvLibraryKey}" set but Plex not returning shows`).toBe(true);
  expect(count, 'API returned 0 shows even though tvEnabled=true').toBeGreaterThan(0);
});

// ── v1 TV shows ───────────────────────────────────────────────────────────────

test('v1 (/): TV shows are visible in the show grid', async ({ page }) => {
  const settings = await checkApiSettings(page);
  if (!settings.tvLibraryKey) {
    test.skip(true, 'TVLibraryKey not configured');
    return;
  }

  await page.goto(BASE + '/');
  await page.waitForLoadState('networkidle');

  // v1 has a library mode toggle — click "TV" to switch to TV mode
  const tvToggle = page.locator('#libraryModeTV');
  const tvToggleVisible = await tvToggle.isVisible().catch(() => false);

  if (tvToggleVisible) {
    await tvToggle.click();
    // Wait for shows to load
    await page.waitForSelector('.show-card', { timeout: 45_000 });
  }

  const showCards = page.locator('.show-card');
  const count = await showCards.count();
  console.log('v1 show cards found:', count);

  await page.screenshot({ path: 'docs/tv-test-v1.png' });
  expect(count, 'v1: no show cards found in the TV grid').toBeGreaterThan(0);
});

// ── beta TV shows ─────────────────────────────────────────────────────────────

test('beta (/beta): TV Shows tab loads and shows the show grid', async ({ page }) => {
  const settings = await checkApiSettings(page);
  if (!settings.tvLibraryKey) {
    test.skip(true, 'TVLibraryKey not configured');
    return;
  }

  // Intercept /api/shows to log what the beta receives
  let apiResponse: any = null;
  page.on('response', async resp => {
    if (resp.url().includes('/api/shows') && !resp.url().includes('cache-status') && !resp.url().includes('play')) {
      try { apiResponse = await resp.json(); } catch {}
    }
  });

  await page.goto(BASE + '/beta');
  await page.waitForLoadState('domcontentloaded');

  // Click the TV Shows tab
  const tvTab = page.locator('button', { hasText: 'TV Shows' });
  await tvTab.waitFor({ timeout: 10_000 });
  await tvTab.click();

  // Wait for either show cards or the "not configured" message
  await page.waitForTimeout(2000); // give init() time to start

  // Wait up to 45s for /api/shows to respond (Plex fetch can be slow)
  try {
    await page.waitForSelector('.show-card', { timeout: 45_000 });
  } catch {
    // Log diagnostic info before failing
    const errorText = await page.locator('#discovery-tab, [x-data="showGrid()"]').textContent().catch(() => '');
    console.log('Beta TV tab content:', errorText.slice(0, 500));
    console.log('API /api/shows response intercepted:', JSON.stringify(apiResponse).slice(0, 500));

    const statusMsg = await page.locator('text=No TV shows loaded').isVisible().catch(() => false);
    const errorMsg = await page.locator('[x-text="error"]').textContent().catch(() => '');
    console.log('"No TV shows loaded" visible:', statusMsg);
    console.log('Error field:', errorMsg);
  }

  const showCards = page.locator('.show-card');
  const count = await showCards.count();
  console.log('beta show cards found:', count);
  console.log('beta /api/shows response:', JSON.stringify(apiResponse)?.slice(0, 300));

  await page.screenshot({ path: 'docs/tv-test-beta.png' });
  expect(count, 'beta: no show cards found in the TV grid').toBeGreaterThan(0);
});

// ── Beta localStorage cache ───────────────────────────────────────────────────

test('beta: TV shows are served from localStorage cache on second visit', async ({ page }) => {
  // This test seeds fake cache data — no TVLibraryKey required.

  // Seed the cache manually so we can test without Plex being reachable
  await page.goto(BASE + '/beta');
  await page.waitForLoadState('domcontentloaded');

  const fakeShows = [
    { RatingKey: '999', Title: 'Cache Test Show', Year: 2020, SeasonCount: 1, EpisodeCount: 5, Rating: 8.5, Genres: [] },
  ];
  await page.evaluate((shows) => {
    localStorage.setItem('plexdash.tv.shows.v1', JSON.stringify({ shows, ts: Date.now() }));
  }, fakeShows);

  // Reload and switch to TV tab — should show cached data immediately
  await page.reload();
  await page.waitForLoadState('domcontentloaded');
  const tvTab = page.locator('button', { hasText: 'TV Shows' });
  await tvTab.click();

  // Should appear quickly (from cache, no API needed)
  await page.waitForSelector('.show-card', { timeout: 5_000 });
  const count = await page.locator('.show-card').count();
  console.log('cache test: show cards from localStorage:', count);

  // Clean up
  await page.evaluate(() => localStorage.removeItem('plexdash.tv.shows.v1'));

  expect(count).toBeGreaterThan(0);
});
