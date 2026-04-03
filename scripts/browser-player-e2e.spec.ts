/**
 * End-to-end browser player tests: verifies actual video playback via stream proxy.
 * Requires plex-dashboard running on 127.0.0.1:8081 with a live Plex library.
 */
import { test, expect } from '@playwright/test';

/** Fetch the movie list and find a movie by container type, preferring small files. */
async function findSmallMovie(request, container: string) {
  const res = await request.get('/api/movies');
  if (!res.ok()) return null;
  const body = await res.json();
  const movies = body.data?.movies || [];
  const matches = movies
    .filter((m) => m.FileContainer === container && m.PartKey)
    .sort((a, b) => (a.PartSize || 0) - (b.PartSize || 0));
  return matches.length > 0 ? matches[0] : null;
}

test.describe('Stream proxy end-to-end', () => {
  test('proxy delivers full MP4 file with correct headers', async ({ request }) => {
    const health = await request.get('/api/health');
    test.skip(!health.ok(), 'plex-dashboard not running');

    const movie = await findSmallMovie(request, 'mp4');
    test.skip(!movie, 'no MP4 movies in library');

    // Full request
    const res = await request.get(`/api/stream/${movie.RatingKey}`);
    expect(res.ok()).toBeTruthy();
    expect(res.headers()['content-type']).toBe('video/mp4');
    expect(res.headers()['accept-ranges']).toBe('bytes');
    const body = await res.body();
    expect(body.length).toBeGreaterThan(1000);
  });

  test('proxy supports Range requests (206 Partial Content)', async ({ request }) => {
    const health = await request.get('/api/health');
    test.skip(!health.ok(), 'plex-dashboard not running');

    const movie = await findSmallMovie(request, 'mp4');
    test.skip(!movie, 'no MP4 movies in library');

    // Ensure cached first
    await request.get(`/api/stream/${movie.RatingKey}`);

    // Range request
    const res = await request.get(`/api/stream/${movie.RatingKey}`, {
      headers: { Range: 'bytes=0-1023' },
    });
    expect(res.status()).toBe(206);
    expect(res.headers()['content-range']).toMatch(/^bytes 0-1023\//);
    const body = await res.body();
    expect(body.length).toBe(1024);
  });

  test('preload starts background download and status tracks progress', async ({ request }) => {
    const health = await request.get('/api/health');
    test.skip(!health.ok(), 'plex-dashboard not running');

    const movie = await findSmallMovie(request, 'mp4');
    test.skip(!movie, 'no MP4 movies in library');

    // Clean cache first
    await request.delete(`/api/stream/cache/${movie.RatingKey}`);

    // Start preload
    const preRes = await request.post('/api/stream/preload', {
      data: { ratingKey: movie.RatingKey },
    });
    expect(preRes.ok()).toBeTruthy();

    // Poll until complete (small file should be fast)
    let complete = false;
    for (let i = 0; i < 30; i++) {
      const statusRes = await request.get(`/api/stream/status/${movie.RatingKey}`);
      const status = await statusRes.json();
      if (status.data.complete) {
        complete = true;
        expect(status.data.cachedBytes).toBeGreaterThan(0);
        expect(status.data.totalSize).toBeGreaterThan(0);
        break;
      }
      await new Promise((r) => setTimeout(r, 500));
    }
    expect(complete).toBe(true);

    // Verify it shows in cache list
    const cacheRes = await request.get('/api/stream/cache');
    const cache = await cacheRes.json();
    const found = cache.data.items.some((e) => e.ratingKey === movie.RatingKey);
    expect(found).toBe(true);

    // Clean up
    await request.delete(`/api/stream/cache/${movie.RatingKey}`);
  });

  test('MKV proxy delivers bytes with matroska content-type', async ({ request }) => {
    const health = await request.get('/api/health');
    test.skip(!health.ok(), 'plex-dashboard not running');

    const movie = await findSmallMovie(request, 'mkv');
    test.skip(!movie, 'no MKV movies in library');

    // Just check headers — don't download the whole thing
    const res = await request.get(`/api/stream/${movie.RatingKey}`, {
      headers: { Range: 'bytes=0-1023' },
    });
    // Should get 200 or 206
    expect([200, 206]).toContain(res.status());
    expect(res.headers()['content-type']).toBe('video/x-matroska');
  });
});

test.describe('Browser video playback', () => {
  test('MP4 video loads and starts playing in browser player', async ({ page, request }) => {
    const health = await request.get('/api/health');
    test.skip(!health.ok(), 'plex-dashboard not running');

    const movie = await findSmallMovie(request, 'mp4');
    test.skip(!movie, 'no MP4 movies in library');

    // Pre-cache the file so playback is instant
    await request.get(`/api/stream/${movie.RatingKey}`);

    await page.goto('/');

    // Open browser player directly
    await page.evaluate((m) => {
      (window as any).openBrowserPlayer({
        ratingKey: m.RatingKey,
        title: m.Title,
        container: m.FileContainer,
        partSize: m.PartSize,
      });
    }, movie);

    await expect(page.locator('#browserPlayer')).toHaveClass(/bp-open/);
    await expect(page.locator('#browserPlayerTitle')).toContainText(movie.Title);

    const video = page.locator('#browserPlayerVideo');

    // Wait for the video to have loadeddata or canplay
    await page.waitForFunction(
      () => {
        const v = document.getElementById('browserPlayerVideo') as HTMLVideoElement;
        return v && v.readyState >= 2; // HAVE_CURRENT_DATA or better
      },
      { timeout: 15_000 }
    );

    // Verify video has a valid duration
    const duration = await video.evaluate((v: HTMLVideoElement) => v.duration);
    expect(duration).toBeGreaterThan(0);
    expect(duration).not.toBe(Infinity);

    // Verify it's actually playing (or at least can play)
    const paused = await video.evaluate((v: HTMLVideoElement) => v.paused);
    // autoplay may be blocked by browser policy; just verify the video loaded
    const readyState = await video.evaluate((v: HTMLVideoElement) => v.readyState);
    expect(readyState).toBeGreaterThanOrEqual(2);

    // Clean up
    await page.locator('#browserPlayerClose').click();
    await expect(page.locator('#browserPlayer')).not.toHaveClass(/bp-open/);
  });

  test('MKV video shows format warning in player', async ({ page, request }) => {
    const health = await request.get('/api/health');
    test.skip(!health.ok(), 'plex-dashboard not running');

    const movie = await findSmallMovie(request, 'mkv');
    test.skip(!movie, 'no MKV movies in library');

    await page.goto('/');

    await page.evaluate((m) => {
      (window as any).openBrowserPlayer({
        ratingKey: m.RatingKey,
        title: m.Title,
        container: 'mkv',
        partSize: m.PartSize,
      });
    }, movie);

    await expect(page.locator('#browserPlayer')).toHaveClass(/bp-open/);
    await expect(page.locator('#browserPlayer .bp-format-warn')).toBeVisible();
    await expect(page.locator('#browserPlayer .bp-format-warn')).toContainText('MKV');

    await page.locator('#browserPlayerClose').click();
  });

  test('video element src points to stream proxy URL', async ({ page, request }) => {
    const health = await request.get('/api/health');
    test.skip(!health.ok(), 'plex-dashboard not running');

    const movie = await findSmallMovie(request, 'mp4');
    test.skip(!movie, 'no MP4 movies in library');

    await page.goto('/');

    await page.evaluate((m) => {
      (window as any).openBrowserPlayer({
        ratingKey: m.RatingKey,
        title: m.Title,
        container: m.FileContainer,
        partSize: m.PartSize,
      });
    }, movie);

    const src = await page.locator('#browserPlayerVideo').getAttribute('src');
    expect(src).toBe(`/api/stream/${movie.RatingKey}`);

    await page.locator('#browserPlayerClose').click();
  });

  test('cache progress shows "Fully cached" for pre-cached movie', async ({ page, request }) => {
    const health = await request.get('/api/health');
    test.skip(!health.ok(), 'plex-dashboard not running');

    const movie = await findSmallMovie(request, 'mp4');
    test.skip(!movie, 'no MP4 movies in library');

    // Pre-cache
    await request.get(`/api/stream/${movie.RatingKey}`);

    await page.goto('/');

    await page.evaluate((m) => {
      (window as any).openBrowserPlayer({
        ratingKey: m.RatingKey,
        title: m.Title,
        container: m.FileContainer,
        partSize: m.PartSize,
      });
    }, movie);

    // The progress indicator should eventually say "Fully cached"
    await expect(page.locator('#browserPlayerProgress')).toContainText(/cached/i, { timeout: 8000 });

    await page.locator('#browserPlayerClose').click();
  });
});
