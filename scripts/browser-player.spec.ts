/**
 * Browser player: stream proxy API, player overlay, hover-popup play/cache buttons, and smash animation.
 * Requires plex-dashboard on 127.0.0.1:8081 with at least one movie in the library.
 */
import { test, expect } from '@playwright/test';

/** Wait for the movie grid to finish loading (movieCount shows non-zero). */
async function waitForGrid(page) {
  await page.waitForFunction(
    () => {
      const el = document.getElementById('movieCount');
      if (!el) return false;
      const m = el.textContent.match(/(\d+)\s*movie/);
      return m && parseInt(m[1], 10) > 0;
    },
    { timeout: 30_000 }
  );
}

/** Inject a test card and directly trigger _showMovieInfo so the popup appears. */
async function injectAndShowPopup(page, id: string, opts: { ratingKey?: string; title?: string } = {}) {
  const rk = opts.ratingKey || id;
  const title = opts.title || `E2E ${id}`;
  await page.evaluate(
    ({ id, rk, title }) => {
      const grid = document.getElementById('movieGrid');
      if (!grid) throw new Error('missing #movieGrid');
      const card = document.createElement('div');
      card.className = 'movie-card';
      card.id = id;
      card.style.cssText = 'min-width:100px;min-height:140px;position:relative;background:#333;';
      grid.prepend(card);

      const movie = {
        RatingKey: rk,
        PartKey: '/library/parts/99999/file.mp4',
        FileContainer: 'mp4',
        PartSize: 1000000,
        Title: title,
        Year: 2025,
        Summary: 'E2E test movie for browser player',
        Rating: 8.0,
        DurationMillis: 7200000,
        ViewCount: 0,
        Directors: ['Test Director'],
        Genres: ['Action'],
      };
      (window as any)._showMovieInfo(card, movie);
    },
    { id, rk, title }
  );
}

/* ── API contract tests ────────────────────────────────────────────────────── */

test.describe('Stream proxy API', () => {
  test('GET /api/stream/cache returns list (possibly empty)', async ({ request }) => {
    const health = await request.get('/api/health');
    test.skip(!health.ok(), 'plex-dashboard not running');

    const res = await request.get('/api/stream/cache');
    expect(res.ok()).toBeTruthy();
    const body = await res.json();
    expect(body.success).toBeTruthy();
    expect(body.data).toHaveProperty('items');
    expect(Array.isArray(body.data.items)).toBeTruthy();
    expect(body.data).toHaveProperty('totalBytes');
  });

  test('GET /api/stream/status/ for unknown key returns zeros', async ({ request }) => {
    const health = await request.get('/api/health');
    test.skip(!health.ok(), 'plex-dashboard not running');

    const res = await request.get('/api/stream/status/nonexistent-key-999');
    expect(res.ok()).toBeTruthy();
    const body = await res.json();
    expect(body.success).toBeTruthy();
    expect(body.data.complete).toBe(false);
    expect(body.data.cachedBytes).toBe(0);
  });

  test('POST /api/stream/preload rejects empty ratingKey', async ({ request }) => {
    const health = await request.get('/api/health');
    test.skip(!health.ok(), 'plex-dashboard not running');

    const res = await request.post('/api/stream/preload', {
      data: { ratingKey: '' },
    });
    expect(res.status()).toBe(400);
    const body = await res.json();
    expect(body.success).toBe(false);
  });

  test('DELETE /api/stream/cache/ for unknown key succeeds gracefully', async ({ request }) => {
    const health = await request.get('/api/health');
    test.skip(!health.ok(), 'plex-dashboard not running');

    const res = await request.delete('/api/stream/cache/nonexistent-key-999');
    expect(res.ok()).toBeTruthy();
    const body = await res.json();
    expect(body.success).toBeTruthy();
    expect(body.data.removed).toBe(false);
  });

  test('GET /api/stream/ without ratingKey returns 400', async ({ request }) => {
    const health = await request.get('/api/health');
    test.skip(!health.ok(), 'plex-dashboard not running');

    const res = await request.get('/api/stream/');
    expect(res.status()).toBe(400);
  });
});

/* ── UI: player overlay markup & transport option ─────────────────────────── */

