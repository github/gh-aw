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

test("gives the docs homepage a site-level social card", async ({ page }) => {
  await page.goto("/gh-aw/");

  await expect(page).toHaveTitle("GitHub Agentic Workflows");
  await expect(page.locator('meta[property="og:title"]')).toHaveAttribute("content", "GitHub Agentic Workflows");
  await expect(page.locator('meta[property="og:type"]')).toHaveAttribute("content", "website");
  await expect(page.locator('meta[property="og:image"]')).toHaveAttribute("content", "https://github.github.com/gh-aw/og-home-1200x630.png");
  await expect(page.locator('meta[property="og:image:alt"]')).toHaveAttribute("content", /repository automation/);
});

test("serves the social image", async ({ request }) => {
  const response = await request.get("/gh-aw/og-home-1200x630.png");
  expect(response.ok()).toBe(true);
  expect(response.headers()["content-type"]).toBe("image/png");
});

test("gives documentation pages their own social image", async ({ page, request }) => {
  await page.goto("/gh-aw/setup/creating-workflows/");

  const image = "https://github.github.com/gh-aw/og/setup/creating-workflows.png";
  await expect(page.locator('meta[property="og:image"]')).toHaveAttribute("content", image);
  await expect(page.locator('meta[name="twitter:image"]')).toHaveAttribute("content", image);

  const response = await request.get("/gh-aw/og/setup/creating-workflows.png");
  expect(response.ok()).toBe(true);
  expect(response.headers()["content-type"]).toBe("image/png");
});

test("falls back to the site image for pages outside the docs collection", async ({ page }) => {
  await page.goto("/gh-aw/blog/");

  await expect(page.locator('meta[property="og:image"]')).toHaveAttribute("content", "https://github.github.com/gh-aw/og-home-1200x630.png");
});
