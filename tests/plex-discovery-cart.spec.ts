import { test, expect } from '@playwright/test';

export const suite = {
  id: "plex-dashboard",
  summary: "Plex discovery cart — movie selection from browse/search, cart management, and watchlist sync",
  tags: ["api", "ui", "media", "search"],
} as const;

const BASE = (process.env.TEST_BASE_URL || 'http://127.0.0.1:8081') + '/beta';

const SEED_CART = [
  { tmdbId: 27205, title: 'Inception', year: 2010 },
  { tmdbId: 157336, title: 'Interstellar', year: 2014 },
  { tmdbId: 49026, title: 'The Dark Knight Rises', year: 2012 },
];

// Sets localStorage before the page loads (addInitScript runs before navigation).
async function seedCart(page: any, items = SEED_CART) {
  await page.addInitScript((cart: any[]) => {
    localStorage.setItem('plexdash.discovery.cart.v1', JSON.stringify(cart));
  }, items);
}

// Navigate straight to the discovery tab by pre-setting the tab preference.
// The shell reads pd-tab from localStorage on init, loads the partial, and inits Alpine.
async function goToDiscovery(page: any) {
  await page.addInitScript(() => {
    localStorage.setItem('pd-tab', 'discovery');
  });
  await page.goto(BASE, { waitUntil: 'domcontentloaded' });
  // Wait for the lazy-loaded partial to be injected and Alpine-initialized
  await page.waitForSelector('#discovery-tab', { timeout: 12000 });
}

// ── Cart icon ──────────────────────────────────────────────────────────────────

test('cart icon shows badge count when cart has items', async ({ page }) => {
  await seedCart(page);
  await goToDiscovery(page);

  const cartBtn = page.locator('#cart-icon-btn');
  await expect(cartBtn).toBeVisible();

  const badge = page.locator('#cart-badge');
  await expect(badge).toBeVisible();
  await expect(badge).toHaveText('3');

  console.log('[discovery-cart] cart icon badge shows correct count');
});

test('cart icon badge is hidden when cart is empty', async ({ page }) => {
  await page.addInitScript(() => {
    localStorage.setItem('plexdash.discovery.cart.v1', '[]');
  });
  await goToDiscovery(page);

  const badge = page.locator('#cart-badge');
  await expect(badge).toBeHidden();

  console.log('[discovery-cart] empty cart has no badge');
});

// ── Cart modal via icon ────────────────────────────────────────────────────────

test('clicking cart icon opens modal with cart items', async ({ page }) => {
  await seedCart(page);
  await goToDiscovery(page);

  await page.locator('#cart-icon-btn').click();

  const overlay = page.locator('#cart-modal-overlay');
  await expect(overlay).toBeVisible({ timeout: 3000 });

  const textarea = page.locator('#cart-modal-textarea');
  await expect(textarea).toBeVisible();

  const text = await textarea.inputValue();
  expect(text).toContain('## Cart');
  expect(text).toContain('Inception');
  expect(text).toContain('Interstellar');
  expect(text).toContain('The Dark Knight Rises');
  expect(text).toContain('2010');

  console.log('[discovery-cart] modal opens with correct markdown content');
});

test('cart modal content is valid markdown list format', async ({ page }) => {
  await seedCart(page);
  await goToDiscovery(page);

  await page.locator('#cart-icon-btn').click();
  await expect(page.locator('#cart-modal-overlay')).toBeVisible();

  const text = await page.locator('#cart-modal-textarea').inputValue();
  const lines = text.split('\n').filter((l: string) => l.trim());

  expect(lines[0]).toBe('## Cart');
  for (const line of lines.slice(1)) {
    expect(line).toMatch(/^- \*\*.+\*\* \(\d{4}\)$/);
  }

  console.log('[discovery-cart] markdown format is correct');
});

// ── Modal close ────────────────────────────────────────────────────────────────

test('cart modal closes via X button', async ({ page }) => {
  await seedCart(page);
  await goToDiscovery(page);

  await page.locator('#cart-icon-btn').click();
  await expect(page.locator('#cart-modal-overlay')).toBeVisible();

  await page.locator('#cart-modal-close').click();
  await expect(page.locator('#cart-modal-overlay')).toBeHidden({ timeout: 2000 });

  console.log('[discovery-cart] modal closes via X button');
});

