/**
 * LG volume API contract (no TV required when LGTV_ADDR unset).
 */
import { test, expect } from '@playwright/test';

test.describe('LG volume API', () => {
  test('POST /api/lg/volume accepts JSON level', async ({ request }) => {
    const health = await request.get('/api/health');
    test.skip(!health.ok(), 'plex-dashboard not running on baseURL');

    const res = await request.post('/api/lg/volume', {
      data: { level: 42 },
    });
    expect(res.ok(), await res.text()).toBeTruthy();
    const body = await res.json();
    expect(body.success).toBeTruthy();
    expect(body.data).toHaveProperty('supported');
    // Either TV not configured (false) or SSAP responded (true + volume or error).
    if (body.data.supported === true && !body.data.error) {
      expect(typeof body.data.volume).toBe('number');
    }
  });

  test('POST /api/lg/volume rejects empty body', async ({ request }) => {
    const health = await request.get('/api/health');
    test.skip(!health.ok(), 'plex-dashboard not running on baseURL');

    const res = await request.post('/api/lg/volume', {
      data: {},
      headers: { 'Content-Type': 'application/json' },
    });
    expect(res.status()).toBe(400);
  });
});
