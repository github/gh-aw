import { test, expect, type Page } from "@playwright/test";

const INSTALL_PROMPT =
  "Initialize this repository for GitHub Agentic Workflows using https://raw.githubusercontent.com/github/gh-aw/main/install.md";

async function expectHeroPromptCopies(page: Page) {
  const prompt = page.locator(".aw-hero [data-copy-prompt]:visible").first();
  await prompt.getByRole("button", { name: "Copy installation prompt" }).click();
  await expect(prompt).toHaveAttribute("data-copied", "");
  expect(await page.evaluate(() => navigator.clipboard.readText())).toBe(INSTALL_PROMPT);
}

async function expectWorkflowsInteractive(page: Page) {
  const tabs = page.locator('[data-wf-picker] [role="tab"]');
  await expect(tabs.first()).toBeVisible();
  await tabs.nth(1).click();
  await expect(tabs.nth(1)).toHaveAttribute("aria-selected", "true");
  await expect(tabs.first()).toHaveAttribute("aria-selected", "false");

  const more = page.locator("[data-ma]").first();
  await more.locator("summary").click();
  await expect(more).toHaveAttribute("data-expanded", "");
  await expect(more).toHaveJSProperty("open", true);
}

test.describe("Homepage Links", () => {
  test.beforeEach(async ({ page }) => {
    await page.goto("/gh-aw/");
    await page.waitForLoadState("networkidle");
  });

  test("should feature the installation prompt and link to workflow creation", async ({ page }) => {
    // The page holds several hero variants; only the active one is visible.
    const prompt = page.locator(".aw-hero [data-copy-prompt]:visible").first();
    await expect(prompt).toHaveAttribute("data-copy-prompt", INSTALL_PROMPT);
    await expect(prompt.getByRole("button", { name: "Copy installation prompt" })).toBeVisible();

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
      for (const selector of [".aw-hero", "[data-watch]", "[data-safe-root]", "[data-wf-picker]", "[data-ma]"]) {
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

    await expectHeroPromptCopies(page);
    await expectWorkflowsInteractive(page);
  });

  test("should keep landing interactions working after real client-side navigation away and back", async ({ page, context }) => {
    await context.grantPermissions(["clipboard-read", "clipboard-write"]);
    const errors: string[] = [];
    page.on("pageerror", error => errors.push(error.message));
    page.on("console", message => {
      if (message.type() === "error") errors.push(message.text());
    });

    await page.getByRole("link", { name: "Create a workflow" }).first().click();
    await expect(page).toHaveURL(/\/gh-aw\/setup\/creating-workflows\//);
    await page.goBack();
    await expect(page).toHaveURL(/\/gh-aw\/$/);

    await expectHeroPromptCopies(page);
    await expectWorkflowsInteractive(page);
    expect(errors).toEqual([]);
  });
});
