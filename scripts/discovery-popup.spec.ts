/**
 * Verifies the Discovery tab deferred hover panel (plot + poster) is present in
 * the served HTML and behaves after the configured delay.
 *
 * Requires plex-dashboard on baseURL (playwright.config). If you see no UI change
 * in the browser, confirm you are hitting this server and wait the full default hover time on title/poster.
 */
import { test, expect } from '@playwright/test';

test.describe('Discovery deferred hover popup', () => {
  test('GET / HTML contains popup markup and configurable hover hook', async ({ request }) => {
    const res = await request.get('/');
    test.skip(!res.ok(), 'dashboard not running');
    const html = await res.text();
    expect(html).toContain('id="discPosterPopup"');
    expect(html).toContain('disc-popup-text');
    expect(html).toContain('__PLEXDASH_DISC_POPUP_HOVER_MS');
    expect(html).toContain('__PLEXDASH_HOVER_POPUP_MS');
    expect(html).toContain('disc-popup-img');
  });

  test('injected title shows synopsis after hover delay', async ({ page, request }) => {
    const health = await request.get('/api/health');
    test.skip(!health.ok(), 'dashboard not running');

    await page.addInitScript(() => {
      window.__PLEXDASH_DISC_POPUP_HOVER_MS = 60;
    });

    await page.goto('/');
    await page.locator('.tab-btn[data-tab="discovery"]').click();
    await expect(page.locator('#tab-discovery')).toBeVisible();

    await page.evaluate(() => {
      const tbody = document.getElementById('discoveryRows');
      if (!tbody) throw new Error('missing #discoveryRows');
      tbody.innerHTML = '';
      const tr = document.createElement('tr');
      const td = document.createElement('td');
      td.colSpan = 14;
      const span = document.createElement('span');
      span.className = 'disc-title';
      span.id = 'e2e-discovery-hover-anchor';
      span.textContent = 'Playwright discovery hover';
      span.addEventListener('mouseenter', () =>
        window._showPosterPopup(
          span,
          '',
          'Playwright discovery hover',
          'E2E_UNIQUE_SYNOPSIS_MARKER_7f3a'
        )
      );
      span.addEventListener('mouseleave', () => window._hidePosterPopup());
      td.appendChild(span);
      tr.appendChild(td);
      tbody.appendChild(tr);
    });

    await page.locator('#e2e-discovery-hover-anchor').hover();

    const plot = page.locator('#discPosterPopup .disc-popup-plot');
    await expect(plot).toContainText('E2E_UNIQUE_SYNOPSIS_MARKER_7f3a', { timeout: 8000 });
    await expect(page.locator('#discPosterPopup')).toHaveClass(/pp-visible/);
  });

  test('moving pointer from anchor to popup keeps panel visible briefly', async ({ page, request }) => {
    const health = await request.get('/api/health');
    test.skip(!health.ok(), 'dashboard not running');

    await page.addInitScript(() => {
      window.__PLEXDASH_DISC_POPUP_HOVER_MS = 60;
    });

    await page.goto('/');
    await page.locator('.tab-btn[data-tab="discovery"]').click();

    await page.evaluate(() => {
      const tbody = document.getElementById('discoveryRows');
      tbody.innerHTML = '';
      const tr = document.createElement('tr');
      const td = document.createElement('td');
      td.colSpan = 14;
      const span = document.createElement('span');
      span.className = 'disc-title';
      span.id = 'e2e-bridge-anchor';
      span.textContent = 'Bridge test';
      span.addEventListener('mouseenter', () =>
        window._showPosterPopup(span, '', 'Bridge test', 'Bridge synopsis')
      );
      span.addEventListener('mouseleave', () => window._hidePosterPopup());
      td.appendChild(span);
      tr.appendChild(td);
      tbody.appendChild(tr);
    });

    await page.locator('#e2e-bridge-anchor').hover();
    await expect(page.locator('#discPosterPopup')).toHaveClass(/pp-visible/, { timeout: 8000 });

    await page.locator('#discPosterPopup').hover();
    await expect(page.locator('#discPosterPopup .disc-popup-plot')).toContainText('Bridge synopsis');

    await page.mouse.move(0, 0);
    await page.waitForTimeout(900);
    await expect(page.locator('#discPosterPopup')).not.toHaveClass(/pp-visible/);
  });
});

test.describe('Dashboard movie info hover popup', () => {
  test('injected card shows synopsis after hover delay', async ({ page, request }) => {
    const health = await request.get('/api/health');
    test.skip(!health.ok(), 'dashboard not running');

    await page.addInitScript(() => {
      window.__PLEXDASH_HOVER_POPUP_MS = 60;
    });

    await page.goto('/');

    await page.evaluate(() => {
      const grid = document.getElementById('movieGrid');
      if (!grid) throw new Error('missing #movieGrid');
      grid.innerHTML = '';
      const card = document.createElement('div');
      card.className = 'movie-card';
      card.id = 'e2e-mip-card';
      card.style.minWidth = '80px';
      card.style.minHeight = '120px';
      const movie = {
        RatingKey: 'e2e-rk',
        Title: 'Playwright dashboard hover',
        Year: 2020,
        Summary: 'E2E_DASHBOARD_MIP_SYNOPSIS',
        Rating: 0,
        DurationMillis: 0,
        ViewCount: 0,
        Directors: [],
        Genres: [],
      };
      card.addEventListener('mouseenter', () => window._showMovieInfo(card, movie));
      card.addEventListener('mouseleave', () => window._hideMovieInfo());
      grid.appendChild(card);
    });

    await page.locator('#e2e-mip-card').hover();
    await expect(page.locator('#movieInfoPopup .mip-plot')).toContainText('E2E_DASHBOARD_MIP_SYNOPSIS', {
      timeout: 8000,
    });
    await expect(page.locator('#movieInfoPopup')).toHaveClass(/mip-visible/);
  });

  test('moving pointer from card to movie popup keeps panel briefly', async ({ page, request }) => {
    const health = await request.get('/api/health');
    test.skip(!health.ok(), 'dashboard not running');

    await page.addInitScript(() => {
      window.__PLEXDASH_HOVER_POPUP_MS = 60;
    });

    await page.goto('/');

    await page.evaluate(() => {
      const grid = document.getElementById('movieGrid');
      grid.innerHTML = '';
      const card = document.createElement('div');
      card.className = 'movie-card';
      card.id = 'e2e-mip-bridge-card';
      card.style.minWidth = '80px';
      card.style.minHeight = '120px';
      const movie = {
        RatingKey: 'e2e-bridge',
        Title: 'MIP bridge',
        Summary: 'MIP bridge synopsis',
        Rating: 0,
        DurationMillis: 0,
        ViewCount: 0,
        Directors: [],
        Genres: [],
      };
      card.addEventListener('mouseenter', () => window._showMovieInfo(card, movie));
      card.addEventListener('mouseleave', () => window._hideMovieInfo());
      grid.appendChild(card);
    });

    await page.locator('#e2e-mip-bridge-card').hover();
    await expect(page.locator('#movieInfoPopup')).toHaveClass(/mip-visible/, { timeout: 8000 });

    await page.locator('#movieInfoPopup').hover();
    await expect(page.locator('#movieInfoPopup .mip-plot')).toContainText('MIP bridge synopsis');

    await page.mouse.move(0, 0);
    await page.waitForTimeout(900);
    await expect(page.locator('#movieInfoPopup')).not.toHaveClass(/mip-visible/);
  });
});
