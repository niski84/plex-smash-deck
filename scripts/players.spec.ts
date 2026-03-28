/**
 * Player discovery: API + Dashboard "Refresh Players" flow.
 * Run with plex-dashboard listening on 127.0.0.1:8081 (see playwright.config baseURL).
 *
 * Empty player lists are common when:
 * - plex.tv /api/resources fails (token, network, outage)
 * - No active Plex session (LG embedded app often absent from cloud; /status/sessions is empty when idle)
 * - LGTV_ADDR + LGTV_CLIENT_KEY not set (no guaranteed static LG row)
 */
import { test, expect } from '@playwright/test';

test.describe('Plex players discovery', () => {
  test('GET /api/health and /api/players return target + players list', async ({ request }) => {
    const health = await request.get('/api/health');
    test.skip(!health.ok(), 'plex-dashboard not running — start server on :8081');

    const res = await request.get('/api/players');
    expect(res.ok(), await res.text()).toBeTruthy();
    const body = await res.json();
    expect(body.success).toBeTruthy();
    const data = body.data;
    expect(data).toHaveProperty('players');
    expect(Array.isArray(data.players)).toBeTruthy();
    expect(data).toHaveProperty('targetClient');
    expect(typeof data.targetClient).toBe('string');
  });

  test('Dashboard: Refresh Players updates status', async ({ page, request }) => {
    const health = await request.get('/api/health');
    test.skip(!health.ok(), 'plex-dashboard not running — start server on :8081');

    await page.goto('/');
    await expect(page.locator('#refreshPlayersBtn')).toBeVisible();

    await page.locator('#refreshPlayersBtn').click();

    await expect(page.locator('[data-testid="players-status"]')).not.toContainText(
      'Uses discovered Plex players',
      { timeout: 30_000 }
    );

    const status = await page.locator('[data-testid="players-status"]').textContent();
    expect(status).toMatch(/Loaded \d+ player|No players discovered|Player refresh failed/i);
  });
});
