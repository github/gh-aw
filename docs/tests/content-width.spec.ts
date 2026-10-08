import { test, expect } from "@playwright/test";

test("documentation fills the available column without changing the hero", async ({ page }) => {
  await page.setViewportSize({ width: 1920, height: 1080 });
  await page.goto("/gh-aw/reference/work-queue/");

  const widths = await page.locator("main > .content-panel:has(h1#_top) + .content-panel").evaluate(panel => {
    const container = panel.querySelector(".sl-container");
    if (!container) throw new Error("Documentation content container not found");
    const styles = getComputedStyle(panel);
    return {
      available: panel.clientWidth - parseFloat(styles.paddingLeft) - parseFloat(styles.paddingRight),
      content: container.getBoundingClientRect().width,
    };
  });
  expect(widths.available).toBeGreaterThan(608);
  expect(widths.content).toBeGreaterThanOrEqual(widths.available - 2);

  await page.goto("/gh-aw/");
  expect(await page.locator("body").evaluate(body => getComputedStyle(body).getPropertyValue("--sl-content-width").trim())).toBe("38rem");
  expect(
    await page
      .locator(".aw-hero-inner")
      .first()
      .evaluate(hero => getComputedStyle(hero).maxWidth)
  ).toBe("1152px");
});
