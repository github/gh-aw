import { test, expect, type Page } from "@playwright/test";

const presentation = (page: Page) => page.getByRole("dialog", { name: "GitHub Agentic Workflows presentation" });

async function startSlideshow(page: Page) {
  const trigger = page.getByRole("button", { name: "Start slideshow" });
  await trigger.click();
  await expect(presentation(page)).toBeVisible();
}

const runtimeAsset = /\/slideshow(?:-[^/.]+)?\.[^/]+\.js(?:\?|$)/;

test.beforeEach(async ({ page }) => {
  await page.goto("/gh-aw/");
});

test("dims the toolbar only when neither hovered nor focused", async ({ page }) => {
  await startSlideshow(page);
  const controls = presentation(page).getByRole("group", { name: "Slide navigation" });
  await page.mouse.move(0, 0);
  await page.evaluate(() => (document.activeElement as HTMLElement)?.blur());
  await expect(controls).toHaveCSS("opacity", "0.1");

  await controls.hover();
  await expect(controls).toHaveCSS("opacity", "1");
  await page.mouse.move(0, 0);
  await expect(controls).toHaveCSS("opacity", "0.1");

  await controls.getByRole("button", { name: "Exit slideshow" }).focus();
  await expect(controls).toHaveCSS("opacity", "1");
  await page.evaluate(() => (document.activeElement as HTMLElement)?.blur());
  await expect(controls).toHaveCSS("opacity", "0.1");
});

test("loads the complete slideshow runtime only on first use", async ({ page }) => {
  const initialAssets = await page.evaluate(pattern => {
    const matcher = new RegExp(pattern);
    return performance
      .getEntriesByType("resource")
      .filter(entry => matcher.test(entry.name))
      .map(entry => entry.name);
  }, runtimeAsset.source);
  expect(initialAssets).toEqual([]);
  await expect(page.locator("[data-slideshow-ink]")).not.toHaveAttribute("data-tool");
  await expect(page.locator("main [data-snippet-trigger]")).toHaveCount(0);
  const requests: string[] = [];
  page.on("request", request => {
    if (runtimeAsset.test(request.url())) requests.push(request.url());
  });
  await startSlideshow(page);
  expect(requests.length).toBeGreaterThan(0);
  await expect(page.locator("[data-slideshow-ink]")).toHaveAttribute("data-tool", "pointer");
  await page.keyboard.press("PageDown");
  await expect(presentation(page).getByRole("button", { name: "Expand daily-issue-summary.md", exact: true })).toBeVisible();
  const loaded = [...requests];
  await page.keyboard.press("Escape");
  await startSlideshow(page);
  expect(requests).toEqual(loaded);
});

test("reports lazy-load failures and restores the launcher", async ({ page }) => {
  await page.route(runtimeAsset, route => route.abort());
  const trigger = page.getByRole("button", { name: "Start slideshow" });
  await trigger.click();
  await expect(page.getByRole("alert")).toHaveText("Unable to load the slideshow. Reload the page and try again.");
  await expect(trigger).toBeEnabled();
  await expect(trigger).not.toHaveAttribute("aria-busy");
  await expect(presentation(page)).not.toBeVisible();
  await expect(page.locator("main [data-slideshow-slides] > section")).toHaveCount(12);
  await page.unroute(runtimeAsset);
  await page.reload();
  await startSlideshow(page);
});