test.describe('Browser player UI', () => {
  test('HTML contains player overlay and browser transport option', async ({ request }) => {
    const res = await request.get('/');
    test.skip(!res.ok(), 'dashboard not running');
    const html = await res.text();

    expect(html).toContain('id="browserPlayer"');
    expect(html).toContain('id="browserPlayerVideo"');
    expect(html).toContain('id="browserPlayerClose"');
    expect(html).toContain('id="browserPlayerTitle"');
    expect(html).toContain('id="browserPlayerProgress"');
    expect(html).toContain('value="browser"');
  });

  test('transport dropdown has browser option', async ({ page, request }) => {
    const health = await request.get('/api/health');
    test.skip(!health.ok(), 'plex-dashboard not running');

    await page.goto('/');
    const opt = page.locator('#moviePlayTransport option[value="browser"]');
    await expect(opt).toBeAttached();
    await expect(opt).toHaveText(/Browser/i);
  });

  test('browser player overlay is hidden by default', async ({ page, request }) => {
    const health = await request.get('/api/health');
    test.skip(!health.ok(), 'plex-dashboard not running');

    await page.goto('/');
    const player = page.locator('#browserPlayer');
    await expect(player).not.toHaveClass(/bp-open/);
  });
});

/* ── UI: movie info popup play + cache buttons ────────────────────────────── */

test.describe('Movie info popup play buttons', () => {
  test('popup shows Play on TV, Play in Browser, and Cache buttons', async ({ page, request }) => {
    const health = await request.get('/api/health');
    test.skip(!health.ok(), 'plex-dashboard not running');

    await page.addInitScript(() => {
      (window as any).__PLEXDASH_HOVER_POPUP_MS = 60;
    });

    await page.goto('/');
    await waitForGrid(page);
    await injectAndShowPopup(page, 'e2e-player-card', {
      ratingKey: 'e2e-player-rk',
      title: 'E2E Player Test Movie',
    });

    await expect(page.locator('#movieInfoPopup')).toHaveClass(/mip-visible/, { timeout: 8000 });

    await expect(page.locator('[data-testid="mip-play-tv"]')).toBeVisible();
    await expect(page.locator('[data-testid="mip-play-browser"]')).toBeVisible();
    await expect(page.locator('[data-testid="mip-play-download"]')).toBeVisible();
    await expect(page.locator('[data-testid="mip-play-tv"]')).toHaveText(/Play on TV/i);
    await expect(page.locator('[data-testid="mip-play-browser"]')).toHaveText(/Play in Browser/i);
    await expect(page.locator('[data-testid="mip-play-download"]')).toHaveText(/Cache/i);
  });

  test('Play in Browser button opens the player overlay', async ({ page, request }) => {
    const health = await request.get('/api/health');
    test.skip(!health.ok(), 'plex-dashboard not running');

    await page.addInitScript(() => {
      (window as any).__PLEXDASH_HOVER_POPUP_MS = 60;
    });

    await page.goto('/');
    await waitForGrid(page);
    await injectAndShowPopup(page, 'e2e-play-browser-card', {
      ratingKey: 'e2e-play-browser-rk',
      title: 'E2E Browser Play',
    });

    await expect(page.locator('#movieInfoPopup')).toHaveClass(/mip-visible/, { timeout: 8000 });

    await page.locator('[data-testid="mip-play-browser"]').click();

    await expect(page.locator('#browserPlayer')).toHaveClass(/bp-open/, { timeout: 5000 });
    await expect(page.locator('#browserPlayerTitle')).toContainText('E2E Browser Play');

    const src = await page.locator('#browserPlayerVideo').getAttribute('src');
    expect(src).toContain('/api/stream/');
    expect(src).toContain('e2e-play-browser-rk');
  });

  test('close button dismisses player overlay', async ({ page, request }) => {
    const health = await request.get('/api/health');
    test.skip(!health.ok(), 'plex-dashboard not running');

    await page.goto('/');

    await page.evaluate(() => {
      (window as any).openBrowserPlayer({
        ratingKey: 'e2e-close-test',
        title: 'Close Test Movie (2025)',
        container: 'mp4',
        partSize: 100000,
      });
    });

    await expect(page.locator('#browserPlayer')).toHaveClass(/bp-open/);
    await expect(page.locator('#browserPlayerTitle')).toContainText('Close Test Movie');

    await page.locator('#browserPlayerClose').click();
    await expect(page.locator('#browserPlayer')).not.toHaveClass(/bp-open/);
  });

  test('Escape key dismisses player overlay', async ({ page, request }) => {
    const health = await request.get('/api/health');
    test.skip(!health.ok(), 'plex-dashboard not running');

    await page.goto('/');

    await page.evaluate(() => {
      (window as any).openBrowserPlayer({
        ratingKey: 'e2e-esc-test',
        title: 'Escape Test (2025)',
        container: 'mp4',
        partSize: 100000,
      });
    });

    await expect(page.locator('#browserPlayer')).toHaveClass(/bp-open/);
    await page.keyboard.press('Escape');
    await expect(page.locator('#browserPlayer')).not.toHaveClass(/bp-open/);
  });

  test('popup shows play buttons on regular card hover (no opts.showPlay)', async ({ page, request }) => {
    const health = await request.get('/api/health');
    test.skip(!health.ok(), 'plex-dashboard not running');

    await page.addInitScript(() => {
      (window as any).__PLEXDASH_HOVER_POPUP_MS = 60;
    });

    await page.goto('/');
    await waitForGrid(page);
    await injectAndShowPopup(page, 'e2e-no-showplay-card', {
      ratingKey: 'e2e-no-showplay',
      title: 'No showPlay opt',
    });

    await expect(page.locator('#movieInfoPopup')).toHaveClass(/mip-visible/, { timeout: 8000 });
    await expect(page.locator('[data-testid="mip-play-tv"]')).toBeVisible();
    await expect(page.locator('[data-testid="mip-play-browser"]')).toBeVisible();
    await expect(page.locator('[data-testid="mip-play-download"]')).toBeVisible();
  });

  test('format warning shown for MKV container', async ({ page, request }) => {
    const health = await request.get('/api/health');
    test.skip(!health.ok(), 'plex-dashboard not running');

    await page.goto('/');

    await page.evaluate(() => {
      (window as any).openBrowserPlayer({
        ratingKey: 'e2e-mkv-warn',
        title: 'MKV Format Test (2025)',
        container: 'mkv',
        partSize: 3000000000,
      });
    });

    await expect(page.locator('#browserPlayer')).toHaveClass(/bp-open/);
    await expect(page.locator('#browserPlayer .bp-format-warn')).toBeVisible();
    await expect(page.locator('#browserPlayer .bp-format-warn')).toContainText('MKV');
    await page.locator('#browserPlayerClose').click();
  });
});

