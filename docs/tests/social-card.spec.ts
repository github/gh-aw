import { test, expect } from "@playwright/test";

test("uses the short URL for the docs homepage social card", async ({ page }) => {
  await page.goto("/gh-aw/");

  await expect(page.locator('meta[property="og:url"]')).toHaveAttribute("content", "https://gh.io/gh-aw");
  await expect(page.locator('meta[name="twitter:url"]')).toHaveAttribute("content", "https://gh.io/gh-aw");
});

test("keeps individual documentation page URLs in social cards", async ({ page }) => {
  await page.goto("/gh-aw/introduction/how-they-work/");

  const pageUrl = "https://github.github.com/gh-aw/introduction/how-they-work/";
  await expect(page.locator('meta[property="og:url"]')).toHaveAttribute("content", pageUrl);
  await expect(page.locator('meta[name="twitter:url"]')).toHaveAttribute("content", pageUrl);
});