test("does not open a stale slideshow when navigation interrupts lazy loading", async ({ page }) => {
  let resume = () => {};
  const held = new Promise<void>(resolve => {
    resume = resolve;
  });
  let intercepted = false;
  await page.route(runtimeAsset, async route => {
    intercepted = true;
    await held;
    await route.continue();
  });
  try {
    await page.getByRole("button", { name: "Start slideshow" }).click();
    await expect.poll(() => intercepted).toBe(true);
    await expect(page.getByRole("button", { name: "Start slideshow" })).toHaveAttribute("aria-busy", "true");
    await page.getByRole("banner").getByRole("link", { name: "Get started", exact: true }).click();
    await expect(page).toHaveURL(/\/gh-aw\/setup\/quick-start\//);
    resume();
    await expect(page.locator("#landing-slideshow")).toHaveCount(0);
    await page.goBack();
    await startSlideshow(page);
  } finally {
    resume();
  }
});

test("ignores a completed lazy import after its launcher was disposed", async ({ page }) => {
  let resume = () => {};
  const held = new Promise<void>(resolve => {
    resume = resolve;
  });
  let asset = "";
  await page.route(runtimeAsset, async route => {
    asset = route.request().url();
    await held;
    await route.continue();
  });
  try {
    await page.getByRole("button", { name: "Start slideshow" }).click();
    await expect.poll(() => asset).not.toBe("");
    await page.evaluate(() => document.dispatchEvent(new Event("astro:before-swap")));
    resume();
    await page.evaluate(async url => {
      await import(url);
    }, asset);
    await expect(presentation(page)).not.toBeVisible();
    await expect(page.locator("[data-slideshow-ink]")).not.toHaveAttribute("data-tool");
    await page.evaluate(() => document.dispatchEvent(new Event("astro:page-load")));
    await startSlideshow(page);
  } finally {
    resume();
  }
});

test("presents all landing sections with bounded button and keyboard navigation", async ({ page }) => {
  const titleSlide = page.locator("main [data-slideshow-slides] > .aw-title-slide");
  await expect(titleSlide).not.toBeVisible();
  await expect(page.locator("main [data-slideshow-slides] > .aw-hero")).toBeVisible();
  await startSlideshow(page);
  const dialog = presentation(page);
  const slides = dialog.locator("[data-slideshow-slides] > section");
  await expect(dialog).toBeVisible();
  await expect(slides).toHaveCount(12);
  await expect(dialog.locator("section:visible")).toHaveCount(1);
  await expect(dialog.getByRole("heading", { name: "GitHub Agentic Workflows", exact: true })).toBeVisible();
  await expect(dialog.locator("[data-slideshow-status]")).toContainText("1 / 12 — GitHub Agentic Workflows");
  await expect(dialog.getByRole("button", { name: "Previous slide" })).toBeDisabled();
  await expect(dialog.getByRole("button", { name: "Exit slideshow" })).toBeFocused();

  await page.keyboard.press("ArrowLeft");
  await expect(dialog.locator("[data-slideshow-status]")).toContainText("1 / 12");
  await dialog.getByRole("button", { name: "Next slide" }).click();
  await expect(dialog.locator(".aw-hero")).toBeVisible();
  await expect(dialog.locator("[data-slideshow-status]")).toContainText("2 / 12");
  await dialog.getByRole("button", { name: "Next slide" }).click();
  await expect(dialog.getByRole("heading", { name: "Workflows that already work" })).toBeVisible();
  await expect(dialog.locator("[data-slideshow-status]")).toContainText("3 / 12");
  await page.keyboard.press("PageDown");
  await expect(dialog.getByRole("heading", { name: "Watch it run" })).toBeVisible();
  await page.keyboard.press("PageUp");
  await expect(dialog.locator("[data-slideshow-status]")).toContainText("3 / 12");
  await page.keyboard.press("End");
  await expect(dialog.getByRole("heading", { name: "Create your first workflow" })).toBeVisible();
  await expect(dialog.getByRole("button", { name: "Next slide" })).toBeDisabled();
  await page.keyboard.press("ArrowRight");
  await expect(dialog.locator("[data-slideshow-status]")).toContainText("12 / 12");
  await page.keyboard.press("Home");
  await expect(dialog.locator("[data-slideshow-status]")).toContainText("1 / 12");
  await dialog.getByRole("button", { name: "Next slide" }).click();
  await dialog.getByRole("button", { name: "Previous slide" }).click();
  await expect(dialog.locator("[data-slideshow-status]")).toContainText("1 / 12");
});

test("renders the hero background on the presentation-only title slide", async ({ page }) => {
  const titleSlide = page.locator("[data-slideshow-slides] > .aw-title-slide");
  await expect(titleSlide).not.toBeVisible();
  await startSlideshow(page);
  const background = titleSlide.locator("[data-hero-bg-a]");
  const canvas = background.locator("canvas");
  await expect(background).toHaveAttribute("data-placement", "hero");
  await expect(background.locator("..")).toHaveAttribute("aria-hidden", "true");
  await expect(background.locator("..")).toHaveCSS("pointer-events", "none");
  for (const theme of ["light", "dark"]) {
    await page.evaluate(value => (document.documentElement.dataset.theme = value), theme);
    await expect(canvas).toBeVisible();
    await expect(canvas).toHaveClass(/is-ready/);
    await expect(canvas).toHaveCSS("opacity", "1");
    await expect
      .poll(async () => {
        const slideBounds = await titleSlide.boundingBox();
        const backgroundBounds = await background.boundingBox();
        return (
          !!slideBounds &&
          !!backgroundBounds &&
          Math.abs(slideBounds.x - backgroundBounds.x) < 1 &&
          Math.abs(slideBounds.y - backgroundBounds.y) < 1 &&
          Math.abs(slideBounds.width - backgroundBounds.width) < 1 &&
          Math.abs(slideBounds.height - backgroundBounds.height) < 1
        );
      })
      .toBe(true);
  }
  await page.keyboard.press("PageDown");
  await expect(titleSlide).not.toBeVisible();
  await page.keyboard.press("Home");
  await expect(canvas).toBeVisible();
  await page.keyboard.press("Escape");
  await expect(titleSlide).not.toBeVisible();
  await startSlideshow(page);
  await expect(canvas).toBeVisible();
  await expect(canvas).toHaveCSS("opacity", "1");
});

test("presents the security diagram and layers, then cost dashboard and guardrails, on separate slides", async ({ page }) => {
  const deck = page.locator("main [data-slideshow-slides]");
  await expect(deck.locator("#safety .tri-fig")).toBeVisible();
  await expect(deck.locator("#safety-layers [data-safe-root]")).toBeVisible();
  await expect(deck.locator("#cost .aw-dash")).toBeVisible();
  await expect(deck.locator("#cost-guardrails .aw-points")).toBeVisible();

  await startSlideshow(page);
  const dialog = presentation(page);
  for (const [number, title, visible, absent] of [
    [5, "Safe by default", ".tri-fig", "[data-safe-root]"],
    [6, "Six layers of defence", "[data-safe-root]", ".tri-fig"],
    [8, "Cost under control", ".aw-dash", ".aw-points"],
    [9, "Budget guardrails", ".aw-points", ".aw-dash"],
  ] as const) {
    await dialog.getByRole("button", { name: "Exit slideshow" }).focus();
    await page.keyboard.press("Home");
    for (let index = 1; index < number; index++) await page.keyboard.press("ArrowRight");
    await expect(dialog.locator("[data-slideshow-status]")).toContainText(`${number} / 12 — ${title}`);
    const slide = dialog.locator("[data-slideshow-slides] > section:visible");
    await expect(slide.locator(visible)).toBeVisible();
    await expect(slide.locator(absent)).toHaveCount(0);
  }
  await page.keyboard.press("Escape");
  await expect(deck.locator("#safety .tri-fig")).toBeVisible();
  await expect(deck.locator("#safety-layers [data-safe-root]")).toBeVisible();
});

test("fits every slide to desktop and short windows in either theme", async ({ page }) => {
  await startSlideshow(page);
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
      for (let index = 0; index < 12; index++) {
        await expect(dialog.locator("[data-slideshow-status]")).toContainText(`${index + 1} / 12`);
        await expect
          .poll(async () => {
            const slide = await dialog.locator("[data-slideshow-slides] > section:visible").boundingBox();
            const viewport = await dialog.locator("[data-slideshow-viewport]").boundingBox();
            return (
              !!slide &&
              !!viewport &&
              slide.width > 0 &&
              slide.height > 0 &&
              viewport.x === 0 &&
              viewport.y === 0 &&
              viewport.width === size.width &&
              viewport.height === size.height &&
              Math.abs(slide.x) < 1 &&
              Math.abs(slide.y) < 1 &&
              Math.abs(slide.width - size.width) < 1 &&
              Math.abs(slide.height - size.height) < 1
            );
          })
          .toBe(true);
        if (index < 11) await page.keyboard.press("ArrowRight");
      }
    }
  }
});