/* ── Smash arcade button animation ────────────────────────────────────────── */

test.describe('Smash button animation on popup buttons', () => {
  test('clicking Play in Browser triggers smash-active class', async ({ page, request }) => {
    const health = await request.get('/api/health');
    test.skip(!health.ok(), 'plex-dashboard not running');

    await page.addInitScript(() => {
      (window as any).__PLEXDASH_HOVER_POPUP_MS = 60;
    });

    await page.goto('/');
    await waitForGrid(page);
    await injectAndShowPopup(page, 'e2e-smash-card', {
      ratingKey: 'e2e-smash-rk',
      title: 'Smash Button Test',
    });

    await expect(page.locator('#movieInfoPopup')).toHaveClass(/mip-visible/, { timeout: 8000 });

    const btn = page.locator('[data-testid="mip-play-browser"]');
    await expect(btn).toBeVisible();

    // Dispatch pointerdown to trigger smash animation.
    await btn.dispatchEvent('pointerdown');

    // smash-active should be applied (may be transient due to animationend).
    await expect(btn).toHaveClass(/smash-active/, { timeout: 1000 });
  });

  test('Cache button in popup is visible and clickable', async ({ page, request }) => {
    const health = await request.get('/api/health');
    test.skip(!health.ok(), 'plex-dashboard not running');

    await page.addInitScript(() => {
      (window as any).__PLEXDASH_HOVER_POPUP_MS = 60;
    });

    await page.goto('/');
    await waitForGrid(page);
    await injectAndShowPopup(page, 'e2e-cache-btn-card', {
      ratingKey: 'e2e-cache-btn-rk',
      title: 'Cache Button Test',
    });

    await expect(page.locator('#movieInfoPopup')).toHaveClass(/mip-visible/, { timeout: 8000 });

    const cacheBtn = page.locator('[data-testid="mip-play-download"]');
    await expect(cacheBtn).toBeVisible();
    await expect(cacheBtn).toHaveText(/Cache/i);

    // Clicking cache changes text — for a fake ratingKey the preload API will fail,
    // so we just verify the button reacted (text no longer the initial "Cache").
    await cacheBtn.click();
    await expect(cacheBtn).not.toHaveText('Cache', { timeout: 3000 });
  });

  test('all three popup buttons inherit arcade border-radius', async ({ page, request }) => {
    const health = await request.get('/api/health');
    test.skip(!health.ok(), 'plex-dashboard not running');

    await page.addInitScript(() => {
      (window as any).__PLEXDASH_HOVER_POPUP_MS = 60;
    });

    await page.goto('/');
    await waitForGrid(page);
    await injectAndShowPopup(page, 'e2e-style-card', {
      ratingKey: 'e2e-style-rk',
      title: 'Style Test',
    });

    await expect(page.locator('#movieInfoPopup')).toHaveClass(/mip-visible/, { timeout: 8000 });

    // Verify arcade pill shape (border-radius: 999px from global button style).
    for (const testId of ['mip-play-tv', 'mip-play-browser', 'mip-play-download']) {
      const btn = page.locator(`[data-testid="${testId}"]`);
      await expect(btn).toBeVisible();
      const radius = await btn.evaluate(el => getComputedStyle(el).borderRadius);
      expect(radius).toBe('999px');
    }
  });
});
