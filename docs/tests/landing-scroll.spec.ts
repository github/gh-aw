import { test, expect, type Locator, type Page } from "@playwright/test";

const sections = (page: Page) => page.locator("main [data-slideshow-slides] > section:visible");

async function expectAligned(section: Locator) {
  await expect
    .poll(() =>
      section.evaluate(element => {
        const header = document.querySelector(".header")!;
        return Math.abs(element.getBoundingClientRect().top - header.getBoundingClientRect().bottom);
      })
    )
    .toBeLessThanOrEqual(1);
}

for (const viewport of [
  { width: 1440, height: 900 },
  { width: 390, height: 844 },
]) {
  test.describe(`Landing section scrolling at ${viewport.width}px`, () => {
    test.use({ viewport, reducedMotion: "no-preference" });

    test("snaps wheel scrolling to logical sections below the fixed header", async ({ page }) => {
      await page.goto("/gh-aw/");
      await page.evaluate(() => document.fonts.ready);
      await expect(page.locator("html")).toHaveCSS("scroll-snap-type", /^y(?: proximity)?$/);
      const slides = sections(page);
      await expect(slides.first()).toHaveClass(/aw-hero/);
      await expect(slides.nth(1)).toHaveCSS("scroll-snap-align", "start");
      await page.mouse.move(viewport.width - 20, viewport.height / 2);

      for (const index of [1, 2, 1]) {
        const section = slides.nth(index);
        const delta = await section.evaluate(element => {
          const header = document.querySelector(".header")!;
          return element.getBoundingClientRect().top - header.getBoundingClientRect().bottom;
        });
        await page.mouse.wheel(0, delta - 60);
        await expectAligned(section);
      }
    });

    test("keeps the footer reachable by keyboard", async ({ page }) => {
      await page.goto("/gh-aw/");
      await page.keyboard.press("End");
      await expect.poll(() => page.evaluate(() => document.documentElement.scrollHeight - window.innerHeight - window.scrollY)).toBeLessThanOrEqual(1);
      await expect(page.locator(".site-footer")).toBeInViewport();
    });
  });
}

test("allows scrolling through the middle of a tall section on mobile", async ({ page }) => {
  await page.setViewportSize({ width: 390, height: 844 });
  await page.emulateMedia({ reducedMotion: "no-preference" });
  await page.goto("/gh-aw/");
  await page.evaluate(() => document.fonts.ready);
  const target = await sections(page).evaluateAll(elements => {
    const tallest = elements.reduce((a, b) => (a.clientHeight > b.clientHeight ? a : b));
    const header = document.querySelector(".header")!.getBoundingClientRect().height;
    const available = window.innerHeight - header;
    if (tallest.clientHeight < available * 2) throw new Error("Expected a section taller than two viewports");
    return window.scrollY + tallest.getBoundingClientRect().top + (tallest.clientHeight - available) / 2 - header;
  });
  await page.evaluate(top => window.scrollTo({ top, behavior: "instant" }), target);
  await expect.poll(() => page.evaluate(() => window.scrollY)).toBeCloseTo(target, 0);
  await page.mouse.move(370, 400);
  await page.mouse.wheel(0, 100);
  await expect.poll(() => page.evaluate(() => window.scrollY)).toBeCloseTo(target + 100, 0);
});

test("disables snapping for reduced motion and on documentation pages", async ({ page }) => {
  await page.emulateMedia({ reducedMotion: "reduce" });
  await page.goto("/gh-aw/");
  await expect(page.locator("html")).toHaveCSS("scroll-snap-type", "none");
  await expect(sections(page).first()).toHaveCSS("scroll-snap-align", "none");
  await page.emulateMedia({ reducedMotion: "no-preference" });
  await expect(page.locator("html")).toHaveCSS("scroll-snap-type", /^y(?: proximity)?$/);
  await page.goto("/gh-aw/setup/creating-workflows/");
  await expect(page.locator("html")).toHaveCSS("scroll-snap-type", "none");
});