test("preserves interactive demos and restores the original page, focus and scroll", async ({ page }) => {
  await page.evaluate(() => window.scrollTo(0, 450));
  const scrollY = await page.evaluate(() => window.scrollY);
  await startSlideshow(page);
  const dialog = presentation(page);
  await page.keyboard.press("ArrowRight");
  await page.keyboard.press("ArrowRight");
  const tabs = dialog.locator('[data-wf-picker] [role="tab"]');
  await tabs.nth(1).click();
  await page.keyboard.press("ArrowRight");
  await expect(tabs.nth(2)).toHaveAttribute("aria-selected", "true");
  await expect(dialog.locator("[data-slideshow-status]")).toContainText("3 / 12");

  await page.keyboard.press("Escape");
  await expect(dialog).not.toBeVisible();
  await expect(page.getByRole("button", { name: "Start slideshow" })).toBeFocused();
  await expect(page.locator("main [data-slideshow-slides] > section")).toHaveCount(12);
  await expect(page.locator("main [data-slideshow-slides] > section[hidden]")).toHaveCount(0);
  await expect(page.locator("main [data-slideshow-slides] > .aw-title-slide")).not.toBeVisible();
  await expect.poll(() => page.evaluate(() => window.scrollY)).toBe(scrollY);
  await expect(page.locator('[data-wf-picker] [role="tab"]').nth(2)).toHaveAttribute("aria-selected", "true");
  await startSlideshow(page);
  await expect(dialog.locator("[data-slideshow-status]")).toContainText("1 / 12");
  await dialog.getByRole("button", { name: "Exit slideshow" }).click();
  await expect(dialog).not.toBeVisible();
});