test('cart modal closes via Escape key', async ({ page }) => {
  await seedCart(page);
  await goToDiscovery(page);

  await page.locator('#cart-icon-btn').click();
  await expect(page.locator('#cart-modal-overlay')).toBeVisible();

  await page.keyboard.press('Escape');
  await expect(page.locator('#cart-modal-overlay')).toBeHidden({ timeout: 2000 });

  console.log('[discovery-cart] modal closes via Escape');
});

test('cart modal closes via backdrop click', async ({ page }) => {
  await seedCart(page);
  await goToDiscovery(page);

  await page.locator('#cart-icon-btn').click();
  await expect(page.locator('#cart-modal-overlay')).toBeVisible();

  // Click top-left corner of the overlay (outside the inner box)
  await page.locator('#cart-modal-overlay').click({ position: { x: 10, y: 10 } });
  await expect(page.locator('#cart-modal-overlay')).toBeHidden({ timeout: 2000 });

  console.log('[discovery-cart] modal closes via backdrop click');
});

// ── Copy to clipboard (with clipboard permission granted) ──────────────────────

test('copy to clipboard button works when permission granted', async ({ browser }) => {
  const ctx = await browser.newContext({
    permissions: ['clipboard-read', 'clipboard-write'],
  });
  const page = await ctx.newPage();

  await page.addInitScript((cart: any[]) => {
    localStorage.setItem('plexdash.discovery.cart.v1', JSON.stringify(cart));
    localStorage.setItem('pd-tab', 'discovery');
  }, SEED_CART);

  await page.goto(BASE, { waitUntil: 'domcontentloaded' });
  await page.waitForSelector('#discovery-tab', { timeout: 12000 });

  await page.locator('#cart-icon-btn').click();
  await expect(page.locator('#cart-modal-overlay')).toBeVisible();

  await page.locator('#copy-cart-modal-btn').click();

  // Button should enter success state
  await expect(page.locator('#copy-cart-modal-btn')).toContainText('Copied', { timeout: 3000 });

  // Clipboard should contain the markdown
  const clipText = await page.evaluate(() => navigator.clipboard.readText());
  expect(clipText).toContain('## Cart');
  expect(clipText).toContain('Inception');

  await ctx.close();
  console.log('[discovery-cart] clipboard copy works with permission');
});

// ── Copy as Markdown button (in cart strip) ────────────────────────────────────

test('Copy as Markdown button opens modal', async ({ page }) => {
  await seedCart(page);
  await goToDiscovery(page);

  const copyBtn = page.locator('#copy-cart-btn');
  await expect(copyBtn).toBeVisible({ timeout: 5000 });
  await copyBtn.click();

  await expect(page.locator('#cart-modal-overlay')).toBeVisible({ timeout: 3000 });
  await expect(page.locator('#cart-modal-textarea')).toBeVisible();

  console.log('[discovery-cart] Copy as Markdown button opens the modal');
});

// ── Empty cart state in modal ──────────────────────────────────────────────────

test('modal shows empty state when cart is empty', async ({ page }) => {
  await page.addInitScript(() => {
    localStorage.setItem('plexdash.discovery.cart.v1', '[]');
  });
  await goToDiscovery(page);

  await page.locator('#cart-icon-btn').click();
  await expect(page.locator('#cart-modal-overlay')).toBeVisible();

  await expect(page.locator('#cart-modal-textarea')).toBeHidden();
  await expect(page.locator('#cart-modal-overlay')).toContainText('Cart is empty');
  await expect(page.locator('#copy-cart-modal-btn')).toBeHidden();

  console.log('[discovery-cart] empty cart shows empty state in modal');
});

// ── Cart item count in modal header ───────────────────────────────────────────

test('modal badge shows "1 item" (singular)', async ({ page }) => {
  await seedCart(page, [{ tmdbId: 1, title: 'Movie A', year: 2020 }]);
  await goToDiscovery(page);

  await page.locator('#cart-icon-btn').click();
  await expect(page.locator('#cart-modal-overlay')).toBeVisible();

  await expect(page.locator('#cart-modal-overlay .badge')).toContainText('1 item');

  console.log('[discovery-cart] modal badge shows singular "1 item"');
});

test('modal badge shows plural for multiple items', async ({ page }) => {
  await seedCart(page);
  await goToDiscovery(page);

  await page.locator('#cart-icon-btn').click();
  await expect(page.locator('#cart-modal-overlay')).toBeVisible();

  await expect(page.locator('#cart-modal-overlay .badge')).toContainText('3 items');

  console.log('[discovery-cart] modal badge shows plural "3 items"');
});
