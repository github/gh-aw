import { test, expect, type Page } from "@playwright/test";

const presentation = (page: Page) => page.getByRole("dialog", { name: "GitHub Agentic Workflows presentation" });

test.beforeEach(async ({ page }) => {
  await page.goto("/gh-aw/");
});

test("presents all landing sections with bounded button and keyboard navigation", async ({ page }) => {
  await page.getByRole("button", { name: "Start slideshow" }).click();
  const dialog = presentation(page);
  const slides = dialog.locator("[data-slideshow-slides] > section");
  await expect(dialog).toBeVisible();
  await expect(slides).toHaveCount(9);
  await expect(dialog.locator("section:visible")).toHaveCount(1);
  await expect(dialog.locator("[data-slideshow-status]")).toContainText("1 / 9");
  await expect(dialog.getByRole("button", { name: "Previous slide" })).toBeDisabled();
  await expect(dialog.getByRole("button", { name: "Exit slideshow" })).toBeFocused();

  await page.keyboard.press("ArrowLeft");
  await expect(dialog.locator("[data-slideshow-status]")).toContainText("1 / 9");
  await dialog.getByRole("button", { name: "Next slide" }).click();
  await expect(dialog.getByRole("heading", { name: "Workflows that already work" })).toBeVisible();
  await expect(dialog.locator("[data-slideshow-status]")).toContainText("2 / 9");
  await page.keyboard.press("PageDown");
  await expect(dialog.getByRole("heading", { name: "Watch it run" })).toBeVisible();
  await page.keyboard.press("PageUp");
  await expect(dialog.locator("[data-slideshow-status]")).toContainText("2 / 9");
  await page.keyboard.press("End");
  await expect(dialog.getByRole("heading", { name: "Create your first workflow" })).toBeVisible();
  await expect(dialog.getByRole("button", { name: "Next slide" })).toBeDisabled();
  await page.keyboard.press("ArrowRight");
  await expect(dialog.locator("[data-slideshow-status]")).toContainText("9 / 9");
  await page.keyboard.press("Home");
  await expect(dialog.locator("[data-slideshow-status]")).toContainText("1 / 9");
  await dialog.getByRole("button", { name: "Next slide" }).click();
  await dialog.getByRole("button", { name: "Previous slide" }).click();
  await expect(dialog.locator("[data-slideshow-status]")).toContainText("1 / 9");
});

test("fits every slide to desktop and short windows in either theme", async ({ page }) => {
  await page.getByRole("button", { name: "Start slideshow" }).click();
  const dialog = presentation(page);
  for (const theme of ["light", "dark"]) {
    await page.evaluate(value => (document.documentElement.dataset.theme = value), theme);
    for (const size of [
      { width: 1440, height: 900 },
      { width: 1024, height: 600 },
    ]) {
      await page.setViewportSize(size);
      await dialog.getByRole("button", { name: "Exit slideshow" }).focus();
      await page.keyboard.press("Home");
      for (let index = 0; index < 9; index++) {
        await expect(dialog.locator("[data-slideshow-status]")).toContainText(`${index + 1} / 9`);
        await expect
          .poll(async () => {
            const slide = await dialog.locator("[data-slideshow-slides] > section:visible").boundingBox();
            const viewport = await dialog.locator("[data-slideshow-viewport]").boundingBox();
            return (
              !!slide &&
              !!viewport &&
              slide.width > 0 &&
              slide.height > 0 &&
              slide.x >= viewport.x - 1 &&
              slide.y >= viewport.y - 1 &&
              slide.x + slide.width <= viewport.x + viewport.width + 1 &&
              slide.y + slide.height <= viewport.y + viewport.height + 1
            );
          })
          .toBe(true);
        if (index < 8) await page.keyboard.press("ArrowRight");
      }
    }
  }
});