test("hides the desktop control and exits when resized to mobile", async ({ page }) => {
  await startSlideshow(page);
  await page.setViewportSize({ width: 390, height: 844 });
  await expect(presentation(page)).not.toBeVisible();
  await expect(page.getByRole("button", { name: "Start slideshow" })).not.toBeVisible();
  await expect(page.locator("main [data-slideshow-slides] > section")).toHaveCount(12);
  await page.getByRole("button", { name: "Menu", exact: true }).click();
  await expect(page.locator("#site-menu [data-slideshow-trigger]")).toHaveCount(0);
});

test("floats a translucent toolbar that becomes opaque on hover and keyboard focus", async ({ page }) => {
  const trigger = page.getByRole("button", { name: "Start slideshow" });
  const themeIcon = page.locator("starlight-theme-select svg").first();
  expect(await trigger.locator("svg").evaluate(element => element.outerHTML)).not.toBe(await themeIcon.evaluate(element => element.outerHTML));
  await startSlideshow(page);
  const toolbar = presentation(page).getByRole("group", { name: "Slide navigation" });
  await expect(toolbar).toHaveCSS("position", "absolute");
  await expect(toolbar).toHaveCSS("border-radius", "16px");
  const viewportBounds = await presentation(page).locator("[data-slideshow-viewport]").boundingBox();
  const toolbarBounds = await toolbar.boundingBox();
  if (!viewportBounds || !toolbarBounds) throw new Error("Presentation viewport and toolbar must be visible");
  expect(toolbarBounds.y).toBeLessThan(viewportBounds.y + viewportBounds.height);
  expect(toolbarBounds.y + toolbarBounds.height).toBeLessThanOrEqual(viewportBounds.y + viewportBounds.height + 1);
  const size = page.viewportSize();
  if (!size) throw new Error("Presentation viewport size must be available");
  expect(toolbarBounds.width).toBeLessThan(size.width * 0.7);
  expect(toolbarBounds.height).toBeLessThanOrEqual(48);
  expect(Math.abs(toolbarBounds.x + toolbarBounds.width / 2 - size.width / 2)).toBeLessThan(1);
  expect(size.height - toolbarBounds.y - toolbarBounds.height).toBeGreaterThan(16);
  await page.mouse.move(0, 0);
  await expect(toolbar).toHaveCSS("opacity", "0.1");
  await toolbar.hover();
  await expect(toolbar).toHaveCSS("opacity", "1");
  await page.mouse.move(0, 0);
  await expect(toolbar).toHaveCSS("opacity", "0.1");
  await page.keyboard.press("Shift+Tab");
  await expect(toolbar).toHaveCSS("opacity", "1");
  await toolbar.getByRole("button", { name: "Drawing tools", exact: true }).click();
  await page.locator("[data-slideshow-viewport]").click({ position: { x: 1, y: 1 } });
  await page.mouse.move(0, 0);
  await expect(toolbar).toHaveCSS("opacity", "1");
});

