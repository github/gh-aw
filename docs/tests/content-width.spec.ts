import { test, expect } from "@playwright/test";

// Docs prose is capped at a comfortable reading measure; tables, diagrams and
// images may run wider. See .github/skills/docs-design/SKILL.md.
const PROSE_WIDTH = 620;
const MIN_GUTTER = 60;

test("docs prose stays within the reading measure, centred with gutters", async ({ page }) => {
  await page.setViewportSize({ width: 1920, height: 1080 });
  await page.goto("/gh-aw/reference/work-queue/");

  const layout = await page.evaluate(() => {
    const box = (el: Element | null) => {
      if (!el) throw new Error("Element not found");
      const r = el.getBoundingClientRect();
      return { left: r.left, right: r.right, width: r.width };
    };
    return {
      sidebar: box(document.querySelector("#starlight__sidebar")),
      toc: box(document.querySelector(".right-sidebar-panel")),
      title: box(document.querySelector("h1#_top")),
      footer: box(document.querySelector(".content-panel footer")),
      prose: [...document.querySelectorAll(".sl-markdown-content > :not(.table-scroll-wrapper, pre.mermaid, figure, picture, img, video)")].map(el => box(el)),
      tables: [...document.querySelectorAll(".sl-markdown-content > .table-scroll-wrapper")].map(el => box(el)),
    };
  });

  for (const el of [layout.title, layout.footer, ...layout.prose]) {
    expect(el.width).toBeLessThanOrEqual(PROSE_WIDTH + 1);
    expect(el.left - layout.sidebar.right).toBeGreaterThanOrEqual(MIN_GUTTER);
    expect(layout.toc.left - el.right).toBeGreaterThanOrEqual(MIN_GUTTER);
  }

  // Centred between the sidebar and "On this page"
  const p = layout.prose[0];
  expect(Math.abs(p.left - layout.sidebar.right - (layout.toc.left - p.right))).toBeLessThanOrEqual(2);

  // Wide tables may run past the measure, still clear of the navigation
  expect(Math.max(...layout.tables.map(t => t.width))).toBeGreaterThan(PROSE_WIDTH);
  for (const t of layout.tables) {
    expect(t.left - layout.sidebar.right).toBeGreaterThanOrEqual(MIN_GUTTER);
    expect(layout.toc.left - t.right).toBeGreaterThanOrEqual(MIN_GUTTER);
  }
});

test("the landing page keeps its own wide layout", async ({ page }) => {
  await page.setViewportSize({ width: 1920, height: 1080 });
  await page.goto("/gh-aw/");
  expect(
    await page
      .locator(".aw-hero-inner")
      .first()
      .evaluate(hero => getComputedStyle(hero).maxWidth)
  ).toBe("1152px");
});

test("specs open in a full-viewport zen layout with a readable measure", async ({ page }) => {
  await page.setViewportSize({ width: 1920, height: 1080 });
  await page.goto("/gh-aw/specs/unified-agent-session-specification/#abstract");

  const title = page.getByRole("heading", { level: 1, name: "Unified Agent Session Specification" });
  await expect(title).toBeVisible();
  await expect(page.getByText("Specification", { exact: true })).toBeVisible();
  const abstract = page.locator("#abstract");
  await expect(abstract).toBeInViewport();
  const section = abstract.locator("..");
  await expect(section).toHaveCSS("border-block-start-width", "1px");
  expect((await section.boundingBox())?.width).toBeLessThanOrEqual(PROSE_WIDTH + 1);

  const frame = page.locator(".main-pane");
  const bounds = await frame.boundingBox();
  expect(bounds?.x).toBe(0);
  expect(bounds?.width).toBe(await page.evaluate(() => document.documentElement.clientWidth));
  const chrome = [".page > header", "#starlight__sidebar", ".right-sidebar-panel", ".content-panel footer", ".site-footer"];
  for (const selector of chrome) await expect(page.locator(selector)).toBeHidden();

  const toggle = page.getByRole("button", { name: "Zen mode" });
  await expect(toggle).toHaveAttribute("aria-pressed", "true");
  await toggle.focus();
  await toggle.press("Space");
  await expect(toggle).toHaveAttribute("aria-pressed", "false");
  await expect(toggle).toBeFocused();
  for (const selector of chrome) await expect(page.locator(selector)).toBeVisible();
  await toggle.press("Space");
  await expect(page.locator("#starlight__sidebar")).toBeHidden();

  await page.goto("/gh-aw/reference/work-queue/");
  await expect(toggle).toHaveCount(0);
  await expect(page.locator(".spec-label")).toHaveCount(0);
  await expect(page.locator(".sl-markdown-content > .level-h2").first()).toHaveCSS("border-block-start-width", "0px");
});