test("preserves interactive demos and restores the original page, focus and scroll", async ({ page }) => {
  await page.evaluate(() => window.scrollTo(0, 450));
  const scrollY = await page.evaluate(() => window.scrollY);
  await page.getByRole("button", { name: "Start slideshow" }).click();
  const dialog = presentation(page);
  await page.keyboard.press("ArrowRight");
  const tabs = dialog.locator('[data-wf-picker] [role="tab"]');
  await tabs.nth(1).click();
  await page.keyboard.press("ArrowRight");
  await expect(tabs.nth(2)).toHaveAttribute("aria-selected", "true");
  await expect(dialog.locator("[data-slideshow-status]")).toContainText("2 / 9");

  await page.keyboard.press("Escape");
  await expect(dialog).not.toBeVisible();
  await expect(page.getByRole("button", { name: "Start slideshow" })).toBeFocused();
  await expect(page.locator("main [data-slideshow-slides] > section")).toHaveCount(9);
  await expect(page.locator("main [data-slideshow-slides] > section[hidden]")).toHaveCount(0);
  await expect.poll(() => page.evaluate(() => window.scrollY)).toBe(scrollY);
  await expect(page.locator('[data-wf-picker] [role="tab"]').nth(2)).toHaveAttribute("aria-selected", "true");
  await page.getByRole("button", { name: "Start slideshow" }).click();
  await expect(dialog.locator("[data-slideshow-status]")).toContainText("1 / 9");
  await dialog.getByRole("button", { name: "Exit slideshow" }).click();
  await expect(dialog).not.toBeVisible();
});

test("hides the desktop control and exits when resized to mobile", async ({ page }) => {
  await page.getByRole("button", { name: "Start slideshow" }).click();
  await page.setViewportSize({ width: 390, height: 844 });
  await expect(presentation(page)).not.toBeVisible();
  await expect(page.getByRole("button", { name: "Start slideshow" })).not.toBeVisible();
  await expect(page.locator("main [data-slideshow-slides] > section")).toHaveCount(9);
  await page.getByRole("button", { name: "Menu", exact: true }).click();
  await expect(page.locator("#site-menu [data-slideshow-trigger]")).toHaveCount(0);
});

test("floats a translucent toolbar that becomes opaque on hover and keyboard focus", async ({ page }) => {
  const trigger = page.getByRole("button", { name: "Start slideshow" });
  const themeIcon = page.locator("starlight-theme-select svg").first();
  expect(await trigger.locator("svg").evaluate(element => element.outerHTML)).not.toBe(await themeIcon.evaluate(element => element.outerHTML));
  await trigger.click();
  const toolbar = presentation(page).getByRole("group", { name: "Slide navigation" });
  await expect(toolbar).toHaveCSS("position", "absolute");
  await page.mouse.move(0, 0);
  await expect(toolbar).toHaveCSS("opacity", "0.65");
  await toolbar.hover();
  await expect(toolbar).toHaveCSS("opacity", "1");
  await page.mouse.move(0, 0);
  await expect(toolbar).toHaveCSS("opacity", "0.65");
  await page.keyboard.press("Shift+Tab");
  await expect(toolbar).toHaveCSS("opacity", "1");
});

test("lets keyboard users leave demo tabs and navigate slides without a trap", async ({ page }) => {
  await page.getByRole("button", { name: "Start slideshow" }).click();
  await page.keyboard.press("PageDown");
  const dialog = presentation(page);
  const tabs = dialog.locator('[data-wf-picker] [role="tab"]');
  await tabs.first().focus();
  await page.keyboard.press("ArrowRight");
  await expect(tabs.nth(1)).toBeFocused();
  await page.keyboard.press("Tab");
  await expect(dialog.getByRole("button", { name: "Copy setup prompt" })).toBeFocused();
  await page.keyboard.press("Shift+Tab");
  await expect(tabs.nth(1)).toBeFocused();
  await page.keyboard.press("ArrowDown");
  await expect(dialog.locator("[data-slideshow-status]")).toContainText("3 / 9");
  const watchTabs = dialog.locator('[data-watch] [role="tab"]');
  await watchTabs.first().focus();
  await page.keyboard.press("ArrowUp");
  await expect(dialog.locator("[data-slideshow-status]")).toContainText("2 / 9");
  await tabs.nth(1).focus();
  await page.keyboard.press("PageDown");
  await expect(dialog.locator("[data-slideshow-status]")).toContainText("3 / 9");
  await watchTabs.first().focus();
  await page.keyboard.press("PageUp");
  await expect(dialog.locator("[data-slideshow-status]")).toContainText("2 / 9");
});

