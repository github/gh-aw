import { test, expect } from "@playwright/test";

test.describe("Homepage Links", () => {
  test.beforeEach(async ({ page }) => {
    await page.goto("/gh-aw/");
    await page.waitForLoadState("networkidle");
  });

  test("should feature the agentic prompt and link to workflow creation", async ({ page }) => {
    const command = page.locator(".aw-hero .aw-copy-text");
    await expect(command).toHaveText("$ /agentic-workflows create a daily status");

    const createWorkflow = page.getByRole("link", { name: "Create a workflow" }).first();
    await expect(createWorkflow).toBeVisible();
    await expect(createWorkflow).toHaveAttribute("href", "/gh-aw/setup/creating-workflows/");
  });

  test("should navigate to workflow creation when the CTA is clicked", async ({ page }) => {
    await page.getByRole("link", { name: "Create a workflow" }).first().click();
    await page.waitForLoadState("networkidle");
    await expect(page).toHaveURL(/\/gh-aw\/setup\/creating-workflows\//);
  });

  test("should provide accessible labels for homepage videos", async ({ page }) => {
    const videos = page.locator(".aw-watch-frame video");
    await expect(videos).toHaveCount(2);

    await expect(videos.nth(0)).toHaveAttribute("aria-label", "From the terminal");
    await expect(videos.nth(1)).toHaveAttribute("aria-label", "From github.com");
  });

  test("should include caption tracks on homepage videos", async ({ page }) => {
    const captionTracks = page.locator('.aw-watch-frame video track[kind="captions"]');
    await expect(captionTracks).toHaveCount(2);

    await expect(captionTracks.nth(0)).toHaveAttribute("src", "/gh-aw/videos/install-and-add-workflow-in-cli.vtt");
    await expect(captionTracks.nth(1)).toHaveAttribute("src", "/gh-aw/videos/create-workflow-on-github.vtt");
  });

  test("should restore landing interactions after an Astro page swap", async ({ page, context }) => {
    await context.grantPermissions(["clipboard-read", "clipboard-write"]);
    await page.evaluate(() => {
      document.dispatchEvent(new Event("astro:before-swap"));
      for (const selector of [".aw-hero", "[data-watch]", "[data-safe-root]"]) {
        const element = document.querySelector(selector);
        if (element) {
          const replacement = element.cloneNode(true);
          replacement.querySelectorAll(".hero-bg-a-canvas").forEach(canvas => canvas.classList.remove("is-ready"));
          element.replaceWith(replacement);
        }
      }
      document.dispatchEvent(new Event("astro:page-load"));
    });

    const canvas = page.locator(".aw-hero .hero-bg-a-canvas");
    if (await canvas.evaluate(element => (element as HTMLCanvasElement).getContext("webgl") !== null)) {
      await expect(canvas).toHaveClass(/is-ready/);
    }
    await expect(page.locator("[data-safe-root]")).toHaveAttribute("data-enhanced", "");
    const watchTabs = page.locator('[data-watch] [role="tab"]');
    await expect(page.locator("[data-watch]")).toHaveAttribute("data-enhanced", "");
    await watchTabs.nth(1).click();
    await expect(watchTabs.nth(1)).toHaveAttribute("aria-selected", "true");

    const copyButton = page.locator(".aw-hero .aw-copy-btn");
    await copyButton.click();
    await expect(page.locator(".aw-hero [data-copy-command]")).toHaveAttribute("data-copied", "");
  });
});