test("lets keyboard users leave demo tabs and navigate slides without a trap", async ({ page }) => {
  await startSlideshow(page);
  await page.keyboard.press("PageDown");
  await page.keyboard.press("PageDown");
  const dialog = presentation(page);
  await expect(dialog.locator("[data-slideshow-status]")).toContainText("3 / 12");
  const tabs = dialog.locator('[data-wf-picker] [role="tab"]');
  await tabs.first().focus();
  await page.keyboard.press("ArrowRight");
  await expect(tabs.nth(1)).toBeFocused();
  await page.keyboard.press("Tab");
  await expect(dialog.getByRole("button", { name: "Copy setup prompt" })).toBeFocused();
  await page.keyboard.press("Shift+Tab");
  await expect(tabs.nth(1)).toBeFocused();
  await page.keyboard.press("ArrowDown");
  await expect(dialog.locator("[data-slideshow-status]")).toContainText("4 / 12");
  const watchTabs = dialog.locator('[data-watch] [role="tab"]');
  await watchTabs.first().focus();
  await page.keyboard.press("ArrowUp");
  await expect(dialog.locator("[data-slideshow-status]")).toContainText("3 / 12");
  await tabs.nth(1).focus();
  await page.keyboard.press("PageDown");
  await expect(dialog.locator("[data-slideshow-status]")).toContainText("4 / 12");
  await watchTabs.first().focus();
  await page.keyboard.press("PageUp");
  await expect(dialog.locator("[data-slideshow-status]")).toContainText("3 / 12");
});

test("hides annotated secondary content only during the presentation", async ({ page }) => {
  await expect(page.locator(".aw-hero .aw-cta-note:visible")).toHaveCount(1);
  await expect(page.locator(".wf-detail:visible")).toHaveCount(1);
  await expect(page.locator(".ma")).toBeVisible();
  await startSlideshow(page);
  const dialog = presentation(page);
  await expect(dialog.locator("[data-slideshow-hide]:visible")).toHaveCount(0);
  await page.keyboard.press("PageDown");
  await page.keyboard.press("PageDown");
  await expect(dialog.locator("[data-slideshow-status]")).toContainText("3 / 12");
  await expect(dialog.locator(".wf-outcome:visible")).toHaveCount(1);
  await expect(dialog.locator(".wf-detail:visible")).toHaveCount(0);
  await expect(dialog.locator(".ma")).not.toBeVisible();
  await page.keyboard.press("Escape");
  await expect(page.locator(".aw-hero .aw-cta-note:visible")).toHaveCount(1);
  await expect(page.locator(".wf-detail:visible")).toHaveCount(1);
  await expect(page.locator(".ma")).toBeVisible();
});

test("skips annotated slide sections and restores them on exit", async ({ page }) => {
  await page.evaluate(() => {
    document.querySelector("#watch")?.setAttribute("data-slideshow-hide", "");
    document.dispatchEvent(new Event("astro:page-load"));
  });
  await startSlideshow(page);
  const dialog = presentation(page);
  await expect(dialog.locator("[data-slideshow-status]")).toContainText("1 / 11");
  await page.keyboard.press("PageDown");
  await expect(dialog.locator("[data-slideshow-status]")).toContainText("2 / 11");
  await page.keyboard.press("PageDown");
  await expect(dialog.locator("[data-slideshow-status]")).toContainText("3 / 11");
  await page.keyboard.press("PageDown");
  await expect(dialog.locator("[data-slideshow-status]")).toContainText("4 / 11");
  await expect(dialog.locator("[data-slideshow-status]")).not.toContainText("Watch it run");
  await expect(dialog.locator("#watch")).not.toBeVisible();
  await page.keyboard.press("End");
  await expect(dialog.locator("[data-slideshow-status]")).toContainText("11 / 11");
  await page.keyboard.press("Escape");
  await expect(page.locator("main #watch")).toBeVisible();
});