test("uses directional CSS View Transitions and handles rapid navigation", async ({ page }) => {
  await page.getByRole("button", { name: "Start slideshow" }).click();
  await page.evaluate(() => {
    const start = document.startViewTransition.bind(document);
    document.startViewTransition = (...args) => {
      document.documentElement.dataset.lastSlideDirection = document.documentElement.dataset.slideshowDirection;
      return start(...args);
    };
  });
  await page.keyboard.press("ArrowRight");
  await expect(presentation(page).locator("[data-slideshow-status]")).toContainText("2 / 9");
  await expect(page.locator("html")).toHaveAttribute("data-last-slide-direction", "forward");
  await expect
    .poll(() =>
      page.evaluate(() =>
        document
          .getAnimations()
          .filter(animation => animation instanceof CSSAnimation)
          .map(animation => animation.animationName)
      )
    )
    .toContain("aw-slide-in");
  await page.keyboard.press("ArrowLeft");
  await expect(presentation(page).locator("[data-slideshow-status]")).toContainText("1 / 9");
  await expect(page.locator("html")).toHaveAttribute("data-last-slide-direction", "backward");
  await page.keyboard.press("ArrowRight");
  await page.keyboard.press("ArrowRight");
  await page.keyboard.press("ArrowRight");
  await expect(presentation(page).locator("[data-slideshow-status]")).toContainText("4 / 9");
  await expect(page.locator("html")).not.toHaveAttribute("data-slideshow-direction");
  await page.keyboard.press("Escape");
  await expect(page.locator("main [data-slideshow-slides] > section")).toHaveCount(9);
});

test("navigates without animations for reduced motion or unsupported browsers", async ({ page }) => {
  await page.emulateMedia({ reducedMotion: "reduce" });
  await page.getByRole("button", { name: "Start slideshow" }).click();
  await page.keyboard.press("ArrowRight");
  await expect(presentation(page).locator("[data-slideshow-status]")).toContainText("2 / 9");
  await expect(page.locator("html")).not.toHaveAttribute("data-slideshow-direction");
  await page.emulateMedia({ reducedMotion: "no-preference" });
  await page.evaluate(() => {
    Object.defineProperty(document, "startViewTransition", { value: undefined, configurable: true });
  });
  await page.keyboard.press("ArrowRight");
  await expect(presentation(page).locator("[data-slideshow-status]")).toContainText("3 / 9");
  await expect(page.locator("html")).not.toHaveAttribute("data-slideshow-direction");
});

test("cleans up on page swaps and works after navigating away and back", async ({ page }) => {
  const errors: string[] = [];
  page.on("pageerror", error => errors.push(error.message));
  await page.getByRole("button", { name: "Start slideshow" }).click();
  await page.evaluate(() => document.dispatchEvent(new Event("astro:before-swap")));
  await expect(presentation(page)).not.toBeVisible();
  await expect(page.locator("main [data-slideshow-slides] > section")).toHaveCount(9);
  await page.evaluate(() => document.dispatchEvent(new Event("astro:page-load")));
  await page.getByRole("button", { name: "Start slideshow" }).click();
  await presentation(page).getByRole("link", { name: "Create a workflow" }).first().click();
  await expect(page).toHaveURL(/\/gh-aw\/setup\/creating-workflows\//);
  await expect(page.locator("[data-slideshow-trigger]")).toHaveCount(0);
  await page.goBack();
  await expect(page).toHaveURL(/\/gh-aw\/$/);
  await page.getByRole("button", { name: "Start slideshow" }).click();
  await expect(presentation(page).locator("[data-slideshow-status]")).toContainText("1 / 9");
  await page.keyboard.press("Escape");
  expect(errors).toEqual([]);
});
