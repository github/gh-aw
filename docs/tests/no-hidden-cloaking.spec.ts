import { test, expect } from "@playwright/test";
import { readFile } from "node:fs/promises";
import { dirname, resolve } from "node:path";
import { fileURLToPath } from "node:url";

const testDir = dirname(fileURLToPath(import.meta.url));
const repoRoot = resolve(testDir, "../..");

test.describe("Hidden text cloaking guard", () => {
  test("README exposes agent guidance without hidden setup instructions", async () => {
    const readme = await readFile(resolve(repoRoot, "README.md"), "utf8");

    expect(readme).not.toMatch(/<!--[\s\S]*?(fellow agent|agentic workflows)[\s\S]*?-->/i);
    expect(readme).toMatch(/<details>\s*<summary>Agent quick links<\/summary>[\s\S]*Hello fellow agent![\s\S]*<\/details>/i);
    expect(readme).not.toContain("If this repository hasn't been configured with agentic workflows yet");
  });

  test("responsive header keeps primary links visible and the mobile sheet closed by default", async ({ page }) => {
    // At desktop widths the primary links are visible in the header, not tucked into a menu.
    await page.setViewportSize({ width: 900, height: 768 });
    await page.goto("/gh-aw/");
    await page.waitForLoadState("networkidle");

    await expect(page.locator(".site-menu-toggle")).toBeHidden();
    await expect(page.locator(".site-header-nav a")).toHaveCount(3);
    for (const link of await page.locator(".site-header-nav a").all()) {
      await expect(link).toBeVisible();
    }

    // Below 50rem the same links live in the menu sheet, which is hidden (display: none,
    // so out of the accessibility tree) until the menu button opens it.
    await page.setViewportSize({ width: 390, height: 844 });
    const menuButton = page.locator(".site-menu-toggle");
    const sheet = page.locator("#site-menu");

    await expect(menuButton).toBeVisible();
    await expect(sheet).toBeHidden();

    await menuButton.click();
    await expect(menuButton).toHaveAttribute("aria-expanded", "true");
    await expect(sheet.locator(".mobile-nav a")).toHaveCount(3);
    await expect(sheet.locator("a", { hasText: "Get started" })).toBeVisible();

    await page.keyboard.press("Escape");
    await expect(menuButton).toHaveAttribute("aria-expanded", "false");
    await expect(sheet).toBeHidden();
  });
});
