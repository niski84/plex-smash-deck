/**
 * Demo walkthrough: captures screenshots for the animated README GIF.
 * Scenes: movie grid search → snapshot diff → studio discovery (A24).
 */
import { test, expect } from '@playwright/test';
import path from 'path';
import fs from 'fs';

const OUT = path.join(__dirname, '../docs/demo-frames');
let frame = 0;

async function shot(page: any, label: string) {
  const padded = String(++frame).padStart(3, '0');
  await page.screenshot({
    path: path.join(OUT, `${padded}-${label}.png`),
    fullPage: false,
  });
}

// Visible movie card selector — the filter hides cards with [hidden] attribute
const VISIBLE_CARD = '#movieGrid .movie-card:not([hidden])';

test.beforeAll(() => {
  fs.rmSync(OUT, { recursive: true, force: true });
  fs.mkdirSync(OUT, { recursive: true });
});

test('plex-dashboard demo walkthrough', async ({ page }) => {
  await page.setViewportSize({ width: 1400, height: 860 });

  // ── 1. Dashboard tab ────────────────────────────────────────────────────────
  await page.goto('/');
  await page.waitForTimeout(1000);
  await shot(page, 'dashboard-home');

  // Trigger movie load (click the button or wait for cache hydration)
  const loadBtn = page.locator('#loadMoviesBtn');
  await loadBtn.waitFor({ state: 'visible', timeout: 8000 });

  // If the cache exists the grid auto-hydrates; if not, click to load
  const statusEl = page.locator('#movieStatus');
  const status = await statusEl.textContent().catch(() => '');
  if (!status?.includes('cache') && !status?.includes('Loaded')) {
    await loadBtn.click();
  }

  // Wait for at least 10 cards
  await page.waitForFunction(
    () => document.querySelectorAll('#movieGrid .movie-card').length > 10,
    { timeout: 30000 }
  );
  // Scroll to the top of the grid so the first row of posters is in the
  // viewport — lazy loading only fires for visible images.
  await page.locator('#movieGrid').scrollIntoViewIfNeeded();
  await page.evaluate(() => window.scrollTo(0, 0));
  // Let poster images finish loading — networkidle = no more than 2 in-flight
  // requests for 500 ms. Cap at 20 s for a cold start against a real Plex.
  await page.waitForLoadState('networkidle', { timeout: 20000 }).catch(() => {});
  await page.waitForTimeout(2000);
  await shot(page, 'dashboard-grid');

  // ── 2. Search for "Sneakers" ────────────────────────────────────────────────
  const searchBox = page.locator('#movieSearch');
  await searchBox.click();
  await page.waitForTimeout(200);

  for (const char of ['S', 'n', 'e', 'a', 'k']) {
    await searchBox.type(char, { delay: 120 });
    await page.waitForTimeout(250);
  }
  await shot(page, 'search-sneak');

  await searchBox.type('ers', { delay: 100 });
  // Wait for the filtered grid to settle and any visible-card images to load.
  await page.waitForTimeout(400);
  await page.waitForLoadState('networkidle', { timeout: 8000 }).catch(() => {});
  await page.waitForTimeout(600);
  await shot(page, 'search-sneakers');

  // Hover the first visible card to show the info popup
  await page.waitForSelector(VISIBLE_CARD, { timeout: 5000 });
  const firstVisible = page.locator(VISIBLE_CARD).first();
  await firstVisible.scrollIntoViewIfNeeded();
  await firstVisible.hover({ force: true });
  await page.waitForTimeout(800);
  await shot(page, 'search-sneakers-hover');

  await page.mouse.move(0, 0); // dismiss popup
  await page.waitForTimeout(300);

  // Click the poster thumbnail to open the full-screen lightbox.
  // We use evaluate() to fire the click directly on the img element to avoid
  // any overlay or popup that may intercept mouse events in headless mode.
  const posterImg = firstVisible.locator('.movie-card-poster img');
  await posterImg.waitFor({ state: 'visible' });
  await page.evaluate(() => {
    // Dismiss the hover popup first, then simulate a click on the poster img.
    if (typeof (window as any)._hideMovieInfo === 'function') {
      (window as any)._hideMovieInfo();
    }
    const card = document.querySelector('#movieGrid .movie-card:not([hidden])') as HTMLElement;
    const img = card?.querySelector('.movie-card-poster img') as HTMLElement;
    img?.click();
  });
  // Wait for lightbox to open and the full-res image to finish loading
  await page.waitForSelector('#posterLightbox.lb-open', { timeout: 8000 });
  await page.waitForFunction(
    () => {
      const img = document.getElementById('posterLightboxImg') as HTMLImageElement;
      return img && img.complete && img.naturalHeight > 0;
    },
    { timeout: 12000 }
  );
  await page.waitForTimeout(600);
  await shot(page, 'sneakers-lightbox');

  // Close lightbox before continuing
  await page.keyboard.press('Escape');
  await page.waitForTimeout(300);

  // Clear search — wait for the full grid to re-render and images to reload.
  await searchBox.fill('');
  await page.waitForLoadState('networkidle', { timeout: 15000 }).catch(() => {});
  await page.waitForTimeout(1200);
  await shot(page, 'search-cleared');

  // ── 3. Select a few movies ──────────────────────────────────────────────────
  const cards = page.locator(VISIBLE_CARD);
  await cards.nth(0).locator('input[type="checkbox"]').check({ force: true });
  await page.waitForTimeout(150);
  await cards.nth(1).locator('input[type="checkbox"]').check({ force: true });
  await page.waitForTimeout(150);
  await cards.nth(2).locator('input[type="checkbox"]').check({ force: true });
  await page.waitForTimeout(400);
  await shot(page, 'movies-selected');

  // Uncheck
  for (let i = 0; i < 3; i++) {
    await cards.nth(i).locator('input[type="checkbox"]').uncheck({ force: true });
  }

  // ── 4. Snapshots tab ────────────────────────────────────────────────────────
  await page.getByRole('button', { name: /Snapshots/i }).click();
  await page.waitForTimeout(1200);
  await shot(page, 'snapshots-tab');

  // Wait for diff to render
  await page.waitForSelector('#snapDropList, #snapDropNone, .snap-drop-empty', { timeout: 10000 }).catch(() => {});
  await page.waitForTimeout(1000);
  await shot(page, 'snapshots-diff');

  // Pattern analysis box (if present)
  const patternsBox = page.locator('#snapDropPatterns');
  if (await patternsBox.isVisible().catch(() => false)) {
    await patternsBox.scrollIntoViewIfNeeded();
    await page.waitForTimeout(500);
    await shot(page, 'snapshots-patterns');
  }

  // ── 5. Discovery tab ────────────────────────────────────────────────────────
  await page.getByRole('button', { name: /Discovery/i }).click();
  await page.waitForTimeout(800);
  await shot(page, 'discovery-tab');

  // Switch to Studio mode
  await page.locator('#discoverMode').selectOption('studio');
  await page.waitForTimeout(500);
  await shot(page, 'discovery-studio-mode');

  // Type studio name A24
  const studioInput = page.locator('#discoverStudio');
  await studioInput.click();
  await studioInput.type('A24', { delay: 120 });
  await page.waitForTimeout(400);

  // Set min rating 6.5
  await page.locator('#discoverMinRating').selectOption('6.5');
  await page.waitForTimeout(200);
  await shot(page, 'discovery-a24-ready');

  // Run search
  await page.locator('#runDiscoveryBtn').click();
  await page.waitForTimeout(600);
  await shot(page, 'discovery-searching');

  // Wait for results
  await page.waitForFunction(
    () => {
      const el = document.getElementById('discoveryStatus');
      const txt = el?.textContent ?? '';
      return txt.length > 0 && !txt.includes('Analyzing');
    },
    { timeout: 60000 }
  );
  await page.waitForTimeout(800);
  await shot(page, 'discovery-results');

  // Scroll down to see results
  await page.locator('#discoveryTableWrap').scrollIntoViewIfNeeded();
  await page.keyboard.press('PageDown');
  await page.waitForLoadState('networkidle', { timeout: 8000 }).catch(() => {});
  await page.waitForTimeout(800);
  await shot(page, 'discovery-results-scrolled');

  // Hover first result row for plot popup
  const firstRow = page.locator('#discoveryRows tr').first();
  if (await firstRow.count() > 0) {
    await firstRow.scrollIntoViewIfNeeded();
    await firstRow.hover();
    await page.waitForTimeout(800);
    await shot(page, 'discovery-row-hover');
  }

  console.log(`\n✓ Captured ${frame} frames → ${OUT}`);
});