test("uses directional CSS View Transitions and handles rapid navigation", async ({ page }) => {
  await startSlideshow(page);
  await page.evaluate(() => {
    const start = document.startViewTransition.bind(document);
    document.startViewTransition = (...args) => {
      document.documentElement.dataset.lastSlideDirection = document.documentElement.dataset.slideshowDirection;
      return start(...args);
    };
  });
  await page.keyboard.press("ArrowRight");
  await expect(presentation(page).locator("[data-slideshow-status]")).toContainText("2 / 12");
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
  await expect(page.locator("html")).toHaveCSS("--aw-slide-offset", "100vw");
  await expect
    .poll(() =>
      page.evaluate(() => {
        const animation = document.getAnimations().find(animation => animation instanceof CSSAnimation && animation.animationName === "aw-slide-in");
        const frame = (animation?.effect as KeyframeEffect | null)?.getKeyframes()[0];
        if (!frame?.transform) return null;
        const transform = new DOMMatrix(String(frame.transform));
        return { x: transform.m41, scaleX: transform.m11, scaleY: transform.m22 };
      })
    )
    .toEqual({ x: page.viewportSize()!.width, scaleX: 1, scaleY: 1 });
  await page.keyboard.press("ArrowLeft");
  await expect(presentation(page).locator("[data-slideshow-status]")).toContainText("1 / 12");
  await expect(page.locator("html")).toHaveAttribute("data-last-slide-direction", "backward");
  await expect(page.locator("html")).toHaveCSS("--aw-slide-offset", "-100vw");
  await page.keyboard.press("ArrowRight");
  await page.keyboard.press("ArrowRight");
  await page.keyboard.press("ArrowRight");
  await expect(presentation(page).locator("[data-slideshow-status]")).toContainText("4 / 12");
  await expect(page.locator("html")).not.toHaveAttribute("data-slideshow-direction");
  await page.keyboard.press("Escape");
  await expect(page.locator("main [data-slideshow-slides] > section")).toHaveCount(12);
});

test("navigates without animations for reduced motion or unsupported browsers", async ({ page }) => {
  await page.emulateMedia({ reducedMotion: "reduce" });
  await startSlideshow(page);
  await page.keyboard.press("ArrowRight");
  await expect(presentation(page).locator("[data-slideshow-status]")).toContainText("2 / 12");
  await expect(page.locator("html")).not.toHaveAttribute("data-slideshow-direction");
  await page.emulateMedia({ reducedMotion: "no-preference" });
  await page.evaluate(() => {
    Object.defineProperty(document, "startViewTransition", { value: undefined, configurable: true });
  });
  await page.keyboard.press("ArrowRight");
  await expect(presentation(page).locator("[data-slideshow-status]")).toContainText("3 / 12");
  await expect(page.locator("html")).not.toHaveAttribute("data-slideshow-direction");
});

test("cleans up on page swaps and works after navigating away and back", async ({ page }) => {
  const errors: string[] = [];
  page.on("pageerror", error => errors.push(error.message));
  await startSlideshow(page);
  await page.evaluate(() => document.dispatchEvent(new Event("astro:before-swap")));
  await expect(presentation(page)).not.toBeVisible();
  await expect(page.locator("main [data-slideshow-slides] > section")).toHaveCount(12);
  await page.evaluate(() => document.dispatchEvent(new Event("astro:page-load")));
  await startSlideshow(page);
  await page.keyboard.press("ArrowRight");
  await presentation(page).getByRole("link", { name: "Create a workflow" }).first().click();
  await expect(page).toHaveURL(/\/gh-aw\/setup\/creating-workflows\//);
  await expect(page.locator("[data-slideshow-trigger]")).toHaveCount(0);
  await page.goBack();
  await expect(page).toHaveURL(/\/gh-aw\/$/);
  await startSlideshow(page);
  await expect(presentation(page).locator("[data-slideshow-status]")).toContainText("1 / 12");
  await page.keyboard.press("Escape");
  expect(errors).toEqual([]);
});
