import { test, expect } from "@playwright/test";

test.describe("Mobile homepage Slides link", () => {
  for (const width of [320, 360, 393, 412, 799]) {
    test(`homepage slides link stays accessible at ${width}px`, async ({ page }) => {
      await page.setViewportSize({ width, height: 852 });
      await page.goto("/gh-aw/");

      const header = page.locator(".site-header");
      const slides = header.getByRole("link", { name: "Slides", exact: true });
      const search = header.locator("button[data-open-modal]");
      const menu = header.locator(".site-menu-toggle");
      await expect(slides).toBeVisible();
      await expect(slides).toBeInViewport({ ratio: 1 });
      await expect(slides).toHaveAttribute("href", "https://gh.io/gh-aw-slides");
      await expect(search).toBeVisible();
      await expect(menu).toBeVisible();
      await expect(menu).toHaveAttribute("aria-expanded", "false");

      const titleBox = await header.locator(".site-title").boundingBox();
      const searchBox = await search.boundingBox();
      const slidesBox = await slides.boundingBox();
      const menuBox = await menu.boundingBox();
      expect(titleBox).not.toBeNull();
      expect(searchBox).not.toBeNull();
      expect(slidesBox).not.toBeNull();
      expect(menuBox).not.toBeNull();
      if (titleBox && searchBox && slidesBox && menuBox) {
        expect(titleBox.x + titleBox.width).toBeLessThanOrEqual(searchBox.x);
        expect(searchBox.x + searchBox.width).toBeLessThanOrEqual(slidesBox.x);
        expect(slidesBox.x + slidesBox.width).toBeLessThanOrEqual(menuBox.x);
        expect(slidesBox.width).toBeGreaterThanOrEqual(44);
        expect(slidesBox.height).toBeGreaterThanOrEqual(44);
      }

      await page.route("https://gh.io/gh-aw-slides", route => route.fulfill({ body: "Slides" }));
      await slides.click();
      await expect(page).toHaveURL("https://gh.io/gh-aw-slides");
    });
  }

  test("should keep the slides header link homepage-only on mobile", async ({ page }) => {
    await page.setViewportSize({ width: 360, height: 800 });
    await page.goto("/gh-aw/introduction/overview/");
    await expect(page.locator(".site-header").getByRole("link", { name: "Slides", exact: true })).toHaveCount(0);
  });
});
