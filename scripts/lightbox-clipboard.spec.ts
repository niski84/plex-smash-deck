/**
 * Poster lightbox "Copy" uses the async Clipboard API with image/png or image/jpeg.
 * Run with plex-dashboard on 127.0.0.1:8081 (playwright.config baseURL).
 */
import { test, expect } from '@playwright/test';

const MIN_PNG =
  'iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mP8z8BQDwAEhQGAhKmMIQAAAABJRU5ErkJggg==';

test.describe('Poster lightbox clipboard', () => {
  test('Copy button writes an image to the clipboard', async ({ page, context, request }) => {
    const health = await request.get('/api/health');
    test.skip(!health.ok(), 'plex-dashboard not running — start server on :8081');

    await context.grantPermissions(['clipboard-read', 'clipboard-write'], {
      origin: 'http://127.0.0.1:8081',
    });

    await page.goto('/');

    await page.evaluate(async (b64) => {
      const overlay = document.getElementById('posterLightbox');
      const img = document.getElementById('posterLightboxImg') as HTMLImageElement;
      if (!overlay || !img) throw new Error('lightbox DOM missing');
      overlay.classList.add('lb-open');
      img.src = 'data:image/png;base64,' + b64;
      await new Promise<void>((resolve, reject) => {
        if (img.complete && img.naturalWidth > 0) {
          resolve();
          return;
        }
        img.onload = () => resolve();
        img.onerror = () => reject(new Error('img load failed'));
      });
      document.getElementById('posterLightboxCopy')?.click();
    }, MIN_PNG);

    await expect(page.locator('#posterLightboxHint')).toContainText('Image copied to clipboard', {
      timeout: 15_000,
    });

    const clipboardHasImage = await page.evaluate(async () => {
      const items = await navigator.clipboard.read();
      for (const item of items) {
        for (const t of item.types) {
          if (t.startsWith('image/')) {
            const blob = await item.getType(t);
            if (blob && blob.size > 0) return true;
          }
        }
      }
      return false;
    });
    expect(clipboardHasImage).toBe(true);
  });
});
