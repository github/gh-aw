import { expect, test } from "@playwright/test";

test("mobile menu supports the hamburger button selector", async ({ page }) => {
  await page.setViewportSize({ width: 390, height: 844 });
  await page.goto("/gh-aw/");

  const menuButton = page.locator(".hamburger-btn");
  await expect(menuButton).toBeVisible();
  await expect(menuButton).toHaveAttribute("aria-expanded", "false");
  await menuButton.click();
  await expect(menuButton).toHaveAttribute("aria-expanded", "true");
  await expect(page.locator("#site-menu")).toBeVisible();
});

test("homepage exposes one primary heading and labels decorative logos", async ({ page }) => {
  await page.goto("/gh-aw/");

  await expect(page.getByRole("heading", { level: 1 })).toHaveCount(1);

  const logos = page.locator(".theme-logo, .site-footer-logo");
  await expect(logos).toHaveCount(3);
  for (let index = 0; index < (await logos.count()); index++) {
    await expect(logos.nth(index)).toHaveAttribute("alt", "");
    await expect(logos.nth(index)).toHaveAttribute("aria-hidden", "true");
  }
});
