import { test, expect } from "@playwright/test";

test.describe("Heading copy links", () => {
  test("copies the full hashed URL and announces success", async ({ page, context }) => {
    await context.grantPermissions(["clipboard-read", "clipboard-write"]);
    await page.goto("/gh-aw/introduction/how-they-work/");

    const link = page.locator(".sl-markdown-content .sl-anchor-link").first();
    await expect(link).toHaveAttribute("aria-label", /^Copy link to section:/);
    const expectedUrl = await link.evaluate(anchor => new URL(anchor.getAttribute("href") ?? "", window.location.href).toString());

    await link.click();

    await expect(page).toHaveURL(expectedUrl);
    await expect(link).toHaveAttribute("data-copied", "");
    await expect(page.locator(".aw-anchor-live")).toHaveText("Link copied");
    await expect.poll(() => page.evaluate(() => navigator.clipboard.readText())).toBe(expectedUrl);
  });

  test("keeps the heading URL in the address bar when clipboard access is denied", async ({ page }) => {
    await page.addInitScript(() => {
      Object.defineProperty(navigator, "clipboard", {
        configurable: true,
        value: {
          writeText: () => Promise.reject(new DOMException("Permission denied", "NotAllowedError")),
        },
      });
    });
    await page.goto("/gh-aw/introduction/how-they-work/");

    const link = page.locator(".sl-markdown-content .sl-anchor-link").first();
    const expectedUrl = await link.evaluate(anchor => new URL(anchor.getAttribute("href") ?? "", window.location.href).toString());

    await link.click();

    await expect(page).toHaveURL(expectedUrl);
    await expect(link).not.toHaveAttribute("data-copied", "");
  });
});
