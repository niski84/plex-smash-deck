/**
 * Scroll tour: captures every safe tab with top + scroll-down frames for an
 * animated GIF showing the full app without exposing sensitive credentials.
 *
 * Tabs captured:  Dashboard → Playlists → Discovery → Snapshots → Help
 * Tabs SKIPPED:   Settings (contains API keys, server URLs, TV IP addresses)
 *
 * Output frames → docs/scroll-tour-frames/
 * Assemble GIF  → npm run scroll-tour:gif
 */
import { test } from '@playwright/test';
import path from 'path';
import fs from 'fs';

const OUT = path.join(__dirname, '../docs/scroll-tour-frames');
let frame = 0;

async function shot(page: any, label: string) {
  const padded = String(++frame).padStart(3, '0');
  await page.screenshot({
    path: path.join(OUT, `${padded}-${label}.png`),
    fullPage: false,
  });
}

/**
 * Take a top shot, then scroll down `steps` times by `scrollPx` each step,
 * shooting after each scroll.  Resets to top when done.
 */
async function scrollShots(
  page: any,
  prefix: string,
  steps = 3,
  scrollPx = 420,
) {
  await shot(page, `${prefix}-top`);
  for (let i = 1; i <= steps; i++) {
    await page.evaluate(
      (amt: number) => window.scrollBy({ top: amt, behavior: 'smooth' }),
      scrollPx,
    );
    await page.waitForTimeout(650);
    await shot(page, `${prefix}-scroll-${i}`);
  }
  // Reset for next tab
  await page.evaluate(() => window.scrollTo({ top: 0, behavior: 'instant' }));
  await page.waitForTimeout(300);
}

test.beforeAll(() => {
  fs.rmSync(OUT, { recursive: true, force: true });
  fs.mkdirSync(OUT, { recursive: true });
});

test('plex-dashboard scroll tour', async ({ page }) => {
  await page.setViewportSize({ width: 1400, height: 860 });

  // ── 1. Dashboard ─────────────────────────────────────────────────────────────
  await page.goto('/');
  await page.waitForTimeout(1000);

  // Trigger movie load (auto-hydrates from cache, or click the button)
  const loadBtn = page.locator('#loadMoviesBtn');
  await loadBtn.waitFor({ state: 'visible', timeout: 8000 });
  const status = await page.locator('#movieStatus').textContent().catch(() => '');
  if (!status?.includes('cache') && !status?.includes('Loaded')) {
    await loadBtn.click();
  }

  // Wait for grid to populate
  await page.waitForFunction(
    () => document.querySelectorAll('#movieGrid .movie-card').length > 10,
    { timeout: 30000 },
  );
  await page.locator('#movieGrid').scrollIntoViewIfNeeded();
  await page.evaluate(() => window.scrollTo(0, 0));
  await page.waitForLoadState('networkidle', { timeout: 20000 }).catch(() => {});
  await page.waitForTimeout(2000);

  // 5 scroll steps on the dashboard to show the full poster grid
  await scrollShots(page, 'dashboard', 5, 400);

  // ── 2. Playlists ─────────────────────────────────────────────────────────────
  await page.getByRole('button', { name: /Playlists/i }).click();
  await page.waitForTimeout(1000);
  await scrollShots(page, 'playlists', 3, 420);

  // ── 3. Discovery ─────────────────────────────────────────────────────────────
  await page.getByRole('button', { name: /Discovery/i }).click();
  await page.waitForTimeout(900);
  await scrollShots(page, 'discovery', 3, 420);

  // ── 4. Snapshots ─────────────────────────────────────────────────────────────
  await page.getByRole('button', { name: /Snapshots/i }).click();
  await page.waitForTimeout(1200);
  // Wait for diff / empty-state to render
  await page
    .waitForSelector('#snapDropList, #snapDropNone, .snap-drop-empty', {
      timeout: 10000,
    })
    .catch(() => {});
  await page.waitForTimeout(800);
  await scrollShots(page, 'snapshots', 3, 420);

  // ── 5. Help ───────────────────────────────────────────────────────────────────
  await page.getByRole('button', { name: /Help/i }).click();
  await page.waitForTimeout(900);
  await scrollShots(page, 'help', 3, 420);

  // ── Settings intentionally skipped ───────────────────────────────────────────
  // Settings contains sensitive data: Plex token, TMDB key, LG TV IP/key,
  // Radarr API key, OMDB key, server URLs.  Do not capture it.

  console.log(`\n✓ Captured ${frame} frames → ${OUT}`);
});
