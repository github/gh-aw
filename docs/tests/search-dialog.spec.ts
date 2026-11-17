import { test, expect } from '@playwright/test';

const VIEWPORTS = [
  { name: 'desktop', width: 1366, height: 768 },
  { name: 'tablet', width: 834, height: 1194 },
];

for (const viewport of VIEWPORTS) {
  test(`search dialog is usable and dismissible on ${viewport.name}`, async ({ page }) => {
    await page.setViewportSize({ width: viewport.width, height: viewport.height });
    await page.goto('/gh-aw/');
    await page.waitForLoadState('networkidle');

    await page.getByRole('button', { name: 'Search' }).click();

    const openDialog = page.locator('dialog[open]');
    await expect(openDialog).toBeVisible();
    await expect(openDialog.getByRole('search').getByRole('textbox', { name: 'Search' })).toBeVisible();

    await page.keyboard.press('Escape');
    await expect(openDialog).toHaveCount(0);

    await page.getByRole('button', { name: 'Search' }).click();
    const reopenedDialog = page.locator('dialog[open]');
    await expect(reopenedDialog).toBeVisible();
    await expect(
      reopenedDialog.getByRole('search').getByRole('textbox', { name: 'Search' })
    ).toBeVisible();
    await page.keyboard.press('Escape');
    await expect(reopenedDialog).toHaveCount(0);

    await page.getByRole('banner').getByRole('link', { name: 'Get started', exact: true }).click();
    await expect(page).toHaveURL(/\/gh-aw\/setup\/quick-start\//);
  });
}
